package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeForge is an httptest-backed API. Both SDKs take a base URL, so one server
// stands in for either platform — which is the property that made §3.6 choose
// SDKs over the `gh` / `glab` CLIs, and it is why this package needs no seam of
// its own.
type fakeForge struct {
	server *httptest.Server
}

func newFakeForge(t *testing.T, handler http.HandlerFunc) *fakeForge {
	t.Helper()
	f := &fakeForge{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}
