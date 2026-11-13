package ociresources

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-302 (IS-04 c.1, c.2, c.3, c.4, c.6, c.7): how the level 1 table
// reports progress while registries answer one after the other.
// c.1, c.2, c.4 were implemented before this file: these tests lock them.

// answerImages feeds one registry's answer with explicit images.
func answerImages(t *testing.T, m Model, entryKey string, imgs ...imagesearch.Image) Model {
	t.Helper()
	return feed(t, m, ImageSearchResultMsg{Gen: m.registryBrowser.gen, EntryKey: entryKey, Images: imgs})
}

// c.7: once the search is over, the footer describes the selected row with its
// full reference and the registry it came from.
func TestLevelOneFooterDescribesTheSelectedRow(t *testing.T) {
	m := lvl1SearchingModel(t)
	m = answerImages(t, m, "hub", imagesearch.Image{Name: "nginx", Repo: "library/nginx"})
	m = answerImages(t, m, "prod")
	if _, ok := m.registryBrowser.LoadingLabel(); ok {
		t.Fatal("the search is still running")
	}
	row, ok := m.registryBrowser.searchTable.Selected()
	if !ok || row.img.Repo != "library/nginx" {
		t.Fatalf("selected = %+v, %v, want the hub row", row, ok)
	}
	foot := lvl1Footer(m)
	if !strings.Contains(foot, "Image docker.io/library/nginx on hub") {
		t.Errorf("the footer lacks the exact sentence: %q", foot)
	}
	if strings.Contains(foot, "Searching") {
		t.Errorf("the footer still says the search runs: %q", foot)
	}
}

// c.7, priority: while a registry has not answered, the footer says so, not the row.
func TestLevelOneFooterSearchingWinsOverTheRowDescription(t *testing.T) {
	m := lvl1SearchingModel(t)
	m = answerImages(t, m, "hub", imagesearch.Image{Name: "nginx", Repo: "library/nginx"})
	foot := lvl1Footer(m)
	if !strings.Contains(foot, "Searching registries...") {
		t.Errorf("the footer does not say the search runs: %q", foot)
	}
	if strings.Contains(foot, "docker.io/library/nginx") {
		t.Errorf("the footer describes the row while the search runs: %q", foot)
	}
}

// c.1: rows already received sit in the table while the search runs, and the
// footer carries the shared activity dot, never a braille spinner.
func TestLevelOneSearchingKeepsTheTableAndTheActivityDot(t *testing.T) {
	m := lvl1SearchingModel(t)
	m = lvl1Answer(t, m, "prod", "nginx")
	if !m.WantsBlink() {
		t.Error("WantsBlink is false while one registry is pending")
	}
	if st := m.formStatus(); st.Text != "Searching registries..." || !st.Activity {
		t.Errorf("formStatus = %+v, want the searching text with the activity dot", st)
	}
	body := ansi.Strip(m.registryBrowser.View())
	if body != ansi.Strip(m.registryBrowser.searchTable.View()) {
		t.Error("the body is not the table's")
	}
	if !strings.Contains(body, "Name") || !strings.Contains(body, "nginx") {
		t.Errorf("the body lacks the header or the received row: %q", body)
	}
	if strings.Contains(body, "Searching") {
		t.Errorf("the body carries the loading message: %q", body)
	}
	foot := lvl1Footer(m)
	for _, r := range "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏" {
		if strings.ContainsRune(foot, r) {
			t.Errorf("the footer carries a braille spinner frame %q", r)
		}
	}
}

// c.2: the columns are measured again on every arrival, and the line spans the
// terminal width minus the frame (Rule 116).
func TestLevelOneColumnsAreMeasuredAgainOnEveryArrival(t *testing.T) {
	const width, height = 120, 30
	m := lvl1SearchingModel(t)
	m = feed(t, m, testutil.Resize(width, height))
	m = lvl1Answer(t, m, "hub", "nginx", "redis")
	long := strings.Repeat("n", 40)
	m = lvl1Answer(t, m, "prod", long)
	body := ansi.Strip(m.registryBrowser.View())
	if !strings.Contains(body, long) || strings.Contains(body, "…") {
		t.Errorf("the 40-character name is truncated: %q", body)
	}
	if got, want := m.registryBrowser.searchTable.RenderedWidth(), width-2; got != want {
		t.Errorf("the line spans %d, want %d", got, want)
	}
}

// c.3: every registry answered and none had a row: a header-only table, no dot.
func TestLevelOneEveryRegistryEmptyLeavesAHeaderOnlyTable(t *testing.T) {
	m := lvl1SearchingModel(t)
	m = lvl1Answer(t, m, "prod")
	m = lvl1Answer(t, m, "hub")
	if _, ok := m.registryBrowser.LoadingLabel(); ok {
		t.Error("LoadingLabel is true once every registry answered")
	}
	if st := m.formStatus(); st.Activity {
		t.Errorf("formStatus = %+v, want no activity dot", st)
	}
	if m.WantsBlink() {
		t.Error("WantsBlink is true with nothing in flight")
	}
	body := ansi.Strip(m.registryBrowser.View())
	if !strings.Contains(body, "Name") || !strings.Contains(body, "Registry") {
		t.Errorf("the body lacks the header: %q", body)
	}
	if got := len(m.registryBrowser.searchTable.Visible()); got != 0 {
		t.Errorf("%d rows, want none", got)
	}
}

// c.4: a late answer of an old search is ignored and does not disturb the
// indicator of the new one.
func TestLevelOneLateAnswerOfAnOldSearchLeavesTheIndicatorAlone(t *testing.T) {
	m := lvl1SearchingModel(t)
	oldGen := m.registryBrowser.gen
	m.registryBrowser.startImageSearch("redis", m.registryBrowser.entries)
	if m.registryBrowser.gen == oldGen {
		t.Fatal("the new search kept the old generation")
	}
	pending := m.registryBrowser.pendingSearches
	m = feed(t, m, ImageSearchResultMsg{
		Gen: oldGen, EntryKey: "prod",
		Images: []imagesearch.Image{{EntryKey: "prod", Name: "nginx", Repo: "nginx"}},
	})
	if got := m.registryBrowser.pendingSearches; got != pending {
		t.Errorf("pendingSearches = %d, want %d: a stale answer must not count", got, pending)
	}
	if _, ok := m.registryBrowser.LoadingLabel(); !ok {
		t.Error("the new search stopped looking in flight")
	}
	if !m.WantsBlink() {
		t.Error("the dot stopped blinking")
	}
	if got := len(m.registryBrowser.searchTable.Visible()); got != 0 {
		t.Errorf("%d rows, want none from a stale answer", got)
	}
}

// c.6: the registry search is not a job. Its batch emits only
// ImageSearchResultMsg, and the router's jobs stay empty.
func TestLevelOneSearchIsNotAJob(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m, cmd := submitRaw(t, browsingModel(t), "nginx")
	if cmd == nil {
		t.Fatal("the search started nothing")
	}
	msgs := testutil.Msgs(cmd)
	got := 0
	for _, msg := range msgs {
		switch msg.(type) {
		case ImageSearchResultMsg:
			got++
		case jobs.ChangedMsg, jobs.FrameMsg:
			t.Errorf("the search emitted a jobs message: %T", msg)
		}
	}
	if got != 2 {
		t.Errorf("%d ImageSearchResultMsg, want 2", got)
	}
	if len(m.jobs) != 0 {
		t.Errorf("m.jobs holds %d runs, want none", len(m.jobs))
	}
}

// An entry the registry list no longer knows leaves the label empty: the
// sentence stops at the reference, with no dangling " on".
func TestLevelOneFooterOmitsTheTailWithoutALabel(t *testing.T) {
	m := lvl1SearchingModel(t)
	m = answerImages(t, m, "hub", imagesearch.Image{Name: "nginx", Repo: "library/nginx"})
	m = answerImages(t, m, "prod")
	m.registryBrowser.images = []imagesearch.Image{{EntryKey: "ghost", Name: "alpine", Repo: "alpine"}}
	m.registryBrowser.rebuildSearchTable()
	if row, ok := m.registryBrowser.searchTable.Selected(); !ok || row.label != "" {
		t.Fatalf("selected = %+v, %v, want a row without a label", row, ok)
	}
	foot := lvl1Footer(m)
	if !strings.Contains(foot, "Image alpine") || strings.Contains(foot, "Image alpine on") {
		t.Errorf("the footer = %q, want the reference without a tail", foot)
	}
}
