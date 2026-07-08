package store

import (
	"path/filepath"
	"testing"

	"github.com/wago-org/registry-backend/internal/model"
)

// TestSetCommentArchived verifies the soft-hide round-trip on both store
// backends: archive → persists, unarchive → persists, unknown id → error.
func TestSetCommentArchived(t *testing.T) {
	json, err := Open(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatalf("open json: %v", err)
	}
	pebble, err := OpenPebble(t.TempDir())
	if err != nil {
		t.Fatalf("open pebble: %v", err)
	}
	defer pebble.Close()

	for _, tc := range []struct {
		name string
		s    Store
	}{{"json", json}, {"pebble", pebble}} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.s
			if err := s.UpsertPackage(model.Package{Short: "redis", Name: "github.com/acme/wago-redis"}); err != nil {
				t.Fatal(err)
			}
			c, err := s.AddComment("redis", "u1", "hello", "")
			if err != nil {
				t.Fatal(err)
			}

			got, err := s.SetCommentArchived(c.ID, true)
			if err != nil {
				t.Fatalf("archive: %v", err)
			}
			if !got.Archived {
				t.Fatal("returned comment not marked archived")
			}
			if reread, ok := s.GetComment(c.ID); !ok || !reread.Archived {
				t.Fatalf("archived flag did not persist: ok=%v archived=%v", ok, reread.Archived)
			}

			if _, err := s.SetCommentArchived(c.ID, false); err != nil {
				t.Fatalf("unarchive: %v", err)
			}
			if reread, ok := s.GetComment(c.ID); !ok || reread.Archived {
				t.Fatal("unarchive did not persist")
			}

			if _, err := s.SetCommentArchived("does-not-exist", true); err == nil {
				t.Fatal("expected error archiving unknown comment")
			}
		})
	}
}
