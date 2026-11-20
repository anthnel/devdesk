package imagesearch

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-580 (NX-08b, PR3, NX-04): the real CatalogSearcher over
// oci.ListCatalogUnderContext walks the _catalog answers MEASURED on Nexus
// 3.98.1-01 with the upstream of docker-proxy reachable and then cut, replayed by
// fakenexus, then Classify. Every expectation below is what the fixture says, not
// a guess; a measurement that contradicts a criterion or the code is pinned as
// measured (see docs/measurements/2026-11-20-nexus-cached-only.md), never fixed here.
//
// What a fixture cannot replay is the DELAY: the first read of the proxy after the
// cut takes Nexus about 20 s to answer its 502 (cut1-), while the registry client
// gives up after 15 s. On the real bench DevDesk therefore sees a deadline (verdict
// failed, no StatusError), not the 502 replayed here. That timing and that verdict
// are reported by the session of 2026-11-20 (TestIntegrationDevDeskOnACutUpstream,
// internal/nexusbench, guarded, needs the bench), not replayable by a fixture. The
// 20 s itself (H-CUTDELAY) was measured on an uncached manifest read, not on
// _catalog. Both end in "failed", the cause differs.
//
// The three states of the proxy never share one fake: nominal-, cut1- and cut2-
// record the SAME request (GET /v2/docker-proxy/_catalog?n=1000) with different
// answers, and fakenexus refuses two recordings of one request.

// loadCachedOnly returns the fixtures of the cachedonly campaign by name.
func loadCachedOnly(t *testing.T) map[string]nexusfixture.Fixture {
	t.Helper()
	fixtures, err := nexusfixture.Load("cachedonly")
	if err != nil {
		t.Fatalf("Load(cachedonly): %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatal("the cachedonly campaign is empty")
	}
	by := make(map[string]nexusfixture.Fixture, len(fixtures))
	for _, f := range fixtures {
		by[f.Name] = f
	}
	return by
}

// cachedOnlyFixtures picks fixtures by name; a name that is not embedded fails the
// test (a removed fixture must turn it red, not make it vacuous).
func cachedOnlyFixtures(t *testing.T, names ...string) []nexusfixture.Fixture {
	t.Helper()
	by := loadCachedOnly(t)
	out := make([]nexusfixture.Fixture, 0, len(names))
	for _, n := range names {
		f, ok := by[n]
		if !ok {
			t.Fatalf("the cachedonly campaign has no fixture %q", n)
		}
		out = append(out, f)
	}
	return out
}

// wantAnswer pins what a fixture recorded: its request and its status. A fixture
// regenerated with another status fails here before any replay can hide it.
func wantAnswer(t *testing.T, f nexusfixture.Fixture, target string, status int) nexusfixture.Exchange {
	t.Helper()
	if len(f.Exchanges) != 1 {
		t.Fatalf("%s: %d exchanges, want 1", f.Name, len(f.Exchanges))
	}
	ex := f.Exchanges[0]
	if ex.Target != target || ex.Status != status {
		t.Fatalf("%s recorded %s %s -> %d, this test was written for %s -> %d: the measurement changed",
			f.Name, ex.Method, ex.Target, ex.Status, target, status)
	}
	return ex
}

const (
	proxyCatalogTarget = "/v2/docker-proxy/_catalog?n=1000"
	// blockedMessage starts the body of every answer of a proxy that Nexus
	// auto-blocked; what follows is the date the block ends, which changes at every
	// recording and which no test reads.
	blockedMessage = "Remote Auto Blocked until "
)

func TestProxyCatalogOnMeasuredCutUpstream(t *testing.T) {
	// A Nexus group member read by its path, as discovery stores it: the API URL of
	// the HTTP port, the prefix docker-proxy, the catalog read under it.
	type probe struct {
		fixture string // the one fixture the fake replays
		id      nexusfixture.Identity
		status  int // pinned: what the bench answered
		query   string
		want    Verdict
		rows    []string
		code    int // the StatusError Code expected when the verdict comes from an error
	}
	proxy := []probe{
		// Control: the upstream reachable. The catalog of a proxy lists what its
		// upstream holds, mirror/uncached included (it relays _catalog).
		{"nominal-catalog-proxy-path--admin", nexusfixture.Admin, 200, "mirror", VerdictOK, []string{"docker-proxy/mirror/cached", "docker-proxy/mirror/uncached"}, 0},
		{"nominal-catalog-proxy-path--pull", nexusfixture.Pull, 200, "mirror", VerdictOK, []string{"docker-proxy/mirror/cached", "docker-proxy/mirror/uncached"}, 0},
		{"nominal-catalog-proxy-path--anon", nexusfixture.Anonymous, 200, "mirror", VerdictOK, []string{"docker-proxy/mirror/cached", "docker-proxy/mirror/uncached"}, 0},
		// The first read after the cut: Nexus relays the failure of its upstream as a
		// 502. Not refused (a 5xx is a failure), whatever the identity would be.
		{"cut1-catalog-proxy-path--admin", nexusfixture.Admin, 502, "mirror", VerdictFailed, nil, 502},
		// Once Nexus has auto-blocked the proxy: a 404 whose body says "Remote Auto
		// Blocked". DevDesk reads no body, a 404 is a refusal: "unsupported or
		// refused", although the proxy is blocked and holds mirror/cached.
		{"cut2-catalog-proxy-path--admin", nexusfixture.Admin, 404, "mirror", VerdictRefused, nil, 404},
		{"cut2-catalog-proxy-path--pull", nexusfixture.Pull, 404, "mirror", VerdictRefused, nil, 404},
		{"cut2-catalog-proxy-path--anon", nexusfixture.Anonymous, 404, "mirror", VerdictRefused, nil, 404},
	}
	for _, p := range proxy {
		t.Run(p.fixture, func(t *testing.T) {
			fixtures := cachedOnlyFixtures(t, p.fixture)
			wantAnswer(t, fixtures[0], proxyCatalogTarget, p.status)
			s := fakenexus.New(t, fixtures...)
			tg := targetFor(s.URL(nexusHTTPPort), "docker-proxy", p.id)
			tg.CatalogUnderPrefix = true

			imgs, partial, err := CatalogSearcher{}.Search(context.Background(), tg, Query{Text: p.query})
			out := Classify(err, len(imgs), partial)

			if out != (Outcome{Verdict: p.want}) {
				t.Fatalf("outcome = %+v, want verdict %v alone (err = %v)", out, p.want, err)
			}
			var got []string
			for _, im := range imgs {
				got = append(got, im.Repo)
			}
			if !reflect.DeepEqual(got, p.rows) {
				t.Errorf("rows = %v, want %v", got, p.rows)
			}
			if p.code == 0 {
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
			} else {
				var se *oci.StatusError
				if !errors.As(err, &se) || se.Code != p.code || se.Source != oci.SourceRegistry {
					t.Errorf("err = %v (%T), want *oci.StatusError{%d, SourceRegistry}", err, err, p.code)
				}
			}
			if h := s.Hits(p.fixture); h != 1 {
				t.Errorf("Hits(%s) = %d, want 1", p.fixture, h)
			}
			if n := totalHits(s, fixtures); n != 1 {
				t.Errorf("%d requests answered in total, want 1", n)
			}
		})
	}

	// The 404 of a blocked proxy is Nexus's own message, not an unknown route; the
	// date inside it is not read. Matching the prefix keeps these tests valid when a
	// regeneration (nexus-bench record) writes a new date.
	for _, name := range []string{"cut2-catalog-proxy-path--admin", "cut2-catalog-proxy-path--pull", "cut2-catalog-proxy-path--anon",
		"cut2-tags-proxy-mirror-cached--admin", "cut2-tags-proxy-mirror-uncached--admin"} {
		t.Run("blocked message of "+name, func(t *testing.T) {
			ex := cachedOnlyFixtures(t, name)[0].Exchanges[0]
			if ex.Status != 404 {
				t.Fatalf("%s: status %d, want 404", name, ex.Status)
			}
			var body struct {
				Errors []struct {
					Message string `json:"message"`
				} `json:"errors"`
			}
			if err := json.Unmarshal(ex.Body, &body); err != nil || len(body.Errors) != 1 {
				t.Fatalf("%s: body %q is not one Docker error (%v)", name, ex.Body, err)
			}
			date, ok := strings.CutPrefix(body.Errors[0].Message, blockedMessage)
			if !ok {
				t.Fatalf("%s: message %q does not start with %q", name, body.Errors[0].Message, blockedMessage)
			}
			if _, err := time.Parse(time.RFC3339, date); err != nil {
				t.Errorf("%s: %q is not a date: %v", name, date, err)
			}
		})
	}
}

// The hosted member and the group connector, while the proxy is blocked. The
// hosted catalog is intact (control). The group's catalog SILENTLY omits the
// proxy's names: no error, no marker, a 200 with the hosted names only. Read by
// DevDesk as an ordinary answer, a search for "mirror" is "no result" there,
// while the cache holds mirror/cached (see TestManifestDigestOnMeasuredCutUpstream).
func TestHostedAndGroupCatalogOnMeasuredCutUpstream(t *testing.T) {
	t.Run("hosted by path is unaffected", func(t *testing.T) {
		fixtures := cachedOnlyFixtures(t, "cut2-catalog-hosted-path--admin")
		wantAnswer(t, fixtures[0], "/v2/docker-hosted/_catalog?n=1000", 200)
		s := fakenexus.New(t, fixtures...)
		tg := targetFor(s.URL(nexusHTTPPort), "docker-hosted", nexusfixture.Admin)
		tg.CatalogUnderPrefix = true

		imgs, partial, err := CatalogSearcher{}.Search(context.Background(), tg, Query{Text: "api"})
		if out := Classify(err, len(imgs), partial); out != (Outcome{Verdict: VerdictOK}) {
			t.Fatalf("outcome = %+v err = %v, want ok", out, err)
		}
		if got := names(imgs); got != "docker-hosted/api,docker-hosted/team/api" {
			t.Errorf("rows = %q", got)
		}
	})

	// The group connector read from its root, as a group nobody discovered members
	// for is (the entry of its own). Two fakes: the nominal answer is in the catalog
	// campaign and records the same request as the cut one.
	group := []struct {
		name     string
		campaign string
		fixture  string
		query    string
		want     Verdict
		rows     string
	}{
		{"nominal, mirror", "catalog", "page-group-n1000--admin", "mirror", VerdictOK, "mirror/cached,mirror/uncached"},
		{"proxy cut, api", "cachedonly", "cut2-catalog-group-connector-30482--admin", "api", VerdictOK, "api,team/api"},
		{"proxy cut, mirror", "cachedonly", "cut2-catalog-group-connector-30482--admin", "mirror", VerdictNoResult, ""},
		{"proxy cut, base", "cachedonly", "cut2-catalog-group-connector-30482--admin", "base", VerdictOK, "shared/base"},
	}
	for _, g := range group {
		t.Run("group connector, "+g.name, func(t *testing.T) {
			all, err := nexusfixture.Load(g.campaign)
			if err != nil {
				t.Fatal(err)
			}
			var fixtures []nexusfixture.Fixture
			for _, f := range all {
				if f.Name == g.fixture {
					fixtures = append(fixtures, f)
				}
			}
			if len(fixtures) != 1 {
				t.Fatalf("campaign %s has no fixture %s", g.campaign, g.fixture)
			}
			wantAnswer(t, fixtures[0], "/v2/_catalog?n=1000", 200)
			s := fakenexus.New(t, fixtures...)
			tg := targetFor(s.URL(nexusGroupPort), "", nexusfixture.Admin)

			imgs, partial, err := CatalogSearcher{}.Search(context.Background(), tg, Query{Text: g.query})
			if out := Classify(err, len(imgs), partial); out != (Outcome{Verdict: g.want}) {
				t.Fatalf("outcome = %+v err = %v, want %v", out, err, g.want)
			}
			if got := names(imgs); got != g.rows {
				t.Errorf("rows = %q, want %q", got, g.rows)
			}
		})
	}
}

// Criterion 2 of NX-08b: an exchange that makes the request fail is replayed and
// the verdict is "failed", with a typed cause. The measured failure is the 502 of
// the first read after the cut (cut1-catalog-proxy-path--admin): Classify keeps it
// out of "refused" (only 401, 403, 404 and 405 are refusals), and the cause is
// the registry's own *oci.StatusError, with the Code and the Source Nexus gave.
//
// Limit: this is the 502 as Nexus sent it after about 20 s. DevDesk's client gives
// up after 15 s, so against the live bench the same read is reported (session of
// 2026-11-20, integration test of internal/nexusbench) to end in a deadline
// (failed, no StatusError); not replayable by a fixture.
func TestFailedNexusAnswerKeepsItsCause(t *testing.T) {
	fixtures := cachedOnlyFixtures(t, "cut1-catalog-proxy-path--admin")
	ex := wantAnswer(t, fixtures[0], proxyCatalogTarget, 502)
	if got := ex.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type of the 502 = %q: the measurement changed", got)
	}

	s := fakenexus.New(t, fixtures...)
	tg := targetFor(s.URL(nexusHTTPPort), "docker-proxy", nexusfixture.Admin)
	tg.CatalogUnderPrefix = true

	imgs, partial, err := CatalogSearcher{}.Search(context.Background(), tg, Query{Text: "mirror"})
	if len(imgs) != 0 || partial {
		t.Errorf("rows = %v partial = %v, want none", imgs, partial)
	}
	out := Classify(err, len(imgs), partial)
	if out.Verdict != VerdictFailed {
		t.Fatalf("verdict = %v, want failed (err = %v)", out.Verdict, err)
	}
	if out.Partial || out.Skipped {
		t.Errorf("marks = %+v, want none", out)
	}

	var se *oci.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v (%T), want it to wrap a *oci.StatusError", err, err)
	}
	if se.Code != 502 || se.Code != ex.Status {
		t.Errorf("Code = %d, want 502 (the fixture says %d)", se.Code, ex.Status)
	}
	if se.Source != oci.SourceRegistry {
		t.Errorf("Source = %v, want SourceRegistry (the answer came from the registry endpoint, not a token or management one)", se.Source)
	}
	for _, other := range []error{ErrUnsupported, ErrPrefixNotInCatalog, ErrQueryTooShort} {
		if errors.Is(err, other) {
			t.Errorf("err is %v: the cause is the status, nothing else", other)
		}
	}
	if got := err.Error(); got != "registry returned 502" {
		t.Errorf("err.Error() = %q, want the message the journal shows without any URL", got)
	}
	if h := s.Hits("cut1-catalog-proxy-path--admin"); h != 1 {
		t.Errorf("Hits = %d, want 1: a 502 is not retried", h)
	}
}
