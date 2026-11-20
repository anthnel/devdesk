package imagesearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-579 (NX-12), step 2: the real CatalogSearcher over
// oci.ListCatalogUnderContext against httptest, then Classify. The wire is what
// is asserted: which paths are requested, and that the root /v2/_catalog is
// never one of them for a target read under its prefix (criterion 2).

// pathLog records the request target of every request a test server receives.
type pathLog struct {
	mu   sync.Mutex
	uris []string
}

func (l *pathLog) record(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.uris = append(l.uris, r.RequestURI)
}

func (l *pathLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.uris...)
}

// root counts the requests that reached the root catalog.
func (l *pathLog) root() int {
	n := 0
	for _, u := range l.all() {
		if p, _, _ := strings.Cut(u, "?"); p == "/v2/_catalog" {
			n++
		}
	}
	return n
}

// catalogServer serves one JSON catalog per path and answers a code per path
// that is listed in failures; any other path is a 501 so a stray request is seen.
func catalogServer(t *testing.T, catalogs map[string][]string, failures map[string]int) (*httptest.Server, *pathLog) {
	t.Helper()
	log := &pathLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		if code, ok := failures[r.URL.Path]; ok {
			w.WriteHeader(code)
			return
		}
		if names, ok := catalogs[r.URL.Path]; ok {
			_, _ = fmt.Fprint(w, catalogJSON(names...))
			return
		}
		w.WriteHeader(http.StatusNotImplemented)
	}))
	t.Cleanup(srv.Close)
	return srv, log
}

func underTarget(apiURL, prefix string) Target {
	return Target{EntryKey: "k-" + prefix, APIURL: apiURL, RepoPrefix: prefix, CatalogUnderPrefix: true}
}

func TestCatalogUnderPrefixHTTPReadsThePrefixedPath(t *testing.T) {
	srv, log := catalogServer(t, map[string][]string{
		"/v2/docker-hosted/_catalog": {"api", "shared/base", "team/api", "tools/cli"},
		"/v2/docker-proxy/_catalog":  {"mirror/cached", "mirror/uncached", "shared/base"},
		"/v2/a/b/_catalog":           {"api"},
	}, nil)

	cases := []struct {
		name, apiURL, prefix, text, uri, rows string
	}{
		{"hosted", srv.URL, "docker-hosted", "api", "/v2/docker-hosted/_catalog?n=1000", "docker-hosted/api,docker-hosted/team/api"},
		{"proxy", srv.URL, "docker-proxy", "mirror", "/v2/docker-proxy/_catalog?n=1000", "docker-proxy/mirror/cached,docker-proxy/mirror/uncached"},
		{"nested prefix", srv.URL, "a/b", "api", "/v2/a/b/_catalog?n=1000", "a/b/api"},
		{"API URL with a trailing slash", srv.URL + "/", "docker-hosted", "base", "/v2/docker-hosted/_catalog?n=1000", "docker-hosted/shared/base"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := len(log.all())
			imgs, out, err := catalogOutcome(t, CatalogSearcher{}, underTarget(tc.apiURL, tc.prefix), tc.text)
			if err != nil || out != (Outcome{Verdict: VerdictOK}) {
				t.Fatalf("outcome = %+v err = %v, want ok", out, err)
			}
			if got := names(imgs); got != tc.rows {
				t.Errorf("rows = %s, want %s", got, tc.rows)
			}
			if got := log.all()[before:]; !reflect.DeepEqual(got, []string{tc.uri}) {
				t.Errorf("requests = %v, want exactly %s", got, tc.uri)
			}
		})
	}
	if n := log.root(); n != 0 {
		t.Errorf("%d request(s) reached the root /v2/_catalog, want 0 (criterion 2)", n)
	}
}

// Pagination is the walk's: a Link under the prefix is followed, and the names of
// every page get the prefix back.
func TestCatalogUnderPrefixHTTPFollowsPages(t *testing.T) {
	log := &pathLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		switch {
		case r.URL.Path != "/v2/docker-hosted/_catalog":
			w.WriteHeader(http.StatusNotImplemented)
		case r.URL.Query().Get("last") == "":
			w.Header().Set("Link", `</v2/docker-hosted/_catalog?n=1000&last=api>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogJSON("api", "shared/base"))
		default:
			_, _ = fmt.Fprint(w, catalogJSON("team/api"))
		}
	}))
	t.Cleanup(srv.Close)

	imgs, out, err := catalogOutcome(t, CatalogSearcher{}, underTarget(srv.URL, "docker-hosted"), "api")
	if err != nil || out != (Outcome{Verdict: VerdictOK}) {
		t.Fatalf("outcome = %+v err = %v, want ok", out, err)
	}
	if got := names(imgs); got != "docker-hosted/api,docker-hosted/team/api" {
		t.Errorf("rows = %s", got)
	}
	if got := log.all(); len(got) != 2 || log.root() != 0 {
		t.Errorf("requests = %v, want 2 under the prefix and none on the root", got)
	}
}

// Criterion 6: a member that does not answer is refused or failed with a readable
// cause, and does not take the other members of the batch with it. The group of
// the HTTP port answers 404 (NX-03); a 401 or 403 is the account; a 5xx or a 429
// is a failure.
func TestCatalogUnderPrefixHTTPAnswersPerMember(t *testing.T) {
	srv, log := catalogServer(t,
		map[string][]string{
			"/v2/docker-hosted/_catalog": {"api", "team/api"},
			"/v2/docker-proxy/_catalog":  {"mirror/cached"},
		},
		map[string]int{
			"/v2/docker-group/_catalog":  404,
			"/v2/denied-401/_catalog":    401,
			"/v2/denied-403/_catalog":    403,
			"/v2/no-method/_catalog":     405,
			"/v2/broken-500/_catalog":    500,
			"/v2/broken-503/_catalog":    503,
			"/v2/throttled-429/_catalog": 429,
		})

	type want struct {
		prefix  string
		text    string
		verdict Verdict
		code    int // the StatusError code, 0 for none
		rows    string
	}
	wants := []want{
		{"docker-hosted", "api", VerdictOK, 0, "docker-hosted/api,docker-hosted/team/api"},
		{"docker-group", "api", VerdictRefused, 404, ""},
		{"denied-401", "api", VerdictRefused, 401, ""},
		{"docker-proxy", "mirror", VerdictOK, 0, "docker-proxy/mirror/cached"},
		{"denied-403", "api", VerdictRefused, 403, ""},
		{"no-method", "api", VerdictRefused, 405, ""},
		{"broken-500", "api", VerdictFailed, 500, ""},
		{"broken-503", "api", VerdictFailed, 503, ""},
		{"throttled-429", "api", VerdictFailed, 429, ""},
	}
	// One batch, as the browser runs it: a memo shared by every member.
	ctx := WithSharedWalks(context.Background())
	for _, w := range wants {
		t.Run(w.prefix, func(t *testing.T) {
			imgs, partial, err := CatalogSearcher{}.Search(ctx, underTarget(srv.URL, w.prefix), Query{Text: w.text})
			out := Classify(err, len(imgs), partial)
			if out.Verdict != w.verdict || out.Partial || out.Skipped {
				t.Fatalf("outcome = %+v, want %v with no mark (err = %v)", out, w.verdict, err)
			}
			if got := names(imgs); got != w.rows {
				t.Errorf("rows = %q, want %q", got, w.rows)
			}
			if w.code == 0 {
				return
			}
			var se *oci.StatusError
			if !errors.As(err, &se) || se.Code != w.code || se.Source != oci.SourceRegistry {
				t.Fatalf("err = %v (%T), want *oci.StatusError{%d, SourceRegistry}", err, err, w.code)
			}
			// A 401 with no challenge reads "unexpected auth challenge" (internal/oci),
			// not its status: the cause only has to be there.
			if strings.TrimSpace(err.Error()) == "" || (w.code != 401 && !strings.Contains(err.Error(), fmt.Sprint(w.code))) {
				t.Errorf("cause %q is not readable", err)
			}
		})
	}
	if n := log.root(); n != 0 {
		t.Errorf("%d request(s) reached the root /v2/_catalog, want 0 (criterion 2)", n)
	}
}

// An empty or unsafe prefix is refused before any request, with the verdict
// "unsupported or refused": nothing reaches the network, the root least of all.
func TestCatalogUnderPrefixHTTPUnsafePrefixSendsNothing(t *testing.T) {
	srv, log := catalogServer(t, map[string][]string{"/v2/_catalog": {"api"}}, nil)
	for _, prefix := range []string{"", "..", "a/../b", "a//b", "/a", "a/", ".", "my repo", "a%2e%2e", `a\b`, "a?b", "a#b", "a\x1b[31m", "a\x00b"} {
		t.Run(fmt.Sprintf("%q", prefix), func(t *testing.T) {
			imgs, out, err := catalogOutcome(t, CatalogSearcher{}, underTarget(srv.URL, prefix), "api")
			if len(imgs) != 0 || out != (Outcome{Verdict: VerdictRefused}) {
				t.Fatalf("imgs = %v out = %+v err = %v, want none, refused", imgs, out, err)
			}
			if err == nil || strings.TrimSpace(err.Error()) == "" {
				t.Errorf("err = %v, want a readable cause", err)
			}
		})
	}
	if got := log.all(); len(got) != 0 {
		t.Errorf("requests = %v, want none: the root must never be asked as a fallback", got)
	}
}

// A member that answered and holds nothing is "no result", and the prefix is not
// looked for in the names.
func TestCatalogUnderPrefixHTTPEmptyMember(t *testing.T) {
	srv, log := catalogServer(t, map[string][]string{"/v2/docker-hosted/_catalog": {}}, nil)
	_, out, err := catalogOutcome(t, CatalogSearcher{}, underTarget(srv.URL, "docker-hosted"), "api")
	if err != nil || out != (Outcome{Verdict: VerdictNoResult}) {
		t.Errorf("outcome = %+v err = %v, want no result", out, err)
	}
	if log.root() != 0 {
		t.Error("the root catalog was requested")
	}
}

// Members of one host, in one batch: one request each, and a second search of the
// same member reuses its walk (criterion 4).
func TestCatalogUnderPrefixHTTPOneWalkPerMember(t *testing.T) {
	srv, log := catalogServer(t, map[string][]string{
		"/v2/docker-hosted/_catalog": {"api", "shared/base"},
		"/v2/docker-proxy/_catalog":  {"mirror/cached", "shared/base"},
	}, nil)
	ctx := WithSharedWalks(context.Background())
	search := func(key, prefix, text string) string {
		tg := underTarget(srv.URL, prefix)
		tg.EntryKey = key
		imgs, _, err := CatalogSearcher{}.Search(ctx, tg, Query{Text: text})
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		return names(imgs)
	}
	if got := search("h1", "docker-hosted", "base"); got != "docker-hosted/shared/base" {
		t.Errorf("hosted rows = %q", got)
	}
	if got := search("p1", "docker-proxy", "base"); got != "docker-proxy/shared/base" {
		t.Errorf("proxy rows = %q", got)
	}
	if got := search("h2", "docker-hosted", "api"); got != "docker-hosted/api" {
		t.Errorf("hosted rows, second search = %q", got)
	}
	want := []string{"/v2/docker-hosted/_catalog?n=1000", "/v2/docker-proxy/_catalog?n=1000"}
	if got := log.all(); !reflect.DeepEqual(got, want) {
		t.Errorf("requests = %v, want %v", got, want)
	}
}

// Without CatalogUnderPrefix nothing changes on the wire: the root catalog, then
// R1 on what it lists.
func TestCatalogWithoutTheFlagStillAsksTheRoot(t *testing.T) {
	srv, log := catalogServer(t, map[string][]string{
		"/v2/_catalog":               {"docker-hosted/api", "other/api"},
		"/v2/docker-hosted/_catalog": {"must-not-be-asked"},
	}, nil)
	tg := Target{APIURL: srv.URL, RepoPrefix: "docker-hosted"}
	imgs, out, err := catalogOutcome(t, CatalogSearcher{}, tg, "api")
	if err != nil || out != (Outcome{Verdict: VerdictOK}) || names(imgs) != "docker-hosted/api" {
		t.Fatalf("imgs = %v out = %+v err = %v, want docker-hosted/api", names(imgs), out, err)
	}
	if got := log.all(); !reflect.DeepEqual(got, []string{"/v2/_catalog?n=1000"}) {
		t.Errorf("requests = %v, want the root catalog only", got)
	}
}
