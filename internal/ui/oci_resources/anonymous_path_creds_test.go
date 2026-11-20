package ociresources

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-629 (F7), security: Docker stores a login under host[:port], and
// docker.GetStoredCreds now finds it for a URL that carries a path
// (http://host:port/repository/docker-hosted). That fallback must never be
// reached for an entry the user marked anonymous: the gate config.UsesCredentials
// comes BEFORE any lookup (credsFor for the tag and catalog flows, the head of
// detectRegistryGroupCmd for the group probe). These tests watch the wire: the
// server records the Authorization header of every request it gets.

// seenRequest is one request the recording server got.
type seenRequest struct{ path, auth string }

// authRecorder serves what a registry serves (a catalog, a tag list, a Nexus
// repository description) for any path and records every request's path and
// Authorization header. It never demands credentials, so a login can only be seen
// by what the client chose to send.
type authRecorder struct {
	*httptest.Server
	mu   sync.Mutex
	seen []seenRequest
}

func newAuthRecorder(t *testing.T) *authRecorder {
	t.Helper()
	r := &authRecorder{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.seen = append(r.seen, seenRequest{req.URL.Path, req.Header.Get("Authorization")})
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(req.URL.Path, "/_catalog"):
			_, _ = w.Write([]byte(`{"repositories":["nginx"]}`))
		case strings.HasSuffix(req.URL.Path, "/tags/list"):
			_, _ = w.Write([]byte(`{"name":"nginx","tags":["v1"]}`))
		default:
			_, _ = w.Write([]byte(`{"type":"hosted","format":"docker"}`))
		}
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *authRecorder) requests() []seenRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]seenRequest(nil), r.seen...)
}

// storeLoginForHost writes the login the way `docker login` keys it: by the
// server's host:port, with no path.
func storeLoginForHost(t *testing.T, srv *httptest.Server, user, pass string) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	writeDockerConfig(t, map[string]any{
		"auths": map[string]any{u.Host: map[string]string{"auth": encodeAuth(user, pass)}},
	})
}

// wantAuthOnEveryRequest checks that the lot reached the server and that every
// request carried exactly want ("" means no Authorization header at all).
func wantAuthOnEveryRequest(t *testing.T, r *authRecorder, want string) {
	t.Helper()
	seen := r.requests()
	if len(seen) == 0 {
		t.Fatal("the server was never asked: the test would pass whatever the code does")
	}
	for _, s := range seen {
		if s.auth != want {
			t.Errorf("GET %s carried Authorization %q, want %q", s.path, s.auth, want)
		}
	}
}

// The browser's two flows, for an entry whose URL names a repository path and
// whose login is stored under the bare host: level 1 (catalog search) and the tag
// search (no searcher, a short name). Anonymous sends nothing; the contrast case,
// same URL and same stored login in credentials mode, sends Basic, so the
// assertion cannot pass by the login never being found.
func TestAnAnonymousPathEntryIsNeverSentTheHostLogin(t *testing.T) {
	const user, pass = "alice", "pass-of-a"
	// The Searcher is read when the browser opens, so each flow installs it first.
	flows := []struct {
		name     string
		searcher func(t *testing.T) imagesearch.Searcher
		wantPath string // the endpoint the flow must have reached
	}{
		{"catalog search", func(t *testing.T) imagesearch.Searcher { return nexusOnlySearcher(t) }, "/v2/_catalog"},
		{"tag search", func(*testing.T) imagesearch.Searcher { return nil }, "/v2/nginx/tags/list"},
	}
	modes := []struct {
		name, mode, want string
	}{
		{"anonymous", config.AuthAnonymous, ""},
		{"credentials", config.AuthCredentials, "Basic " + encodeAuth(user, pass)},
	}
	for _, f := range flows {
		for _, md := range modes {
			t.Run(f.name+"/"+md.name, func(t *testing.T) {
				srv := newAuthRecorder(t)
				storeLoginForHost(t, srv.Server, user, pass)
				reg := config.RegistryItem{
					URL: srv.URL + "/repository/docker-hosted", Alias: "nx-hosted", Slug: "nx-hosted",
					AuthMode: md.mode,
				}

				withImageSearcher(t, f.searcher(t))
				m := feed(t, registryModel(t, reg), testutil.Resize(180, 30))
				m, cmd := submitRaw(t, m, "nginx")
				if f.name == "catalog search" && m.registryBrowser.state != browserStateImages {
					t.Fatalf("state = %d, want the level 1 search", m.registryBrowser.state)
				}
				if f.name == "tag search" && m.registryBrowser.state != browserStateTags {
					t.Fatalf("state = %d, want the tag lot", m.registryBrowser.state)
				}
				testutil.Msgs(cmd)

				if !slices.ContainsFunc(srv.requests(), func(r seenRequest) bool { return strings.HasSuffix(r.path, f.wantPath) }) {
					t.Fatalf("the %s never reached %s (saw %v)", f.name, f.wantPath, srv.requests())
				}
				wantAuthOnEveryRequest(t, srv, md.want)
			})
		}
	}
}

// The same for the group probe (discoveryCreds sits behind the same gate in
// detectRegistryGroupCmd): an anonymous Nexus group whose URL carries a path is
// probed with no Authorization header, even when a login is stored for its host
// and even when a password was handed in. A group using credentials still gets the
// stored login, which keeps the assertion honest.
func TestAnAnonymousPathGroupIsProbedWithoutAnyLogin(t *testing.T) {
	const user, pass = "alice", "pass-of-a"
	tests := []struct {
		name, mode, password, want string
	}{
		{"anonymous", config.AuthAnonymous, "", ""},
		{"anonymous with a password handed in", config.AuthAnonymous, "typed-pass", ""},
		{"credentials", config.AuthCredentials, "", "Basic " + encodeAuth(user, pass)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newAuthRecorder(t)
			storeLoginForHost(t, srv.Server, user, pass)
			reg := config.RegistryItem{
				Slug: "anon-path-group", Kind: config.KindGroup, Provider: config.ProviderNexus,
				URL: srv.URL + "/repository/docker-group", AuthMode: tt.mode,
			}
			t.Cleanup(func() {
				if c, err := cache.NewRegistryGroupCache(); err == nil {
					_ = c.Delete(reg.Slug)
				}
			})

			run(t, detectRegistryGroupCmd(reg, tt.password))

			wantAuthOnEveryRequest(t, srv, tt.want)
		})
	}
}
