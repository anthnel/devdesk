package ociresources

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-579 (NX-12), step 5: a Nexus group member is searched through its own
// catalog, /v2/<repo_prefix>/_catalog, never through the root /v2/_catalog that
// Nexus does not serve on the HTTP port (measured, 404 page). The browser sets
// CatalogUnderPrefix on every entry whose group is declared `provider: nexus`:
// the members discovered for it and the entries that declare it as `parent:`.
// Entries of any other registry keep the root catalog and R1 (D140).
//
// The answers come from the fixtures measured on the bench (Nexus 3.98.1-01),
// replayed by fakenexus; the failures nobody measured (5xx, 429) use httptest.

// benchHTTPPort is the recorded Host of the bench's REST port, where hosted and
// proxy repositories answer under /v2/<repository>/ (docker.pathEnabled).
const benchHTTPPort = "127.0.0.1:30481"

// nexusFake replays the measured catalog campaign and returns it with the
// fixtures, so a test can name which recording answered.
func nexusFake(t *testing.T) (*fakenexus.Server, []nexusfixture.Fixture) {
	t.Helper()
	fixtures, err := nexusfixture.Load("catalog")
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("Load(catalog) = %d fixtures, %v", len(fixtures), err)
	}
	return fakenexus.New(t, fixtures...), fixtures
}

// nexusOnlySearcher is the production routing with a Docker Hub that must never
// be asked: a Nexus member is not Docker Hub.
func nexusOnlySearcher(t *testing.T) imagesearch.Searcher {
	t.Helper()
	hub := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("Docker Hub was asked: %s", r.URL.Path)
	}))
	t.Cleanup(hub.Close)
	return wiredLikeProduction(hub)
}

// modelWithGroups opens the browser on items with the discovered members of the
// groups, as the Registries tab's cache would give them.
func modelWithGroups(t *testing.T, items []config.RegistryItem, groups map[string]cache.RegistryGroupEntry) Model {
	t.Helper()
	cfg := testConfig()
	cfg.Registry.Registries = items
	m := feed(t, New(cfg), testutil.Resize(200, 40),
		ImagesListMsg{Images: imageFixtures()},
		RegistryGroupCacheLoadedMsg{Entries: groups},
	)
	m = feed(t, m, testutil.Key(keymap.Browser))
	if m.registryBrowser == nil {
		t.Fatal("'b' did not open the registry browser")
	}
	return m
}

// anonymousNexusGroup is the Nexus group "nx" at base, read without credentials.
func anonymousNexusGroup(base string) config.RegistryItem {
	g := nexusGroup()
	g.AuthMode = config.AuthAnonymous
	g.URL = base
	return g
}

// discoveredMembers are members of "nx" the way discovery stores them for a
// path-routed group: the host of the HTTP port plus one prefix each.
func discoveredMembers(base string, prefixes ...string) map[string]cache.RegistryGroupEntry {
	var members []cache.RegistryGroupMember
	for _, p := range prefixes {
		members = append(members, cache.RegistryGroupMember{Alias: p, URL: base, RepoPrefix: p})
	}
	return map[string]cache.RegistryGroupEntry{"nx": {Members: members}}
}

// nexusMembersModel opens the browser on the anonymous group "nx" whose
// discovered members are the given prefixes at base.
func nexusMembersModel(t *testing.T, base string, prefixes ...string) Model {
	t.Helper()
	return modelWithGroups(t, []config.RegistryItem{anonymousNexusGroup(base)}, discoveredMembers(base, prefixes...))
}

// declaredMember is an entry declared by hand under the group "nx".
func declaredMember(base, slug, prefix string) config.RegistryItem {
	return config.RegistryItem{
		URL: base, Alias: slug, Slug: slug, Kind: config.KindRegistry,
		Parent: "nx", RepoPrefix: prefix, AuthMode: config.AuthAnonymous,
	}
}

// wantOnlyMemberCatalogsAsked checks that a Nexus-by-path lot asked the two member
// catalogs once each and nothing else: no root catalog, no connector, no other recording.
func wantOnlyMemberCatalogsAsked(t *testing.T, s *fakenexus.Server, fixtures []nexusfixture.Fixture) {
	t.Helper()
	for _, name := range []string{"catalog-hosted-path--anon", "catalog-proxy-path--anon"} {
		if h := s.Hits(name); h != 1 {
			t.Errorf("Hits(%s) = %d, want 1 (one walk per member)", name, h)
		}
	}
	for _, f := range fixtures {
		if strings.Contains(f.Name, "-root-") || strings.Contains(f.Name, "-connector-") {
			if h := s.Hits(f.Name); h != 0 {
				t.Errorf("Hits(%s) = %d, want 0: the root /v2/_catalog was asked", f.Name, h)
			}
		}
	}
	total := 0
	for _, f := range fixtures {
		total += s.Hits(f.Name)
	}
	if total != 2 {
		t.Errorf("%d requests answered in total, want 2", total)
	}
}

func sorted(rows []string) []string {
	out := slices.Clone(rows)
	slices.Sort(out)
	return out
}

// Criteria 1, 2, 4 and 5: two members of one host, addressed by path, are one
// walk each on their own catalog; their rows carry their own reference, the
// verdicts are per member, and the root catalog is never asked.
func TestNexusMembersByPathWalkTheirOwnCatalog(t *testing.T) {
	tests := []struct {
		name  string
		query string
		// rows by member prefix; a member with no row answered "no result".
		hosted, proxy []string
	}{
		{
			// shared/base is in BOTH catalogs: each member lists it, with its own
			// reference (a pull through the group would resolve it to the first).
			name: "a name both members hold", query: "base",
			hosted: []string{"docker-hosted/shared/base"},
			proxy:  []string{"docker-proxy/shared/base"},
		},
		{
			name: "a name only the hosted member holds", query: "api",
			hosted: []string{"docker-hosted/api", "docker-hosted/team/api"},
		},
		{
			name: "a name only the proxy member holds", query: "mirror",
			proxy: []string{"docker-proxy/mirror/cached", "docker-proxy/mirror/uncached"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, fixtures := nexusFake(t)
			base := s.URL(benchHTTPPort)
			withImageSearcher(t, nexusOnlySearcher(t))

			m := runLot(t, nexusMembersModel(t, base, "docker-hosted", "docker-proxy"), tt.query, 2)

			wantOnlyMemberCatalogsAsked(t, s, fixtures)
			var want []string
			for _, r := range tt.hosted {
				want = append(want, memberKey("nx", base, "docker-hosted")+":"+r)
			}
			for _, r := range tt.proxy {
				want = append(want, memberKey("nx", base, "docker-proxy")+":"+r)
			}
			if got := rowsOf(m); !slices.Equal(sorted(got), sorted(want)) {
				t.Errorf("rows = %v, want %v", got, want)
			}
			for prefix, rows := range map[string][]string{"docker-hosted": tt.hosted, "docker-proxy": tt.proxy} {
				wantV := imagesearch.VerdictOK
				if len(rows) == 0 {
					wantV = imagesearch.VerdictNoResult
				}
				wantVerdict(t, m, memberKey("nx", base, prefix), wantV)
			}
		})
	}
}

// The same members, once a lot has run, are listed as "Cached only" under their
// own labels, and nothing is Failed or Refused.
func TestNexusMembersByPathAreCachedOnlyAndHealthy(t *testing.T) {
	s, _ := nexusFake(t)
	base := s.URL(benchHTTPPort)
	withImageSearcher(t, nexusOnlySearcher(t))

	m := runLot(t, nexusMembersModel(t, base, "docker-hosted", "docker-proxy"), "base", 2)

	_, vals := headerLines(m)
	if want := "nx/docker-hosted, nx/docker-proxy"; vals["Cached only"] != want {
		t.Errorf("Cached only = %q, want %q", vals["Cached only"], want)
	}
	for _, k := range []string{"Failed", "Refused"} {
		if vals[k] != "" {
			t.Errorf("%s = %q, want none", k, vals[k])
		}
	}
}

// Q2 (i): an entry that declares `parent: nx` is a member too, and is read under
// its prefix exactly like a discovered one. The group itself is left unchecked:
// with no discovered members it is offered as an entry of its own, and this test
// is about the members.
func TestDeclaredNexusMembersWalkTheirOwnCatalog(t *testing.T) {
	s, fixtures := nexusFake(t)
	base := s.URL(benchHTTPPort)
	withImageSearcher(t, nexusOnlySearcher(t))

	m := modelWithGroups(t, []config.RegistryItem{
		anonymousNexusGroup(base),
		declaredMember(base, "hosted", "docker-hosted"),
		declaredMember(base, "proxy", "docker-proxy"),
	}, nil)
	m.registryBrowser.selectedRegs["nx"] = false
	m = runLot(t, m, "base", 2)

	wantOnlyMemberCatalogsAsked(t, s, fixtures)
	want := []string{"hosted:docker-hosted/shared/base", "proxy:docker-proxy/shared/base"}
	if got := rowsOf(m); !slices.Equal(sorted(got), want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	wantVerdict(t, m, "hosted", imagesearch.VerdictOK)
	wantVerdict(t, m, "proxy", imagesearch.VerdictOK)
}

// pathServer answers the catalog of each member path with a status and counts
// every request it gets, by path. A path it was not told about answers 404 and is
// counted all the same, so a request to the root shows.
type pathServer struct {
	*httptest.Server
	mu     sync.Mutex
	counts map[string]int
}

func newPathServer(t *testing.T, status map[string]int) *pathServer {
	t.Helper()
	ps := &pathServer{counts: map[string]int{}}
	ps.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ps.mu.Lock()
		ps.counts[r.URL.Path]++
		ps.mu.Unlock()
		code, known := status[r.URL.Path]
		if !known {
			http.NotFound(w, r)
			return
		}
		if code != http.StatusOK {
			http.Error(w, "refused by the fake", code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"repositories":["nginx","nginx-x","other"]}`))
	}))
	t.Cleanup(ps.Close)
	return ps
}

func (p *pathServer) requests(path string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.counts[path]
}

// total counts every request, whatever its path.
func (p *pathServer) total() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, c := range p.counts {
		n += c
	}
	return n
}

// Criterion 6: a member whose own catalog answers an error is that member's
// problem. 404, 401, 403 and 405 are a refusal (this Nexus does not serve a
// catalog there, or not to this account); 5xx and 429 are a failure. The other
// member keeps its verdict and its rows, the root catalog is never asked, and
// the journal says why without the URL (a 401 without a Bearer challenge reads
// as an unexpected challenge, not as a status).
func TestAMemberWhoseCatalogAnswersAnErrorDoesNotSpoilTheOthers(t *testing.T) {
	tests := []struct {
		status  int
		verdict imagesearch.Verdict
		header  string // the header line that names the member
		level   string // the journal level of that verdict
		cause   string // what the journal says about it
	}{
		{http.StatusNotFound, imagesearch.VerdictRefused, "Refused", "INFO", "404"},
		{http.StatusUnauthorized, imagesearch.VerdictRefused, "Refused", "INFO", "auth challenge"},
		{http.StatusForbidden, imagesearch.VerdictRefused, "Refused", "INFO", "403"},
		{http.StatusMethodNotAllowed, imagesearch.VerdictRefused, "Refused", "INFO", "405"},
		{http.StatusServiceUnavailable, imagesearch.VerdictFailed, "Failed", "ERROR", "503"},
		{http.StatusInternalServerError, imagesearch.VerdictFailed, "Failed", "ERROR", "500"},
		{http.StatusTooManyRequests, imagesearch.VerdictFailed, "Failed", "ERROR", "429"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.status), func(t *testing.T) {
			srv := newPathServer(t, map[string]int{"/v2/a/_catalog": tt.status, "/v2/b/_catalog": http.StatusOK})
			withImageSearcher(t, wiredLikeProduction(srv.Server))
			logs := captureLog(t)

			m := runLot(t, nexusMembersModel(t, srv.URL, "a", "b"), "nginx", 2)

			keyA, keyB := memberKey("nx", srv.URL, "a"), memberKey("nx", srv.URL, "b")
			wantVerdict(t, m, keyA, tt.verdict)
			wantVerdict(t, m, keyB, imagesearch.VerdictOK)
			want := []string{keyB + ":b/nginx", keyB + ":b/nginx-x"}
			if got := rowsOf(m); !slices.Equal(sorted(got), want) {
				t.Errorf("rows = %v, want only b's: %v", got, want)
			}
			if n := srv.requests("/v2/_catalog"); n != 0 {
				t.Errorf("the root /v2/_catalog got %d requests, want 0", n)
			}
			if n := srv.requests("/v2/b/_catalog"); n != 1 {
				t.Errorf("/v2/b/_catalog got %d requests, want 1", n)
			}
			if n := srv.requests("/v2/a/_catalog"); n == 0 {
				t.Error("/v2/a/_catalog was never asked")
			}

			_, vals := headerLines(m)
			if !strings.Contains(vals[tt.header], "nx/a") || strings.Contains(vals[tt.header], "nx/b") {
				t.Errorf("%s = %q, want nx/a and not nx/b", tt.header, vals[tt.header])
			}
			if !strings.Contains(vals["Cached only"], "nx/b") {
				t.Errorf("Cached only = %q, want it to list nx/b", vals["Cached only"])
			}

			// The cause is in the journal: readable (the status), with no URL.
			out := logs.String()
			line := tt.level + " [oci_resources] image search on nx/a:"
			if !strings.Contains(out, line) || !strings.Contains(out, tt.cause) {
				t.Errorf("journal = %q, want %q and the cause %q", out, line, tt.cause)
			}
			u, _ := url.Parse(srv.URL)
			for _, leak := range []string{u.Host, "_catalog", "http://"} {
				if strings.Contains(out, leak) {
					t.Errorf("the journal holds %q: %q", leak, out)
				}
			}
		})
	}
}

// A prefix that is not a repository path cannot name a catalog: the member is
// refused with a cause that names the prefix, no request is sent for it, and the
// other member is searched as usual.
func TestAMemberWithAnInvalidPrefixIsRefusedWithoutARequest(t *testing.T) {
	for _, bad := range []string{"Docker-Hosted", "a//b", "../x"} {
		t.Run(bad, func(t *testing.T) {
			srv := newPathServer(t, map[string]int{"/v2/b/_catalog": http.StatusOK})
			withImageSearcher(t, wiredLikeProduction(srv.Server))
			logs := captureLog(t)

			groups := map[string]cache.RegistryGroupEntry{"nx": {Members: []cache.RegistryGroupMember{
				{Alias: "bad", URL: srv.URL, RepoPrefix: bad},
				{Alias: "b", URL: srv.URL, RepoPrefix: "b"},
			}}}
			m := modelWithGroups(t, []config.RegistryItem{anonymousNexusGroup(srv.URL)}, groups)
			m = runLot(t, m, "nginx", 2)

			wantVerdict(t, m, memberKey("nx", srv.URL, bad), imagesearch.VerdictRefused)
			wantVerdict(t, m, memberKey("nx", srv.URL, "b"), imagesearch.VerdictOK)
			if n := srv.total(); n != 1 {
				t.Errorf("the server got %d requests, want 1 (b's catalog only)", n)
			}
			out := logs.String()
			line := "INFO [oci_resources] image search on nx/bad: unsupported or refused:"
			if !strings.Contains(out, line) || !strings.Contains(out, bad) {
				t.Errorf("journal = %q, want %q naming the prefix %q", out, line, bad)
			}
			if strings.Contains(out, "ERROR") {
				t.Errorf("a refusal was logged as an error: %q", out)
			}
			_, vals := headerLines(m)
			if !strings.Contains(vals["Refused"], "nx/bad") {
				t.Errorf("Refused = %q, want it to list nx/bad", vals["Refused"])
			}
		})
	}
}

// What the browser hands the Searcher for each kind of entry: the flag is set
// for the members of a Nexus group (discovered, and declared with `parent:`)
// that have a prefix, and for nothing else. The same predicate as listsCacheOnly.
func TestBuildEntriesFlagsTheCatalogUnderThePrefix(t *testing.T) {
	member := func(alias, prefix string) cache.RegistryGroupMember {
		return cache.RegistryGroupMember{Alias: alias, URL: "nx.example.com", RepoPrefix: prefix}
	}
	harbor := config.RegistryItem{Slug: "hb", Kind: config.KindGroup, Provider: config.ProviderHarbor, URL: "harbor.example.com", Alias: "hb"}
	generic := config.RegistryItem{Slug: "gn", Kind: config.KindGroup, Provider: config.ProviderGeneric, URL: "reg.example.com", Alias: "gn"}
	nexusPlain := config.RegistryItem{Slug: "n2", Kind: config.KindRegistry, Provider: config.ProviderNexus, URL: "n2.example.com", Alias: "n2", RepoPrefix: "p"}
	solo := config.RegistryItem{Slug: "solo", Kind: config.KindRegistry, URL: "solo.example.com", Alias: "solo", RepoPrefix: "p"}
	hub := config.RegistryItem{Slug: "hub", Kind: config.KindRegistry, URL: "docker.io", Alias: "hub"}
	declared := declaredMember("decl.example.com", "decl", "docker-hosted")
	declaredGeneric := config.RegistryItem{Slug: "declg", Kind: config.KindRegistry, Parent: "gn", URL: "declg.example.com", Alias: "declg", RepoPrefix: "p"}
	declaredHarbor := config.RegistryItem{Slug: "declh", Kind: config.KindRegistry, Parent: "hb", URL: "declh.example.com", Alias: "declh", RepoPrefix: "p"}
	declaredOrphan := config.RegistryItem{Slug: "declo", Kind: config.KindRegistry, Parent: "zz", URL: "declo.example.com", Alias: "declo", RepoPrefix: "p"}
	regs := []config.RegistryItem{
		nexusGroup(), harbor, generic, nexusPlain, solo, hub,
		declared, declaredGeneric, declaredHarbor, declaredOrphan,
	}

	tests := []struct {
		name   string
		groups map[string]cache.RegistryGroupEntry
		want   map[string]bool // Alias -> CatalogUnderPrefix
	}{
		{
			name:   "nothing discovered",
			groups: nil,
			want: map[string]bool{
				"nx": false, // the group itself has no prefix of its own
				"hb": false, "gn": false, "n2": false, "solo": false, "hub": false,
				"decl": true, "declg": false, "declh": false, "declo": false,
			},
		},
		{
			name: "members discovered",
			groups: map[string]cache.RegistryGroupEntry{
				"nx": {Members: []cache.RegistryGroupMember{member("nx-a", "docker-hosted"), member("nx-b", "docker-proxy")}},
				"hb": {Members: []cache.RegistryGroupMember{member("hb-a", "library")}},
				"gn": {Members: []cache.RegistryGroupMember{member("gn-a", "team")}},
			},
			want: map[string]bool{
				"nx-a": true, "nx-b": true, "hb-a": false, "gn-a": false,
				"n2": false, "solo": false, "hub": false,
				"decl": true, "declg": false, "declh": false, "declo": false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newRegistryBrowser(regs, tt.groups, nil, 120, 40)
			got := map[string]bool{}
			for _, e := range b.entries {
				tg := b.targetFor(e)
				got[e.Alias] = tg.CatalogUnderPrefix
				if tg.RepoPrefix != e.repoPrefix {
					t.Errorf("%s: Target.RepoPrefix = %q, want %q", e.Alias, tg.RepoPrefix, e.repoPrefix)
				}
			}
			if len(got) != len(tt.want) {
				t.Fatalf("entries = %v, want aliases %v", got, tt.want)
			}
			for alias, want := range tt.want {
				if g, ok := got[alias]; !ok || g != want {
					t.Errorf("%s: CatalogUnderPrefix = %v (present %v), want %v", alias, g, ok, want)
				}
			}
		})
	}
}

// The flag reaches the Searcher through a real lot: startImageSearch builds each
// Target with targetFor, so what the Searcher sees is what the user would get.
func TestTheLotHandsTheSearcherTheCatalogUnderPrefixFlag(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]imagesearch.Target{}
	withImageSearcher(t, imagesearch.SearcherFunc(func(_ context.Context, tg imagesearch.Target, _ imagesearch.Query) ([]imagesearch.Image, bool, error) {
		mu.Lock()
		seen[tg.EntryKey] = tg
		mu.Unlock()
		return nil, false, nil
	}))

	const base = "http://nexus.invalid"
	items := []config.RegistryItem{
		anonymousNexusGroup(base),
		declaredMember(base, "decl", "docker-proxy"),
		genericEntry(base, "solo", "docker-hosted"),
	}
	m := modelWithGroups(t, items, discoveredMembers(base, "docker-hosted"))
	runLot(t, m, "nginx", 3)

	for key, want := range map[string]bool{
		memberKey("nx", base, "docker-hosted"): true,  // discovered member
		"decl":                                 true,  // declared member
		"solo":                                 false, // standalone, same host
	} {
		mu.Lock()
		tg, ok := seen[key]
		mu.Unlock()
		if !ok {
			t.Errorf("the Searcher never saw %s", key)
			continue
		}
		if tg.CatalogUnderPrefix != want {
			t.Errorf("%s: Target.CatalogUnderPrefix = %v, want %v", key, tg.CatalogUnderPrefix, want)
		}
	}
}
