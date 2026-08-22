package gitlab

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// The harness is internal/gitlab's, moved with the code it exercises: the SDK
// takes a base URL, so an httptest server stands in for a GitLab instance and
// the backend needs no seam of its own. That property is the reason §3.6 chose
// SDKs over the `gh` / `glab` CLIs, and it is what these tests spend.

// recordedRequest is one call the backend made to the fake API.
type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Body   string
}

func (r recordedRequest) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(r.Body), v); err != nil {
		t.Fatalf("decoding request body %q: %v", r.Body, err)
	}
}

// fakeGitLab is an httptest-backed GitLab API. It records every request so a
// test can assert on what the backend asked for, then delegates to handler.
type fakeGitLab struct {
	mu       sync.Mutex
	requests []recordedRequest
	server   *httptest.Server
}

func newFakeGitLab(t *testing.T, handler http.HandlerFunc) *fakeGitLab {
	t.Helper()
	f := &fakeGitLab{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Body:   string(body),
		})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

// forge returns a backend pointed at the fake API.
func (f *fakeGitLab) forge(t *testing.T) *Forge {
	t.Helper()
	fg, err := New(f.server.URL, "test-token")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return fg
}

func (f *fakeGitLab) calls() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

// paths returns every request path in order, for a test that cares about which
// endpoints were hit rather than about their payloads.
func (f *fakeGitLab) paths() []string {
	calls := f.calls()
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Path
	}
	return out
}

// countPaths counts the requests whose path contains substr.
func (f *fakeGitLab) countPaths(substr string) int {
	n := 0
	for _, p := range f.paths() {
		if strings.Contains(p, substr) {
			n++
		}
	}
	return n
}
