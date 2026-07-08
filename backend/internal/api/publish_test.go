package api

import (
	"testing"

	"github.com/wago-org/registry-backend/internal/model"
)

func v(ver string) model.Version { return model.Version{Version: ver} }

// TestApplyRelease covers the 0.0.0 placeholder rules: hidden, freely
// re-publishable, and deleted the moment a real version ships.
func TestApplyRelease(t *testing.T) {
	// First publish of 0.0.0 → one hidden, latest placeholder.
	vs, conflict := applyRelease(nil, v("0.0.0"))
	if conflict {
		t.Fatal("0.0.0 must never conflict")
	}
	if len(vs) != 1 || !vs[0].Hidden || !vs[0].Latest {
		t.Fatalf("first 0.0.0: want 1 hidden+latest version, got %+v", vs)
	}

	// Re-publish 0.0.0 → still exactly one 0.0.0 (overwritten), still hidden.
	vs, conflict = applyRelease(vs, v("0.0.0"))
	if conflict {
		t.Fatal("re-publishing 0.0.0 must not conflict")
	}
	if len(vs) != 1 || vs[0].Version != "0.0.0" || !vs[0].Hidden {
		t.Fatalf("re-publish 0.0.0: want single hidden 0.0.0, got %+v", vs)
	}

	// Ship a real version → 0.0.0 deleted, 1.0.0 latest and not hidden.
	vs, conflict = applyRelease(vs, v("1.0.0"))
	if conflict {
		t.Fatal("first 1.0.0 must not conflict")
	}
	if len(vs) != 1 || vs[0].Version != "1.0.0" || vs[0].Hidden || !vs[0].Latest {
		t.Fatalf("real release: want single visible+latest 1.0.0 (0.0.0 gone), got %+v", vs)
	}

	// Re-publishing a real version conflicts.
	if _, conflict = applyRelease(vs, v("1.0.0")); !conflict {
		t.Fatal("re-publishing 1.0.0 must conflict")
	}

	// A newer real version appends and takes latest; the prior stays, unhidden.
	vs, conflict = applyRelease(vs, v("1.1.0"))
	if conflict {
		t.Fatal("1.1.0 must not conflict")
	}
	if len(vs) != 2 || !vs[1].Latest || vs[0].Latest {
		t.Fatalf("1.1.0: want [1.0.0(old), 1.1.0(latest)], got %+v", vs)
	}

	// Publishing 0.0.0 again after real releases: added as hidden latest, and it
	// does not disturb the real versions (they remain, unhidden).
	vs, conflict = applyRelease(vs, v("0.0.0"))
	if conflict {
		t.Fatal("0.0.0 after real releases must not conflict")
	}
	var placeholders, reals int
	for _, x := range vs {
		if x.Version == "0.0.0" {
			placeholders++
			if !x.Hidden || !x.Latest {
				t.Fatalf("placeholder should be hidden+latest, got %+v", x)
			}
		} else {
			reals++
		}
	}
	if placeholders != 1 || reals != 2 {
		t.Fatalf("want 1 placeholder + 2 real versions, got %+v", vs)
	}
}
