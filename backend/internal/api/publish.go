package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

// authorizePublish decides whether u may publish the package pointing at
// repository. The default is author-only: the repo's owner / org admins (GitHub
// "admin" permission) can always publish. Anyone else — an org member or a
// collaborator — may publish only if the package owner has added their login to
// AllowedPublishers, and they still have write access to the repo.
func (a *App) authorizePublish(u *model.User, repository string, p model.Package, existed bool) error {
	owner, repo, ok := parseGitHubRepo(repository)
	if !ok {
		return errors.New("manifest.repository must be a GitHub URL, e.g. https://github.com/owner/repo")
	}
	if u.GitHubToken == "" {
		return fmt.Errorf("re-run `wago auth login` so we can verify your access to github.com/%s/%s", owner, repo)
	}
	perm, isOrg, err := a.GitHub.RepoAccess(u.GitHubToken, owner, repo)
	if err != nil {
		return fmt.Errorf("can't verify access to github.com/%s/%s — confirm it exists and that you can see it", owner, repo)
	}
	// The author / org admin always publishes.
	if perm == "admin" {
		return nil
	}
	// A configured publisher who still has push access to the repo.
	if existed && containsFold(p.AllowedPublishers, u.Login) && hasWrite(perm) {
		return nil
	}
	// Rejected — point them at how to get access.
	who := "the repo's author"
	if isOrg {
		who = "an org owner/admin"
	}
	return fmt.Errorf("publishing github.com/%s/%s is limited to its author; ask %s to add you as a publisher in the package settings (your access: %s)", owner, repo, who, perm)
}

// hasWrite reports whether perm grants write (push) access or better.
func hasWrite(perm string) bool {
	switch perm {
	case "admin", "maintain", "write":
		return true
	}
	return false
}

// normalizeDeps trims, dedupes, and drops empties + self-references from a
// manifest's dependency list.
func normalizeDeps(list []string, self string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, d := range list {
		d = strings.TrimSpace(d)
		if d == "" || strings.EqualFold(d, self) {
			continue
		}
		if key := strings.ToLower(d); !seen[key] {
			seen[key] = true
			out = append(out, d)
		}
	}
	return out
}

// containsFold reports whether list contains s, case-insensitively.
func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// sameRepo reports whether two repository URLs point at the same owner/repo.
func sameRepo(a, b string) bool {
	ao, ar, aok := parseGitHubRepo(a)
	bo, br, bok := parseGitHubRepo(b)
	return aok && bok && strings.EqualFold(ao, bo) && strings.EqualFold(ar, br)
}

// shortFromModule derives a package short id from a module path: the last path

// shortFromModule derives a package short id from a module path: the last path
// element with a leading "wago-" or "wago_" stripped.
func shortFromModule(module string) string {
	const github = "github.com/"
	if !strings.HasPrefix(module, github) {
		return ""
	}
	id := strings.TrimPrefix(module, github)
	parts := strings.Split(id, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return id
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
	// wago/v1 is the canonical manifest schema the CLI now emits; wago-plugin/v1
	// remains accepted for manifests published before the rename.
	if req.Manifest.Schema != "wago/v1" && req.Manifest.Schema != "wago-plugin/v1" {
		httpx.WriteError(w, http.StatusBadRequest, `manifest.schema must be "wago/v1"`)
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
	if short == "" {
		httpx.WriteError(w, http.StatusBadRequest, "manifest.module must be a GitHub owner/repository module path")
		return
	}
	p, existed := a.Store.GetPackage(short)
	if !existed {
		p = model.Package{Short: short, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	}

	// A published package is pinned to its repository; a re-publish can't swap it.
	if existed && p.Repository != "" && !sameRepo(p.Repository, req.Manifest.Repository) {
		httpx.WriteError(w, http.StatusForbidden, fmt.Sprintf("%s is published from %s; change it there, not to %s", short, p.Repository, req.Manifest.Repository))
		return
	}

	// Authorization: publishing is author-only (the repo's owner / org admin) by
	// default; other people publish only if the owner has added them to the
	// package's allowed publishers.
	if err := a.authorizePublish(u, req.Manifest.Repository, p, existed); err != nil {
		httpx.WriteError(w, http.StatusForbidden, err.Error())
		return
	}

	// The first publisher (an authorized author) becomes the package owner.
	if p.OwnerLogin == "" {
		p.OwnerLogin = u.Login
	}

	p.Name = req.Manifest.Module
	// Carry the top-level manifest metadata onto the package (subpackage roll-up
	// below only fills what the top level leaves blank).
	if v := req.Manifest.Repository; v != "" {
		p.Repository = v
	}
	if v := req.Manifest.Homepage; v != "" {
		p.Homepage = v
	}
	if v := req.Manifest.License; v != "" {
		p.License = v
	}
	if v := req.Manifest.Description; v != "" {
		p.Description = v
	}
	if v := req.Manifest.Stability; v != "" {
		p.Stability = v
	}
	if len(req.Manifest.Keywords) > 0 {
		p.Keywords = unionStrings(p.Keywords, req.Manifest.Keywords)
	}
	// Dependencies are replaced from the manifest each publish (the manifest is
	// the source of truth), trimmed + deduped, with self-references dropped.
	p.Dependencies = normalizeDeps(req.Manifest.Dependencies, req.Manifest.Module)
	if len(req.Manifest.Authors) > 0 {
		p.Authors = parseAuthors(req.Manifest.Authors)
	} else if len(p.Authors) == 0 {
		// No authors declared: default to the publisher (a verified author of the
		// repo), so the package still shows a real GitHub identity + avatar.
		name := u.Name
		if name == "" {
			name = u.Login
		}
		p.Authors = []model.Author{{Name: name, Github: u.Login}}
	}
	subs := req.Manifest.ResolvedSubpackages()
	p.Subpackages = subs
	aggregateFromSubpackages(&p, subs)

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

// parseAuthors turns manifest author strings into model.Authors. Authors are
// GitHub-identity-first: it recognises a trailing handle in "Name <handle>" or
// "Name (@handle)", a bare "@handle", or a bare login like "octocat" (so
// authors: ["octocat"] gets an avatar). A string with spaces or non-login
// characters is treated as a display name only.
func parseAuthors(list []string) []model.Author {
	out := make([]model.Author, 0, len(list))
	for _, raw := range list {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		a := model.Author{Name: s}
		switch {
		case strings.IndexAny(s, "<(") >= 0:
			i := strings.IndexAny(s, "<(")
			a.Name = strings.TrimSpace(s[:i])
			a.Github = strings.TrimPrefix(strings.Trim(s[i:], "<>()@ "), "@")
		case strings.HasPrefix(s, "@"):
			a.Name = strings.TrimPrefix(s, "@")
			a.Github = a.Name
		case isGitHubLogin(s):
			a.Github = s // bare login → treat as a GitHub handle
		}
		out = append(out, a)
	}
	return out
}

// isGitHubLogin reports whether s is a plausible GitHub login: 1–39 chars of
// alphanumerics and non-leading/trailing hyphens (no spaces).
func isGitHubLogin(s string) bool {
	if s == "" || len(s) > 39 {
		return false
	}
	for i, r := range s {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if alnum {
			continue
		}
		if r == '-' && i > 0 && i < len(s)-1 {
			continue
		}
		return false
	}
	return true
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
