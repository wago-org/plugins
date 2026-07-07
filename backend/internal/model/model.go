// Package model holds the domain types for the wago plugins registry: the stored
// records (users, packages, reviews, comments) and the wago-plugin/v1 manifest
// that a package's release publishes. These types are the single source of truth
// for the JSON shapes the store persists and the API serves.
package model

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
}

// Manifest is a wago-plugin/v1 document: a Go module that ships one or more
// subpackages. It is what a publisher POSTs to /api/publish.
type Manifest struct {
	Schema      string       `json:"schema"`
	Module      string       `json:"module"`
	Subpackages []Subpackage `json:"subpackages"`
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

// User is a GitHub-authenticated user, keyed by the GitHub numeric id as a
// string. Seed users use ids of the form "seed:<login>".
type User struct {
	ID        string `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
	Email     string `json:"email"`
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
}

// Package is the stored registry record for a Go module that ships a wago
// plugin. Derived fields (live star totals, install counts, the convenience
// "version"/"latestVersion") are added by the API layer at response time and are
// not stored here.
type Package struct {
	Name              string        `json:"name"` // module path
	Short             string        `json:"short"`
	Description       string        `json:"description"`
	Category          string        `json:"category"`
	Tags              []string      `json:"tags"`
	Keywords          []string      `json:"keywords"`
	License           string        `json:"license"`
	Repository        string        `json:"repository"`
	Homepage          string        `json:"homepage"`
	Stability         Stability     `json:"stability"`
	Verified          bool          `json:"verified"`
	Official          bool          `json:"official"`
	OwnerLogin        string        `json:"ownerLogin"`
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
