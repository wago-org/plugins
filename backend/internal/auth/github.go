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
// star repositories on the user's behalf. read:org is always requested so the
// registry can list the user's organizations and verify which ones they
// administer (the signal that lets them act on the org's behalf).
func (g *GitHub) AuthorizeURL(state string, star bool) string {
	scope := "read:user user:email read:org"
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

// RepoAccess returns the token user's permission on owner/repo — one of "admin",
// "maintain", "write", "triage", "read", or "none" — plus whether the repo is
// owned by an organization, from GET /repos/{owner}/{repo}. "admin" means the
// user is the author/owner (a user repo's owner, or an org owner/admin). An error
// means the repo couldn't be read at all (missing, or private without repo scope),
// which the caller treats as "no verified access". Public repos work with any
// signed-in user's token.
func (g *GitHub) RepoAccess(token, owner, repo string) (perm string, isOrg bool, err error) {
	r, err := ghGetJSON[struct {
		Owner struct {
			Type string `json:"type"`
		} `json:"owner"`
		Permissions struct {
			Admin    bool `json:"admin"`
			Maintain bool `json:"maintain"`
			Push     bool `json:"push"`
			Triage   bool `json:"triage"`
			Pull     bool `json:"pull"`
		} `json:"permissions"`
	}](token, "https://api.github.com/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repo))
	if err != nil {
		return "", false, err
	}
	isOrg = strings.EqualFold(r.Owner.Type, "Organization")
	switch {
	case r.Permissions.Admin:
		return "admin", isOrg, nil
	case r.Permissions.Maintain:
		return "maintain", isOrg, nil
	case r.Permissions.Push:
		return "write", isOrg, nil
	case r.Permissions.Triage:
		return "triage", isOrg, nil
	case r.Permissions.Pull:
		return "read", isOrg, nil
	}
	return "none", isOrg, nil
}

// ResolveTagCommit resolves an exact release tag to the commit GitHub serves for
// it. The commits endpoint dereferences both annotated and lightweight tags.
func (g *GitHub) ResolveTagCommit(token, owner, repo, tag string) (string, error) {
	tagSegments := strings.Split(tag, "/")
	for index := range tagSegments {
		tagSegments[index] = url.PathEscape(tagSegments[index])
	}
	endpoint := "https://api.github.com/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/commits/" + strings.Join(tagSegments, "/")
	body, err := ghGetMedia(token, endpoint, "application/vnd.github.sha")
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(string(body))
	if !fullLowerGitCommit(commit) {
		return "", errors.New("github returned an invalid commit SHA for the release tag")
	}
	return commit, nil
}

func fullLowerGitCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

// Org is one of the authenticated user's GitHub organizations, with the profile
// bits the registry surfaces and the viewer's role in it. Role is "admin" when
// the user is an org owner (or the special-case single-owner), else "member".
type Org struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
	Role      string `json:"role"`
	Bio       string `json:"bio,omitempty"`
	Blog      string `json:"blog,omitempty"`
	Location  string `json:"location,omitempty"`
	HTMLURL   string `json:"htmlUrl,omitempty"`
	CreatedAt string `json:"githubCreatedAt,omitempty"`
}

// ghMembership is one entry of GET /user/memberships/orgs — the viewer's role in
// each org plus a nested organization profile summary.
type ghMembership struct {
	Role         string `json:"role"` // "admin" | "member"
	State        string `json:"state"`
	Organization struct {
		Login       string `json:"login"`
		AvatarURL   string `json:"avatar_url"`
		Description string `json:"description"`
	} `json:"organization"`
}

// FetchOrgs lists the authenticated user's organization memberships (active
// only), newest GitHub API page first. Requires the read:org scope. The role
// distinguishes org owners ("admin") from plain members.
func (g *GitHub) FetchOrgs(token string) ([]Org, error) {
	ms, err := ghGetJSONSlice[ghMembership](token, "https://api.github.com/user/memberships/orgs?per_page=100&state=active")
	if err != nil {
		return nil, err
	}
	out := make([]Org, 0, len(ms))
	for _, m := range ms {
		if m.Organization.Login == "" {
			continue
		}
		out = append(out, Org{
			Login:     m.Organization.Login,
			Name:      m.Organization.Login,
			AvatarURL: m.Organization.AvatarURL,
			Role:      strings.ToLower(m.Role),
			Bio:       m.Organization.Description,
		})
	}
	return out, nil
}

// OrgRole returns the authenticated user's role in a single organization
// ("admin", "member") or "" when they are not a member / it can't be read.
func (g *GitHub) OrgRole(token, org string) string {
	m, err := ghGetJSON[ghMembership](token, "https://api.github.com/user/memberships/orgs/"+url.PathEscape(org))
	if err != nil || m.State != "active" {
		return ""
	}
	return strings.ToLower(m.Role)
}

// ghOrg is the subset of GET /orgs/{org} we surface on an org profile.
type ghOrg struct {
	Login       string `json:"login"`
	Name        string `json:"name"`
	AvatarURL   string `json:"avatar_url"`
	Description string `json:"description"`
	Blog        string `json:"blog"`
	Location    string `json:"location"`
	HTMLURL     string `json:"html_url"`
	CreatedAt   string `json:"created_at"`
}

// FetchOrgProfile retrieves an organization's public profile, used to seed the
// org's pseudo-user record the first time a member acts on its behalf.
func (g *GitHub) FetchOrgProfile(token, org string) (Org, error) {
	o, err := ghGetJSON[ghOrg](token, "https://api.github.com/orgs/"+url.PathEscape(org))
	if err != nil {
		return Org{}, err
	}
	name := o.Name
	if name == "" {
		name = o.Login
	}
	return Org{
		Login:     o.Login,
		Name:      name,
		AvatarURL: o.AvatarURL,
		Bio:       o.Description,
		Blog:      o.Blog,
		Location:  o.Location,
		HTMLURL:   o.HTMLURL,
		CreatedAt: o.CreatedAt,
	}, nil
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
	return ghGetMedia(token, endpoint, "application/vnd.github+json")
}

func ghGetMedia(token, endpoint, accept string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", accept)
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
