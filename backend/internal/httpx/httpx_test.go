package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSOrigins(t *testing.T) {
	tests := []struct {
		name       string
		frontend   string
		devMode    bool
		origin     string
		wantOrigin string
	}{
		{"configured production origin", "https://plugins.wago.sh", false, "https://plugins.wago.sh", "https://plugins.wago.sh"},
		{"production rejects private origin", "https://plugins.wago.sh", false, "http://192.168.0.28:8000", ""},
		{"development allows private origin", "http://localhost:8000", true, "http://192.168.0.28:8000", "http://192.168.0.28:8000"},
		{"development rejects wrong port", "http://localhost:8000", true, "http://192.168.0.28:9000", ""},
		{"development rejects public origin", "http://localhost:8000", true, "http://example.com:8000", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
			req := httptest.NewRequest(http.MethodGet, "http://api.local/api/packages", nil)
			req.Header.Set("Origin", tt.origin)
			res := httptest.NewRecorder()

			CORS(tt.frontend, tt.devMode, next).ServeHTTP(res, req)

			if got := res.Header().Get("Access-Control-Allow-Origin"); got != tt.wantOrigin {
				t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, tt.wantOrigin)
			}
		})
	}
}
