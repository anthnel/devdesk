package ociresources

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
		Members: []registrymgr.GroupMember{
			{Alias: "docker-hosted", URL: "registry.example.com/hosted"},
			{Alias: "docker-proxy", URL: "registry.example.com/proxy"},
		},
	})
	if m.registryBrowser.state != browserStateResolving {
		t.Error("the browser stopped resolving with one detection outstanding")
	}

	m = feed(t, m, RegistryGroupDetectedMsg{RegistryURL: "docker.io"})
	if m.registryBrowser.state != browserStateInput {
		t.Errorf("the browser is in state %d once every detection landed", m.registryBrowser.state)
	}
	// The group expanded into its two members, plus the plain registry.
	if got := len(m.registryBrowser.entries); got != 3 {
		t.Errorf("entries = %d, want the group's two members plus docker.io", got)
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
