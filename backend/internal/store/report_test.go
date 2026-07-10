package store

import (
	"path/filepath"
	"testing"
)

func TestReportsRoundTrip(t *testing.T) {
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
			r, err := s.AddReport("wasi", "u1", "alice", "malware", "looks bad")
			if err != nil || r.ID == "" || r.Resolved {
				t.Fatalf("AddReport: %+v %v", r, err)
			}
			list := s.ListReports()
			if len(list) != 1 || list[0].Reason != "malware" || list[0].ReporterLogin != "alice" {
				t.Fatalf("ListReports: %+v", list)
			}
			got, ok := s.ResolveReport(r.ID, "jane")
			if !ok || !got.Resolved || got.ResolvedBy != "jane" {
				t.Fatalf("ResolveReport: %+v %v", got, ok)
			}
			if !s.ListReports()[0].Resolved {
				t.Fatal("resolved flag not persisted in list")
			}
			if _, ok := s.ResolveReport("nope", "jane"); ok {
				t.Fatal("resolving a missing report should fail")
			}
		})
	}
}
