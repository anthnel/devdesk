package ociresources

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-579 (NX-12), step 6, decision Q3 = 3-A: a Nexus group on a connector of
// its own (a dedicated HTTP port) answers /v2/_catalog with the UNION of its
// members' names and no attribution (measured, Nexus 3.98.1-01, fixtures
// catalog-group-connector-30482--*). Its discovered members have no address of
// their own (the group's URL, no prefix), so the search treats them as ONE unit:
// the group. One walk, rows under the group's label, no duplicate, no name
// attributed to a member.
//
// The label of that unit is groupUnitSuffix appended to the group's label
// ("nx (group)"). It is one constant of the production code, and every assertion
// below reads it from there, so the label the UX settles on changes in one place.
// It shows in: the Registry column of the level 1 rows, the header lines that
// name a registry (Cached only, Failed, Refused, No result), the `r` filter
// token, the Registry column of the tag table a row opens, and the journal.
// An undiscovered group is the group itself, offered under its own plain label.

// benchConnectorPort is the recorded Host of the group's Docker connector.
const benchConnectorPort = "127.0.0.1:30482"

// groupUnitLabel is what the unit of group slug is labelled with.
func groupUnitLabel(alias string) string { return alias + groupUnitSuffix }

// benchGroupAs is the Nexus group "nx" at base, read as id: anonymous sends
// nothing, any other identity is the bench account stored for base's host in the
// docker config, the way the browser finds a login.
func benchGroupAs(t *testing.T, id nexusfixture.Identity, base string) config.RegistryItem {
	t.Helper()
	g := nexusGroup()
	g.URL = base
	if id == nexusfixture.Anonymous {
		g.AuthMode = config.AuthAnonymous
		return g
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	writeDockerConfig(t, map[string]any{
		"auths": map[string]any{u.Host: map[string]string{"auth": encodeAuth(fakenexus.User(id), fakenexus.Password)}},
	})
	g.AuthMode = config.AuthCredentials
	return g
}

// connectorMembers are members of "nx" the way discovery stores them for a group
// on a connector of its own: the group's URL and no prefix, whatever the member.
func connectorMembers(base string, aliases ...string) map[string]cache.RegistryGroupEntry {
	var members []cache.RegistryGroupMember
	for _, a := range aliases {
		members = append(members, cache.RegistryGroupMember{Alias: a, URL: base})
	}
	return map[string]cache.RegistryGroupEntry{"nx": {Members: members}}
}

// discoveredGroupModel opens the browser on the group "nx" read as id, on the
// recorded connector, with two discovered members that have no address of their own.
func discoveredGroupModel(t *testing.T, id nexusfixture.Identity) (Model, *fakenexus.Server, []nexusfixture.Fixture, string) {
	t.Helper()
	s, fixtures := nexusFake(t)
	base := s.URL(benchConnectorPort)
	withImageSearcher(t, nexusOnlySearcher(t))
	m := modelWithGroups(t, []config.RegistryItem{benchGroupAs(t, id, base)}, connectorMembers(base, "docker-hosted", "docker-proxy"))
	return m, s, fixtures, base
}

// runLotAll submits text, runs every search of the lot and feeds the answers back.
// It returns the answers, so a test counts the walks the lot really made (a lot of
// one search is not a batch, which runLot cannot tell).
func runLotAll(t *testing.T, m Model, text string) (Model, []ImageSearchResultMsg) {
	t.Helper()
	m, cmd := submitRaw(t, m, text)
	if cmd == nil || m.registryBrowser.state != browserStateImages {
		t.Fatalf("the search did not start (state %d)", m.registryBrowser.state)
	}
	var results []ImageSearchResultMsg
	for _, msg := range testutil.Msgs(cmd) {
		if r, ok := msg.(ImageSearchResultMsg); ok {
			results = append(results, r)
		}
	}
	for _, r := range results {
		m = feed(t, m, r)
	}
	return m, results
}

// wantExactHits checks the requests the fake answered, fixture by fixture: each
// named fixture the given number of times, every other one never.
func wantExactHits(t *testing.T, s *fakenexus.Server, fixtures []nexusfixture.Fixture, want map[string]int) {
	t.Helper()
	known := map[string]bool{}
	for _, f := range fixtures {
		known[f.Name] = true
		if got := s.Hits(f.Name); got != want[f.Name] {
			t.Errorf("Hits(%s) = %d, want %d", f.Name, got, want[f.Name])
		}
	}
	for name := range want {
		if !known[name] {
			t.Fatalf("the test names an unknown fixture %q", name)
		}
	}
}

// searchRows are the level 1 rows: label, reference and image, in table order.
func searchRows(m Model) []searchRow { return m.registryBrowser.searchTable.Visible() }

// answerKeys are the entry keys the answers of a lot came for.
func answerKeys(results []ImageSearchResultMsg) []string {
	var keys []string
	for _, r := range results {
		keys = append(keys, r.EntryKey)
	}
	return keys
}

func repoKeys(m Model) []string { return sorted(rowsOf(m)) }

// ---- searchUnits (pure) ----------------------------------------------------

// connectorTestGroup is a group of the given provider at url, with a prefix of
// its own when prefix is not empty.
func connectorTestGroup(slug, url, prefix, provider string) config.RegistryItem {
	return config.RegistryItem{
		Slug: slug, Alias: slug, Kind: config.KindGroup, Provider: provider,
		URL: url, RepoPrefix: prefix, AuthMode: config.AuthAnonymous,
	}
}

// groupMemberEntry is the picker entry buildEntries makes for a discovered member.
func groupMemberEntry(g config.RegistryItem, alias, url, prefix string) browserRegistryEntry {
	return browserRegistryEntry{
		URL: url, Alias: alias, repoPrefix: prefix, Slug: g.Slug,
		ParentAlias: browserAlias(g), ParentSlug: g.Slug, parentURL: g.URL,
		authMode: g.AuthMode, cachedOnly: config.IsNexusGroup(g),
		catalogUnderPrefix: config.IsNexusGroup(g) && prefix != "",
		key:                memberKey(g.Slug, url, prefix),
	}
}

// TestSearchUnits pins searchUnits: it turns the entries a search is asked on into what it walks: the
// members of a group that share the group's address (its URL and its prefix, so
// the same catalog under the same identity) are replaced by ONE entry, the group,
// standing where the first of them stood. Everything else is kept as it is, in
// order. It is pure: same input, same output, the input untouched.
func TestSearchUnits(t *testing.T) {
	const gURL = "nexus.example.com:5001"
	nx := connectorTestGroup("nx", gURL, "", config.ProviderNexus)
	ob := connectorTestGroup("ob", "other.example.com:5002", "", config.ProviderNexus)
	gn := connectorTestGroup("gn", "reg.example.com", "", config.ProviderGeneric)
	pg := connectorTestGroup("pg", "p.example.com:5003", "p", config.ProviderNexus)
	groups := map[string]config.RegistryItem{"nx": nx, "ob": ob, "gn": gn, "pg": pg}

	a := groupMemberEntry(nx, "docker-hosted", gURL, "")
	b := groupMemberEntry(nx, "docker-proxy", gURL, "")
	p1 := groupMemberEntry(nx, "docker-hosted", gURL, "docker-hosted")
	p2 := groupMemberEntry(nx, "docker-proxy", gURL, "docker-proxy")
	elsewhere := groupMemberEntry(nx, "docker-other", "elsewhere.example.com", "")
	orphan := groupMemberEntry(config.RegistryItem{Slug: "zz", Alias: "zz"}, "docker-hosted", gURL, "")
	obA := groupMemberEntry(ob, "docker-hosted", ob.URL, "")
	obB := groupMemberEntry(ob, "docker-proxy", ob.URL, "")
	gnA := groupMemberEntry(gn, "team-a", gn.URL, "")
	gnB := groupMemberEntry(gn, "team-b", gn.URL, "")
	pgOwn := groupMemberEntry(pg, "own", pg.URL, "p")
	pgBare := groupMemberEntry(pg, "bare", pg.URL, "")
	// What buildEntries makes of a group nobody discovered (the group itself) and
	// of standalone registries: none of them is a member.
	groupAlone := browserRegistryEntry{URL: gURL, Alias: "nx", Slug: "nx", key: "nx", cachedOnly: true, authMode: config.AuthAnonymous}
	soloSameHost := browserRegistryEntry{URL: gURL, Alias: "solo", Slug: "solo", key: "solo", authMode: config.AuthAnonymous}
	soloPrefixed := browserRegistryEntry{URL: gURL, Alias: "other", Slug: "other", key: "other", repoPrefix: "p", authMode: config.AuthAnonymous}
	hub := browserRegistryEntry{URL: "docker.io", Alias: "hub", Slug: "hub", key: "hub", authMode: config.AuthAnonymous}

	tests := []struct {
		name    string
		entries []browserRegistryEntry
		// wantKeys is the keys of the result, in order; wantUnits the ones among
		// them that are a group unit (not an entry of the input).
		wantKeys  []string
		wantUnits []string
	}{
		{name: "two members with no address of their own are one unit", entries: []browserRegistryEntry{a, b}, wantKeys: []string{"nx"}, wantUnits: []string{"nx"}},
		{name: "one member with no address of its own is still the group", entries: []browserRegistryEntry{a}, wantKeys: []string{"nx"}, wantUnits: []string{"nx"}},
		{name: "members that each have a path stay as they are", entries: []browserRegistryEntry{p1, p2}, wantKeys: []string{p1.key, p2.key}},
		{name: "a mix: the unit stands where the first bare member stood", entries: []browserRegistryEntry{a, p1, b}, wantKeys: []string{"nx", p1.key}, wantUnits: []string{"nx"}},
		{name: "a mix with path members on both sides", entries: []browserRegistryEntry{p1, a, p2, b}, wantKeys: []string{p1.key, "nx", p2.key}, wantUnits: []string{"nx"}},
		{name: "a group nobody discovered is not a member", entries: []browserRegistryEntry{groupAlone}, wantKeys: []string{"nx"}},
		{name: "generic entries are untouched, even on the group's address", entries: []browserRegistryEntry{hub, soloSameHost, soloPrefixed}, wantKeys: []string{"hub", "solo", "other"}},
		{name: "an undiscovered group and a standalone entry beside a discovered one", entries: []browserRegistryEntry{hub, a, b, soloSameHost}, wantKeys: []string{"hub", "nx", "solo"}, wantUnits: []string{"nx"}},
		{name: "members of two groups are two units, by first appearance", entries: []browserRegistryEntry{a, obA, b, obB}, wantKeys: []string{"nx", "ob"}, wantUnits: []string{"nx", "ob"}},
		{name: "a member whose group is unknown is untouched", entries: []browserRegistryEntry{orphan}, wantKeys: []string{orphan.key}},
		{name: "a member on another host than its group has an address of its own", entries: []browserRegistryEntry{elsewhere, a}, wantKeys: []string{elsewhere.key, "nx"}, wantUnits: []string{"nx"}},
		{name: "the group's prefix is part of its address", entries: []browserRegistryEntry{pgOwn, pgBare}, wantKeys: []string{"pg", pgBare.key}, wantUnits: []string{"pg"}},
		{name: "rule is the same address, not Nexus", entries: []browserRegistryEntry{gnA, gnB}, wantKeys: []string{"gn"}, wantUnits: []string{"gn"}},
		{name: "no entry", entries: nil, wantKeys: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := slices.Clone(tt.entries)
			got := searchUnits(tt.entries, groups)

			var keys []string
			for _, e := range got {
				keys = append(keys, e.key)
			}
			if !slices.Equal(keys, tt.wantKeys) {
				t.Fatalf("keys = %v, want %v", keys, tt.wantKeys)
			}
			if !slices.Equal(tt.entries, in) {
				t.Errorf("the input was modified: %v, was %v", tt.entries, in)
			}
			if again := searchUnits(tt.entries, groups); !slices.Equal(again, got) {
				t.Errorf("a second call differs: %v, then %v", again, got)
			}

			var units []string
			for _, e := range got {
				if !slices.Contains(tt.entries, e) {
					units = append(units, e.key)
					checkGroupUnit(t, e, groups[e.key], tt.entries)
				}
			}
			if !slices.Equal(units, tt.wantUnits) {
				t.Errorf("group units = %v, want %v", units, tt.wantUnits)
			}
		})
	}
}

// checkGroupUnit checks what a unit is: the group's own address, resolved the way
// the group itself would be (its credentials, its key), marked with the label.
func checkGroupUnit(t *testing.T, unit browserRegistryEntry, g config.RegistryItem, input []browserRegistryEntry) {
	t.Helper()
	var first browserRegistryEntry
	for _, e := range input {
		if e.ParentSlug == g.Slug {
			first = e
			break
		}
	}
	if unit.key != g.Slug || unit.Slug != g.Slug {
		t.Errorf("unit key = %q, Slug = %q, want both %q", unit.key, unit.Slug, g.Slug)
	}
	if unit.URL != g.URL || unit.repoPrefix != g.RepoPrefix {
		t.Errorf("unit address = (%q, %q), want the group's (%q, %q)", unit.URL, unit.repoPrefix, g.URL, g.RepoPrefix)
	}
	// The group-level `r` stop (resultFilter.groupSlug) must still take the unit's rows.
	if unit.ParentSlug != g.Slug || unit.ParentAlias != "" {
		t.Errorf("unit ParentSlug = %q, ParentAlias = %q, want %q and none", unit.ParentSlug, unit.ParentAlias, g.Slug)
	}
	if unit.authMode != first.authMode || unit.cachedOnly != first.cachedOnly {
		t.Errorf("unit authMode = %q, cachedOnly = %v, want its members' (%q, %v)", unit.authMode, unit.cachedOnly, first.authMode, first.cachedOnly)
	}
	if unit.catalogUnderPrefix {
		t.Error("a unit at the group's own address is read at the root, never under a prefix")
	}
	if got, want := entryAlias(unit), groupUnitLabel(browserAlias(g)); got != want {
		t.Errorf("unit label = %q, want %q", got, want)
	}
}

// ---- the group's connector, discovered ------------------------------------

// The core of 3-A, on the measured answer of the connector: two discovered
// members with no address of their own are searched as ONE unit. One walk (one
// recorded request), one verdict under the group's key, rows under the group's
// label, no duplicate, and no name attributed to a member.
func TestDiscoveredGroupMembersOnTheConnectorAreSearchedAsTheGroup(t *testing.T) {
	tests := []struct {
		name  string
		query string
		repos []string // the union, as the connector gave it
	}{
		{"a name only the hosted repository holds", "api", []string{"api", "team/api"}},
		{"a name both repositories hold is listed once", "base", []string{"shared/base"}},
		{"a name only the proxy repository holds", "mirror", []string{"mirror/cached", "mirror/uncached"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, s, fixtures, base := discoveredGroupModel(t, nexusfixture.Admin)
			label := groupUnitLabel("nx")

			m, results := runLotAll(t, m, tt.query)

			// One walk: one answer, one request, on the connector's root catalog.
			if len(results) != 1 || results[0].EntryKey != "nx" {
				t.Fatalf("%d answers, for %v; want exactly one, for the group's key \"nx\"", len(results), answerKeys(results))
			}
			wantExactHits(t, s, fixtures, map[string]int{"page-group-n1000--admin": 1})

			var want []string
			for _, r := range tt.repos {
				want = append(want, "nx:"+r)
			}
			if got := repoKeys(m); !slices.Equal(got, sorted(want)) {
				t.Errorf("rows = %v, want %v (under the group, no member)", got, sorted(want))
			}
			u, _ := url.Parse(base)
			for _, r := range searchRows(m) {
				if r.label != label {
					t.Errorf("row %s: label = %q, want the group's %q", r.img.Repo, r.label, label)
				}
				if want := u.Host + "/" + r.img.Repo; r.ref != want {
					t.Errorf("row %s: reference = %q, want %q (the group's address, pullable)", r.img.Repo, r.ref, want)
				}
			}

			// One verdict, under the group's key; the members have none.
			outcomes := m.RegistryBrowserOutcomes()
			if len(outcomes) != 1 {
				t.Errorf("outcomes = %v, want only the group's", outcomes)
			}
			wantVerdict(t, m, "nx", imagesearch.VerdictOK)

			// Header: the count of the rows, and the unit named once as cached only.
			keys, vals := headerLines(m)
			if want := []string{"Images", "Cached only"}; !slices.Equal(keys, want) {
				t.Errorf("header keys = %v, want %v", keys, want)
			}
			if vals["Images"] != strconv.Itoa(len(tt.repos)) {
				t.Errorf("Images = %q, want %d", vals["Images"], len(tt.repos))
			}
			if vals["Cached only"] != label {
				t.Errorf("Cached only = %q, want %q once, not once per member", vals["Cached only"], label)
			}

			// The picker is not the search: it still shows the group and its two members.
			if n := len(m.registryBrowser.entries); n != 2 {
				t.Errorf("the picker holds %d entries, want its 2 members", n)
			}
		})
	}
}

// The label of the unit is on the screen, in the Registry column, and a member's
// label (nx/docker-hosted) is nowhere: the lines are not attributed to a member.
func TestTheGroupUnitLabelIsOnTheScreen(t *testing.T) {
	m, _, _, _ := discoveredGroupModel(t, nexusfixture.Admin)
	m, _ = runLotAll(t, m, "api")

	screen := ansi.Strip(m.Render())
	label := groupUnitLabel("nx")
	if n := strings.Count(screen, label); n != 2 {
		t.Errorf("the screen shows %q %d times, want once per row (2):\n%s", label, n, screen)
	}
	for _, member := range []string{"nx/docker-hosted", "nx/docker-proxy"} {
		if strings.Contains(screen, member) {
			t.Errorf("the screen names the member %q, which the answer does not attribute anything to:\n%s", member, screen)
		}
	}
}

// A refusal or a failure of the connector is one verdict, under the group's label,
// from one request: not one per member, not twice in the header.
func TestAFailingGroupConnectorIsOneVerdictUnderTheGroupLabel(t *testing.T) {
	tests := []struct {
		status  int
		verdict imagesearch.Verdict
		header  string
	}{
		{http.StatusNotFound, imagesearch.VerdictRefused, "Refused"},
		{http.StatusServiceUnavailable, imagesearch.VerdictFailed, "Failed"},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			srv := newPathServer(t, map[string]int{"/v2/_catalog": tt.status})
			withImageSearcher(t, wiredLikeProduction(srv.Server))

			m := modelWithGroups(t, []config.RegistryItem{anonymousNexusGroup(srv.URL)}, connectorMembers(srv.URL, "docker-hosted", "docker-proxy"))
			m, results := runLotAll(t, m, "nginx")

			if len(results) != 1 {
				t.Fatalf("answers = %d, want 1 (one unit)", len(results))
			}
			if n := srv.requests("/v2/_catalog"); n != 1 || srv.total() != 1 {
				t.Errorf("the connector got %d requests on the root, %d in all, want 1", n, srv.total())
			}
			wantVerdict(t, m, "nx", tt.verdict)
			if got := m.RegistryBrowserOutcomes(); len(got) != 1 {
				t.Errorf("outcomes = %v, want only the group's", got)
			}
			_, vals := headerLines(m)
			if want := groupUnitLabel("nx"); vals[tt.header] != want {
				t.Errorf("%s = %q, want %q", tt.header, vals[tt.header], want)
			}
			if rows := rowsOf(m); len(rows) != 0 {
				t.Errorf("rows = %v, want none", rows)
			}
		})
	}
}

// → on a row of the unit lists the tags of the group itself: the connector, the
// row's repository as is, the group's credentials, and the tag table carries the
// unit's label (the unit is gone from the picker, so the lot must still resolve it).
func TestArrowOnAGroupUnitRowListsTheGroupsTags(t *testing.T) {
	m, _, _, base := discoveredGroupModel(t, nexusfixture.Admin)
	rec := withFullTagRecorder(t)
	m, _ = runLotAll(t, m, "api")

	if r, ok := m.registryBrowser.searchTable.Selected(); ok && r.img.Repo != "api" {
		m = feed(t, m, testutil.Key("down"))
	}
	row, ok := m.registryBrowser.searchTable.Selected()
	if !ok || row.img.Repo != "api" {
		t.Fatalf("selected row = %+v (%v), want the api row", row.img, ok)
	}
	m, cmd := step(t, m, testutil.Key("right"))
	m = feed(t, m, testutil.Msgs(cmd)...)

	if m.registryBrowser.state != browserStateTags {
		t.Fatalf("state = %d, want the tag table (%d)", m.registryBrowser.state, browserStateTags)
	}
	rec.mu.Lock()
	calls := slices.Clone(rec.calls)
	rec.mu.Unlock()
	want := fullTagCall{apiURL: base, repo: "api", user: fakenexus.User(nexusfixture.Admin), pass: fakenexus.Password}
	if len(calls) != 1 || calls[0] != want {
		t.Errorf("tag requests = %+v, want exactly %+v", calls, want)
	}
	tags := m.registryBrowser.tagTable.Visible()
	if len(tags) == 0 {
		t.Fatal("the tag table is empty")
	}
	for _, r := range tags {
		if r.label != groupUnitLabel("nx") || r.tag.EntryKey != "nx" {
			t.Errorf("tag row = label %q, key %q, want %q and \"nx\"", r.label, r.tag.EntryKey, groupUnitLabel("nx"))
		}
	}
}

// A unit lives as long as its lot. A new search that has no unit leaves none behind
// in entryFor (which every label, `r` stop and → resolves through), a new search
// that has one makes it again, and the picker never held it.
func TestAGroupUnitBelongsToItsLotOnly(t *testing.T) {
	m, _, _, _ := discoveredGroupModel(t, nexusfixture.Admin)
	m, _ = runLotAll(t, m, "api")
	b := m.registryBrowser

	e := b.entryFor("nx")
	if e == nil {
		t.Fatal("during its lot the unit does not resolve")
	}
	if got := entryAlias(*e); got != groupUnitLabel("nx") {
		t.Errorf("the unit resolves to %q, want %q", got, groupUnitLabel("nx"))
	}
	if slices.ContainsFunc(b.entries, func(e browserRegistryEntry) bool { return e.key == "nx" }) {
		t.Error("the unit leaked into the picker's entries")
	}

	b.startImageSearch("api", nil)
	if e := b.entryFor("nx"); e != nil {
		t.Errorf("a new lot without that unit still resolves it: %+v", *e)
	}

	b.startImageSearch("api", slices.Clone(b.entries))
	if b.entryFor("nx") == nil {
		t.Error("a new lot with the members does not make the unit again")
	}
}

// ---- the group's connector, not discovered --------------------------------

// A group nobody discovered (the pull account is refused the discovery, DEVDESK-578)
// is offered as itself: one walk of the connector's root, rows under the group's
// plain label (it is the group, not a stand-in for members), a reference that
// pulls. Frozen on the measured answer of the connector for the pull account.
func TestAnUndiscoveredGroupOnTheConnectorIsSearchedAsItself(t *testing.T) {
	tests := []struct {
		name  string
		query string
		repos []string
	}{
		{"hosted names", "api", []string{"api", "team/api"}},
		{"a shared name once", "base", []string{"shared/base"}},
		{"proxy names", "mirror", []string{"mirror/cached", "mirror/uncached"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, fixtures := nexusFake(t)
			base := s.URL(benchConnectorPort)
			withImageSearcher(t, nexusOnlySearcher(t))
			m := modelWithGroups(t, []config.RegistryItem{benchGroupAs(t, nexusfixture.Pull, base)}, nil)

			m, results := runLotAll(t, m, tt.query)

			if len(results) != 1 || results[0].EntryKey != "nx" {
				t.Fatalf("%d answers, for %v; want exactly one, for \"nx\"", len(results), answerKeys(results))
			}
			wantExactHits(t, s, fixtures, map[string]int{"catalog-group-connector-30482--pull": 1})

			var want []string
			for _, r := range tt.repos {
				want = append(want, "nx:"+r)
			}
			if got := repoKeys(m); !slices.Equal(got, sorted(want)) {
				t.Errorf("rows = %v, want %v", got, sorted(want))
			}
			u, _ := url.Parse(base)
			for _, r := range searchRows(m) {
				if r.label != "nx" {
					t.Errorf("row %s: label = %q, want the plain group label \"nx\"", r.img.Repo, r.label)
				}
				if want := u.Host + "/" + r.img.Repo; r.ref != want {
					t.Errorf("row %s: reference = %q, want %q", r.img.Repo, r.ref, want)
				}
			}
			wantVerdict(t, m, "nx", imagesearch.VerdictOK)
			keys, vals := headerLines(m)
			if want := []string{"Images", "Cached only"}; !slices.Equal(keys, want) {
				t.Errorf("header keys = %v, want %v", keys, want)
			}
			if vals["Cached only"] != "nx" {
				t.Errorf("Cached only = %q, want \"nx\"", vals["Cached only"])
			}
		})
	}
}

// ---- mixed lots, and members that keep their path -------------------------

// A group whose discovered members are a mix: two with a path of their own (the
// REST port) and two with none (the connector). Each member with a path keeps its
// own walk under its prefix (step 5, unchanged), the other two are one unit: three
// walks, three verdicts, and each member's reference is its own.
func mixedGroupModel(t *testing.T) (Model, *fakenexus.Server, []nexusfixture.Fixture, string, string) {
	t.Helper()
	s, fixtures := nexusFake(t)
	connector, rest := s.URL(benchConnectorPort), s.URL(benchHTTPPort)
	withImageSearcher(t, nexusOnlySearcher(t))
	groups := map[string]cache.RegistryGroupEntry{"nx": {Members: []cache.RegistryGroupMember{
		{Alias: "docker-hosted", URL: rest, RepoPrefix: "docker-hosted"},
		{Alias: "docker-proxy", URL: rest, RepoPrefix: "docker-proxy"},
		{Alias: "legacy-a", URL: connector},
		{Alias: "legacy-b", URL: connector},
	}}}
	m := modelWithGroups(t, []config.RegistryItem{benchGroupAs(t, nexusfixture.Admin, connector)}, groups)
	return m, s, fixtures, connector, rest
}

func TestMembersWithAPathKeepTheirOwnWalkBesideTheGroupUnit(t *testing.T) {
	m, s, fixtures, _, rest := mixedGroupModel(t)
	hosted, proxy := memberKey("nx", rest, "docker-hosted"), memberKey("nx", rest, "docker-proxy")

	m, results := runLotAll(t, m, "base")

	got := answerKeys(results)
	if want := sorted([]string{hosted, proxy, "nx"}); !slices.Equal(sorted(got), want) {
		t.Fatalf("answers came for %v, want one each for %v", got, want)
	}
	wantExactHits(t, s, fixtures, map[string]int{
		"catalog-hosted-path--admin": 1, "catalog-proxy-path--admin": 1, "page-group-n1000--admin": 1,
	})
	want := sorted([]string{hosted + ":docker-hosted/shared/base", proxy + ":docker-proxy/shared/base", "nx:shared/base"})
	if rows := repoKeys(m); !slices.Equal(rows, want) {
		t.Errorf("rows = %v, want %v", rows, want)
	}
	for _, k := range []string{hosted, proxy, "nx"} {
		wantVerdict(t, m, k, imagesearch.VerdictOK)
	}
	// The summary, not the header: the header abbreviates a long list ("+2").
	if got, want := m.registryBrowser.searchSummary().CachedOnly, []string{"nx/docker-hosted", "nx/docker-proxy", groupUnitLabel("nx")}; !slices.Equal(got, want) {
		t.Errorf("Cached only = %v, want %v", got, want)
	}
}

// `r` cycles over the units of the lot: the group (which takes the unit's rows
// too), each member with a path, then the unit under its own label, then off.
func TestTheRegistryFilterStopsAtTheGroupUnit(t *testing.T) {
	m, _, _, _, rest := mixedGroupModel(t)
	hosted, proxy := memberKey("nx", rest, "docker-hosted"), memberKey("nx", rest, "docker-proxy")
	m, _ = runLotAll(t, m, "base")
	b := m.registryBrowser

	stops := []struct {
		label string
		rows  []string
	}{
		{"nx", sorted([]string{hosted + ":docker-hosted/shared/base", proxy + ":docker-proxy/shared/base", "nx:shared/base"})},
		{"nx/docker-hosted", []string{hosted + ":docker-hosted/shared/base"}},
		{"nx/docker-proxy", []string{proxy + ":docker-proxy/shared/base"}},
		{groupUnitLabel("nx"), []string{"nx:shared/base"}},
	}
	for i, stop := range stops {
		m = feed(t, m, testutil.Key("r"))
		if got := repoKeys(m); !slices.Equal(got, stop.rows) {
			t.Errorf("stop %d (%s): rows = %v, want %v", i, stop.label, got, stop.rows)
		}
		if got := b.filterLabel(b.searchFilter); got != stop.label {
			t.Errorf("stop %d: label = %q, want %q", i, got, stop.label)
		}
	}
	m = feed(t, m, testutil.Key("r"))
	if !b.searchFilter.isEmpty() || len(rowsOf(m)) != 3 {
		t.Errorf("after the last stop: filter = %+v, rows = %v, want off and 3 rows", b.searchFilter, rowsOf(m))
	}
}

// Non-regression (step 5): members that all have a path are NOT collapsed. Each is
// one walk of its own catalog under its prefix, with its own key and reference.
func TestPrefixedMembersAreNotCollapsedIntoAGroupUnit(t *testing.T) {
	s, fixtures := nexusFake(t)
	base := s.URL(benchHTTPPort)
	withImageSearcher(t, nexusOnlySearcher(t))

	m, results := runLotAll(t, nexusMembersModel(t, base, "docker-hosted", "docker-proxy"), "base")

	got := answerKeys(results)
	hosted, proxy := memberKey("nx", base, "docker-hosted"), memberKey("nx", base, "docker-proxy")
	if want := sorted([]string{hosted, proxy}); !slices.Equal(sorted(got), want) {
		t.Fatalf("answers came for %v, want one per member %v", got, want)
	}
	wantOnlyMemberCatalogsAsked(t, s, fixtures)
	want := sorted([]string{hosted + ":docker-hosted/shared/base", proxy + ":docker-proxy/shared/base"})
	if rows := repoKeys(m); !slices.Equal(rows, want) {
		t.Errorf("rows = %v, want %v", rows, want)
	}
	for _, r := range searchRows(m) {
		if strings.Contains(r.label, groupUnitSuffix) {
			t.Errorf("row %s carries the group unit label %q", r.img.Repo, r.label)
		}
	}
	if b := m.registryBrowser; b.entryFor("nx") != nil {
		t.Error("a lot of path members made a group unit")
	}
}
