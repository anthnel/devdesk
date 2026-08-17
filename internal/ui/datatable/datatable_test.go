package datatable

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// A domain type with something to sort by, something to search and something to
// colour the selection with — the three things the views actually do.
type row struct {
	Name  string
	Size  int
	State string
}

func testConfig() Config[row] {
	return Config[row]{
		SortColumn: 0,
		Columns: []Column[row]{
			{
				Title: "Name", MinWidth: 20, Flex: 1,
				Cell:   func(r row) string { return r.Name },
				Less:   func(a, b row) bool { return a.Name < b.Name },
				Search: func(r row) string { return r.Name },
			},
			{
				Title: "Size", MinWidth: 10,
				Cell: func(r row) string { return strings.Repeat("#", r.Size) },
				Less: func(a, b row) bool { return a.Size < b.Size },
			},
			{
				Title: "State", MinWidth: 12,
				Cell:   func(r row) string { return r.State },
				Search: func(r row) string { return r.State },
			},
		},
	}
}

func fixtures() []row {
	return []row{
		{Name: "api", Size: 3, State: "running"},
		{Name: "cache", Size: 1, State: "exited"},
		{Name: "web", Size: 2, State: "running"},
	}
}

func loaded(t *testing.T) Model[row] {
	t.Helper()
	m := New(testConfig())
	m.Resize(120, 10)
	m.SetItems(fixtures())
	return m
}

func names(items []row) []string {
	out := make([]string, len(items))
	for i, r := range items {
		out[i] = r.Name
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── The cursor and the rows are one thing ────────────────────────────────────

// The duplication worth removing on correctness grounds: nine getSelectedX()
// replayed filter-then-sort by hand, with nothing tying that ordering to the one
// the rows were built from. Here there is one slice, so the cursor cannot point
// at a different object than the screen shows.
func TestSelectedAgreesWithTheRowOnScreen(t *testing.T) {
	m := loaded(t)

	for cursor := range m.Visible() {
		m.Table().SetCursor(cursor)
		item, ok := m.Selected()
		if !ok {
			t.Fatalf("nothing selected at cursor %d", cursor)
		}
		if got := m.Table().Rows()[cursor][0]; got != item.Name {
			t.Errorf("row %d shows %q while Selected() returns %q", cursor, got, item.Name)
		}
	}
}

// And it still agrees once a filter and a sort have reordered everything, which
// is where a hand-replayed pipeline drifts.
func TestSelectedStillAgreesAfterFilteringAndSorting(t *testing.T) {
	m := loaded(t)
	m.CycleSort() // Name descending

	m.Update(testutil.Key("/"))
	for _, msg := range testutil.Type("e") {
		m.Update(msg)
	}

	if len(m.Visible()) == 0 {
		t.Fatal("the filter matched nothing, so there is nothing to check")
	}
	for cursor := range m.Visible() {
		m.Table().SetCursor(cursor)
		item, _ := m.Selected()
		if got := m.Table().Rows()[cursor][0]; got != item.Name {
			t.Errorf("row %d shows %q while Selected() returns %q", cursor, got, item.Name)
		}
	}
}

func TestSelectedOnAnEmptyTableReportsNothing(t *testing.T) {
	m := New(testConfig())
	m.Resize(120, 10)

	if _, ok := m.Selected(); ok {
		t.Error("an empty table reported a selection")
	}
}

// bubbles/table.SetRows leaves the cursor where it was, so a list that just got
// shorter strands it past the end — invisible until the user presses a key, and
// clamped in only two of the eight tables that needed it.
func TestTheCursorIsClampedWhenTheListShrinks(t *testing.T) {
	m := loaded(t)
	m.Table().SetCursor(2)

	m.SetItems(fixtures()[:1])

	if got := m.Cursor(); got != 0 {
		t.Errorf("cursor = %d after the list shrank to one row, want 0", got)
	}
	if item, ok := m.Selected(); !ok || item.Name != "api" {
		t.Errorf("Selected() = %+v, %v — want the row that is actually there", item, ok)
	}
}

// The other half: a refresh that does not shorten the list must leave the
// cursor alone. netdiag's ports table rebuilds every two seconds, and jumping
// to the top each time is what its lazy-rebuild workaround exists to avoid.
func TestARefreshKeepsTheCursorWhereItWas(t *testing.T) {
	m := loaded(t)
	m.Table().SetCursor(2)

	m.SetItems(fixtures()) // same length, new slice

	if got := m.Cursor(); got != 2 {
		t.Errorf("cursor = %d after a refresh, want it left at 2", got)
	}
}

// A view that does want the cursor reset — security resets it on a tab change —
// has to say so, because that is the exception.
func TestGotoTopIsExplicit(t *testing.T) {
	m := loaded(t)
	m.Table().SetCursor(2)

	m.GotoTop()

	if got := m.Cursor(); got != 0 {
		t.Errorf("cursor = %d after GotoTop, want 0", got)
	}
}

// ── Sorting ──────────────────────────────────────────────────────────────────

func TestTheTableOpensSortedOnItsDefaultColumn(t *testing.T) {
	m := loaded(t)

	if got := names(m.Visible()); !equal(got, []string{"api", "cache", "web"}) {
		t.Errorf("visible = %v, want them sorted by name", got)
	}
}

// `.` walks ascending, descending, then on to the next sortable column — the
// same cycle the three hand-written comparators implemented.
func TestCycleSortWalksEachSortableColumnBothWays(t *testing.T) {
	m := loaded(t)

	m.CycleSort() // name descending
	if col, desc := m.SortState(); col != 0 || !desc {
		t.Fatalf("SortState = %d, %v, want name descending", col, desc)
	}
	if got := names(m.Visible()); !equal(got, []string{"web", "cache", "api"}) {
		t.Errorf("visible = %v, want them reversed", got)
	}

	m.CycleSort() // size ascending
	if col, desc := m.SortState(); col != 1 || desc {
		t.Fatalf("SortState = %d, %v, want size ascending", col, desc)
	}
	if got := names(m.Visible()); !equal(got, []string{"cache", "web", "api"}) {
		t.Errorf("visible = %v, want them by size", got)
	}

	m.CycleSort() // size descending
	m.CycleSort() // wraps back to name, since the table opened sorted
	if col, desc := m.SortState(); col != 0 || desc {
		t.Errorf("SortState = %d, %v, want it wrapped back to name ascending", col, desc)
	}
}

// A column with no Less is not a stop on the cycle.
func TestAColumnWithNoComparatorIsSkipped(t *testing.T) {
	m := loaded(t)

	for range 4 {
		m.CycleSort()
		if col, _ := m.SortState(); col == 2 {
			t.Fatal("the cycle stopped on the column that cannot be sorted by")
		}
	}
}

// A table that opened unsorted gets "no sort" back as a stop, so the order the
// items arrived in stays reachable.
func TestATableThatOpenedUnsortedCanReturnToNoSort(t *testing.T) {
	cfg := testConfig()
	cfg.SortColumn = -1
	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(fixtures())

	if col, _ := m.SortState(); col != -1 {
		t.Fatalf("SortState = %d on open, want no sort", col)
	}
	for range 4 { // name asc, name desc, size asc, size desc
		m.CycleSort()
	}
	m.CycleSort()

	if col, _ := m.SortState(); col != -1 {
		t.Errorf("SortState = %d, want it back to no sort", col)
	}
}

// Nothing sortable at all must not leave `.` looping on nothing.
func TestCycleSortDoesNothingWithNoSortableColumn(t *testing.T) {
	cfg := Config[row]{SortColumn: -1, Columns: []Column[row]{
		{Title: "Name", MinWidth: 10, Cell: func(r row) string { return r.Name }},
	}}
	m := New(cfg)
	m.Resize(80, 10)
	m.SetItems(fixtures())

	m.CycleSort()

	if col, _ := m.SortState(); col != -1 {
		t.Errorf("SortState = %d, want it unchanged", col)
	}
}

// SetSort is the one thing CycleSort cannot express: putting the sort back
// where the table opened. The registry browser does it when a new search starts.
func TestSetSortPutsTheOrderBackAndMovesTheArrow(t *testing.T) {
	m := loaded(t)
	m.CycleSort() // name descending
	m.CycleSort() // size ascending

	m.SetSort(0, false)

	if col, desc := m.SortState(); col != 0 || desc {
		t.Fatalf("SortState = %d, %v, want name ascending", col, desc)
	}
	if got := names(m.Visible()); !equal(got, []string{"api", "cache", "web"}) {
		t.Errorf("visible = %v, want them sorted by name again", got)
	}
	if got := m.Table().Columns()[0].Title; !strings.Contains(got, sortArrowAsc) {
		t.Errorf("the Name header is %q, want the ascending arrow back", got)
	}
	if got := m.Table().Columns()[1].Title; strings.Contains(got, sortArrowAsc) {
		t.Errorf("the Size header is %q, want its arrow gone", got)
	}
}

// A column that cannot be sorted by leaves the table unsorted rather than
// silently sorted by something else — the same invariant New settles, so that
// nothing downstream has to re-check which column it holds.
func TestSetSortRefusesAColumnThatCannotBeSortedBy(t *testing.T) {
	for _, column := range []int{2, -1, 99} { // no comparator, none, out of range
		m := loaded(t)

		m.SetSort(column, true)

		if col, desc := m.SortState(); col != -1 || desc {
			t.Errorf("SetSort(%d) left SortState = %d, %v, want no sort", column, col, desc)
		}
	}
}

// The arrow says which column is sorted and which way — three views wrote this
// out verbatim, down to the rune.
func TestTheHeaderCarriesTheSortArrow(t *testing.T) {
	m := loaded(t)

	if got := m.Table().Columns()[0].Title; got != "Name ▲" {
		t.Errorf("header = %q, want the ascending arrow", got)
	}

	m.CycleSort()
	if got := m.Table().Columns()[0].Title; got != "Name ▼" {
		t.Errorf("header = %q, want the descending arrow", got)
	}

	m.CycleSort() // on to Size
	if got := m.Table().Columns()[0].Title; got != "Name" {
		t.Errorf("header = %q, want the arrow gone from the column no longer sorted", got)
	}
	if got := m.Table().Columns()[1].Title; got != "Size ▲" {
		t.Errorf("header = %q, want the arrow moved", got)
	}
}

// sort.Slice is not stable, so two rows equal on the sort column would swap
// between rebuilds and the list would flicker on every refresh.
func TestSortingIsStable(t *testing.T) {
	m := New(testConfig())
	m.Resize(120, 10)
	tied := []row{
		{Name: "a", Size: 1}, {Name: "b", Size: 1}, {Name: "c", Size: 1},
	}
	m.SetItems(tied)
	m.CycleSort() // name desc
	m.CycleSort() // size asc — every row ties

	first := names(m.Visible())
	m.SetItems(tied)

	if got := names(m.Visible()); !equal(got, first) {
		t.Errorf("a second rebuild reordered tied rows: %v then %v", first, got)
	}
}

// ── Filtering ────────────────────────────────────────────────────────────────

// `/` opens the search, and it matches the columns that opted in.
func TestTheSearchMatchesTheSearchableColumns(t *testing.T) {
	m := loaded(t)

	m.Update(testutil.Key("/"))
	if !m.InEditMode() {
		t.Fatal("'/' did not give the search the keyboard")
	}
	for _, msg := range testutil.Type("exited") {
		m.Update(msg)
	}

	if got := names(m.Visible()); !equal(got, []string{"cache"}) {
		t.Errorf("visible = %v, want the row matching on its State column", got)
	}
}

// A column with no Search does not quietly widen the filter.
func TestAColumnWithNoSearchIsNotMatchedAgainst(t *testing.T) {
	m := loaded(t)

	m.Update(testutil.Key("/"))
	for _, msg := range testutil.Type("###") { // the Size column's cell text
		m.Update(msg)
	}

	if got := len(m.Visible()); got != 0 {
		t.Errorf("%d rows matched a query only the non-searchable column contains", got)
	}
}

// Toggle filters are the other half of Rule 136's bar.
func TestATokenFilterNarrowsTheList(t *testing.T) {
	cfg := testConfig()
	cfg.Tokens = []components.FilterToken{{Label: "running"}}
	cfg.TokenMatch = func(r row, active map[string]bool) bool {
		return !active["running"] || r.State == "running"
	}
	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(fixtures())

	m.SetTokenActive("running", true)

	if got := names(m.Visible()); !equal(got, []string{"api", "web"}) {
		t.Errorf("visible = %v, want only the running rows", got)
	}
	if !m.IsTokenActive("running") {
		t.Error("the token does not report itself active")
	}

	m.SetTokenActive("running", false)
	if got := len(m.Visible()); got != 3 {
		t.Errorf("%d rows with the token off, want all of them back", got)
	}
}

// Filtering down past the cursor must not leave it pointing at nothing — the
// clamp and the filter are the same code path for exactly this reason.
func TestFilteringClampsTheCursorToo(t *testing.T) {
	m := loaded(t)
	m.Table().SetCursor(2)

	m.Update(testutil.Key("/"))
	for _, msg := range testutil.Type("api") {
		m.Update(msg)
	}

	if item, ok := m.Selected(); !ok || item.Name != "api" {
		t.Errorf("Selected() = %+v, %v after filtering to one row", item, ok)
	}
}

// ── The selection style ──────────────────────────────────────────────────────

// containers and security each had a refreshSelectionStyle that re-derived the
// filtered, sorted list to find out what was under the cursor. The component
// asks the view for a style instead, which is what keeps it from having to know
// what a severity or a container state is.
func TestTheSelectionStyleFollowsTheSelectedItem(t *testing.T) {
	var asked []string
	cfg := testConfig()
	cfg.SelectedStyles = func(r row) table.Styles {
		asked = append(asked, r.Name)
		if r.State == "exited" {
			return theme.TableStylesForState("error")
		}
		return theme.DefaultTableStyles()
	}
	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(fixtures())

	if len(asked) == 0 || asked[len(asked)-1] != "api" {
		t.Fatalf("the view was asked about %v, want the first row", asked)
	}

	m.Update(testutil.Key("down")) // cache, which is exited

	if asked[len(asked)-1] != "cache" {
		t.Errorf("the view was asked about %q after moving down, want %q", asked[len(asked)-1], "cache")
	}
}

// A blurred table is not the one the user is acting on, so its selection stays
// dimmed rather than lit by whatever is under its cursor.
func TestABlurredTableDoesNotLightItsSelection(t *testing.T) {
	var asked int
	cfg := testConfig()
	cfg.SelectedStyles = func(row) table.Styles {
		asked++
		return theme.DefaultTableStyles()
	}
	m := New(cfg)
	m.Resize(120, 10)
	m.Blur()
	before := asked

	m.SetItems(fixtures())

	if asked != before {
		t.Error("a blurred table asked for its selection style")
	}

	m.Focus()
	if asked == before {
		t.Error("focus did not take the styles back")
	}
}

// ── Keys ─────────────────────────────────────────────────────────────────────

func TestNavigationKeysMoveTheCursor(t *testing.T) {
	m := loaded(t)

	m.Update(testutil.Key("down"))
	if m.Cursor() != 1 {
		t.Errorf("cursor = %d after down, want 1", m.Cursor())
	}
	m.Update(testutil.Key("down"))
	if m.Cursor() != 2 {
		t.Errorf("cursor = %d after a second down, want 2", m.Cursor())
	}
	m.Update(testutil.Key("home"))
	if m.Cursor() != 0 {
		t.Errorf("cursor = %d after home, want the top", m.Cursor())
	}
	m.Update(testutil.Key("end"))
	if m.Cursor() != 2 {
		t.Errorf("cursor = %d after end, want the bottom", m.Cursor())
	}
	m.Update(testutil.Key("up"))
	if m.Cursor() != 1 {
		t.Errorf("cursor = %d after up, want 1", m.Cursor())
	}
}

// The letters that used to alias the arrows are actions now, and the table must
// leave them for the view (§3.26). This is the component-level half of the rule
// keymap's source scan states globally.
func TestVimAliasesAreNotNavigation(t *testing.T) {
	for _, key := range []string{"j", "k", "g", "G", "h", "l"} {
		m := loaded(t)
		m.Update(testutil.Key("down"))
		before := m.Cursor()

		m.Update(testutil.Key(key))
		if m.Cursor() != before {
			t.Errorf("%q moved the cursor from %d to %d; no bare letter is navigation",
				key, before, m.Cursor())
		}
	}
}

// The view keeps its own actions: anything the component does not claim is left
// for it, or every table would swallow ctrl+d.
func TestUnclaimedKeysAreLeftAlone(t *testing.T) {
	m := loaded(t)
	before := m.Cursor()

	if cmd := m.Update(testutil.Key(keymap.Delete)); cmd != nil {
		t.Errorf("the component answered ctrl+d with %T", testutil.Msg(cmd))
	}
	if m.Cursor() != before {
		t.Error("an unclaimed key moved the cursor")
	}
}

// While the search has the keyboard, the navigation keys are text. "a" is used
// rather than "j" so rows survive the filter and a moved cursor would show.
func TestTheSearchTakesTheKeysWhileItIsOpen(t *testing.T) {
	m := loaded(t)
	m.Update(testutil.Key("/"))

	for _, msg := range testutil.Type("aj") {
		m.Update(msg)
	}

	if got := m.FilterBar().SearchQuery(); got != "aj" {
		t.Errorf("the query is %q, want both characters typed into it", got)
	}

	// Backspace out the 'j' so rows come back, and check nothing navigated.
	m.Update(testutil.Key("backspace"))
	if got := len(m.Visible()); got != 2 {
		t.Fatalf("%d rows match \"a\", want api and cache", got)
	}
	if m.Cursor() != 0 {
		t.Errorf("cursor = %d — a typed key moved the cursor", m.Cursor())
	}
}

// An empty result leaves nothing to select, and saying so is the whole contract
// of Selected's second return value.
func TestFilteringToNothingSelectsNothing(t *testing.T) {
	m := loaded(t)
	m.Update(testutil.Key("/"))

	for _, msg := range testutil.Type("zzz") {
		m.Update(msg)
	}

	if len(m.Visible()) != 0 {
		t.Fatalf("visible = %v, want nothing to match", names(m.Visible()))
	}
	if _, ok := m.Selected(); ok {
		t.Error("a table showing no rows reported a selection")
	}
}

// ── Rows ─────────────────────────────────────────────────────────────────────

// Rule 122: a styled string in a table.Row is truncated mid-escape and bleeds
// over every row below it. Cell returns a string and the component writes it
// unchanged, so there is nowhere for a style to enter.
func TestTheRowsHoldExactlyWhatTheCellsReturned(t *testing.T) {
	m := loaded(t)

	for i, item := range m.Visible() {
		for j, c := range m.cfg.Columns {
			if got, want := m.Table().Rows()[i][j], c.Cell(item); got != want {
				t.Errorf("row %d column %d = %q, want %q", i, j, got, want)
			}
			if strings.Contains(m.Table().Rows()[i][j], "\x1b") {
				t.Errorf("row %d column %d carries an escape sequence", i, j)
			}
		}
	}
}

func TestResizeAppliesTheSolvedWidths(t *testing.T) {
	m := loaded(t)

	m.Resize(100, 10)

	total := 0
	for _, c := range m.Table().Columns() {
		total += c.Width
	}
	if want := availableFor(100, 3); total != want {
		t.Errorf("the columns sum to %d, want %d (Rule 116)", total, want)
	}
}

// The other half of the clamp, which the search test found: a filter typed past
// its last match empties the table and bubbles/table reports -1, and correcting
// the query must give the selection back rather than leave every row unselected.
func TestTheCursorComesBackWhenRowsDo(t *testing.T) {
	m := loaded(t)
	m.SetItems(nil)

	if _, ok := m.Selected(); ok {
		t.Fatal("an empty table reported a selection")
	}

	m.SetItems(fixtures())

	item, ok := m.Selected()
	if !ok {
		t.Fatal("rows came back with nothing selected")
	}
	if item.Name != "api" {
		t.Errorf("Selected() = %q, want the first row", item.Name)
	}
}

// ── The rest of the surface ──────────────────────────────────────────────────

func TestItemsReturnsEverythingHeld(t *testing.T) {
	m := loaded(t)
	m.Update(testutil.Key("/"))
	for _, msg := range testutil.Type("api") {
		m.Update(msg)
	}

	if got := len(m.Visible()); got != 1 {
		t.Fatalf("visible = %d, want the filter to have narrowed it", got)
	}
	if got := len(m.Items()); got != 3 {
		t.Errorf("Items() = %d, want everything held regardless of the filter", got)
	}
}

func TestViewRendersTheRows(t *testing.T) {
	m := loaded(t)
	view := m.View()

	for _, want := range []string{"Name", "api", "cache", "web"} {
		if !strings.Contains(view, want) {
			t.Errorf("the view does not contain %q:\n%s", want, view)
		}
	}
}

// A SortColumn past the end is a typo in the config, and starting unsorted beats
// panicking on the first rebuild.
func TestASortColumnOutOfRangeIsReadAsNoSort(t *testing.T) {
	cfg := testConfig()
	cfg.SortColumn = 99
	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(fixtures())

	if col, _ := m.SortState(); col != -1 {
		t.Errorf("SortState = %d, want no sort", col)
	}
	if got := names(m.Visible()); !equal(got, []string{"api", "cache", "web"}) {
		t.Errorf("visible = %v, want the order they arrived in", got)
	}
}

// Paging is navigation like any other, and it has to clamp at both ends.
func TestPagingMovesAndClamps(t *testing.T) {
	m := loaded(t)

	m.Update(testutil.Key("pgdown"))
	if m.Cursor() != 2 {
		t.Errorf("cursor = %d after pgdown on a three-row table, want the last row", m.Cursor())
	}
	m.Update(testutil.Key("pgup"))
	if m.Cursor() != 0 {
		t.Errorf("cursor = %d after pgup, want the top", m.Cursor())
	}
}

// The component only claims keys; a message that is not one is not its business.
func TestANonKeyMessageIsIgnored(t *testing.T) {
	m := loaded(t)

	if cmd := m.Update(struct{}{}); cmd != nil {
		t.Errorf("a non-key message produced %T", testutil.Msg(cmd))
	}
}

// A viewport with no room to give must still render rather than panic on a
// negative height. bubbles/table counts the header against the height it is
// given, so a floor of one line leaves zero data rows — which is the honest
// answer for a terminal that small, not an error.
func TestTheHeightNeverGoesNegative(t *testing.T) {
	m := loaded(t)

	m.SetHeight(-5)

	if got := m.Table().Height(); got < 0 {
		t.Errorf("height = %d, want it floored rather than negative", got)
	}
	if m.View() == "" {
		t.Error("a table with no room rendered nothing at all")
	}
}

// A SortColumn naming a column that has no comparator is the other config
// mistake: it must leave the items in the order they arrived rather than sort
// them by nothing, and `.` has to be able to walk off it.
func TestASortColumnWithNoComparatorSortsNothing(t *testing.T) {
	cfg := testConfig()
	cfg.SortColumn = 2 // State, which has no Less
	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems([]row{{Name: "web"}, {Name: "api"}, {Name: "cache"}})

	if got := names(m.Visible()); !equal(got, []string{"web", "api", "cache"}) {
		t.Errorf("visible = %v, want the order they arrived in", got)
	}

	m.CycleSort()
	if col, _ := m.SortState(); col == 2 {
		t.Errorf("SortState = %d, want the cycle to have moved off the column it cannot sort by", col)
	}
	if got := names(m.Visible()); !equal(got, []string{"api", "cache", "web"}) {
		t.Errorf("visible = %v, want them sorted once the cycle reached a sortable column", got)
	}
}

// A query spanning two columns matches the row. netdiag searched a joined
// haystack while the other filter loops went per field; matching both ways is a
// superset of either, so migrating a view cannot silently drop matches.
func TestAQuerySpanningTwoColumnsMatchesTheRow(t *testing.T) {
	m := loaded(t)
	m.Update(testutil.Key("/"))

	for _, msg := range testutil.Type("api run") { // Name then State
		m.Update(msg)
	}

	if got := names(m.Visible()); !equal(got, []string{"api"}) {
		t.Errorf("visible = %v, want the row whose two columns together match", got)
	}
}

// The joined pass must not invent matches out of the gap between columns: a
// query that spans them in the wrong order still matches nothing.
func TestTheJoinedPassRespectsColumnOrder(t *testing.T) {
	m := loaded(t)
	m.Update(testutil.Key("/"))

	for _, msg := range testutil.Type("running api") { // State then Name — reversed
		m.Update(msg)
	}

	if got := len(m.Visible()); got != 0 {
		t.Errorf("%d rows matched a query in the wrong column order", got)
	}
}

// A table can open on the descending order. It exists for the count columns,
// where ascending is the useless end — an inventory sorted by CRITICAL wants
// the worst row first, and cycling `.` past ascending to reach it on every open
// is not a default.
func TestATableCanOpenDescending(t *testing.T) {
	cfg := testConfig()
	cfg.SortDesc = true
	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(fixtures())

	if col, desc := m.SortState(); col != 0 || !desc {
		t.Fatalf("SortState = %d, %v, want name descending", col, desc)
	}
	if got := names(m.Visible()); !equal(got, []string{"web", "cache", "api"}) {
		t.Errorf("visible = %v, want them reversed on open", got)
	}
	if title := m.Table().Columns()[0].Title; !strings.Contains(title, "▼") {
		t.Errorf("header = %q, want the descending arrow", title)
	}
}

// The direction has to go with the column: dropping an unusable SortColumn but
// keeping SortDesc would put the arrow on nothing and make `.` open descending.
func TestADirectionWithNoColumnToSortIsDropped(t *testing.T) {
	cfg := testConfig()
	cfg.SortColumn = 2 // the column with no comparator
	cfg.SortDesc = true
	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(fixtures())

	if col, desc := m.SortState(); col != -1 || desc {
		t.Errorf("SortState = %d, %v, want no sort in either direction", col, desc)
	}
	m.CycleSort()
	if _, desc := m.SortState(); desc {
		t.Error("the first `.` opened on descending")
	}
}

// A sortable column has to be wide enough for its own header *plus* the arrow
// this package appends to it. MinWidth is the view's statement about the
// column's content, and nothing told it about those two cells — so the security
// inventory's CRIT column asked for 5, rendered "CRIT ▼" into it, and lost
// exactly the character that says which way it is sorted.
func TestASortableColumnKeepsRoomForItsArrow(t *testing.T) {
	cfg := Config[row]{
		SortColumn: 0,
		Columns: []Column[row]{
			{
				Title: "CRIT", MinWidth: 5, // narrower than "CRIT ▼"
				Cell: func(r row) string { return "0" },
				Less: func(a, b row) bool { return a.Size < b.Size },
			},
			{Title: "Name", MinWidth: 20, Flex: 1, Cell: func(r row) string { return r.Name }},
		},
	}
	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(fixtures())

	title := m.Table().Columns()[0].Title
	if lipgloss.Width(title) > m.Table().Columns()[0].Width {
		t.Errorf("header %q is %d wide in a column of %d — the arrow is truncated",
			title, lipgloss.Width(title), m.Table().Columns()[0].Width)
	}
	if !strings.Contains(m.View(), title) {
		t.Errorf("the rendered header does not carry %q:\n%s", title, m.View())
	}
}

// The room is reserved whether or not the column is the sorted one, so cycling
// `.` does not resize it and shift every column beside it.
func TestTheArrowReserveDoesNotDependOnTheSort(t *testing.T) {
	cfg := Config[row]{
		SortColumn: -1,
		Columns: []Column[row]{
			{
				Title: "CRIT", MinWidth: 5,
				Cell: func(r row) string { return "0" },
				Less: func(a, b row) bool { return a.Size < b.Size },
			},
			{Title: "Name", MinWidth: 20, Flex: 1, Cell: func(r row) string { return r.Name }},
		},
	}
	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(fixtures())

	unsorted := m.Table().Columns()[0].Width
	m.CycleSort()
	if sorted := m.Table().Columns()[0].Width; sorted != unsorted {
		t.Errorf("the column is %d wide unsorted and %d sorted; sorting must not move the layout",
			unsorted, sorted)
	}
}

// A column that cannot be sorted by gets no arrow, so it gets no reserve: the
// view's MinWidth is the whole of its ask.
func TestAnUnsortableColumnIsNotWidenedForAnArrow(t *testing.T) {
	cfg := Config[row]{
		SortColumn: -1,
		Columns: []Column[row]{
			{Title: "LONGHEADER", MinWidth: 4, Cell: func(r row) string { return "0" }},
			{Title: "Name", MinWidth: 20, Flex: 1, Cell: func(r row) string { return r.Name }},
		},
	}
	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(fixtures())

	if got := m.Table().Columns()[0].Width; got != 4 {
		t.Errorf("the column is %d wide, want the 4 it asked for", got)
	}
}
