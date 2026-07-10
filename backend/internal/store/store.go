// Package store defines the persistence contract for the registry and a JSON-file
// implementation of it. It depends only on the model package.
package store

import "github.com/wago-org/registry-backend/internal/model"

// InstallPoint is one day's install count in an install-history series.
type InstallPoint struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// Store is the persistence contract the API layer depends on. All methods are
// safe for concurrent use and persist mutations before returning.
type Store interface {
	// Packages.
	ListPackages() []model.Package
	// GetPackage matches by short id first, then by full module name.
	GetPackage(id string) (model.Package, bool)
	UpsertPackage(p model.Package) error
	DeletePackage(short string) error
	PackageCount() int

	// Users.
	GetUser(id string) (model.User, bool)
	GetUserByLogin(login string) (model.User, bool)
	UpsertUser(u model.User) error

	// API tokens (CLI / CI auth). CreateToken returns the one-time plaintext.
	CreateToken(userID, label string) (plaintext string, tok model.APIToken, err error)
	UserByToken(plaintext string) (model.User, bool)
	ListTokens(userID string) []model.APIToken
	RevokeToken(userID, tokenID string) error

	// Stars (keyed by package short id).
	StarCount(short string) int
	StarCounts() map[string]int
	IsStarred(short, userID string) bool
	SetStar(short, userID string, starred bool) (int, error)
	// StarsForUser returns the package shorts a user has starred.
	StarsForUser(userID string) []string

	// Reviews and their votes.
	ReviewsForPackage(short string) []model.Review
	UpsertReview(short, userID string, rating int, body string) (model.Review, error)
	GetReview(id string) (model.Review, bool)
	DeleteReview(id string) error
	SetVote(reviewID, userID, dir string) (up, down int, err error)
	VoteTally(reviewID string) (up, down int)
	MyVote(reviewID, userID string) *string

	// Comments.
	CommentsForPackage(short string) []model.Comment
	AddComment(short, userID, body, parentID string) (model.Comment, error)
	GetComment(id string) (model.Comment, bool)
	UpdateComment(id, body string) (model.Comment, error)
	SetCommentArchived(id string, archived bool) (model.Comment, error)
	DeleteComment(id string) error

	// Reports (moderation queue).
	AddReport(short, reporterID, reporterLogin, reason, detail string) (model.Report, error)
	ListReports() []model.Report
	ResolveReport(id, byLogin string) (model.Report, bool)

	// Installs (keyed by package short id; dates are YYYY-MM-DD).
	RecordInstall(short, date string) error
	InstallSeries(short string, sinceDays int) []InstallPoint
	InstallTotal(short string) int
	InstallWeek(short string) int
}
