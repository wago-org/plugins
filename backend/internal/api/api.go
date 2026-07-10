// Package api wires the registry's HTTP endpoints. The App struct holds the
// config, store, and auth dependencies; NewRouter builds the net/http 1.22
// method+pattern mux and wraps it with CORS. This package sits at the top of the
// import graph (model, store, auth, httpx, config).
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/wago-org/registry-backend/internal/auth"
	"github.com/wago-org/registry-backend/internal/config"
	"github.com/wago-org/registry-backend/internal/email"
	"github.com/wago-org/registry-backend/internal/httpx"
	"github.com/wago-org/registry-backend/internal/model"
	"github.com/wago-org/registry-backend/internal/store"
)

// App holds the shared runtime dependencies for the HTTP handlers.
type App struct {
	Cfg      config.Config
	Store    store.Store
	Sessions *auth.Sessions
	GitHub   *auth.GitHub
	Email    *email.Sender
	Device   *deviceAuth
	list     *listCache
	orgRoles orgRoleCache
}

// New builds an App and its auth dependencies from config and a store.
func New(cfg config.Config, st store.Store) *App {
	return &App{
		Cfg:      cfg,
		Store:    st,
		Sessions: auth.NewSessions(cfg, st),
		GitHub:   auth.NewGitHub(cfg),
		Email: email.New(email.Config{
			Host: cfg.SMTPHost,
			Port: cfg.SMTPPort,
			User: cfg.SMTPUser,
			Pass: cfg.SMTPPass,
			From: cfg.SMTPFrom,
		}),
		Device: newDeviceAuth(),
		list:   &listCache{},
	}
}

// listCache holds pre-marshaled anonymous responses — the default package list
// and every package's detail — the two dominant browse requests. Both are rebuilt
// together at most once per listTTL, so read throughput is decoupled from the
// per-request decorate cost and the install write rate (counts are at most listTTL
// stale). Filtered or authenticated requests bypass the cache.
type listCache struct {
	mu      sync.RWMutex
	list    []byte
	details map[string][]byte // package short -> decorated detail JSON
	builtAt time.Time
}

const listTTL = time.Second

// isDefaultListQuery reports whether a list request carries no filters/sort, so
// its response is identical for every anonymous viewer and can be cached.
func isDefaultListQuery(q url.Values) bool {
	for _, k := range []string{"q", "category", "tag", "stability", "engine", "verified", "sort"} {
		if q.Get(k) != "" {
			return false
		}
	}
	return true
}

// refreshCache rebuilds the anonymous list + detail snapshots when stale. Only
// one goroutine rebuilds; others serve the current bytes.
func (a *App) refreshCache() {
	a.list.mu.RLock()
	fresh := a.list.list != nil && time.Since(a.list.builtAt) < listTTL
	a.list.mu.RUnlock()
	if fresh {
		return
	}
	a.list.mu.Lock()
	defer a.list.mu.Unlock()
	if a.list.list != nil && time.Since(a.list.builtAt) < listTTL {
		return
	}
	all := a.Store.ListPackages()
	pkgs := a.filterPackages(all, "", "", "", "", "", false)
	a.sortPackages(pkgs, "")
	out := make([]map[string]any, 0, len(pkgs))
	details := make(map[string][]byte, len(all))
	for _, p := range pkgs {
		dec := a.decoratePackage(p, "")
		// Cache the full detail first, then trim the same map for the lean list.
		if b, err := json.Marshal(dec); err == nil {
			details[p.Short] = b
		}
		out = append(out, trimForList(dec))
	}
	if b, err := json.Marshal(map[string]any{"packages": out, "total": len(out)}); err == nil {
		a.list.list = b
		a.list.details = details
		a.list.builtAt = time.Now()
	}
}

// cachedDefaultList returns the pre-marshaled default list (nil if unbuilt).
func (a *App) cachedDefaultList() []byte {
	a.refreshCache()
	a.list.mu.RLock()
	defer a.list.mu.RUnlock()
	return a.list.list
}

// cachedDetail returns the pre-marshaled anonymous detail for short (nil if
// absent — caller falls back to the compute path).
func (a *App) cachedDetail(short string) []byte {
	a.refreshCache()
	a.list.mu.RLock()
	defer a.list.mu.RUnlock()
	return a.list.details[short]
}

// NewRouter registers every endpoint and wraps the mux with CORS.
func (a *App) NewRouter() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", a.handleHealth)

	// Auth.
	mux.HandleFunc("GET /api/auth/github/client", a.handleGithubClient)
	mux.HandleFunc("POST /api/auth/github/exchange", a.handleGithubExchange)
	mux.HandleFunc("GET /auth/github/login", a.handleLogin)
	mux.HandleFunc("GET /auth/cli/login", a.handleCLILogin)
	mux.HandleFunc("GET /auth/github/callback", a.handleCallback)

	// Device authorization (RFC 8628) — CLI login for headless/remote machines.
	mux.HandleFunc("GET /device", a.handleDevicePage)
	mux.HandleFunc("POST /api/device/code", a.handleDeviceCode)
	mux.HandleFunc("POST /api/device/token", a.handleDeviceToken)
	mux.HandleFunc("POST /api/device/approve", a.handleDeviceApprove)
	mux.HandleFunc("POST /api/logout", a.handleLogout)
	mux.HandleFunc("GET /api/me", a.handleMe)
	mux.HandleFunc("GET /api/me/stars", a.handleMyStars)
	mux.HandleFunc("GET /api/users/{login}", a.handlePublicUser)

	// Secondary emails (add + verify with an emailed code).
	mux.HandleFunc("GET /api/me/emails", a.handleListEmails)
	mux.HandleFunc("POST /api/me/emails", a.handleAddEmail)
	mux.HandleFunc("POST /api/me/emails/verify", a.handleVerifyEmail)
	mux.HandleFunc("DELETE /api/me/emails/{email}", a.handleDeleteEmail)

	// API tokens (CLI / CI).
	mux.HandleFunc("POST /api/tokens", a.handleCreateToken)
	mux.HandleFunc("GET /api/tokens", a.handleListTokens)
	mux.HandleFunc("DELETE /api/tokens/{id}", a.handleRevokeToken)

	// Packages.
	mux.HandleFunc("GET /api/packages", a.handleListPackages)
	mux.HandleFunc("GET /api/packages/{name}", a.handleGetPackage)
	mux.HandleFunc("GET /api/packages/{name}/versions", a.handleVersions)

	// Installs.
	mux.HandleFunc("POST /api/packages/{name}/installs", a.handleRecordInstall)
	mux.HandleFunc("GET /api/packages/{name}/installs", a.handleInstallSeries)

	// Social: stars, reviews, votes, comments.
	mux.HandleFunc("POST /api/packages/{name}/star", a.handleStar)
	mux.HandleFunc("DELETE /api/packages/{name}/star", a.handleUnstar)
	mux.HandleFunc("POST /api/packages/{name}/gh-star", a.handleGitHubStar)
	mux.HandleFunc("DELETE /api/packages/{name}/gh-star", a.handleGitHubUnstar)
	mux.HandleFunc("GET /api/packages/{name}/reviews", a.handleListReviews)
	mux.HandleFunc("POST /api/packages/{name}/reviews", a.handleCreateReview)
	mux.HandleFunc("POST /api/reviews/{id}/vote", a.handleVote)
	mux.HandleFunc("DELETE /api/reviews/{id}", a.handleDeleteReview)
	mux.HandleFunc("GET /api/packages/{name}/comments", a.handleListComments)
	mux.HandleFunc("POST /api/packages/{name}/comments", a.handleCreateComment)
	mux.HandleFunc("POST /api/comments/{id}/vote", a.handleVoteComment)
	mux.HandleFunc("POST /api/comments/{id}/archive", a.handleArchiveComment)
	mux.HandleFunc("PUT /api/comments/{id}", a.handleUpdateComment)
	mux.HandleFunc("DELETE /api/comments/{id}", a.handleDeleteComment)

	// Moderation.
	mux.HandleFunc("POST /api/packages/{name}/report", a.handleReportPackage)
	mux.HandleFunc("GET /api/reports", a.handleListReports)
	mux.HandleFunc("POST /api/reports/{id}/resolve", a.handleResolveReport)

	// Publish / manage.
	mux.HandleFunc("POST /api/publish", a.handlePublish)
	mux.HandleFunc("DELETE /api/packages/{name}", a.handleUnpublishPackage)
	mux.HandleFunc("DELETE /api/packages/{name}/versions/{version}", a.handleUnpublishVersion)
	mux.HandleFunc("POST /api/packages/{name}/deprecate", a.handleDeprecate)
	mux.HandleFunc("PUT /api/packages/{name}/publishers", a.handleSetPublishers)

	return httpx.CORS(a.Cfg.FrontendURL, mux)
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "packages": a.Store.PackageCount()})
}

// decodeJSON decodes a size-limited JSON body into v.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any, max int64) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, max)).Decode(v)
}

// compactCount renders an install count in a compact form: >=1e6 -> "4.2M",
// >=1e3 -> "48.2k", else the number.
func compactCount(n int) string {
	switch {
	case n >= 1_000_000:
		return trimZero(float64(n)/1_000_000) + "M"
	case n >= 1_000:
		return trimZero(float64(n)/1_000) + "k"
	default:
		return strconv.Itoa(n)
	}
}

// trimZero formats a float to one decimal place, dropping a trailing ".0".
func trimZero(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	if len(s) > 2 && s[len(s)-2:] == ".0" {
		return s[:len(s)-2]
	}
	return s
}

// decoratePackage marshals a stored Package to a map and layers on the derived
// fields the frontend expects (live stars, installs, latest-version convenience
// fields). When viewerID is non-empty, "starred" is included.
func (a *App) decoratePackage(p model.Package, viewerID string) map[string]any {
	raw, _ := json.Marshal(p)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)

	latest := p.LatestVersion()
	m["version"] = latest.Version
	m["latestVersion"] = latest.Version
	if latest.PublishedAt != "" {
		m["updatedAt"] = latest.PublishedAt
	}

	m["stars"] = p.Stars + a.Store.StarCount(p.Short)
	if viewerID != "" {
		m["starred"] = a.Store.IsStarred(p.Short, viewerID)
	}

	week := a.Store.InstallWeek(p.Short)
	total := a.Store.InstallTotal(p.Short)
	m["installsWeek"] = week
	m["installsWeekLabel"] = compactCount(week)
	m["installsTotal"] = total

	return m
}

// viewerID returns the current user's id, or "" when unauthenticated.
func (a *App) viewerID(r *http.Request) string {
	if u := a.Sessions.CurrentUser(r); u != nil {
		return u.ID
	}
	return ""
}
