package api

import (
	"net/http"
	"strings"

	"github.com/wago-org/registry-backend/internal/httpx"
	"github.com/wago-org/registry-backend/internal/model"
)

// ownedPackage resolves the package named in the URL and checks that the current
// user owns it. On any failure it writes the response and returns ok=false.
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
	if p.OwnerLogin != u.Login {
		httpx.WriteError(w, http.StatusForbidden, "not the package owner")
		return model.Package{}, false
	}
	return p, true
}

// handleUnpublishPackage removes an entire package (owner only).
func (a *App) handleUnpublishPackage(w http.ResponseWriter, r *http.Request) {
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

// handleUnpublishVersion removes a single version. If it was the last version,
// the whole package is removed. (owner only)
func (a *App) handleUnpublishVersion(w http.ResponseWriter, r *http.Request) {
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
	// Re-point "latest" at the newest remaining version (last in the slice).
	for i := range kept {
		kept[i].Latest = false
	}
	kept[len(kept)-1].Latest = true
	p.Versions = kept
	if err := a.Store.UpsertPackage(p); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.decoratePackage(p, ""))
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
	httpx.WriteJSON(w, http.StatusOK, a.decoratePackage(p, ""))
}
