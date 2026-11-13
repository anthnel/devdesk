package imagesearch

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// End-to-end cases of DEVDESK-306 (criteria 1 to 8, D134 point 7): the real
// CatalogSearcher over oci.ListCatalogContext against httptest, then Classify.

func catalogJSON(names ...string) string {
	b, _ := json.Marshal(map[string][]string{"repositories": names})
	return string(b)
}

func catalogOutcome(t *testing.T, s Searcher, tg Target, text string) ([]Image, Outcome, error) {
	t.Helper()
	imgs, partial, err := s.Search(context.Background(), tg, Query{Text: text})
	return imgs, Classify(err, len(imgs), partial), err
}

func names(imgs []Image) string {
	var out []string
	for _, im := range imgs {
		out = append(out, im.Repo)
	}
	return strings.Join(out, ",")
}

// Criterion 1: pages are chained, the filter runs client-side, verdict ok.
func TestCatalogHTTPChainedPages(t *testing.T) {
	var requests atomic.Int32
	var rawQueries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		rawQueries = append(rawQueries, r.URL.RawQuery)
		switch r.URL.Query().Get("last") {
		case "":
			w.Header().Set("Link", `</v2/_catalog?n=1000&last=a>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogJSON("nginx", "redis"))
		case "a":
			w.Header().Set("Link", `</v2/_catalog?n=1000&last=b>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogJSON("postgres", "bitnami/nginx"))
		default:
			_, _ = fmt.Fprint(w, catalogJSON("alpine"))
		}
	}))
	t.Cleanup(srv.Close)

	imgs, out, err := catalogOutcome(t, CatalogSearcher{}, Target{RegistryURL: srv.URL, APIURL: srv.URL}, "NGinx")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(imgs); got != "nginx,bitnami/nginx" {
		t.Errorf("rows = %s, want nginx,bitnami/nginx", got)
	}
	if requests.Load() != 3 {
		t.Errorf("requests = %d, want 3", requests.Load())
	}
	if out != (Outcome{Verdict: VerdictOK}) {
		t.Errorf("outcome = %+v, want ok", out)
	}
	for _, q := range rawQueries {
		if strings.Contains(strings.ToLower(q), "nginx") {
			t.Errorf("the query reached the registry: %q", q)
		}
	}
}

// Criterion 2: the entry's prefix is applied once.
func TestCatalogHTTPPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, catalogJSON("docker-hosted/api", "other/api"))
	}))
	t.Cleanup(srv.Close)
	imgs, out, err := catalogOutcome(t, CatalogSearcher{}, Target{APIURL: srv.URL, RepoPrefix: "docker-hosted"}, "api")
	if err != nil || out.Verdict != VerdictOK || len(imgs) != 1 {
		t.Fatalf("imgs = %+v out = %+v err = %v", imgs, out, err)
	}
	if imgs[0].Name != "api" || imgs[0].Repo != "docker-hosted/api" {
		t.Errorf("row = %+v, want Name api, Repo docker-hosted/api", imgs[0])
	}
}

// Criterion 3: an endless next chain stops at 10 pages and 100 rows.
func TestCatalogHTTPEndlessNext(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(requests.Add(1))
		page := make([]string, 1000)
		for i := range page {
			page[i] = fmt.Sprintf("nginx-%02d-%04d", n, i)
		}
		w.Header().Set("Link", fmt.Sprintf(`</v2/_catalog?n=1000&last=%s>; rel="next"`, page[len(page)-1]))
		_, _ = fmt.Fprint(w, catalogJSON(page...))
	}))
	t.Cleanup(srv.Close)

	imgs, out, err := catalogOutcome(t, CatalogSearcher{}, Target{APIURL: srv.URL}, "nginx")
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 10 {
		t.Errorf("requests = %d, want 10", requests.Load())
	}
	if len(imgs) != 100 {
		t.Errorf("rows = %d, want 100", len(imgs))
	}
	if out != (Outcome{Verdict: VerdictOK, Partial: true}) {
		t.Errorf("outcome = %+v, want ok + Partial", out)
	}
}

// Criterion 4: a Bearer challenge, with and without credentials.
func TestCatalogHTTPBearer(t *testing.T) {
	run := func(t *testing.T, user, pass string) (tokenAuth, firstAuth string, out Outcome, imgs []Image) {
		t.Helper()
		var tokAuth, first atomic.Value
		tokAuth.Store("")
		first.Store("")
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/token" {
				tokAuth.Store(r.Header.Get("Authorization"))
				_, _ = fmt.Fprint(w, `{"token":"tok123"}`)
				return
			}
			auth := r.Header.Get("Authorization")
			if first.Load() == "" {
				first.Store("seen:" + auth)
			}
			if auth != "Bearer tok123" {
				w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="x"`, srv.URL))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = fmt.Fprint(w, catalogJSON("nginx"))
		}))
		t.Cleanup(srv.Close)
		imgs, out, err := catalogOutcome(t, CatalogSearcher{}, Target{APIURL: srv.URL, Username: user, Password: pass}, "nginx")
		if err != nil {
			t.Fatal(err)
		}
		return tokAuth.Load().(string), first.Load().(string), out, imgs
	}

	t.Run("credentials go to the token endpoint only", func(t *testing.T) {
		tok, first, out, imgs := run(t, "u", "p")
		if want := "Basic " + base64.StdEncoding.EncodeToString([]byte("u:p")); tok != want {
			t.Errorf("token endpoint Authorization = %q, want %q", tok, want)
		}
		// internal/oci sends Basic to the registry itself first, gets the Bearer
		// challenge, then replays with the token (the pages carry Bearer, which
		// the handler enforces by only answering 200 to "Bearer tok123").
		if want := "seen:" + "Basic " + base64.StdEncoding.EncodeToString([]byte("u:p")); first != want {
			t.Errorf("first catalog GET = %q, want %q", first, want)
		}
		if out.Verdict != VerdictOK || len(imgs) != 1 {
			t.Errorf("out = %+v imgs = %v", out, imgs)
		}
	})
	t.Run("anonymous sends no Authorization", func(t *testing.T) {
		tok, first, out, _ := run(t, "", "")
		if tok != "" || first != "seen:" {
			t.Errorf("token auth = %q, first = %q, want none", tok, first)
		}
		if out.Verdict != VerdictOK {
			t.Errorf("out = %+v, want ok", out)
		}
	})
}

// Criterion 5: refusals, a missing prefix and an empty catalog.
func TestCatalogHTTPRefusedAndEmpty(t *testing.T) {
	status := func(code int) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
	}
	var tokenSrv *httptest.Server
	tokenSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token"`, tokenSrv.URL))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(tokenSrv.Close)

	cases := []struct {
		name    string
		handler http.Handler
		prefix  string
		want    Verdict
	}{
		{"401", status(401), "", VerdictRefused},
		{"401 after challenge", tokenSrv.Config.Handler, "", VerdictRefused},
		{"403", status(403), "", VerdictRefused},
		{"404", status(404), "", VerdictRefused},
		{"405", status(405), "", VerdictRefused},
		{"prefix absent", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, catalogJSON("other/nginx"))
		}), "docker-hosted", VerdictRefused},
		{"empty catalog", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"repositories":[]}`)
		}), "", VerdictNoResult},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			t.Cleanup(srv.Close)
			tg := Target{APIURL: srv.URL, RepoPrefix: tc.prefix}
			if tc.name == "401 after challenge" {
				tg.APIURL = tokenSrv.URL
			}
			imgs, out, err := catalogOutcome(t, CatalogSearcher{}, tg, "nginx")
			if len(imgs) != 0 {
				t.Errorf("rows = %v, want none", imgs)
			}
			if out.Verdict != tc.want || out.Partial || out.Skipped {
				t.Errorf("outcome = %+v, want %v with no mark (err = %v)", out, tc.want, err)
			}
		})
	}
}

// Criterion 6: a page that never answers fails the walk with no row kept.
func TestCatalogHTTPStalledSecondPage(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("last") == "" {
			w.Header().Set("Link", `</v2/_catalog?n=1000&last=a>; rel="next"`)
			_, _ = fmt.Fprint(w, catalogJSON("nginx"))
			return
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	imgs, partial, err := CatalogSearcher{}.Search(ctx, Target{APIURL: srv.URL}, Query{Text: "nginx"})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("returned after %v, want under 1s", elapsed)
	}
	if got := Classify(err, len(imgs), partial); got.Verdict != VerdictFailed {
		t.Errorf("verdict = %v, want failed (err = %v)", got.Verdict, err)
	}
	if len(imgs) != 0 {
		t.Errorf("rows = %v, want none", imgs)
	}
}

// Criterion 7: a one-rune query walks nothing.
func TestCatalogHTTPShortQuery(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = fmt.Fprint(w, catalogJSON("nginx"))
	}))
	t.Cleanup(srv.Close)
	imgs, out, _ := catalogOutcome(t, CatalogSearcher{}, Target{APIURL: srv.URL}, "n")
	if requests.Load() != 0 {
		t.Errorf("requests = %d, want 0", requests.Load())
	}
	if len(imgs) != 0 || out != (Outcome{Verdict: VerdictNoResult, Skipped: true}) {
		t.Errorf("imgs = %v out = %+v, want none, {no result, Skipped}", imgs, out)
	}
}

// Criterion 8: routing keeps Hub and the catalog apart, and dhi.io unqueried.
func TestCatalogHTTPRouting(t *testing.T) {
	var hubReqs, catReqs atomic.Int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hubReqs.Add(1)
		_, _ = fmt.Fprint(w, `{"results":[]}`)
	}))
	t.Cleanup(hub.Close)
	cat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		catReqs.Add(1)
		_, _ = fmt.Fprint(w, catalogJSON("nginx"))
	}))
	t.Cleanup(cat.Close)
	s := ByRegistry(HubSearcher{Endpoint: hub.URL}, CatalogSearcher{})

	t.Run("dhi.io is refused before any request", func(t *testing.T) {
		_, out, _ := catalogOutcome(t, s, Target{RegistryURL: "dhi.io", APIURL: cat.URL}, "nginx")
		if out.Verdict != VerdictRefused {
			t.Errorf("verdict = %v, want refused", out.Verdict)
		}
		if hubReqs.Load() != 0 || catReqs.Load() != 0 {
			t.Errorf("requests hub=%d catalog=%d, want 0 0", hubReqs.Load(), catReqs.Load())
		}
	})
	t.Run("one-rune query on a Hub target still asks Hub", func(t *testing.T) {
		_, _, _ = catalogOutcome(t, s, Target{RegistryURL: "docker.io"}, "n")
		if hubReqs.Load() != 1 || catReqs.Load() != 0 {
			t.Errorf("requests hub=%d catalog=%d, want 1 0", hubReqs.Load(), catReqs.Load())
		}
	})
}
