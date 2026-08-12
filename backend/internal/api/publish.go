package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/wago-org/registry-backend/internal/httpx"
	"github.com/wago-org/registry-backend/internal/model"
)

// releaseFingerprint covers every immutable input a consumer relies on,
// including source checksum and the complete provider definitions. Timestamps
// and popularity metadata are deliberately excluded.
func releaseFingerprint(module string, v model.Version) string {
	providers := append([]model.PublishedProvider(nil), v.Providers...)
	sort.Slice(providers, func(i, j int) bool {
		if providers[i].ID != providers[j].ID {
			return providers[i].ID < providers[j].ID
		}
		if providers[i].ImportPath != providers[j].ImportPath {
			return providers[i].ImportPath < providers[j].ImportPath
		}
		return providers[i].DefinitionDigest < providers[j].DefinitionDigest
	})
	payload := struct {
		Module         string                    `json:"module"`
		Version        string                    `json:"version"`
		SourceChecksum string                    `json:"sourceChecksum"`
		Commit         string                    `json:"commit"`
		Notes          string                    `json:"notes"`
		UnpackedKB     int                       `json:"unpackedKB"`
		Providers      []model.PublishedProvider `json:"providers"`
	}{module, v.Version, v.SourceChecksum, v.Commit, v.Notes, v.UnpackedKB, providers}
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// publishRequest is the body of POST /api/publish.
type publishRequest struct {
	Manifest   model.Manifest           `json:"manifest"`
	Version    string                   `json:"version"`
	Checksum   string                   `json:"checksum"`
	Providers  []model.ProviderManifest `json:"providers"`
	Commit     string                   `json:"commit"`
	Notes      string                   `json:"notes"`
	UnpackedKB int                      `json:"unpackedKB"`
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

func providerDependencies(providers []model.PublishedProvider) []string {
	seen := map[string]struct{}{}
	for _, provider := range providers {
		for _, dependency := range provider.Definition.Requires {
			seen[dependency.ID] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
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

// shortFromModule returns the package's stable registry key. The v1 system uses
// the full canonical source module everywhere; HTTP endpoints percent-encode the
// slashes when this key occupies one path parameter.
func shortFromModule(module string) string {
	const github = "github.com/"
	if !strings.HasPrefix(module, github) || model.ValidatePluginID(module) != nil {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(module, github), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return module
}

func moduleBelongsToRepository(module, repository string) bool {
	owner, repo, ok := parseGitHubRepo(repository)
	if !ok {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(module, "github.com/"), "/")
	return len(parts) >= 2 && strings.EqualFold(parts[0], owner) && strings.EqualFold(parts[1], repo)
}

// moduleReleaseTag maps a canonical Go module version to its repository tag.
// Nested modules prefix tags with their module subdirectory; a v2+ module-path
// suffix is not part of that prefix.
func moduleReleaseTag(module, version string) string {
	subdirectory := moduleReleaseSubdirectory(module, version)
	if subdirectory == "" {
		return version
	}
	return subdirectory + "/" + version
}

// moduleReleaseSubdirectory is the Go command's Origin.Subdir for a module.
// The repository-relative v2+ module-path suffix selects the semantic major,
// but is not part of either the repository subdirectory or its tag prefix.
func moduleReleaseSubdirectory(module, version string) string {
	parts := strings.Split(strings.TrimPrefix(module, "github.com/"), "/")
	if len(parts) <= 2 {
		return ""
	}
	subdirectory := parts[2:]
	major, _, _ := strings.Cut(strings.TrimPrefix(version, "v"), ".")
	if major != "" && major != "0" && major != "1" && subdirectory[len(subdirectory)-1] == "v"+major {
		subdirectory = subdirectory[:len(subdirectory)-1]
	}
	return strings.Join(subdirectory, "/")
}

// handlePublish creates or updates a package from a manifest and a release.
func (a *App) handlePublish(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req publishRequest
	if err := decodeJSONStrict(w, r, &req, 2<<20); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid publish request: "+err.Error())
		return
	}
	if err := req.Manifest.Validate(); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	version, err := canonicalModuleVersion(req.Version)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "version: "+err.Error())
		return
	}
	req.Version = version
	if err := model.ValidateSourceChecksum(req.Checksum); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.UnpackedKB < 0 {
		httpx.WriteError(w, http.StatusBadRequest, "unpackedKB must not be negative")
		return
	}
	if req.UnpackedKB > 1<<30 {
		httpx.WriteError(w, http.StatusBadRequest, "unpackedKB exceeds the registry limit")
		return
	}
	if !fullGitCommit(req.Commit) || len(req.Notes) > 64<<10 {
		httpx.WriteError(w, http.StatusBadRequest, "commit must be a full lowercase Git SHA and notes must not exceed 64 KiB")
		return
	}
	manifest := *req.Manifest.Package
	if err := model.ValidateProviderCatalog(manifest, req.Version, req.Providers); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	short := shortFromModule(manifest.Module)
	if short == "" {
		httpx.WriteError(w, http.StatusBadRequest, "manifest.package.module must be a canonical github.com source module path")
		return
	}
	if !moduleBelongsToRepository(manifest.Module, manifest.Repository) {
		httpx.WriteError(w, http.StatusBadRequest, "manifest.package.repository must be the exact GitHub repository containing manifest.package.module")
		return
	}
	unlockPackage := a.lockPackageWrite(short)
	defer unlockPackage()
	p, existed := a.Store.GetPackage(short)
	if !existed {
		p = model.Package{Short: short, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	}

	// A published package is pinned to its repository; a re-publish can't swap it.
	if existed && p.Repository != "" && !sameRepo(p.Repository, manifest.Repository) {
		httpx.WriteError(w, http.StatusForbidden, fmt.Sprintf("%s is published from %s; change it there, not to %s", short, p.Repository, manifest.Repository))
		return
	}

	// Authorization: publishing is author-only (the repo's owner / org admin) by
	// default; other people publish only if the owner has added them to the
	// package's allowed publishers.
	if err := a.authorizePublish(u, manifest.Repository, p, existed); err != nil {
		httpx.WriteError(w, http.StatusForbidden, err.Error())
		return
	}
	owner, repo, _ := parseGitHubRepo(manifest.Repository)
	releaseTag := moduleReleaseTag(manifest.Module, req.Version)
	resolvedCommit, err := a.GitHub.ResolveTagCommit(u.GitHubToken, owner, repo, releaseTag)
	if err != nil {
		httpx.WriteError(w, http.StatusUnprocessableEntity, fmt.Sprintf("can't resolve release tag %s: %v", releaseTag, err))
		return
	}
	if resolvedCommit != req.Commit {
		httpx.WriteError(w, http.StatusUnprocessableEntity, fmt.Sprintf("submitted commit %s does not match release tag %s commit %s", req.Commit, releaseTag, resolvedCommit))
		return
	}
	if a.sourceVerifier == nil {
		httpx.WriteError(w, http.StatusInternalServerError, "source verification is not configured")
		return
	}
	if err := a.sourceVerifier.Verify(r.Context(), model.PluginSource{
		Module: manifest.Module, Version: req.Version, Checksum: req.Checksum,
	}, resolvedCommit, manifest, req.Providers); err != nil {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "source verification failed: "+err.Error())
		return
	}

	// The first publisher (an authorized author) becomes the package owner.
	if p.OwnerLogin == "" {
		p.OwnerLogin = u.Login
	}

	p.Name = manifest.Module
	if p.Repository == "" {
		p.Repository = manifest.Repository
	}

	// Add the caller as a contributor (deduped).
	p.Contributors = unionStrings(p.Contributors, []string{u.Login})

	providers := make([]model.PublishedProvider, 0, len(req.Providers))
	for _, declared := range req.Providers {
		definition, err := model.CanonicalPluginDefinition(declared.Definition)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid plugin definition: "+err.Error())
			return
		}
		providers = append(providers, model.PublishedProvider{
			ID:         definition.ID,
			ImportPath: declared.ImportPath,
			Source: model.PluginSource{
				Module: manifest.Module, Version: req.Version, Checksum: req.Checksum,
			},
			Definition:       definition,
			DefinitionDigest: declared.DefinitionDigest,
		})
	}
	// Provider IDs are globally unique across source modules. Serialize the
	// catalog snapshot, uniqueness proof, and package write so two concurrent
	// first publishes cannot both claim the same ID from different modules.
	a.catalogWrites.Lock()
	defer a.catalogWrites.Unlock()
	if err := a.validateProviderGraph(providers); err != nil {
		httpx.WriteError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	nv := model.Version{
		Version:        req.Version,
		Commit:         req.Commit,
		Notes:          req.Notes,
		UnpackedKB:     req.UnpackedKB,
		PublishedAt:    time.Now().UTC().Format(time.RFC3339),
		SourceChecksum: req.Checksum,
		Providers:      providers,
	}
	nv.ReleaseFingerprint = releaseFingerprint(manifest.Module, nv)

	versions, conflict := applyRelease(p.Versions, nv)
	if conflict {
		httpx.WriteError(w, http.StatusConflict, "version already published")
		return
	}
	p.Versions = versions
	if packageVersionIsLatest(p, nv.Version) {
		applyPackageManifest(&p, manifest, req.UnpackedKB)
	}
	p.Dependencies = providerDependencies(p.LatestVersion().Providers)
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := a.Store.UpsertPackage(p); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.decoratePackage(p, u.ID))
}

func fullGitCommit(value string) bool {
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

func packageVersionIsLatest(p model.Package, version string) bool {
	return strings.TrimPrefix(p.LatestVersion().Version, "v") == strings.TrimPrefix(version, "v")
}

// applyPackageManifest projects display/source-package metadata from the
// greatest semantic release. Publishing an older release must not roll the
// package page back while the newer immutable provider catalog remains latest.
func applyPackageManifest(p *model.Package, manifest model.PackageManifest, unpackedKB int) {
	p.Name = manifest.Module
	p.DisplayName = manifest.Name
	p.Repository = manifest.Repository
	p.Homepage = manifest.Homepage
	p.License = manifest.License
	p.Description = manifest.Description
	p.Stability = manifest.Stability
	p.Category = manifest.Category
	p.Tags = append([]string(nil), manifest.Tags...)
	p.Keywords = nil
	p.Compat = model.Compatibility{
		Engines: cloneStringMap(manifest.Engines), Platforms: append([]string(nil), manifest.Platforms...),
	}
	p.Subpackages = append([]model.PackageSub(nil), manifest.Subpackages...)
	p.Authors = make([]model.Author, 0, len(manifest.Authors))
	for _, author := range manifest.Authors {
		p.Authors = append(p.Authors, model.Author{
			Name: author.Name, Email: author.Email, URL: author.URL, Github: author.Github,
		})
	}
	p.UnpackedKB = unpackedKB
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

// applyRelease returns the version list after publishing nv, and whether the
// publish conflicts with an existing immutable version.
func applyRelease(existing []model.Version, nv model.Version) (versions []model.Version, conflict bool) {
	for _, v := range existing {
		if strings.TrimPrefix(v.Version, "v") == strings.TrimPrefix(nv.Version, "v") {
			return nil, true
		}
	}
	versions = append(append([]model.Version(nil), existing...), nv)
	markLatestVersion(versions)
	return versions, false
}

// markLatestVersion marks the greatest semantic version. Publishing an older
// release must not silently downgrade the package's default resolution.
func markLatestVersion(versions []model.Version) {
	if len(versions) == 0 {
		return
	}
	best := -1
	for i := range versions {
		versions[i].Latest = false
		if model.ValidateVersion(versions[i].Version) != nil {
			continue
		}
		if best < 0 {
			best = i
			continue
		}
		if comparison, err := model.CompareVersions(versions[i].Version, versions[best].Version); err == nil && comparison >= 0 {
			best = i
		}
	}
	if best < 0 {
		best = len(versions) - 1
	}
	versions[best].Latest = true
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
