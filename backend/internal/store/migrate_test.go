package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/wago-org/registry-backend/internal/model"
)

const (
	legacyPackageKey   = "wago-org/wasi"
	canonicalPackageID = "github.com/wago-org/wasi"
)

func legacyPackageRecord(t *testing.T, storageKey string) json.RawMessage {
	t.Helper()
	record := map[string]any{
		"name":            canonicalPackageID,
		"short":           storageKey,
		"description":     "WASI host functions",
		"license":         "Apache-2.0",
		"repository":      "https://github.com/wago-org/wasi",
		"homepage":        "https://github.com/wago-org/wasi",
		"ownerLogin":      "wago-org",
		"dependencies":    []string{"github.com/example/legacy-dependency"},
		"readme":          "legacy readme",
		"installBaseWeek": 7,
		"stars":           3,
		"subpackages": []map[string]any{{
			"import": "github.com/wago-org/wasi/p1",
			"id":     "p1",
		}},
		"versions": []map[string]any{{
			"version":     "0.0.0",
			"commit":      "8992be17acf63031c74eacc82edf30370291a150",
			"publishedAt": "2026-07-31T01:30:33Z",
			"latest":      true,
			"hidden":      true,
			"hash":        "sha256:d496d75c40357f810bfab97a7094b8017ba052862f80b78b033bbc18de6f053b",
		}},
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func legacyFixtureDoc(t *testing.T) map[string]any {
	t.Helper()
	var p any
	if err := json.Unmarshal(legacyPackageRecord(t, legacyPackageKey), &p); err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"users":    map[string]any{"user-1": model.User{ID: "user-1", Login: "alice"}},
		"packages": map[string]any{legacyPackageKey: p},
		"stars":    map[string]any{legacyPackageKey: []string{"user-1"}},
		"reviews": map[string]any{"review-1": model.Review{
			ID: "review-1", PackageShort: legacyPackageKey, UserID: "user-1", Rating: 5,
		}},
		"votes": map[string]any{},
		"comments": map[string]any{"comment-1": model.Comment{
			ID: "comment-1", PackageShort: legacyPackageKey, UserID: "user-1", Body: "hello",
		}},
		"installs": map[string]any{legacyPackageKey: map[string]int{"2026-08-11": 13}},
		"reports": map[string]any{"report-1": model.Report{
			ID: "report-1", PackageShort: legacyPackageKey, ReporterID: "user-1",
		}},
		"tokens": map[string]any{},
		"notifications": map[string]any{"notification-1": model.Notification{
			ID: "notification-1", Recipient: "bob", Kind: model.NotifyPublishInvite,
			PackageShort: legacyPackageKey, PackageName: canonicalPackageID, Status: model.NotifyPending,
		}},
	}
}

func writeLegacyJSONStore(t *testing.T, path string, fixture map[string]any) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return raw
}

func writeLegacyPebbleStore(t *testing.T, dir string, packages map[string]json.RawMessage) {
	t.Helper()
	db, err := pebble.Open(dir, &pebble.Options{})
	if err != nil {
		t.Fatal(err)
	}
	b := db.NewBatch()
	putJSON := func(key []byte, value any) {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Set(key, raw, nil); err != nil {
			t.Fatal(err)
		}
	}
	for key, raw := range packages {
		if err := b.Set(recKey(kpPackage, key), raw, nil); err != nil {
			t.Fatal(err)
		}
	}
	putJSON(recKey(kpUser, "user-1"), model.User{ID: "user-1", Login: "alice"})
	if err := b.Set(recKey(kpStar, legacyPackageKey, "user-1"), nil, nil); err != nil {
		t.Fatal(err)
	}
	putJSON(recKey(kpReview, "review-1"), model.Review{ID: "review-1", PackageShort: legacyPackageKey, UserID: "user-1", Rating: 5})
	putJSON(recKey(kpComment, "comment-1"), model.Comment{ID: "comment-1", PackageShort: legacyPackageKey, UserID: "user-1", Body: "hello"})
	putJSON(recKey(kpReport, "report-1"), model.Report{ID: "report-1", PackageShort: legacyPackageKey, ReporterID: "user-1"})
	putJSON(recKey(kpNotif, "notification-1"), model.Notification{
		ID: "notification-1", Recipient: "bob", Kind: model.NotifyPublishInvite,
		PackageShort: legacyPackageKey, PackageName: canonicalPackageID, Status: model.NotifyPending,
	})
	if err := b.Set(recKey(kpInstall, legacyPackageKey, "2026-08-11"), []byte("13"), nil); err != nil {
		t.Fatal(err)
	}
	if err := b.Commit(pebble.Sync); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertMigratedStore(t *testing.T, s Store) {
	t.Helper()
	if _, ok := s.GetPackage(legacyPackageKey); ok {
		t.Fatal("legacy package alias remained active")
	}
	p, ok := s.GetPackage(canonicalPackageID)
	if !ok {
		t.Fatal("canonical package is missing")
	}
	if p.Short != canonicalPackageID || p.Name != canonicalPackageID {
		t.Fatalf("package identity = (%q, %q)", p.Short, p.Name)
	}
	if p.Description != "WASI host functions" || p.Readme != "legacy readme" || p.Stars != 3 || p.InstallBaseWeek != 7 {
		t.Fatalf("safe package metadata was not preserved: %#v", p)
	}
	if len(p.Versions) != 0 || len(p.Dependencies) != 0 || len(p.Subpackages) != 0 {
		t.Fatalf("incompatible v0 release metadata remained active: %#v", p)
	}
	if got := s.StarCount(canonicalPackageID); got != 1 {
		t.Fatalf("canonical stars = %d, want 1", got)
	}
	if got := s.StarsForUser("user-1"); !reflect.DeepEqual(got, []string{canonicalPackageID}) {
		t.Fatalf("stars for user = %v", got)
	}
	if got := s.InstallTotal(canonicalPackageID); got != 13 {
		t.Fatalf("canonical installs = %d, want 13", got)
	}
	if reviews := s.ReviewsForPackage(canonicalPackageID); len(reviews) != 1 || reviews[0].PackageShort != canonicalPackageID {
		t.Fatalf("canonical reviews = %#v", reviews)
	}
	if comments := s.CommentsForPackage(canonicalPackageID); len(comments) != 1 || comments[0].PackageShort != canonicalPackageID {
		t.Fatalf("canonical comments = %#v", comments)
	}
	if reports := s.ListReports(); len(reports) != 1 || reports[0].PackageShort != canonicalPackageID {
		t.Fatalf("canonical reports = %#v", reports)
	}
	if notifications := s.PendingNotifications(canonicalPackageID, model.NotifyPublishInvite); len(notifications) != 1 || notifications[0].PackageName != canonicalPackageID {
		t.Fatalf("canonical notifications = %#v", notifications)
	}
}

func assertQuarantine(t *testing.T, d doc) {
	t.Helper()
	if d.StoreSchemaVersion != currentStoreSchemaVersion {
		t.Fatalf("store schema = %d, want %d", d.StoreSchemaVersion, currentStoreSchemaVersion)
	}
	record, ok := d.QuarantinedV0Packages[legacyPackageKey]
	if !ok || record.CanonicalID != canonicalPackageID {
		t.Fatalf("quarantine = %#v", d.QuarantinedV0Packages)
	}
	var raw map[string]any
	if err := json.Unmarshal(record.Record, &raw); err != nil {
		t.Fatal(err)
	}
	versions, _ := raw["versions"].([]any)
	if len(versions) != 1 || versions[0].(map[string]any)["hash"] == nil {
		t.Fatalf("legacy release was not retained exactly enough for recovery: %s", record.Record)
	}
}

func TestJSONStoreMigratesLegacyPackagesAtomicallyAndIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	writeLegacyJSONStore(t, path, legacyFixtureDoc(t))

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	assertMigratedStore(t, s)
	assertQuarantine(t, s.doc)
	if report, ok := s.StartupMigration(); !ok || report.CanonicalPackages != 1 || report.QuarantinedPackages != 1 {
		t.Fatalf("migration report = %#v, %v", report, ok)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	assertMigratedStore(t, s2)
	assertQuarantine(t, s2.doc)
	if report, ok := s2.StartupMigration(); ok {
		t.Fatalf("idempotent reopen reported another migration: %#v", report)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("second open rewrote an already-migrated JSON store")
	}

	p, _ := s2.GetPackage(canonicalPackageID)
	p.Versions = []model.Version{{Version: "1.0.0", SourceChecksum: "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}}
	if err := s2.UpsertPackage(p); err != nil {
		t.Fatal(err)
	}
	s3, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := s3.GetPackage(canonicalPackageID); !ok || len(p.Versions) != 1 {
		t.Fatalf("canonical v1 publication did not persist: %#v, %v", p, ok)
	}
}

func TestPebbleStoreMigratesLegacyPackagesAtomicallyAndIdempotently(t *testing.T) {
	dir := t.TempDir()
	writeLegacyPebbleStore(t, dir, map[string]json.RawMessage{legacyPackageKey: legacyPackageRecord(t, legacyPackageKey)})

	s, err := OpenPebble(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertMigratedStore(t, s)
	assertQuarantine(t, s.doc)
	if report, ok := s.StartupMigration(); !ok || report.CanonicalPackages != 1 || report.QuarantinedPackages != 1 {
		t.Fatalf("migration report = %#v, %v", report, ok)
	}
	first := snapshotPebble(t, s.db)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := OpenPebble(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertMigratedStore(t, s2)
	assertQuarantine(t, s2.doc)
	if report, ok := s2.StartupMigration(); ok {
		t.Fatalf("idempotent reopen reported another migration: %#v", report)
	}
	second := snapshotPebble(t, s2.db)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("second open rewrote an already-migrated Pebble store")
	}
	if _, closer, err := s2.db.Get(recKey(kpPackage, legacyPackageKey)); err == nil {
		closer.Close()
		t.Fatal("legacy Pebble package key remains")
	} else if err != pebble.ErrNotFound {
		t.Fatal(err)
	}
	if _, closer, err := s2.db.Get(packageScopedKey(kpStar, canonicalPackageID, "user-1")); err != nil {
		t.Fatalf("encoded canonical star key: %v", err)
	} else {
		closer.Close()
	}
	if _, closer, err := s2.db.Get(packageScopedKey(kpInstall, canonicalPackageID, "2026-08-11")); err != nil {
		t.Fatalf("encoded canonical install key: %v", err)
	} else {
		closer.Close()
	}

	p, _ := s2.GetPackage(canonicalPackageID)
	p.Versions = []model.Version{{Version: "1.0.0", SourceChecksum: "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}}
	if err := s2.UpsertPackage(p); err != nil {
		t.Fatal(err)
	}
	if err := s2.Close(); err != nil {
		t.Fatal(err)
	}
	s3, err := OpenPebble(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s3.Close()
	if p, ok := s3.GetPackage(canonicalPackageID); !ok || len(p.Versions) != 1 {
		t.Fatalf("canonical v1 publication did not persist: %#v, %v", p, ok)
	}
}

func TestLegacyMigrationRejectsCanonicalCollisionWithoutWrites(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "store.json")
		fixture := legacyFixtureDoc(t)
		var canonical any
		if err := json.Unmarshal(legacyPackageRecord(t, canonicalPackageID), &canonical); err != nil {
			t.Fatal(err)
		}
		fixture["packages"].(map[string]any)[canonicalPackageID] = canonical
		before := writeLegacyJSONStore(t, path, fixture)
		if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "both identify") {
			t.Fatalf("collision error = %v", err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("failed JSON migration modified the store")
		}
	})

	t.Run("pebble", func(t *testing.T) {
		dir := t.TempDir()
		writeLegacyPebbleStore(t, dir, map[string]json.RawMessage{
			legacyPackageKey:   legacyPackageRecord(t, legacyPackageKey),
			canonicalPackageID: legacyPackageRecord(t, canonicalPackageID),
		})
		before := snapshotClosedPebble(t, dir)
		if _, err := OpenPebble(dir); err == nil || !strings.Contains(err.Error(), "both identify") {
			t.Fatalf("collision error = %v", err)
		}
		after := snapshotClosedPebble(t, dir)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("failed Pebble migration modified the store")
		}
	})
}

func TestLegacyMigrationRejectsUnprovenRepository(t *testing.T) {
	var p model.Package
	if err := json.Unmarshal(legacyPackageRecord(t, legacyPackageKey), &p); err != nil {
		t.Fatal(err)
	}
	p.Repository = "https://example.com/wago-org/wasi"
	d := emptyDoc()
	d.StoreSchemaVersion = 0
	d.Packages = map[string]model.Package{legacyPackageKey: p}
	before, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrateLegacyDoc(&d, nil); err == nil || !strings.Contains(err.Error(), "does not prove") {
		t.Fatalf("identity proof error = %v", err)
	}
	after, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed identity proof mutated the document")
	}
}

func TestCanonicalGitHubPackageIDForKnownLegacyKeys(t *testing.T) {
	for _, legacy := range []string{
		"JairusSW/lease",
		"JairusSW/pool",
		"JairusSW/wide",
		"wago-org/wasi",
		"wago-org/workers",
	} {
		canonical := "github.com/" + legacy
		got, err := canonicalGitHubPackageID(legacy, model.Package{
			Short: legacy, Name: canonical, Repository: "https://github.com/" + legacy,
		})
		if err != nil {
			t.Errorf("%s: %v", legacy, err)
			continue
		}
		if got != canonical {
			t.Errorf("%s: canonical = %q, want %q", legacy, got, canonical)
		}
	}
}

func snapshotPebble(t *testing.T, db *pebble.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	it, err := db.NewIter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	for it.First(); it.Valid(); it.Next() {
		out[string(it.Key())] = string(append([]byte(nil), it.Value()...))
	}
	if err := it.Error(); err != nil {
		t.Fatal(err)
	}
	return out
}

func snapshotClosedPebble(t *testing.T, dir string) map[string]string {
	t.Helper()
	db, err := pebble.Open(dir, &pebble.Options{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotPebble(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return snapshot
}
