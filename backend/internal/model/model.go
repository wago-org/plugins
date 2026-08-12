// Package model holds the domain types for the wago plugins registry. These
// types are the single source of truth for the JSON shapes the store persists
// and the API serves.
package model

// Stability marks how settled a plugin's or package's public surface is. It
// mirrors wago's own wago.Stability (experimental|stable|deprecated).
type Stability string

const (
	Experimental Stability = "experimental"
	Stable       Stability = "stable"
	Deprecated   Stability = "deprecated"
)

// Compatibility describes the environments a package or plugin supports. It is
// the wago manifest's compatibility block: semver constraints keyed by engine
// (wago/tinygo/go) plus supported GOOS/GOARCH platforms. This is deliberately
// coarse — it is NOT a per-syscall or per-function compatibility matrix.
type Compatibility struct {
	Engines   map[string]string `json:"engines,omitempty"`
	Platforms []string          `json:"platforms,omitempty"`
}

// Author is a named author with an optional GitHub login.
type Author struct {
	Name   string `json:"name"`
	Email  string `json:"email,omitempty"`
	URL    string `json:"url,omitempty"`
	Github string `json:"github,omitempty"`
}

// Version is a single published release of a package.
type Version struct {
	Version            string              `json:"version"`
	Commit             string              `json:"commit"`
	PublishedAt        string              `json:"publishedAt"`
	Notes              string              `json:"notes"`
	UnpackedKB         int                 `json:"unpackedKB"`
	Latest             bool                `json:"latest"`
	InstallShare       int                 `json:"installShare"`
	Deprecated         bool                `json:"deprecated,omitempty"`
	SourceChecksum     string              `json:"sourceChecksum"`
	Providers          []PublishedProvider `json:"providers"`
	ReleaseFingerprint string              `json:"releaseFingerprint"`
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

// Notification is an actionable item in a user's inbox, addressed by GitHub
// login: an invite to publish a package, or an offer to receive ownership of
// one. It stays "pending" until the recipient accepts or declines.
type Notification struct {
	ID           string `json:"id"`
	Recipient    string `json:"recipient"` // GitHub login (lowercased) this is addressed to
	Kind         string `json:"kind"`      // "publish-invite" | "transfer"
	PackageShort string `json:"packageShort"`
	PackageName  string `json:"packageName"` // display (module path)
	FromLogin    string `json:"fromLogin"`   // who initiated it
	Status       string `json:"status"`      // "pending" | "accepted" | "declined"
	CreatedAt    string `json:"createdAt"`
	ResolvedAt   string `json:"resolvedAt,omitempty"`
}

// Notification kinds and statuses.
const (
	NotifyPublishInvite = "publish-invite"
	NotifyTransfer      = "transfer"

	NotifyPending  = "pending"
	NotifyAccepted = "accepted"
	NotifyDeclined = "declined"
)

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

	// IsOrg marks a pseudo-user that represents a GitHub organization rather than
	// a person. Org identities are keyed "org:<login>" and let a member who
	// administers the org act on the org's behalf (own packages, star, discuss)
	// while reusing every user-keyed store path. Orgs never carry an OAuth token.
	IsOrg bool `json:"isOrg,omitempty"`
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
	DisplayName string    `json:"displayName,omitempty"`
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
	Dependencies      []string      `json:"dependencies,omitempty"` // canonical plugin IDs used by the latest release
	Readme            string        `json:"readme,omitempty"`
	DeprecatedMessage string        `json:"deprecatedMessage,omitempty"`
	Compat            Compatibility `json:"compatibility"`
	Authors           []Author      `json:"authors"`
	Subpackages       []PackageSub  `json:"subpackages,omitempty"` // source-package metadata, never provider discovery
	Contributors      []string      `json:"contributors"`
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
