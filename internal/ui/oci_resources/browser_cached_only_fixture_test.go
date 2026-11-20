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

// DEVDESK-580 (NX-08b, PR3, NX-04), behavior changed by DEVDESK-629 (F2, F4); the
// frozen note (docs/measurements/2026-11-20-nexus-cached-only.md) describes the code
// before it.
//
// What the level 1 header of the registry browser says about a Nexus group when the
// upstream of its proxy is reachable, then cut, replayed from the answers MEASURED
// on Nexus 3.98.1-01 (fixtures of the cachedonly campaign), through the real
// browser, the real CatalogSearcher and the real registry client.
//
// Criterion 1 of NX-08b wanted "the Cached only verdict for the proxy". The measure
// contradicted it: once the upstream is cut the proxy is failed (first read) or
// failed with the cause "temporarily blocked" (after Nexus blocked it), and the
// group on its connector answers 200 WITHOUT the proxy's names. DEVDESK-629 settles
// it: the line is `May omit images`, it follows the role of the entry (type read at
// discovery), a proxy is on it only when it failed or was refused (never when its
// upstream answers), a group whatever it answered, a hosted repository never.
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

// Behavior changed by DEVDESK-629 (F2); the frozen note describes the code before it.
//
// Two members reached by path, as discovery stores them for a Nexus group on the
// HTTP port: the hosted one (answers, no mirror name) and the proxy. Typed, as
// discovery now stores them, the proxy is on May omit images only when it failed;
// the hosted one never. Untyped (a cache written before the type), nothing is ever
// mentioned: no mention is better than a false one.
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
	members := []struct {
		name    string
		proxy   string // fixture of the proxy catalog
		hosted  string // fixture of the hosted catalog (campaign catalog if nominal, else cachedonly)
		status  int
		verdict imagesearch.Verdict
		want    searchSummary // typed members: the names of the level 1 header, by class, unabbreviated
		rows    []string      // Repo of each row, sorted
		level   string        // journal level of the verdict ("" = no journal line)
		cause   string
	}{
		{
			// The upstream answers: the proxy is a plain proxy, not "limited to the cache".
			name: "upstream reachable", proxy: "nominal-catalog-proxy-path--admin", hosted: hostedNominal, status: 200,
			verdict: imagesearch.VerdictOK,
			want:    searchSummary{NoResult: []string{hostedMember}},
			rows:    []string{"docker-proxy/mirror/cached", "docker-proxy/mirror/uncached"}, // under the proxy member
		},
		{
			// First read after the cut: 502 from Nexus. The proxy is Failed, and may have
			// omitted images: both lines. The hosted one answered with nothing, plainly.
			name: "first read after the cut", proxy: "cut1-catalog-proxy-path--admin", hosted: hostedCut, status: 502,
			verdict: imagesearch.VerdictFailed,
			want:    searchSummary{Failed: []string{proxyMember}, MayOmit: []string{proxyMember}, NoResult: []string{hostedMember}},
			level:   "ERROR", cause: "502",
		},
		{
			// Once Nexus has blocked the proxy: 404 "Remote Auto Blocked". DEVDESK-629 (F3):
			// Failed (the upstream is temporarily blocked), and so on May omit images too.
			name: "proxy auto-blocked", proxy: "cut2-catalog-proxy-path--admin", hosted: hostedCut, status: 404,
			verdict: imagesearch.VerdictFailed,
			want:    searchSummary{Failed: []string{proxyMember}, MayOmit: []string{proxyMember}, NoResult: []string{hostedMember}},
			level:   "ERROR", cause: "temporarily blocked",
		},
	}
	for _, tc := range members {
		for _, typed := range []bool{true, false} {
			name := tc.name + ", typed members"
			if !typed {
				name = tc.name + ", members with no type"
			}
			t.Run(name, func(t *testing.T) {
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

				discovered := discoveredMembers(base, "docker-hosted", "docker-proxy")
				if typed {
					discovered = typedMembers(base, typedMember{"docker-hosted", "hosted"}, typedMember{"docker-proxy", "proxy"})
				}
				m := modelWithGroups(t, []config.RegistryItem{adminGroup(t, base)}, discovered)
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
				want := tc.want
				if !typed {
					want.MayOmit = nil
				}
				got := m.registryBrowser.searchSummary()
				if !reflect.DeepEqual(got, want) {
					t.Errorf("summary = %+v, want %+v", got, want)
				}
				// The hosted member is never on the line, typed or not.
				if slices.Contains(got.MayOmit, hostedMember) {
					t.Errorf("%s = %v: names the hosted member", mayOmitKey, got.MayOmit)
				}
				if tc.level != "" {
					out := logs.String()
					line := tc.level + " [oci_resources] image search on " + proxyMember + ":"
					if !strings.Contains(out, line) || !strings.Contains(out, tc.cause) {
						t.Errorf("journal = %q, want %q and %q", out, line, tc.cause)
					}
				}
				// The type came from discovery: the search asks the two catalogs, no more.
				if h := s.Hits(tc.proxy); h != 1 {
					t.Errorf("Hits(%s) = %d, want 1", tc.proxy, h)
				}
				if h := s.Hits(tc.hosted); h != 1 {
					t.Errorf("Hits(%s) = %d, want 1", tc.hosted, h)
				}
			})
		}
	}
}

// Behavior changed by DEVDESK-629 (F2, F4); the frozen note describes the code before it.
//
// The group on its own connector, its members having no address of their own: one
// unit, the group, read at /v2/_catalog. When the proxy is blocked Nexus omits the
// proxy's names from that catalog without a word: 200, the hosted names only.
// DevDesk cannot tell that from an ordinary answer, and it must not pretend to: for
// "mirror" the verdict is "no result", and the header never says the image is absent.
// It says the group may omit images, on every answer of a group (R9: also when
// nothing is omitted), whatever the members it stands for are typed.
func TestCachedOnlyLineOfAGroupOnItsConnectorOnMeasuredCutUpstream(t *testing.T) {
	cases := []struct {
		name     string
		campaign string
		fixture  string
		rows     []string
		mayOmit  string // the May omit images value; the unit is labelled with groupUnitSuffix
	}{
		{"upstream reachable", "catalog", "page-group-n1000--admin",
			[]string{"nx:mirror/cached", "nx:mirror/uncached"}, groupUnitLabel("nx")},
		{"proxy auto-blocked", "cachedonly", "cut2-catalog-group-connector-30482--admin",
			nil, groupUnitLabel("nx") + noResultMark},
	}
	for _, tc := range cases {
		for _, typed := range []bool{true, false} {
			name := tc.name + ", typed members"
			if !typed {
				name = tc.name + ", members with no type"
			}
			t.Run(name, func(t *testing.T) {
				fixtures := pickFixtures(t, tc.campaign, tc.fixture)
				wantRecorded(t, fixtures[0], "/v2/_catalog?n=1000", 200)
				s := fakenexus.New(t, fixtures...)
				base := s.URL(benchConnectorPort)
				withImageSearcher(t, nexusOnlySearcher(t))

				members := connectorMembers(base, "docker-hosted", "docker-proxy")
				if typed {
					members = typedConnectorMembers(base, typedMember{"docker-hosted", "hosted"}, typedMember{"docker-proxy", "proxy"})
				}
				m := modelWithGroups(t, []config.RegistryItem{benchGroupAs(t, nexusfixture.Admin, base)}, members)
				m, results := runLotAll(t, m, "mirror")
				if len(results) != 1 {
					t.Fatalf("%d answers, want 1 (the group is one unit)", len(results))
				}
				// One request on the group's connector, and no other: the members are not
				// probed (the fake fails the test on a request it has no recording for).
				wantExactHits(t, s, fixtures, map[string]int{tc.fixture: 1})

				if tc.rows == nil {
					wantVerdict(t, m, "nx", imagesearch.VerdictNoResult)
				} else {
					wantVerdict(t, m, "nx", imagesearch.VerdictOK)
				}
				// Never "No result" (that would say absent), never a word that says absent.
				keys, vals := headerLines(m)
				if want := []string{"Images", mayOmitKey}; !slices.Equal(keys, want) {
					t.Fatalf("header keys = %v, want %v (values %v)", keys, want, vals)
				}
				if vals[mayOmitKey] != tc.mayOmit {
					t.Errorf("%s = %q, want %q", mayOmitKey, vals[mayOmitKey], tc.mayOmit)
				}
				for k, v := range vals {
					if strings.Contains(strings.ToLower(k+" "+v), "absent") {
						t.Errorf("header line %s = %q says absent", k, v)
					}
				}
				if got := rowsOf(m); !slices.Equal(sorted(got), tc.rows) {
					t.Errorf("rows = %v, want %v", got, tc.rows)
				}
			})
		}
	}
}

// F4: the connector of a blocked proxy answers 200 without the proxy's names, and
// nothing in that answer says so. What DevDesk can do is stay honest about it. On
// the same recording: "api" is an ordinary ok (the hosted names are there), and
// "mirror" asks ONE request, the group's catalog, and does not go and ask the
// members what they hold (that would be a request per member per search).
func TestAGroupOnItsConnectorThatOmitsTheProxyNamesNeverSaysAbsent(t *testing.T) {
	const fixture = "cut2-catalog-group-connector-30482--admin"
	cases := []struct {
		query   string
		verdict imagesearch.Verdict
		rows    []string
		mayOmit string
	}{
		{"api", imagesearch.VerdictOK, []string{"nx:api", "nx:team/api"}, groupUnitLabel("nx")},
		{"mirror", imagesearch.VerdictNoResult, nil, groupUnitLabel("nx") + noResultMark},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			fixtures := pickFixtures(t, "cachedonly", fixture)
			wantRecorded(t, fixtures[0], "/v2/_catalog?n=1000", 200)
			s := fakenexus.New(t, fixtures...)
			base := s.URL(benchConnectorPort)
			withImageSearcher(t, nexusOnlySearcher(t))
			m := modelWithGroups(t, []config.RegistryItem{benchGroupAs(t, nexusfixture.Admin, base)},
				typedConnectorMembers(base, typedMember{"docker-hosted", "hosted"}, typedMember{"docker-proxy", "proxy"}))

			m, results := runLotAll(t, m, tc.query)

			if len(results) != 1 {
				t.Fatalf("%d answers, want 1", len(results))
			}
			wantExactHits(t, s, fixtures, map[string]int{fixture: 1})
			wantVerdict(t, m, "nx", tc.verdict)
			if got := rowsOf(m); !slices.Equal(sorted(got), tc.rows) {
				t.Errorf("rows = %v, want %v", got, tc.rows)
			}
			got := m.registryBrowser.searchSummary()
			if len(got.NoResult) != 0 {
				t.Errorf("No result = %v: the group's empty answer must not read as absent", got.NoResult)
			}
			if want := []string{tc.mayOmit}; !slices.Equal(got.MayOmit, want) {
				t.Errorf("%s = %v, want %v", mayOmitKey, got.MayOmit, want)
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
