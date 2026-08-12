package store

import (
	"testing"

	"github.com/wago-org/registry-backend/internal/model"
)

func TestScrubCloneCredentials(t *testing.T) {
	sourceDir := t.TempDir()
	destDir := t.TempDir()
	st, err := OpenPebble(sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	user := model.User{
		ID:           "1",
		Login:        "alice",
		Name:         "Alice",
		GitHubToken:  "github-secret",
		GitHubScopes: "repo",
		Emails: []model.UserEmail{{
			Address: "alice@example.com", Source: "added", Code: "123456", CodeExpiry: 42,
		}},
	}
	if err := st.UpsertUser(user); err != nil {
		t.Fatal(err)
	}
	plaintext, _, err := st.CreateToken(user.ID, "production-token")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	report, err := WriteSanitizedClone(sourceDir, destDir)
	if err != nil {
		t.Fatal(err)
	}
	if report.Users != 1 || report.APITokens != 1 {
		t.Fatalf("report = %+v, want 1 user and 1 API token", report)
	}

	clone, err := OpenPebble(destDir)
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Close()
	got, ok := clone.GetUser(user.ID)
	if !ok {
		t.Fatal("scrubbed user is missing")
	}
	if got.GitHubToken != "" || got.GitHubScopes != "" || got.Emails[0].Code != "" || got.Emails[0].CodeExpiry != 0 {
		t.Fatalf("credentials survived scrub: %+v", got)
	}
	if _, ok := clone.UserByToken(plaintext); ok {
		t.Fatal("production API token survived scrub")
	}
}
