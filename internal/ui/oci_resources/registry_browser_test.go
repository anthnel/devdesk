package ociresources

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The browser searches registries over HTTP and pulls through Docker. No test
// executes one: tag lists, group detections and pull results are fed in as
// messages.
//
// It is opened through the model rather than constructed directly, because the
// model is what wires the scan cache in and what the router talks to.

// browsingModel returns a model with the registry browser open. The group cache
// is empty here, so the group is offered as itself and there are two entries;
// groupedModel is the variant with members.
func browsingModel(t *testing.T) Model {
	t.Helper()
	m := feed(t, loadedModel(t), testutil.Key(keymap.Browser))
	if m.registryBrowser == nil {
		t.Fatal("'b' did not open the registry browser")
	}
	return m
}

// searchedModel returns a browser showing tags from both registries.
func searchedModel(t *testing.T) Model {
	t.Helper()
	return feed(t, browsingModel(t),
		MultiRegistryTagsLoadedMsg{
			EntryKey: "prod", RegistryURL: "registry.example.com", Alias: "prod", Repo: "api",
			Tags: []string{"v1", "v2"},
		},
		MultiRegistryTagsLoadedMsg{
			EntryKey: "hub", RegistryURL: "docker.io", Alias: "hub", Repo: "api",
			Tags: []string{"latest"},
		},
	)
}

// ── Opening ──────────────────────────────────────────────────────────────────

// The browser needs somewhere to search, so it says what is missing rather than
// opening empty.
func TestBrowserNeedsARegistry(t *testing.T) {
	cfg := testConfig()
	cfg.Registry.Registries = nil
	m := feed(t, New(cfg), tea.WindowSizeMsg{Width: 180, Height: 30}, ImagesListMsg{Images: imageFixtures()})

	m = feed(t, m, testutil.Key(keymap.Browser))

	if m.registryBrowser != nil {
		t.Error("the browser opened with no registries configured")
	}
	if !strings.Contains(m.footer.Text(), "No registries") {
		t.Errorf("footer = %q, want it to name the missing configuration", m.footer.Text())
	}
}

// D13, fixed. The browser used to open into a resolving state and ignore every
// key — esc included — for as long as its detections took: up to eight seconds
// per configured registry, on a screen the user may have opened by mistake.
//
// Members come from config plus the cache now, so the form is there on the first
// frame and esc works on it, offline included.
func TestTheBrowserOpensStraightOntoItsForm(t *testing.T) {
	m := groupedModel(t)

	if m.registryBrowser.state != browserStateInput {
		t.Fatalf("the browser opened in state %d, want the form", m.registryBrowser.state)
	}
	// The group expanded into its two cached members, plus the plain registry.
	if got := len(m.registryBrowser.entries); got != 3 {
		t.Errorf("entries = %d, want the group's two members plus docker.io", got)
	}

	// esc is answered rather than swallowed, which is the half of D13 that
	// mattered: the browser used to ignore every key while it resolved.
	_, cmd := step(t, m, testutil.Key("esc"))
	if _, ok := testutil.MsgOf[RegistryBrowserCloseMsg](cmd); !ok {
		t.Fatalf("esc produced %T on a browser that had only just opened", testutil.Msg(cmd))
	}
	if m = feed(t, m, RegistryBrowserCloseMsg{}); m.registryBrowser != nil {
		t.Error("the close message left the browser open")
	}
}

// Opening it must not touch the network at all — that is what makes it work
// offline, and what the cache is for.
func TestOpeningTheBrowserIssuesNoCommand(t *testing.T) {
	m := feed(t, loadedModel(t), RegistryGroupCacheLoadedMsg{Entries: groupCacheFixture()})

	_, cmd := step(t, m, testutil.Key(keymap.Browser))

	if cmd != nil {
		t.Errorf("opening the browser produced %T, want nothing to run", testutil.Msg(cmd))
	}
}

// A group nobody has discovered yet is still a pullable registry. Hiding it
// would be worse than listing it without the members it may have.
func TestAGroupWithNothingCachedIsOfferedAsItself(t *testing.T) {
	m := browsingModel(t) // the group cache is empty here

	if got := len(m.registryBrowser.entries); got != 2 {
		t.Fatalf("entries = %d, want both registries offered as themselves", got)
	}
	for _, e := range m.registryBrowser.entries {
		if e.ParentSlug != "" {
			t.Errorf("entry %q claims a parent with nothing discovered", e.Alias)
		}
	}
}

// ── Searching ────────────────────────────────────────────────────────────────

// Results from several registries land in one table, tagged with which registry
// they came from — that is the whole point of the multi-registry browser.
func TestTagsFromEveryRegistryLandInOneTable(t *testing.T) {
	m := searchedModel(t)

	if got := len(m.registryBrowser.tags); got != 3 {
		t.Fatalf("tags = %d, want both registries' results", got)
	}

	seen := map[string]bool{}
	for _, tag := range m.registryBrowser.tags {
		seen[tag.RegistryURL] = true
	}
	if !seen["registry.example.com"] || !seen["docker.io"] {
		t.Errorf("the results came from %v, want both registries", seen)
	}
}

// A slow registry keeps the browser in its searching state, so the list does
// not look complete while it is still filling.
func TestTheBrowserSearchesUntilEveryRegistryAnswers(t *testing.T) {
	m := browsingModel(t)
	m.registryBrowser.pendingSearches = 2 // two registries queried

	m = feed(t, m, MultiRegistryTagsLoadedMsg{
		EntryKey: "prod", RegistryURL: "registry.example.com", Repo: "api", Tags: []string{"v1"},
	})
	if !m.registryBrowser.IsSearching() {
		t.Error("the browser stopped searching with one registry outstanding")
	}

	m = feed(t, m, MultiRegistryTagsLoadedMsg{EntryKey: "hub", RegistryURL: "docker.io", Repo: "api", Tags: []string{"latest"}})
	if m.registryBrowser.IsSearching() {
		t.Error("the browser is still searching once every registry answered")
	}
}

// A duplicate or late response used to drive the counter negative, so the next
// search started from that base and its spinner never showed.
func TestExtraResponsesDoNotDriveTheCounterNegative(t *testing.T) {
	m := browsingModel(t)
	m.registryBrowser.pendingSearches = 1

	m = feed(t, m,
		MultiRegistryTagsLoadedMsg{EntryKey: "prod", RegistryURL: "registry.example.com", Repo: "api", Tags: []string{"v1"}},
		MultiRegistryTagsLoadedMsg{EntryKey: "prod", RegistryURL: "registry.example.com", Repo: "api", Tags: []string{"v1"}},
		MultiRegistryTagsLoadedMsg{EntryKey: "prod", RegistryURL: "registry.example.com", Repo: "api", Tags: []string{"v1"}},
	)

	if got := m.registryBrowser.pendingSearches; got != 0 {
		t.Errorf("pendingSearches = %d after three responses to one search, want 0", got)
	}

	// A fresh search must be reported as in flight.
	m.registryBrowser.pendingSearches = 1
	if !m.registryBrowser.IsSearching() {
		t.Error("IsSearching() is false for a search that has just started")
	}
}

// One registry failing must not lose the other's results.
func TestAFailedSearchKeepsTheOtherResults(t *testing.T) {
	m := feed(t, browsingModel(t),
		MultiRegistryTagsLoadedMsg{EntryKey: "prod", RegistryURL: "registry.example.com", Repo: "api", Tags: []string{"v1", "v2"}},
		MultiRegistryTagsLoadedMsg{EntryKey: "hub", RegistryURL: "docker.io", Repo: "api", Err: errors.New("401 unauthorized")},
	)

	if got := len(m.registryBrowser.tags); got != 2 {
		t.Errorf("tags = %d, want the registry that answered to still be listed", got)
	}
}

// Last-updated times arrive separately, after the tag list, so the table is
// usable before the enrichment lands.
func TestTagMetadataEnrichesTheRows(t *testing.T) {
	m := searchedModel(t)

	m = feed(t, m, MultiRegistryTagsMetaMsg{
		EntryKey: "prod", RegistryURL: "registry.example.com",
		Repo: "api",
		Meta: map[string]time.Time{"v1": at(1)},
	})

	var enriched bool
	for _, tag := range m.registryBrowser.tags {
		if tag.Tag == "v1" && !tag.UpdatedAt.IsZero() {
			enriched = true
		}
	}
	if !enriched {
		t.Error("the last-updated time did not reach the tag")
	}
}

// ── Pulling ──────────────────────────────────────────────────────────────────

// Both outcomes report, and the level is what tells them apart — the two used
// to live in separate fields, so "reported nothing" was how a success was
// recognised.
func TestPullResultIsReported(t *testing.T) {
	ok := feed(t, searchedModel(t), RegistryPullCompleteMsg{ImageName: "registry.example.com/api:v1"})
	if !ok.footer.IsSet() || ok.footer.Level() != sharedcomponents.LevelInfo {
		t.Errorf("a successful pull reported %q at level %v, want it as info",
			ok.footer.Text(), ok.footer.Level())
	}

	failed := feed(t, searchedModel(t), RegistryPullCompleteMsg{
		ImageName: "registry.example.com/api:v1", Err: errors.New("manifest unknown"),
	})
	if failed.footer.Level() != sharedcomponents.LevelError {
		t.Errorf("a failed pull reported %q at level %v, want it as an error",
			failed.footer.Text(), failed.footer.Level())
	}
}

// ── Closing ──────────────────────────────────────────────────────────────────

// While the browser is open it owns every key, so the images tab's shortcuts
// must not fire behind it.
func TestTheBrowserSwallowsTheViewShortcuts(t *testing.T) {
	m := browsingModel(t)

	m = feed(t, m, testutil.Key(keymap.Delete), testutil.Key(keymap.Prune))

	if m.confirmModal != nil {
		t.Error("a destructive key fired behind the registry browser")
	}
}

func TestInEditModeCoversTheBrowser(t *testing.T) {
	if !browsingModel(t).InEditMode() {
		t.Error("InEditMode() is false with the registry browser open")
	}
}

// ── Credentials on the browse path (D12) ─────────────────────────────────────

// The other half of D12. `docker login` is keyed on host, so the credentials
// stored for one repository on a Nexus instance were sent to every repository
// the browser searched on that host — the flag saying a registry needed no
// authentication was read nowhere on this path.
func TestAnAnonymousRegistryIsSearchedWithoutCredentials(t *testing.T) {
	writeDockerConfig(t, map[string]any{
		"auths": map[string]any{
			"registry.example.com": map[string]string{"auth": encodeAuth("nexus-admin", "s3cret")},
		},
	})

	b := &RegistryBrowser{registries: []config.RegistryItem{
		{URL: "registry.example.com", Username: "anthnel", AuthMode: config.AuthAnonymous},
	}}

	user, pass := b.credsFor(browserRegistryEntry{
		URL:      "registry.example.com",
		authMode: config.AuthAnonymous,
	})

	if user != "" || pass != "" {
		t.Errorf("the search would go out as %q/%q, want nothing sent to an anonymous registry", user, pass)
	}
}

// And the contrast, so the test above cannot pass by breaking credentials
// outright: a registry that asks for them still gets both, the configured
// username winning over the stored one.
func TestARegistryThatUsesCredentialsStillGetsThem(t *testing.T) {
	writeDockerConfig(t, map[string]any{
		"auths": map[string]any{
			"registry.example.com": map[string]string{"auth": encodeAuth("stored-user", "s3cret")},
		},
	})

	b := &RegistryBrowser{registries: []config.RegistryItem{
		{URL: "registry.example.com", Username: "anthnel", AuthMode: config.AuthCredentials},
	}}

	user, pass := b.credsFor(browserRegistryEntry{
		URL:      "registry.example.com",
		authMode: config.AuthCredentials,
	})

	if user != "anthnel" {
		t.Errorf("username = %q, want the configured one", user)
	}
	if pass != "s3cret" {
		t.Errorf("password = %q, want the stored one", pass)
	}
}

// A member is searched at its own URL but authenticated with its group's
// credentials, because the two share a host and therefore a credential entry.
// That inheritance carries the refusal as well as the password.
func TestAGroupMemberFollowsItsGroupOnCredentials(t *testing.T) {
	writeDockerConfig(t, map[string]any{
		"auths": map[string]any{
			"registry.example.com": map[string]string{"auth": encodeAuth("stored-user", "s3cret")},
		},
	})

	b := &RegistryBrowser{registries: []config.RegistryItem{
		{URL: "registry.example.com", Username: "group-user", AuthMode: config.AuthCredentials},
	}}
	member := browserRegistryEntry{
		URL:         "registry.example.com/repository/dhi-proxy",
		ParentAlias: "prod",
		parentURL:   "registry.example.com",
	}

	member.authMode = config.AuthCredentials
	if user, pass := b.credsFor(member); user != "group-user" || pass != "s3cret" {
		t.Errorf("a member of a group using credentials got %q/%q, want the group's", user, pass)
	}

	member.authMode = config.AuthAnonymous
	if user, pass := b.credsFor(member); user != "" || pass != "" {
		t.Errorf("a member of an anonymous group got %q/%q, want nothing", user, pass)
	}
}

// The mode a member ends up with is settled once, when the group resolves —
// there is no second place that could disagree with it.
func TestCachedMembersCarryTheirGroupsMode(t *testing.T) {
	b := newRegistryBrowser(
		[]config.RegistryItem{{
			URL: "registry.example.com", Alias: "prod", Slug: "prod",
			Kind: config.KindGroup, AuthMode: config.AuthAnonymous,
		}},
		map[string]cache.RegistryGroupEntry{"prod": {Members: []cache.RegistryGroupMember{
			{Alias: "hosted", URL: "registry.example.com/repository/docker-hosted"},
		}}},
		nil, 120, 30,
	)

	if len(b.entries) != 1 {
		t.Fatalf("got %d entries, want the one member", len(b.entries))
	}
	if b.entries[0].authMode != config.AuthAnonymous {
		t.Errorf("the member resolved as %q, want its anonymous group's mode", b.entries[0].authMode)
	}
	if b.entries[0].ParentSlug != "prod" {
		t.Errorf("ParentSlug = %q, want the group it came from", b.entries[0].ParentSlug)
	}
}

// ── The group cache (§3.8 step 3) ────────────────────────────────────────────

// registriesTab returns a model on the Registries tab with the group row
// selected, which is where the explicit refresh lives.
func registriesTab(t *testing.T) Model {
	t.Helper()
	// The table is built when the group cache lands, which Init guarantees.
	m := feed(t, loadedModel(t), RegistryGroupCacheLoadedMsg{})
	for m.activeTab != tabRegistries {
		m = feed(t, m, testutil.Key("tab"))
	}
	return m
}

// Discovery is a network round trip against a repository manager. Writing the
// result through means the next open reads it from disk instead.
func TestADiscoveryIsWrittenToTheCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"type":"group","format":"docker",
			"attributes":{"group":{"memberNames":["docker-hosted","dhi-proxy"]}}}`))
	}))
	defer srv.Close()

	reg := config.RegistryItem{
		Slug:     "cached-group",
		Kind:     config.KindGroup,
		Provider: config.ProviderNexus,
		URL:      srv.URL + "/repository/docker-group",
		AuthMode: config.AuthAnonymous,
	}
	t.Cleanup(func() {
		if c, err := cache.NewRegistryGroupCache(); err == nil {
			_ = c.Delete("cached-group")
		}
	})

	msg := run(t, detectRegistryGroupCmd(reg, "")).(RegistryGroupDetectedMsg)
	if msg.Err != nil {
		t.Fatalf("Err = %v", msg.Err)
	}
	if msg.Slug != "cached-group" {
		t.Errorf("Slug = %q, want the key the cache and the table use", msg.Slug)
	}

	c, err := cache.NewRegistryGroupCache()
	if err != nil {
		t.Fatalf("reopening the group cache: %v", err)
	}
	entry := c.Get("cached-group")
	if entry == nil {
		t.Fatal("the discovery was not written to the cache")
	}
	if len(entry.Members) != 2 {
		t.Errorf("Members = %+v, want both", entry.Members)
	}
	if entry.DiscoveredAt.IsZero() {
		t.Error("the entry carries no time, so the column cannot show its age")
	}
}

// An unreachable manager must not empty what was last known: a stale answer is
// worth more than none, and the column says how stale it is.
func TestAFailedDiscoveryLeavesTheCacheAlone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, err := cache.NewRegistryGroupCache()
	if err != nil {
		t.Fatalf("opening the group cache: %v", err)
	}
	known := cache.RegistryGroupEntry{
		Members:      []cache.RegistryGroupMember{{Alias: "hosted", URL: "https://n/repository/hosted"}},
		DiscoveredAt: time.Now().Add(-24 * time.Hour),
	}
	if err := c.Set("kept-group", known); err != nil {
		t.Fatalf("seeding the cache: %v", err)
	}
	t.Cleanup(func() { _ = c.Delete("kept-group") })

	run(t, detectRegistryGroupCmd(config.RegistryItem{
		Slug:     "kept-group",
		Kind:     config.KindGroup,
		Provider: config.ProviderNexus,
		URL:      srv.URL + "/repository/docker-group",
		AuthMode: config.AuthAnonymous,
	}, ""))

	reopened, err := cache.NewRegistryGroupCache()
	if err != nil {
		t.Fatalf("reopening the group cache: %v", err)
	}
	entry := reopened.Get("kept-group")
	if entry == nil || len(entry.Members) != 1 {
		t.Errorf("the cache holds %+v after a failed refresh, want what was last known", entry)
	}
}

// The Members column is what keeps a stale cache visible. Without it the cache
// looks current whatever it holds, which is worse than the re-detection it
// replaced.
func TestTheMembersColumnShowsTheCountAndTheAge(t *testing.T) {
	m := loadedModel(t)
	m = feed(t, m, RegistryGroupCacheLoadedMsg{Entries: map[string]cache.RegistryGroupEntry{
		"prod": {
			Members:      []cache.RegistryGroupMember{{Alias: "hosted"}, {Alias: "dhi"}},
			DiscoveredAt: time.Now().Add(-3 * time.Hour),
		},
	}})

	rows := m.registryTable.Table().Rows()
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want the two configured registries", len(rows))
	}
	const members = 5
	if !strings.HasPrefix(rows[0][members], "2 ") {
		t.Errorf("the group's Members cell is %q, want it to start with the count", rows[0][members])
	}
	if !strings.Contains(rows[0][members], "3 hr ago") {
		t.Errorf("the group's Members cell is %q, want the age of the discovery (Rule 127)", rows[0][members])
	}
	if rows[1][members] != "" {
		t.Errorf("a plain registry shows %q in Members, want nothing — it has none", rows[1][members])
	}
}

// A group nobody has asked about yet must not read as a group with no members.
func TestAGroupNeverDiscoveredSaysSo(t *testing.T) {
	m := registriesTab(t)

	const members = 5
	if got := m.registryTable.Table().Rows()[0][members]; got != "never" {
		t.Errorf("Members = %q for a group with no cached discovery, want %q", got, "never")
	}
}

// ctrl+r on a group row is the explicit refresh: discovery costs a round trip,
// so it happens when asked rather than on every browser open.
func TestCtrlRRefreshesTheSelectedGroup(t *testing.T) {
	m := registriesTab(t)

	m = feed(t, m, testutil.Key("ctrl+r"))

	if !m.refreshingGroups["prod"] {
		t.Error("ctrl+r on a group row started no refresh")
	}
	const members = 5
	if got := m.registryTable.Table().Rows()[0][members]; !strings.Contains(got, "refreshing") {
		t.Errorf("Members = %q while refreshing, want it to say so", got)
	}
}

// A plain registry has nothing to discover, so ctrl+r must not pretend to.
func TestCtrlROnAPlainRegistryStartsNoDiscovery(t *testing.T) {
	m := registriesTab(t)
	m.registryTable.Update(testutil.Key("down")) // docker.io

	m = feed(t, m, testutil.Key("ctrl+r"))

	if len(m.refreshingGroups) != 0 {
		t.Errorf("refreshingGroups = %v after ctrl+r on a plain registry", m.refreshingGroups)
	}
}

// A second ctrl+r while one is in flight must not fire a second probe.
func TestASecondRefreshIsNotStartedWhileOneIsRunning(t *testing.T) {
	m := registriesTab(t)
	m = feed(t, m, testutil.Key("ctrl+r"))

	before := m.refreshSelectedGroupCmd()

	if before != nil {
		t.Error("a second refresh was started for a group already refreshing")
	}
}

// The result clears the in-flight marker whether it succeeded or not, or the
// row would say "refreshing" forever.
func TestAFinishedRefreshClearsItsMarker(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  RegistryGroupDetectedMsg
	}{
		{"a discovery that worked", RegistryGroupDetectedMsg{RegistryURL: "registry.example.com", Slug: "prod"}},
		{"one that failed", RegistryGroupDetectedMsg{
			RegistryURL: "registry.example.com", Slug: "prod", Err: errors.New("unreachable")}},
	} {
		m := feed(t, registriesTab(t), testutil.Key("ctrl+r"))

		m = feed(t, m, tc.msg)

		if m.refreshingGroups["prod"] {
			t.Errorf("%s left the row marked as refreshing", tc.name)
		}
	}
}

// A refresh that failed says so, rather than leaving the row looking refreshed.
func TestAFailedRefreshIsReported(t *testing.T) {
	m := feed(t, registriesTab(t), testutil.Key("ctrl+r"))

	m = feed(t, m, RegistryGroupDetectedMsg{
		RegistryURL: "registry.example.com", Slug: "prod", Err: errors.New("unreachable")})

	if !m.footer.IsSet() {
		t.Error("a failed refresh reported nothing")
	}
}

// Rule 130: the refresh is only offered where it does something.
func TestTheGroupRefreshShortcutFollowsTheSelectedRow(t *testing.T) {
	m := registriesTab(t)

	if !hasShortcut(m, "Refresh group members") {
		t.Error("no group-refresh shortcut is offered on a group row")
	}

	m.registryTable.Update(testutil.Key("down")) // docker.io, a plain registry
	if hasShortcut(m, "Refresh group members") {
		t.Error("the group-refresh shortcut is offered on a registry with no members")
	}
}

func hasShortcut(m Model, description string) bool {
	_, ok := shortcutByDescription(m, description)
	return ok
}

// shortcutGreyed reports whether the entry is advertised but disabled. A
// missing entry is not greyed — it is absent, which Rule 130 forbids for a
// state, so the two are asserted apart.
func shortcutGreyed(m Model, description string) bool {
	s, ok := shortcutByDescription(m, description)
	return ok && s.Disabled
}

func shortcutByDescription(m Model, description string) (shortcut.Shortcut, bool) {
	for _, s := range m.GetShortcuts() {
		if s.Description == description {
			return s, true
		}
	}
	return shortcut.Shortcut{}, false
}

// ── Remembered selection (§3.8 step 6) ───────────────────────────────────────

// Reopening the browser must not undo what the user just narrowed it to.
func TestTheSelectionSurvivesClosingTheBrowser(t *testing.T) {
	m := groupedModel(t)
	dropped := m.registryBrowser.entries[0].key
	m.registryBrowser.selectedRegs[dropped] = false

	m = feed(t, m, RegistryBrowserCloseMsg{})
	if !m.browserDeselected[dropped] {
		t.Fatalf("closing the browser forgot the unchecked %q", dropped)
	}

	m = feed(t, m, testutil.Key(keymap.Browser))
	if m.registryBrowser.selectedRegs[dropped] {
		t.Errorf("%q came back checked", dropped)
	}
	for _, e := range m.registryBrowser.entries {
		if e.key != dropped && !m.registryBrowser.selected(e) {
			t.Errorf("%q was unchecked too, want only the one", e.URL)
		}
	}
}

// What is remembered is the exclusions, so a member discovered since the last
// visit arrives checked like every other new entry rather than silently sitting
// out of every search.
func TestAMemberDiscoveredSinceTheLastVisitArrivesChecked(t *testing.T) {
	m := groupedModel(t)
	m.registryBrowser.selectedRegs[m.registryBrowser.entries[0].key] = false
	m = feed(t, m, RegistryBrowserCloseMsg{})

	// A third member turns up in the cache.
	entries := groupCacheFixture()
	entries["prod"] = cache.RegistryGroupEntry{
		Members: append(entries["prod"].Members,
			cache.RegistryGroupMember{Alias: "new", URL: "registry.example.com", RepoPrefix: "new-proxy"}),
	}
	m = feed(t, m, RegistryGroupCacheLoadedMsg{Entries: entries}, testutil.Key(keymap.Browser))

	if !m.registryBrowser.selectedRegs[memberKey("prod", "registry.example.com", "new-proxy")] {
		t.Error("a member discovered since the last visit opened unchecked")
	}
}

// ── Two entries on one host (D40) ────────────────────────────────────────────

// A checkbox belongs to an entry, not to a URL. Two registries declared on one
// host is something the form accepts — it enforces slug uniqueness, not URL
// uniqueness — and §3.18 makes it the ordinary case: one entry per proxy, all
// of them on the instance's host.

// oneHostModel returns a browser over two registries sharing a URL.
func oneHostModel(t *testing.T) Model {
	t.Helper()
	cfg := testConfig()
	cfg.Registry.Registries = []config.RegistryItem{
		{URL: "nexus.example.com", Alias: "dhi", Slug: "dhi", AuthMode: config.AuthAnonymous},
		{URL: "nexus.example.com", Alias: "quay", Slug: "quay", AuthMode: config.AuthAnonymous},
	}
	m := feed(t, New(cfg), tea.WindowSizeMsg{Width: 180, Height: 30}, ImagesListMsg{Images: imageFixtures()})
	m = feed(t, m, testutil.Key(keymap.Browser))
	if m.registryBrowser == nil {
		t.Fatal("'b' did not open the registry browser")
	}
	if len(m.registryBrowser.entries) != 2 {
		t.Fatalf("the picker holds %d entries, want one per configured registry", len(m.registryBrowser.entries))
	}
	return m
}

func TestTwoRegistriesOnOneHostKeepIndependentCheckboxes(t *testing.T) {
	m := oneHostModel(t)
	b := m.registryBrowser

	feed(t, m, testutil.Key("down"), testutil.Key(" "))

	if b.selected(b.entries[0]) {
		t.Error("space did not uncheck the focused entry")
	}
	if !b.selected(b.entries[1]) {
		t.Error("unchecking one entry unchecked the other one declared on the same host")
	}
}

// The half that outlives the session: an exclusion is what gets remembered, so
// a collision here excludes the other entry from every future search too.
func TestAnExclusionOnOneHostDoesNotCarryToTheOtherEntry(t *testing.T) {
	m := oneHostModel(t)

	m = feed(t, m, testutil.Key("down"), testutil.Key(" "), RegistryBrowserCloseMsg{})

	if len(m.browserDeselected) != 1 {
		t.Fatalf("closing remembered %d exclusions, want only the entry unchecked", len(m.browserDeselected))
	}

	m = feed(t, m, testutil.Key(keymap.Browser))
	b := m.registryBrowser
	if b.selected(b.entries[0]) {
		t.Error("the unchecked entry came back checked")
	}
	if !b.selected(b.entries[1]) {
		t.Error("the other entry on that host came back unchecked, and nothing will check it again")
	}
}

// The obvious key is the wrong one: an entry's Slug is its *group's* for a
// member, so keying the selection on it would give a whole group one checkbox.
func TestGroupMembersKeepIndependentCheckboxes(t *testing.T) {
	m := groupedModel(t)
	b := m.registryBrowser
	if b.entries[0].ParentSlug == "" || b.entries[0].ParentSlug != b.entries[1].ParentSlug {
		t.Fatal("the fixture no longer opens on two members of one group")
	}

	// Two rows down: the group header, then its first member.
	feed(t, m, testutil.Key("down"), testutil.Key("down"), testutil.Key(" "))

	if b.selected(b.entries[0]) {
		t.Error("space did not uncheck the focused member")
	}
	if !b.selected(b.entries[1]) {
		t.Error("unchecking one member unchecked its sibling: the group shares one checkbox")
	}
}

// ── Drill-down in the Registries tab (§3.8 step 5) ───────────────────────────

func TestEnteringAGroupListsItsMembers(t *testing.T) {
	m := feed(t, registriesTab(t), RegistryGroupCacheLoadedMsg{Entries: groupCacheFixture()})

	m = feed(t, m, testutil.Key("right"))

	if m.registryGroupSlug != "prod" {
		t.Fatalf("registryGroupSlug = %q, want the group entered", m.registryGroupSlug)
	}
	if got := cells(m.registryTable.Table().Rows(), 0); !equal(got, []string{"docker-hosted", "dhi"}) {
		t.Errorf("the table holds %v, want the group's members", got)
	}
	// A member is not a config entry: its credentials are the group's.
	if got := m.registryTable.Table().Rows()[0][3]; got != config.AuthInherit {
		t.Errorf("a member's Auth reads %q, want %q", got, config.AuthInherit)
	}
	if crumb := m.renderRegistryBreadcrumb(120); !strings.Contains(crumb, "prod") {
		t.Errorf("the breadcrumb does not name the group:\n%s", crumb)
	}

	m = feed(t, m, testutil.Key("left"))
	if m.registryGroupSlug != "" {
		t.Errorf("left did not go back up, still in %q", m.registryGroupSlug)
	}
	if m.renderRegistryBreadcrumb(120) != "" {
		t.Error("the breadcrumb survived going back to the top level")
	}
}

// A group nobody has discovered has no level to enter, and saying so beats an
// empty table with no explanation.
func TestEnteringAGroupWithNoMembersSaysWhy(t *testing.T) {
	m := registriesTab(t) // the group cache is empty here

	m = feed(t, m, testutil.Key("right"))

	if m.registryGroupSlug != "" {
		t.Error("the tab drilled into a group with nothing discovered")
	}
	if !strings.Contains(m.footer.Text(), "ctrl+r") {
		t.Errorf("footer = %q, want it to name the key that would help", m.footer.Text())
	}
}

// Inside a group the rows are cached members, not config entries — nothing on
// them is editable, so the actions are greyed. A level of the same table is not
// a different screen, so the column keeps its shape (Rule 130).
func TestInsideAGroupTheEntryActionsAreGreyed(t *testing.T) {
	top := feed(t, registriesTab(t), RegistryGroupCacheLoadedMsg{Entries: groupCacheFixture()})
	m := feed(t, top, testutil.Key("right"))

	topKeys := testutil.ShortcutKeys(top.GetShortcuts())
	insideKeys := testutil.ShortcutKeys(m.GetShortcuts())
	if strings.Join(topKeys, " ") != strings.Join(insideKeys, " ") {
		t.Errorf("inside a group the tab advertises %v, want the same keys as the list %v", insideKeys, topKeys)
	}

	for _, greyed := range []string{"Edit registry", "Log in or out", "Remove", "New registry", "Show members"} {
		if !hasShortcut(m, greyed) {
			t.Errorf("%q disappeared on a discovered member instead of being greyed", greyed)
		} else if !shortcutGreyed(m, greyed) {
			t.Errorf("%q is still offered on a discovered member", greyed)
		}
	}
	if shortcutGreyed(m, "Back to registries") {
		t.Error("the way back is greyed inside a group")
	}

	// And the actions themselves refuse, naming why rather than returning in
	// silence — the header greys them from the same answer.
	m = feed(t, m, testutil.Key(keymap.Edit))
	if m.registryForm != nil || m.confirmModal != nil {
		t.Error("an entry action fired on a discovered member")
	}
	if !strings.Contains(m.footer.Text(), reasonInsideGroup) {
		t.Errorf("footer = %q, want it to carry %q", m.footer.Text(), reasonInsideGroup)
	}
}

// esc is the other way back (Rule 111), and must not close anything at the top.
func TestEscLeavesAGroupAndDoesNothingAtTheTop(t *testing.T) {
	m := feed(t, registriesTab(t), RegistryGroupCacheLoadedMsg{Entries: groupCacheFixture()})
	m = feed(t, m, testutil.Key("right"))

	m = feed(t, m, testutil.Key("esc"))
	if m.registryGroupSlug != "" {
		t.Error("esc did not leave the group")
	}

	m = feed(t, m, testutil.Key("esc"))
	if m.activeTab != tabRegistries {
		t.Error("esc at the top level moved away from the tab")
	}
}

// The selection reaches disk, so it survives the process and not just the view.
func TestTheSelectionIsWrittenToDisk(t *testing.T) {
	run(t, saveBrowserSelectionCmd([]string{"docker.io"}))
	t.Cleanup(func() {
		if c, err := cache.NewBrowserSelectionCache(); err == nil {
			ctx, _ := config.GetCurrentContext()
			_ = c.SetDeselected(ctx, nil)
		}
	})

	msg := run(t, loadBrowserSelectionCmd()).(BrowserSelectionLoadedMsg)

	if !msg.Deselected["docker.io"] {
		t.Errorf("Deselected = %v after a save, want the entry that was unchecked", msg.Deselected)
	}
}

// ── The Registries table after §3.21 ─────────────────────────────────────────

// Rule 116, now the solver's job: the copy written out here clamped URL at 20
// *after* the remainder had been computed, which pushes the sum back over the
// space available and leaves the selected row overrunning the border.
func TestRegistryColumnsHoldTheWidthInvariant(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120, 200} {
		m := feed(t, registriesTab(t), tea.WindowSizeMsg{Width: width, Height: 40})

		for i, col := range m.registryTable.Table().Columns() {
			if col.Width < 0 {
				t.Errorf("at width %d column %d is %d cells wide", width, i, col.Width)
			}
		}
		// RenderedWidth rather than the declared columns plus two cells each: a
		// column dropped for want of room renders nothing and hands its padding
		// back, so that arithmetic asks for less than the line spans (D61).
		if got, want := m.registryTable.RenderedWidth(), width-2; got != want {
			t.Errorf("at width %d the line spans %d, want %d", width, got, want)
		}
	}
}

// Rule 122: the Members cell used to render the spinner through its style while
// a discovery ran. Nothing bled today because the bubbles default emits no
// escape sequence, but one line setting a style would have made it.
func TestTheRefreshingCellCarriesNoEscapeSequence(t *testing.T) {
	m := feed(t, registriesTab(t), testutil.Key("ctrl+r"))

	for _, row := range m.registryTable.Table().Rows() {
		for col, cell := range row {
			if strings.Contains(cell, "\x1b[") {
				t.Errorf("row cell [%d] = %q carries an escape sequence", col, cell)
			}
		}
	}
}

// The two populations share one table, so a row has to say which it belongs to.
// Resolving the cursor by indexing m.registries is right today only because
// this table neither sorts nor filters — the dependency nothing signalled.
func TestAMemberRowStandsForNoConfigEntry(t *testing.T) {
	m := feed(t, registriesTab(t), RegistryGroupCacheLoadedMsg{Entries: groupCacheFixture()})

	if idx := m.getSelectedRegistryIndex(); idx != 0 {
		t.Errorf("at the top level the first row resolves to index %d, want 0", idx)
	}

	m = feed(t, m, testutil.Key("right"))
	if idx := m.getSelectedRegistryIndex(); idx != -1 {
		t.Errorf("a discovered member resolves to config index %d, want none", idx)
	}
	if reg := m.getSelectedRegistry(); reg != nil {
		t.Errorf("a discovered member resolves to the config entry %+v", reg)
	}
}
