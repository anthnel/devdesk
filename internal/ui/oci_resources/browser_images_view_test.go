package ociresources

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Level 1 of the image search on screen (DEVDESK-395, IS-01 c.7 and c.8): the
// header count, the shortcuts, the help, the filter bar, esc, the loading
// state in the footer (Rules 114, 125, 128, 130, 139) and enter doing nothing.

// searchingModel opens the browser at level 1 with both registries still
// pending: the lot is started, no answer has come back yet.
func lvl1SearchingModel(t *testing.T) Model {
	t.Helper()
	withImageSearcher(t, &fakeSearcher{})
	m, cmd := submitRaw(t, browsingModel(t), "nginx")
	if cmd == nil {
		t.Fatal("the search started nothing")
	}
	if m.registryBrowser.state != browserStateImages {
		t.Fatalf("state = %d, want browserStateImages", m.registryBrowser.state)
	}
	if m.registryBrowser.pendingSearches != 2 {
		t.Fatalf("pendingSearches = %d, want 2", m.registryBrowser.pendingSearches)
	}
	return m
}

// answer feeds one registry's answer, stamped with the open lot.
func lvl1Answer(t *testing.T, m Model, entryKey string, names ...string) Model {
	t.Helper()
	images := make([]imagesearch.Image, len(names))
	for i, n := range names {
		images[i] = imagesearch.Image{EntryKey: entryKey, Name: n, Repo: n}
	}
	return feed(t, m, ImageSearchResultMsg{Gen: m.registryBrowser.gen, EntryKey: entryKey, Images: images})
}

// imagesModel is level 1 settled with three rows: two from "prod", one from "hub".
func lvl1ImagesModel(t *testing.T) Model {
	t.Helper()
	m := lvl1SearchingModel(t)
	m = lvl1Answer(t, m, "prod", "nginx", "nginx-unprivileged")
	m = lvl1Answer(t, m, "hub", "redis")
	if got := len(m.registryBrowser.searchTable.Visible()); got != 3 {
		t.Fatalf("fixture holds %d rows, want 3", got)
	}
	if m.registryBrowser.pendingSearches != 0 {
		t.Fatalf("pendingSearches = %d, want 0", m.registryBrowser.pendingSearches)
	}
	return m
}

func lvl1Header(t *testing.T, m Model, key string) string {
	t.Helper()
	for _, info := range m.GetHeaderInfo("") {
		if info.Key == key {
			return info.Value
		}
	}
	return ""
}

func lvl1Names(m Model) []string {
	var out []string
	for _, r := range m.registryBrowser.searchTable.Visible() {
		out = append(out, r.img.Name)
	}
	return out
}

func lvl1Footer(m Model) string {
	return ansi.Strip(m.RenderFooter(120))
}

// c.7: the header counts the visible rows.
func TestLevelOneHeaderCountsTheVisibleImages(t *testing.T) {
	m := lvl1SearchingModel(t)
	if got := lvl1Header(t, m, "Images"); got != "0" {
		t.Errorf("Images before any answer = %q, want 0", got)
	}
	m = lvl1ImagesModel(t)
	if got := lvl1Header(t, m, "Images"); got != "3" {
		t.Errorf("Images after the answers = %q, want 3", got)
	}
	if got := lvl1Header(t, m, "Disk Usage"); got != "" {
		t.Errorf("the local Images tab's Disk Usage leaked into level 1: %q", got)
	}

	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("redis")...)
	m = feed(t, m, testutil.Key("enter"))
	if got := lvl1Header(t, m, "Images"); got != "1" {
		t.Errorf("Images after a filter = %q, want 1", got)
	}
}

// c.7: the filter bar of level 1 filters on the name.
func TestLevelOneFilterBarFiltersOnTheName(t *testing.T) {
	m := lvl1ImagesModel(t)
	m = feed(t, m, testutil.Key("/"))
	if !m.registryBrowser.searchTable.InEditMode() {
		t.Fatal("/ did not open the filter bar")
	}
	if !m.registryBrowser.FilterIsVisible() {
		t.Error("the filter bar is not visible after /")
	}
	if got, want := m.GetFooterHeight(), 4; got != want {
		t.Errorf("GetFooterHeight with the bar = %d, want %d", got, want)
	}
	m = feed(t, m, testutil.Type("nginx")...)
	if got := lvl1Names(m); !slices.Equal(got, []string{"nginx", "nginx-unprivileged"}) {
		t.Errorf("rows after typing nginx = %v", got)
	}
	// The registry label is not searched: only the name is.
	m = feed(t, m, testutil.Key("esc"))
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("hub")...)
	if got := lvl1Names(m); len(got) != 0 {
		t.Errorf("rows after typing a registry label = %v, want none (the filter is on the name)", got)
	}
	if bar := ansi.Strip(m.registryBrowser.FilterBarView(80)); !strings.Contains(bar, "hub") {
		t.Errorf("FilterBarView does not show the query: %q", bar)
	}
}

// c.7: ":" while the bar has the keyboard is a character.
func TestLevelOneColonGoesIntoTheFilterField(t *testing.T) {
	m := lvl1ImagesModel(t)
	if m.registryBrowser.InEditMode() {
		t.Error("InEditMode is true at rest on level 1")
	}
	m = feed(t, m, testutil.Key("/"))
	if !m.registryBrowser.InEditMode() {
		t.Fatal("InEditMode is false while the filter bar has the keyboard")
	}
	m = feed(t, m, testutil.Type(":")...)
	if got := m.registryBrowser.searchTable.FilterBar().SearchQuery(); got != ":" {
		t.Errorf("query = %q, want the colon typed into the field", got)
	}
}

// c.7: esc closes the open bar first, then a confirmed query, then goes back.
func TestLevelOneEscClosesTheOpenBarAndStaysOnLevelOne(t *testing.T) {
	m := lvl1ImagesModel(t)
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("redis")...)
	m = feed(t, m, testutil.Key("esc"))
	if m.registryBrowser.state != browserStateImages {
		t.Fatalf("esc in the bar left level 1 (state %d)", m.registryBrowser.state)
	}
	if m.registryBrowser.InEditMode() || m.registryBrowser.FilterIsVisible() {
		t.Error("the bar is still open after esc")
	}
	if got := len(lvl1Names(m)); got != 3 {
		t.Errorf("%d rows after closing the bar, want all 3 back", got)
	}
}

func TestLevelOneEscClearsAConfirmedQueryThenGoesBack(t *testing.T) {
	m := lvl1ImagesModel(t)
	m.registryBrowser.repoInput.SetValue("nginx")
	selected := make(map[string]bool)
	for k, v := range m.registryBrowser.selectedRegs {
		selected[k] = v
	}

	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("redis")...)
	m = feed(t, m, testutil.Key("enter"))
	if got := lvl1Names(m); !slices.Equal(got, []string{"redis"}) {
		t.Fatalf("rows after a confirmed query = %v", got)
	}

	m = feed(t, m, testutil.Key("esc"))
	if m.registryBrowser.state != browserStateImages {
		t.Fatalf("the first esc left level 1 (state %d)", m.registryBrowser.state)
	}
	if got := m.registryBrowser.searchTable.FilterBar().SearchQuery(); got != "" {
		t.Errorf("query = %q after esc, want it cleared", got)
	}
	if got := len(lvl1Names(m)); got != 3 {
		t.Errorf("%d rows after clearing the query, want 3", got)
	}

	m = feed(t, m, testutil.Key("esc"))
	if m.registryBrowser.state != browserStateInput {
		t.Fatalf("the second esc did not return to the form (state %d)", m.registryBrowser.state)
	}
	if got := m.registryBrowser.repoInput.Value(); got != "nginx" {
		t.Errorf("the typed name = %q, want it kept", got)
	}
	for k, v := range selected {
		if m.registryBrowser.selectedRegs[k] != v {
			t.Errorf("selection of %q changed", k)
		}
	}
}

// c.8: enter on a level 1 row does nothing at all.
func TestLevelOneEnterDoesNothing(t *testing.T) {
	m := lvl1ImagesModel(t)
	m = feed(t, m, testutil.Key("down"))
	cursor := m.registryBrowser.searchTable.Cursor()
	names := lvl1Names(m)

	next, cmd := step(t, m, testutil.Key("enter"))
	if cmd != nil {
		t.Errorf("enter returned a Cmd: %T", testutil.Msg(cmd))
	}
	if next.registryBrowser == nil || next.registryBrowser.state != browserStateImages {
		t.Fatal("enter left level 1")
	}
	if got := next.registryBrowser.searchTable.Cursor(); got != cursor {
		t.Errorf("cursor = %d, want %d", got, cursor)
	}
	if got := lvl1Names(next); !slices.Equal(got, names) {
		t.Errorf("rows = %v, want %v", got, names)
	}
	if next.footer.IsSet() {
		t.Errorf("enter set a footer message: %q", next.footer.Text())
	}
	if next.confirmModal != nil || next.scanAllModal != nil {
		t.Error("enter opened a modal")
	}
}

// c.7, c.8, Rules 130 and 137: the shortcuts of level 1.
func TestLevelOneShortcuts(t *testing.T) {
	m := lvl1ImagesModel(t)
	sc := m.GetShortcuts()
	for _, key := range []string{"→", "r", "/", ".", "esc"} {
		if !testutil.HasShortcut(sc, key) {
			t.Errorf("level 1 lacks the %q shortcut", key)
		}
		if testutil.HasShortcut(sc, key) && !testutil.ShortcutEnabled(sc, key) {
			t.Errorf("%q is greyed at level 1", key)
		}
	}
	if testutil.HasShortcut(sc, "enter") {
		t.Error("level 1 advertises enter")
	}

	if got, want := testutil.ShortcutKeys(sc), []string{"→", "r", "/", ".", "esc"}; !slices.Equal(got, want) {
		t.Errorf("level 1 keys = %v, want %v (r before /, as on level 2)", got, want)
	}
	if !slices.ContainsFunc(sc, func(s shortcut.Shortcut) bool { return s.Key == "r" && s.Description == "Filter registry" }) {
		t.Error(`level 1 lacks {Key: "r", Description: "Filter registry"}`)
	}
	if !slices.ContainsFunc(sc, func(s shortcut.Shortcut) bool { return s.Key == "." && s.Description == "Sort" }) {
		t.Error(`level 1 lacks {Key: ".", Description: "Sort"}`)
	}

	m = feed(t, m, testutil.Key("/"))
	sc = m.GetShortcuts()
	if !testutil.HasShortcut(sc, "enter/esc") {
		t.Errorf("no enter/esc Close filter while typing: %v", testutil.ShortcutKeys(sc))
	}
}

// Rule 130: the set of keys does not depend on whether there are rows.
func TestLevelOneShortcutKeysDoNotDependOnTheRows(t *testing.T) {
	empty := lvl1SearchingModel(t)
	filled := lvl1ImagesModel(t)
	a := testutil.ShortcutKeys(empty.GetShortcuts())
	b := testutil.ShortcutKeys(filled.GetShortcuts())
	if !slices.Equal(a, b) {
		t.Errorf("keys without rows = %v, with rows = %v", a, b)
	}
}

// Level 2 keeps its enter.
func TestLevelTwoEnterIsStillThere(t *testing.T) {
	m := resultsModel(t)
	var found shortcut.Shortcut
	for _, s := range m.GetShortcuts() {
		if s.Key == "enter" {
			found = s
		}
	}
	if found.Description != "View CVE details" {
		t.Errorf("level 2 enter = %q, want View CVE details", found.Description)
	}
}

// Rule 114: the help lists the keys of level 1 and the activity legend.
func TestLevelOneHelpListsItsKeysAndTheLegend(t *testing.T) {
	c := lvl1ImagesModel(t).GetHelpContent()
	keys := map[string]bool{}
	for _, kb := range c.KeyBindings {
		keys[kb.Key] = true
	}
	for _, want := range []string{"→ (Browser images)", "/ (Browser images)", "r (Browser images)", "esc (Browser images)"} {
		if !keys[want] {
			t.Errorf("help lacks the %q binding", want)
		}
	}
	if !keys[". (Browser images)"] {
		t.Error("help lacks the \". (Browser images)\" binding")
	}
	for _, kb := range c.KeyBindings {
		if kb.Key != ". (Browser images)" {
			continue
		}
		d := strings.ToLower(kb.Description)
		for _, w := range []string{"cycle", "sort", "name", "registry", "stars", "most first", "each way", "default order"} {
			if !strings.Contains(d, w) {
				t.Errorf("help text for . lacks %q: %q", w, kb.Description)
			}
		}
	}
	for _, kb := range c.KeyBindings {
		d := strings.ToLower(kb.Description)
		switch kb.Key {
		case "/ (Browser images)":
			if !strings.Contains(d, "by name or description") {
				t.Errorf("help text for / lacks %q: %q", "by name or description", kb.Description)
			}
		case "r (Browser images)":
			for _, w := range []string{"filter", "registry", "group", "cycl", "then off"} {
				if !strings.Contains(d, w) {
					t.Errorf("help text for r lacks %q: %q", w, kb.Description)
				}
			}
			if strings.Contains(d, "esc") {
				t.Errorf("help text for r mentions esc: %q", kb.Description)
			}
		}
	}
	if keys["enter (Browser images)"] {
		t.Error("help documents an enter at level 1, which does nothing")
	}
	if !lvl1HasSection(c, indicator.Legend().Title) {
		t.Errorf("help lacks the %q section", indicator.Legend().Title)
	}
	if !lvl1HasSection(c, "Registry Browser") {
		t.Error(`help lacks a "Registry Browser" section`)
	}
}

func lvl1HasSection(c help.Content, title string) bool {
	for _, s := range c.Sections {
		if s.Title == title {
			return true
		}
	}
	return false
}

func TestLevelOneTitleCarriesTheTypedName(t *testing.T) {
	title := ansi.Strip(lvl1ImagesModel(t).GetTitle())
	if !strings.Contains(title, "Registry Browser") || !strings.HasSuffix(title, "nginx") {
		t.Errorf("title = %q, want it to end with the typed name", title)
	}
}

// Rules 128 and 139: the search says so in the footer, never in the body.
func TestLevelOneSearchShowsALoadingMessageInTheFooter(t *testing.T) {
	m := lvl1SearchingModel(t)
	if label, ok := m.registryBrowser.LoadingLabel(); !ok || label != "Searching registries..." {
		t.Errorf("LoadingLabel = %q, %v, want the searching label", label, ok)
	}
	st := m.formStatus()
	if st.Text != "Searching registries..." || !st.Activity {
		t.Errorf("formStatus = %+v, want the searching text with the activity dot", st)
	}
	if !strings.Contains(lvl1Footer(m), "Searching registries...") {
		t.Errorf("the footer does not say the search runs: %q", lvl1Footer(m))
	}

	m = lvl1Answer(t, m, "prod", "nginx")
	if _, ok := m.registryBrowser.LoadingLabel(); !ok {
		t.Error("the search is over while one registry has not answered")
	}
	m = lvl1Answer(t, m, "hub", "redis")
	if _, ok := m.registryBrowser.LoadingLabel(); ok {
		t.Error("LoadingLabel still true once every registry answered")
	}
	if st := m.formStatus(); st.Text != "" || st.Activity {
		t.Errorf("formStatus after the search = %+v, want empty", st)
	}
}

func TestLevelOneBodyStaysATableWhileSearchingAndWhenEmpty(t *testing.T) {
	for name, m := range map[string]Model{"searching": lvl1SearchingModel(t), "settled": lvl1ImagesModel(t)} {
		body := ansi.Strip(m.registryBrowser.View())
		if body != ansi.Strip(m.registryBrowser.searchTable.View()) {
			t.Errorf("%s: the body is not the table's", name)
		}
		for _, col := range []string{"Name", "Registry"} {
			if !strings.Contains(body, col) {
				t.Errorf("%s: the body lacks the %q header", name, col)
			}
		}
		if strings.Contains(body, "Searching") || strings.Contains(body, "No images") {
			t.Errorf("%s: the body carries a message: %q", name, body)
		}
	}

	// A filter that matches nothing leaves the header and no row.
	m := lvl1ImagesModel(t)
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("zzz")...)
	body := ansi.Strip(m.registryBrowser.View())
	if !strings.Contains(body, "Name") || strings.Contains(body, "nginx") {
		t.Errorf("an empty filter did not leave a header-only table: %q", body)
	}
}

// The activity dot blinks while the search runs and not at rest.
func TestLevelOneBlinksOnlyWhileSearching(t *testing.T) {
	m := lvl1SearchingModel(t)
	if !m.WantsBlink() {
		t.Error("WantsBlink is false while the search runs")
	}
	m = lvl1ImagesModel(t)
	if m.WantsBlink() {
		t.Error("WantsBlink is true at rest on level 1")
	}
}

func TestLevelOneTakesTheBlinkPhase(t *testing.T) {
	m := lvl1ImagesModel(t)
	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if m.registryBrowser.searchTable.BlinkPhase() {
		t.Error("the level 1 table did not take the off phase")
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if !m.registryBrowser.searchTable.BlinkPhase() {
		t.Error("the level 1 table did not take the on phase")
	}
}
