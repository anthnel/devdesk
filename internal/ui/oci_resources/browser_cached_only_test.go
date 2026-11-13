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

// DEVDESK-308 (IS-14), step 1: which picker entries can only list cached
// content. A Nexus group, its discovered members and an entry declaring
// `parent:` towards a Nexus group do (Q-B ii); nothing else does.
func TestListsCacheOnly(t *testing.T) {
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
			if got := listsCacheOnly(tt.reg, bySlug); got != tt.want {
				t.Fatalf("listsCacheOnly(%+v) = %v, want %v", tt.reg, got, tt.want)
			}
		})
	}
}

func TestBuildEntriesFlagsCachedOnly(t *testing.T) {
	harbor := config.RegistryItem{Slug: "hb", Kind: config.KindGroup, Provider: config.ProviderHarbor, URL: "harbor.example.com", Alias: "hb"}
	generic := config.RegistryItem{Slug: "gn", Kind: config.KindGroup, Provider: config.ProviderGeneric, URL: "reg.example.com", Alias: "gn"}
	nexusPlain := config.RegistryItem{Slug: "n2", Kind: config.KindRegistry, Provider: config.ProviderNexus, URL: "n2.example.com", Alias: "n2"}
	hub := config.RegistryItem{Slug: "hub", Kind: config.KindRegistry, URL: "docker.io", Alias: "hub"}
	declared := config.RegistryItem{Slug: "decl", Kind: config.KindRegistry, Parent: "nx", URL: "decl.example.com", Alias: "decl"}
	declaredGeneric := config.RegistryItem{Slug: "declg", Kind: config.KindRegistry, Parent: "gn", URL: "declg.example.com", Alias: "declg"}

	member := func(a string) cache.RegistryGroupMember {
		return cache.RegistryGroupMember{Alias: a, URL: a + ".example.com"}
	}
	regs := []config.RegistryItem{nexusGroup(), harbor, generic, nexusPlain, hub, declared, declaredGeneric}

	tests := []struct {
		name   string
		groups map[string]cache.RegistryGroupEntry
		want   map[string]bool // Alias -> cachedOnly
	}{
		{
			name:   "nothing discovered",
			groups: nil,
			want: map[string]bool{
				"nx": true, "hb": false, "gn": false, "n2": false,
				"hub": false, "decl": true, "declg": false,
			},
		},
		{
			name: "members discovered",
			groups: map[string]cache.RegistryGroupEntry{
				"nx": {Members: []cache.RegistryGroupMember{member("nx-a"), member("nx-b")}},
				"hb": {Members: []cache.RegistryGroupMember{member("hb-a")}},
				"gn": {Members: []cache.RegistryGroupMember{member("gn-a")}},
			},
			want: map[string]bool{
				"nx-a": true, "nx-b": true, "hb-a": false, "gn-a": false,
				"n2": false, "hub": false, "decl": true, "declg": false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newRegistryBrowser(regs, tt.groups, nil, 120, 40)
			got := map[string]bool{}
			for _, e := range b.entries {
				got[e.Alias] = e.cachedOnly
			}
			if len(got) != len(tt.want) {
				t.Fatalf("entries = %v, want aliases %v", got, tt.want)
			}
			for alias, want := range tt.want {
				g, ok := got[alias]
				if !ok || g != want {
					t.Errorf("entry %q cachedOnly = %v (present %v), want %v", alias, g, ok, want)
				}
			}
		})
	}
}

// DEVDESK-308 (IS-14), step 2: the level 1 header says which registries can only
// list cached content (Q-A a: a `Cached only` line, the no-result ones marked).

func headerKeysOf(infos []shortcut.HeaderInfo) []string {
	var keys []string
	for _, i := range infos {
		keys = append(keys, i.Key)
	}
	return keys
}

func TestSearchHeaderInfoPlacesCachedOnlyAfterPartialInOrange(t *testing.T) {
	s := searchSummary{
		Pending:    1,
		Failed:     []string{"f"},
		Refused:    []string{"r"},
		Partial:    []string{"p"},
		CachedOnly: []string{"c1", "c2" + cachedNoResultMark},
		NoResult:   []string{"n"},
	}
	infos := searchHeaderInfo(3, s, 200)
	want := []string{"Images", "Pending", "Failed", "Refused", "Partial", "Cached only", "No result"}
	if got := headerKeysOf(infos); !slices.Equal(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	if len(infos) > shortcut.HeaderRows {
		t.Errorf("%d lines, want at most %d", len(infos), shortcut.HeaderRows)
	}
	for _, i := range infos {
		if i.Key != "Cached only" {
			continue
		}
		if i.Value != "c1, c2 (no result)" {
			t.Errorf("Cached only = %q, want %q", i.Value, "c1, c2 (no result)")
		}
		if got, want := i.Style.Render("x"), theme.HeaderWarnValueStyle().Render("x"); got != want {
			t.Errorf("Cached only is not styled like Partial (orange): %q vs %q", got, want)
		}
	}
}

func TestSearchHeaderInfoCachedOnlyAloneAndWithInterrupted(t *testing.T) {
	infos := searchHeaderInfo(0, searchSummary{CachedOnly: []string{"c"}}, 200)
	if got, want := headerKeysOf(infos), []string{"Images", "Cached only"}; !slices.Equal(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	s := searchSummary{Interrupted: []string{"i"}, Failed: []string{"f"}, Refused: []string{"r"},
		Partial: []string{"p"}, CachedOnly: []string{"c"}, NoResult: []string{"n"}}
	infos = searchHeaderInfo(0, s, 200)
	if got := len(infos); got != shortcut.HeaderRows {
		t.Errorf("%d lines, want exactly %d", got, shortcut.HeaderRows)
	}
	if !slices.Contains(headerKeysOf(infos), "Cached only") {
		t.Errorf("Cached only was cut: %v", headerKeysOf(infos))
	}
}

// nexusSearchModel opens the browser on a Nexus group "nx" and two entries that
// declare it as their parent, a and b, then submits a search.
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

func TestANexusGroupListsCachedOnlyAndLeavesNoResult(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := nexusSearchModel(t)
	m = answerFor(t, m, "nx", oneImage("nginx"), false, nil)
	m = answerFor(t, m, "a", oneImage("nginx"), false, nil)
	m = answerFor(t, m, "b", nil, false, nil)

	vals := wantHeaderKeys(t, m, "Images", "Cached only")
	if want := "nx, a, b (no result)"; vals["Cached only"] != want {
		t.Errorf("Cached only = %q, want %q", vals["Cached only"], want)
	}
}

func TestACachedOnlyRegistryThatFailedIsOnlyFailed(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := nexusSearchModel(t)
	m = answerFor(t, m, "nx", oneImage("nginx"), false, nil)
	m = answerFor(t, m, "a", nil, false, imagesearch.ErrUnsupported)
	m = answerFor(t, m, "b", nil, false, errors.New("boom"))

	vals := wantHeaderKeys(t, m, "Images", "Failed", "Refused", "Cached only")
	if vals["Refused"] != "a" || vals["Failed"] != "b" || vals["Cached only"] != "nx" {
		t.Errorf("header = %v, want Refused a, Failed b, Cached only nx", vals)
	}
}

// A one-character text skips the catalog walk: Skipped alone, no Cached only.
func TestASkippedNexusGroupShowsSkippedWithoutCachedOnly(t *testing.T) {
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
	if slices.Contains(keys, "Cached only") {
		t.Errorf("header has a Cached only line (%v): Skipped takes it", vals)
	}
	if want := "nx (type 2+ characters)"; vals["Skipped"] != want {
		t.Errorf("Skipped = %q, want %q (keys %v)", vals["Skipped"], want, keys)
	}
}

// Docker Hub and a generic group are not concerned: no mention, whatever the verdict.
func TestHubAndGenericNeverMentionCachedOnly(t *testing.T) {
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

// Limit case, not reachable today (Skipped and Cached only exclude each other):
// with all seven classes present, Skipped is the line cut.
func TestSearchHeaderInfoCutsSkippedWhenEveryClassIsPresent(t *testing.T) {
	s := searchSummary{Interrupted: []string{"i"}, Failed: []string{"f"}, Refused: []string{"r"},
		Partial: []string{"p"}, CachedOnly: []string{"c"}, NoResult: []string{"n"}, Skipped: []string{"s"}}
	infos := searchHeaderInfo(1, s, 200)
	if got := len(infos); got != shortcut.HeaderRows {
		t.Errorf("%d lines, want %d", got, shortcut.HeaderRows)
	}
	if keys := headerKeysOf(infos); slices.Contains(keys, "Skipped") || !slices.Contains(keys, "No result") {
		t.Errorf("keys = %v, want Skipped cut and No result kept", keys)
	}
}

// A discovered member of a Nexus group is keyed by memberKey, not by its slug,
// and still lands on Cached only.
func TestADiscoveredNexusMemberListsCachedOnlyByKey(t *testing.T) {
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
				{Alias: "hosted", URL: "nexus.example.com", RepoPrefix: "hosted"},
				{Alias: "proxy", URL: "nexus.example.com", RepoPrefix: "proxy"},
			}},
		}},
	)
	m = feed(t, m, testutil.Key(keymap.Browser))
	if m.registryBrowser == nil {
		t.Fatal("'b' did not open the registry browser")
	}
	key := memberKey("nx", "nexus.example.com", "hosted")
	b := m.registryBrowser
	if e := b.entryFor(key); e == nil || !e.cachedOnly {
		t.Fatalf("entry %q = %+v, want a cachedOnly member", key, e)
	}
	if b.entryCachedOnly("nx") {
		t.Fatal("the group slug must not resolve to a member")
	}
	m = searchLot(t, m)
	m = answerFor(t, m, key, oneImage("nginx"), false, nil)
	m = answerFor(t, m, memberKey("nx", "nexus.example.com", "proxy"), nil, false, nil)
	m = answerFor(t, m, "hub", oneImage("nginx"), false, nil)

	vals := wantHeaderKeys(t, m, "Images", "Cached only")
	if want := "nx/hosted, nx/proxy (no result)"; vals["Cached only"] != want {
		t.Errorf("Cached only = %q, want %q", vals["Cached only"], want)
	}
}
