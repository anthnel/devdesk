package fakenexus_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
)

const (
	hostHTTP  = "127.0.0.1:30481" // the HTTP port: REST API and /repository/<name>
	hostGroup = "127.0.0.1:30482" // the group's Docker connector

	groupREST = "/service/rest/v1/repositories/docker/group/docker-group"

	// clientTimeout bounds every request a test makes: a fake that never
	// answers must fail the test, not hang it.
	clientTimeout = 10 * time.Second
)

// newClient is the one client of this file. Redirects are not followed.
func newClient() *http.Client {
	return &http.Client{
		Timeout:       clientTimeout,
		Transport:     &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// recorder stands in for the *testing.T handed to fakenexus.New, so that a test
// can observe the error the server reports for an unrecorded request without
// failing itself. Everything else (Cleanup, Helper, Logf) goes to the real T.
// The server answers from another goroutine, hence the mutex.
type recorder struct {
	testing.TB
	mu   sync.Mutex
	errs []string
}

func (r *recorder) record(args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, strings.TrimSpace(fmt.Sprint(args...)))
}

func (r *recorder) Errorf(format string, args ...any) { r.record(fmt.Sprintf(format, args...)) }
func (r *recorder) Error(args ...any)                 { r.record(args...) }
func (r *recorder) Fatalf(format string, args ...any) { r.record(fmt.Sprintf(format, args...)) }
func (r *recorder) Fatal(args ...any)                 { r.record(args...) }

func (r *recorder) failures() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.errs...)
}

// all returns the embedded NX-03 campaign, sorted by name.
func all(t *testing.T) []nexusfixture.Fixture {
	t.Helper()
	fixtures, err := nexusfixture.Load("catalog")
	if err != nil {
		t.Fatalf("Load(catalog): %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatal("the catalog campaign is empty")
	}
	return fixtures
}

// without returns the campaign minus the fixtures whose name holds any of the
// parts: what a test of the tolerance needs once the exact "?n=1000" answer has
// been recorded (DEVDESK-580), since an exact match always wins.
func without(t *testing.T, parts ...string) []nexusfixture.Fixture {
	t.Helper()
	var out []nexusfixture.Fixture
next:
	for _, f := range all(t) {
		for _, p := range parts {
			if strings.Contains(f.Name, p) {
				continue next
			}
		}
		out = append(out, f)
	}
	return out
}

// loadAll returns the embedded NX-03 campaign by name.
func loadAll(t *testing.T) map[string]nexusfixture.Fixture {
	t.Helper()
	fixtures := all(t)
	byName := make(map[string]nexusfixture.Fixture, len(fixtures))
	for _, f := range fixtures {
		byName[f.Name] = f
	}
	return byName
}

// pick returns the named fixtures, failing on a name that does not exist.
func pick(t *testing.T, names ...string) []nexusfixture.Fixture {
	t.Helper()
	byName := loadAll(t)
	out := make([]nexusfixture.Fixture, 0, len(names))
	for _, n := range names {
		f, ok := byName[n]
		if !ok {
			t.Fatalf("no embedded fixture named %q", n)
		}
		out = append(out, f)
	}
	return out
}

// get sends GET target to base as the identity (no Authorization for anonymous)
// and returns the answer with its body read. Redirects are not followed.
func get(t *testing.T, base, target string, id nexusfixture.Identity) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+target, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if id != nexusfixture.Anonymous {
		req.SetBasicAuth(fakenexus.User(id), fakenexus.Password)
	}
	return do(t, req)
}

func do(t *testing.T, req *http.Request) (*http.Response, []byte) {
	t.Helper()
	resp, err := newClient().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s %s: %v", req.Method, req.URL, err)
	}
	return resp, body
}

// recordedBody is the body of the first (only) exchange of a fixture.
func recordedBody(t *testing.T, f nexusfixture.Fixture) []byte {
	t.Helper()
	if len(f.Exchanges) == 0 {
		t.Fatalf("%s has no exchange", f.Name)
	}
	return f.Exchanges[0].Body
}

func TestCredentials(t *testing.T) {
	cases := []struct {
		id   nexusfixture.Identity
		want string
	}{
		{nexusfixture.Admin, "admin"},
		{nexusfixture.Pull, "bench-pull"},
		{nexusfixture.Anonymous, ""},
	}
	for _, c := range cases {
		if got := fakenexus.User(c.id); got != c.want {
			t.Errorf("User(%q) = %q, want %q", c.id, got, c.want)
		}
	}
	if fakenexus.Password == "" {
		t.Error("Password is empty")
	}
}

func TestOneServerPerRecordedHost(t *testing.T) {
	s := fakenexus.New(t, all(t)...)
	a, b := s.URL(hostHTTP), s.URL(hostGroup)
	if a == b {
		t.Fatalf("both hosts share one server: %s", a)
	}
	for _, raw := range []string{a, b} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("URL %q: %v", raw, err)
		}
		if u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" {
			t.Errorf("URL %q is not a loopback http://127.0.0.1:port address", raw)
		}
		if u.Path != "" {
			t.Errorf("URL %q carries a path", raw)
		}
	}
	if s.URL(hostHTTP) != a {
		t.Error("URL is not stable between calls")
	}
}

func TestEachHostServesOnlyItsOwnFixtures(t *testing.T) {
	s := fakenexus.New(t, all(t)...)

	// The same path is recorded on both hosts with different answers.
	resp, _ := get(t, s.URL(hostHTTP), "/v2/_catalog", nexusfixture.Anonymous)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("HTTP port /v2/_catalog status = %d, want 404 (recorded: path routing, no shared catalog)", resp.StatusCode)
	}
	resp, _ = get(t, s.URL(hostGroup), "/v2/_catalog", nexusfixture.Anonymous)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("group connector /v2/_catalog status = %d, want 200", resp.StatusCode)
	}

	// A path recorded on the HTTP port only is unknown on the connector.
	rec := &recorder{TB: t}
	s2 := fakenexus.New(rec, all(t)...)
	resp, _ = get(t, s2.URL(hostGroup), groupREST, nexusfixture.Admin)
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("REST path on the connector host: status = %d, want 501", resp.StatusCode)
	}
	if len(rec.failures()) != 1 {
		t.Errorf("REST path on the connector host: %d test errors, want 1: %q", len(rec.failures()), rec.failures())
	}
}

func TestIdentityPicksTheAnswer(t *testing.T) {
	byName := loadAll(t)
	s := fakenexus.New(t, all(t)...)

	cases := []struct {
		id      nexusfixture.Identity
		fixture string
		status  int
	}{
		{nexusfixture.Admin, "rest-repositories-group-docker-group--admin", http.StatusOK},
		{nexusfixture.Pull, "rest-repositories-group-docker-group--pull", http.StatusNotFound},
		{nexusfixture.Anonymous, "rest-repositories-group-docker-group--anon", http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(string(c.id), func(t *testing.T) {
			resp, body := get(t, s.URL(hostHTTP), groupREST, c.id)
			if resp.StatusCode != c.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, c.status)
			}
			if want := byName[c.fixture].Exchanges[0]; want.Stub == "" && !bytes.Equal(body, want.Body) {
				t.Errorf("body differs from the recorded one:\n got %q\nwant %q", body, want.Body)
			}
			if got := s.Hits(c.fixture); got != 1 {
				t.Errorf("Hits(%s) = %d, want 1", c.fixture, got)
			}
		})
	}

	// Only the admin answer lists the members.
	_, admin := get(t, s.URL(hostHTTP), groupREST, nexusfixture.Admin)
	if !bytes.Contains(admin, []byte(`"memberNames"`)) {
		t.Errorf("admin answer has no memberNames: %s", admin)
	}
	_, pull := get(t, s.URL(hostHTTP), groupREST, nexusfixture.Pull)
	if bytes.Contains(pull, []byte(`"memberNames"`)) {
		t.Errorf("pull answer lists members: %s", pull)
	}
}

func TestAnIdentityWithoutItsOwnFixtureIsNotServedAnother(t *testing.T) {
	// Only the admin answer is recorded here: neither anonymous nor the pull
	// account may be handed it.
	for _, id := range []nexusfixture.Identity{nexusfixture.Anonymous, nexusfixture.Pull} {
		t.Run(string(id), func(t *testing.T) {
			rec := &recorder{TB: t}
			s := fakenexus.New(rec, pick(t, "rest-repositories-group-docker-group--admin")...)
			resp, _ := get(t, s.URL(hostHTTP), groupREST, id)
			if resp.StatusCode != http.StatusNotImplemented {
				t.Errorf("status = %d, want 501", resp.StatusCode)
			}
			if n := len(rec.failures()); n != 1 {
				t.Errorf("%d test errors, want 1: %q", n, rec.failures())
			}
		})
	}
}

func TestUnrecordedRequestFailsTheTest(t *testing.T) {
	type req struct {
		name    string
		method  string
		target  string
		mutate  func(*http.Request)
		wantErr []string // substrings the error must carry
	}
	cases := []req{
		{
			name:    "wrong password",
			method:  http.MethodGet,
			target:  groupREST,
			mutate:  func(r *http.Request) { r.SetBasicAuth("admin", "not-the-password") },
			wantErr: []string{"no recorded answer"},
		},
		{
			name:    "unknown user",
			method:  http.MethodGet,
			target:  groupREST,
			mutate:  func(r *http.Request) { r.SetBasicAuth("mallory", fakenexus.Password) },
			wantErr: []string{"no recorded answer"},
		},
		{
			name:    "bearer",
			method:  http.MethodGet,
			target:  groupREST,
			mutate:  func(r *http.Request) { r.Header.Set("Authorization", "Bearer abc") },
			wantErr: []string{"no recorded answer"},
		},
		{
			name:    "unknown path",
			method:  http.MethodGet,
			target:  "/service/rest/v1/repositories/docker/group/nope",
			mutate:  func(r *http.Request) { r.SetBasicAuth(fakenexus.User(nexusfixture.Admin), fakenexus.Password) },
			wantErr: []string{"no recorded answer", "/service/rest/v1/repositories/docker/group/nope"},
		},
		{
			name:    "unknown method",
			method:  http.MethodPost,
			target:  groupREST,
			mutate:  func(r *http.Request) { r.SetBasicAuth(fakenexus.User(nexusfixture.Admin), fakenexus.Password) },
			wantErr: []string{"no recorded answer", http.MethodPost},
		},
		{
			name:    "different query",
			method:  http.MethodGet,
			target:  groupREST + "?x=1",
			mutate:  func(r *http.Request) { r.SetBasicAuth(fakenexus.User(nexusfixture.Admin), fakenexus.Password) },
			wantErr: []string{"no recorded answer"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &recorder{TB: t}
			s := fakenexus.New(rec, all(t)...)
			req, err := http.NewRequest(c.method, s.URL(hostHTTP)+c.target, nil)
			if err != nil {
				t.Fatal(err)
			}
			c.mutate(req)
			resp, body := do(t, req)
			if resp.StatusCode != http.StatusNotImplemented {
				t.Errorf("status = %d, want 501", resp.StatusCode)
			}
			if bytes.Contains(body, []byte("memberNames")) {
				t.Errorf("an unrecorded request got a recorded body: %s", body)
			}
			errs := rec.failures()
			if len(errs) != 1 {
				t.Fatalf("%d test errors, want exactly 1: %q", len(errs), errs)
			}
			for _, want := range c.wantErr {
				if !strings.Contains(errs[0], want) {
					t.Errorf("error %q does not mention %q", errs[0], want)
				}
			}
		})
	}
}

func TestAMatchedRequestReportsNoError(t *testing.T) {
	rec := &recorder{TB: t}
	s := fakenexus.New(rec, all(t)...)
	for _, id := range []nexusfixture.Identity{nexusfixture.Admin, nexusfixture.Pull, nexusfixture.Anonymous} {
		get(t, s.URL(hostGroup), "/v2/_catalog", id)
	}
	if errs := rec.failures(); len(errs) != 0 {
		t.Errorf("recorded requests reported errors: %q", errs)
	}
}

func TestCatalogTolerance(t *testing.T) {
	cases := []struct {
		name    string
		host    string
		target  string
		id      nexusfixture.Identity
		fixture string // the one that answers; "" = unmatched
	}{
		{"anon n=1000 reuses the no-n anon fixture", hostGroup, "/v2/_catalog?n=1000", nexusfixture.Anonymous, "catalog-group-connector-30482--anon"},
		{"pull n=1000 reuses the no-n pull fixture", hostGroup, "/v2/_catalog?n=1000", nexusfixture.Pull, "catalog-group-connector-30482--pull"},
		{"admin n=1000 prefers its exact fixture", hostGroup, "/v2/_catalog?n=1000", nexusfixture.Admin, "page-group-n1000--admin"},
		{"hosted n=1000 through its path", hostHTTP, "/repository/docker-hosted/v2/_catalog?n=1000", nexusfixture.Admin, "catalog-hosted-repository--admin"},
		{"proxy n=1000 through its path, anonymous", hostHTTP, "/repository/docker-proxy/v2/_catalog?n=1000", nexusfixture.Anonymous, "catalog-proxy-repository--anon"},
		{"n smaller than the list (6 names, n=2)", hostGroup, "/v2/_catalog?n=2", nexusfixture.Anonymous, ""},
		// Only n=1000 (what DevDesk asks, and the only value measured to answer
		// like no n) is tolerated: any other n is an inference nothing backs.
		{"n equal to a short list is not measured (n=6)", hostGroup, "/v2/_catalog?n=6", nexusfixture.Anonymous, ""},
		{"n above the list is not measured (n=100)", hostGroup, "/v2/_catalog?n=100", nexusfixture.Anonymous, ""},
		{"n above the measured one (n=1001)", hostGroup, "/v2/_catalog?n=1001", nexusfixture.Anonymous, ""},
		{"n=100 on hosted", hostHTTP, "/repository/docker-hosted/v2/_catalog?n=100", nexusfixture.Admin, ""},
		{"n=100 on proxy", hostHTTP, "/repository/docker-proxy/v2/_catalog?n=100", nexusfixture.Anonymous, ""},
		{"n one below the list (6 names, n=5)", hostGroup, "/v2/_catalog?n=5", nexusfixture.Anonymous, ""},
		{"last is a page walk, not tolerated", hostGroup, "/v2/_catalog?n=1000&last=api", nexusfixture.Anonymous, ""},
		{"last alone", hostGroup, "/v2/_catalog?last=api", nexusfixture.Anonymous, ""},
		{"n is not a number", hostGroup, "/v2/_catalog?n=many", nexusfixture.Anonymous, ""},
		{"n is empty", hostGroup, "/v2/_catalog?n=", nexusfixture.Anonymous, ""},
		{"an unrelated parameter", hostGroup, "/v2/_catalog?n=1000&x=1", nexusfixture.Anonymous, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &recorder{TB: t}
			s := fakenexus.New(rec, all(t)...)
			resp, body := get(t, s.URL(c.host), c.target, c.id)
			if c.fixture == "" {
				if resp.StatusCode != http.StatusNotImplemented {
					t.Errorf("status = %d, want 501 (unmatched)", resp.StatusCode)
				}
				if n := len(rec.failures()); n != 1 {
					t.Errorf("%d test errors, want 1: %q", n, rec.failures())
				}
				return
			}
			if errs := rec.failures(); len(errs) != 0 {
				t.Fatalf("unexpected test errors: %q", errs)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			want := recordedBody(t, loadAll(t)[c.fixture])
			if !bytes.Equal(body, want) {
				t.Errorf("body = %q, want %q", body, want)
			}
			if got := s.Hits(c.fixture); got != 1 {
				t.Errorf("Hits(%s) = %d, want 1", c.fixture, got)
			}
			if resp.Header.Get("Link") != "" {
				t.Errorf("a tolerated answer carries a Link: %q", resp.Header.Get("Link"))
			}
		})
	}
}

// A recorded 404 is never tolerated: with the exact "?n=1000" fixtures of the
// root and of the group path left out, "?n=1000" on those targets is unmatched
// (501 and a test error), although the no-n 404 is recorded.
func TestCatalogToleranceDoesNotServeA404(t *testing.T) {
	for _, target := range []string{"/v2/_catalog?n=1000", "/v2/docker-group/_catalog?n=1000"} {
		t.Run(target, func(t *testing.T) {
			rec := &recorder{TB: t}
			s := fakenexus.New(rec, without(t, "-n1000--")...)
			resp, _ := get(t, s.URL(hostHTTP), target, nexusfixture.Anonymous)
			if resp.StatusCode != http.StatusNotImplemented {
				t.Errorf("status = %d, want 501 (unmatched)", resp.StatusCode)
			}
			if n := len(rec.failures()); n != 1 {
				t.Errorf("%d test errors, want 1: %q", n, rec.failures())
			}
		})
	}
}

// The tolerance rests on measured facts: asking the bench for n=1000 and not
// asking for n answer the same, on the group connector (page-group-n1000--admin)
// and, since DEVDESK-580, on the hosted and proxy catalogs by path and on the 404
// of the root and of the group path (catalog-*-n1000--*), for the three identities.
func TestTheCatalogToleranceHasMeasuredGround(t *testing.T) {
	byName := loadAll(t)
	type pair struct{ paged, plain, pagedTarget, plainTarget string }
	pairs := []pair{{"page-group-n1000--admin", "catalog-group-connector-30482--admin", "/v2/_catalog?n=1000", "/v2/_catalog"}}
	for _, id := range []string{"admin", "pull", "anon"} {
		for _, m := range []string{"hosted", "proxy", "group"} {
			path := "/v2/docker-" + m + "/_catalog"
			pairs = append(pairs, pair{"catalog-" + m + "-path-n1000--" + id, "catalog-" + m + "-path--" + id, path + "?n=1000", path})
		}
		pairs = append(pairs, pair{"catalog-hosted-root-30481-n1000--" + id, "catalog-hosted-root-30481--" + id, "/v2/_catalog?n=1000", "/v2/_catalog"})
	}
	for _, c := range pairs {
		t.Run(c.paged, func(t *testing.T) {
			paged, plain := byName[c.paged], byName[c.plain]
			if len(paged.Exchanges) != 1 || len(plain.Exchanges) != 1 {
				t.Fatalf("want one exchange each, got %d and %d", len(paged.Exchanges), len(plain.Exchanges))
			}
			p, q := paged.Exchanges[0], plain.Exchanges[0]
			if p.Target != c.pagedTarget || q.Target != c.plainTarget {
				t.Fatalf("targets = %q and %q, not the n=1000 and no-n pair", p.Target, q.Target)
			}
			if p.Status != q.Status {
				t.Errorf("status %d != %d", p.Status, q.Status)
			}
			if !bytes.Equal(p.Body, q.Body) {
				t.Errorf("bodies differ:\n%s\n%s", p.Body, q.Body)
			}
			if v := p.Header.Get("Link"); v != "" {
				t.Errorf("n=1000 answer has a Link: %q", v)
			}
			if v := q.Header.Get("Link"); v != "" {
				t.Errorf("no-n answer has a Link: %q", v)
			}
		})
	}
}

// DEVDESK-579, hypothesis H-4, measured by DEVDESK-580. DevDesk asks
// "GET /v2/<repository>/_catalog?n=1000"; the catalog by path was first recorded
// without n, and the "?n=1000" answers are recorded now (catalog-*-path-n1000--*),
// so the real replay never reaches the tolerance on these paths. The tolerance
// code stays (the group connector's other identities rely on it); this pins it on
// hosted and proxy by leaving the exact fixtures out, and
// TestTheCatalogToleranceHasMeasuredGround pins that the measured answers agree.
func TestPathCatalogToleranceServesN1000FromTheNoNAnswer(t *testing.T) {
	ids := []nexusfixture.Identity{nexusfixture.Admin, nexusfixture.Pull, nexusfixture.Anonymous}
	for _, repo := range []string{"hosted", "proxy"} {
		for _, id := range ids {
			fixture := "catalog-" + repo + "-path--" + string(id)
			t.Run(fixture, func(t *testing.T) {
				rec := &recorder{TB: t}
				s := fakenexus.New(rec, without(t, "-path-n1000--")...)
				resp, body := get(t, s.URL(hostHTTP), "/v2/docker-"+repo+"/_catalog?n=1000", id)
				if errs := rec.failures(); len(errs) != 0 {
					t.Fatalf("unexpected test errors: %q", errs)
				}
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("status = %d, want 200", resp.StatusCode)
				}
				if want := recordedBody(t, loadAll(t)[fixture]); !bytes.Equal(body, want) {
					t.Errorf("body = %q, want %q", body, want)
				}
				if got := s.Hits(fixture); got != 1 {
					t.Errorf("Hits(%s) = %d, want 1", fixture, got)
				}
				if resp.Header.Get("Link") != "" {
					t.Errorf("a tolerated answer carries a Link: %q", resp.Header.Get("Link"))
				}
			})
		}
	}
}

// The group has no catalog by path: the recorded no-n answer is a 404 page, and
// so is the recorded "?n=1000" one (DEVDESK-580). Both are served as recorded; the
// tolerance itself serves only 200s (TestCatalogToleranceDoesNotServeA404).
func TestPathCatalogOfTheGroupIsA404WithAndWithoutN(t *testing.T) {
	for _, id := range []nexusfixture.Identity{nexusfixture.Admin, nexusfixture.Pull, nexusfixture.Anonymous} {
		fixture := "catalog-group-path--" + string(id)
		t.Run(fixture+" no n", func(t *testing.T) {
			rec := &recorder{TB: t}
			s := fakenexus.New(rec, all(t)...)
			resp, _ := get(t, s.URL(hostHTTP), "/v2/docker-group/_catalog", id)
			if errs := rec.failures(); len(errs) != 0 {
				t.Fatalf("unexpected test errors: %q", errs)
			}
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want the recorded 404", resp.StatusCode)
			}
			if got := s.Hits(fixture); got != 1 {
				t.Errorf("Hits(%s) = %d, want 1", fixture, got)
			}
		})
		t.Run(fixture+" n=1000", func(t *testing.T) {
			rec := &recorder{TB: t}
			s := fakenexus.New(rec, all(t)...)
			resp, _ := get(t, s.URL(hostHTTP), "/v2/docker-group/_catalog?n=1000", id)
			if errs := rec.failures(); len(errs) != 0 {
				t.Fatalf("unexpected test errors: %q", errs)
			}
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want the recorded 404", resp.StatusCode)
			}
			if got := s.Hits("catalog-group-path-n1000--" + string(id)); got != 1 {
				t.Errorf("Hits(catalog-group-path-n1000--%s) = %d, want 1", id, got)
			}
		})
	}
}

func TestWWWAuthenticateIsRewrittenToTheFakeServer(t *testing.T) {
	s := fakenexus.New(t, all(t)...)
	base := s.URL(hostGroup)
	resp, body := get(t, base, "/v2/", nexusfixture.Anonymous)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	want := `Bearer realm="` + base + `/v2/token",service="` + base + `/v2/token"`
	if got := resp.Header.Get("WWW-Authenticate"); got != want {
		t.Errorf("WWW-Authenticate = %q, want %q", got, want)
	}
	if strings.Contains(resp.Header.Get("WWW-Authenticate"), ":30482") {
		t.Error("the recorded port leaked into WWW-Authenticate")
	}
	// The body is not rewritten.
	if want := recordedBody(t, loadAll(t)["v2-ping-30482--anon"]); !bytes.Equal(body, want) {
		t.Errorf("body = %q, want %q", body, want)
	}
}

// synthetic builds an in-memory fixture (not a Nexus answer): it exercises the
// rewrite of headers no NX-03 file carries yet (Link, Location).
func synthetic(name, host, target string, status int, h http.Header, body string) nexusfixture.Fixture {
	return nexusfixture.Fixture{
		Campaign: "synthetic",
		Name:     name,
		Scenario: name,
		Identity: nexusfixture.Anonymous,
		Exchanges: []nexusfixture.Exchange{{
			Method: http.MethodGet,
			Target: target,
			Host:   host,
			Status: status,
			Header: h,
			Body:   []byte(body),
		}},
	}
}

func TestLinkAndLocationAreRewrittenToTheMatchingFakeServer(t *testing.T) {
	page := synthetic("page--anon", hostGroup, "/v2/_catalog?n=1&last=a", http.StatusOK, http.Header{
		"Link": {`<http://127.0.0.1:30482/v2/_catalog?last=b&n=1>; rel="next"`},
	}, `{"repositories":["b"]}`)
	redirect := synthetic("redirect--anon", hostGroup, "/v2/moved", http.StatusTemporaryRedirect, http.Header{
		"Location": {"http://127.0.0.1:30481/repository/docker-hosted/v2/"},
	}, "")
	other := synthetic("other--anon", hostHTTP, "/other", http.StatusOK, http.Header{
		"Content-Type": {"text/plain"},
	}, "x")

	s := fakenexus.New(t, page, redirect, other)
	group, httpPort := s.URL(hostGroup), s.URL(hostHTTP)

	resp, _ := get(t, group, "/v2/_catalog?n=1&last=a", nexusfixture.Anonymous)
	if want := `<` + group + `/v2/_catalog?last=b&n=1>; rel="next"`; resp.Header.Get("Link") != want {
		t.Errorf("Link = %q, want %q", resp.Header.Get("Link"), want)
	}

	resp, _ = get(t, group, "/v2/moved", nexusfixture.Anonymous)
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307", resp.StatusCode)
	}
	if want := httpPort + "/repository/docker-hosted/v2/"; resp.Header.Get("Location") != want {
		t.Errorf("Location = %q, want %q (the other recorded host's server)", resp.Header.Get("Location"), want)
	}
}

func TestStatusHeadersAndBodyAreReplayedVerbatim(t *testing.T) {
	byName := loadAll(t)
	s := fakenexus.New(t, all(t)...)

	t.Run("verbatim JSON body", func(t *testing.T) {
		resp, body := get(t, s.URL(hostGroup), "/v2/_catalog", nexusfixture.Pull)
		want := byName["catalog-group-connector-30482--pull"].Exchanges[0]
		if resp.StatusCode != want.Status {
			t.Errorf("status = %d, want %d", resp.StatusCode, want.Status)
		}
		if !bytes.Equal(body, want.Body) {
			t.Errorf("body not verbatim:\n got %q\nwant %q", body, want.Body)
		}
		for _, key := range []string{"Server", "Content-Type", "Docker-Distribution-Api-Version"} {
			if got, w := resp.Header.Get(key), want.Header.Get(key); got != w || w == "" {
				t.Errorf("%s = %q, want %q", key, got, w)
			}
		}
		if resp.ContentLength != int64(len(body)) {
			t.Errorf("Content-Length = %d, body is %d bytes", resp.ContentLength, len(body))
		}
		if resp.ContentLength != 95 {
			t.Errorf("Content-Length = %d, want the recorded 95", resp.ContentLength)
		}
	})

	t.Run("multi-line REST body keeps its bytes", func(t *testing.T) {
		_, body := get(t, s.URL(hostHTTP), groupREST, nexusfixture.Admin)
		want := byName["rest-repositories-group-docker-group--admin"].Exchanges[0].Body
		if !bytes.Equal(body, want) {
			t.Errorf("body not verbatim:\n got %q\nwant %q", body, want)
		}
		// The recorded host inside a body is data, not a URL to rewrite.
		if !bytes.Contains(body, []byte("http://127.0.0.1:30481/repository/docker-group")) {
			t.Errorf("the body's own URL was rewritten: %s", body)
		}
		if got := respOf(t, s.URL(hostHTTP), groupREST, nexusfixture.Admin).Header.Get("Content-Length"); got != "" && got != strconv.Itoa(len(want)) {
			t.Errorf("Content-Length = %s, body is %d bytes", got, len(want))
		}
	})

	t.Run("a stubbed HTML body is minimal and its length coherent", func(t *testing.T) {
		resp, body := get(t, s.URL(hostHTTP), "/v2/_catalog", nexusfixture.Anonymous)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		if !bytes.Contains(body, []byte("<title>404 - Sonatype Nexus Repository</title>")) {
			t.Errorf("stub body lacks the recorded title: %q", body)
		}
		// The recorded 1309 bytes were summarised away: the header must not be copied.
		if resp.ContentLength != int64(len(body)) {
			t.Errorf("Content-Length = %d, body is %d bytes (the recorded 1309 must not be copied)", resp.ContentLength, len(body))
		}
		if got := resp.Header.Get("Content-Type"); got != "text/html;charset=utf-8" {
			t.Errorf("Content-Type = %q", got)
		}
	})

	t.Run("an empty stub is an empty body", func(t *testing.T) {
		resp, body := get(t, s.URL(hostHTTP), groupREST, nexusfixture.Anonymous)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
		if len(body) != 0 {
			t.Errorf("body = %q, want empty (not the literal stub text)", body)
		}
		if resp.ContentLength > 0 {
			t.Errorf("Content-Length = %d for an empty body", resp.ContentLength)
		}
	})
}

func respOf(t *testing.T, base, target string, id nexusfixture.Identity) *http.Response {
	t.Helper()
	r, _ := get(t, base, target, id)
	return r
}

func TestHitsCountsAnsweredRequestsPerFixture(t *testing.T) {
	s := fakenexus.New(t, pick(t,
		"catalog-group-connector-30482--anon",
		"catalog-group-connector-30482--admin",
		"v2-ping-30482--anon",
	)...)
	for _, name := range []string{"catalog-group-connector-30482--anon", "catalog-group-connector-30482--admin", "v2-ping-30482--anon"} {
		if got := s.Hits(name); got != 0 {
			t.Errorf("Hits(%s) before any request = %d, want 0", name, got)
		}
	}
	get(t, s.URL(hostGroup), "/v2/_catalog", nexusfixture.Anonymous)
	get(t, s.URL(hostGroup), "/v2/_catalog", nexusfixture.Anonymous)
	get(t, s.URL(hostGroup), "/v2/_catalog?n=1000", nexusfixture.Anonymous) // by tolerance: counts for the same fixture
	get(t, s.URL(hostGroup), "/v2/_catalog", nexusfixture.Admin)

	if got := s.Hits("catalog-group-connector-30482--anon"); got != 3 {
		t.Errorf("Hits(anon) = %d, want 3", got)
	}
	if got := s.Hits("catalog-group-connector-30482--admin"); got != 1 {
		t.Errorf("Hits(admin) = %d, want 1", got)
	}
	if got := s.Hits("v2-ping-30482--anon"); got != 0 {
		t.Errorf("Hits(ping) = %d, want 0", got)
	}
	if got := s.Hits("no-such-fixture"); got != 0 {
		t.Errorf("Hits(unknown name) = %d, want 0", got)
	}
}

func TestAnUnmatchedRequestCountsForNoFixture(t *testing.T) {
	rec := &recorder{TB: t}
	s := fakenexus.New(rec, pick(t, "catalog-group-connector-30482--anon")...)
	get(t, s.URL(hostGroup), "/v2/nothing", nexusfixture.Anonymous)
	if got := s.Hits("catalog-group-connector-30482--anon"); got != 0 {
		t.Errorf("Hits = %d, want 0", got)
	}
}

func TestConcurrentRequestsAreCounted(t *testing.T) {
	s := fakenexus.New(t, pick(t, "catalog-group-connector-30482--anon")...)
	const n = 20
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			req, err := http.NewRequest(http.MethodGet, s.URL(hostGroup)+"/v2/_catalog", nil)
			if err != nil {
				t.Error(err)
				return
			}
			r, err := newClient().Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
		})
	}
	wg.Wait()
	if got := s.Hits("catalog-group-connector-30482--anon"); got != n {
		t.Errorf("Hits = %d, want %d", got, n)
	}
}

// The test must not dial the closed address again: the kernel is free to hand
// that port to any other server (another test, another process), which would
// answer and fail this test for nothing (DEVDESK-35). It keeps a connection
// open instead: when the server is closed, the connection ends.
func TestServersAreClosedWithTheTest(t *testing.T) {
	var conn net.Conn
	t.Run("lifetime", func(t *testing.T) {
		s := fakenexus.New(t, pick(t, "catalog-group-connector-30482--anon")...)
		u, err := url.Parse(s.URL(hostGroup))
		if err != nil {
			t.Fatal(err)
		}
		conn, err = net.DialTimeout("tcp", u.Host, clientTimeout)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		t.Cleanup(func() { _ = conn.Close() }) // runs after the server's, the sub-test being over
		_ = conn.SetDeadline(time.Now().Add(clientTimeout))
		if _, err := io.WriteString(conn, "GET /v2/_catalog HTTP/1.1\r\nHost: "+u.Host+"\r\n\r\n"); err != nil {
			t.Fatalf("write: %v", err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d while the test runs", resp.StatusCode)
		}
	})
	// The sub-test is over, its Cleanup closed the server and with it the idle
	// keep-alive connection: reading ends instead of timing out.
	_ = conn.SetReadDeadline(time.Now().Add(clientTimeout))
	if n, err := conn.Read(make([]byte, 1)); err == nil {
		t.Errorf("the connection still delivers data after its test ended (%d byte)", n)
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Errorf("the server still holds its connection after its test ended: %v", err)
	}
}

// The realm of a Bearer challenge is rewritten to the fake server, but nothing
// answers there: /v2/token is not recorded (NX-03 recorded no token exchange).
// A client that follows the challenge is a test that needs a recording, so it
// fails with "no recorded answer", it is never given an invented token. Kept as
// is on purpose (NX-05 will record the Bearer flow).
func TestAFollowedBearerRealmFailsTheTest(t *testing.T) {
	cases := []struct {
		host, ping, realmPath string
	}{
		{hostGroup, "/v2/", "/v2/token"},
		{hostHTTP, "/v2/", "/repository/docker-hosted/v2/token"},
	}
	for _, c := range cases {
		t.Run(c.host, func(t *testing.T) {
			rec := &recorder{TB: t}
			s := fakenexus.New(rec, all(t)...)
			base := s.URL(c.host)
			resp, _ := get(t, base, c.ping, nexusfixture.Anonymous)
			challenge := resp.Header.Get("WWW-Authenticate")
			realm := strings.TrimPrefix(challenge, `Bearer realm="`)
			realm, _, _ = strings.Cut(realm, `"`)
			if realm != base+c.realmPath {
				t.Fatalf("realm = %q, want %q (challenge %q)", realm, base+c.realmPath, challenge)
			}
			if errs := rec.failures(); len(errs) != 0 {
				t.Fatalf("the ping alone reported errors: %q", errs)
			}

			tokenResp, _ := get(t, base, c.realmPath, nexusfixture.Anonymous)
			if tokenResp.StatusCode != http.StatusNotImplemented {
				t.Errorf("token endpoint status = %d, want 501 (no token is invented)", tokenResp.StatusCode)
			}
			errs := rec.failures()
			if len(errs) != 1 || !strings.Contains(errs[0], "no recorded answer") || !strings.Contains(errs[0], c.realmPath) {
				t.Errorf("errors = %q, want one \"no recorded answer\" naming %s", errs, c.realmPath)
			}
		})
	}
}

func TestNewTakesNoFixtureAtAll(t *testing.T) {
	rec := &recorder{TB: t}
	s := fakenexus.New(rec)
	if s == nil {
		t.Fatal("New returned nil")
	}
	if got := s.Hits("anything"); got != 0 {
		t.Errorf("Hits = %d, want 0", got)
	}
	if errs := rec.failures(); len(errs) != 0 {
		t.Errorf("an empty server reported %q", errs)
	}
}

// withStub is a synthetic fixture whose body was summarised away.
func withStub(name, stub string, h http.Header) nexusfixture.Fixture {
	f := synthetic(name, hostHTTP, "/stubbed", http.StatusOK, h, "")
	f.Exchanges[0].Body = nil
	f.Exchanges[0].Stub = stub
	return f
}

func joined(errs []string) string { return strings.Join(errs, "\n") }

// A stub says "a body was here", not what it was. Only two stubs have a faithful
// stand-in: "<empty body>" and an HTML summary that kept its title. Replaying
// anything else as an empty body would hand the code under test an answer that
// was never recorded, so the test fails and says which fixture.
func TestAnUnplayableStubFailsTheTest(t *testing.T) {
	cases := []struct {
		name string
		stub string
	}{
		{"not HTML", "<body omitted: 2048 bytes, application/octet-stream>"},
		{"HTML without a title", "<HTML body omitted: 1309 bytes>"},
		{"not a stub at all", "something else"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := withStub("stubbed--anon", c.stub, http.Header{"Content-Type": {"application/octet-stream"}})
			rec := &recorder{TB: t}
			s := fakenexus.New(rec, f)
			get(t, s.URL(hostHTTP), "/stubbed", nexusfixture.Anonymous)
			errs := rec.failures()
			if len(errs) == 0 {
				t.Fatalf("a stub %q was replayed as an empty body without a test error", c.stub)
			}
			if !strings.Contains(joined(errs), "stubbed--anon") {
				t.Errorf("errors %q do not name the fixture", errs)
			}
		})
	}
}

func TestTheStubsWithAFaithfulStandInReportNoError(t *testing.T) {
	t.Run("empty body", func(t *testing.T) {
		rec := &recorder{TB: t}
		s := fakenexus.New(rec, withStub("empty--anon", "<empty body>", http.Header{}))
		_, body := get(t, s.URL(hostHTTP), "/stubbed", nexusfixture.Anonymous)
		if len(body) != 0 {
			t.Errorf("body = %q, want empty", body)
		}
		if errs := rec.failures(); len(errs) != 0 {
			t.Errorf("errors = %q, want none", errs)
		}
	})
	t.Run("HTML with a title", func(t *testing.T) {
		rec := &recorder{TB: t}
		stub := "<HTML body omitted: 1309 bytes, <title>404 - Sonatype Nexus Repository</title>>"
		s := fakenexus.New(rec, withStub("html--anon", stub, http.Header{"Content-Type": {"text/html;charset=utf-8"}}))
		_, body := get(t, s.URL(hostHTTP), "/stubbed", nexusfixture.Anonymous)
		if !bytes.Contains(body, []byte("<title>404 - Sonatype Nexus Repository</title>")) {
			t.Errorf("body = %q, want the recorded title", body)
		}
		if errs := rec.failures(); len(errs) != 0 {
			t.Errorf("errors = %q, want none", errs)
		}
	})
}

// A recording without a Content-Type has none. Go's server would otherwise
// sniff one from the body ("text/plain; charset=utf-8", "text/html"...), a
// header Nexus never sent.
func TestNoContentTypeIsInvented(t *testing.T) {
	for name, body := range map[string]string{
		"json":  `{"repositories":["api"]}`,
		"html":  "<html><head><title>x</title></head></html>",
		"plain": "hello",
	} {
		t.Run(name, func(t *testing.T) {
			f := synthetic("untyped--anon", hostHTTP, "/untyped", http.StatusOK, http.Header{"Server": {"Nexus/3.98.1-01 (COMMUNITY)"}}, body)
			s := fakenexus.New(t, f)
			resp, got := get(t, s.URL(hostHTTP), "/untyped", nexusfixture.Anonymous)
			if string(got) != body {
				t.Fatalf("body = %q, want %q", got, body)
			}
			if v, ok := resp.Header["Content-Type"]; ok {
				t.Errorf("Content-Type = %q, the fixture has none", v)
			}
		})
	}
}

// New refuses what cannot be replayed faithfully, at once and not at the first
// request that happens to reach it.
func TestNewRefusesAnExchangeWithoutAStatus(t *testing.T) {
	f := synthetic("nostatus--anon", hostHTTP, "/x", 0, http.Header{}, "x")
	rec := &recorder{TB: t}
	fakenexus.New(rec, f)
	errs := rec.failures()
	if len(errs) == 0 {
		t.Fatal("an exchange with Status 0 was accepted")
	}
	if !strings.Contains(joined(errs), "nostatus--anon") {
		t.Errorf("errors %q do not name the fixture", errs)
	}
}

func TestNewRefusesDuplicateExchanges(t *testing.T) {
	mk := func(name string, mutate func(*nexusfixture.Fixture)) nexusfixture.Fixture {
		f := synthetic(name, hostHTTP, "/dup", http.StatusOK, http.Header{}, "x")
		if mutate != nil {
			mutate(&f)
		}
		return f
	}
	twice := mk("twice--anon", func(f *nexusfixture.Fixture) { f.Exchanges = append(f.Exchanges, f.Exchanges[0]) })

	refused := []struct {
		name     string
		fixtures []nexusfixture.Fixture
		mention  []string
	}{
		{"same key in two fixtures", []nexusfixture.Fixture{mk("a--anon", nil), mk("b--anon", nil)}, []string{"a--anon", "b--anon"}},
		{"same key twice in one fixture", []nexusfixture.Fixture{twice}, []string{"twice--anon"}},
	}
	for _, c := range refused {
		t.Run(c.name, func(t *testing.T) {
			rec := &recorder{TB: t}
			fakenexus.New(rec, c.fixtures...)
			errs := rec.failures()
			if len(errs) == 0 {
				t.Fatal("duplicate exchanges were accepted")
			}
			for _, name := range c.mention {
				if !strings.Contains(joined(errs), name) {
					t.Errorf("errors %q do not name %s", errs, name)
				}
			}
		})
	}

	// What differs in the host, the method, the target or the identity is not a duplicate.
	distinct := []struct {
		name   string
		mutate func(*nexusfixture.Fixture)
	}{
		{"identity", func(f *nexusfixture.Fixture) { f.Identity = nexusfixture.Admin }},
		{"host", func(f *nexusfixture.Fixture) { f.Exchanges[0].Host = hostGroup }},
		{"method", func(f *nexusfixture.Fixture) { f.Exchanges[0].Method = http.MethodHead }},
		{"target", func(f *nexusfixture.Fixture) { f.Exchanges[0].Target = "/dup?n=1000" }},
	}
	for _, c := range distinct {
		t.Run("differs by "+c.name, func(t *testing.T) {
			rec := &recorder{TB: t}
			fakenexus.New(rec, mk("a--anon", nil), mk("b--other", c.mutate))
			if errs := rec.failures(); len(errs) != 0 {
				t.Errorf("distinct exchanges refused: %q", errs)
			}
		})
	}
}

// The embedded campaign is itself free of the above.
func TestTheEmbeddedCampaignIsAccepted(t *testing.T) {
	rec := &recorder{TB: t}
	fakenexus.New(rec, all(t)...)
	if errs := rec.failures(); len(errs) != 0 {
		t.Errorf("the NX-03 fixtures were refused: %q", errs)
	}
}
