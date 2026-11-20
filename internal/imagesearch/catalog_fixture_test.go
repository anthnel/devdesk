package imagesearch

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-511 (NX-08, step 4): the real CatalogSearcher over oci.ListCatalogContext
// walks the _catalog answers MEASURED on Nexus 3.98.1-01 (NX-03, DEVDESK-506),
// replayed by fakenexus, then Classify. Every expected verdict below comes from
// a fixture, not from a guess about Nexus.
//
// Out of scope here, because no measured answer exists yet: the verdict
// "failed" on a Nexus answer (a 5xx, an unreadable body), the mention "Cached
// only" (decided from the config by the UI package, and it needs the NX-04
// answers) and the Bearer flow on a catalog (NX-05). Nothing is invented to
// stand in for them.

const (
	nexusHTTPPort  = "127.0.0.1:30481" // REST API, /repository/<name>; hosted and proxy live here
	nexusGroupPort = "127.0.0.1:30482" // the group's Docker connector

	// The member repositories of the bench, by the path under which they answer.
	nexusHosted = "/repository/docker-hosted"
	nexusProxy  = "/repository/docker-proxy"
	nexusGroup  = "/repository/docker-group"
)

// measuredIdentities are the three accounts every catalog was measured with: the
// answers are identical for all of them (NX-03, H2).
var measuredIdentities = []nexusfixture.Identity{nexusfixture.Admin, nexusfixture.Pull, nexusfixture.Anonymous}

func loadCatalogFixtures(t *testing.T) []nexusfixture.Fixture {
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

// answeringFixture is the fixture that answers DevDesk's "GET .../v2/_catalog?n=1000"
// for a scenario and an identity. Only the group connector's admin answer was
// recorded with n=1000 itself; every other one is a no-n recording that the
// fake lets answer n=1000 (the tolerance of fakenexus, measured on that one
// connector, inferred for hosted and proxy).
func answeringFixture(scenario string, id nexusfixture.Identity) string {
	if scenario == "catalog-group-connector-30482" && id == nexusfixture.Admin {
		return "page-group-n1000--admin"
	}
	return scenario + "--" + string(id)
}

// totalHits counts every request answered by any fixture of the campaign.
func totalHits(s *fakenexus.Server, fixtures []nexusfixture.Fixture) int {
	n := 0
	for _, f := range fixtures {
		n += s.Hits(f.Name)
	}
	return n
}

// targetFor builds the entry a user would have: the API URL, the prefix and the
// credentials of the identity (none for anonymous).
func targetFor(apiURL, prefix string, id nexusfixture.Identity) Target {
	tg := Target{APIURL: apiURL, RepoPrefix: prefix}
	if id != nexusfixture.Anonymous {
		tg.Username, tg.Password = fakenexus.User(id), fakenexus.Password
	}
	return tg
}

func TestCatalogOnMeasuredNexusAnswers(t *testing.T) {
	fixtures := loadCatalogFixtures(t)

	cases := []struct {
		name     string
		host     string // recorded Host whose fake server is walked
		path     string // path kept by the entry's API URL ("" = the connector root)
		scenario string // the fixture answering (see answeringFixture)
		prefix   string
		query    string
		want     Verdict
		rows     []string // Repo of each row, in the order Search returns them
		prefixIs bool     // the error is ErrPrefixNotInCatalog (criterion 3)
		// underPrefix reads the catalog at /v2/<prefix>/_catalog (DEVDESK-579): the
		// names come back unprefixed and Repo is "<prefix>/<name>".
		underPrefix bool
	}{
		// The group connector: the one place where a root /v2/_catalog answers. Six
		// names, none prefixed, the union of both members.
		{
			name: "group connector, api", host: nexusGroupPort, scenario: "catalog-group-connector-30482",
			query: "api", want: VerdictOK, rows: []string{"api", "team/api"},
		},
		{
			// F1: the proxy relays its upstream's catalog, so a name it holds nothing
			// of is listed: the search shows mirror/uncached.
			name: "group connector, mirror", host: nexusGroupPort, scenario: "catalog-group-connector-30482",
			query: "mirror", want: VerdictOK, rows: []string{"mirror/cached", "mirror/uncached"},
		},
		{
			name: "group connector, no match", host: nexusGroupPort, scenario: "catalog-group-connector-30482",
			query: "zz", want: VerdictNoResult,
		},
		{
			// Criterion 3: R1 on a REAL, unprefixed catalog. Nexus lists "api", not
			// "docker-hosted/api": no name carries the prefix, so the entry is refused
			// (not "no result"). The same holds for the proxy member.
			name: "group connector, prefix docker-hosted", host: nexusGroupPort, scenario: "catalog-group-connector-30482",
			prefix: "docker-hosted", query: "api", want: VerdictRefused, prefixIs: true,
		},
		{
			name: "group connector, prefix docker-proxy", host: nexusGroupPort, scenario: "catalog-group-connector-30482",
			prefix: "docker-proxy", query: "mirror", want: VerdictRefused, prefixIs: true,
		},

		// A member (or the group) reached by its own path: no prefix to apply.
		{
			name: "hosted by path, api", host: nexusHTTPPort, path: nexusHosted, scenario: "catalog-hosted-repository",
			query: "api", want: VerdictOK, rows: []string{"api", "team/api"},
		},
		{
			name: "hosted by path, mirror", host: nexusHTTPPort, path: nexusHosted, scenario: "catalog-hosted-repository",
			query: "mirror", want: VerdictNoResult,
		},
		{
			name: "proxy by path, mirror", host: nexusHTTPPort, path: nexusProxy, scenario: "catalog-proxy-repository",
			query: "mirror", want: VerdictOK, rows: []string{"mirror/cached", "mirror/uncached"},
		},
		{
			name: "proxy by path, api", host: nexusHTTPPort, path: nexusProxy, scenario: "catalog-proxy-repository",
			query: "api", want: VerdictNoResult,
		},
		{
			name: "group by path, api", host: nexusHTTPPort, path: nexusGroup, scenario: "catalog-group-repository",
			query: "api", want: VerdictOK, rows: []string{"api", "team/api"},
		},
		{
			// R1 again, through the by-path form: a Nexus member's catalog is never
			// prefixed by the repository name either.
			name: "hosted by path, prefix docker-hosted", host: nexusHTTPPort, path: nexusHosted, scenario: "catalog-hosted-repository",
			prefix: "docker-hosted", query: "api", want: VerdictRefused, prefixIs: true,
		},

		// DEVDESK-579 (NX-12): a member read at /v2/<prefix>/_catalog on the HTTP port
		// (docker.pathEnabled). Nexus answers with the member's own, unprefixed
		// names; DevDesk rebuilds "<prefix>/<name>" and applies no R1. The root
		// /v2/_catalog of that port (a 404 page) is never asked.
		{
			name: "hosted under its prefix, api", host: nexusHTTPPort, scenario: "catalog-hosted-path",
			prefix: "docker-hosted", underPrefix: true, query: "api", want: VerdictOK,
			rows: []string{"docker-hosted/api", "docker-hosted/team/api"},
		},
		{
			// shared/base is in the hosted AND the proxy catalog: each member reports
			// its own, with its own reference.
			name: "hosted under its prefix, base", host: nexusHTTPPort, scenario: "catalog-hosted-path",
			prefix: "docker-hosted", underPrefix: true, query: "base", want: VerdictOK,
			rows: []string{"docker-hosted/shared/base"},
		},
		{
			name: "hosted under its prefix, mirror", host: nexusHTTPPort, scenario: "catalog-hosted-path",
			prefix: "docker-hosted", underPrefix: true, query: "mirror", want: VerdictNoResult,
		},
		{
			name: "proxy under its prefix, mirror", host: nexusHTTPPort, scenario: "catalog-proxy-path",
			prefix: "docker-proxy", underPrefix: true, query: "mirror", want: VerdictOK,
			rows: []string{"docker-proxy/mirror/cached", "docker-proxy/mirror/uncached"},
		},
		{
			name: "proxy under its prefix, base", host: nexusHTTPPort, scenario: "catalog-proxy-path",
			prefix: "docker-proxy", underPrefix: true, query: "base", want: VerdictOK,
			rows: []string{"docker-proxy/shared/base"},
		},
		{
			name: "proxy under its prefix, api", host: nexusHTTPPort, scenario: "catalog-proxy-path",
			prefix: "docker-proxy", underPrefix: true, query: "api", want: VerdictNoResult,
		},
	}
	for _, tc := range cases {
		for _, id := range measuredIdentities {
			t.Run(tc.name+" as "+string(id), func(t *testing.T) {
				s := fakenexus.New(t, fixtures...)
				tg := targetFor(s.URL(tc.host)+tc.path, tc.prefix, id)
				tg.CatalogUnderPrefix = tc.underPrefix

				imgs, partial, err := CatalogSearcher{}.Search(context.Background(), tg, Query{Text: tc.query})
				out := Classify(err, len(imgs), partial)

				if out.Verdict != tc.want {
					t.Fatalf("verdict = %v, want %v (err = %v)", out.Verdict, tc.want, err)
				}
				if out.Partial || out.Skipped {
					t.Errorf("marks = %+v, want none", out)
				}
				if got := errors.Is(err, ErrPrefixNotInCatalog); got != tc.prefixIs {
					t.Errorf("errors.Is(err, ErrPrefixNotInCatalog) = %v, want %v (err = %v)", got, tc.prefixIs, err)
				}
				if tc.want != VerdictRefused && err != nil {
					t.Errorf("err = %v, want nil", err)
				}
				var got []string
				for _, im := range imgs {
					got = append(got, im.Repo)
					switch {
					case tc.underPrefix && im.Repo != tc.prefix+"/"+im.Name:
						t.Errorf("row %+v: Repo is not %q + Name", im, tc.prefix+"/")
					case !tc.underPrefix && im.Name != im.Repo:
						t.Errorf("row %+v: Name differs from Repo with no prefix applied", im)
					}
				}
				if !reflect.DeepEqual(got, tc.rows) {
					t.Errorf("rows = %v, want %v", got, tc.rows)
				}

				// Criterion 2: a target read under its prefix never asks a root catalog.
				if tc.underPrefix {
					for _, f := range fixtures {
						if strings.Contains(f.Name, "-root-") || strings.Contains(f.Name, "-connector-") {
							if h := s.Hits(f.Name); h != 0 {
								t.Errorf("Hits(%s) = %d, want 0: a root catalog was asked", f.Name, h)
							}
						}
					}
				}

				// Exactly one catalog request, answered by the expected recording.
				if h := s.Hits(answeringFixture(tc.scenario, id)); h != 1 {
					t.Errorf("Hits(%s) = %d, want 1", answeringFixture(tc.scenario, id), h)
				}
				if n := totalHits(s, fixtures); n != 1 {
					t.Errorf("%d requests answered in total, want 1", n)
				}
			})
		}
	}
}

// What discovery gives a Nexus group member is the HTTP port plus the member's
// prefix (registrymgr, step 5). DevDesk then asks the root of that port for
// /v2/_catalog?n=1000, where Nexus has no catalog at all: it answers 404 (an
// HTML page, measured as catalog-hosted-root-30481--*). The 404 comes before any
// filtering, so R1 is not reached: the verdict is refused, with a registry
// StatusError, not "no result" and not "failed".
//
// Pending (DEVDESK-580): the root 404 was recorded WITHOUT the "n=1000"
// DevDesk sends, and fakenexus deliberately does not let a recorded non-200
// answer an n=1000 request (TestCatalogTolerance, "a recorded 404 is not
// tolerated"). There is no measured fixture for GET /v2/_catalog?n=1000 on the
// root port of a hosted repository, and none is invented here. It is to be
// recorded on the bench through DEVDESK-580 (NX-08b) with `nexus-bench record`;
// remove the Skip below once that fixture exists, the body is ready to run.
func TestCatalogOnTheHTTPPortRootIsRefused(t *testing.T) {
	fixtures := loadCatalogFixtures(t)
	if !recordsTarget(fixtures, nexusHTTPPort, "/v2/_catalog?n=1000") {
		t.Skip("pending DEVDESK-580 (NX-08b): no measured fixture for GET /v2/_catalog?n=1000 on the root port of a hosted repository; record it on the bench with `nexus-bench record` (this test runs by itself once it is recorded)")
	}
	for _, id := range measuredIdentities {
		for _, prefix := range []string{"docker-hosted", "docker-proxy"} {
			t.Run(prefix+" as "+string(id), func(t *testing.T) {
				s := fakenexus.New(t, fixtures...)
				tg := targetFor(s.URL(nexusHTTPPort), prefix, id)

				imgs, partial, err := CatalogSearcher{}.Search(context.Background(), tg, Query{Text: "api"})
				if got := Classify(err, len(imgs), partial); got != (Outcome{Verdict: VerdictRefused}) {
					t.Fatalf("outcome = %+v, want refused (err = %v)", got, err)
				}
				var se *oci.StatusError
				if !errors.As(err, &se) || se.Code != 404 || se.Source != oci.SourceRegistry {
					t.Errorf("err = %v (%T), want *oci.StatusError{404, SourceRegistry}", err, err)
				}
				if errors.Is(err, ErrPrefixNotInCatalog) {
					t.Error("R1 was reached: there is no catalog to filter on this port")
				}
				if h := s.Hits("catalog-hosted-root-30481--" + string(id)); h != 1 {
					t.Errorf("Hits(catalog-hosted-root-30481--%s) = %d, want 1", id, h)
				}
			})
		}
	}
}

// recordsTarget reports whether a loaded fixture holds an exchange for exactly
// this target on this recorded Host, for any account: the tolerance of fakenexus
// is not a recording, and the test above needs the real answer.
func recordsTarget(fixtures []nexusfixture.Fixture, host, target string) bool {
	for _, f := range fixtures {
		for _, ex := range f.Exchanges {
			if ex.Host == host && ex.Target == target {
				return true
			}
		}
	}
	return false
}

// The fixtures are measured with the three accounts and the answers do not
// depend on the identity (H2): a verdict that differed between them would mean a
// fixture, or the matching, changed. Guard the premise the table above relies on.
func TestMeasuredCatalogsDoNotDependOnTheAccount(t *testing.T) {
	byName := map[string]nexusfixture.Fixture{}
	for _, f := range loadCatalogFixtures(t) {
		byName[f.Name] = f
	}
	for _, scenario := range []string{
		"catalog-group-connector-30482", "catalog-group-repository",
		"catalog-hosted-repository", "catalog-proxy-repository",
	} {
		admin := byName[scenario+"--admin"]
		for _, id := range []nexusfixture.Identity{nexusfixture.Pull, nexusfixture.Anonymous} {
			other := byName[scenario+"--"+string(id)]
			if len(admin.Exchanges) != 1 || len(other.Exchanges) != 1 {
				t.Fatalf("%s: want one exchange per account", scenario)
			}
			a, o := admin.Exchanges[0], other.Exchanges[0]
			if a.Status != o.Status || string(a.Body) != string(o.Body) {
				t.Errorf("%s: %s answers %d %q, admin %d %q", scenario, id, o.Status, o.Body, a.Status, a.Body)
			}
			if !strings.HasPrefix(string(a.Body), `{"repositories":[`) {
				t.Errorf("%s: unexpected body %q", scenario, a.Body)
			}
		}
	}
}

// underTargetFor is targetFor for an entry whose catalog is read under its prefix.
func underTargetFor(apiURL, prefix string, id nexusfixture.Identity) Target {
	tg := targetFor(apiURL, prefix, id)
	tg.CatalogUnderPrefix = true
	return tg
}

// Criterion 4 on measured answers: hosted and proxy are two members of ONE host
// (127.0.0.1:30481). Each is walked once, under its own path, in a shared batch;
// searching the same member again reuses its walk. shared/base is in both
// catalogs, and each row keeps its own member's reference.
func TestCatalogMembersOfOneHostAreWalkedSeparately(t *testing.T) {
	fixtures := loadCatalogFixtures(t)
	for _, id := range measuredIdentities {
		t.Run(string(id), func(t *testing.T) {
			s := fakenexus.New(t, fixtures...)
			apiURL := s.URL(nexusHTTPPort)
			ctx := WithSharedWalks(context.Background())
			search := func(key, prefix, text string) string {
				tg := underTargetFor(apiURL, prefix, id)
				tg.EntryKey = key
				imgs, partial, err := CatalogSearcher{}.Search(ctx, tg, Query{Text: text})
				if out := Classify(err, len(imgs), partial); out != (Outcome{Verdict: VerdictOK}) {
					t.Fatalf("%s: outcome = %+v err = %v, want ok", key, out, err)
				}
				return names(imgs)
			}

			if got := search("hosted", "docker-hosted", "base"); got != "docker-hosted/shared/base" {
				t.Errorf("hosted rows = %q, want docker-hosted/shared/base", got)
			}
			if got := search("proxy", "docker-proxy", "base"); got != "docker-proxy/shared/base" {
				t.Errorf("proxy rows = %q, want docker-proxy/shared/base", got)
			}
			if got := search("hosted-again", "docker-hosted", "api"); got != "docker-hosted/api,docker-hosted/team/api" {
				t.Errorf("hosted rows, second search = %q", got)
			}

			if h := s.Hits("catalog-hosted-path--" + string(id)); h != 1 {
				t.Errorf("Hits(catalog-hosted-path--%s) = %d, want 1: one walk for the hosted member", id, h)
			}
			if h := s.Hits("catalog-proxy-path--" + string(id)); h != 1 {
				t.Errorf("Hits(catalog-proxy-path--%s) = %d, want 1: one walk for the proxy member", id, h)
			}
			if n := totalHits(s, fixtures); n != 2 {
				t.Errorf("%d requests answered in total, want 2 (criterion 4); none on a root catalog (criterion 2)", n)
			}
		})
	}
}

// The group by its path on the HTTP port is not a catalog: Nexus answers a 404
// page there (catalog-group-path--*, measured WITHOUT n). With the "?n=1000" that
// DevDesk sends, fakenexus does not let a recorded 404 answer (TestCatalogTolerance,
// "a recorded 404 is not tolerated"): that target answers a 501 and a t.Errorf.
// That a Nexus 404 stays a 404 with a "?n=1000" is an assumption, not a
// measurement, and none is invented here (plan DEVDESK-579, hypothesis H-4).
//
// Pending: record GET /v2/docker-group/_catalog?n=1000 on 127.0.0.1:30481 with
// `nexus-bench record` (DEVDESK-580, NX-08b); this test then runs by itself.
// Criterion 6: the group is refused with its StatusError, not "no result" and not
// "failed", and it is not a prefix problem either.
func TestCatalogOfAGroupUnderItsPrefixIsRefused(t *testing.T) {
	fixtures := loadCatalogFixtures(t)
	const target = "/v2/docker-group/_catalog?n=1000"
	if !recordsTarget(fixtures, nexusHTTPPort, target) {
		t.Skip("pending DEVDESK-580 (NX-08b): no measured fixture for GET " + target + " on the HTTP port (only the no-n answer, catalog-group-path--*, is measured, and fakenexus does not tolerate a 404 for n=1000); record it on the bench with `nexus-bench record`")
	}
	for _, id := range measuredIdentities {
		t.Run(string(id), func(t *testing.T) {
			s := fakenexus.New(t, fixtures...)
			ctx := WithSharedWalks(context.Background())

			imgs, partial, err := CatalogSearcher{}.Search(ctx, underTargetFor(s.URL(nexusHTTPPort), "docker-group", id), Query{Text: "api"})
			if got := Classify(err, len(imgs), partial); got != (Outcome{Verdict: VerdictRefused}) {
				t.Fatalf("outcome = %+v, want refused (err = %v)", got, err)
			}
			var se *oci.StatusError
			if !errors.As(err, &se) || se.Code != 404 || se.Source != oci.SourceRegistry {
				t.Errorf("err = %v (%T), want *oci.StatusError{404, SourceRegistry}", err, err)
			}
			if errors.Is(err, ErrPrefixNotInCatalog) {
				t.Error("R1 was reached: a catalog read by path has none")
			}
			if n := totalHits(s, fixtures); n != 1 {
				t.Errorf("%d requests answered in total, want 1", n)
			}

			// The group's 404 does not touch the members of the same host.
			other, partial, err := CatalogSearcher{}.Search(ctx, underTargetFor(s.URL(nexusHTTPPort), "docker-hosted", id), Query{Text: "api"})
			if got := Classify(err, len(other), partial); got != (Outcome{Verdict: VerdictOK}) {
				t.Errorf("hosted member: outcome = %+v, want ok (err = %v)", got, err)
			}
		})
	}
}
