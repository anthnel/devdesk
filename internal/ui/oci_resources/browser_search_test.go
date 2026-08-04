package ociresources

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/registrymgr"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The search form and the results table, driven through the model — the router
// is what forwards keys to the browser, and the model is what wires the scan
// cache in. registry_browser_test.go covers opening, group resolution and the
// arrival of results; this file covers what the user does once it is open.

// groupedModel returns a browser whose first registry resolved into a group of
// two members, so the entry list mixes members and a plain registry.
func groupedModel(t *testing.T) Model {
	t.Helper()
	m := feed(t, loadedModel(t), testutil.Key("b"))
	return feed(t, m,
		RegistryGroupDetectedMsg{
			RegistryURL: "registry.example.com",
			Members: []registrymgr.GroupMember{
				{Alias: "hosted", URL: "registry.example.com/repository/docker-hosted"},
				{Alias: "dhi", URL: "registry.example.com/repository/dhi-proxy"},
			},
		},
		RegistryGroupDetectedMsg{RegistryURL: "docker.io"},
	)
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
		if !b.selectedRegs[entry.URL] {
			t.Errorf("%s opened unchecked", entry.URL)
		}
	}
}

// Rule 135: space is the only key that toggles a checkbox.
func TestSpaceTogglesTheFocusedRegistry(t *testing.T) {
	m := browsingModel(t)
	b := m.registryBrowser
	first := b.entries[0].URL

	m = feed(t, m, testutil.Key("down"), testutil.Key(" "))
	if b.selectedRegs[first] {
		t.Error("space did not uncheck the focused registry")
	}
	feed(t, m, testutil.Key(" "))
	if !b.selectedRegs[first] {
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
		if !b.selectedRegs[entry.URL] {
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
		b.selectedRegs[entry.URL] = false
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
	b.selectedRegs[b.entries[0].URL] = false

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
	b.registryFilter = "docker.io"
	b.tagSortCol = tagSortByUpdated
	b.tagSortDesc = true

	m = feed(t, m, testutil.Key("esc")) // back to the form
	m = typeInto(t, m, "redis")
	m = feed(t, m, testutil.Key("enter"))

	if len(b.tags) != 0 {
		t.Errorf("tags = %v, want the previous results dropped", b.tags)
	}
	if b.filterInput.Value() != "" || b.registryFilter != "" {
		t.Errorf("filters survived: %q / %q", b.filterInput.Value(), b.registryFilter)
	}
	if b.tagSortCol != tagSortByName || b.tagSortDesc {
		t.Error("the sort survived into a new search")
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
		col  tagSortField
		desc bool
	}{
		{tagSortByName, true},
		{tagSortByUpdated, false},
		{tagSortByUpdated, true},
		{tagSortByName, false},
	}
	for i, step := range want {
		m = feed(t, m, testutil.Key("."))
		if b.tagSortCol != step.col || b.tagSortDesc != step.desc {
			t.Fatalf("after %d presses: col %v desc %v, want %v/%v", i+1, b.tagSortCol, b.tagSortDesc, step.col, step.desc)
		}
	}
}

func TestTheSortedColumnCarriesTheArrow(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser

	if !strings.Contains(b.tagTable.Columns()[1].Title, "▲") {
		t.Errorf("the Tag header is %q, want an ascending arrow", b.tagTable.Columns()[1].Title)
	}
	m = feed(t, m, testutil.Key("."), testutil.Key("."))
	if !strings.Contains(b.tagTable.Columns()[2].Title, "▲") {
		t.Errorf("the Updated header is %q, want the arrow to have moved", b.tagTable.Columns()[2].Title)
	}
	if strings.Contains(b.tagTable.Columns()[1].Title, "▲") {
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

	if got := len(b.filteredSortedMultiTags()); got != 1 {
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
	if b.registryFilter != "registry.example.com" {
		t.Fatalf("registryFilter = %q, want the first registry", b.registryFilter)
	}
	if got := len(b.filteredSortedMultiTags()); got != 2 {
		t.Errorf("%d rows shown, want only that registry's", got)
	}

	m = feed(t, m, testutil.Key("r"))
	if b.registryFilter != "docker.io" {
		t.Fatalf("registryFilter = %q, want the second registry", b.registryFilter)
	}

	m = feed(t, m, testutil.Key("r"))
	if b.registryFilter != "" {
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

	if m.registryBrowser.registryFilter != "" {
		t.Errorf("registryFilter = %q, want no filter with one registry", m.registryBrowser.registryFilter)
	}
}

// D14, asserted inverted: the label is resolved against the configured
// registries only, so a group member — which is discovered, not configured —
// falls through to the raw synthesised URL while every other row shows a short
// alias. §3.8 fixes this by persisting members; when it does, this test is what
// fails.
func TestAGroupMembersFilterLabelIsStillARawURL(t *testing.T) {
	m := groupedModel(t)
	b := m.registryBrowser
	memberURL := "registry.example.com/repository/dhi-proxy"
	b.registryFilter = memberURL

	if got := b.registryFilterLabel(); got != memberURL {
		t.Errorf("registryFilterLabel() = %q — D14 appears fixed; see the comment above", got)
	}

	// A configured registry resolves properly, which is the contrast.
	b.registryFilter = "registry.example.com"
	if got := b.registryFilterLabel(); got != "prod" {
		t.Errorf("registryFilterLabel() = %q, want the configured alias", got)
	}
}

// A registry configured without an alias has nothing to shorten to, so the URL
// is the label.
func TestARegistryWithNoAliasIsLabelledByItsURL(t *testing.T) {
	m := browsingModel(t)
	b := m.registryBrowser
	b.registries[0].Alias = ""
	b.registryFilter = "registry.example.com"

	if got := b.registryFilterLabel(); got != "registry.example.com" {
		t.Errorf("registryFilterLabel() = %q, want the URL", got)
	}
}

// ── Acting on a tag ──────────────────────────────────────────────────────────

func TestPullingATagShowsTheOperation(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser

	m = feed(t, m, testutil.Key("p"))

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
	m = feed(t, m, testutil.Key("p"))

	feed(t, m, testutil.Key("esc"), testutil.Key("p"), testutil.Key("."))

	if b.state != browserStateStatus {
		t.Errorf("state = %d, want the pull still showing", b.state)
	}
}

func TestAFinishedPullReturnsToTheTags(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser
	m = feed(t, m, testutil.Key("p"))

	b.SetOperationSuccess()

	if b.state != browserStateTags || b.OperationImageName() != "" {
		t.Errorf("state = %d, image = %q, want the results screen back", b.state, b.OperationImageName())
	}
}

func TestAFailedPullAlsoReturnsToTheTags(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser
	m = feed(t, m, testutil.Key("p"))

	b.SetOperationError("manifest unknown")

	if b.state != browserStateTags {
		t.Errorf("state = %d, want the results screen back so the user can retry", b.state)
	}
}

// ctrl+s scans the tag where it is, without pulling it first — that is the
// point of the direct scan.
func TestCtrlSAsksForADirectScanOfTheSelectedTag(t *testing.T) {
	m := resultsModel(t)

	_, cmd := step(t, m, testutil.Key("ctrl+s"))

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

	for _, name := range []string{"p", "ctrl+s", "enter"} {
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

	for _, row := range b.tagTable.Rows() {
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

	if got := b.tagTable.Rows()[0][2]; got != "-" {
		t.Errorf("Updated = %q, want a dash before the metadata lands", got)
	}

	m = feed(t, m, MultiRegistryTagsMetaMsg{
		RegistryURL: "registry.example.com", Repo: "api",
		Meta: map[string]time.Time{"v1": time.Now().Add(-2 * time.Hour)},
	})
	if got := b.tagTable.Rows()[0][2]; !strings.Contains(got, "hr ago") {
		t.Errorf("Updated = %q, want a relative time", got)
	}
}

// A registry whose alias never made it onto the tag still has to be named.
func TestATagWithNoAliasIsLabelledByItsRegistry(t *testing.T) {
	m := resultsModel(t, MultiRegistryTagsLoadedMsg{
		RegistryURL: "registry.example.com", Repo: "api", Tags: []string{"v1"},
	})

	if got := m.registryBrowser.tagTable.Rows()[0][0]; got != "registry.example.com" {
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

	if !strings.Contains(view, "prod:") {
		t.Error("the group is not named above its members")
	}
	for _, member := range []string{"hosted", "dhi"} {
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
	rows := b.tagTable.Rows()
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row[1])
	}
	return out
}

// rowFor returns the rendered row for an image reference.
func rowFor(t *testing.T, b *RegistryBrowser, imageName string) []string {
	t.Helper()
	for i, tag := range b.filteredSortedMultiTags() {
		if multiImageName(tag.RegistryURL, tag.Repo, tag.Tag) == imageName {
			return b.tagTable.Rows()[i]
		}
	}
	t.Fatalf("no row for %q", imageName)
	return nil
}
