package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// These tests pin ListCatalogContext (DEVDESK-306 step 3): a bounded walk of
// /v2/_catalog that follows Link rel="next", never leaves the origin, and
// reuses the tag flow's authentication.

func catalogBody(names ...string) string {
	b, _ := json.Marshal(map[string][]string{"repositories": names})
	return string(b)
}

func numbered(prefix string, from, count int) []string {
	out := make([]string, count)
	for i := range out {
		out[i] = fmt.Sprintf("%s%05d", prefix, from+i)
	}
	return out
}

func TestListCatalogContextPagination(t *testing.T) {
	var requests atomic.Int32
	var firstQuery atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		if n == 1 {
			firstQuery.Store(r.URL.Query().Get("n"))
		}
		switch r.URL.Query().Get("last") {
		case "":
			w.Header().Set("Link", `</v2/_catalog?n=1000&last=a2>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogBody("a1", "a2"))
		case "a2":
			w.Header().Set("Link", `</v2/_catalog?n=1000&last=b2>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogBody("b1", "b2"))
		default:
			_, _ = fmt.Fprint(w, catalogBody("c1"))
		}
	}))
	defer srv.Close()

	names, truncated, err := ListCatalogContext(context.Background(), srv.URL, "", "", 10, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(names, ","), "a1,a2,b1,b2,c1"; got != want {
		t.Errorf("names = %s, want %s", got, want)
	}
	if truncated {
		t.Error("truncated = true, want false")
	}
	if requests.Load() != 3 {
		t.Errorf("requests = %d, want 3", requests.Load())
	}
	if q, _ := firstQuery.Load().(string); q != "1000" {
		t.Errorf("first request n = %q, want 1000", q)
	}
}

func TestListCatalogContextTrailingSlashBase(t *testing.T) {
	var path atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path.Store(r.URL.Path)
		_, _ = fmt.Fprint(w, catalogBody("a"))
	}))
	defer srv.Close()
	if _, _, err := ListCatalogContext(context.Background(), srv.URL+"/", "", "", 1, 10); err != nil {
		t.Fatal(err)
	}
	if p, _ := path.Load().(string); p != "/v2/_catalog" {
		t.Errorf("path = %q, want /v2/_catalog", p)
	}
}

func TestListCatalogContextBounds(t *testing.T) {
	t.Run("endless next stops at maxPages", func(t *testing.T) {
		var requests atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n := int(requests.Add(1))
			w.Header().Set("Link", fmt.Sprintf(`</v2/_catalog?n=1000&last=p%d>; rel="next"`, n))
			_, _ = fmt.Fprint(w, catalogBody(numbered("r", n*1000, 1000)...))
		}))
		defer srv.Close()

		names, truncated, err := ListCatalogContext(context.Background(), srv.URL, "", "", 10, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if requests.Load() != 10 {
			t.Errorf("requests = %d, want 10", requests.Load())
		}
		if !truncated {
			t.Error("truncated = false, want true")
		}
		if len(names) != 10000 {
			t.Errorf("len(names) = %d, want 10000", len(names))
		}
	})

	t.Run("page larger than pageSize is cut", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, catalogBody(numbered("r", 0, 1500)...))
		}))
		defer srv.Close()

		names, truncated, err := ListCatalogContext(context.Background(), srv.URL, "", "", 1, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if len(names) != 1000 {
			t.Errorf("len(names) = %d, want 1000", len(names))
		}
		if !truncated {
			t.Error("truncated = false, want true")
		}
	})

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
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
			}))
			defer srv.Close()
			_, _, err := ListCatalogContext(context.Background(), srv.URL, "", "", tt.maxPages, tt.pgSize)
			if err == nil {
				t.Fatal("err = nil, want an error")
			}
			if requests.Load() != 0 {
				t.Errorf("requests = %d, want 0", requests.Load())
			}
		})
	}
}

func TestListCatalogContextNextOrigin(t *testing.T) {
	var otherHits atomic.Int32
	var otherAuth atomic.Value
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherHits.Add(1)
		otherAuth.Store(r.Header.Get("Authorization"))
		_, _ = fmt.Fprint(w, catalogBody("evil"))
	}))
	defer other.Close()

	t.Run("other origin is not followed", func(t *testing.T) {
		srvOther := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Link", `<`+other.URL+`/v2/_catalog?last=x>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogBody("first"))
		}))
		defer srvOther.Close()

		names, truncated, err := ListCatalogContext(context.Background(), srvOther.URL, "user", "pass", 10, 1000)
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
	})

	for _, tt := range []struct {
		name string
		link func(self string) string
	}{
		{"relative next is followed", func(string) string { return "/v2/_catalog?last=x" }},
		{"same-origin absolute next is followed", func(self string) string { return self + "/v2/_catalog?last=x" }},
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

			names, truncated, err := ListCatalogContext(context.Background(), s.URL, "", "", 10, 1000)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(names, ","); got != "first,second" {
				t.Errorf("names = %s, want first,second", got)
			}
			if truncated {
				t.Error("truncated = true, want false")
			}
		})
	}
}

func TestSameOrigin(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"https://r.io/v2/_catalog", "https://r.io/v2/_catalog?last=x", true},
		{"https://r.io/a", "http://r.io/a", false},
		{"https://r.io/a", "https://other.io/a", false},
		{"https://r.io/a", "https://r.io:8443/a", false},
		{"http://127.0.0.1:5000/a", "http://127.0.0.1:5000/b", true},
		{"http://127.0.0.1:5000/a", "http://127.0.0.1:5001/b", false},
	}
	for _, tt := range tests {
		if got := sameOrigin(tt.a, tt.b); got != tt.want {
			t.Errorf("sameOrigin(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestListCatalogContextStatusError(t *testing.T) {
	for _, code := range []int{401, 403, 404, 405, 429, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
			}))
			defer srv.Close()

			names, _, err := ListCatalogContext(context.Background(), srv.URL, "", "", 10, 1000)
			var se *StatusError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v (%T), want a *StatusError", err, err)
			}
			if se.Code != code || se.Source != SourceRegistry {
				t.Errorf("got {%d, %v}, want {%d, SourceRegistry}", se.Code, se.Source, code)
			}
			if names != nil {
				t.Errorf("names = %v, want nil", names)
			}
		})
	}
}

// bearerCatalogServer answers /v2/_catalog behind a Bearer challenge whose
// realm is its own /token path.
type bearerCatalogServer struct {
	*httptest.Server
	tokenCalls  atomic.Int32
	tokenAuth   atomic.Value // Authorization header seen by /token
	firstAuth   atomic.Value // Authorization header of the first catalog GET
	pageAuths   []string
	pageAuthsMu chan struct{}
}

func newBearerCatalogServer(t *testing.T, tokenStatus int) *bearerCatalogServer {
	t.Helper()
	s := &bearerCatalogServer{pageAuthsMu: make(chan struct{}, 1)}
	var first atomic.Bool
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			s.tokenCalls.Add(1)
			s.tokenAuth.Store(r.Header.Get("Authorization"))
			if tokenStatus != 200 {
				w.WriteHeader(tokenStatus)
				return
			}
			_, _ = fmt.Fprint(w, `{"token":"tok123"}`)
			return
		}
		auth := r.Header.Get("Authorization")
		if first.CompareAndSwap(false, true) {
			s.firstAuth.Store(auth)
		}
		s.pageAuthsMu <- struct{}{}
		s.pageAuths = append(s.pageAuths, auth)
		<-s.pageAuthsMu
		if auth != "Bearer tok123" {
			w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="x",scope="registry:catalog:*"`, s.URL))
			w.WriteHeader(401)
			return
		}
		if r.URL.Query().Get("last") == "" {
			w.Header().Set("Link", `</v2/_catalog?n=1000&last=a>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogBody("a"))
			return
		}
		_, _ = fmt.Fprint(w, catalogBody("b"))
	}))
	t.Cleanup(s.Close)
	return s
}

func TestListCatalogContextBearerFlow(t *testing.T) {
	t.Run("with credentials the token is fetched once and reused", func(t *testing.T) {
		s := newBearerCatalogServer(t, 200)
		names, truncated, err := ListCatalogContext(context.Background(), s.URL, "user", "pass", 10, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(names, ","); got != "a,b" || truncated {
			t.Errorf("names = %s truncated = %v, want a,b false", got, truncated)
		}
		if s.tokenCalls.Load() != 1 {
			t.Errorf("realm calls = %d, want 1 (token kept across pages)", s.tokenCalls.Load())
		}
		wantBasic := "Basic dXNlcjpwYXNz" // user:pass
		if a, _ := s.tokenAuth.Load().(string); a != wantBasic {
			t.Errorf("token endpoint Authorization = %q, want %q", a, wantBasic)
		}
		last := s.pageAuths[len(s.pageAuths)-1]
		if last != "Bearer tok123" {
			t.Errorf("page 2 Authorization = %q, want Bearer tok123", last)
		}
	})

	t.Run("without credentials no Authorization header is sent", func(t *testing.T) {
		s := newBearerCatalogServer(t, 200)
		if _, _, err := ListCatalogContext(context.Background(), s.URL, "", "", 10, 1000); err != nil {
			t.Fatal(err)
		}
		if a, _ := s.tokenAuth.Load().(string); a != "" {
			t.Errorf("token endpoint Authorization = %q, want none", a)
		}
		if a, _ := s.firstAuth.Load().(string); a != "" {
			t.Errorf("first GET Authorization = %q, want none", a)
		}
	})

	t.Run("401 after the exchange", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/token" {
				_, _ = fmt.Fprint(w, `{"token":"nope"}`)
				return
			}
			w.Header().Set("Www-Authenticate", `Bearer realm="http://`+r.Host+`/token",service="x"`)
			w.WriteHeader(401)
		}))
		defer srv.Close()
		_, _, err := ListCatalogContext(context.Background(), srv.URL, "user", "pass", 10, 1000)
		var se *StatusError
		if !errors.As(err, &se) || se.Code != 401 || se.Source != SourceRegistry {
			t.Fatalf("err = %v, want *StatusError{401, SourceRegistry}", err)
		}
	})

	t.Run("token endpoint 403", func(t *testing.T) {
		s := newBearerCatalogServer(t, 403)
		_, _, err := ListCatalogContext(context.Background(), s.URL, "user", "pass", 10, 1000)
		var se *StatusError
		if !errors.As(err, &se) || se.Code != 403 || se.Source != SourceToken {
			t.Fatalf("err = %v, want *StatusError{403, SourceToken}", err)
		}
	})
}

func TestListCatalogContextBody(t *testing.T) {
	t.Run("body over 1 MiB", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"repositories":["`+strings.Repeat("a", maxCatalogPage)+`"]}`)
		}))
		defer srv.Close()
		names, _, err := ListCatalogContext(context.Background(), srv.URL, "", "", 10, 1000)
		if err == nil {
			t.Fatal("err = nil, want an error")
		}
		var se *StatusError
		if errors.As(err, &se) {
			t.Errorf("err = %v is a *StatusError, want a plain error", err)
		}
		if !strings.Contains(err.Error(), "catalog page over 1048576 bytes") {
			t.Errorf("err = %q, want it to mention the page cap", err)
		}
		if names != nil {
			t.Errorf("names = %d entries, want nil", len(names))
		}
	})

	t.Run("unreadable body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `<html>not json`)
		}))
		defer srv.Close()
		_, _, err := ListCatalogContext(context.Background(), srv.URL, "", "", 10, 1000)
		if err == nil || !strings.Contains(err.Error(), "decode catalog response") {
			t.Errorf("err = %v, want decode catalog response", err)
		}
	})

	for _, body := range []string{`{}`, `{"repositories":null}`, `{"repositories":[]}`} {
		t.Run("empty "+body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, body)
			}))
			defer srv.Close()
			names, truncated, err := ListCatalogContext(context.Background(), srv.URL, "", "", 10, 1000)
			if err != nil {
				t.Fatal(err)
			}
			if len(names) != 0 || truncated {
				t.Errorf("names = %v truncated = %v, want none false", names, truncated)
			}
		})
	}
}

func TestListCatalogContextCancelled(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("last") == "" {
			w.Header().Set("Link", `</v2/_catalog?n=1000&last=a>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogBody("a"))
			return
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	names, _, err := ListCatalogContext(ctx, srv.URL, "", "", 10, 1000)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if names != nil {
		t.Errorf("names = %v, want nil", names)
	}
}
