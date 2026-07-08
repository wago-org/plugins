package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wago-org/registry-backend/internal/config"
	"github.com/wago-org/registry-backend/internal/model"
)

// githubHTTP is the shared client for GitHub API calls.
var githubHTTP = &http.Client{Timeout: 15 * time.Second}

// GitHub performs the OAuth code exchange and user/email lookups.
type GitHub struct {
	clientID     string
	clientSecret string
	redirectURL  string
}

// NewGitHub builds a GitHub client from config.
func NewGitHub(cfg config.Config) *GitHub {
	return &GitHub{
		clientID:     cfg.GithubClientID,
		clientSecret: cfg.GithubClientSecret,
		redirectURL:  cfg.OAuthRedirectURL,
	}
}

// ghUser is the subset of https://api.github.com/user we consume.
type ghUser struct {
	ID              int64  `json:"id"`
	Login           string `json:"login"`
	Name            string `json:"name"`
	AvatarURL       string `json:"avatar_url"`
	Email           string `json:"email"`
	Bio             string `json:"bio"`
	Company         string `json:"company"`
	Location        string `json:"location"`
	Blog            string `json:"blog"`
	TwitterUsername string `json:"twitter_username"`
	Followers       int    `json:"followers"`
	Following       int    `json:"following"`
	PublicRepos     int    `json:"public_repos"`
	CreatedAt       string `json:"created_at"`
	HTMLURL         string `json:"html_url"`
	Hireable        bool   `json:"hireable"`
}

// ghEmail is one entry from https://api.github.com/user/emails.
type ghEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

// AuthorizeURL builds the GitHub OAuth authorize URL for the given state. When
// star is true the public_repo scope is also requested, letting the registry
// star repositories on the user's behalf.
func (g *GitHub) AuthorizeURL(state string, star bool) string {
	scope := "read:user user:email"
	if star {
		scope += " public_repo"
	}
	q := url.Values{}
	q.Set("client_id", g.clientID)
	q.Set("redirect_uri", g.redirectURL)
	q.Set("scope", scope)
	q.Set("state", state)
	// Always show GitHub's account picker so users with multiple GitHub accounts
	// consciously choose one instead of being silently signed in with whichever
	// account is currently active in the browser.
	q.Set("prompt", "select_account")
	return "https://github.com/login/oauth/authorize?" + q.Encode()
}

// ExchangeCode swaps an OAuth code for an access token, returning the token and
// the comma-separated scopes GitHub actually granted.
func (g *GitHub) ExchangeCode(code string) (token, scope string, err error) {
	form := url.Values{}
	form.Set("client_id", g.clientID)
	form.Set("client_secret", g.clientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", g.redirectURL)

	req, err := http.NewRequest(http.MethodPost,
		"https://github.com/login/oauth/access_token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := githubHTTP.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", errors.New("github token exchange failed: " + resp.Status)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", "", err
	}
	if tok.Error != "" {
		return "", "", errors.New("github token error: " + tok.Error)
	}
	if tok.AccessToken == "" {
		return "", "", errors.New("github returned empty access token")
	}
	return tok.AccessToken, tok.Scope, nil
}

// SetStar stars (on=true) or unstars (on=false) owner/repo on behalf of the
// token's user. Requires a token with public_repo (or repo) scope.
func (g *GitHub) SetStar(token, owner, repo string, on bool) error {
	method := http.MethodDelete
	if on {
		method = http.MethodPut
	}
	endpoint := "https://api.github.com/user/starred/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	req, err := http.NewRequest(method, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Length", "0")
	resp, err := githubHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// 204 = success; 304 = already in desired state.
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil
	}
	return errors.New("github star failed: " + resp.Status)
}

// OrgRole returns the token user's membership role in org — "admin" for owners,
// "member" otherwise — via GET /user/memberships/orgs/{org}. It returns an empty
// role (and no error) when the user isn't an active member of the org; a non-nil
// error only signals a transient/API failure the caller may choose to ignore.
func (g *GitHub) OrgRole(token, org string) (string, error) {
	m, err := ghGetJSON[struct {
		State string `json:"state"`
		Role  string `json:"role"`
	}](token, "https://api.github.com/user/memberships/orgs/"+url.PathEscape(org))
	if err != nil {
		// A non-member yields 404, which ghGet surfaces as an error. Treat that as
		// "no role" rather than a hard failure so callers don't special-case it.
		if strings.Contains(err.Error(), "404") {
			return "", nil
		}
		return "", err
	}
	if m.State != "active" {
		return "", nil
	}
	return m.Role, nil
}

// FetchUser retrieves the authenticated GitHub user, resolving a primary email
// when the profile email is private.
func (g *GitHub) FetchUser(token string) (model.User, error) {
	gu, err := ghGetJSON[ghUser](token, "https://api.github.com/user")
	if err != nil {
		return model.User{}, err
	}
	if gu.ID == 0 {
		return model.User{}, errors.New("github user has no id")
	}
	email := gu.Email
	if email == "" {
		if emails, err := ghGetJSONSlice[ghEmail](token, "https://api.github.com/user/emails"); err == nil {
			for _, e := range emails {
				if e.Primary && e.Verified {
					email = e.Email
					break
				}
			}
			if email == "" && len(emails) > 0 {
				email = emails[0].Email
			}
		}
	}
	u := model.User{
		ID:              strconv.FormatInt(gu.ID, 10),
		Login:           gu.Login,
		Name:            gu.Name,
		AvatarURL:       gu.AvatarURL,
		Email:           email,
		Bio:             gu.Bio,
		Company:         gu.Company,
		Location:        gu.Location,
		Blog:            gu.Blog,
		TwitterUsername: gu.TwitterUsername,
		HTMLURL:         gu.HTMLURL,
		GithubCreatedAt: gu.CreatedAt,
		Followers:       gu.Followers,
		Following:       gu.Following,
		PublicRepos:     gu.PublicRepos,
		Hireable:        gu.Hireable,
	}
	// Seed the email list with the GitHub primary as a verified "github" entry.
	if email != "" {
		u.Emails = []model.UserEmail{{Address: email, Verified: true, Source: "github"}}
	}
	return u, nil
}

// ghGetJSON performs an authenticated GET and decodes a JSON object.
func ghGetJSON[T any](token, endpoint string) (T, error) {
	var out T
	body, err := ghGet(token, endpoint)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

// ghGetJSONSlice performs an authenticated GET and decodes a JSON array.
func ghGetJSONSlice[T any](token, endpoint string) ([]T, error) {
	body, err := ghGet(token, endpoint)
	if err != nil {
		return nil, err
	}
	var out []T
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ghGet performs an authenticated GET against the GitHub API.
func ghGet(token, endpoint string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "wago-registry-backend")

	resp, err := githubHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("github GET " + endpoint + " failed: " + resp.Status)
	}
	return body, nil
}
