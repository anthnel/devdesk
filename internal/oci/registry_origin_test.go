package oci

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// These tests pin DEVDESK-410: no credential leaves the origin the caller
// configured, whether through a Link rel="next" or an HTTP redirect, and a
// token response is bounded. The strong assertion is that no Authorization
// header reaches the foreign target.

// hitLog records every request a test server receives.
type hitLog struct {
	mu   sync.Mutex
	hits []string // "host path"
	auth []string // Authorization header of each hit
}

func (l *hitLog) record(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits = append(l.hits, r.Host+" "+r.URL.RequestURI())
	l.auth = append(l.auth, r.Header.Get("Authorization"))
}

func (l *hitLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.hits)
}

func (l *hitLog) anyAuth() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, a := range l.auth {
		if a != "" {
			return a
		}
	}
	return ""
}

// useClientTransport swaps registryHTTPClient for a copy of itself with
// another transport, so any CheckRedirect the production client carries
// stays in force.
func useClientTransport(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	old := registryHTTPClient
	c := *old
	c.Transport = rt
	registryHTTPClient = &c
	t.Cleanup(func() { registryHTTPClient = old })
}

// dialTo sends every connection to addr whatever host name was asked for, so
// a test can use distinct host names backed by local servers.
func dialTo(addr string) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
}

func TestListRegistryTagsNextOrigin(t *testing.T) {
	const tags = `{"tags":["a"]}`

	for _, tt := range []struct {
		name string
		link func(self string) string
	}{
		{"relative next is followed", func(string) string { return "/v2/x/tags/list?last=a" }},
		{"same-origin absolute next is followed", func(self string) string { return self + "/v2/x/tags/list?last=a" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var s *httptest.Server
			s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("last") == "" {
					w.Header().Set("Link", `<`+tt.link(s.URL)+`>; rel="next"`)
					_, _ = fmt.Fprint(w, tags)
					return
				}
				if _, _, ok := r.BasicAuth(); !ok {
					t.Error("same-origin page lost its credentials")
				}
				_, _ = fmt.Fprint(w, `{"tags":["b"]}`)
			}))
			defer s.Close()
			got, err := ListRegistryTagsContext(context.Background(), s.URL, "x", "user", "pass")
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, ",") != "a,b" {
				t.Errorf("tags = %v, want [a b]", got)
			}
		})
	}
}

// serveBoth answers a tags or a catalog request.
func serveBoth(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/_catalog") {
		_, _ = fmt.Fprint(w, catalogBody("evil"))
		return
	}
	_, _ = fmt.Fprint(w, `{"tags":["evil"]}`)
}

func portOfServer(s *httptest.Server) string {
	_, p, _ := net.SplitHostPort(s.Listener.Addr().String())
	return p
}

// redirectCase wires a registry that answers every request with a 302 to a
// foreign target, and returns the registry's API URL and the target's log.
type redirectCase struct {
	name  string
	setup func(t *testing.T) (apiURL string, target *hitLog)
}

// redirectingRegistry starts a registry redirecting to loc(requestURI).
func redirectingRegistry(t *testing.T, tlsServer bool, loc func(uri string) string) *httptest.Server {
	t.Helper()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, loc(r.URL.RequestURI()), http.StatusFound)
	})
	var s *httptest.Server
	if tlsServer {
		s = httptest.NewTLSServer(h)
	} else {
		s = httptest.NewServer(h)
	}
	t.Cleanup(s.Close)
	return s
}

func recordingServer(t *testing.T, log *hitLog) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		serveBoth(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func redirectCases() []redirectCase {
	return []redirectCase{
		{"other port, same host", func(t *testing.T) (string, *hitLog) {
			var target hitLog
			foreign := recordingServer(t, &target)
			reg := redirectingRegistry(t, false, func(uri string) string { return foreign.URL + uri })
			return reg.URL, &target
		}},
		{"https to http, same host", func(t *testing.T) (string, *hitLog) {
			var target hitLog
			foreign := recordingServer(t, &target)
			reg := redirectingRegistry(t, true, func(uri string) string { return foreign.URL + uri })
			// Trust the test certificate but keep the production client's
			// redirect policy.
			useClientTransport(t, reg.Client().Transport)
			return reg.URL, &target
		}},
		{"sub-domain of the registry host", func(t *testing.T) (string, *hitLog) {
			var target hitLog
			var reg *httptest.Server
			reg = redirectingRegistry(t, false, func(uri string) string {
				return "http://sub.registry.test:" + portOfServer(reg) + uri
			})
			// Both names reach the same listener; the target is told apart
			// by the Host it is asked for.
			wrapped := reg.Config.Handler
			reg.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.Host, "sub.") {
					target.record(r)
					serveBoth(w, r)
					return
				}
				wrapped.ServeHTTP(w, r)
			})
			useClientTransport(t, dialTo(reg.Listener.Addr().String()))
			return "http://registry.test:" + portOfServer(reg), &target
		}},
		{"other host", func(t *testing.T) (string, *hitLog) {
			var target hitLog
			var reg *httptest.Server
			reg = redirectingRegistry(t, false, func(uri string) string {
				return "http://other.example:" + portOfServer(reg) + uri
			})
			wrapped := reg.Config.Handler
			reg.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.Host, "other.") {
					target.record(r)
					serveBoth(w, r)
					return
				}
				wrapped.ServeHTTP(w, r)
			})
			useClientTransport(t, dialTo(reg.Listener.Addr().String()))
			return "http://registry.test:" + portOfServer(reg), &target
		}},
	}
}

// The redirect is refused or followed without credentials; either way the
// foreign target never sees an Authorization header.
func TestRegistryRedirectOffOriginSendsNoCredentials(t *testing.T) {
	for _, c := range redirectCases() {
		t.Run("tags/"+c.name, func(t *testing.T) {
			api, target := c.setup(t)
			_, _ = ListRegistryTagsContext(context.Background(), api, "x", "user", "pass")
			if a := target.anyAuth(); a != "" {
				t.Errorf("credentials reached the redirect target: %q", a)
			}
		})
		t.Run("catalog/"+c.name, func(t *testing.T) {
			api, target := c.setup(t)
			_, _, _ = ListCatalogContext(context.Background(), api, "user", "pass", 10, 1000)
			if a := target.anyAuth(); a != "" {
				t.Errorf("credentials reached the redirect target: %q", a)
			}
		})
	}
}

// A redirect that stays on the origin is followed and keeps its credentials.
func TestRegistryRedirectSameOriginKeepsCredentials(t *testing.T) {
	var gotAuth string
	var mu sync.Mutex
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/moved") {
			mu.Lock()
			gotAuth = r.Header.Get("Authorization")
			mu.Unlock()
			serveBoth(w, r)
			return
		}
		http.Redirect(w, r, "/moved"+r.URL.RequestURI(), http.StatusFound)
	}))
	defer s.Close()

	check := func(t *testing.T) {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if !strings.HasPrefix(gotAuth, "Basic ") {
			t.Errorf("Authorization after a same-origin redirect = %q, want Basic", gotAuth)
		}
		gotAuth = ""
	}
	t.Run("tags", func(t *testing.T) {
		got, err := ListRegistryTagsContext(context.Background(), s.URL, "x", "user", "pass")
		if err != nil || strings.Join(got, ",") != "evil" {
			t.Fatalf("tags = %v, err = %v, want the redirected list", got, err)
		}
		check(t)
	})
	t.Run("catalog", func(t *testing.T) {
		names, _, err := ListCatalogContext(context.Background(), s.URL, "user", "pass", 10, 1000)
		if err != nil || strings.Join(names, ",") != "evil" {
			t.Fatalf("names = %v, err = %v, want the redirected list", names, err)
		}
		check(t)
	})
}

// A token response over the bound is an error, not an unbounded read.
func TestExchangeBearerTokenBoundsResponse(t *testing.T) {
	const bound = 1 << 20
	huge := `{"token":"tok","pad":"` + strings.Repeat("a", 2*bound) + `"}`
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, huge)
	}))
	defer tokenSrv.Close()
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Any Bearer token is accepted, so only the token bound can fail.
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			_, _ = fmt.Fprint(w, `{"tags":["a"]}`)
			return
		}
		w.Header().Set("Www-Authenticate", `Bearer realm="`+tokenSrv.URL+`/token",service="s"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer reg.Close()

	if _, err := ListRegistryTagsContext(context.Background(), reg.URL, "x", "user", "pass"); err == nil {
		t.Fatal("err = nil, want an error for a token response over 1 MiB")
	}
}

// A token response under the bound still works.
func TestExchangeBearerTokenSmallResponse(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"token":"tok"}`)
	}))
	defer tokenSrv.Close()
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.Header().Set("Www-Authenticate", `Bearer realm="`+tokenSrv.URL+`/token",service="s"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprint(w, `{"tags":["a"]}`)
	}))
	defer reg.Close()
	got, err := ListRegistryTagsContext(context.Background(), reg.URL, "x", "user", "pass")
	if err != nil || strings.Join(got, ",") != "a" {
		t.Fatalf("tags = %v, err = %v, want [a]", got, err)
	}
}
