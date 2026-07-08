package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wago-org/registry-backend/internal/auth"
	"github.com/wago-org/registry-backend/internal/httpx"
)

// handleLogin starts the GitHub OAuth flow: mint state, set the state cookie,
// redirect to GitHub's authorize endpoint.
func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := auth.RandomToken(24)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "state generation failed")
		return
	}
	http.SetCookie(w, a.Sessions.NewStateCookie(state))
	// ?star=1 additionally requests the public_repo scope so the registry can
	// star repositories on the user's behalf.
	star := r.URL.Query().Get("star") == "1"
	http.Redirect(w, r, a.GitHub.AuthorizeURL(state, star), http.StatusFound)
}

// handleCallback verifies the OAuth state, exchanges the code, upserts the user,
// sets the session cookie, and redirects to the frontend.
func (a *App) handleCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(reason string) {
		http.Redirect(w, r, a.Cfg.FrontendURL+"/#/auth?error="+reason, http.StatusFound)
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		fail("missing_code")
		return
	}
	if !a.Sessions.VerifyState(r, state) {
		fail("state_mismatch")
		return
	}

	token, scope, err := a.GitHub.ExchangeCode(code)
	if err != nil {
		log.Printf("oauth: token exchange: %v", err)
		fail("token_exchange")
		return
	}
	u, err := a.GitHub.FetchUser(token)
	if err != nil {
		log.Printf("oauth: fetch user: %v", err)
		fail("user_fetch")
		return
	}
	// Sync is non-destructive: preserve any user-added emails from the stored
	// record so re-auth never wipes a verified secondary email.
	if existing, ok := a.Store.GetUser(u.ID); ok {
		u = mergeAddedEmails(u, existing)
		u.CreatedAt = existing.CreatedAt // preserve original wago join date
	}
	// Stamp the wago membership date on first sign-in.
	if u.CreatedAt == "" {
		u.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	// sanitize clears server-only fields; set the fresh GitHub token/scopes after
	// it so they persist (and are only ever exposed via derived flags, not raw).
	u = sanitize(u)
	u.GitHubToken = token
	u.GitHubScopes = scope
	u.Admin = a.Cfg.IsAdmin(u.Login)
	if err := a.Store.UpsertUser(u); err != nil {
		log.Printf("oauth: upsert user: %v", err)
		fail("store")
		return
	}
	http.SetCookie(w, a.Sessions.ClearStateCookie())

	// CLI login: mint an API token and hand it back to the CLI's loopback
	// listener instead of setting a browser session.
	if ctx, ok := a.Sessions.CLIContext(r); ok {
		http.SetCookie(w, a.Sessions.ClearCLICookie())
		port, cliState, valid := parseCLIContext(ctx)
		if !valid {
			fail("cli_context")
			return
		}
		plaintext, _, err := a.Store.CreateToken(u.ID, "wago-cli")
		if err != nil {
			fail("token")
			return
		}
		dest := fmt.Sprintf("http://127.0.0.1:%s/callback?token=%s&state=%s",
			port, url.QueryEscape(plaintext), url.QueryEscape(cliState))
		http.Redirect(w, r, dest, http.StatusFound)
		return
	}

	http.SetCookie(w, a.Sessions.NewSessionCookie(u.ID))
	http.Redirect(w, r, a.Cfg.FrontendURL+"/#/account", http.StatusFound)
}

// handleCLILogin starts the CLI login flow: it records the CLI's loopback port
// and state, then hands off to the normal GitHub OAuth flow.
func (a *App) handleCLILogin(w http.ResponseWriter, r *http.Request) {
	port := r.URL.Query().Get("port")
	cliState := r.URL.Query().Get("state")
	if !isNumericPort(port) || cliState == "" {
		httpx.WriteError(w, http.StatusBadRequest, "port and state required")
		return
	}
	state, err := auth.RandomToken(24)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "state generation failed")
		return
	}
	http.SetCookie(w, a.Sessions.NewStateCookie(state))
	http.SetCookie(w, a.Sessions.NewCLICookie(port+"|"+cliState))
	http.Redirect(w, r, a.GitHub.AuthorizeURL(state, false), http.StatusFound)
}

func parseCLIContext(ctx string) (port, state string, ok bool) {
	i := strings.IndexByte(ctx, '|')
	if i < 0 {
		return "", "", false
	}
	port, state = ctx[:i], ctx[i+1:]
	return port, state, isNumericPort(port) && state != ""
}

func isNumericPort(p string) bool {
	if p == "" || len(p) > 5 {
		return false
	}
	for _, c := range p {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// handleLogout clears the session cookie.
func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, a.Sessions.ClearSessionCookie())
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleMe returns the current user, or 401.
func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	// Expose only a derived capability flag, never the token/scopes themselves.
	canStar := u.GitHubToken != "" && hasStarScope(u.GitHubScopes)
	su := sanitize(*u)
	raw, _ := json.Marshal(su)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["canStar"] = canStar
	httpx.WriteJSON(w, http.StatusOK, m)
}

// handlePublicUser returns a public profile for a registered wago user by login,
// or 404 if no one with that login has signed in. No auth required and no
// sensitive fields (email, token) are exposed — the client uses this to show a
// claimed "wago member" profile, falling back to a generated one otherwise.
func (a *App) handlePublicUser(w http.ResponseWriter, r *http.Request) {
	u, ok := a.Store.GetUserByLogin(r.PathValue("login"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "user not found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"login":           u.Login,
		"name":            u.Name,
		"avatarUrl":       u.AvatarURL,
		"bio":             u.Bio,
		"company":         u.Company,
		"location":        u.Location,
		"blog":            u.Blog,
		"twitterUsername": u.TwitterUsername,
		"htmlUrl":         u.HTMLURL,
		"githubCreatedAt": u.GithubCreatedAt,
		"createdAt":       u.CreatedAt,
		"followers":       u.Followers,
		"following":       u.Following,
		"publicRepos":     u.PublicRepos,
		"starsGiven":      len(a.Store.StarsForUser(u.ID)),
		"claimed":         true,
	})
}

// handleCreateToken mints an API token for the current user (for CI use). The
// plaintext is returned once and never stored.
func (a *App) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Label string `json:"label"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	plaintext, tok, err := a.Store.CreateToken(u.ID, req.Label)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "token creation failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"token": plaintext, "id": tok.ID, "label": tok.Label, "createdAt": tok.CreatedAt,
	})
}

// handleListTokens lists the current user's tokens (hashes omitted).
func (a *App) handleListTokens(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tokens": a.Store.ListTokens(u.ID)})
}

// handleRevokeToken revokes one of the current user's tokens.
func (a *App) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := a.Store.RevokeToken(u.ID, r.PathValue("id")); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "revoke failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
