package api

import (
	"net/http"
	"strings"

	"github.com/wago-org/registry-backend/internal/httpx"
	"github.com/wago-org/registry-backend/internal/model"
)

// ownedPackage resolves the package named in the URL and checks that the current
// user may manage it — its owner (directly, or as an owner/admin of the GitHub
// org that owns it), or a site admin. On any failure it writes the response and
// returns ok=false.
func (a *App) ownedPackage(w http.ResponseWriter, r *http.Request) (model.Package, bool) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return model.Package{}, false
	}
	p, ok := a.Store.GetPackage(r.PathValue("name"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "package not found")
		return model.Package{}, false
	}
	if !a.ownsPackage(u, p) {
		httpx.WriteError(w, http.StatusForbidden, "not the package owner")
		return model.Package{}, false
	}
	return p, true
}

// decorateForViewer decorates p and adds a viewer-specific canManage flag
// (org-aware), so the frontend can surface owner controls for org owners/admins
// too, not just the literal owner login.
func (a *App) decorateForViewer(p model.Package, u *model.User) map[string]any {
	id := ""
	if u != nil {
		id = u.ID
	}
	m := a.decoratePackage(p, id)
	canManage := a.ownsPackage(u, p)
	m["canManage"] = canManage
	// Managers also see outstanding publish invites (not yet accepted) so the
	// publishers editor can show them as pending chips.
	if canManage {
		pending := []map[string]string{}
		for _, n := range a.Store.PendingNotifications(p.Short, model.NotifyPublishInvite) {
			pending = append(pending, map[string]string{"login": n.Recipient, "id": n.ID})
		}
		m["pendingPublishers"] = pending
	}
	return m
}

// handleUnpublishPackage removes an entire package (owner only).
func (a *App) handleUnpublishPackage(w http.ResponseWriter, r *http.Request) {
	unlockPackage := a.lockPackageWrite(r.PathValue("name"))
	defer unlockPackage()
	p, ok := a.ownedPackage(w, r)
	if !ok {
		return
	}
	if err := a.Store.DeletePackage(p.Short); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "unpublished": p.Short})
}

// publishersRequest is the body of PUT /api/packages/{name}/publishers.
type publishersRequest struct {
	Publishers []string `json:"publishers"`
}

// handleSetPublishers sets the package's allowed publishers — extra GitHub logins
// (beyond the repo's author/admins) permitted to publish. Owner / admin only.
func (a *App) handleSetPublishers(w http.ResponseWriter, r *http.Request) {
	unlockPackage := a.lockPackageWrite(r.PathValue("name"))
	defer unlockPackage()
	p, ok := a.ownedPackage(w, r)
	if !ok {
		return
	}
	var req publishersRequest
	if err := decodeJSON(w, r, &req, 1<<16); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	seen := map[string]bool{}
	out := []string{}
	for _, s := range req.Publishers {
		login := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "@"))
		// Skip blanks, the owner (always allowed), and duplicates.
		if login == "" || strings.EqualFold(login, p.OwnerLogin) {
			continue
		}
		if key := strings.ToLower(login); !seen[key] {
			seen[key] = true
			out = append(out, login)
		}
	}
	p.AllowedPublishers = out
	if err := a.Store.UpsertPackage(p); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.decorateForViewer(p, a.Sessions.CurrentUser(r)))
}

// transferRequest is the body of POST /api/packages/{name}/transfer.
type transferRequest struct {
	Owner string `json:"owner"`
}

// handleTransfer reassigns a package's owner login, or sends a transfer invite.
// The caller must currently manage the package. Transferring to their own login
// or to the GitHub org that owns the source repo (verified via repo admin
// access) applies immediately — they're already the authority. Transferring to
// any other login creates a pending invite the recipient must accept.
func (a *App) handleTransfer(w http.ResponseWriter, r *http.Request) {
	unlockPackage := a.lockPackageWrite(r.PathValue("name"))
	defer unlockPackage()
	p, ok := a.ownedPackage(w, r)
	if !ok {
		return
	}
	u := a.Sessions.CurrentUser(r) // non-nil once ownedPackage passed
	var req transferRequest
	if err := decodeJSON(w, r, &req, 1<<16); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(req.Owner), "@"))
	if target == "" {
		httpx.WriteError(w, http.StatusBadRequest, "owner is required")
		return
	}
	// Already the owner (case-insensitive): nothing to do.
	if strings.EqualFold(target, p.OwnerLogin) {
		httpx.WriteJSON(w, http.StatusOK, a.decorateForViewer(p, u))
		return
	}

	// Immediate paths: your own login, or the org that owns the source repo when
	// you have admin on it (org owners / repo admins), which needs no read:org.
	immediate := strings.EqualFold(target, u.Login)
	if !immediate {
		if repoOwner, repo, ok := parseGitHubRepo(p.Repository); ok &&
			strings.EqualFold(target, repoOwner) && a.repoAdmin(u, repoOwner, repo) {
			immediate = true
		}
	}
	if immediate {
		p.OwnerLogin = target
		if err := a.Store.UpsertPackage(p); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "store error")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, a.decorateForViewer(p, u))
		return
	}

	// Otherwise it's an offer to another account: create a pending transfer invite
	// the recipient accepts to become owner. Don't stack duplicates.
	for _, n := range a.Store.PendingNotifications(p.Short, model.NotifyTransfer) {
		if strings.EqualFold(n.Recipient, target) {
			httpx.WriteError(w, http.StatusConflict, "@"+target+" already has a pending transfer invite")
			return
		}
	}
	if _, err := a.Store.AddNotification(model.Notification{
		Recipient:    target,
		Kind:         model.NotifyTransfer,
		PackageShort: p.Short,
		PackageName:  p.Name,
		FromLogin:    u.Login,
	}); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	m := a.decorateForViewer(p, u)
	m["transferInvited"] = target
	httpx.WriteJSON(w, http.StatusOK, m)
}

// handleUnpublishVersion removes a single version. If it was the last version,
// the whole package is removed. (owner only)
func (a *App) handleUnpublishVersion(w http.ResponseWriter, r *http.Request) {
	unlockPackage := a.lockPackageWrite(r.PathValue("name"))
	defer unlockPackage()
	p, ok := a.ownedPackage(w, r)
	if !ok {
		return
	}
	target := r.PathValue("version")
	kept := make([]model.Version, 0, len(p.Versions))
	found := false
	for _, v := range p.Versions {
		if v.Version == target {
			found = true
			continue
		}
		kept = append(kept, v)
	}
	if !found {
		httpx.WriteError(w, http.StatusNotFound, "version not found")
		return
	}
	if len(kept) == 0 {
		if err := a.Store.DeletePackage(p.Short); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "store error")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "unpublished": p.Short})
		return
	}
	// Re-point "latest" at the greatest remaining semantic version.
	markLatestVersion(kept)
	p.Versions = kept
	latest := p.LatestVersion()
	p.Dependencies = providerDependencies(latest.Providers)
	p.UnpackedKB = latest.UnpackedKB
	p.UpdatedAt = latest.PublishedAt
	if err := a.Store.UpsertPackage(p); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.decorateForViewer(p, a.Sessions.CurrentUser(r)))
}

// deprecateRequest is the body of POST /api/packages/{name}/deprecate.
type deprecateRequest struct {
	Message string `json:"message"`
	Version string `json:"version"`
	Undo    bool   `json:"undo"`
}

// handleDeprecate marks a package (or a specific version) deprecated, or undoes
// it. (owner only)
func (a *App) handleDeprecate(w http.ResponseWriter, r *http.Request) {
	unlockPackage := a.lockPackageWrite(r.PathValue("name"))
	defer unlockPackage()
	p, ok := a.ownedPackage(w, r)
	if !ok {
		return
	}
	var req deprecateRequest
	if err := decodeJSON(w, r, &req, 1<<16); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if v := strings.TrimSpace(req.Version); v != "" {
		found := false
		for i := range p.Versions {
			if p.Versions[i].Version == v {
				p.Versions[i].Deprecated = !req.Undo
				found = true
				break
			}
		}
		if !found {
			httpx.WriteError(w, http.StatusNotFound, "version not found")
			return
		}
	} else if req.Undo {
		p.DeprecatedMessage = ""
	} else {
		msg := strings.TrimSpace(req.Message)
		if msg == "" {
			msg = "This package is deprecated."
		}
		p.DeprecatedMessage = msg
	}

	if err := a.Store.UpsertPackage(p); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.decorateForViewer(p, a.Sessions.CurrentUser(r)))
}
