package api

import "testing"

// TestTrimForList verifies the browse/search list projection drops the heavy
// detail-only fields while keeping the card + profile fields the frontend needs
// (authors/contributors power the profile pages that read the list).
func TestTrimForList(t *testing.T) {
	m := map[string]any{
		"short":        "wasi",
		"name":         "github.com/wago-org/wasi",
		"description":  "WASI host functions",
		"tags":         []any{"wasi"},
		"stars":        5,
		"ownerLogin":   "wago-org",
		"authors":      []any{map[string]any{"github": "octocat"}},
		"contributors": []any{"alice"},
		"versions":     []any{map[string]any{"version": "1.0.0"}},
		"subpackages":  []any{map[string]any{"id": "p1"}},
		"readme":       "# a long readme blob",
	}
	trimForList(m)

	for _, k := range []string{"versions", "subpackages", "readme"} {
		if _, ok := m[k]; ok {
			t.Errorf("%q should be trimmed from the list payload", k)
		}
	}
	for _, k := range []string{"short", "name", "description", "tags", "stars", "ownerLogin", "authors", "contributors"} {
		if _, ok := m[k]; !ok {
			t.Errorf("%q must be kept in the list payload", k)
		}
	}
}
