package ociresources

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The search form and the results table, driven through the model — the router
// is what forwards keys to the browser, and the model is what wires the scan
// cache in. registry_browser_test.go covers opening, group resolution and the
// arrival of results; this file covers what the user does once it is open.

// groupedModel returns a browser whose first registry resolved into a group of
// two members, so the entry list mixes members and a plain registry.
func groupedModel(t *testing.T) Model {
	t.Helper()
	m := feed(t, loadedModel(t), RegistryGroupCacheLoadedMsg{Entries: groupCacheFixture()})
	return feed(t, m, testutil.Key(keymap.Browser))
}

func typeInto(t *testing.T, m Model, s string) Model {
	t.Helper()
	return feed(t, m, testutil.Type(s)...)
}

// resultsModel returns a browser on its results screen, having actually
// submitted a search. searchedModel feeds tags in without submitting, which is
// enough to test where results land but leaves the browser on the form — the
// results table is only rebuilt while the tags screen is showing.
func resultsModel(t *testing.T, msgs ...tea.Msg) Model {
	t.Helper()

	m := typeInto(t, browsingModel(t), "api")
	m = feed(t, m, testutil.Key("enter"))
	if m.registryBrowser.state != browserStateTags {
		t.Fatalf("the search did not reach the results screen (state %d)", m.registryBrowser.state)
	}
	if len(msgs) == 0 {
		msgs = []tea.Msg{
			MultiRegistryTagsLoadedMsg{RegistryURL: "registry.example.com", Alias: "prod", Repo: "api", Tags: []string{"v1", "v2"}},
			MultiRegistryTagsLoadedMsg{RegistryURL: "docker.io", Alias: "hub", Repo: "api", Tags: []string{"latest"}},
		}
	}
	return feed(t, m, msgs...)
}

// ── The search form ──────────────────────────────────────────────────────────

// Rule 135: ↑ / ↓ walk the fields. They clamp rather than wrap here, so the
// list of registries can be walked without the focus jumping past its ends.
func TestTheFormWalksFromTheRepoThroughEveryRegistryToTheButton(t *testing.T) {
	m := browsingModel(t)
	b := m.registryBrowser

	if b.focusedField != brFieldRepo {
		t.Fatalf("the form opened on field %d, want the repository", b.focusedField)
	}
	m = feed(t, m, testutil.Key("up"))
	if b.focusedField != brFieldRepo {
		t.Errorf("focus = %d, want it held on the first field", b.focusedField)
	}

	for i := range b.entries {
		m = feed(t, m, testutil.Key("down"))
		if b.focusedField != b.brFieldReg(i) {
			t.Fatalf("focus = %d, want the checkbox for entry %d", b.focusedField, i)
		}
	}
	m = feed(t, m, testutil.Key("down"))
	if b.focusedField != b.brFieldSubmit() {
		t.Errorf("focus = %d, want the button", b.focusedField)
	}
	feed(t, m, testutil.Key("down"))
	if b.focusedField != b.brFieldSubmit() {
		t.Errorf("focus = %d, want it held on the button", b.focusedField)
	}
}

// Every registry starts checked: the common case is searching all of them, and
// an empty selection would make the button do nothing on open.
func TestEveryRegistryStartsSelected(t *testing.T) {
	b := browsingModel(t).registryBrowser

	for _, entry := range b.entries {
		if !b.selected(entry) {
			t.Errorf("%s opened unchecked", entry.URL)
		}
	}
}

// Rule 135: space is the only key that toggles a checkbox.
func TestSpaceTogglesTheFocusedRegistry(t *testing.T) {
	m := browsingModel(t)
	b := m.registryBrowser
	first := b.entries[0]

	m = feed(t, m, testutil.Key("down"), testutil.Key(" "))
	if b.selected(first) {
		t.Error("space did not uncheck the focused registry")
	}
	feed(t, m, testutil.Key(" "))
	if !b.selected(first) {
		t.Error("space did not check it back")
	}
}

// Space on the repository field is a character, not a toggle — a repository
// name is text and the field has to take it.
func TestSpaceOnTheRepositoryFieldIsTyped(t *testing.T) {
	m := browsingModel(t)
	b := m.registryBrowser

	m = typeInto(t, m, "my repo")

	if b.repoInput.Value() != "my repo" {
		t.Errorf("repository = %q, want the space kept", b.repoInput.Value())
	}
	for _, entry := range b.entries {
		if !b.selected(entry) {
			t.Errorf("typing a space unchecked %s", entry.URL)
		}
	}
}

// Enter searches from the repository field — the user has just typed the name
// and should not have to walk to the button.
func TestEnterOnTheRepositoryFieldSearches(t *testing.T) {
	m := typeInto(t, browsingModel(t), "nginx")

	m = feed(t, m, testutil.Key("enter"))

	if m.registryBrowser.state != browserStateTags {
		t.Errorf("state = %d, want the results screen", m.registryBrowser.state)
	}
	if got := m.registryBrowser.pendingSearches; got != 2 {
		t.Errorf("pendingSearches = %d, want one per selected registry", got)
	}
}

// On a checkbox, Enter advances instead — Rule 135 keeps it off the toggle, and
// it has to do something rather than nothing.
func TestEnterOnACheckboxAdvances(t *testing.T) {
	m := typeInto(t, browsingModel(t), "nginx")
	b := m.registryBrowser

	m = feed(t, m, testutil.Key("down"), testutil.Key("enter"))

	if b.state != browserStateInput {
		t.Fatalf("enter on a checkbox started a search (state %d)", b.state)
	}
	if b.focusedField != b.brFieldReg(1) {
		t.Errorf("focus = %d, want the next checkbox", b.focusedField)
	}
}

func TestSearchingWithNoRepositoryDoesNothing(t *testing.T) {
	m := browsingModel(t)

	m = feed(t, m, testutil.Key("enter"))

	if m.registryBrowser.state != browserStateInput {
		t.Errorf("state = %d, want the form still open", m.registryBrowser.state)
	}
	if m.registryBrowser.pendingSearches != 0 {
		t.Error("a search was started with no repository")
	}
}

// Unchecking everything is a way to ask for nothing, and it must not leave the
// user on an empty results screen with no way to tell why.
func TestSearchingWithEveryRegistryUncheckedDoesNothing(t *testing.T) {
	m := typeInto(t, browsingModel(t), "nginx")
	b := m.registryBrowser
	for _, entry := range b.entries {
		b.selectedRegs[entry.key] = false
	}

	m = feed(t, m, testutil.Key("enter"))

	if b.state != browserStateInput {
		t.Errorf("state = %d, want the form still open", b.state)
	}
	if b.pendingSearches != 0 {
		t.Errorf("pendingSearches = %d, want none", b.pendingSearches)
	}
}

// Only the checked registries are queried, which is the point of the checkbox.
func TestOnlyTheCheckedRegistriesAreSearched(t *testing.T) {
	m := typeInto(t, browsingModel(t), "nginx")
	b := m.registryBrowser
	b.selectedRegs[b.entries[0].key] = false

	m = feed(t, m, testutil.Key("enter"))

	if got := b.pendingSearches; got != 1 {
		t.Errorf("pendingSearches = %d, want only the checked registry", got)
	}
}

// A second search must not show the first one's tags, filter or sort — the
// results screen is about the repository just asked for.
func TestASecondSearchStartsFromAClearTable(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser
	b.filterInput.SetValue("v1")
	b.registryFilter = resultFilter{url: "docker.io"}
	b.tagTable.SetSort(tagColumnUpdated, true)

	m = feed(t, m, testutil.Key("esc")) // back to the form
	m = typeInto(t, m, "redis")
	m = feed(t, m, testutil.Key("enter"))

	if len(b.tags) != 0 {
		t.Errorf("tags = %v, want the previous results dropped", b.tags)
	}
	if b.filterInput.Value() != "" || !b.registryFilter.isEmpty() {
		t.Errorf("filters survived: %q / %q", b.filterInput.Value(), b.registryFilter)
	}
	if col, desc := b.tagTable.SortState(); col != tagColumnTag || desc {
		t.Errorf("the sort is column %d desc %v, want it back on the tag name ascending", col, desc)
	}
}

// A group member is shown under its parent, and the results table has to say
// which group a tag came from — "dhi" alone is ambiguous across two Nexus
// instances.
func TestAGroupMemberIsSearchedUnderAQualifiedAlias(t *testing.T) {
	m := groupedModel(t)
	b := m.registryBrowser

	var member *browserRegistryEntry
	for i := range b.entries {
		if b.entries[i].ParentAlias != "" {
			member = &b.entries[i]
			break
		}
	}
	if member == nil {
		t.Fatal("the group did not expand into members")
	}
	if member.ParentAlias != "prod" {
		t.Errorf("ParentAlias = %q, want the configured alias of the group", member.ParentAlias)
	}
	// The parent is what credentials are looked up under: Docker keys them by
	// host, and the members share the group's host.
	if member.parentURL != "registry.example.com" {
		t.Errorf("parentURL = %q, want the group's own URL", member.parentURL)
	}
}

func TestEscOnTheFormClosesTheBrowser(t *testing.T) {
	m := browsingModel(t)

	_, cmd := step(t, m, testutil.Key("esc"))

	if _, ok := testutil.MsgOf[RegistryBrowserCloseMsg](cmd); !ok {
		t.Error("esc did not ask to close the browser")
	}
}

// ── Repository names ─────────────────────────────────────────────────────────

// Docker Hub keeps its own images under library/, and a bare "nginx" is how
// every user refers to them — the browser has to translate rather than 404.
func TestABareNameIsQualifiedForDockerHubOnly(t *testing.T) {
	cases := []struct {
		registry string
		repo     string
		want     string
	}{
		{"docker.io", "nginx", "library/nginx"},
		{"registry-1.docker.io", "nginx", "library/nginx"},
		{"DOCKER.IO", "nginx", "library/nginx"},
		{"docker.io", "bitnami/nginx", "bitnami/nginx"},
		{"registry.example.com", "nginx", "nginx"},
	}
	for _, tc := range cases {
		if got := normalizeRepoForRegistry(tc.registry, tc.repo); got != tc.want {
			t.Errorf("normalizeRepoForRegistry(%q, %q) = %q, want %q", tc.registry, tc.repo, got, tc.want)
		}
	}
}

// The pull reference is not the browse URL: Docker Hub images are pulled by
// bare name, everything else carries its registry host.
func TestThePullReferenceCarriesTheRegistryExceptOnTheHub(t *testing.T) {
	cases := []struct {
		registry, repo, tag, want string
	}{
		{"docker.io", "library/nginx", "1.25", "library/nginx:1.25"},
		{"registry-1.docker.io", "library/nginx", "1.25", "library/nginx:1.25"},
		{"registry.example.com", "api", "v1", "registry.example.com/api:v1"},
		{"registry.example.com/", "api", "v1", "registry.example.com/api:v1"},
	}
	for _, tc := range cases {
		if got := multiImageName(tc.registry, tc.repo, tc.tag); got != tc.want {
			t.Errorf("multiImageName(%q, %q, %q) = %q, want %q", tc.registry, tc.repo, tc.tag, got, tc.want)
		}
	}
}

// D39: a scheme is not part of a Docker reference. Nothing stopped one reaching
// the pull, and every case below produced a string `docker pull` rejects
// outright — including the Nexus member URLs the group discovery synthesises,
// which inherit their group's scheme.
//
// The path form is confirmed pullable on a real Nexus (§3.8), so one URL per
// member is enough and the browse URL is the pull URL with its scheme removed.
func TestThePullReferenceNeverCarriesAScheme(t *testing.T) {
	cases := []struct {
		name, registry, repo, tag, want string
	}{
		{"https is stripped", "https://registry.example.com", "api", "v1", "registry.example.com/api:v1"},
		{"http is stripped too", "http://registry.example.com:5000", "api", "v1", "registry.example.com:5000/api:v1"},
		{"an uppercase scheme is still a scheme", "HTTPS://registry.example.com", "api", "v1", "registry.example.com/api:v1"},
		{"a Nexus member keeps its path", "https://nexus.example.com/repository/dhi", "alpine", "3.19", "nexus.example.com/repository/dhi/alpine:3.19"},
		{"the hub is recognised through a scheme", "https://docker.io", "library/nginx", "1.25", "library/nginx:1.25"},
		{"surrounding space is not a host", "  registry.example.com  ", "api", "v1", "registry.example.com/api:v1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := multiImageName(tc.registry, tc.repo, tc.tag)
			if got != tc.want {
				t.Errorf("multiImageName(%q, %q, %q) = %q, want %q", tc.registry, tc.repo, tc.tag, got, tc.want)
			}
			if strings.Contains(got, "://") {
				t.Errorf("the reference carries a scheme: %q", got)
			}
		})
	}
}

// The hub is the one registry whose references carry no host, so failing to
// recognise it costs the `library/` prefix and every bare image name 404s.
func TestTheHubIsRecognisedWhicheverWayItIsWritten(t *testing.T) {
	for _, url := range []string{
		"docker.io", "registry-1.docker.io",
		"https://docker.io", "https://registry-1.docker.io/", "HTTP://Docker.IO",
	} {
		if got := normalizeRepoForRegistry(url, "nginx"); got != "library/nginx" {
			t.Errorf("normalizeRepoForRegistry(%q, \"nginx\") = %q, want the library prefix", url, got)
		}
		if got := registryAPIURL(url); got != "https://registry-1.docker.io" {
			t.Errorf("registryAPIURL(%q) = %q, want the hub's API host", url, got)
		}
	}
}

// registryAPIURL is the one place a scheme is kept: an explicit http:// is how
// a registry on a plain-HTTP port is reached, and upgrading it would break that
// registry rather than fix anything.
func TestTheBrowseURLKeepsAnExplicitScheme(t *testing.T) {
	cases := map[string]string{
		"http://nexus.example.com:8081": "http://nexus.example.com:8081",
		"https://registry.example.com":  "https://registry.example.com",
		"registry.example.com":          "https://registry.example.com",
		"registry.example.com/":         "https://registry.example.com",
	}
	for in, want := range cases {
		if got := registryAPIURL(in); got != want {
			t.Errorf("registryAPIURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// ── The results table ────────────────────────────────────────────────────────

func TestEscOnTheResultsReturnsToTheForm(t *testing.T) {
	m := resultsModel(t)

	m = feed(t, m, testutil.Key("esc"))

	if m.registryBrowser.state != browserStateInput {
		t.Errorf("state = %d, want the search form", m.registryBrowser.state)
	}
}

// The dot cycles name ascending → name descending → updated → updated
// descending → back, and the header arrow is what tells the user where they
// are.
func TestTheSortCyclesThroughBothColumnsAndBack(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser

	want := []struct {
		col  int
		desc bool
	}{
		{tagColumnTag, true},
		{tagColumnUpdated, false},
		{tagColumnUpdated, true},
		{tagColumnTag, false},
	}
	for i, step := range want {
		m = feed(t, m, testutil.Key("."))
		col, desc := b.tagTable.SortState()
		if col != step.col || desc != step.desc {
			t.Fatalf("after %d presses: col %v desc %v, want %v/%v", i+1, col, desc, step.col, step.desc)
		}
	}
}

func TestTheSortedColumnCarriesTheArrow(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser

	if !strings.Contains(b.tagTable.Table().Columns()[1].Title, "▲") {
		t.Errorf("the Tag header is %q, want an ascending arrow", b.tagTable.Table().Columns()[1].Title)
	}
	m = feed(t, m, testutil.Key("."), testutil.Key("."))
	if !strings.Contains(b.tagTable.Table().Columns()[2].Title, "▲") {
		t.Errorf("the Updated header is %q, want the arrow to have moved", b.tagTable.Table().Columns()[2].Title)
	}
	if strings.Contains(b.tagTable.Table().Columns()[1].Title, "▲") {
		t.Error("the Tag header kept its arrow after the sort moved")
	}
}

func TestSortingByNameOrdersTheRows(t *testing.T) {
	m := resultsModel(t, MultiRegistryTagsLoadedMsg{
		RegistryURL: "registry.example.com", Alias: "prod", Repo: "api",
		Tags: []string{"v3", "v1", "v2"},
	})
	b := m.registryBrowser

	if got := tagOrder(b); strings.Join(got, ",") != "v1,v2,v3" {
		t.Errorf("tags = %v, want them ascending by name", got)
	}
	m = feed(t, m, testutil.Key("."))
	if got := tagOrder(b); strings.Join(got, ",") != "v3,v2,v1" {
		t.Errorf("tags = %v, want them descending", got)
	}
}

// Sorting by date is what the column is for: newest first, so the tag a user
// most likely wants is at the top.
func TestSortingByUpdatedPutsTheNewestFirst(t *testing.T) {
	m := resultsModel(t, MultiRegistryTagsLoadedMsg{
		RegistryURL: "registry.example.com", Alias: "prod", Repo: "api",
		Tags: []string{"old", "new"},
	})
	m = feed(t, m, MultiRegistryTagsMetaMsg{
		RegistryURL: "registry.example.com", Repo: "api",
		Meta: map[string]time.Time{"old": at(1), "new": at(5)},
	})
	b := m.registryBrowser

	m = feed(t, m, testutil.Key("."), testutil.Key(".")) // onto updated, ascending

	if got := tagOrder(b); strings.Join(got, ",") != "new,old" {
		t.Errorf("tags = %v, want the newest first", got)
	}
}

// Rule 136 is about the shared FilterBar; the browser predates it and has its
// own, but the behaviour has to be the same: "/" opens it, typing narrows the
// table, and confirming keeps the query.
func TestTheTagFilterNarrowsTheTable(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser

	m = feed(t, m, testutil.Key("/"))
	if !b.filterActive || !b.InEditMode() {
		t.Fatal("'/' did not open the filter")
	}

	m = typeInto(t, m, "v1")
	if got := tagOrder(b); strings.Join(got, ",") != "v1" {
		t.Errorf("tags = %v, want only the matching one", got)
	}

	m = feed(t, m, testutil.Key("enter"))
	if b.filterActive {
		t.Error("enter did not confirm the filter")
	}
	if b.filterInput.Value() != "v1" {
		t.Errorf("the query is %q after confirming, want it kept", b.filterInput.Value())
	}
	if !b.FilterIsVisible() {
		t.Error("a confirmed query left the filter bar hidden")
	}
}

// A tag typed in the filter also matches the repository, since one search can
// span repositories on different registries.
func TestTheTagFilterAlsoMatchesTheRepository(t *testing.T) {
	m := resultsModel(t,
		MultiRegistryTagsLoadedMsg{RegistryURL: "registry.example.com", Alias: "prod", Repo: "api", Tags: []string{"v1"}},
		MultiRegistryTagsLoadedMsg{RegistryURL: "docker.io", Alias: "hub", Repo: "web", Tags: []string{"v1"}},
	)
	b := m.registryBrowser

	m = feed(t, m, testutil.Key("/"))
	m = typeInto(t, m, "web")

	if got := len(b.tagTable.Visible()); got != 1 {
		t.Errorf("%d rows matched 'web', want the one whose repository does", got)
	}
}

// Escaping out of the filter leaves the query in place rather than clearing it:
// the bar is still shown, so the state stays legible.
func TestEscapingTheFilterKeepsTheQuery(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser
	m = feed(t, m, testutil.Key("/"))
	m = typeInto(t, m, "v")

	m = feed(t, m, testutil.Key("esc"))

	if b.filterActive {
		t.Error("esc did not leave the filter")
	}
	if b.filterInput.Value() != "v" {
		t.Errorf("the query is %q, want it kept", b.filterInput.Value())
	}
	if b.state != browserStateTags {
		t.Error("esc left the results screen instead of the filter")
	}
}

// 'r' walks the registries that actually returned something, then back to all.
func TestTheRegistryFilterCyclesThroughEachRegistryAndBack(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser

	m = feed(t, m, testutil.Key("r"))
	if b.registryFilter != (resultFilter{url: "registry.example.com"}) {
		t.Fatalf("registryFilter = %q, want the first registry", b.registryFilter)
	}
	if got := len(b.tagTable.Visible()); got != 2 {
		t.Errorf("%d rows shown, want only that registry's", got)
	}

	m = feed(t, m, testutil.Key("r"))
	if b.registryFilter != (resultFilter{url: "docker.io"}) {
		t.Fatalf("registryFilter = %q, want the second registry", b.registryFilter)
	}

	m = feed(t, m, testutil.Key("r"))
	if !b.registryFilter.isEmpty() {
		t.Errorf("registryFilter = %q, want it back to all", b.registryFilter)
	}
}

// With one registry answering there is nothing to cycle between, and leaving a
// filter set would hide nothing while claiming to filter.
func TestTheRegistryFilterIsInertWithASingleRegistry(t *testing.T) {
	m := resultsModel(t, MultiRegistryTagsLoadedMsg{
		RegistryURL: "registry.example.com", Alias: "prod", Repo: "api", Tags: []string{"v1"},
	})

	m = feed(t, m, testutil.Key("r"))

	if !m.registryBrowser.registryFilter.isEmpty() {
		t.Errorf("registryFilter = %q, want no filter with one registry", m.registryBrowser.registryFilter)
	}
}

// D14, fixed — this test was written inverted and is turned around here.
//
// The label used to be resolved against the configured registries only. A group
// member is discovered, not configured, so filtering to one fell through to the
// raw synthesised URL: the single row in the view showing a URL where every
// other showed a short alias. Members are entries now, so they resolve like
// anything else, qualified by the group they came from.
func TestAGroupMembersFilterLabelResolves(t *testing.T) {
	b := groupedModel(t).registryBrowser
	b.registryFilter = resultFilter{url: "registry.example.com/repository/dhi-proxy"}

	if got := b.registryFilterLabel(); got != "prod/dhi" {
		t.Errorf("registryFilterLabel() = %q, want the member qualified by its group", got)
	}

	// And the group level itself, which is what one keystroke narrows to.
	b.registryFilter = resultFilter{groupSlug: "prod"}
	if got := b.registryFilterLabel(); got != "prod" {
		t.Errorf("registryFilterLabel() = %q, want the group's alias", got)
	}
}

// A registry configured without an alias has nothing to shorten to, so the URL
// is the label.
func TestARegistryWithNoAliasIsLabelledByItsURL(t *testing.T) {
	cfg := testConfig()
	cfg.Registry.Registries[0].Alias = ""
	cfg.Registry.Registries[0].Kind = config.KindRegistry
	m := feed(t, New(cfg), tea.WindowSizeMsg{Width: 180, Height: 30}, ImagesListMsg{Images: imageFixtures()})
	m = feed(t, m, testutil.Key(keymap.Browser))

	b := m.registryBrowser
	b.registryFilter = resultFilter{url: "registry.example.com"}

	if got := b.registryFilterLabel(); got != "registry.example.com" {
		t.Errorf("registryFilterLabel() = %q, want the URL", got)
	}
}

// ── Acting on a tag ──────────────────────────────────────────────────────────

func TestPullingATagShowsTheOperation(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser

	m = feed(t, m, testutil.Key(keymap.Get))

	if b.state != browserStateStatus {
		t.Fatalf("state = %d, want the status screen", b.state)
	}
	if b.OperationImageName() == "" {
		t.Error("the pull did not record which image it is pulling")
	}
	if !strings.Contains(b.View(), "Pulling") {
		t.Error("the status screen does not say what it is doing")
	}
}

// The status screen owns no keys: the pull is not cancellable, and a key that
// changed the state under it would leave the answer arriving into the wrong
// screen.
func TestNoKeyActsWhileAPullRuns(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser
	m = feed(t, m, testutil.Key(keymap.Get))

	feed(t, m, testutil.Key("esc"), testutil.Key(keymap.Get), testutil.Key("."))

	if b.state != browserStateStatus {
		t.Errorf("state = %d, want the pull still showing", b.state)
	}
}

func TestAFinishedPullReturnsToTheTags(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser
	m = feed(t, m, testutil.Key(keymap.Get))

	b.SetOperationSuccess()

	if b.state != browserStateTags || b.OperationImageName() != "" {
		t.Errorf("state = %d, image = %q, want the results screen back", b.state, b.OperationImageName())
	}
}

func TestAFailedPullAlsoReturnsToTheTags(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser
	m = feed(t, m, testutil.Key(keymap.Get))

	b.SetOperationError("manifest unknown")

	if b.state != browserStateTags {
		t.Errorf("state = %d, want the results screen back so the user can retry", b.state)
	}
}

// ctrl+s scans the tag where it is, without pulling it first — that is the
// point of the direct scan.
func TestCtrlSAsksForADirectScanOfTheSelectedTag(t *testing.T) {
	m := resultsModel(t)

	_, cmd := step(t, m, testutil.Key(keymap.Scan))

	msg, ok := testutil.MsgOf[RegistryTagDirectScanMsg](cmd)
	if !ok {
		t.Fatal("ctrl+s asked for no scan")
	}
	if want := m.registryBrowser.selectedImageName(); msg.ImageName != want {
		t.Errorf("ImageName = %q, want the selected tag %q", msg.ImageName, want)
	}
}

// Rule 130: Enter opens details, and there are none to open until a scan has
// been cached — opening an empty panel would be worse than doing nothing.
func TestEnterOpensScanDetailsOnlyOnceThereAreSome(t *testing.T) {
	m := resultsModel(t)

	if _, cmd := step(t, m, testutil.Key("enter")); cmd != nil {
		t.Error("enter opened details for a tag that was never scanned")
	}

	b := m.registryBrowser
	name := b.selectedImageName()
	b.SetScanCache(map[string]cache.ImageScanEntry{name: {Critical: 1, ScannedAt: at(1)}})
	if !b.HasSelectedTagScanResults() {
		t.Fatal("the cached scan was not seen")
	}

	_, cmd := step(t, m, testutil.Key("enter"))
	msg, ok := testutil.MsgOf[ScanDetailsRequestMsg](cmd)
	if !ok {
		t.Fatal("enter did not open the cached details")
	}
	if msg.ImageName != name {
		t.Errorf("ImageName = %q, want the selected tag %q", msg.ImageName, name)
	}
}

// With no rows there is nothing under the cursor, and every action has to
// decline rather than act on a phantom row.
func TestTheTagActionsDeclineWithNothingSelected(t *testing.T) {
	m := browsingModel(t)
	b := m.registryBrowser
	b.state = browserStateTags

	for _, name := range []string{keymap.Get, keymap.Scan, "enter"} {
		next, cmd := step(t, m, testutil.Key(name))
		if cmd != nil {
			t.Errorf("%q acted with no tag selected", name)
		}
		if next.registryBrowser.state != browserStateTags {
			t.Errorf("%q changed the state with no tag selected", name)
		}
	}
	if b.HasSelectedTagScanResults() {
		t.Error("HasSelectedTagScanResults() is true with no rows")
	}
}

// ── The results table as rendered ────────────────────────────────────────────

// Rule 122: a styled cell is truncated mid-escape by bubbles/table and bleeds
// over every row below it.
func TestTagRowsCarryNoEscapeSequences(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser
	b.SetScanCache(map[string]cache.ImageScanEntry{
		b.selectedImageName(): {Critical: 3, High: 2, Medium: 1, ScannedAt: at(1)},
	})
	b.SetTagScanning("registry.example.com/api:v2", true)

	for _, row := range b.tagTable.Table().Rows() {
		for col, cell := range row {
			if strings.Contains(cell, "\x1b[") {
				t.Errorf("row cell [%d] = %q carries an escape sequence", col, cell)
			}
		}
	}
}

// The counts are the reason to scan from the browser at all, and a tag being
// scanned has to look different from one that never was.
func TestTheSeverityColumnsShowCachedCountsAndScanProgress(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser
	scanned := b.selectedImageName()

	b.SetScanCache(map[string]cache.ImageScanEntry{scanned: {Critical: 3, High: 2, Medium: 1, ScannedAt: at(1)}})
	if got := rowFor(t, b, scanned); got[3] != "3" || got[4] != "2" || got[5] != "1" || got[6] != "0" {
		t.Errorf("severity cells = %v, want the cached counts", got[3:])
	}

	b.SetTagScanning(scanned, true)
	if got := rowFor(t, b, scanned); got[3] == "3" {
		t.Errorf("severity cells = %v, want the scan in progress to replace the counts", got[3:])
	}

	b.SetTagScanning(scanned, false)
	if got := rowFor(t, b, scanned); got[3] != "3" {
		t.Errorf("severity cells = %v, want the counts back once the scan finished", got[3:])
	}
}

// An unscanned tag shows a dash rather than a zero: nought findings and never
// looked at are different answers.
func TestAnUnscannedTagShowsNoCounts(t *testing.T) {
	b := resultsModel(t).registryBrowser

	row := rowFor(t, b, b.selectedImageName())
	if row[3] != "-" || row[6] != "-" {
		t.Errorf("severity cells = %v, want dashes for a tag never scanned", row[3:])
	}
}

// Rule 127: relative times, from the shared helper.
func TestTheUpdatedColumnIsRelative(t *testing.T) {
	m := resultsModel(t, MultiRegistryTagsLoadedMsg{
		RegistryURL: "registry.example.com", Alias: "prod", Repo: "api", Tags: []string{"v1"},
	})
	b := m.registryBrowser

	if got := b.tagTable.Table().Rows()[0][2]; got != "-" {
		t.Errorf("Updated = %q, want a dash before the metadata lands", got)
	}

	m = feed(t, m, MultiRegistryTagsMetaMsg{
		RegistryURL: "registry.example.com", Repo: "api",
		Meta: map[string]time.Time{"v1": time.Now().Add(-2 * time.Hour)},
	})
	if got := b.tagTable.Table().Rows()[0][2]; !strings.Contains(got, "hr ago") {
		t.Errorf("Updated = %q, want a relative time", got)
	}
}

// A registry whose alias never made it onto the tag still has to be named.
func TestATagWithNoAliasIsLabelledByItsRegistry(t *testing.T) {
	m := resultsModel(t, MultiRegistryTagsLoadedMsg{
		RegistryURL: "registry.example.com", Repo: "api", Tags: []string{"v1"},
	})

	if got := m.registryBrowser.tagTable.Table().Rows()[0][0]; got != "registry.example.com" {
		t.Errorf("the Registry cell is %q, want the URL when there is no alias", got)
	}
}

// A search that matched nothing has to say so, rather than showing an empty
// table that looks like it is still loading.
func TestAnEmptyResultSaysSo(t *testing.T) {
	m := browsingModel(t)
	b := m.registryBrowser
	b.state = browserStateTags

	if !strings.Contains(b.View(), "No tags found") {
		t.Error("an empty result screen does not say it is empty")
	}

	b.pendingSearches = 1
	if !strings.Contains(b.View(), "Searching registries") {
		t.Error("a search still in flight does not say so")
	}
}

// The bar is hidden entirely when nothing is filtered, and shows the active
// registry token when one is.
func TestTheFilterBarAppearsOnlyWhenSomethingIsFiltered(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser

	if b.FilterIsVisible() {
		t.Error("the filter bar is shown with no filter active")
	}

	m = feed(t, m, testutil.Key("r"))
	if !b.FilterIsVisible() {
		t.Fatal("the filter bar is hidden with a registry filter active")
	}
	if !strings.Contains(b.FilterBarView(80), "[prod]") {
		t.Errorf("the filter bar does not name the active registry: %q", b.FilterBarView(80))
	}
}

func TestTheFilterBarShowsTheQuery(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser

	m = feed(t, m, testutil.Key("/"))
	if !strings.Contains(b.FilterBarView(80), "/") {
		t.Error("the filter bar does not show its prompt while editing")
	}

	m = typeInto(t, m, "v1")
	feed(t, m, testutil.Key("enter"))
	if !strings.Contains(b.FilterBarView(80), "v1") {
		t.Error("the filter bar does not show the confirmed query")
	}
}

// The form is the browser's own viewport content, and a group has to read as a
// group rather than as four unrelated registries.
func TestTheFormGroupsMembersUnderTheirParent(t *testing.T) {
	view := groupedModel(t).registryBrowser.View()

	if !strings.Contains(view, "prod") {
		t.Error("the group is not named above its members")
	}
	for _, member := range []string{"docker-hosted", "dhi"} {
		if !strings.Contains(view, member) {
			t.Errorf("member %q is not listed", member)
		}
	}
	if !strings.Contains(view, "Browse Tags") {
		t.Error("the submit button is missing")
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// tagOrder returns the tag column of the table as displayed.
func tagOrder(b *RegistryBrowser) []string {
	rows := b.tagTable.Table().Rows()
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row[1])
	}
	return out
}

// rowFor returns the rendered row for an image reference.
func rowFor(t *testing.T, b *RegistryBrowser, imageName string) []string {
	t.Helper()
	for i, row := range b.tagTable.Visible() {
		tag := row.tag
		if multiImageName(tag.RegistryURL, tag.Repo, tag.Tag) == imageName {
			return b.tagTable.Table().Rows()[i]
		}
	}
	t.Fatalf("no row for %q", imageName)
	return nil
}

// ── Group checkboxes (§3.8 step 6) ───────────────────────────────────────────

// A group is a row of its own, and its checkbox covers its members: eight
// proxies must not be eight keystrokes.
func TestTheGroupCheckboxCoversItsMembers(t *testing.T) {
	m := groupedModel(t)
	b := m.registryBrowser

	if got := b.groupState("prod"); got != theme.CheckAll {
		t.Fatalf("groupState = %v on open, want everything checked", got)
	}

	// The group header is the first picker row, so one down from the repo field.
	m = feed(t, m, testutil.Key("down"), testutil.Key(" "))

	if got := b.groupState("prod"); got != theme.CheckNone {
		t.Errorf("groupState = %v after toggling the group, want none", got)
	}
	for _, e := range b.entries {
		if e.ParentSlug == "prod" && b.selected(e) {
			t.Errorf("member %q stayed checked", e.Alias)
		}
	}

	feed(t, m, testutil.Key(" "))
	if got := b.groupState("prod"); got != theme.CheckAll {
		t.Errorf("groupState = %v after toggling back, want everything", got)
	}
}

// Half a group selected is not the same statement as none, and rendering them
// alike is how a user unchecks something they did not mean to.
func TestAHalfSelectedGroupSaysSo(t *testing.T) {
	b := groupedModel(t).registryBrowser

	for _, e := range b.entries {
		if e.ParentSlug == "prod" {
			b.selectedRegs[e.key] = false
			break
		}
	}

	if got := b.groupState("prod"); got != theme.CheckSome {
		t.Errorf("groupState = %v with one member unchecked, want the third state", got)
	}
	if got := theme.RenderCheckboxTri(theme.CheckSome, "prod", false); strings.Contains(got, theme.IconCheckbox) {
		t.Error("a partial group renders as an empty box, which reads as none")
	}
}

// A partial selection resolves upwards: the user is more likely completing it
// than discarding it.
func TestTogglingAPartialGroupCompletesIt(t *testing.T) {
	b := groupedModel(t).registryBrowser
	b.selectedRegs[b.entries[0].key] = false

	b.toggleGroup("prod")

	if got := b.groupState("prod"); got != theme.CheckAll {
		t.Errorf("groupState = %v, want the selection completed rather than cleared", got)
	}
}

// A registry that belongs to no group has no header of its own to walk past.
func TestAStandaloneRegistryIsItsOwnRow(t *testing.T) {
	b := groupedModel(t).registryBrowser

	var headers int
	for _, row := range b.rows {
		if row.groupSlug != "" {
			headers++
		}
	}
	if headers != 1 {
		t.Errorf("%d group headers, want only the one group", headers)
	}
	if len(b.rows) != len(b.entries)+1 {
		t.Errorf("rows = %d for %d entries, want one extra for the header", len(b.rows), len(b.entries))
	}
}

// ── The result filter's group level (§3.8 step 6) ────────────────────────────

// `r` gains a group stop: narrowing to everything from one Nexus is one
// keystroke rather than one per proxy.
func TestTheResultFilterHasAGroupLevel(t *testing.T) {
	m := groupedModel(t)
	m = typeInto(t, m, "api")
	m = feed(t, m, testutil.Key("enter"))
	m = feed(t, m,
		MultiRegistryTagsLoadedMsg{RegistryURL: "registry.example.com/repository/docker-hosted", Alias: "prod/docker-hosted", Repo: "api", Tags: []string{"v1"}},
		MultiRegistryTagsLoadedMsg{RegistryURL: "registry.example.com/repository/dhi-proxy", Alias: "prod/dhi", Repo: "api", Tags: []string{"v2"}},
		MultiRegistryTagsLoadedMsg{RegistryURL: "docker.io", Alias: "hub", Repo: "api", Tags: []string{"latest"}},
	)
	b := m.registryBrowser

	m = feed(t, m, testutil.Key("r"))
	if b.registryFilter.groupSlug != "prod" {
		t.Fatalf("the first stop is %+v, want the group", b.registryFilter)
	}
	if got := len(b.tagTable.Visible()); got != 2 {
		t.Errorf("%d rows under the group filter, want both its members'", got)
	}

	// Then the individual registries, then back to everything.
	seen := map[string]bool{}
	for range len(b.entries) {
		m = feed(t, m, testutil.Key("r"))
		seen[b.registryFilter.url] = true
	}
	for _, e := range b.entries {
		if !seen[e.URL] {
			t.Errorf("the cycle never stopped on %s", e.URL)
		}
	}

	m = feed(t, m, testutil.Key("r"))
	if !b.registryFilter.isEmpty() {
		t.Errorf("registryFilter = %+v, want it back to everything", b.registryFilter)
	}
}

// ── The tag table after §3.21 ────────────────────────────────────────────────

// Rule 116: the copy written out here floored the flexible Tag column at 8 and
// then handed the whole shortfall to the last severity column, which goes
// negative on a narrow terminal. The solver shares the shortfall instead.
func TestTagColumnsHoldTheWidthInvariant(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m := feed(t, resultsModel(t), tea.WindowSizeMsg{Width: width, Height: 40})
		b := m.registryBrowser

		total := 0
		cols := b.tagTable.Table().Columns()
		for i, col := range cols {
			total += col.Width
			if col.Width < 0 {
				t.Errorf("at width %d column %d is %d cells wide", width, i, col.Width)
			}
		}
		// The browser is handed the viewport content width, so what is left to
		// share is that width less the per-cell padding.
		if want := width - 2 - len(cols)*2; total != want {
			t.Errorf("at width %d the columns sum to %d, want %d", width, total, want)
		}
	}
}

// The cursor is resolved through the table rather than by replaying the filter
// and the sort. `p`, `ctrl+s` and `enter` all act on what it returns, so the two
// orderings drifting apart is a pull of the wrong image.
func TestThePullActsOnTheRowUnderTheCursorAfterASort(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser

	// Descending by tag, then down one row: neither the arrival order nor the
	// ascending one puts the same tag there.
	m = feed(t, m, testutil.Key("."), testutil.Key("down"))

	row, ok := b.tagTable.Selected()
	if !ok {
		t.Fatal("no row is selected")
	}
	want := multiImageName(row.tag.RegistryURL, row.tag.Repo, row.tag.Tag)
	if got := b.selectedImageName(); got != want {
		t.Errorf("selectedImageName() = %q, want the highlighted row %q", got, want)
	}
}
