package oci

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// These tests pin ListCatalogUnderContext (DEVDESK-579 step 1): the catalog of
// a Nexus group member is read at /v2/<prefix>/_catalog. Names come back as the
// server sent them (not prefixed); rebuilding "<prefix>/<name>" is the
// caller's job. The walk keeps every guard of ListCatalogContext (same origin,
// bounded pages, no credentials to another origin), the prefix segments are
// escaped, and an unsafe prefix is refused before any request.

// requestLog records the raw request target (path and query, as sent on the
// wire) of every request a test server receives, and the Authorization header
// of each.
type requestLog struct {
	mu    sync.Mutex
	uris  []string
	auths []string
}

func (l *requestLog) add(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.uris = append(l.uris, r.RequestURI)
	l.auths = append(l.auths, r.Header.Get("Authorization"))
}

func (l *requestLog) URIs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.uris...)
}

func (l *requestLog) Auths() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.auths...)
}

// rawPath is the path part of a request target.
func rawPath(uri string) string {
	p, _, _ := strings.Cut(uri, "?")
	return p
}

func TestListCatalogUnderContextPath(t *testing.T) {
	tests := []struct {
		name       string
		prefix     string
		wantRaw    string // exact path on the wire
		wantDecode string // path as the server decodes it
	}{
		{"empty prefix is the root catalog", "", "/v2/_catalog", "/v2/_catalog"},
		{"one segment", "docker-hosted", "/v2/docker-hosted/_catalog", "/v2/docker-hosted/_catalog"},
		{"proxy member", "docker-proxy", "/v2/docker-proxy/_catalog", "/v2/docker-proxy/_catalog"},
		{"two segments", "a/b", "/v2/a/b/_catalog", "/v2/a/b/_catalog"},
		{"three segments", "a/b/c", "/v2/a/b/c/_catalog", "/v2/a/b/c/_catalog"},
		{"dot and dash inside a segment are kept", "my-repo.v2", "/v2/my-repo.v2/_catalog", "/v2/my-repo.v2/_catalog"},
		{"space is escaped", "my repo", "/v2/my%20repo/_catalog", "/v2/my repo/_catalog"},
		{"space in each segment", "a b/c d", "/v2/a%20b/c%20d/_catalog", "/v2/a b/c d/_catalog"},
		{"percent is escaped", "50%", "/v2/50%25/_catalog", "/v2/50%/_catalog"},
		{"an encoded slash stays one segment", "a%2Fb", "/v2/a%252Fb/_catalog", "/v2/a%2Fb/_catalog"},
		{"an encoded dot-dot is not decoded", "%2e%2e", "/v2/%252e%252e/_catalog", "/v2/%2e%2e/_catalog"},
		{"question mark does not start a query", "a?b", "/v2/a%3Fb/_catalog", "/v2/a?b/_catalog"},
		{"hash does not start a fragment", "a#b", "/v2/a%23b/_catalog", "/v2/a#b/_catalog"},
		{"non-ASCII is escaped", "café", "/v2/caf%C3%A9/_catalog", "/v2/café/_catalog"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var log requestLog
			var decoded atomic.Value
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				log.add(r)
				decoded.Store(r.URL.Path)
				_, _ = fmt.Fprint(w, catalogBody("api", "team/api"))
			}))
			defer srv.Close()

			names, truncated, err := ListCatalogUnderContext(context.Background(), srv.URL, tt.prefix, "", "", 10, 1000)
			if err != nil {
				t.Fatal(err)
			}
			// Names are the server's own: never prefixed by this function.
			if got, want := strings.Join(names, ","), "api,team/api"; got != want {
				t.Errorf("names = %s, want %s", got, want)
			}
			if truncated {
				t.Error("truncated = true, want false")
			}
			uris := log.URIs()
			if len(uris) != 1 {
				t.Fatalf("requests = %v, want exactly one", uris)
			}
			if got := rawPath(uris[0]); got != tt.wantRaw {
				t.Errorf("wire path = %q, want %q", got, tt.wantRaw)
			}
			if got, _ := decoded.Load().(string); got != tt.wantDecode {
				t.Errorf("decoded path = %q, want %q", got, tt.wantDecode)
			}
			// The page size is the only query parameter, whatever the prefix held.
			if _, q, _ := strings.Cut(uris[0], "?"); q != "n=1000" {
				t.Errorf("query = %q, want n=1000", q)
			}
		})
	}
}

func TestListCatalogUnderContextFirstRequestTarget(t *testing.T) {
	var log requestLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		_, _ = fmt.Fprint(w, catalogBody("api"))
	}))
	defer srv.Close()

	if _, _, err := ListCatalogUnderContext(context.Background(), srv.URL, "docker-hosted", "", "", 10, 1000); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(log.URIs(), " "), "/v2/docker-hosted/_catalog?n=1000"; got != want {
		t.Errorf("request target = %q, want %q", got, want)
	}
}

func TestListCatalogUnderContextTrailingSlashBase(t *testing.T) {
	var log requestLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r)
		_, _ = fmt.Fprint(w, catalogBody("a"))
	}))
	defer srv.Close()

	if _, _, err := ListCatalogUnderContext(context.Background(), srv.URL+"/", "docker-hosted", "", "", 1, 10); err != nil {
		t.Fatal(err)
	}
	uris := log.URIs()
	if len(uris) != 1 || rawPath(uris[0]) != "/v2/docker-hosted/_catalog" {
		t.Errorf("requests = %v, want one on /v2/docker-hosted/_catalog", uris)
	}
}

func TestListCatalogUnderContextRefusesUnsafePrefix(t *testing.T) {
	const user, pass = "svc-user-4711", "s3cret-pass-4711"
	for _, tt := range []struct {
		name   string
		prefix string
	}{
		{"dot-dot", ".."},
		{"dot-dot last", "a/.."},
		{"dot-dot first", "../a"},
		{"dot-dot middle", "a/../b"},
		{"dot-dot inside a segment", "a..b"},
		{"single dot", "."},
		{"dot first", "./a"},
		{"dot middle", "a/./b"},
		{"empty segment in the middle", "a//b"},
		{"leading slash", "/docker-hosted"},
		{"trailing slash", "docker-hosted/"},
		{"only a slash", "/"},
		{"backslash", `a\b`},
		{"leading backslash", `\docker-hosted`},
		{"NUL byte", "a\x00b"},
		{"NUL alone", "\x00"},
		{"blank segment", " "},
		{"blank segment in the middle", "a/ /b"},
		{"tab-only segment", "\t"},
		{"newline", "a\nb"},
		{"carriage return", "a\rb"},
		{"tab", "a\tb"},
		{"DEL", "a\x7fb"},
		{"segment over 255 bytes", strings.Repeat("a", 256)},
		{"second segment over 255 bytes", "a/" + strings.Repeat("b", 256)},
		{"prefix over 1024 bytes", strings.Join([]string{strings.Repeat("a", 250), strings.Repeat("b", 250), strings.Repeat("c", 250), strings.Repeat("d", 250), strings.Repeat("e", 21)}, "/")},
		{"huge prefix", strings.Repeat("a/", 5000) + "a"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				_, _ = fmt.Fprint(w, catalogBody("api"))
			}))
			defer srv.Close()

			names, truncated, err := ListCatalogUnderContext(context.Background(), srv.URL, tt.prefix, user, pass, 10, 1000)
			if err == nil {
				t.Fatal("err = nil, want a refusal")
			}
			if hits.Load() != 0 {
				t.Errorf("requests = %d, want 0: an unsafe prefix must be refused before any request", hits.Load())
			}
			if names != nil || truncated {
				t.Errorf("names = %v truncated = %v, want nil false", names, truncated)
			}
			var se *StatusError
			if errors.As(err, &se) {
				t.Errorf("err = %v is a *StatusError, want a plain refusal (nothing was asked)", err)
			}
			if strings.TrimSpace(err.Error()) == "" {
				t.Error("err message is empty, want a readable one")
			}
			// The error gives the rule, not the prefix (which may be long).
			if len(err.Error()) > 200 {
				t.Errorf("err is %d bytes long, want a short rule: %.80q...", len(err.Error()), err)
			}
			// The refusal never echoes a secret.
			for _, secret := range []string{user, pass} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("err = %q echoes %q", err, secret)
				}
			}
		})
	}
}

// The length bound is on the edge: 255 bytes per segment and 1024 in total are
// accepted, one more is not (the refusals are in the unsafe prefix table).
func TestListCatalogUnderContextAcceptsPrefixAtTheLengthBound(t *testing.T) {
	atTotal := strings.Join([]string{strings.Repeat("a", 250), strings.Repeat("b", 250), strings.Repeat("c", 250), strings.Repeat("d", 250), strings.Repeat("e", 20)}, "/")
	if len(atTotal) != 1024 {
		t.Fatalf("fixture is %d bytes, want 1024", len(atTotal))
	}
	for _, tt := range []struct {
		name   string
		prefix string
	}{
		{"segment of 255 bytes", strings.Repeat("a", 255)},
		{"1024 bytes in total", atTotal},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				_, _ = fmt.Fprint(w, catalogBody("api"))
			}))
			defer srv.Close()

			if _, _, err := ListCatalogUnderContext(context.Background(), srv.URL, tt.prefix, "", "", 1, 10); err != nil {
				t.Fatal(err)
			}
			if hits.Load() != 1 {
				t.Errorf("requests = %d, want 1", hits.Load())
			}
		})
	}
}

func TestListCatalogUnderContextBoundsStillApply(t *testing.T) {
	t.Run("non-positive limits are refused without a request", func(t *testing.T) {
		for _, tt := range []struct {
			name             string
			maxPages, pgSize int
		}{
			{"zero maxPages", 0, 1000},
			{"negative maxPages", -1, 1000},
			{"zero pageSize", 10, 0},
			{"negative pageSize", 10, -5},
		} {
			t.Run(tt.name, func(t *testing.T) {
				var hits atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					hits.Add(1)
				}))
				defer srv.Close()
				_, _, err := ListCatalogUnderContext(context.Background(), srv.URL, "docker-hosted", "", "", tt.maxPages, tt.pgSize)
				if err == nil {
					t.Fatal("err = nil, want an error")
				}
				if hits.Load() != 0 {
					t.Errorf("requests = %d, want 0", hits.Load())
				}
			})
		}
	})

	t.Run("endless next stops at maxPages, on the prefix path", func(t *testing.T) {
		var log requestLog
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.add(r)
			n := len(log.URIs())
			w.Header().Set("Link", fmt.Sprintf(`</v2/docker-hosted/_catalog?n=1000&last=p%d>; rel="next"`, n))
			_, _ = fmt.Fprint(w, catalogBody(numbered("r", n*1000, 1000)...))
		}))
		defer srv.Close()

		names, truncated, err := ListCatalogUnderContext(context.Background(), srv.URL, "docker-hosted", "", "", 4, 1000)
		if err != nil {
			t.Fatal(err)
		}
		uris := log.URIs()
		if len(uris) != 4 {
			t.Errorf("requests = %d, want 4", len(uris))
		}
		for _, u := range uris {
			if rawPath(u) != "/v2/docker-hosted/_catalog" {
				t.Errorf("request %q left the member's catalog path", u)
			}
		}
		if !truncated {
			t.Error("truncated = false, want true")
		}
		if len(names) != 4000 {
			t.Errorf("len(names) = %d, want 4000", len(names))
		}
	})

	t.Run("page larger than pageSize is cut", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, catalogBody(numbered("r", 0, 1500)...))
		}))
		defer srv.Close()

		names, truncated, err := ListCatalogUnderContext(context.Background(), srv.URL, "docker-hosted", "", "", 1, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if len(names) != 1000 || !truncated {
			t.Errorf("len(names) = %d truncated = %v, want 1000 true", len(names), truncated)
		}
	})

	t.Run("body over the page cap is a plain error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"repositories":["`+strings.Repeat("a", maxCatalogPage)+`"]}`)
		}))
		defer srv.Close()

		names, _, err := ListCatalogUnderContext(context.Background(), srv.URL, "docker-hosted", "", "", 10, 1000)
		if err == nil || !strings.Contains(err.Error(), "catalog page over 1048576 bytes") {
			t.Fatalf("err = %v, want the page cap error", err)
		}
		if names != nil {
			t.Errorf("names = %d entries, want nil", len(names))
		}
	})
}

func TestListCatalogUnderContextPagination(t *testing.T) {
	var log requestLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r)
		// The measured Nexus shape: a same-origin Link in path form.
		switch r.URL.Query().Get("last") {
		case "":
			w.Header().Set("Link", `</v2/docker-hosted/_catalog?n=2&last=b>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogBody("a", "b"))
		case "b":
			w.Header().Set("Link", `</v2/docker-hosted/_catalog?n=2&last=d>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogBody("c", "d"))
		default:
			_, _ = fmt.Fprint(w, catalogBody("e"))
		}
	}))
	defer srv.Close()

	names, truncated, err := ListCatalogUnderContext(context.Background(), srv.URL, "docker-hosted", "", "", 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(names, ","), "a,b,c,d,e"; got != want {
		t.Errorf("names = %s, want %s", got, want)
	}
	if truncated {
		t.Error("truncated = true, want false")
	}
	want := []string{
		"/v2/docker-hosted/_catalog?n=2",
		"/v2/docker-hosted/_catalog?n=2&last=b",
		"/v2/docker-hosted/_catalog?n=2&last=d",
	}
	if got := log.URIs(); !reflect.DeepEqual(got, want) {
		t.Errorf("request targets = %v, want %v", got, want)
	}
}

func TestListCatalogUnderContextNextOrigin(t *testing.T) {
	var otherHits atomic.Int32
	var otherAuth atomic.Value
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherHits.Add(1)
		otherAuth.Store(r.Header.Get("Authorization"))
		_, _ = fmt.Fprint(w, catalogBody("evil"))
	}))
	defer other.Close()

	t.Run("other origin is not followed and gets no credentials", func(t *testing.T) {
		var log requestLog
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.add(r)
			w.Header().Set("Link", `<`+other.URL+`/v2/docker-hosted/_catalog?last=x>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogBody("first"))
		}))
		defer srv.Close()

		names, truncated, err := ListCatalogUnderContext(context.Background(), srv.URL, "docker-hosted", "user", "pass", 10, 1000)
		if err != nil {
			t.Fatalf("err = %v, want a partial walk without error", err)
		}
		if got := strings.Join(names, ","); got != "first" {
			t.Errorf("names = %s, want first", got)
		}
		if !truncated {
			t.Error("truncated = false, want true")
		}
		if otherHits.Load() != 0 {
			t.Errorf("other origin hit %d times, want 0", otherHits.Load())
		}
		if a, _ := otherAuth.Load().(string); a != "" {
			t.Errorf("credentials reached the other origin: %q", a)
		}
		if n := len(log.URIs()); n != 1 {
			t.Errorf("requests to the member's origin = %d, want 1", n)
		}
	})

	for _, tt := range []struct {
		name string
		link func(self string) string
	}{
		{"relative next is followed", func(string) string { return "/v2/docker-hosted/_catalog?last=x" }},
		{"same-origin absolute next is followed", func(self string) string { return self + "/v2/docker-hosted/_catalog?last=x" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var s *httptest.Server
			s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("last") == "" {
					w.Header().Set("Link", `<`+tt.link(s.URL)+`>; rel="next"`)
					_, _ = fmt.Fprint(w, catalogBody("first"))
					return
				}
				_, _ = fmt.Fprint(w, catalogBody("second"))
			}))
			defer s.Close()

			names, truncated, err := ListCatalogUnderContext(context.Background(), s.URL, "docker-hosted", "", "", 10, 1000)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(names, ","); got != "first,second" || truncated {
				t.Errorf("names = %s truncated = %v, want first,second false", got, truncated)
			}
		})
	}
}

// Under a prefix, a next link is followed only when it stays on the member's
// catalog path: a link to /v2/_catalog or to another repository would attach
// names of another catalog to the prefix. With an empty prefix the walk is the
// root one and follows any same-origin path, as before.
func TestListCatalogUnderContextNextPath(t *testing.T) {
	pageWithNext := func(link string, log *requestLog) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.add(r)
			if r.URL.Query().Get("last") == "" && r.URL.Path != "/v2/other/_catalog" {
				w.Header().Set("Link", `<`+link+`>; rel="next"`)
				_, _ = fmt.Fprint(w, catalogBody("first"))
				return
			}
			_, _ = fmt.Fprint(w, catalogBody("second"))
		}))
	}

	for _, tt := range []struct {
		name   string
		prefix string
		link   string
		follow bool
	}{
		{"member path is followed", "docker-hosted", "/v2/docker-hosted/_catalog?n=1000&last=x", true},
		{"escaped member path is followed", "my repo", "/v2/my%20repo/_catalog?last=x", true},
		{"nested member path is followed", "a/b", "/v2/a/b/_catalog?last=x", true},
		{"root catalog is not followed", "docker-hosted", "/v2/_catalog?last=x", false},
		{"another repository is not followed", "docker-hosted", "/v2/other/_catalog?last=x", false},
		{"a sub-repository is not followed", "docker-hosted", "/v2/docker-hosted/sub/_catalog?last=x", false},
		{"a parent of a nested member is not followed", "a/b", "/v2/a/_catalog?last=x", false},
		{"a path outside /v2 is not followed", "docker-hosted", "/other?last=x", false},
		{"a trailing slash is not followed", "docker-hosted", "/v2/docker-hosted/_catalog/?last=x", false},
		{"a dot-dot out of the member is not followed", "docker-hosted", "/v2/docker-hosted/../_catalog?last=x", false},
		{"another spelling of the escape is not followed", "my repo", "/v2/my%2520repo/_catalog?last=x", false},
		{"empty prefix follows the root catalog", "", "/v2/_catalog?last=x", true},
		{"empty prefix still follows another same-origin path", "", "/v2/other/_catalog?last=x", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var log requestLog
			srv := pageWithNext(tt.link, &log)
			defer srv.Close()

			names, truncated, err := ListCatalogUnderContext(context.Background(), srv.URL, tt.prefix, "user", "pass", 10, 1000)
			if err != nil {
				t.Fatalf("err = %v, want a walk without error", err)
			}
			uris := log.URIs()
			if tt.follow {
				if got := strings.Join(names, ","); got != "first,second" || truncated {
					t.Errorf("names = %s truncated = %v, want first,second false", got, truncated)
				}
				if len(uris) != 2 {
					t.Errorf("requests = %v, want 2", uris)
				}
				return
			}
			if got := strings.Join(names, ","); got != "first" {
				t.Errorf("names = %s, want first only (no name of another catalog)", got)
			}
			if !truncated {
				t.Error("truncated = false, want true")
			}
			if len(uris) != 1 {
				t.Errorf("requests = %v, want exactly the first page", uris)
			}
		})
	}
}

// A next link that changes the origin by its spelling is not followed either,
// and the other side never sees the credentials.
func TestListCatalogUnderContextNextOriginSpellings(t *testing.T) {
	t.Run("protocol-relative next to another host", func(t *testing.T) {
		var otherHits atomic.Int32
		var otherAuth atomic.Value
		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			otherHits.Add(1)
			otherAuth.Store(r.Header.Get("Authorization"))
			_, _ = fmt.Fprint(w, catalogBody("evil"))
		}))
		defer other.Close()

		var log requestLog
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.add(r)
			w.Header().Set("Link", `<//`+other.Listener.Addr().String()+`/v2/docker-hosted/_catalog?last=x>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogBody("first"))
		}))
		defer srv.Close()

		names, truncated, err := ListCatalogUnderContext(context.Background(), srv.URL, "docker-hosted", "user", "pass", 10, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(names, ","); got != "first" || !truncated {
			t.Errorf("names = %s truncated = %v, want first true", got, truncated)
		}
		if otherHits.Load() != 0 {
			t.Errorf("other host hit %d times, want 0", otherHits.Load())
		}
		if a, _ := otherAuth.Load().(string); a != "" {
			t.Errorf("credentials reached the other host: %q", a)
		}
		if n := len(log.URIs()); n != 1 {
			t.Errorf("requests to the member's host = %d, want 1", n)
		}
	})

	t.Run("https to http on the same host is not followed", func(t *testing.T) {
		var log requestLog
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.add(r)
			downgraded := "http://" + r.Host + "/v2/docker-hosted/_catalog?last=x"
			w.Header().Set("Link", `<`+downgraded+`>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogBody("first"))
		}))
		defer srv.Close()
		// Trust the test certificate; the walk's own policy is unchanged.
		useClientTransport(t, srv.Client().Transport)

		names, truncated, err := ListCatalogUnderContext(context.Background(), srv.URL, "docker-hosted", "user", "pass", 10, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(names, ","); got != "first" || !truncated {
			t.Errorf("names = %s truncated = %v, want first true", got, truncated)
		}
		if n := len(log.URIs()); n != 1 {
			t.Errorf("requests = %d, want 1: the downgraded link must not be followed", n)
		}
		for _, a := range log.Auths() {
			if strings.HasPrefix(a, "Basic ") && len(log.URIs()) > 1 {
				t.Errorf("credentials sent after a downgrade: %q", a)
			}
		}
	})
}

func TestListCatalogUnderContextSendsBasicCredentials(t *testing.T) {
	var log requestLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r)
		if r.Header.Get("Authorization") != "Basic dXNlcjpwYXNz" { // user:pass
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprint(w, catalogBody("api"))
	}))
	defer srv.Close()

	names, _, err := ListCatalogUnderContext(context.Background(), srv.URL, "docker-hosted", "user", "pass", 10, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names, ","); got != "api" {
		t.Errorf("names = %s, want api", got)
	}
	for _, u := range log.URIs() {
		if rawPath(u) != "/v2/docker-hosted/_catalog" {
			t.Errorf("request %q is not on the member's catalog path", u)
		}
	}
}

func TestListCatalogUnderContextStatusError(t *testing.T) {
	for _, code := range []int{401, 403, 404, 405, 429, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
			}))
			defer srv.Close()

			names, truncated, err := ListCatalogUnderContext(context.Background(), srv.URL, "docker-hosted", "", "", 10, 1000)
			var se *StatusError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v (%T), want a *StatusError", err, err)
			}
			if se.Code != code || se.Source != SourceRegistry {
				t.Errorf("got {%d, %v}, want {%d, SourceRegistry}", se.Code, se.Source, code)
			}
			if names != nil || truncated {
				t.Errorf("names = %v truncated = %v, want nil false", names, truncated)
			}
		})
	}
}

func TestListCatalogUnderContextBearerFlow(t *testing.T) {
	var tokenCalls atomic.Int32
	var log requestLog
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokenCalls.Add(1)
			_, _ = fmt.Fprint(w, `{"token":"tok123"}`)
			return
		}
		log.add(r)
		if r.Header.Get("Authorization") != "Bearer tok123" {
			w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="x"`, srv.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("last") == "" {
			w.Header().Set("Link", `</v2/docker-hosted/_catalog?n=1000&last=a>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogBody("a"))
			return
		}
		_, _ = fmt.Fprint(w, catalogBody("b"))
	}))
	defer srv.Close()

	names, truncated, err := ListCatalogUnderContext(context.Background(), srv.URL, "docker-hosted", "user", "pass", 10, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names, ","); got != "a,b" || truncated {
		t.Errorf("names = %s truncated = %v, want a,b false", got, truncated)
	}
	if tokenCalls.Load() != 1 {
		t.Errorf("realm calls = %d, want 1 (token kept across pages)", tokenCalls.Load())
	}
	for _, u := range log.URIs() {
		if rawPath(u) != "/v2/docker-hosted/_catalog" {
			t.Errorf("request %q is not on the member's catalog path", u)
		}
	}
}

// ListCatalogContext is ListCatalogUnderContext with an empty prefix: same
// requests, same result, same error, so the existing callers see no change.
func TestListCatalogContextDelegatesWithEmptyPrefix(t *testing.T) {
	type outcome struct {
		names     []string
		truncated bool
		errText   string
		status    int
	}
	run := func(fn func(url string) ([]string, bool, error), handler http.HandlerFunc) (outcome, []string) {
		var log requestLog
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.add(r)
			handler(w, r)
		}))
		defer srv.Close()
		names, truncated, err := fn(srv.URL)
		o := outcome{names: names, truncated: truncated}
		if err != nil {
			o.errText = err.Error()
			var se *StatusError
			if errors.As(err, &se) {
				o.status = se.Code
			}
		}
		return o, log.URIs()
	}

	paged := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("last") {
		case "":
			w.Header().Set("Link", `</v2/_catalog?n=2&last=b>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogBody("a", "b"))
		default:
			_, _ = fmt.Fprint(w, catalogBody("c"))
		}
	}
	endless := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `</v2/_catalog?n=2&last=`+r.URL.Query().Get("last")+`x>; rel="next"`)
		_, _ = fmt.Fprint(w, catalogBody("a", "b"))
	}
	notFound := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }
	undecodable := func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `<html>not json`) }

	for _, tt := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"paged walk", paged},
		{"truncated walk", endless},
		{"404", notFound},
		{"undecodable body", undecodable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			viaLegacy, legacyURIs := run(func(url string) ([]string, bool, error) {
				return ListCatalogContext(context.Background(), url, "user", "pass", 3, 2)
			}, tt.handler)
			viaUnder, underURIs := run(func(url string) ([]string, bool, error) {
				return ListCatalogUnderContext(context.Background(), url, "", "user", "pass", 3, 2)
			}, tt.handler)

			if !reflect.DeepEqual(viaLegacy, viaUnder) {
				t.Errorf("results differ:\n ListCatalogContext          = %+v\n ListCatalogUnderContext(\"\") = %+v", viaLegacy, viaUnder)
			}
			if !reflect.DeepEqual(legacyURIs, underURIs) {
				t.Errorf("requests differ:\n ListCatalogContext          = %v\n ListCatalogUnderContext(\"\") = %v", legacyURIs, underURIs)
			}
			if len(underURIs) == 0 || rawPath(underURIs[0]) != "/v2/_catalog" {
				t.Errorf("first request = %v, want /v2/_catalog", underURIs)
			}
		})
	}
}
