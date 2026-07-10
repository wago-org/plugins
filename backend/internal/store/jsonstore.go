package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/wago-org/registry-backend/internal/model"
)

// doc is the single on-disk JSON document.
type doc struct {
	Users    map[string]model.User        `json:"users"`
	Packages map[string]model.Package     `json:"packages"` // keyed by short
	Stars    map[string][]string          `json:"stars"`    // short -> userIDs
	Reviews  map[string]model.Review      `json:"reviews"`  // reviewID -> review
	Votes    map[string]map[string]string `json:"votes"`    // reviewID -> userID -> "up"|"down"
	Comments map[string]model.Comment     `json:"comments"` // commentID -> comment
	Installs map[string]map[string]int    `json:"installs"` // short -> YYYY-MM-DD -> count
	Reports  map[string]model.Report      `json:"reports"`  // reportID -> report
	Tokens   map[string]model.APIToken    `json:"tokens"`   // tokenID -> token (hash only)
}

// JSONStore is a Store backed by a single JSON file, guarded by an RWMutex and
// persisted atomically (temp file + rename) on every mutation.
type JSONStore struct {
	mu   sync.RWMutex
	path string
	doc  doc
}

// compile-time assertion that JSONStore satisfies Store.
var _ Store = (*JSONStore)(nil)

// Open reads the store from path, creating an empty document if it is missing.
func Open(path string) (*JSONStore, error) {
	s := &JSONStore{path: path, doc: emptyDoc()}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := s.persistLocked(); err != nil {
				return nil, err
			}
			return s, nil
		}
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &s.doc); err != nil {
			return nil, err
		}
	}
	s.normalize()
	return s, nil
}

func emptyDoc() doc {
	return doc{
		Users:    map[string]model.User{},
		Packages: map[string]model.Package{},
		Stars:    map[string][]string{},
		Reviews:  map[string]model.Review{},
		Votes:    map[string]map[string]string{},
		Comments: map[string]model.Comment{},
		Installs: map[string]map[string]int{},
		Reports:  map[string]model.Report{},
		Tokens:   map[string]model.APIToken{},
	}
}

// normalize guards against nil maps after unmarshalling a partial document.
func (s *JSONStore) normalize() {
	if s.doc.Users == nil {
		s.doc.Users = map[string]model.User{}
	}
	if s.doc.Packages == nil {
		s.doc.Packages = map[string]model.Package{}
	}
	if s.doc.Stars == nil {
		s.doc.Stars = map[string][]string{}
	}
	if s.doc.Reviews == nil {
		s.doc.Reviews = map[string]model.Review{}
	}
	if s.doc.Votes == nil {
		s.doc.Votes = map[string]map[string]string{}
	}
	if s.doc.Comments == nil {
		s.doc.Comments = map[string]model.Comment{}
	}
	if s.doc.Installs == nil {
		s.doc.Installs = map[string]map[string]int{}
	}
	if s.doc.Tokens == nil {
		s.doc.Tokens = map[string]model.APIToken{}
	}
	if s.doc.Reports == nil {
		s.doc.Reports = map[string]model.Report{}
	}
}

// persistLocked atomically writes the document. Callers must hold the write lock.
func (s *JSONStore) persistLocked() error {
	data, err := json.MarshalIndent(s.doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// bulkLoad merges a seed document into the store and persists once. Seed guards
// that this only runs on an empty store.
func (s *JSONStore) bulkLoad(d doc) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range d.Users {
		s.doc.Users[k] = v
	}
	for k, v := range d.Packages {
		s.doc.Packages[k] = v
	}
	for k, v := range d.Reviews {
		s.doc.Reviews[k] = v
	}
	for k, v := range d.Comments {
		s.doc.Comments[k] = v
	}
	for k, v := range d.Tokens {
		s.doc.Tokens[k] = v
	}
	for k, v := range d.Stars {
		s.doc.Stars[k] = append([]string(nil), v...)
	}
	for k, v := range d.Votes {
		m := make(map[string]string, len(v))
		for a, b := range v {
			m[a] = b
		}
		s.doc.Votes[k] = m
	}
	for k, v := range d.Installs {
		m := make(map[string]int, len(v))
		for a, b := range v {
			m[a] = b
		}
		s.doc.Installs[k] = m
	}
	return s.persistLocked()
}

// newID returns a random 16-byte hex id.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// nowRFC3339 is the current time formatted for stored timestamps.
func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// --- Packages ---

// ListPackages returns all packages, ordered by short id for stable output.
func (s *JSONStore) ListPackages() []model.Package {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Package, 0, len(s.doc.Packages))
	for _, p := range s.doc.Packages {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Short < out[j].Short })
	return out
}

// GetPackage matches by short id first, then by full module name.
func (s *JSONStore) GetPackage(id string) (model.Package, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.doc.Packages[id]; ok {
		return p, true
	}
	for _, p := range s.doc.Packages {
		if p.Name == id {
			return p, true
		}
	}
	return model.Package{}, false
}

// UpsertPackage inserts or replaces a package keyed by its short id.
func (s *JSONStore) UpsertPackage(p model.Package) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doc.Packages[p.Short] = p
	return s.persistLocked()
}

// DeletePackage removes a package and its associated stars, reviews, votes,
// comments and install history. Returns nil even if the package was absent.
func (s *JSONStore) DeletePackage(short string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.doc.Packages, short)
	delete(s.doc.Stars, short)
	delete(s.doc.Installs, short)
	for id, r := range s.doc.Reviews {
		if r.PackageShort == short {
			delete(s.doc.Reviews, id)
			delete(s.doc.Votes, id)
		}
	}
	for id, c := range s.doc.Comments {
		if c.PackageShort == short {
			delete(s.doc.Comments, id)
		}
	}
	for id, r := range s.doc.Reports {
		if r.PackageShort == short {
			delete(s.doc.Reports, id)
		}
	}
	return s.persistLocked()
}

// PackageCount returns the number of stored packages.
func (s *JSONStore) PackageCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.doc.Packages)
}

// --- API tokens ---

// hashToken returns the hex SHA-256 of a plaintext token.
func hashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// CreateToken mints a token for a user, storing only its hash and returning the
// one-time plaintext (prefixed "wgo_").
func (s *JSONStore) CreateToken(userID, label string) (string, model.APIToken, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", model.APIToken{}, err
	}
	plaintext := "wgo_" + hex.EncodeToString(b)
	if label == "" {
		label = "wago-cli"
	}
	tok := model.APIToken{
		ID:        newID(),
		UserID:    userID,
		Hash:      hashToken(plaintext),
		Label:     label,
		CreatedAt: nowRFC3339(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doc.Tokens[tok.ID] = tok
	if err := s.persistLocked(); err != nil {
		return "", model.APIToken{}, err
	}
	return plaintext, tok, nil
}

// UserByToken resolves a plaintext token to its owning user, updating LastUsedAt.
func (s *JSONStore) UserByToken(plaintext string) (model.User, bool) {
	if plaintext == "" {
		return model.User{}, false
	}
	h := hashToken(plaintext)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, t := range s.doc.Tokens {
		if t.Hash == h {
			u, ok := s.doc.Users[t.UserID]
			if !ok {
				return model.User{}, false
			}
			t.LastUsedAt = nowRFC3339()
			s.doc.Tokens[id] = t
			_ = s.persistLocked()
			return u, true
		}
	}
	return model.User{}, false
}

// ListTokens returns a user's tokens (hashes zeroed for safety), newest first.
func (s *JSONStore) ListTokens(userID string) []model.APIToken {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []model.APIToken
	for _, t := range s.doc.Tokens {
		if t.UserID == userID {
			t.Hash = ""
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// RevokeToken deletes a token, but only if it belongs to the given user.
func (s *JSONStore) RevokeToken(userID, tokenID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.doc.Tokens[tokenID]; ok && t.UserID == userID {
		delete(s.doc.Tokens, tokenID)
		return s.persistLocked()
	}
	return nil
}

// --- Users ---

func (s *JSONStore) GetUser(id string) (model.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.doc.Users[id]
	return u, ok
}

func (s *JSONStore) GetUserByLogin(login string) (model.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.doc.Users {
		if u.Login == login {
			return u, true
		}
	}
	return model.User{}, false
}

func (s *JSONStore) UpsertUser(u model.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doc.Users[u.ID] = u
	return s.persistLocked()
}

// --- Stars ---

func (s *JSONStore) StarCount(short string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.doc.Stars[short])
}

func (s *JSONStore) StarCounts() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]int, len(s.doc.Stars))
	for k, v := range s.doc.Stars {
		out[k] = len(v)
	}
	return out
}

func (s *JSONStore) IsStarred(short, userID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, id := range s.doc.Stars[short] {
		if id == userID {
			return true
		}
	}
	return false
}

func (s *JSONStore) StarsForUser(userID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	for short, ids := range s.doc.Stars {
		for _, id := range ids {
			if id == userID {
				out = append(out, short)
				break
			}
		}
	}
	return out
}

func (s *JSONStore) SetStar(short, userID string, starred bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.doc.Stars[short]
	idx := -1
	for i, id := range cur {
		if id == userID {
			idx = i
			break
		}
	}
	if starred && idx == -1 {
		cur = append(cur, userID)
	} else if !starred && idx != -1 {
		cur = append(cur[:idx], cur[idx+1:]...)
	}
	if len(cur) == 0 {
		delete(s.doc.Stars, short)
	} else {
		s.doc.Stars[short] = cur
	}
	return len(s.doc.Stars[short]), s.persistLocked()
}

// --- Reviews ---

func (s *JSONStore) ReviewsForPackage(short string) []model.Review {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []model.Review
	for _, rv := range s.doc.Reviews {
		if rv.PackageShort == short {
			out = append(out, rv)
		}
	}
	return out
}

// UpsertReview creates or replaces the caller's single review for a package.
func (s *JSONStore) UpsertReview(short, userID string, rating int, body string) (model.Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rev := model.Review{PackageShort: short, UserID: userID, Rating: rating, Body: body}
	for id, existing := range s.doc.Reviews {
		if existing.PackageShort == short && existing.UserID == userID {
			rev.ID = id
			rev.CreatedAt = existing.CreatedAt
			break
		}
	}
	if rev.ID == "" {
		rev.ID = newID()
		rev.CreatedAt = nowRFC3339()
	}
	s.doc.Reviews[rev.ID] = rev
	return rev, s.persistLocked()
}

func (s *JSONStore) GetReview(id string) (model.Review, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rv, ok := s.doc.Reviews[id]
	return rv, ok
}

// DeleteReview removes a review and any votes cast on it.
func (s *JSONStore) DeleteReview(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.doc.Reviews, id)
	delete(s.doc.Votes, id)
	return s.persistLocked()
}

// --- Votes ---

func (s *JSONStore) tallyLocked(reviewID string) (up, down int) {
	for _, dir := range s.doc.Votes[reviewID] {
		switch dir {
		case "up":
			up++
		case "down":
			down++
		}
	}
	return up, down
}

func (s *JSONStore) VoteTally(reviewID string) (up, down int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tallyLocked(reviewID)
}

// SetVote sets, clears (dir==""), or replaces a user's vote and returns the new
// tally.
func (s *JSONStore) SetVote(reviewID, userID, dir string) (up, down int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc.Votes[reviewID] == nil {
		s.doc.Votes[reviewID] = map[string]string{}
	}
	if dir == "" {
		delete(s.doc.Votes[reviewID], userID)
	} else {
		s.doc.Votes[reviewID][userID] = dir
	}
	if len(s.doc.Votes[reviewID]) == 0 {
		delete(s.doc.Votes, reviewID)
	}
	up, down = s.tallyLocked(reviewID)
	return up, down, s.persistLocked()
}

// MyVote returns the caller's vote direction on a review, or nil.
func (s *JSONStore) MyVote(reviewID, userID string) *string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if userID == "" {
		return nil
	}
	if dir, ok := s.doc.Votes[reviewID][userID]; ok {
		d := dir
		return &d
	}
	return nil
}

// --- Comments ---

func (s *JSONStore) CommentsForPackage(short string) []model.Comment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []model.Comment
	for _, c := range s.doc.Comments {
		if c.PackageShort == short {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

func (s *JSONStore) AddComment(short, userID, body, parentID string) (model.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := model.Comment{
		ID:           newID(),
		PackageShort: short,
		UserID:       userID,
		Body:         body,
		CreatedAt:    nowRFC3339(),
		ParentID:     parentID,
	}
	s.doc.Comments[c.ID] = c
	return c, s.persistLocked()
}

func (s *JSONStore) GetComment(id string) (model.Comment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.doc.Comments[id]
	return c, ok
}

// UpdateComment replaces a comment's body.
func (s *JSONStore) UpdateComment(id, body string) (model.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.doc.Comments[id]
	if !ok {
		return model.Comment{}, errors.New("comment not found")
	}
	c.Body = body
	s.doc.Comments[id] = c
	return c, s.persistLocked()
}

// SetCommentArchived soft-hides or restores a comment.
func (s *JSONStore) SetCommentArchived(id string, archived bool) (model.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.doc.Comments[id]
	if !ok {
		return model.Comment{}, errors.New("comment not found")
	}
	c.Archived = archived
	s.doc.Comments[id] = c
	return c, s.persistLocked()
}

// --- Reports (moderation queue) ---

// sortedReports returns reports newest-first.
func sortedReports(m map[string]model.Report) []model.Report {
	out := make([]model.Report, 0, len(m))
	for _, r := range m {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

func (s *JSONStore) AddReport(short, reporterID, reporterLogin, reason, detail string) (model.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := model.Report{
		ID: newID(), PackageShort: short, ReporterID: reporterID, ReporterLogin: reporterLogin,
		Reason: reason, Detail: detail, CreatedAt: nowRFC3339(),
	}
	s.doc.Reports[r.ID] = r
	return r, s.persistLocked()
}

func (s *JSONStore) ListReports() []model.Report {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedReports(s.doc.Reports)
}

func (s *JSONStore) ResolveReport(id, byLogin string) (model.Report, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.doc.Reports[id]
	if !ok {
		return model.Report{}, false
	}
	r.Resolved = true
	r.ResolvedBy = byLogin
	r.ResolvedAt = nowRFC3339()
	s.doc.Reports[id] = r
	return r, s.persistLocked() == nil
}

func (s *JSONStore) DeleteComment(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.doc.Comments, id)
	return s.persistLocked()
}

// --- Installs ---

func (s *JSONStore) RecordInstall(short, date string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc.Installs[short] == nil {
		s.doc.Installs[short] = map[string]int{}
	}
	s.doc.Installs[short][date]++
	return s.persistLocked()
}

// InstallSeries returns the last sinceDays daily buckets, oldest first, filling
// missing days with zero.
func (s *JSONStore) InstallSeries(short string, sinceDays int) []InstallPoint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sinceDays <= 0 {
		sinceDays = 90
	}
	if sinceDays > 366 {
		sinceDays = 366 // cap so an untrusted ?days= can't force a huge allocation
	}
	now := time.Now().UTC()
	out := make([]InstallPoint, 0, sinceDays)
	days := s.doc.Installs[short]
	for d := sinceDays - 1; d >= 0; d-- {
		date := now.AddDate(0, 0, -d).Format("2006-01-02")
		out = append(out, InstallPoint{Date: date, Count: days[date]})
	}
	return out
}

func (s *JSONStore) InstallTotal(short string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, n := range s.doc.Installs[short] {
		total += n
	}
	return total
}

// InstallWeek returns the sum of installs over the last 7 days (today inclusive).
func (s *JSONStore) InstallWeek(short string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now().UTC()
	days := s.doc.Installs[short]
	week := 0
	for d := 0; d < 7; d++ {
		date := now.AddDate(0, 0, -d).Format("2006-01-02")
		week += days[date]
	}
	return week
}
