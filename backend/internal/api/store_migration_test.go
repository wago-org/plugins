package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/registry-backend/internal/config"
	"github.com/wago-org/registry-backend/internal/store"
)

func TestMigratedV0ReleaseIsAbsentFromStrictV1Listings(t *testing.T) {
	const (
		legacyID    = "wago-org/wasi"
		canonicalID = "github.com/wago-org/wasi"
	)
	fixture := map[string]any{
		"packages": map[string]any{legacyID: map[string]any{
			"name": canonicalID, "short": legacyID,
			"repository":  "https://github.com/wago-org/wasi",
			"description": "WASI host functions",
			"versions": []map[string]any{{
				"version": "0.0.0", "latest": true, "hidden": true,
				"hash": "sha256:legacy-v0-release",
			}},
		}},
	}
	raw, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "store.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	app := New(config.Config{SessionSecret: []byte("migration-test-secret")}, st)

	result := httptest.NewRecorder()
	app.NewRouter().ServeHTTP(result, httptest.NewRequest(
		http.MethodGet,
		"/api/v1/plugins/candidates?id="+url.QueryEscape(canonicalID),
		nil,
	))
	if result.Code != http.StatusNotFound {
		t.Fatalf("candidate status = %d, want 404; body = %s", result.Code, result.Body.String())
	}

	result = httptest.NewRecorder()
	app.NewRouter().ServeHTTP(result, httptest.NewRequest(
		http.MethodGet,
		"/api/packages/"+url.PathEscape(canonicalID)+"/versions",
		nil,
	))
	if result.Code != http.StatusOK {
		t.Fatalf("versions status = %d, want 200; body = %s", result.Code, result.Body.String())
	}
	var response struct {
		Versions []json.RawMessage `json:"versions"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Versions) != 0 {
		t.Fatalf("migrated v0 release leaked into package versions: %s", result.Body.String())
	}

	// The canonical active record is immediately usable by strict v1
	// publication. Store one validated provider release through the same helper
	// as the resolver contract tests, then prove it becomes the only candidate.
	provider := testProvider(t, canonicalID, canonicalID, "1.0.0", nil)
	storeProviderRelease(t, app, provider, false)
	result = httptest.NewRecorder()
	app.NewRouter().ServeHTTP(result, httptest.NewRequest(
		http.MethodGet,
		"/api/v1/plugins/candidates?id="+url.QueryEscape(canonicalID),
		nil,
	))
	if result.Code != http.StatusOK {
		t.Fatalf("canonical v1 candidate status = %d, want 200; body = %s", result.Code, result.Body.String())
	}
	var candidates struct {
		Plugins []struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &candidates); err != nil {
		t.Fatal(err)
	}
	if len(candidates.Plugins) != 1 || candidates.Plugins[0].ID != canonicalID || candidates.Plugins[0].Version != "1.0.0" {
		t.Fatalf("canonical v1 candidates = %#v", candidates.Plugins)
	}
}
