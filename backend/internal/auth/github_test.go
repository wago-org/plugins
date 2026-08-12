package auth

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type githubRoundTripFunc func(*http.Request) (*http.Response, error)

func (f githubRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestResolveTagCommitUsesAuthenticatedExactTag(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	previousClient := githubHTTP
	githubHTTP = &http.Client{Transport: githubRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.String() != "https://api.github.com/repos/acme/workers/commits/tags%2Fplugins%2Fpool%2Fv1.2.3-rc.1" {
			t.Fatalf("GitHub request = %s %s", request.Method, request.URL.String())
		}
		if request.Header.Get("Authorization") != "Bearer github-token" || request.Header.Get("Accept") != "application/vnd.github.sha" {
			t.Fatalf("GitHub request headers = %v", request.Header)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(commit)),
			Request:    request,
		}, nil
	})}
	t.Cleanup(func() { githubHTTP = previousClient })

	got, err := (&GitHub{}).ResolveTagCommit("github-token", "acme", "workers", "plugins/pool/v1.2.3-rc.1")
	if err != nil || got != commit {
		t.Fatalf("ResolveTagCommit = %q, %v; want %q", got, err, commit)
	}
}

func TestResolveTagCommitRejectsNonCanonicalSHA(t *testing.T) {
	for _, sha := range []string{strings.Repeat("A", 40), strings.Repeat("a", 39), strings.Repeat("g", 40)} {
		t.Run(sha[:1], func(t *testing.T) {
			previousClient := githubHTTP
			githubHTTP = &http.Client{Transport: githubRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(sha)),
					Request:    request,
				}, nil
			})}
			t.Cleanup(func() { githubHTTP = previousClient })

			if commit, err := (&GitHub{}).ResolveTagCommit("github-token", "acme", "workers", "v1.2.3"); err == nil {
				t.Fatalf("ResolveTagCommit accepted non-canonical SHA %q as %q", sha, commit)
			}
		})
	}
}
