package ociresources

import (
	"errors"
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func nexusGroup() config.RegistryItem {
	return config.RegistryItem{Slug: "nx", Kind: config.KindGroup, Provider: config.ProviderNexus, URL: "nexus.example.com", Alias: "nx"}
}

// DEVDESK-308 (IS-14), step 1, renamed by DEVDESK-629 (F2): which picker entries
// belong to a Nexus group: the group, its discovered members and an entry declaring
// `parent:` towards a Nexus group (Q-B ii). It still drives catalogUnderPrefix; it
// no longer says anything about what an entry lists.
func TestIsNexusGroupOrMember(t *testing.T) {
	harbor := config.RegistryItem{Slug: "hb", Kind: config.KindGroup, Provider: config.ProviderHarbor, URL: "harbor.example.com"}
	generic := config.RegistryItem{Slug: "gn", Kind: config.KindGroup, Provider: config.ProviderGeneric, URL: "reg.example.com"}
	bySlug := map[string]config.RegistryItem{"nx": nexusGroup(), "hb": harbor, "gn": generic}

	tests := []struct {
		name string
		reg  config.RegistryItem
		want bool
	}{
		{"nexus group", nexusGroup(), true},
		{"declared parent to nexus group", config.RegistryItem{Slug: "m", Kind: config.KindRegistry, Parent: "nx"}, true},
		{"harbor group", harbor, false},
		{"generic group", generic, false},
		{"nexus registry without parent", config.RegistryItem{Slug: "n2", Kind: config.KindRegistry, Provider: config.ProviderNexus}, false},
		{"docker hub", config.RegistryItem{Slug: "hub", Kind: config.KindRegistry, URL: "docker.io"}, false},
		{"parent to generic group", config.RegistryItem{Slug: "m2", Kind: config.KindRegistry, Parent: "gn"}, false},
		{"parent to harbor group", config.RegistryItem{Slug: "m3", Kind: config.KindRegistry, Parent: "hb"}, false},
		{"parent to unknown slug", config.RegistryItem{Slug: "m4", Kind: config.KindRegistry, Parent: "zz"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNexusGroupOrMember(tt.reg, bySlug); got != tt.want {
				t.Fatalf("isNexusGroupOrMember(%+v) = %v, want %v", tt.reg, got, tt.want)
			}
		})
	}
}

// H5: the role of an entry. A member discovered for a Nexus group takes the role of
// the type discovery read ("" or anything unexpected: unknown). The group itself,
// when nobody discovered it, is a group. An entry that declares a Nexus group as its
// parent takes the type of the discovered member with the SAME address (URL and
// prefix, exactly), else it is unknown. Nothing outside a Nexus group has a role.
func TestBuildEntriesSetsTheNexusRole(t *testing.T) {
	const host = "nexus.example.com"
	harbor := config.RegistryItem{Slug: "hb", Kind: config.KindGroup, Provider: config.ProviderHarbor, URL: "harbor.example.com", Alias: "hb"}
	generic := config.RegistryItem{Slug: "gn", Kind: config.KindGroup, Provider: config.ProviderGeneric, URL: "reg.example.com", Alias: "gn"}
	nexusPlain := config.RegistryItem{Slug: "n2", Kind: config.KindRegistry, Provider: config.ProviderNexus, URL: "n2.example.com", Alias: "n2"}
	hub := config.RegistryItem{Slug: "hub", Kind: config.KindRegistry, URL: "docker.io", Alias: "hub"}
	declaredAs := func(slug, url, prefix, parent string) config.RegistryItem {
		return config.RegistryItem{Slug: slug, Alias: slug, Kind: config.KindRegistry, Parent: parent, URL: url, RepoPrefix: prefix}
	}
	member := func(alias, url, prefix, typ string) cache.RegistryGroupMember {
		return cache.RegistryGroupMember{Alias: alias, URL: url, RepoPrefix: prefix, Type: typ}
	}
	nx := nexusGroup()
	nx.URL = host

	tests := []struct {
		name   string
		regs   []config.RegistryItem
		groups map[string]cache.RegistryGroupEntry
		want   map[string]nexusRole // Alias -> role
	}{
		{
			name:   "a group nobody discovered is a group, a declared member has no type to read",
			regs:   []config.RegistryItem{nx, harbor, generic, nexusPlain, hub, declaredAs("decl", "decl.example.com", "", "nx"), declaredAs("declg", "declg.example.com", "", "gn")},
			groups: nil,
			want: map[string]nexusRole{
				"nx": nexusRoleGroup, "hb": nexusRoleUnknown, "gn": nexusRoleUnknown, "n2": nexusRoleUnknown,
				"hub": nexusRoleUnknown, "decl": nexusRoleUnknown, "declg": nexusRoleUnknown,
			},
		},
		{
			name: "discovered members take the role of their type",
			regs: []config.RegistryItem{nx, harbor, generic},
			groups: map[string]cache.RegistryGroupEntry{
				"nx": {Members: []cache.RegistryGroupMember{
					member("hosted", host, "hosted", "hosted"),
					member("proxy", host, "proxy", "proxy"),
					member("nested", host, "nested", "group"),
					member("old", host, "old", ""),
					member("odd", host, "odd", "weird"),
				}},
				"hb": {Members: []cache.RegistryGroupMember{member("hb-a", "harbor.example.com", "", "proxy")}},
				"gn": {Members: []cache.RegistryGroupMember{member("gn-a", "reg.example.com", "", "proxy")}},
			},
			want: map[string]nexusRole{
				"hosted": nexusRoleHosted, "proxy": nexusRoleProxy, "nested": nexusRoleGroup,
				"old": nexusRoleUnknown, "odd": nexusRoleUnknown,
				// A member of a group that is not Nexus has no Nexus type to read.
				"hb-a": nexusRoleUnknown, "gn-a": nexusRoleUnknown,
			},
		},
		{
			name: "a declared entry takes the type of the discovered member at its exact address",
			regs: []config.RegistryItem{
				nx,
				declaredAs("d-proxy", host, "docker-proxy", "nx"),
				declaredAs("d-hosted", host, "docker-hosted", "nx"),
				declaredAs("d-nomatch", host, "docker-other", "nx"),
				declaredAs("d-noprefix", host, "", "nx"),
				declaredAs("d-otherhost", "other.example.com", "docker-proxy", "nx"),
				declaredAs("d-case", "NEXUS.example.com", "docker-proxy", "nx"),
				declaredAs("d-generic", host, "docker-proxy", "gn"),
			},
			groups: map[string]cache.RegistryGroupEntry{
				"nx": {Members: []cache.RegistryGroupMember{
					member("docker-proxy", host, "docker-proxy", "proxy"),
					member("docker-hosted", host, "docker-hosted", "hosted"),
				}},
			},
			want: map[string]nexusRole{
				"docker-proxy": nexusRoleProxy, "docker-hosted": nexusRoleHosted,
				"d-proxy": nexusRoleProxy, "d-hosted": nexusRoleHosted,
				"d-nomatch": nexusRoleUnknown, "d-noprefix": nexusRoleUnknown,
				"d-otherhost": nexusRoleUnknown, "d-case": nexusRoleUnknown,
				"d-generic": nexusRoleUnknown,
			},
		},
		{
			name: "a discovered member with no type leaves the declared entry unknown",
			regs: []config.RegistryItem{nx, declaredAs("d-proxy", host, "docker-proxy", "nx")},
			groups: map[string]cache.RegistryGroupEntry{
				"nx": {Members: []cache.RegistryGroupMember{member("docker-proxy", host, "docker-proxy", "")}},
			},
			want: map[string]nexusRole{"docker-proxy": nexusRoleUnknown, "d-proxy": nexusRoleUnknown},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newRegistryBrowser(tt.regs, tt.groups, nil, 120, 40)
			got := map[string]nexusRole{}
			for _, e := range b.entries {
				got[e.Alias] = e.role
			}
			if len(got) != len(tt.want) {
				t.Fatalf("entries = %v, want aliases %v", got, tt.want)
			}
			for alias, want := range tt.want {
				if g, ok := got[alias]; !ok || g != want {
					t.Errorf("entry %q role = %v (present %v), want %v", alias, g, ok, want)
				}
			}
		})
	}
}

// DEVDESK-308 (IS-14), step 2, reworded by DEVDESK-629 (F2): the level 1 header
// says which registries may omit images (the no-result ones marked, and the mark
// does not mean "absent").

func headerKeysOf(infos []shortcut.HeaderInfo) []string {
	var keys []string
	for _, i := range infos {
		keys = append(keys, i.Key)
	}
	return keys
}

const mayOmitKey = "May omit images"

func TestSearchHeaderInfoPlacesMayOmitAfterPartialInOrange(t *testing.T) {
	s := searchSummary{
		Pending:  1,
		Failed:   []string{"f"},
		Refused:  []string{"r"},
		Partial:  []string{"p"},
		MayOmit:  []string{"c1", "c2" + noResultMark},
		NoResult: []string{"n"},
	}
	infos := searchHeaderInfo(3, s, 200)
	want := []string{"Images", "Pending", "Failed", "Refused", "Partial", mayOmitKey, "No result"}
	if got := headerKeysOf(infos); !slices.Equal(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	if len(infos) > shortcut.HeaderRows {
		t.Errorf("%d lines, want at most %d", len(infos), shortcut.HeaderRows)
	}
	for _, i := range infos {
		if i.Key != mayOmitKey {
			continue
		}
		if i.Value != "c1, c2 (no result)" {
			t.Errorf("%s = %q, want %q", mayOmitKey, i.Value, "c1, c2 (no result)")
		}
		if got, want := i.Style.Render("x"), theme.HeaderWarnValueStyle().Render("x"); got != want {
			t.Errorf("%s is not styled like Partial (orange): %q vs %q", mayOmitKey, got, want)
		}
	}
}

func TestSearchHeaderInfoMayOmitAloneAndWithInterrupted(t *testing.T) {
	infos := searchHeaderInfo(0, searchSummary{MayOmit: []string{"c"}}, 200)
	if got, want := headerKeysOf(infos), []string{"Images", mayOmitKey}; !slices.Equal(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	s := searchSummary{Interrupted: []string{"i"}, Failed: []string{"f"}, Refused: []string{"r"},
		Partial: []string{"p"}, MayOmit: []string{"c"}, NoResult: []string{"n"}}
	infos = searchHeaderInfo(0, s, 200)
	if got := len(infos); got != shortcut.HeaderRows {
		t.Errorf("%d lines, want exactly %d", got, shortcut.HeaderRows)
	}
	if !slices.Contains(headerKeysOf(infos), mayOmitKey) {
		t.Errorf("%s was cut: %v", mayOmitKey, headerKeysOf(infos))
	}
}

// R10: a proxy that failed sits on two classes, so the budget has one line more to
// hold. Skipped and May omit images still exclude each other (Skipped means no
// catalog entry answered), which is what keeps seven classes inside HeaderRows.
func TestSearchHeaderInfoKeepsMayOmitWithFailedAndRefused(t *testing.T) {
	s := searchSummary{Failed: []string{"p"}, Refused: []string{"q"}, Partial: []string{"x"},
		MayOmit: []string{"p", "q"}, NoResult: []string{"n"}}
	keys := headerKeysOf(searchHeaderInfo(1, s, 200))
	if want := []string{"Images", "Failed", "Refused", "Partial", mayOmitKey, "No result"}; !slices.Equal(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
}

// nexusSearchModel opens the browser on a Nexus group "nx" and two entries that
// declare it as their parent, a and b, then submits a search. Nobody discovered the
// group, so it is searched as itself, and the declared entries have no discovered
// member to take a type from: their role is unknown.
func nexusSearchModel(t *testing.T) Model {
	t.Helper()
	group := nexusGroup()
	group.AuthMode = config.AuthAnonymous
	child := func(alias string) config.RegistryItem {
		r := anonymousRegistry(alias+".example.com", alias)
		r.Parent = "nx"
		return r
	}
	m := registryModel(t, group, child("a"), child("b"))
	return searchLot(t, feed(t, m, testutil.Resize(200, 40)))
}

// H4: a group is mentioned whatever it answered, with the mark when it had nothing;
// failed and refused also keep their own line. A declared member with no type is
// never mentioned: b's "no result" is a plain No result.
func TestANexusGroupIsMentionedWhateverItsVerdict(t *testing.T) {
	tests := []struct {
		name    string
		answer  func(t *testing.T, m Model) Model
		want    map[string]string
		wantKey []string
	}{
		{"ok", func(t *testing.T, m Model) Model { return answerFor(t, m, "nx", oneImage("nginx"), false, nil) },
			map[string]string{mayOmitKey: "nx", "No result": "b"}, []string{"Images", mayOmitKey, "No result"}},
		{"no result", func(t *testing.T, m Model) Model { return answerFor(t, m, "nx", nil, false, nil) },
			map[string]string{mayOmitKey: "nx (no result)", "No result": "b"}, []string{"Images", mayOmitKey, "No result"}},
		{"failed", func(t *testing.T, m Model) Model { return answerFor(t, m, "nx", nil, false, errors.New("boom")) },
			map[string]string{"Failed": "nx", mayOmitKey: "nx", "No result": "b"}, []string{"Images", "Failed", mayOmitKey, "No result"}},
		{"refused", func(t *testing.T, m Model) Model {
			return answerFor(t, m, "nx", nil, false, imagesearch.ErrUnsupported)
		},
			map[string]string{"Refused": "nx", mayOmitKey: "nx", "No result": "b"}, []string{"Images", "Refused", mayOmitKey, "No result"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withImageSearcher(t, &fakeSearcher{})
			m := tt.answer(t, nexusSearchModel(t))
			m = answerFor(t, m, "a", oneImage("nginx"), false, nil)
			m = answerFor(t, m, "b", nil, false, nil)

			vals := wantHeaderKeys(t, m, tt.wantKey...)
			for k, want := range tt.want {
				if vals[k] != want {
					t.Errorf("%s = %q, want %q", k, vals[k], want)
				}
			}
		})
	}
}

// F2: a proxy that failed or was refused has told nothing, so its names may be
// missing: it is on its verdict's line AND on May omit images, without the mark. A
// hosted repository that failed is only failed: it is not hiding a cache.
func TestAFailedProxyIsOnFailedAndMayOmitButAHostedOneIsOnlyFailed(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		header string
	}{
		{"failed", errors.New("boom"), "Failed"},
		{"refused", imagesearch.ErrUnsupported, "Refused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withImageSearcher(t, &fakeSearcher{})
			const base = "nexus.example.com"
			m := typedNexusMembersModel(t, base, typedMember{"p", "proxy"}, typedMember{"h", "hosted"})
			m = searchLot(t, m)
			m = answerFor(t, m, memberKey("nx", base, "p"), nil, false, tt.err)
			m = answerFor(t, m, memberKey("nx", base, "h"), nil, false, tt.err)

			vals := wantHeaderKeys(t, m, "Images", tt.header, mayOmitKey)
			if vals[tt.header] != "nx/p, nx/h" {
				t.Errorf("%s = %q, want both members", tt.header, vals[tt.header])
			}
			if vals[mayOmitKey] != "nx/p" {
				t.Errorf("%s = %q, want the proxy only, without a mark", mayOmitKey, vals[mayOmitKey])
			}
		})
	}
}

// A proxy whose upstream answers is not "limited to the cache": ok or no result,
// it is not mentioned (a no result is a plain No result). A hosted one neither.
func TestAProxyThatAnsweredIsNotMentioned(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	const base = "nexus.example.com"
	m := typedNexusMembersModel(t, base, typedMember{"p", "proxy"}, typedMember{"h", "hosted"}, typedMember{"q", "proxy"})
	m = searchLot(t, m)
	m = answerFor(t, m, memberKey("nx", base, "p"), oneImage("nginx"), false, nil)
	m = answerFor(t, m, memberKey("nx", base, "h"), nil, false, nil)
	m = answerFor(t, m, memberKey("nx", base, "q"), nil, false, nil)

	vals := wantHeaderKeys(t, m, "Images", "No result")
	if vals["No result"] != "nx/h, nx/q" {
		t.Errorf("No result = %q, want %q", vals["No result"], "nx/h, nx/q")
	}
}

// A one-character text skips the catalog walk: Skipped alone, no May omit images.
func TestASkippedNexusGroupShowsSkippedWithoutMayOmit(t *testing.T) {
	hub, _ := fakeHub(t)
	priv, _ := catalogServer(t, "api", "alpha")
	withImageSearcher(t, wiredLikeProduction(hub))

	group := nexusGroup()
	group.AuthMode = config.AuthAnonymous
	group.URL = priv.URL
	// A second entry keeps the lot a tea.Batch: with one search tea.Batch
	// returns the lone Cmd and runCmds (flattenBatch) would run it away.
	m := registryModel(t, anonymousRegistry("docker.io", "hub"), group)
	m = feed(t, m, testutil.Resize(200, 40))
	m, cmd := submitRaw(t, m, "a")
	if cmd == nil || m.registryBrowser.state != browserStateImages {
		t.Fatalf("the search did not start (state %d)", m.registryBrowser.state)
	}
	results := runCmds(t, cmd)
	for range 2 {
		m = feed(t, m, recvResult(t, results))
	}

	keys, vals := headerLines(m)
	if slices.Contains(keys, mayOmitKey) {
		t.Errorf("header has a %s line (%v): Skipped takes it", mayOmitKey, vals)
	}
	if want := "nx (type 2+ characters)"; vals["Skipped"] != want {
		t.Errorf("Skipped = %q, want %q (keys %v)", vals["Skipped"], want, keys)
	}
}

// Docker Hub and a generic group are not concerned: no mention, whatever the verdict.
func TestHubAndGenericNeverMentionMayOmit(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	generic := config.RegistryItem{Slug: "gn", Kind: config.KindGroup, Provider: config.ProviderGeneric,
		URL: "reg.example.com", Alias: "gn", AuthMode: config.AuthAnonymous}
	m := registryModel(t, anonymousRegistry("docker.io", "hub"), generic)
	m = searchLot(t, feed(t, m, testutil.Resize(200, 40)))
	m = answerFor(t, m, "hub", oneImage("nginx"), false, nil)
	m = answerFor(t, m, "gn", nil, false, nil)

	vals := wantHeaderKeys(t, m, "Images", "No result")
	if vals["No result"] != "gn" {
		t.Errorf("No result = %q, want gn", vals["No result"])
	}
}

// Limit case, not reachable today (Skipped and May omit images exclude each other):
// with all seven classes present, Skipped is the line cut.
func TestSearchHeaderInfoCutsSkippedWhenEveryClassIsPresent(t *testing.T) {
	s := searchSummary{Interrupted: []string{"i"}, Failed: []string{"f"}, Refused: []string{"r"},
		Partial: []string{"p"}, MayOmit: []string{"c"}, NoResult: []string{"n"}, Skipped: []string{"s"}}
	infos := searchHeaderInfo(1, s, 200)
	if got := len(infos); got != shortcut.HeaderRows {
		t.Errorf("%d lines, want %d", got, shortcut.HeaderRows)
	}
	if keys := headerKeysOf(infos); slices.Contains(keys, "Skipped") || !slices.Contains(keys, "No result") {
		t.Errorf("keys = %v, want Skipped cut and No result kept", keys)
	}
}

// A discovered member of a Nexus group is keyed by memberKey, not by its slug; its
// role comes from the type discovery stored, and the mention follows the rule.
func TestADiscoveredNexusMemberIsMentionedByKeyAndType(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	group := nexusGroup()
	group.AuthMode = config.AuthAnonymous
	cfg := testConfig()
	cfg.Registry.Registries = []config.RegistryItem{group, anonymousRegistry("docker.io", "hub")}
	// The group cache is loaded before the browser opens: that is what builds
	// the member entries.
	m := feed(t, New(cfg), testutil.Resize(200, 40),
		ImagesListMsg{Images: imageFixtures()},
		RegistryGroupCacheLoadedMsg{Entries: map[string]cache.RegistryGroupEntry{
			"nx": {Members: []cache.RegistryGroupMember{
				{Alias: "hosted", URL: "nexus.example.com", RepoPrefix: "hosted", Type: "hosted"},
				{Alias: "proxy", URL: "nexus.example.com", RepoPrefix: "proxy", Type: "proxy"},
				{Alias: "old", URL: "nexus.example.com", RepoPrefix: "old"},
			}},
		}},
	)
	m = feed(t, m, testutil.Key(keymap.Browser))
	if m.registryBrowser == nil {
		t.Fatal("'b' did not open the registry browser")
	}
	hosted, proxy, old := memberKey("nx", "nexus.example.com", "hosted"), memberKey("nx", "nexus.example.com", "proxy"), memberKey("nx", "nexus.example.com", "old")
	b := m.registryBrowser
	for key, want := range map[string]nexusRole{hosted: nexusRoleHosted, proxy: nexusRoleProxy, old: nexusRoleUnknown} {
		if e := b.entryFor(key); e == nil || e.role != want {
			t.Fatalf("entry %q = %+v, want role %v", key, e, want)
		}
	}
	if e := b.entryFor("nx"); e != nil {
		t.Fatalf("the group slug must not resolve to a member: %+v", e)
	}
	m = searchLot(t, m)
	m = answerFor(t, m, hosted, nil, false, errors.New("boom"))
	m = answerFor(t, m, proxy, nil, false, errors.New("boom"))
	m = answerFor(t, m, old, nil, false, errors.New("boom"))
	m = answerFor(t, m, "hub", oneImage("nginx"), false, nil)

	vals := wantHeaderKeys(t, m, "Images", "Failed", mayOmitKey)
	if want := "nx/hosted, nx/proxy, nx/old"; vals["Failed"] != want {
		t.Errorf("Failed = %q, want %q", vals["Failed"], want)
	}
	if want := "nx/proxy"; vals[mayOmitKey] != want {
		t.Errorf("%s = %q, want %q: only the proxy, the hosted and the untyped member are not mentioned", mayOmitKey, vals[mayOmitKey], want)
	}
}
