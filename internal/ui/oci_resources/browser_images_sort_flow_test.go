package ociresources

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-309 step 2: the `.` key on level 1, the reset on a new search, and
// the cursor following the selected row. Keys only; no production symbol.

func arriveStars(t *testing.T, b *RegistryBrowser, key string, stars map[string]int) {
	t.Helper()
	var imgs []imagesearch.Image
	for repo, n := range stars {
		imgs = append(imgs, imagesearch.Image{EntryKey: key, Name: repo, Repo: repo, Stars: n, HasStars: true})
	}
	if !b.AddImageResults(ImageSearchResultMsg{Gen: b.gen, EntryKey: key, Images: imgs}) {
		t.Fatal("the result was refused")
	}
}

func pressDot(t *testing.T, b *RegistryBrowser) {
	t.Helper()
	if _, cmd := b.handleImagesKeyMsg(testutil.Key(".")); cmd != nil {
		cmd()
	}
}

func searchNames(b *RegistryBrowser) []string {
	var out []string
	for _, r := range b.searchTable.Visible() {
		out = append(out, r.img.Name)
	}
	return out
}

func TestDotSortsAndTheCursorStaysOnTheSameRow(t *testing.T) {
	b, keyA, keyB := cursorBrowser(t)
	arriveStars(t, b, keyA, map[string]int{"alpha": 5, "bravo": 50})
	arriveStars(t, b, keyB, map[string]int{"charlie": 20, "delta": 1})
	b.searchTable.SetCursor(slices.Index(searchNames(b), "charlie"))
	assertSelected(t, b, keyB, "charlie")

	pressDot(t, b) // Name asc
	pressDot(t, b) // Name desc
	if got, want := searchNames(b), []string{"delta", "charlie", "bravo", "alpha"}; !slices.Equal(got, want) {
		t.Fatalf("Name desc = %q, want %q", got, want)
	}
	assertSelected(t, b, keyB, "charlie")
	for range 3 { // Registry asc, Registry desc, Stars desc
		pressDot(t, b)
	}
	if got, want := searchNames(b), []string{"bravo", "charlie", "alpha", "delta"}; !slices.Equal(got, want) {
		t.Fatalf("Stars desc = %q, want %q", got, want)
	}
	assertSelected(t, b, keyB, "charlie")
}

func TestDotWorksWhileTheSearchIsStillRunning(t *testing.T) {
	b, keyA, _ := cursorBrowser(t)
	arriveStars(t, b, keyA, map[string]int{"alpha": 5, "bravo": 50})
	if b.pendingSearches == 0 {
		t.Fatal("fixture: the search should still be pending")
	}
	for range 5 {
		pressDot(t, b)
	}
	if col, desc := b.searchTable.SortState(); col != searchColStars || !desc {
		t.Fatalf("SortState = (%d,%v), want Stars descending", col, desc)
	}
}

func TestRowsArrivingUnderAStarsSortKeepTheCursor(t *testing.T) {
	b, keyA, keyB := cursorBrowser(t)
	arriveStars(t, b, keyA, map[string]int{"alpha": 5, "bravo": 50, "charlie": 20})
	for range 5 {
		pressDot(t, b)
	}
	b.searchTable.SetCursor(slices.Index(searchNames(b), "charlie"))
	assertSelected(t, b, keyA, "charlie")

	arriveStars(t, b, keyB, map[string]int{"huge": 9000, "tiny": 0})
	if got, want := searchNames(b), []string{"huge", "bravo", "charlie", "alpha", "tiny"}; !slices.Equal(got, want) {
		t.Fatalf("order = %q, want %q", got, want)
	}
	assertSelected(t, b, keyA, "charlie")
}

func TestDotTypedInTheOpenFilterBarGoesToTheField(t *testing.T) {
	b, keyA, _ := cursorBrowser(t)
	arriveStars(t, b, keyA, map[string]int{"alpha": 5})
	b.handleImagesKeyMsg(testutil.Key("/"))
	if !b.searchTable.InEditMode() {
		t.Fatal("/ did not open the bar")
	}
	b.handleImagesKeyMsg(testutil.Key("."))
	if col, desc := b.searchTable.SortState(); col != -1 || desc {
		t.Errorf("SortState = (%d,%v), want (-1,false): the dot sorted", col, desc)
	}
	if bar := ansi.Strip(b.FilterBarView(80)); !strings.Contains(bar, ".") {
		t.Errorf("the dot did not reach the field: %q", bar)
	}
}

func TestSortSurvivesTagsAndBackThenANewSearchResetsIt(t *testing.T) {
	m := lvl1ImagesModel(t)
	for range 2 {
		m = feed(t, m, testutil.Key("."))
	}
	want, wantDesc := m.registryBrowser.searchTable.SortState()
	if want != searchColName || !wantDesc {
		t.Fatalf("SortState = (%d,%v), want Name descending", want, wantDesc)
	}
	order := lvl1Names(m)

	m = feed(t, m, testutil.Key("right"))
	if m.registryBrowser.state != browserStateTags {
		t.Fatalf("state = %d, want tags", m.registryBrowser.state)
	}
	m = feed(t, m, testutil.Key("left"))
	if m.registryBrowser.state != browserStateImages {
		t.Fatalf("state = %d, want images", m.registryBrowser.state)
	}
	if col, desc := m.registryBrowser.searchTable.SortState(); col != want || desc != wantDesc {
		t.Errorf("sort after tags and back = (%d,%v)", col, desc)
	}
	if got := lvl1Names(m); !slices.Equal(got, order) {
		t.Errorf("order after tags and back = %q, want %q", got, order)
	}

	m = feed(t, m, testutil.Key("left")) // back to the form
	m, _ = submitRaw(t, m, "nginx")
	if col, desc := m.registryBrowser.searchTable.SortState(); col != -1 || desc {
		t.Errorf("SortState after a new search = (%d,%v), want (-1,false) (AC3)", col, desc)
	}
	m = lvl1Answer(t, m, "prod", "nginx", "nginx-unprivileged")
	m = lvl1Answer(t, m, "hub", "redis")
	if got, want := lvl1Names(m), []string{"nginx", "nginx-unprivileged", "redis"}; !slices.Equal(got, want) {
		t.Errorf("default order after a new search = %q, want %q", got, want)
	}
}

func TestANewSearchClearsAConfirmedSlashFilter(t *testing.T) {
	b, keyA, _ := cursorBrowser(t)
	arriveStars(t, b, keyA, map[string]int{"alpha": 5, "bravo": 50})
	b.handleImagesKeyMsg(testutil.Key("/"))
	for _, r := range "bra" {
		b.handleImagesKeyMsg(testutil.Key(string(r)))
	}
	b.handleImagesKeyMsg(testutil.Key("enter"))
	if got := b.searchTable.FilterBar().SearchQuery(); got != "bra" {
		t.Fatalf("fixture: confirmed query = %q", got)
	}
	b.startImageSearch("nginx", b.entries)
	if got := b.searchTable.FilterBar().SearchQuery(); got != "" {
		t.Errorf("confirmed query after a new search = %q, want empty", got)
	}
}

// Stars reserves its title plus the sort arrow: 7 cells. The declaration says
// so itself, instead of leaning on askFor's reservation (widths.go).
func TestStarsColumnDeclaresTheWidthOfItsSortedHeader(t *testing.T) {
	if got := searchColumns()[searchColStars].MinWidth; got != 7 {
		t.Errorf("Stars MinWidth = %d, want 7 (\"Stars\" + \" ▼\")", got)
	}
}

// The thresholds below were measured on hubRows, not assumed: with 93 columns
// Description is still there, at 92 it is gone; Stars goes at 70, Official at 61.
func TestSearchColumnsLeaveAtTheMeasuredWidths(t *testing.T) {
	has := func(width int, title string) bool {
		tbl := newSearchTable()
		tbl.SetItems(hubRows())
		tbl.Resize(width, 10)
		header := strings.SplitN(ansi.Strip(tbl.Table().View()), "\n", 2)[0]
		return strings.Contains(header, title)
	}
	for _, c := range []struct {
		title      string
		keep, drop int
	}{{"Description", 93, 92}, {"Stars", 71, 70}, {"Official", 62, 61}} {
		if !has(c.keep, c.title) {
			t.Errorf("%s missing at %d columns", c.title, c.keep)
		}
		if has(c.drop, c.title) {
			t.Errorf("%s still there at %d columns", c.title, c.drop)
		}
	}
}

func TestStarsHeaderKeepsItsArrowAtTheNarrowestWidthItSurvivesAt(t *testing.T) {
	tbl := newSearchTable()
	tbl.SetItems(hubRows())
	tbl.SetSort(searchColStars, true)
	tbl.Resize(71, 10)
	if got := ansi.Strip(tbl.Table().View()); !strings.Contains(got, "Stars ▼") {
		t.Errorf("header at 71 columns lacks %q:\n%s", "Stars ▼", got)
	}
}

// confirmSlash types a query in the level 1 filter bar and confirms it.
func confirmSlash(t *testing.T, b *RegistryBrowser, query string) {
	t.Helper()
	b.handleImagesKeyMsg(testutil.Key("/"))
	for _, r := range query {
		b.handleImagesKeyMsg(testutil.Key(string(r)))
	}
	b.handleImagesKeyMsg(testutil.Key("enter"))
	if got := b.searchTable.FilterBar().SearchQuery(); got != query {
		t.Fatalf("fixture: confirmed query = %q, want %q", got, query)
	}
}

// Review: `.` under a confirmed `/` filter keeps the cursor on the same row
// inside the filtered subset.
func TestDotUnderAConfirmedFilterKeepsTheCursorOnTheSameRow(t *testing.T) {
	b, keyA, keyB := cursorBrowser(t)
	arriveStars(t, b, keyA, map[string]int{"web-alpha": 5, "web-bravo": 50, "other": 7})
	arriveStars(t, b, keyB, map[string]int{"web-charlie": 20, "web-delta": 1})
	confirmSlash(t, b, "web")
	if got := len(searchNames(b)); got != 4 {
		t.Fatalf("fixture: %d filtered rows, want 4: %q", got, searchNames(b))
	}
	b.searchTable.SetCursor(slices.Index(searchNames(b), "web-charlie"))
	assertSelected(t, b, keyB, "web-charlie")
	for range 2 { // Name asc, Name desc
		pressDot(t, b)
		assertSelected(t, b, keyB, "web-charlie")
	}
	if got, want := searchNames(b), []string{"web-delta", "web-charlie", "web-bravo", "web-alpha"}; !slices.Equal(got, want) {
		t.Errorf("filtered Name desc = %q, want %q", got, want)
	}
}

// Review: a new search hides the filter bar, not only empties its query.
func TestANewSearchHidesTheFilterBar(t *testing.T) {
	b, keyA, _ := cursorBrowser(t)
	arriveStars(t, b, keyA, map[string]int{"alpha": 5, "bravo": 50})
	confirmSlash(t, b, "bra")
	if !b.FilterIsVisible() {
		t.Fatal("fixture: the confirmed filter bar is not visible")
	}
	b.startImageSearch("nginx", b.entries)
	if b.FilterIsVisible() {
		t.Error("FilterIsVisible() after a new search, want false")
	}
}

// Review: esc clearing a confirmed filter follows the selected row (EntryKey +
// Repo) instead of keeping the index.
func TestEscClearingTheFilterFollowsTheSelectedRow(t *testing.T) {
	b, keyA, keyB := cursorBrowser(t)
	arriveStars(t, b, keyA, map[string]int{"alpha": 5, "bravo": 50, "web-one": 7})
	arriveStars(t, b, keyB, map[string]int{"charlie": 20, "web-two": 1})
	confirmSlash(t, b, "web")
	b.searchTable.SetCursor(slices.Index(searchNames(b), "web-two"))
	assertSelected(t, b, keyB, "web-two")
	b.handleImagesKeyMsg(testutil.Key("esc"))
	if got := b.searchTable.FilterBar().SearchQuery(); got != "" {
		t.Fatalf("esc left the query %q", got)
	}
	if got := len(searchNames(b)); got != 5 {
		t.Fatalf("%d rows after clearing, want 5", got)
	}
	assertSelected(t, b, keyB, "web-two")
}
