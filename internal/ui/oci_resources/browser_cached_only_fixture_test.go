package ociresources

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
)

// DEVDESK-580 (NX-08b, PR3, NX-04): what the level 1 header of the registry
// browser says about a Nexus group when the upstream of its proxy is reachable,
// then cut, replayed from the answers MEASURED on Nexus 3.98.1-01 (fixtures of the
// cachedonly campaign), through the real browser, the real CatalogSearcher and the
// real registry client. Criterion 1 of NX-08b wants "the Cached only verdict for
// the proxy". The measure contradicts it once the upstream is cut: the proxy is
// failed (first read) or refused (after Nexus blocked it), and a failed or refused
// registry is never listed under Cached only. These tests pin what DevDesk really
// shows; the contradiction and the conditional correction are written in
// docs/measurements/2026-11-20-nexus-cached-only.md, not fixed here.
//
// A fixture does not replay a DELAY. The first read after the cut takes Nexus about
// 20 s to answer its 502 and the registry client gives up after 15 s: against the
// real bench DevDesk shows Failed with a deadline as the cause, not the 502
// replayed here. That delay and that verdict were reported by the session of
// 2026-11-20 (TestIntegrationDevDeskOnACutUpstream, internal/nexusbench), not
// replayable by a fixture. The 20 s itself (H-CUTDELAY) was measured on an
// uncached manifest read, not on _catalog.
//
// The group connector of the bench is not in these cases' fake with the members:
// nominal and cut answers of /v2/_catalog?n=1000 are one request recorded twice.

// pickFixtures returns the named fixtures of a campaign; an unknown name fails the
// test (a removed fixture must not leave it vacuous).
func pickFixtures(t *testing.T, campaign string, names ...string) []nexusfixture.Fixture {
	t.Helper()
	all, err := nexusfixture.Load(campaign)
	if err != nil {
		t.Fatalf("Load(%s): %v", campaign, err)
	}
	var out []nexusfixture.Fixture
	for _, n := range names {
		i := slices.IndexFunc(all, func(f nexusfixture.Fixture) bool { return f.Name == n })
		if i < 0 {
			t.Fatalf("campaign %s has no fixture %q", campaign, n)
		}
		out = append(out, all[i])
	}
	return out
}

// wantRecorded pins what a fixture recorded, so that a regenerated one with another
// answer fails here before the replay can hide it.
func wantRecorded(t *testing.T, f nexusfixture.Fixture, target string, status int) {
	t.Helper()
	if len(f.Exchanges) != 1 || f.Exchanges[0].Target != target || f.Exchanges[0].Status != status {
		t.Fatalf("%s no longer records %s -> %d: the measurement changed", f.Name, target, status)
	}
}

func TestCachedOnlyLineOnMeasuredCutUpstream(t *testing.T) {
	const (
		// The hosted catalog is one request answered the same way in both states. Only
		// the cut state is a fixture of this campaign (the control that the cut leaves
		// the hosted repository alone); the nominal one comes from NX-03 (catalog).
		hostedCut     = "cut2-catalog-hosted-path--admin"
		hostedNominal = "catalog-hosted-path-n1000--admin"
		hostedMember  = "nx/docker-hosted"
		proxyMember   = "nx/docker-proxy"
	)
	// Two members reached by path, as discovery stores them for a Nexus group on the
	// HTTP port: the hosted one (answers, no mirror name) and the proxy.
	members := []struct {
		name    string
		proxy   string // fixture of the proxy catalog
		hosted  string // fixture of the hosted catalog (campaign catalog if nominal, else cachedonly)
		status  int
		verdict imagesearch.Verdict
		want    searchSummary // the names of the level 1 header, by class, unabbreviated
		rows    []string      // Repo of each row, sorted
		level   string        // journal level of the verdict ("" = no journal line)
		cause   string
	}{
		{
			name: "upstream reachable", proxy: "nominal-catalog-proxy-path--admin", hosted: hostedNominal, status: 200,
			verdict: imagesearch.VerdictOK,
			want:    searchSummary{CachedOnly: []string{hostedMember + cachedNoResultMark, proxyMember}},
			rows:    []string{"docker-proxy/mirror/cached", "docker-proxy/mirror/uncached"}, // under the proxy member
		},
		{
			// First read after the cut: 502 from Nexus. Failed, and NOT Cached only: the
			// proxy is not in the Cached only line although it is a proxy of a Nexus group.
			name: "first read after the cut", proxy: "cut1-catalog-proxy-path--admin", hosted: hostedCut, status: 502,
			verdict: imagesearch.VerdictFailed,
			want:    searchSummary{Failed: []string{proxyMember}, CachedOnly: []string{hostedMember + cachedNoResultMark}},
			level:   "ERROR", cause: "502",
		},
		{
			// Once Nexus has blocked the proxy: 404 "Remote Auto Blocked". DEVDESK-629 (F3):
			// Failed (the upstream is temporarily blocked), neither Refused nor Cached only.
			name: "proxy auto-blocked", proxy: "cut2-catalog-proxy-path--admin", hosted: hostedCut, status: 404,
			verdict: imagesearch.VerdictFailed,
			want:    searchSummary{Failed: []string{proxyMember}, CachedOnly: []string{hostedMember + cachedNoResultMark}},
			level:   "ERROR", cause: "temporarily blocked",
		},
	}
	for _, tc := range members {
		t.Run(tc.name, func(t *testing.T) {
			hostedCampaign := "cachedonly"
			if tc.hosted == hostedNominal {
				hostedCampaign = "catalog"
			}
			fixtures := append(pickFixtures(t, "cachedonly", tc.proxy), pickFixtures(t, hostedCampaign, tc.hosted)...)
			wantRecorded(t, fixtures[0], "/v2/docker-proxy/_catalog?n=1000", tc.status)
			wantRecorded(t, fixtures[1], "/v2/docker-hosted/_catalog?n=1000", 200)
			s := fakenexus.New(t, fixtures...)
			base := s.URL(benchHTTPPort)
			withImageSearcher(t, nexusOnlySearcher(t))
			logs := captureLog(t)

			m := modelWithGroups(t, []config.RegistryItem{adminGroup(t, base)}, discoveredMembers(base, "docker-hosted", "docker-proxy"))
			m, results := runLotAll(t, m, "mirror")
			if len(results) != 2 {
				t.Fatalf("%d answers, want one per member (2)", len(results))
			}

			proxyKey := memberKey("nx", base, "docker-proxy")
			wantVerdict(t, m, proxyKey, tc.verdict)
			wantVerdict(t, m, memberKey("nx", base, "docker-hosted"), imagesearch.VerdictNoResult)
			var wantRows []string
			for _, r := range tc.rows {
				wantRows = append(wantRows, proxyKey+":"+r)
			}
			if got := sorted(rowsOf(m)); !slices.Equal(got, sorted(wantRows)) {
				t.Errorf("rows = %v, want %v", got, wantRows)
			}
			// The summary, not the header text: the header abbreviates a long list ("+1").
			got := m.registryBrowser.searchSummary()
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("summary = %+v, want %+v", got, tc.want)
			}
			// The Cached only line never names the proxy unless it answered.
			if named := slices.Contains(got.CachedOnly, proxyMember); named != (tc.verdict == imagesearch.VerdictOK) {
				t.Errorf("Cached only = %v: names the proxy = %v, verdict %v", got.CachedOnly, named, tc.verdict)
			}
			if tc.level != "" {
				out := logs.String()
				line := tc.level + " [oci_resources] image search on " + proxyMember + ":"
				if !strings.Contains(out, line) || !strings.Contains(out, tc.cause) {
					t.Errorf("journal = %q, want %q and %q", out, line, tc.cause)
				}
			}
			if h := s.Hits(tc.proxy); h != 1 {
				t.Errorf("Hits(%s) = %d, want 1", tc.proxy, h)
			}
			if h := s.Hits(tc.hosted); h != 1 {
				t.Errorf("Hits(%s) = %d, want 1", tc.hosted, h)
			}
		})
	}
}

// The group on its own connector, its members having no address of their own: one
// unit, the group, read at /v2/_catalog. When the proxy is blocked Nexus omits the
// proxy's names from that catalog without a word: 200, the hosted names only. DevDesk
// reads an ordinary answer, "mirror" is "no result", and the Cached only line says
// "(no result)", which its own help text reads as "nothing in the cache", while
// mirror/cached IS in the cache and a HEAD of it answers 200 (TestManifestDigestOnMeasuredCutUpstream).
// The mention is insufficient there: it is on, and it is wrong about the cache.
func TestCachedOnlyLineOfAGroupOnItsConnectorOnMeasuredCutUpstream(t *testing.T) {
	cases := []struct {
		name     string
		campaign string
		fixture  string
		rows     []string
		cached   string // the Cached only value; the unit is labelled with groupUnitSuffix
	}{
		{"upstream reachable", "catalog", "page-group-n1000--admin",
			[]string{"nx:mirror/cached", "nx:mirror/uncached"}, groupUnitLabel("nx")},
		{"proxy auto-blocked", "cachedonly", "cut2-catalog-group-connector-30482--admin",
			nil, groupUnitLabel("nx") + cachedNoResultMark},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixtures := pickFixtures(t, tc.campaign, tc.fixture)
			wantRecorded(t, fixtures[0], "/v2/_catalog?n=1000", 200)
			s := fakenexus.New(t, fixtures...)
			base := s.URL(benchConnectorPort)
			withImageSearcher(t, nexusOnlySearcher(t))

			m := modelWithGroups(t, []config.RegistryItem{benchGroupAs(t, nexusfixture.Admin, base)},
				connectorMembers(base, "docker-hosted", "docker-proxy"))
			m, results := runLotAll(t, m, "mirror")
			if len(results) != 1 {
				t.Fatalf("%d answers, want 1 (the group is one unit)", len(results))
			}

			if tc.rows == nil {
				wantVerdict(t, m, "nx", imagesearch.VerdictNoResult)
			} else {
				wantVerdict(t, m, "nx", imagesearch.VerdictOK)
			}
			vals := wantHeaderKeys(t, m, "Images", "Cached only")
			if vals["Cached only"] != tc.cached {
				t.Errorf("Cached only = %q, want %q", vals["Cached only"], tc.cached)
			}
			if got := rowsOf(m); !slices.Equal(sorted(got), tc.rows) {
				t.Errorf("rows = %v, want %v", got, tc.rows)
			}
		})
	}
}

// adminGroup is the Nexus group "nx" at base, read as the bench admin:
// the credentials come from the docker config, the way the browser finds a login,
// and are shared by every member of the group (they share its host).
func adminGroup(t *testing.T, base string) config.RegistryItem {
	t.Helper()
	return benchGroupAs(t, nexusfixture.Admin, base)
}
