// Package model holds the domain types for the wago plugins registry: the stored
// records (users, packages, reviews, comments) and the wago-plugin/v1 manifest
// that a package's release publishes. These types are the single source of truth
// for the JSON shapes the store persists and the API serves.
package model

import (
	"encoding/json"
	"errors"
	"strings"
)

// Stability marks how settled an extension's or package's public surface is. It
// mirrors wago's own wago.Stability (experimental|stable|deprecated).
type Stability string

const (
	Experimental Stability = "experimental"
	Stable       Stability = "stable"
	Deprecated   Stability = "deprecated"
)

// Compatibility describes the environments a package or extension supports. It is
// the wago manifest's compatibility block: semver constraints keyed by engine
// (wago/tinygo/go) plus supported GOOS/GOARCH platforms. This is deliberately
// coarse — it is NOT a per-syscall or per-function compatibility matrix.
type Compatibility struct {
	Engines   map[string]string `json:"engines,omitempty"`
	Platforms []string          `json:"platforms,omitempty"`
}

// Subpackage is one subpackage exposed by a package's wago-plugin.json manifest.
// Import is the Go import path of the subpackage; ID is its stable dotted id.
// (Each subpackage provides a wago Extension, but at the registry/manifest layer
// we call the shipped unit a "subpackage".)
type Subpackage struct {
	Import      string        `json:"import"`
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Description string        `json:"description"`
	Stability   Stability     `json:"stability"`
	Tags        []string      `json:"tags"`
	Compat      Compatibility `json:"compatibility"`
	Readme      string        `json:"readme,omitempty"`
}

// Manifest is a wago-plugin/v1 document: a Go module that ships one or more
// subpackages. It is what a publisher POSTs to /api/publish.
// Manifest is the wago.json a package publishes (schema "wago-plugin/v1"). It is
// self-similar: the top-level module and every subpackage share the same config
// shape (ManifestPkg). Provenance and engines set at the module level are
// inherited by subpackages that omit them.
type Manifest struct {
	ManifestPkg
	Schema string `json:"schema"`
}

// ManifestPkg is one node of a self-similar wago.json — the module itself or a
// subpackage. Identity is the module path; there is no separate id.
type ManifestPkg struct {
	Module       string            `json:"module"`
	Name         string            `json:"name,omitempty"`
	Version      string            `json:"version,omitempty"`
	Description  string            `json:"description,omitempty"`
	Stability    Stability         `json:"stability,omitempty"`
	License      string            `json:"license,omitempty"`
	Homepage     string            `json:"homepage,omitempty"`
	Repository   string            `json:"repository,omitempty"`
	Authors      []string          `json:"authors,omitempty"`
	Keywords     []string          `json:"keywords,omitempty"`
	Engines      map[string]string `json:"engines,omitempty"`
	Platforms    []string          `json:"platforms,omitempty"`
	Dependencies []string          `json:"dependencies,omitempty"` // module paths this package depends on
	Subpackages  []ManifestSub     `json:"subpackages,omitempty"`
}

// ManifestSub is a subpackages[] element. It may be written inline as an object or
// as a "./path/wago.json" string — but the string form must be inlined by the
// publisher before upload (only they have the files), so the string form is
// rejected here with a clear message.
type ManifestSub struct {
	ManifestPkg
}

func (s *ManifestSub) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return errors.New("subpackage path references must be inlined before publishing")
	}
	type alias ManifestSub
	return json.Unmarshal(b, (*alias)(s))
}

// ResolvedSubpackages flattens the manifest's subpackages into stored records,
// applying module-level inheritance (provenance + engines/platforms) and deriving
// each stored id/import from the module path. This is the bridge from the
// self-similar publish format to the flat records the store and API serve.
func (m Manifest) ResolvedSubpackages() []Subpackage {
	out := make([]Subpackage, 0, len(m.Subpackages))
	for _, sub := range m.Subpackages {
		s := sub.ManifestPkg
		engines := s.Engines
		if len(engines) == 0 {
			engines = m.Engines
		}
		platforms := s.Platforms
		if len(platforms) == 0 {
			platforms = m.Platforms
		}
		out = append(out, Subpackage{
			Import:      s.Module,
			ID:          lastSegment(s.Module),
			Name:        s.Name,
			Version:     s.Version,
			Description: s.Description,
			Stability:   s.Stability,
			Tags:        s.Keywords,
			Compat:      Compatibility{Engines: engines, Platforms: platforms},
		})
	}
	return out
}

// lastSegment returns the final path element of a module path (a short, URL-safe
// id): "github.com/wago-org/wasi/p1" → "p1".
func lastSegment(module string) string {
	if i := strings.LastIndex(module, "/"); i >= 0 {
		return module[i+1:]
	}
	return module
}

// Author is a named author with an optional GitHub login.
type Author struct {
	Name   string `json:"name"`
	Github string `json:"github"`
}

// Version is a single published release of a package.
type Version struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	PublishedAt  string `json:"publishedAt"`
	Notes        string `json:"notes"`
	UnpackedKB   int    `json:"unpackedKB"`
	Latest       bool   `json:"latest"`
	InstallShare int    `json:"installShare"`
	Deprecated   bool   `json:"deprecated,omitempty"`
	// Hidden marks a placeholder release (version 0.0.0): re-publishable at any
	// time and deleted the moment a real (>0.0.0) version ships.
	Hidden bool `json:"hidden,omitempty"`
	// Hash is a server-computed content fingerprint (sha256:…) of the release,
	// making each published version tamper-evident and immutable.
	Hash string `json:"hash,omitempty"`
}

// Report is a user's moderation flag on a package, kept in a queue for admins to
// triage. Resolving it marks it done without deleting the record.
type Report struct {
	ID            string `json:"id"`
	PackageShort  string `json:"packageShort"`
	ReporterID    string `json:"reporterId"`
	ReporterLogin string `json:"reporterLogin"`
	Reason        string `json:"reason"`
	Detail        string `json:"detail,omitempty"`
	CreatedAt     string `json:"createdAt"`
	Resolved      bool   `json:"resolved,omitempty"`
	ResolvedBy    string `json:"resolvedBy,omitempty"`
	ResolvedAt    string `json:"resolvedAt,omitempty"`
}

// APIToken is a personal access token used by the CLI / CI to authenticate API
// requests. Only the SHA-256 hash of the token is stored; the plaintext is shown
// once at creation.
type APIToken struct {
	ID         string `json:"id"`
	UserID     string `json:"userId"`
	Hash       string `json:"hash"`
	Label      string `json:"label"`
	CreatedAt  string `json:"createdAt"`
	LastUsedAt string `json:"lastUsedAt"`
}

// Issue is a pass-through repository issue surfaced on a package page.
type Issue struct {
	Num      int      `json:"num"`
	Title    string   `json:"title"`
	State    string   `json:"state"`
	Labels   []string `json:"labels"`
	Comments int      `json:"comments"`
	Age      string   `json:"age"`
	Author   string   `json:"author"`
}

// UserEmail is an email address associated with a user: either the GitHub
// primary (Source "github") or a user-added secondary (Source "added") that must
// be verified with an emailed 6-digit code. Code/CodeExpiry are persisted (so a
// verification survives a restart) but are stripped from every API response by
// api.sanitize, so they never reach the client.
type UserEmail struct {
	Address    string `json:"address"`
	Verified   bool   `json:"verified"`
	Source     string `json:"source"` // "github" | "added"
	Code       string `json:"code,omitempty"`
	CodeExpiry int64  `json:"codeExpiry,omitempty"`
}

// User is a GitHub-authenticated user, keyed by the GitHub numeric id as a
// string. Seed users use ids of the form "seed:<login>". The rich profile fields
// are populated from https://api.github.com/user at sign-in.
type User struct {
	ID        string `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
	Email     string `json:"email"`

	// Rich GitHub profile.
	Bio             string `json:"bio,omitempty"`
	Company         string `json:"company,omitempty"`
	Location        string `json:"location,omitempty"`
	Blog            string `json:"blog,omitempty"`
	TwitterUsername string `json:"twitterUsername,omitempty"`
	HTMLURL         string `json:"htmlUrl,omitempty"`
	GithubCreatedAt string `json:"githubCreatedAt,omitempty"`
	Followers       int    `json:"followers,omitempty"`
	Following       int    `json:"following,omitempty"`
	PublicRepos     int    `json:"publicRepos,omitempty"`
	Hireable        bool   `json:"hireable,omitempty"`

	// CreatedAt is when the user first signed in to wago (RFC3339), used to show
	// how long they've been a member. Distinct from GithubCreatedAt (their GitHub
	// account age).
	CreatedAt string `json:"createdAt,omitempty"`

	// Associated emails: the GitHub primary plus any user-added secondaries.
	Emails []UserEmail `json:"emails,omitempty"`

	// Server-only GitHub OAuth material. Persisted so the registry can star
	// repositories on the user's behalf, but stripped from every API response by
	// api.sanitize — these never reach the client.
	GitHubToken  string `json:"githubToken,omitempty"`
	GitHubScopes string `json:"githubScopes,omitempty"`

	// Admin grants site-wide moderation (derived from config.AdminLogins at
	// sign-in). Exposed to the client so the UI can surface admin affordances.
	Admin bool `json:"admin,omitempty"`
}

// Review is the persisted form of a package review. Author identity is joined in
// from the users map at read time and is not stored here.
type Review struct {
	ID           string `json:"id"`
	PackageShort string `json:"packageShort"`
	UserID       string `json:"userId"`
	Rating       int    `json:"rating"`
	Body         string `json:"body"`
	CreatedAt    string `json:"createdAt"`
}

// Comment is a threaded comment on a package. ParentID is empty for a top-level
// comment, or the id of the comment it replies to.
type Comment struct {
	ID           string `json:"id"`
	PackageShort string `json:"packageShort"`
	UserID       string `json:"userId"`
	Body         string `json:"body"`
	CreatedAt    string `json:"createdAt"`
	ParentID     string `json:"parentId"`
	// Archived soft-hides a comment (author or package owner) without deleting it.
	Archived bool `json:"archived,omitempty"`
}

// Package is the stored registry record for a Go module that ships a wago
// plugin. Derived fields (live star totals, install counts, the convenience
// "version"/"latestVersion") are added by the API layer at response time and are
// not stored here.
type Package struct {
	Name        string    `json:"name"` // module path
	Short       string    `json:"short"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	Tags        []string  `json:"tags"`
	Keywords    []string  `json:"keywords"`
	License     string    `json:"license"`
	Repository  string    `json:"repository"`
	Homepage    string    `json:"homepage"`
	Stability   Stability `json:"stability"`
	Verified    bool      `json:"verified"`
	Official    bool      `json:"official"`
	OwnerLogin  string    `json:"ownerLogin"`
	// AllowedPublishers are extra GitHub logins the owner has granted publish
	// rights, beyond the repo's author/admins (who can always publish). Empty by
	// default: publishing is author-only until the owner configures this.
	AllowedPublishers []string      `json:"allowedPublishers,omitempty"`
	Dependencies      []string      `json:"dependencies,omitempty"` // module paths this package depends on
	Readme            string        `json:"readme,omitempty"`
	DeprecatedMessage string        `json:"deprecatedMessage,omitempty"`
	Compat            Compatibility `json:"compatibility"`
	Capabilities      []string      `json:"capabilities"`
	Authors           []Author      `json:"authors"`
	Contributors      []string      `json:"contributors"`
	Subpackages       []Subpackage  `json:"subpackages"`
	Rating            float64       `json:"rating"`
	RatingCount       int           `json:"ratingCount"`
	Score             int           `json:"score"`
	InstallBaseWeek   int           `json:"installBaseWeek"`
	Stars             int           `json:"stars"` // seed baseline; registry stars accrue on top
	Forks             int           `json:"forks"`
	UnpackedKB        int           `json:"unpackedKB"`
	Versions          []Version     `json:"versions"`
	Issues            []Issue       `json:"issues"`
	CreatedAt         string        `json:"createdAt"`
	UpdatedAt         string        `json:"updatedAt"`
}

// LatestVersion returns the version marked latest, falling back to the first
// listed version, or the zero Version when there are none.
func (p Package) LatestVersion() Version {
	for _, v := range p.Versions {
		if v.Latest {
			return v
		}
	}
	if len(p.Versions) > 0 {
		return p.Versions[0]
	}
	return Version{}
}
