package api

import (
	"strings"
	"sync"
	"time"

	"github.com/wago-org/registry-backend/internal/model"
)

// orgRoleTTL bounds how long a viewer's GitHub org role is trusted before we
// re-check it. Org ownership changes rarely, so caching for a few minutes keeps
// the comments endpoint from hitting GitHub on every page load.
const orgRoleTTL = 10 * time.Minute

type orgRoleEntry struct {
	owner   bool
	expires time.Time
}

// orgRoleCache memoizes "is viewer X an owner of org Y" lookups.
type orgRoleCache struct {
	mu sync.Mutex
	m  map[string]orgRoleEntry
}

// canModeratePackage reports whether viewer may moderate p's discussion (e.g.
// hide comments): true for the package's direct owner login, or — when the
// package is owned by a GitHub organization — for any owner (admin) of that org,
// checked with the viewer's stored GitHub token and cached for orgRoleTTL.
func (a *App) canModeratePackage(viewer *model.User, p model.Package) bool {
	if viewer == nil || p.OwnerLogin == "" {
		return false
	}
	if strings.EqualFold(viewer.Login, p.OwnerLogin) {
		return true
	}
	// Beyond the direct owner, only a GitHub org owner qualifies — and that needs
	// the viewer's token to ask GitHub for their role in the org.
	if viewer.GitHubToken == "" {
		return false
	}
	return a.viewerOwnsOrg(viewer, p.OwnerLogin)
}

// viewerOwnsOrg checks (and caches) whether viewer is an owner/admin of org.
func (a *App) viewerOwnsOrg(viewer *model.User, org string) bool {
	key := strings.ToLower(viewer.Login) + "@" + strings.ToLower(org)
	now := time.Now()

	a.orgRoles.mu.Lock()
	if e, ok := a.orgRoles.m[key]; ok && now.Before(e.expires) {
		a.orgRoles.mu.Unlock()
		return e.owner
	}
	a.orgRoles.mu.Unlock()

	role, err := a.GitHub.OrgRole(viewer.GitHubToken, org)
	owner := err == nil && role == "admin"

	a.orgRoles.mu.Lock()
	if a.orgRoles.m == nil {
		a.orgRoles.m = map[string]orgRoleEntry{}
	}
	a.orgRoles.m[key] = orgRoleEntry{owner: owner, expires: now.Add(orgRoleTTL)}
	a.orgRoles.mu.Unlock()
	return owner
}
