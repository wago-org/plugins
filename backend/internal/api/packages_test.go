package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/wago-org/registry-backend/internal/model"
)

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
		"readme":       "# a long readme blob",
		"subpackages":  []any{map[string]any{"module": "github.com/wago-org/wasi/preview2"}},
	}
	trimForList(m)

	for _, k := range []string{"versions", "readme", "subpackages"} {
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

func TestDecoratedPackageUsesFullCanonicalPublicID(t *testing.T) {
	app := newSessionApp(t)
	pkg := model.Package{
		Name: "github.com/wago-org/wasi", Short: "github.com/wago-org/wasi",
		Versions: []model.Version{{Latest: true, Providers: []model.PublishedProvider{{ID: "github.com/wago-org/wasi/preview2"}}}},
	}
	decorated := app.decoratePackage(pkg, "")
	if decorated["id"] != pkg.Name || decorated["module"] != pkg.Name {
		t.Fatalf("public identity = id %v module %v, want %q", decorated["id"], decorated["module"], pkg.Name)
	}
	if decorated["short"] != pkg.Short {
		t.Fatalf("storage locator = %v, want %q", decorated["short"], pkg.Short)
	}
	providerIDs, ok := decorated["providerIds"].([]string)
	if !ok || len(providerIDs) != 1 || providerIDs[0] != "github.com/wago-org/wasi/preview2" {
		t.Fatalf("provider IDs = %#v", decorated["providerIds"])
	}
}

func TestPackageAPIUsesOnePercentEncodedFullIDParameter(t *testing.T) {
	app := newSessionApp(t)
	id := "github.com/wago-org/wasi"
	if err := app.Store.UpsertPackage(model.Package{Name: id, Short: id}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{
		"/api/packages/" + url.PathEscape(id): http.StatusOK,
		"/api/packages/wago-org%2Fwasi":       http.StatusNotFound,
		"/api/packages/wasi":                  http.StatusNotFound,
	} {
		result := httptest.NewRecorder()
		app.NewRouter().ServeHTTP(result, httptest.NewRequest(http.MethodGet, path, nil))
		if result.Code != want {
			t.Errorf("GET %s = %d, want %d; body = %s", path, result.Code, want, result.Body.String())
		}
	}
}

func TestSortVersionsNewestUsesSemanticOrder(t *testing.T) {
	versions := []model.Version{{Version: "0.9.0"}, {Version: "0.11.0"}, {Version: "0.10.0"}}
	sortVersionsNewest(versions)
	if versions[0].Version != "0.11.0" || versions[1].Version != "0.10.0" || versions[2].Version != "0.9.0" {
		t.Fatalf("semantic order = %+v", versions)
	}
}
