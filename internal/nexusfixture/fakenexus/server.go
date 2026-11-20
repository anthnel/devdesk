// Package fakenexus replays recorded Nexus answers over loopback HTTP. It is the
// only package of the fixture tree that imports testing: the recorder and the
// nexus-bench binary stay free of it (nexusfixture itself must not import it).
// Import it from _test.go files only.
package fakenexus

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// Password is the password the fake accepts for every account.
const Password = "fixture-password"

// User returns the user name an identity authenticates as; anonymous has none.
func User(id nexusfixture.Identity) string {
	switch id {
	case nexusfixture.Admin:
		return "admin"
	case nexusfixture.Pull:
		return "bench-pull"
	default:
		return ""
	}
}

// unknownCredentials names the identity of a request whose credentials are not
// one of the bench's accounts (wrong password, unknown user, Bearer...).
const unknownCredentials = "unrecognized credentials"

// route is one recorded exchange, with the fixture it comes from.
type route struct {
	fixture  string
	identity nexusfixture.Identity
	ex       nexusfixture.Exchange
}

// Server answers the recorded exchanges, one httptest server per recorded Host.
type Server struct {
	t testing.TB

	mu       sync.Mutex // guards hits and the fields set by New
	hits     map[string]int
	urls     map[string]string // recorded Host -> fake server URL
	servers  []*httptest.Server
	rewriter *strings.Replacer
}

// New starts one loopback server for each Host the fixtures were recorded on and
// closes them with t. A request no fixture answers gets a 501 and a t.Errorf
// (never a Fatalf: the handler does not run on the test goroutine).
func New(t testing.TB, fixtures ...nexusfixture.Fixture) *Server {
	t.Helper()
	s := &Server{t: t, hits: map[string]int{}, urls: map[string]string{}}

	routes := map[string][]route{}
	seen := map[exchangeKey]string{} // exchange -> the fixture that recorded it
	var hosts []string
	for _, f := range fixtures {
		for _, ex := range f.Exchanges {
			if _, ok := routes[ex.Host]; !ok {
				hosts = append(hosts, ex.Host)
				routes[ex.Host] = nil
			}
			if !s.accept(f, ex, seen) {
				continue
			}
			routes[ex.Host] = append(routes[ex.Host], route{fixture: f.Name, identity: f.Identity, ex: ex})
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	var pairs []string
	for _, host := range hosts {
		srv := httptest.NewServer(s.handler(routes[host]))
		s.servers = append(s.servers, srv)
		s.urls[host] = srv.URL
		pairs = append(pairs, "http://"+host, srv.URL)
	}
	s.rewriter = strings.NewReplacer(pairs...)
	t.Cleanup(func() {
		for _, srv := range s.servers {
			srv.Close()
		}
	})
	return s
}

// exchangeKey is what a request is matched on: two exchanges sharing it cannot
// both be replayed.
type exchangeKey struct {
	host, method, target string
	identity             nexusfixture.Identity
}

// accept reports whether an exchange can be replayed faithfully, and fails the
// test (naming the fixture) when it cannot: a Status of 0 (the handler would
// panic on it), a stub with no faithful stand-in, or a second exchange for the
// same host, method, target and identity. A refused exchange is not served.
func (s *Server) accept(f nexusfixture.Fixture, ex nexusfixture.Exchange, seen map[exchangeKey]string) bool {
	ok := true
	if ex.Status == 0 {
		s.t.Errorf("fakenexus: fixture %s: %s %s on %s has no status", f.Name, ex.Method, ex.Target, ex.Host)
		ok = false
	}
	if _, valid := bodyOf(ex); !valid {
		s.t.Errorf("fakenexus: fixture %s: %s %s: stub %q has no faithful stand-in (only %q and an HTML summary with a title do)",
			f.Name, ex.Method, ex.Target, ex.Stub, emptyStub)
		ok = false
	}
	key := exchangeKey{host: ex.Host, method: ex.Method, target: ex.Target, identity: f.Identity}
	if first, dup := seen[key]; dup {
		s.t.Errorf("fakenexus: fixtures %s and %s both record %s %s on %s as %s",
			first, f.Name, ex.Method, ex.Target, ex.Host, f.Identity)
		ok = false
	} else {
		seen[key] = f.Name
	}
	return ok
}

// URL returns the base URL of the fake server standing for a recorded Host,
// such as "127.0.0.1:30481".
func (s *Server) URL(recordedHost string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.urls[recordedHost]
	if !ok {
		s.t.Errorf("fakenexus: no fixture was recorded on host %s", recordedHost)
	}
	return u
}

// Hits counts the requests the named fixture answered, tolerated ones included.
func (s *Server) Hits(fixtureName string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[fixtureName]
}

func (s *Server) handler(routes []route) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, known := identify(r)
		if known {
			if rt, ok := match(routes, r.Method, r.URL.RequestURI(), id); ok {
				s.replay(w, rt)
				return
			}
		}
		label := unknownCredentials
		if known {
			label = string(id)
		}
		s.t.Errorf("no recorded answer for %s %s as %s", r.Method, r.URL.RequestURI(), label)
		http.Error(w, "no recorded answer", http.StatusNotImplemented)
	})
}

// identify reads the account a request authenticates as. known is false when the
// credentials are none of the bench's: that never matches a recording.
func identify(r *http.Request) (id nexusfixture.Identity, known bool) {
	if r.Header.Get("Authorization") == "" {
		return nexusfixture.Anonymous, true
	}
	user, pass, ok := r.BasicAuth()
	if !ok || pass != Password {
		return "", false
	}
	for _, id := range []nexusfixture.Identity{nexusfixture.Admin, nexusfixture.Pull} {
		if user == User(id) {
			return id, true
		}
	}
	return "", false
}

// match picks the exchange recorded for the method, target and identity: the
// exact target first, then the catalog tolerance.
func match(routes []route, method, target string, id nexusfixture.Identity) (route, bool) {
	for _, rt := range routes {
		if rt.identity == id && rt.ex.Method == method && rt.ex.Target == target {
			return rt, true
		}
	}
	if method != http.MethodGet {
		return route{}, false
	}
	return matchCatalogTolerance(routes, target, id)
}

// toleratedN is the only n (what DevDesk asks) tolerated without "last".
const toleratedN = 1000

// matchCatalogTolerance lets "GET P?n=1000" (no "last") be answered by a fixture
// recorded as "GET P" without a query, provided that answer is a 200 with no Link
// and lists at most 1000 names: one page holds the whole catalog, as with n=1000.
// Any other n is not measured and is not tolerated.
//
// Measured for the group connector: n=1000 and no n answer identically
// (page-group-n1000--admin and catalog-group-connector-30482--admin, pinned by
// TestTheCatalogToleranceHasMeasuredGround). Inferred for hosted and proxy: only
// no-n and n=2 were measured there. The same inference covers the catalog read by
// path, "/v2/<repository>/_catalog" (DEVDESK-579), on hosted and proxy: no n=1000
// was measured against a real Nexus on that path. A group answers 404 there, which
// is never tolerated, so "?n=1000" on a group path stays unmatched.
func matchCatalogTolerance(routes []route, target string, id nexusfixture.Identity) (route, bool) {
	path, rawQuery, _ := strings.Cut(target, "?")
	query, err := url.ParseQuery(rawQuery)
	if err != nil || len(query) != 1 || len(query["n"]) != 1 {
		return route{}, false
	}
	n, err := strconv.Atoi(query.Get("n"))
	if err != nil || n != toleratedN {
		return route{}, false
	}
	for _, rt := range routes {
		ex := rt.ex
		if rt.identity != id || ex.Method != http.MethodGet || ex.Target != path ||
			ex.Status != http.StatusOK || ex.Header.Get("Link") != "" {
			continue
		}
		var catalog struct {
			Repositories []string `json:"repositories"`
		}
		if json.Unmarshal(ex.Body, &catalog) == nil && catalog.Repositories != nil && len(catalog.Repositories) <= n {
			return rt, true
		}
	}
	return route{}, false
}

// rewrittenHeaders carry an address of the recorded host; they are pointed at the
// fake server of that host. A body is data and is never rewritten.
var rewrittenHeaders = []string{"Link", "Location", "Www-Authenticate"}

// replay writes the recorded status, headers and body. Content-Length is never
// copied: it is recomputed from the body actually served, since a stub is shorter
// than the answer it summarises.
func (s *Server) replay(w http.ResponseWriter, rt route) {
	s.mu.Lock()
	s.hits[rt.fixture]++
	rewriter := s.rewriter
	s.mu.Unlock()

	ex := rt.ex
	h := w.Header()
	for name, values := range ex.Header {
		if name == "Content-Length" {
			continue
		}
		rewrite := contains(rewrittenHeaders, name)
		for _, v := range values {
			if rewrite {
				v = rewriter.Replace(v)
			}
			h.Add(name, v)
		}
	}
	if len(ex.Header.Values("Content-Type")) == 0 {
		h["Content-Type"] = nil // a nil value stops Go from sniffing one
	}
	body, _ := bodyOf(ex) // a stub without a stand-in was refused by New
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(ex.Status)
	_, _ = w.Write(body)
}

var htmlTitle = regexp.MustCompile(`, (<title>.*</title>)>$`)

// emptyStub is the stub of a recorded empty body.
const emptyStub = "<empty body>"

// bodyOf is the body to serve: the recorded one, or a stand-in for a summarised
// body. Only two stubs have a faithful one: an empty body and an HTML page that
// kept its title. valid is false for any other stub.
func bodyOf(ex nexusfixture.Exchange) (body []byte, valid bool) {
	switch {
	case ex.Stub == "":
		return ex.Body, true
	case ex.Stub == emptyStub:
		return nil, true
	case strings.HasPrefix(ex.Stub, "<HTML body omitted:"):
		if m := htmlTitle.FindStringSubmatch(ex.Stub); m != nil {
			return []byte("<html><head>" + m[1] + "</head></html>"), true
		}
	}
	return nil, false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
