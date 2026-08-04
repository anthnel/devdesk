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
	"github.com/anthnel/devdesk/internal/registrymgr"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The browser searches registries over HTTP and pulls through Docker. No test
// executes one: tag lists, group detections and pull results are fed in as
// messages.
//
// It is opened through the model rather than constructed directly, because the
// model is what wires the scan cache in and what the router talks to.

// browsingModel returns a model with the registry browser open and its group
// detections resolved, which is the state the search form is reachable from.
func browsingModel(t *testing.T) Model {
	t.Helper()
	m := feed(t, loadedModel(t), testutil.Key("b"))
	if m.registryBrowser == nil {
		t.Fatal("'b' did not open the registry browser")
	}

	// Two registries are configured, so two detections are outstanding.
	for _, url := range []string{"registry.example.com", "docker.io"} {
		m = feed(t, m, RegistryGroupDetectedMsg{RegistryURL: url})
	}
	return m
}

// searchedModel returns a browser showing tags from both registries.
func searchedModel(t *testing.T) Model {
	t.Helper()
	return feed(t, browsingModel(t),
		MultiRegistryTagsLoadedMsg{
			RegistryURL: "registry.example.com", Alias: "prod", Repo: "api",
			Tags: []string{"v1", "v2"},
		},
		MultiRegistryTagsLoadedMsg{
			RegistryURL: "docker.io", Alias: "hub", Repo: "api",
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

	m = feed(t, m, testutil.Key("b"))

	if m.registryBrowser != nil {
		t.Error("the browser opened with no registries configured")
	}
	if !strings.Contains(m.errorMsg, "No registries") {
		t.Errorf("errorMsg = %q, want it to name the missing configuration", m.errorMsg)
	}
}

// A registry may be a group fronting several members; the browser resolves that
// before showing the list, so the user picks real registries rather than one
// alias hiding four.
func TestBrowserResolvesGroupsBeforeShowingTheForm(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("b"))

	if m.registryBrowser.state != browserStateResolving {
		t.Errorf("the browser opened in state %d, want it resolving", m.registryBrowser.state)
	}

	m = feed(t, m, RegistryGroupDetectedMsg{
		RegistryURL: "registry.example.com",
		Slug:        "prod",
		Members: []registrymgr.GroupMember{
			{Alias: "docker-hosted", URL: "registry.example.com/hosted"},
			{Alias: "docker-proxy", URL: "registry.example.com/proxy"},
		},
	})

	if m.registryBrowser.state != browserStateInput {
		t.Errorf("the browser is in state %d once every detection landed", m.registryBrowser.state)
	}
	// The group expanded into its two members, plus the plain registry.
	if got := len(m.registryBrowser.entries); got != 3 {
		t.Errorf("entries = %d, want the group's two members plus docker.io", got)
	}
}

// Only a group is waited for. A plain registry has nothing to discover, and
// before the provider was declared every one of them cost a round trip that
// could only come back saying so — which is the wait D13 is about.
func TestOnlyGroupsAreWaitedFor(t *testing.T) {
	cfg := testConfig()
	for i := range cfg.Registry.Registries {
		cfg.Registry.Registries[i].Kind = config.KindRegistry
	}
	m := feed(t, New(cfg), tea.WindowSizeMsg{Width: 180, Height: 30}, ImagesListMsg{Images: imageFixtures()})

	m = feed(t, m, testutil.Key("b"))

	if m.registryBrowser.state != browserStateInput {
		t.Errorf("the browser opened in state %d with no group configured, want the form straight away",
			m.registryBrowser.state)
	}
	if got := len(m.registryBrowser.entries); got != 2 {
		t.Errorf("entries = %d, want both registries offered as themselves", got)
	}
}

// A result for something that was never waited for must not be counted, or it
// finalizes the list a second time and drops what the user had unchecked.
func TestAStrayDetectionIsIgnored(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("b"))
	before := m.registryBrowser.pendingDetections

	m = feed(t, m, RegistryGroupDetectedMsg{RegistryURL: "docker.io", Slug: "hub"})

	if got := m.registryBrowser.pendingDetections; got != before {
		t.Errorf("pendingDetections = %d after a result for a plain registry, want %d", got, before)
	}
	if m.registryBrowser.state != browserStateResolving {
		t.Errorf("the browser left the resolving state on a result it never asked for")
	}
}

// A detection that fails must not strand the browser on its spinner: the
// registry is offered as itself.
func TestBrowserSurvivesAFailedDetection(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("b"))

	m = feed(t, m,
		RegistryGroupDetectedMsg{RegistryURL: "registry.example.com", Err: errors.New("404 not found")},
		RegistryGroupDetectedMsg{RegistryURL: "docker.io"},
	)

	if m.registryBrowser.state != browserStateInput {
		t.Errorf("a failed detection left the browser in state %d", m.registryBrowser.state)
	}
	if len(m.registryBrowser.entries) != 2 {
		t.Errorf("entries = %d, want both registries offered as themselves", len(m.registryBrowser.entries))
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
		RegistryURL: "registry.example.com", Repo: "api", Tags: []string{"v1"},
	})
	if !m.registryBrowser.IsSearching() {
		t.Error("the browser stopped searching with one registry outstanding")
	}

	m = feed(t, m, MultiRegistryTagsLoadedMsg{RegistryURL: "docker.io", Repo: "api", Tags: []string{"latest"}})
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
		MultiRegistryTagsLoadedMsg{RegistryURL: "registry.example.com", Repo: "api", Tags: []string{"v1"}},
		MultiRegistryTagsLoadedMsg{RegistryURL: "registry.example.com", Repo: "api", Tags: []string{"v1"}},
		MultiRegistryTagsLoadedMsg{RegistryURL: "registry.example.com", Repo: "api", Tags: []string{"v1"}},
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
		MultiRegistryTagsLoadedMsg{RegistryURL: "registry.example.com", Repo: "api", Tags: []string{"v1", "v2"}},
		MultiRegistryTagsLoadedMsg{RegistryURL: "docker.io", Repo: "api", Err: errors.New("401 unauthorized")},
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
		RegistryURL: "registry.example.com",
		Repo:        "api",
		Meta:        map[string]time.Time{"v1": at(1)},
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

func TestPullResultIsReported(t *testing.T) {
	ok := feed(t, searchedModel(t), RegistryPullCompleteMsg{ImageName: "registry.example.com/api:v1"})
	if ok.errorMsg != "" {
		t.Errorf("a successful pull reported %q", ok.errorMsg)
	}

	failed := feed(t, searchedModel(t), RegistryPullCompleteMsg{
		ImageName: "registry.example.com/api:v1", Err: errors.New("manifest unknown"),
	})
	if failed.errorMsg == "" {
		t.Error("a failed pull reported nothing")
	}
}

// ── Closing ──────────────────────────────────────────────────────────────────

// While the browser is open it owns every key, so the images tab's shortcuts
// must not fire behind it.
func TestTheBrowserSwallowsTheViewShortcuts(t *testing.T) {
	m := browsingModel(t)

	m = feed(t, m, testutil.Key("ctrl+d"), testutil.Key("p"))

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
func TestResolvedMembersCarryTheirGroupsMode(t *testing.T) {
	b, _ := newRegistryBrowser([]config.RegistryItem{
		{URL: "registry.example.com", Alias: "prod", AuthMode: config.AuthAnonymous},
	}, 120, 30)

	b, _ = b.HandleGroupDetected(RegistryGroupDetectedMsg{
		RegistryURL: "registry.example.com",
		Members: []registrymgr.GroupMember{
			{Alias: "hosted", URL: "registry.example.com/repository/docker-hosted"},
		},
	})

	if len(b.entries) != 1 {
		t.Fatalf("got %d entries, want the one member", len(b.entries))
	}
	if b.entries[0].authMode != config.AuthAnonymous {
		t.Errorf("the member resolved as %q, want its anonymous group's mode", b.entries[0].authMode)
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

	rows := m.registryTable.Rows()
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
	if got := m.registryTable.Rows()[0][members]; got != "never" {
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
	if got := m.registryTable.Rows()[0][members]; !strings.Contains(got, "refreshing") {
		t.Errorf("Members = %q while refreshing, want it to say so", got)
	}
}

// A plain registry has nothing to discover, so ctrl+r must not pretend to.
func TestCtrlROnAPlainRegistryStartsNoDiscovery(t *testing.T) {
	m := registriesTab(t)
	m.registryTable.MoveDown(1) // docker.io

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

	if m.errorMsg == "" {
		t.Error("a failed refresh reported nothing")
	}
}

// Rule 130: the refresh is only offered where it does something.
func TestTheGroupRefreshShortcutFollowsTheSelectedRow(t *testing.T) {
	m := registriesTab(t)

	if !hasShortcut(m, "Refresh group members") {
		t.Error("no group-refresh shortcut is offered on a group row")
	}

	m.registryTable.MoveDown(1) // docker.io, a plain registry
	if hasShortcut(m, "Refresh group members") {
		t.Error("the group-refresh shortcut is offered on a registry with no members")
	}
}

func hasShortcut(m Model, description string) bool {
	for _, s := range m.GetShortcuts() {
		if s.Description == description {
			return true
		}
	}
	return false
}
