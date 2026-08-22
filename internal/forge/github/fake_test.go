package github

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

// The harness is the GitLab backend's, ported: the SDK takes a base URL, so an
// httptest server stands in for a GitHub instance and this package needs no
// seam of its own. That property is why §3.6 chose SDKs over the CLIs, and it
// is what these tests spend.

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

type fakeGitHub struct {
	mu       sync.Mutex
	requests []recordedRequest
	server   *httptest.Server
}

func newFakeGitHub(t *testing.T, handler http.HandlerFunc) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{}
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
//
// The server URL is not github.com, so New treats it as Enterprise and appends
// /api/v3 — which is exactly the path this fake serves, and exactly the code
// path an Enterprise user takes.
func (f *fakeGitHub) forge(t *testing.T) *Forge {
	t.Helper()
	fg, err := New(f.server.URL, "ghp_test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return fg
}

func (f *fakeGitHub) calls() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

func (f *fakeGitHub) paths() []string {
	calls := f.calls()
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Path
	}
	return out
}

func (f *fakeGitHub) countPaths(substr string) int {
	n := 0
	for _, p := range f.paths() {
		if strings.Contains(p, substr) {
			n++
		}
	}
	return n
}
