package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/wago-org/registry-backend/internal/httpx"
	"github.com/wago-org/registry-backend/internal/model"
)

// releaseHash is a content fingerprint of a published release: a SHA-256 over the
// module path, version, commit, notes, unpacked size and subpackage manifest.
// json.Marshal sorts map keys, so this is deterministic. It makes each version
// tamper-evident — combined with the append-only, republish-rejected version
// list, a real release is effectively immutable once published. (The 0.0.0
// placeholder is the deliberate exception: hidden and freely re-publishable.)
func releaseHash(module string, v model.Version, subs []model.Subpackage) string {
	payload := struct {
		Module      string             `json:"module"`
		Version     string             `json:"version"`
		Commit      string             `json:"commit"`
		Notes       string             `json:"notes"`
		UnpackedKB  int                `json:"unpackedKB"`
		Subpackages []model.Subpackage `json:"subpackages"`
	}{module, v.Version, v.Commit, v.Notes, v.UnpackedKB, subs}
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// publishRequest is the body of POST /api/publish.
type publishRequest struct {
	Manifest   model.Manifest `json:"manifest"`
	Version    string         `json:"version"`
	Commit     string         `json:"commit"`
	Notes      string         `json:"notes"`
	UnpackedKB int            `json:"unpackedKB"`
	Category   string         `json:"category"`
	Tags       []string       `json:"tags"`
}

// shortFromModule derives a package short id from a module path: the last path
// element with a leading "wago-" or "wago_" stripped.
func shortFromModule(module string) string {
	short := module
	if i := strings.LastIndex(short, "/"); i >= 0 {
		short = short[i+1:]
	}
	short = strings.TrimPrefix(short, "wago-")
	short = strings.TrimPrefix(short, "wago_")
	return short
}

// handlePublish creates or updates a package from a manifest and a release.
func (a *App) handlePublish(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req publishRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Manifest.Schema != "wago-plugin/v1" {
		httpx.WriteError(w, http.StatusBadRequest, "manifest.schema must be wago-plugin/v1")
		return
	}
	if strings.TrimSpace(req.Manifest.Module) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "manifest.module is required")
		return
	}
	if strings.TrimSpace(req.Version) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "version is required")
		return
	}

	short := shortFromModule(req.Manifest.Module)
	p, existed := a.Store.GetPackage(short)
	if !existed {
		p = model.Package{Short: short, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	}

	// Ownership: first publisher becomes owner; later publishes require the owner.
	if p.OwnerLogin == "" {
		p.OwnerLogin = u.Login
	} else if p.OwnerLogin != u.Login {
		httpx.WriteError(w, http.StatusForbidden, "not the package owner")
		return
	}

	p.Name = req.Manifest.Module
	p.Subpackages = req.Manifest.Subpackages
	aggregateFromSubpackages(&p, req.Manifest.Subpackages)

	if req.Category != "" {
		p.Category = req.Category
	}
	p.Tags = unionStrings(p.Tags, req.Tags)
	if req.UnpackedKB > 0 {
		p.UnpackedKB = req.UnpackedKB
	}

	// Add the caller as a contributor (deduped).
	p.Contributors = unionStrings(p.Contributors, []string{u.Login})

	nv := model.Version{
		Version:     req.Version,
		Commit:      req.Commit,
		Notes:       req.Notes,
		UnpackedKB:  req.UnpackedKB,
		PublishedAt: time.Now().UTC().Format(time.RFC3339),
	}
	nv.Hash = releaseHash(p.Name, nv, p.Subpackages)

	versions, conflict := applyRelease(p.Versions, nv)
	if conflict {
		httpx.WriteError(w, http.StatusConflict, "version already published")
		return
	}
	p.Versions = versions
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := a.Store.UpsertPackage(p); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.decoratePackage(p, u.ID))
}

// placeholderVersion is the reserved version that is always hidden, freely
// re-publishable, and deleted when any real version ships.
const placeholderVersion = "0.0.0"

// applyRelease returns the version list after publishing nv, and whether the
// publish conflicts with an existing immutable version.
//
// Version 0.0.0 is a hidden placeholder: it never conflicts (re-publish over it
// anytime) and it is dropped whenever anything is published — replaced on a
// placeholder re-publish, deleted the moment a real (>0.0.0) release ships. Every
// other version is append-only and immutable, so re-publishing one conflicts.
func applyRelease(existing []model.Version, nv model.Version) (versions []model.Version, conflict bool) {
	placeholder := nv.Version == placeholderVersion
	if !placeholder {
		for _, v := range existing {
			if v.Version == nv.Version {
				return nil, true
			}
		}
	}
	kept := make([]model.Version, 0, len(existing)+1)
	for _, v := range existing {
		if v.Version == placeholderVersion {
			continue // transient: superseded by this publish
		}
		v.Latest = false
		kept = append(kept, v)
	}
	nv.Latest = true
	nv.Hidden = placeholder
	return append(kept, nv), false
}

// aggregateFromSubpackages rolls up package-level metadata from a manifest's
// subpackages: the union of tags, description/stability from the first non-empty
// subpackage, and the first subpackage's compatibility as the package-level
// compatibility.
func aggregateFromSubpackages(p *model.Package, subs []model.Subpackage) {
	for _, e := range subs {
		p.Tags = unionStrings(p.Tags, e.Tags)
		if p.Description == "" && e.Description != "" {
			p.Description = e.Description
		}
		if p.Stability == "" && e.Stability != "" {
			p.Stability = e.Stability
		}
	}
	if len(subs) > 0 {
		p.Compat = subs[0].Compat
	}
}

// unionStrings appends items from add that are not already in base, preserving
// order.
func unionStrings(base, add []string) []string {
	seen := make(map[string]bool, len(base))
	for _, s := range base {
		seen[s] = true
	}
	for _, s := range add {
		if s != "" && !seen[s] {
			base = append(base, s)
			seen[s] = true
		}
	}
	return base
}
