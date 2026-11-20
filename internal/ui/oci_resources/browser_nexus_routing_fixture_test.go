package ociresources

import (
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-580 (NX-08b, PR4, NX-05): criterion 5, the URLs DevDesk calls are the ones
// that answered on the bench. The real browser, the real CatalogSearcher and the real
// registry client are driven against fakenexus, which answers ONLY the requests the
// bench answered (any other is an error of the test, naming it) and counts the hits of
// each fixture. Criteria 3 (W5/W6, wrong password) is the second test.
//
// The three ways to address a Nexus repository in the configuration of
// docs/architecture/registries.md, `(url, repo_prefix)`:
//
//   - by path, on the HTTP port: a group whose members are discovered with the URL of
//     the port and their own repo_prefix (docker-hosted): catalog
//     /v2/docker-hosted/_catalog, tags /v2/docker-hosted/<name>/tags/list;
//   - by connector, on the group's own port: the group's URL and no prefix: catalog
//     /v2/_catalog, tags /v2/<name>/tags/list;
//   - by /repository/<name>: an entry whose URL carries the path (the shape D39 says
//     browses and cannot pull; DevDesk does not build it, a user can type it): catalog
//     /repository/docker-hosted/v2/_catalog, tags /repository/docker-hosted/v2/<name>/tags/list.
//
// The subdomain mode was not measured (no DNS on the bench, and the connector that
// serves it is Pro only, not tried): nothing is replayed for it.

type routingMode struct {
	name    string
	host    string
	catalog func(id nexusfixture.Identity) (campaign, fixture string)
	tags    string // fixture prefix of the tag list, "--<identity>" is appended
	repo    string // Repo of the row opened
	build   func(t *testing.T, id nexusfixture.Identity, base string) Model
}

func routingModes() []routingMode {
	return []routingMode{
		{
			name: "by path, HTTP port, member docker-hosted", host: benchHTTPPort,
			catalog: func(id nexusfixture.Identity) (string, string) {
				return "catalog", "catalog-hosted-path-n1000--" + string(id)
			},
			tags: "routing-tags-hosted-path", repo: "docker-hosted/api",
			build: func(t *testing.T, id nexusfixture.Identity, base string) Model {
				return modelWithGroups(t, []config.RegistryItem{benchGroupAs(t, id, base)}, discoveredMembers(base, "docker-hosted"))
			},
		},
		{
			name: "by connector, group port", host: benchConnectorPort,
			catalog: func(id nexusfixture.Identity) (string, string) {
				if id == nexusfixture.Admin {
					return "catalog", "page-group-n1000--admin" // the exact n=1000 recording
				}
				return "catalog", "catalog-group-connector-30482--" + string(id) // tolerated: recorded without n
			},
			tags: "routing-tags-group-connector-30482", repo: "api",
			build: func(t *testing.T, id nexusfixture.Identity, base string) Model {
				return modelWithGroups(t, []config.RegistryItem{benchGroupAs(t, id, base)}, connectorMembers(base, "docker-hosted", "docker-proxy"))
			},
		},
		{
			name: "by /repository/<name>, legacy URL of an entry", host: benchHTTPPort,
			catalog: func(id nexusfixture.Identity) (string, string) {
				return "catalog", "catalog-hosted-repository--" + string(id) // tolerated: recorded without n
			},
			tags: "routing-tags-hosted-repository", repo: "api",
			build: func(t *testing.T, id nexusfixture.Identity, base string) Model {
				entry := config.RegistryItem{Slug: "legacy", Alias: "legacy", URL: base + "/repository/docker-hosted", Kind: config.KindRegistry, Provider: config.ProviderGeneric}
				if id == nexusfixture.Anonymous {
					entry.AuthMode = config.AuthAnonymous
				} else {
					// The login is looked up under the entry's whole address, path included
					// (docker.GetStoredCreds): a login stored for the bare host is not found.
					u, _ := url.Parse(entry.URL)
					writeDockerConfig(t, map[string]any{"auths": map[string]any{u.Host + u.Path: map[string]string{"auth": encodeAuth(fakenexus.User(id), fakenexus.Password)}}})
					entry.AuthMode = config.AuthCredentials
				}
				return modelWithGroups(t, []config.RegistryItem{entry}, nil)
			},
		},
	}
}

// openRow moves the cursor to the row of repo and presses right, then runs the tag
// lot and feeds its answers back.
func openRow(t *testing.T, m Model, repo string) Model {
	t.Helper()
	for range 6 {
		if r, ok := m.registryBrowser.searchTable.Selected(); ok && r.img.Repo == repo {
			break
		}
		m = feed(t, m, testutil.Key("down"))
	}
	if r, ok := m.registryBrowser.searchTable.Selected(); !ok || r.img.Repo != repo {
		t.Fatalf("the cursor is on %+v, want the row %q (rows: %v)", r.img, repo, rowsOf(m))
	}
	m, cmd := step(t, m, testutil.Key("right"))
	if m.registryBrowser.state != browserStateTags {
		t.Fatalf("state = %d, want the tag table", m.registryBrowser.state)
	}
	return feed(t, m, testutil.Msgs(cmd)...)
}

func TestURLsDevDeskCallsAreTheMeasuredOnes(t *testing.T) {
	for _, mode := range routingModes() {
		for _, id := range measuredIdentities {
			t.Run(mode.name+", "+string(id), func(t *testing.T) {
				campaign, catalogName := mode.catalog(id)
				tagsName := mode.tags + "--" + string(id)
				fixtures := append(pickFixtures(t, campaign, catalogName), pickFixtures(t, "access", tagsName)...)
				tagsEx := fixtures[1].Exchanges
				if len(tagsEx) != 1 || tagsEx[0].Status != 200 || string(tagsEx[0].Body) != `{"name":"api","tags":["1.0.0","1.1.0"]}` {
					t.Fatalf("%s no longer records the tag list of api: the measurement changed", tagsName)
				}
				s := fakenexus.New(t, fixtures...)
				base := s.URL(mode.host)
				withImageSearcher(t, nexusOnlySearcher(t))
				m := mode.build(t, id, base)

				m, results := runLotAll(t, m, "api")
				if len(results) != 1 {
					t.Fatalf("%d answers, want 1", len(results))
				}
				for _, r := range results {
					wantVerdict(t, m, r.EntryKey, imagesearch.VerdictOK)
				}
				m = openRow(t, m, mode.repo)

				var got []string
				for _, r := range m.registryBrowser.tagTable.Visible() {
					got = append(got, r.tag.Tag)
				}
				slices.Sort(got)
				if !slices.Equal(got, []string{"1.0.0", "1.1.0"}) {
					t.Errorf("tags = %v, want the two the bench lists for api", got)
				}
				// One catalog read and one tag list, each answered by its fixture once; any
				// other request would have been refused by the fake and reported.
				wantExactHits(t, s, fixtures, map[string]int{catalogName: 1, tagsName: 1})
			})
		}
	}
}

// Criterion 3 and the W5/W6 variants through the browser: what the level 1 header and
// the verdict show for a wrong password and for an anonymous caller, with the bench
// nominal, with the DockerToken realm removed (W5) and with the anonymous access cut
// (W6). A wrong password is "unsupported or refused" everywhere (401, BASIC challenge,
// no Bearer flow). The anonymous caller is served (200) unless the anonymous access is
// cut, where Nexus challenges with a Bearer realm: the follow-up is not measured.
func TestAccessVariantsThroughTheBrowser(t *testing.T) {
	type where struct {
		slug, host, target string
		build              func(t *testing.T, base string, id nexusfixture.Identity, password string) Model
		key                string
		rows               []string
	}
	asEntry := func(id nexusfixture.Identity, password, base string, t *testing.T) config.RegistryItem {
		g := benchGroupAs(t, id, base)
		if id == nexusfixture.BadCredentials {
			u, _ := url.Parse(base)
			writeDockerConfig(t, map[string]any{"auths": map[string]any{u.Host: map[string]string{"auth": encodeAuth(fakenexus.User(id), password)}}})
			g.AuthMode = config.AuthCredentials
		}
		return g
	}
	wheres := []where{
		{
			slug: "catalog-group-connector-30482", host: benchConnectorPort, target: "/v2/_catalog?n=1000", key: "nx",
			rows: []string{"nx:api", "nx:team/api"},
			build: func(t *testing.T, base string, id nexusfixture.Identity, password string) Model {
				return modelWithGroups(t, []config.RegistryItem{asEntry(id, password, base, t)}, connectorMembers(base, "docker-hosted", "docker-proxy"))
			},
		},
		{
			slug: "catalog-hosted-path", host: benchHTTPPort, target: "/v2/docker-hosted/_catalog?n=1000", key: "docker-hosted",
			rows: []string{"api", "team/api"},
			build: func(t *testing.T, base string, id nexusfixture.Identity, password string) Model {
				return modelWithGroups(t, []config.RegistryItem{asEntry(id, password, base, t)}, discoveredMembers(base, "docker-hosted"))
			},
		},
	}
	type state struct{ prefix, label string }
	states := []state{{"bearer", "nominal"}, {"w5", "DockerToken realm removed"}, {"w6", "anonymous access cut"}}

	for _, st := range states {
		for _, w := range wheres {
			cases := []struct {
				id      nexusfixture.Identity
				pass    string
				status  int
				verdict imagesearch.Verdict
			}{
				{nexusfixture.BadCredentials, fakenexus.BadPassword, 401, imagesearch.VerdictRefused},
			}
			if st.prefix != "w6" {
				cases = append(cases, struct {
					id      nexusfixture.Identity
					pass    string
					status  int
					verdict imagesearch.Verdict
				}{nexusfixture.Anonymous, "", 200, imagesearch.VerdictOK})
			}
			if st.prefix != "bearer" {
				cases = append(cases, struct {
					id      nexusfixture.Identity
					pass    string
					status  int
					verdict imagesearch.Verdict
				}{nexusfixture.Admin, "", 200, imagesearch.VerdictOK})
			}
			for _, tc := range cases {
				name := st.prefix + "-" + w.slug + "--" + string(tc.id)
				t.Run(st.label+", "+w.slug+", "+string(tc.id), func(t *testing.T) {
					fixtures := pickFixtures(t, "access", name)
					wantRecorded(t, fixtures[0], w.target, tc.status)
					s := fakenexus.New(t, fixtures...)
					base := s.URL(w.host)
					withImageSearcher(t, nexusOnlySearcher(t))
					logs := captureLog(t)
					m := w.build(t, base, tc.id, tc.pass)

					m, results := runLotAll(t, m, "api")
					if len(results) != 1 {
						t.Fatalf("%d answers, want 1", len(results))
					}

					key := results[0].EntryKey
					wantVerdict(t, m, key, tc.verdict)
					if tc.verdict == imagesearch.VerdictOK {
						want := w.rows
						if w.host == benchHTTPPort {
							want = nil
							for _, r := range w.rows {
								want = append(want, key+":docker-hosted/"+r)
							}
						}
						if got := sorted(rowsOf(m)); !slices.Equal(got, sorted(want)) {
							t.Errorf("rows = %v, want %v", got, want)
						}
					} else {
						if rows := rowsOf(m); len(rows) != 0 {
							t.Errorf("rows = %v, want none", rows)
						}
						if out := logs.String(); !strings.Contains(out, "image search on ") {
							t.Errorf("journal = %q, want the refusal logged", out)
						}
					}
					if h := s.Hits(name); h != 1 {
						t.Errorf("Hits(%s) = %d, want 1", name, h)
					}
				})
			}
		}
	}

	// W6, anonymous: the challenge is a Bearer one, the client goes to the realm, and
	// the answer there is NOT measured. Pinned: no rows, a verdict that is not ok, and
	// the one request nothing recorded (the token endpoint of the realm).
	for _, w := range wheres {
		name := "w6-" + w.slug + "--anon"
		t.Run("anonymous access cut, "+w.slug+", anonymous", func(t *testing.T) {
			fixtures := pickFixtures(t, "access", name)
			wantRecorded(t, fixtures[0], w.target, 401)
			captured := &captureTB{TB: t}
			s := fakenexus.New(captured, fixtures...)
			base := s.URL(w.host)
			withImageSearcher(t, nexusOnlySearcher(t))
			captureLog(t)
			m := w.build(t, base, nexusfixture.Anonymous, "")

			m, results := runLotAll(t, m, "api")

			if len(results) != 1 {
				t.Fatalf("%d answers, want 1", len(results))
			}
			if o, ok := verdictOf(t, m, results[0].EntryKey); !ok || o.Verdict == imagesearch.VerdictOK {
				t.Errorf("verdict = %+v (%v), want one that is not ok", o, ok)
			}
			if rows := rowsOf(m); len(rows) != 0 {
				t.Errorf("rows = %v, want none", rows)
			}
			if got := captured.all(); len(got) != 1 {
				t.Errorf("the fake reported %q, want exactly the unmeasured token request", got)
			}
			if h := s.Hits(name); h != 1 {
				t.Errorf("Hits(%s) = %d, want 1", name, h)
			}
		})
	}
}

// captureTB lets a test expect the complaints of a fake instead of failing on them.
type captureTB struct {
	testing.TB
	mu   sync.Mutex
	errs []string
}

func (c *captureTB) Helper() {}

func (c *captureTB) Errorf(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errs = append(c.errs, strings.TrimSpace(format))
}

func (c *captureTB) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.errs)
}

// measuredIdentities are the three accounts the routing was measured with.
var measuredIdentities = []nexusfixture.Identity{nexusfixture.Admin, nexusfixture.Pull, nexusfixture.Anonymous}
