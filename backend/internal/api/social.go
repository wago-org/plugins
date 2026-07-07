package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/wago-org/registry-backend/internal/httpx"
	"github.com/wago-org/registry-backend/internal/model"
)

// --- Stars ---

func (a *App) handleStar(w http.ResponseWriter, r *http.Request)   { a.setStar(w, r, true) }
func (a *App) handleUnstar(w http.ResponseWriter, r *http.Request) { a.setStar(w, r, false) }

func (a *App) setStar(w http.ResponseWriter, r *http.Request, starred bool) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	p, ok := a.Store.GetPackage(r.PathValue("name"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "package not found")
		return
	}
	count, err := a.Store.SetStar(p.Short, u.ID, starred)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"stars": p.Stars + count, "starred": starred})
}

// handleGitHubStar / handleGitHubUnstar star or unstar the package's repository
// on GitHub on the user's behalf, using their stored OAuth token. Requires the
// public_repo scope — without it, responds 403 so the client can offer to
// re-authorize. Also mirrors the star into the local "Your stars" list.
func (a *App) handleGitHubStar(w http.ResponseWriter, r *http.Request) { a.setGitHubStar(w, r, true) }
func (a *App) handleGitHubUnstar(w http.ResponseWriter, r *http.Request) {
	a.setGitHubStar(w, r, false)
}

func (a *App) setGitHubStar(w http.ResponseWriter, r *http.Request, on bool) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	p, ok := a.Store.GetPackage(r.PathValue("name"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "package not found")
		return
	}
	owner, repo, ok := parseGitHubRepo(p.Repository)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "package has no GitHub repository")
		return
	}
	if u.GitHubToken == "" || !hasStarScope(u.GitHubScopes) {
		httpx.WriteError(w, http.StatusForbidden, "need_permission")
		return
	}
	if err := a.GitHub.SetStar(u.GitHubToken, owner, repo, on); err != nil {
		// The token may have been revoked or lacks scope — treat as needing
		// re-authorization rather than a server error.
		httpx.WriteError(w, http.StatusForbidden, "need_permission")
		return
	}
	// Keep the local "Your stars" list in sync.
	if _, err := a.Store.SetStar(p.Short, u.ID, on); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "starred": on})
}

// parseGitHubRepo extracts owner/repo from a GitHub repository URL.
func parseGitHubRepo(repository string) (owner, repo string, ok bool) {
	i := strings.Index(repository, "github.com/")
	if i < 0 {
		return "", "", false
	}
	rest := strings.Trim(repository[i+len("github.com/"):], "/")
	rest = strings.TrimSuffix(rest, ".git")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), true
}

// handleMyStars returns the package shorts the current user has starred.
func (a *App) handleMyStars(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	shorts := a.Store.StarsForUser(u.ID)
	if shorts == nil {
		shorts = []string{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"stars": shorts})
}

// --- Reviews ---

// reviewView is the client-facing review with joined author and vote fields.
type reviewView struct {
	ID           string  `json:"id"`
	PackageShort string  `json:"packageShort"`
	UserID       string  `json:"userId"`
	Author       string  `json:"author"`
	AvatarURL    string  `json:"avatarUrl"`
	Rating       int     `json:"rating"`
	Body         string  `json:"body"`
	CreatedAt    string  `json:"createdAt"`
	Score        int     `json:"score"`
	Upvotes      int     `json:"upvotes"`
	Downvotes    int     `json:"downvotes"`
	MyVote       *string `json:"myVote"`
	Mine         bool    `json:"mine"`
}

// buildReviewView joins author identity and vote tallies onto a review.
func (a *App) buildReviewView(rv model.Review, viewerID string) reviewView {
	up, down := a.Store.VoteTally(rv.ID)
	author, _ := a.Store.GetUser(rv.UserID)
	return reviewView{
		ID:           rv.ID,
		PackageShort: rv.PackageShort,
		UserID:       rv.UserID,
		Author:       firstNonEmpty(author.Name, author.Login),
		AvatarURL:    author.AvatarURL,
		Rating:       rv.Rating,
		Body:         rv.Body,
		CreatedAt:    rv.CreatedAt,
		Score:        up - down,
		Upvotes:      up,
		Downvotes:    down,
		MyVote:       a.Store.MyVote(rv.ID, viewerID),
		Mine:         viewerID != "" && rv.UserID == viewerID,
	}
}

func (a *App) handleListReviews(w http.ResponseWriter, r *http.Request) {
	p, ok := a.Store.GetPackage(r.PathValue("name"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "package not found")
		return
	}
	viewer := a.viewerID(r)
	raw := a.Store.ReviewsForPackage(p.Short)
	views := make([]reviewView, 0, len(raw))
	for _, rv := range raw {
		views = append(views, a.buildReviewView(rv, viewer))
	}

	// Sort: helpful = score desc, else recent = newest first.
	if r.URL.Query().Get("sort") == "helpful" {
		sort.SliceStable(views, func(i, j int) bool { return views[i].Score > views[j].Score })
	} else {
		sort.SliceStable(views, func(i, j int) bool { return views[i].CreatedAt > views[j].CreatedAt })
	}

	// Summary over real reviews; fall back to the package's seed rating.
	var summary map[string]any
	if len(views) == 0 {
		summary = map[string]any{"average": p.Rating, "count": p.RatingCount}
	} else {
		total := 0
		for _, v := range views {
			total += v.Rating
		}
		avg := float64(total) / float64(len(views))
		avg = float64(int(avg*10+0.5)) / 10
		summary = map[string]any{"average": avg, "count": len(views)}
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"reviews": views, "summary": summary})
}

func (a *App) handleCreateReview(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	p, ok := a.Store.GetPackage(r.PathValue("name"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "package not found")
		return
	}
	var in struct {
		Rating int    `json:"rating"`
		Body   string `json:"body"`
	}
	if err := decodeJSON(w, r, &in, 1<<16); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Rating < 1 || in.Rating > 5 {
		httpx.WriteError(w, http.StatusBadRequest, "rating must be 1-5")
		return
	}
	if len(in.Body) == 0 || len(in.Body) > 4000 {
		httpx.WriteError(w, http.StatusBadRequest, "body must be 1-4000 chars")
		return
	}
	rev, err := a.Store.UpsertReview(p.Short, u.ID, in.Rating, in.Body)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.buildReviewView(rev, u.ID))
}

func (a *App) handleVote(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	rev, ok := a.Store.GetReview(id)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "review not found")
		return
	}
	if rev.UserID == u.ID {
		httpx.WriteError(w, http.StatusBadRequest, "cannot vote on your own review")
		return
	}
	var in struct {
		Dir *string `json:"dir"`
	}
	if err := decodeJSON(w, r, &in, 1<<12); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	dir := ""
	if in.Dir != nil {
		dir = *in.Dir
	}
	if dir != "" && dir != "up" && dir != "down" {
		httpx.WriteError(w, http.StatusBadRequest, "dir must be up, down or null")
		return
	}
	up, down, err := a.Store.SetVote(id, u.ID, dir)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"score":     up - down,
		"upvotes":   up,
		"downvotes": down,
		"myVote":    a.Store.MyVote(id, u.ID),
	})
}

// --- Comments ---

// commentView is the client-facing comment with joined author and vote fields.
type commentView struct {
	ID           string  `json:"id"`
	PackageShort string  `json:"packageShort"`
	UserID       string  `json:"userId"`
	Author       string  `json:"author"`
	AvatarURL    string  `json:"avatarUrl"`
	Body         string  `json:"body"`
	CreatedAt    string  `json:"createdAt"`
	ParentID     string  `json:"parentId"`
	Score        int     `json:"score"`
	Upvotes      int     `json:"upvotes"`
	Downvotes    int     `json:"downvotes"`
	MyVote       *string `json:"myVote"`
}

// buildCommentView joins author identity and vote tallies onto a comment. Votes
// reuse the same opaque-id vote store as reviews (comment and review ids are both
// random 16-byte hex, so they never collide).
func (a *App) buildCommentView(c model.Comment, viewerID string) commentView {
	author, _ := a.Store.GetUser(c.UserID)
	up, down := a.Store.VoteTally(c.ID)
	return commentView{
		ID:           c.ID,
		PackageShort: c.PackageShort,
		UserID:       c.UserID,
		Author:       firstNonEmpty(author.Name, author.Login),
		AvatarURL:    author.AvatarURL,
		Body:         c.Body,
		CreatedAt:    c.CreatedAt,
		ParentID:     c.ParentID,
		Score:        up - down,
		Upvotes:      up,
		Downvotes:    down,
		MyVote:       a.Store.MyVote(c.ID, viewerID),
	}
}

func (a *App) handleListComments(w http.ResponseWriter, r *http.Request) {
	p, ok := a.Store.GetPackage(r.PathValue("name"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "package not found")
		return
	}
	viewer := a.viewerID(r)
	raw := a.Store.CommentsForPackage(p.Short)
	views := make([]commentView, 0, len(raw))
	for _, c := range raw {
		views = append(views, a.buildCommentView(c, viewer))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"comments": views})
}

// handleVoteComment sets/clears the caller's up/down vote on a comment. Unlike
// reviews, a user may vote on their own comment.
func (a *App) handleVoteComment(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	if _, ok := a.Store.GetComment(id); !ok {
		httpx.WriteError(w, http.StatusNotFound, "comment not found")
		return
	}
	var in struct {
		Dir *string `json:"dir"`
	}
	if err := decodeJSON(w, r, &in, 1<<12); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	dir := ""
	if in.Dir != nil {
		dir = *in.Dir
	}
	if dir != "" && dir != "up" && dir != "down" {
		httpx.WriteError(w, http.StatusBadRequest, "dir must be up, down or null")
		return
	}
	up, down, err := a.Store.SetVote(id, u.ID, dir)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"score":     up - down,
		"upvotes":   up,
		"downvotes": down,
		"myVote":    a.Store.MyVote(id, u.ID),
	})
}

func (a *App) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	p, ok := a.Store.GetPackage(r.PathValue("name"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "package not found")
		return
	}
	var in struct {
		Body     string `json:"body"`
		ParentID string `json:"parentId"`
	}
	if err := decodeJSON(w, r, &in, 1<<16); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(in.Body) == 0 || len(in.Body) > 4000 {
		httpx.WriteError(w, http.StatusBadRequest, "body must be 1-4000 chars")
		return
	}
	// A parent, if given, must be an existing comment on the same package.
	if in.ParentID != "" {
		parent, ok := a.Store.GetComment(in.ParentID)
		if !ok || parent.PackageShort != p.Short {
			httpx.WriteError(w, http.StatusBadRequest, "invalid parent comment")
			return
		}
	}
	c, err := a.Store.AddComment(p.Short, u.ID, in.Body, in.ParentID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.buildCommentView(c, u.ID))
}

// handleUpdateComment edits a comment's body. Only the comment's author may edit.
func (a *App) handleUpdateComment(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	c, ok := a.Store.GetComment(r.PathValue("id"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "comment not found")
		return
	}
	if c.UserID != u.ID {
		httpx.WriteError(w, http.StatusForbidden, "forbidden")
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(w, r, &in, 1<<16); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(in.Body) == 0 || len(in.Body) > 4000 {
		httpx.WriteError(w, http.StatusBadRequest, "body must be 1-4000 chars")
		return
	}
	updated, err := a.Store.UpdateComment(c.ID, in.Body)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.buildCommentView(updated, u.ID))
}

// handleDeleteReview removes the caller's review. Only the review's author may
// delete it.
func (a *App) handleDeleteReview(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	rev, ok := a.Store.GetReview(r.PathValue("id"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "review not found")
		return
	}
	if rev.UserID != u.ID {
		httpx.WriteError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := a.Store.DeleteReview(rev.ID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	c, ok := a.Store.GetComment(r.PathValue("id"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "comment not found")
		return
	}
	// Only the comment's author or the package owner may delete it.
	allowed := c.UserID == u.ID
	if !allowed {
		if p, ok := a.Store.GetPackage(c.PackageShort); ok && p.OwnerLogin == u.Login {
			allowed = true
		}
	}
	if !allowed {
		httpx.WriteError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := a.Store.DeleteComment(c.ID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "store error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
