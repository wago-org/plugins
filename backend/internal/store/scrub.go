package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cockroachdb/pebble"
	"github.com/wago-org/registry-backend/internal/model"
)

// ScrubReport summarizes credentials removed from a cloned Pebble store.
type ScrubReport struct {
	Users     int
	APITokens int
}

// WriteSanitizedClone copies a Pebble store into a new database, removing
// secrets that must not leave production while preserving the data used to
// exercise the registry locally. Rewriting into a fresh database ensures old
// LSM values containing credentials are not retained in the clone.
func WriteSanitizedClone(sourceDir, destDir string) (ScrubReport, error) {
	source, err := pebble.Open(sourceDir, &pebble.Options{ReadOnly: true})
	if err != nil {
		return ScrubReport{}, fmt.Errorf("open source store: %w", err)
	}
	defer source.Close()
	dest, err := pebble.Open(destDir, &pebble.Options{})
	if err != nil {
		return ScrubReport{}, fmt.Errorf("open destination store: %w", err)
	}
	defer dest.Close()

	batch := dest.NewBatch()
	defer batch.Close()
	report := ScrubReport{}
	it, err := source.NewIter(nil)
	if err != nil {
		return report, fmt.Errorf("iterate cloned store: %w", err)
	}
	defer it.Close()

	for it.First(); it.Valid(); it.Next() {
		key := append([]byte(nil), it.Key()...)
		switch {
		case strings.HasPrefix(string(key), "u/"):
			var user model.User
			if err := json.Unmarshal(it.Value(), &user); err != nil {
				return report, fmt.Errorf("decode cloned user %q: %w", key, err)
			}
			user.GitHubToken = ""
			user.GitHubScopes = ""
			for i := range user.Emails {
				user.Emails[i].Code = ""
				user.Emails[i].CodeExpiry = 0
			}
			value, err := json.Marshal(user)
			if err != nil {
				return report, fmt.Errorf("encode cloned user %q: %w", key, err)
			}
			if err := batch.Set(key, value, nil); err != nil {
				return report, fmt.Errorf("scrub cloned user %q: %w", key, err)
			}
			report.Users++
		case strings.HasPrefix(string(key), "t/"):
			report.APITokens++
		default:
			value := append([]byte(nil), it.Value()...)
			if err := batch.Set(key, value, nil); err != nil {
				return report, fmt.Errorf("copy cloned record %q: %w", key, err)
			}
		}
	}
	if err := it.Error(); err != nil {
		return report, fmt.Errorf("iterate cloned store: %w", err)
	}
	if err := batch.Commit(pebble.Sync); err != nil {
		return report, fmt.Errorf("commit sanitized clone: %w", err)
	}
	return report, nil
}
