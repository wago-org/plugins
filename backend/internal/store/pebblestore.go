package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/wago-org/registry-backend/internal/model"
)

// PebbleStore is a Store backed by an embedded Pebble (RocksDB-lineage LSM) key
// store for durability, with the full dataset mirrored in memory so reads are
// served lock-cheaply without touching disk. Unlike JSONStore — which rewrote the
// entire document on every mutation — each mutation here persists only the record
// that changed (an O(1) LSM write), so write throughput no longer scales with the
// size of the dataset.
//
// Key layout (one record per key; '/' separates segments, none of which contain
// '/'):
//
//	u/<userID>              -> model.User
//	p/<short>               -> model.Package
//	s/<short>/<userID>      -> "" (star marker; presence == starred)
//	r/<reviewID>            -> model.Review
//	v/<reviewID>/<userID>   -> "up"|"down" (vote direction)
//	c/<commentID>           -> model.Comment
//	i/<short>/<date>        -> decimal install count for that day
//	t/<tokenID>             -> model.APIToken
type PebbleStore struct {
	mu  sync.RWMutex
	db  *pebble.DB
	doc doc
}

var _ Store = (*PebbleStore)(nil)

// key prefixes.
const (
	kpUser    = 'u'
	kpPackage = 'p'
	kpStar    = 's'
	kpReview  = 'r'
	kpVote    = 'v'
	kpComment = 'c'
	kpInstall = 'i'
	kpToken   = 't'
)

// OpenPebble opens (creating if needed) a Pebble store at dir and loads the whole
// dataset into memory.
func OpenPebble(dir string) (*PebbleStore, error) {
	db, err := pebble.Open(dir, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("open pebble: %w", err)
	}
	s := &PebbleStore{db: db, doc: emptyDoc()}
	if err := s.loadAll(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close flushes and closes the underlying database.
func (s *PebbleStore) Close() error { return s.db.Close() }

// loadAll reconstructs the in-memory doc by iterating every key once.
func (s *PebbleStore) loadAll() error {
	it, err := s.db.NewIter(nil)
	if err != nil {
		return err
	}
	defer it.Close()
	for it.First(); it.Valid(); it.Next() {
		k := string(it.Key())
		v := append([]byte(nil), it.Value()...) // value is only valid until Next
		if len(k) < 2 || k[1] != '/' {
			continue
		}
		body := k[2:]
		switch k[0] {
		case kpUser:
			var u model.User
			if json.Unmarshal(v, &u) == nil {
				s.doc.Users[u.ID] = u
			}
		case kpPackage:
			var p model.Package
			if json.Unmarshal(v, &p) == nil {
				s.doc.Packages[p.Short] = p
			}
		case kpReview:
			var r model.Review
			if json.Unmarshal(v, &r) == nil {
				s.doc.Reviews[r.ID] = r
			}
		case kpComment:
			var c model.Comment
			if json.Unmarshal(v, &c) == nil {
				s.doc.Comments[c.ID] = c
			}
		case kpToken:
			var t model.APIToken
			if json.Unmarshal(v, &t) == nil {
				s.doc.Tokens[t.ID] = t
			}
		case kpStar:
			if short, uid, ok := split2(body); ok {
				s.doc.Stars[short] = append(s.doc.Stars[short], uid)
			}
		case kpVote:
			if rid, uid, ok := split2(body); ok {
				if s.doc.Votes[rid] == nil {
					s.doc.Votes[rid] = map[string]string{}
				}
				s.doc.Votes[rid][uid] = string(v)
			}
		case kpInstall:
			if short, date, ok := split2(body); ok {
				n, _ := strconv.Atoi(string(v))
				if s.doc.Installs[short] == nil {
					s.doc.Installs[short] = map[string]int{}
				}
				s.doc.Installs[short][date] = n
			}
		}
	}
	return it.Error()
}

// split2 splits "a/b" into ("a","b"). Segments never contain '/'.
func split2(s string) (string, string, bool) {
	i := strings.IndexByte(s, '/')
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

func recKey(prefix byte, parts ...string) []byte {
	var b strings.Builder
	b.WriteByte(prefix)
	b.WriteByte('/')
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('/')
		}
		b.WriteString(p)
	}
	return []byte(b.String())
}

// putJSON marshals v and writes it under key (durably).
func (s *PebbleStore) putJSON(key []byte, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.db.Set(key, data, pebble.Sync)
}

func (s *PebbleStore) del(key []byte) error { return s.db.Delete(key, pebble.Sync) }

// --- bulk load (seeding) ---

// bulkLoad merges d into memory and persists every record. Used by Seed on an
// empty store, so a single batch commit is enough.
func (s *PebbleStore) bulkLoad(d doc) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.db.NewBatch()
	defer b.Close()
	put := func(key []byte, v any) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return b.Set(key, data, nil)
	}
	for id, u := range d.Users {
		s.doc.Users[id] = u
		if err := put(recKey(kpUser, id), u); err != nil {
			return err
		}
	}
	for short, p := range d.Packages {
		s.doc.Packages[short] = p
		if err := put(recKey(kpPackage, short), p); err != nil {
			return err
		}
	}
	for id, r := range d.Reviews {
		s.doc.Reviews[id] = r
		if err := put(recKey(kpReview, id), r); err != nil {
			return err
		}
	}
	for id, c := range d.Comments {
		s.doc.Comments[id] = c
		if err := put(recKey(kpComment, id), c); err != nil {
			return err
		}
	}
	for id, t := range d.Tokens {
		s.doc.Tokens[id] = t
		if err := put(recKey(kpToken, id), t); err != nil {
			return err
		}
	}
	for short, uids := range d.Stars {
		s.doc.Stars[short] = append([]string(nil), uids...)
		for _, uid := range uids {
			if err := b.Set(recKey(kpStar, short, uid), nil, nil); err != nil {
				return err
			}
		}
	}
	for rid, votes := range d.Votes {
		if s.doc.Votes[rid] == nil {
			s.doc.Votes[rid] = map[string]string{}
		}
		for uid, dir := range votes {
			s.doc.Votes[rid][uid] = dir
			if err := b.Set(recKey(kpVote, rid, uid), []byte(dir), nil); err != nil {
				return err
			}
		}
	}
	for short, days := range d.Installs {
		if s.doc.Installs[short] == nil {
			s.doc.Installs[short] = map[string]int{}
		}
		for date, n := range days {
			s.doc.Installs[short][date] = n
			if err := b.Set(recKey(kpInstall, short, date), []byte(strconv.Itoa(n)), nil); err != nil {
				return err
			}
		}
	}
	return b.Commit(pebble.Sync)
}

// --- helpers shared with JSONStore semantics ---

func hashTokenP(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// --- Packages ---

func (s *PebbleStore) ListPackages() []model.Package {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Package, 0, len(s.doc.Packages))
	for _, p := range s.doc.Packages {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Short < out[j].Short })
	return out
}

func (s *PebbleStore) GetPackage(id string) (model.Package, bool) {
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

func (s *PebbleStore) UpsertPackage(p model.Package) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doc.Packages[p.Short] = p
	return s.putJSON(recKey(kpPackage, p.Short), p)
}

func (s *PebbleStore) DeletePackage(short string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.db.NewBatch()
	defer b.Close()
	delete(s.doc.Packages, short)
	_ = b.Delete(recKey(kpPackage, short), nil)
	for _, uid := range s.doc.Stars[short] {
		_ = b.Delete(recKey(kpStar, short, uid), nil)
	}
	delete(s.doc.Stars, short)
	for date := range s.doc.Installs[short] {
		_ = b.Delete(recKey(kpInstall, short, date), nil)
	}
	delete(s.doc.Installs, short)
	for id, r := range s.doc.Reviews {
		if r.PackageShort == short {
			delete(s.doc.Reviews, id)
			_ = b.Delete(recKey(kpReview, id), nil)
			for uid := range s.doc.Votes[id] {
				_ = b.Delete(recKey(kpVote, id, uid), nil)
			}
			delete(s.doc.Votes, id)
		}
	}
	for id, c := range s.doc.Comments {
		if c.PackageShort == short {
			delete(s.doc.Comments, id)
			_ = b.Delete(recKey(kpComment, id), nil)
		}
	}
	return b.Commit(pebble.Sync)
}

func (s *PebbleStore) PackageCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.doc.Packages)
}

// --- API tokens ---

func (s *PebbleStore) CreateToken(userID, label string) (string, model.APIToken, error) {
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
		Hash:      hashTokenP(plaintext),
		Label:     label,
		CreatedAt: nowRFC3339(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doc.Tokens[tok.ID] = tok
	if err := s.putJSON(recKey(kpToken, tok.ID), tok); err != nil {
		return "", model.APIToken{}, err
	}
	return plaintext, tok, nil
}

func (s *PebbleStore) UserByToken(plaintext string) (model.User, bool) {
	if plaintext == "" {
		return model.User{}, false
	}
	h := hashTokenP(plaintext)
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
			// LastUsedAt is best-effort; NoSync keeps auth off the fsync path.
			if data, err := json.Marshal(t); err == nil {
				_ = s.db.Set(recKey(kpToken, id), data, pebble.NoSync)
			}
			return u, true
		}
	}
	return model.User{}, false
}

func (s *PebbleStore) ListTokens(userID string) []model.APIToken {
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

func (s *PebbleStore) RevokeToken(userID, tokenID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.doc.Tokens[tokenID]; ok && t.UserID == userID {
		delete(s.doc.Tokens, tokenID)
		return s.del(recKey(kpToken, tokenID))
	}
	return nil
}

// --- Users ---

func (s *PebbleStore) GetUser(id string) (model.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.doc.Users[id]
	return u, ok
}

func (s *PebbleStore) GetUserByLogin(login string) (model.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.doc.Users {
		if u.Login == login {
			return u, true
		}
	}
	return model.User{}, false
}

func (s *PebbleStore) UpsertUser(u model.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doc.Users[u.ID] = u
	return s.putJSON(recKey(kpUser, u.ID), u)
}

// --- Stars ---

func (s *PebbleStore) StarCount(short string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.doc.Stars[short])
}

func (s *PebbleStore) StarCounts() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]int, len(s.doc.Stars))
	for k, v := range s.doc.Stars {
		out[k] = len(v)
	}
	return out
}

func (s *PebbleStore) IsStarred(short, userID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, id := range s.doc.Stars[short] {
		if id == userID {
			return true
		}
	}
	return false
}

func (s *PebbleStore) StarsForUser(userID string) []string {
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

func (s *PebbleStore) SetStar(short, userID string, starred bool) (int, error) {
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
	var perr error
	if starred && idx == -1 {
		cur = append(cur, userID)
		perr = s.db.Set(recKey(kpStar, short, userID), nil, pebble.Sync)
	} else if !starred && idx != -1 {
		cur = append(cur[:idx], cur[idx+1:]...)
		perr = s.del(recKey(kpStar, short, userID))
	}
	if len(cur) == 0 {
		delete(s.doc.Stars, short)
	} else {
		s.doc.Stars[short] = cur
	}
	return len(s.doc.Stars[short]), perr
}

// --- Reviews ---

func (s *PebbleStore) ReviewsForPackage(short string) []model.Review {
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

func (s *PebbleStore) UpsertReview(short, userID string, rating int, body string) (model.Review, error) {
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
	return rev, s.putJSON(recKey(kpReview, rev.ID), rev)
}

func (s *PebbleStore) GetReview(id string) (model.Review, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rv, ok := s.doc.Reviews[id]
	return rv, ok
}

// DeleteReview removes a review and any votes cast on it.
func (s *PebbleStore) DeleteReview(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.doc.Reviews, id)
	for uid := range s.doc.Votes[id] {
		_ = s.del(recKey(kpVote, id, uid))
	}
	delete(s.doc.Votes, id)
	return s.del(recKey(kpReview, id))
}

// --- Votes ---

func (s *PebbleStore) tallyLocked(reviewID string) (up, down int) {
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

func (s *PebbleStore) VoteTally(reviewID string) (up, down int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tallyLocked(reviewID)
}

func (s *PebbleStore) SetVote(reviewID, userID, dir string) (up, down int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc.Votes[reviewID] == nil {
		s.doc.Votes[reviewID] = map[string]string{}
	}
	if dir == "" {
		delete(s.doc.Votes[reviewID], userID)
		err = s.del(recKey(kpVote, reviewID, userID))
	} else {
		s.doc.Votes[reviewID][userID] = dir
		err = s.db.Set(recKey(kpVote, reviewID, userID), []byte(dir), pebble.Sync)
	}
	if len(s.doc.Votes[reviewID]) == 0 {
		delete(s.doc.Votes, reviewID)
	}
	up, down = s.tallyLocked(reviewID)
	return up, down, err
}

func (s *PebbleStore) MyVote(reviewID, userID string) *string {
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

func (s *PebbleStore) CommentsForPackage(short string) []model.Comment {
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

func (s *PebbleStore) AddComment(short, userID, body, parentID string) (model.Comment, error) {
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
	return c, s.putJSON(recKey(kpComment, c.ID), c)
}

func (s *PebbleStore) GetComment(id string) (model.Comment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.doc.Comments[id]
	return c, ok
}

// UpdateComment replaces a comment's body.
func (s *PebbleStore) UpdateComment(id, body string) (model.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.doc.Comments[id]
	if !ok {
		return model.Comment{}, errors.New("comment not found")
	}
	c.Body = body
	s.doc.Comments[id] = c
	return c, s.putJSON(recKey(kpComment, id), c)
}

func (s *PebbleStore) DeleteComment(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.doc.Comments, id)
	return s.del(recKey(kpComment, id))
}

// --- Installs ---

func (s *PebbleStore) RecordInstall(short, date string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc.Installs[short] == nil {
		s.doc.Installs[short] = map[string]int{}
	}
	s.doc.Installs[short][date]++
	n := s.doc.Installs[short][date]
	// Install counters are high-volume and tolerant of a tiny loss window on a
	// hard crash, so keep them off the fsync path (NoSync); the WAL still records
	// them and they flush on the next sync.
	return s.db.Set(recKey(kpInstall, short, date), []byte(strconv.Itoa(n)), pebble.NoSync)
}

func (s *PebbleStore) InstallSeries(short string, sinceDays int) []InstallPoint {
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

func (s *PebbleStore) InstallTotal(short string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, n := range s.doc.Installs[short] {
		total += n
	}
	return total
}

func (s *PebbleStore) InstallWeek(short string) int {
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
