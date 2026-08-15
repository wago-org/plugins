// Package httpx holds small HTTP helpers shared by the API layer: JSON
// responders and the CORS middleware. It depends on nothing else in the tree.
package httpx

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// WriteJSON writes v as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes an {"error":msg} JSON body with the given status.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// CORS reflects the configured frontend origin, sets credentialed CORS headers,
// and short-circuits preflight OPTIONS requests with 204. Development also
// accepts private-network origins on the configured frontend port so phones and
// tablets can use a backend running on the developer's machine.
func CORS(frontendURL string, devMode bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && allowedOrigin(origin, frontendURL, devMode) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			h.Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowedOrigin(origin, frontendURL string, devMode bool) bool {
	if origin == frontendURL {
		return true
	}
	if !devMode {
		return false
	}

	originURL, err := url.Parse(origin)
	if err != nil || originURL.Scheme != "http" {
		return false
	}
	frontend, err := url.Parse(frontendURL)
	if err != nil || originURL.Port() != frontend.Port() {
		return false
	}

	host := strings.ToLower(originURL.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback())
}
