package api

import (
	"testing"

	"github.com/wago-org/registry-backend/internal/config"
)

func TestHasWrite(t *testing.T) {
	for perm, want := range map[string]bool{
		"admin": true, "maintain": true, "write": true,
		"triage": false, "read": false, "none": false, "": false,
	} {
		if hasWrite(perm) != want {
			t.Errorf("hasWrite(%q) = %v, want %v", perm, hasWrite(perm), want)
		}
	}
}

func TestContainsFold(t *testing.T) {
	list := []string{"Alice", "bob"}
	for in, want := range map[string]bool{
		"alice": true, "ALICE": true, "bob": true, "BOB": true, "carol": false, "": false,
	} {
		if containsFold(list, in) != want {
			t.Errorf("containsFold(%v, %q) = %v, want %v", list, in, containsFold(list, in), want)
		}
	}
}

func TestSameRepo(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"https://github.com/wago-org/wasi", "https://github.com/wago-org/wasi.git", true},
		{"https://github.com/Wago-Org/Wasi", "https://github.com/wago-org/wasi", true},
		{"https://github.com/wago-org/wasi", "https://github.com/wago-org/other", false},
		{"https://github.com/a/b", "https://gitlab.com/a/b", false},
		{"https://evil.example/github.com/a/b", "https://github.com/a/b", false},
		{"https://github.com/a/b/issues", "https://github.com/a/b", false},
		{"https://github.com/a/b?tab=readme", "https://github.com/a/b", false},
	}
	for _, c := range cases {
		if sameRepo(c.a, c.b) != c.want {
			t.Errorf("sameRepo(%q, %q) = %v, want %v", c.a, c.b, sameRepo(c.a, c.b), c.want)
		}
	}
}

func TestModuleBelongsToRepository(t *testing.T) {
	for _, tc := range []struct {
		module, repository string
		want               bool
	}{
		{"github.com/acme/plugin", "https://github.com/acme/plugin", true},
		{"github.com/acme/monorepo/plugins/pool", "https://github.com/acme/monorepo", true},
		{"github.com/acme/plugin", "https://github.com/acme/other", false},
		{"github.com/acme/plugin", "https://evil.example/github.com/acme/plugin", false},
	} {
		if got := moduleBelongsToRepository(tc.module, tc.repository); got != tc.want {
			t.Errorf("moduleBelongsToRepository(%q, %q) = %v, want %v", tc.module, tc.repository, got, tc.want)
		}
	}
}

func TestSafeReturn(t *testing.T) {
	a := &App{Cfg: config.Config{FrontendURL: "https://plugins.wago.sh"}}
	cases := map[string]string{
		"https://plugins.wago.sh/JairusSW/wasi": "https://plugins.wago.sh/JairusSW/wasi",
		"/JairusSW/wasi":                        "https://plugins.wago.sh/JairusSW/wasi",
		"https://plugins.wago.sh":               "https://plugins.wago.sh",
		"":                                      "",
		"https://evil.com/x":                    "",                                    // other origin
		"//evil.com":                            "",                                    // protocol-relative
		"/\\evil.com":                           "https://plugins.wago.sh/%5Cevil.com", // backslash → stays on our host
		"https://plugins.wago.sh.evil.com/x":    "",                                    // prefix trickery
	}
	for in, want := range cases {
		if got := a.safeReturn(in); got != want {
			t.Errorf("safeReturn(%q) = %q, want %q", in, got, want)
		}
	}
}
