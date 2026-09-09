// Package datatable is the shared mechanism behind the application's tables.
//
// What was shared before was only the *look* — theme.DefaultTableStyles,
// TableStylesForState/ForSeverity, components.FilterBar, theme.TimeAgo. Column
// widths, sorting, sort arrows, filter matching, cursor clamping and
// cursor-to-object resolution were written out again in each of the fifteen
// tables, and drifted:
//
//   - Rule 116's width invariant is broken by five of the twelve copies, which
//     clamp per column after the remainder has been computed (see widths.go).
//   - Two of eight clamp the cursor when the row count shrinks; the rest leave
//     it dangling, and oci_resources compensates by jumping to the top on every
//     filter toggle, throwing away the scroll position.
//   - Nine getSelectedX() replay filter-then-sort by hand to map a cursor back
//     to an object, with nothing tying that ordering to the one the rows were
//     built from. If they drift, the action lands on the wrong object and says
//     nothing. That is the duplication worth removing on correctness grounds.
//
// Here the filtered, sorted slice is built once and kept, so Selected() cannot
// disagree with what is on screen, and Cell func(T) string gives styled text
// nowhere to go — Rule 122 by construction rather than by review.
package datatable

import (
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Sizing is what decides a column's width: the number it declared, or the
// content it holds. Every column states one — the zero value is refused by
// TestEveryColumnDeclaresItsSizing rather than given a meaning, because a
// default here would decide for twenty tables nobody had looked at (D12).
type Sizing int

const (
	// SizingUnset is the zero value, and means nothing. See the test above.
	SizingUnset Sizing = iota
	// SizingFixed takes exactly the width declared. For a count, a state, an
	// icon — anything whose size was known when the code was written.
	SizingFixed
	// SizingContent takes the width of its widest visible value, never below
	// MinWidth and never above MaxWidth. For paths, URLs, addresses, image
	// references: values whose length is a property of the data.
	SizingContent
)

// Column describes one column and how to get its value out of an item.
type Column[T any] struct {
	Title string
	// Sizing is the column's nature (see above). It has no default.
	Sizing Sizing
	// MinWidth is the width a SizingFixed column takes, and the floor a
	// SizingContent column is never squeezed under.
	//
	// It is a floor now, not the request it used to be: the shortfall comes out
	// of the content columns down to their MinWidth, and after that whole
	// columns are removed rather than emptied one cell at a time (widths.go).
	// The name stays MinWidth on a fixed column, where it *is* the width, for
	// the reason §3.45 gives: Width would be false on a content column that
	// grows past it, where MinWidth is merely redundant on a fixed one — a
	// redundant name is bearable, a false one is not.
	MinWidth int
	// MaxWidth caps a SizingContent column. 0 means no ceiling, and there is no
	// global default: a ceiling chosen once would apply to columns nobody has
	// looked at, and a full IPv6 address is 45 cells — so the first "reasonable"
	// default re-truncates exactly what the measurement was for. Meaningless on
	// SizingFixed, where MinWidth already is the width.
	MaxWidth int
	// Optional marks a column that may be dropped whole when the table does not
	// fit, before any column that is not. Orthogonal to Sizing because all four
	// combinations exist: the interface error counters have an exact width and
	// are also the first thing worth losing.
	Optional bool
	// DropFirst moves an Optional column to the head of the queue: it goes
	// before every other Optional one, wherever it sits on screen. Meaningless
	// without Optional, which is the flag that decides whether a column may go
	// at all.
	//
	// It exists because drop() reads the column order as an order of
	// importance, and that is true of every column whose place is chosen by
	// what it *is*. A gauge's place is chosen by what it *illustrates* — it has
	// to sit beside the number it draws, in the middle of the table, or it
	// stops being read as that number's picture (§3.71). Left to position
	// alone, the containers table would shed its I/O counters to keep two bars
	// that are the most expendable thing on the row.
	//
	// A bool rather than a rank: two tiers is what the case needs, and an int
	// would invite every table to number its columns against each other.
	DropFirst bool
	// TruncateHead cuts the start of an over-long value rather than its end.
	//
	// A flag per column rather than a rule derived from Sizing, because the
	// answer depends on the column and not on its nature: a URL, an image
	// reference and a path share their prefix and are told apart by their end,
	// while an address is identified by the network it starts with. It cannot
	// be left to Cell either — Cell does not know the width it will be rendered
	// at, and that is deliberate: it is what makes the value measurable.
	TruncateHead bool
	// Flex is the column's share of the space left over once every column has
	// what it wants. 0 takes no part; the leftover is split between the
	// flexible ones by weight.
	//
	// Flex says who receives the surplus, never who needs it — which is why it
	// cannot decide widths on its own: the ports table gave Process 110 cells
	// for "svchost.exe" at 200 columns while Peer Address stayed frozen at 26,
	// cutting an IPv6 endpoint in half. That is what Sizing answers.
	Flex int
	// Cell returns the text for this column. Plain text only: it is measured
	// and truncated before anything is applied to it, and runewidth counts an
	// escape sequence's bytes as width — a styled string is cut mid-escape and
	// bleeds over every row below it (Rule 122). Taking a string rather than a
	// styled value is what makes that unexpressible here.
	//
	// It must be pure and cheap. The rendering calls it once per visible cell;
	// the measurement calls it once per cell of every visible row, off screen
	// included. Nothing can check that — it is an arbitrary closure — so it is
	// stated here rather than tested.
	Cell func(T) string
	// Style colours the cell after it has been measured and truncated, which is
	// the only order in which colour is safe (see render.go). Nil leaves the
	// cell in the table's own colours.
	//
	// On a row a view has coloured whole via SelectedStyles (error, busy, a CVE
	// severity), it is not consulted: that row is handed to styles.Selected
	// whole, and a colour inside it would end the highlight mid-row. On the
	// plain "normal" selection it *is* consulted — see render.go's cellStyle,
	// which repaints the shared background on every cell instead of once on an
	// outer wrap, so per-cell colour and the highlight no longer fight.
	Style func(T) lipgloss.Style
	// Cut and TailStyle split a cell into two coloured runs instead of Style's
	// one: Style colours the first Cut(item) cells of the *finished* text —
	// already measured, truncated and padded to the column's width — and
	// TailStyle colours the rest. Both nil (the ordinary case, every column but
	// the containers load gauges) leaves Style covering the whole cell as
	// before.
	//
	// This is not the gradient Rule 122 forbids: nothing styled is ever
	// measured. Cut reads a plain, already-final string — the split happens
	// after truncation, at the same point Style already does its colouring —
	// so it is the same "measure first, colour after" order applied twice
	// instead of once, not an exception to it. It stays two runs rather than an
	// arbitrary list: one caller has needed it since it was added, and Cut, a
	// bare integer, is cheaper to reason about than a slice of spans that only
	// ever holds two elements.
	//
	// Dropped only for a row a view has coloured whole — error, busy, a CVE
	// severity — for the same reason Style is: a two-run cell still needs its
	// own reset between the runs, which that solid background cannot afford.
	// The plain "normal" selection consults both: each run repaints
	// ColorSeverityLow and bold itself, the same per-cell repaint that makes a
	// single-run selected cell safe, so a load gauge keeps its fill/track
	// split under the cursor instead of collapsing to Style's colour alone.
	Cut       func(T) int
	TailStyle func(T) lipgloss.Style
	// Less sorts by this column. Nil means the column cannot be sorted by, and
	// `.` skips it.
	Less func(a, b T) bool
	// Search is what the text filter matches against. Nil means the column does
	// not participate.
	Search func(T) string
}

// Config is everything a view has to say about its table.
type Config[T any] struct {
	Columns []Column[T]
	// Tokens are the toggle filters shown in the bar (Rule 136).
	Tokens []components.FilterToken
	// TokenMatch decides whether an item passes the active toggles. Nil means
	// the toggles are ignored, which is only sensible with no Tokens.
	TokenMatch func(item T, active map[string]bool) bool
	// SortColumn is the column sorted by on open; -1 for no initial sort.
	SortColumn int
	// SortDesc opens the table on the descending order of SortColumn. It exists
	// for the count columns, where ascending is the useless end: an inventory
	// sorted by CRITICAL wants the worst target first, and cycling `.` past
	// ascending to reach it on every open is not a default.
	SortDesc bool
	// Key identifies an item across rebuilds, and is what the busy set is held
	// on. Nil switches the busy facility off entirely, which is the default:
	// most tables have no action to run on a row.
	//
	// It cannot be a `Busy func(T) bool` on this struct instead. Columns are
	// built once, in New, and close over nothing — imageRow exists because of
	// that — so a predicate here would have to close over the view's map of
	// in-flight actions, which is the same trap. Separating the identity from
	// the state is what avoids it, and it is also what lets IsBusy answer for an
	// object whose row does not exist yet: the guard on a confirm path runs
	// before anything is rebuilt.
	//
	// Keying on identity rather than a flag on the row is also what survives the
	// periodic refresh, which replaces the items while an action is running.
	Key func(T) string
	// StatusColumn is the column whose cell the spinner replaces while a row is
	// busy — the one that answers "what about this row". Ignored when Key is
	// nil.
	//
	// That column must not be sortable: askFor reserves width(Title)+2 for a
	// column carrying a Less, which is expensive for a glyph.
	StatusColumn int
	// SelectedStyles returns the table styles to use while the given item is
	// under the cursor — that is the only thing bubbles/table can vary per
	// selection, and it is what containers and security each re-derived by
	// hand. Returning a style rather than a state keyword is what keeps this
	// package from having to know what a severity is. Nil means the default.
	SelectedStyles func(T) table.Styles
}

// Model is a table over a slice of T.
type Model[T any] struct {
	cfg   Config[T]
	table table.Model
	bar   components.FilterBar

	items []T
	// visible is the filtered, sorted slice the rows were built from. Selected()
	// reads it rather than recomputing, so the cursor cannot point at one
	// ordering while the screen shows another.
	visible []T

	// styles is what was last handed to the table. The rendering is this
	// package's now (render.go) and bubbles keeps its copy unexported, so the
	// styles have to be kept on this side to be readable at render time.
	styles table.Styles
	// preserveColumnColors is true when styles.Selected is the plain "normal"
	// look (ColorSeverityLow) rather than a view's own error/busy/severity
	// override. Computed once in applyStyles, read per cell in cellStyle: it
	// is what tells the two selected-row treatments apart without render.go
	// having to know what a severity or a busy row is.
	preserveColumnColors bool
	// offset is the first visible row — the scroll window bubbles kept in its
	// viewport. clampOffset owns it.
	offset int

	// busy maps an item's Key to the label of the action running on it. Held
	// here rather than in the view so that one answer — IsBusy — serves the
	// rendering, the footer and the guard that stops a second command being
	// issued for the same object.
	busy map[string]string
	// spinnerFrame is what a busy row's status cell shows. The frame lives here
	// and the tick stays in the view: a spinner needs a Cmd, and this package
	// returns none (Rule 110).
	spinnerFrame string
	spinnerIdx   int

	// natural is the widest text each column produces over the visible rows,
	// header included. rebuild refreshes it because it is already walking every
	// cell there; it costs one lipgloss.Width per cell and nothing else.
	natural []int
	// measured is the copy the solver reads — natural as of the last
	// measurement *moment*. The two are separate because the moment is the
	// whole design: SetItems cannot tell a periodic refresh from a change of
	// population (both arrive the same way), and remeasuring on every tick
	// would make the columns dance on their own. See Remeasure.
	measured []int
	// measuredOnce records that a non-empty population has been measured. The
	// first one measures itself: without that a table stays at its MinWidths
	// for the life of the view, and nothing on screen says why.
	measuredOnce bool

	sortColumn int
	sortDesc   bool
	width      int
}

// New builds a table from its configuration.
func New[T any](cfg Config[T]) Model[T] {
	cols := make([]table.Column, len(cfg.Columns))
	for i, c := range cfg.Columns {
		cols[i] = table.Column{Title: c.Title, Width: c.MinWidth}
	}
	t := table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(10))

	bar := components.NewFilterBar()
	if len(cfg.Tokens) > 0 {
		bar = components.NewFilterBarWithTokens(cfg.Tokens)
	}

	m := Model[T]{cfg: cfg, table: t, bar: bar, sortColumn: cfg.SortColumn, sortDesc: cfg.SortDesc}
	// A frame from the start: a table whose view never calls AdvanceSpinner —
	// one rendered in the same frame the action began — would otherwise show an
	// empty status cell, which reads as "nothing is happening".
	m.AdvanceSpinner()
	// Through setStyles rather than the table alone: a table rendered before
	// anything calls applyStyles — one built and drawn in the same frame — would
	// otherwise render with the zero Styles, which carries no cell padding and
	// so lays out two cells narrower per column than every width was solved for.
	m.setStyles(theme.DefaultTableStyles())
	// The invariant the rest of the file rests on: sortColumn is either -1 or a
	// column that can actually be sorted by. Settling it once here is what lets
	// CycleSort and sorted() stop re-checking — and what stops `.` getting stuck
	// flipping the direction of a column with no comparator.
	if m.sortColumn >= len(cfg.Columns) || (m.sortColumn >= 0 && cfg.Columns[m.sortColumn].Less == nil) {
		m.sortColumn = -1
		// A direction with no column to apply it to would put the header arrow
		// on nothing and make `.` open on descending.
		m.sortDesc = false
	}
	return m
}

// SetItems replaces the contents. Filtering, sorting, row building and the
// cursor clamp all happen here and only here, which is what stops a cursor from
// being left pointing past the end — bubbles/table's SetRows does not clamp, and
// the stale cursor is invisible until the user presses a key.
func (m *Model[T]) SetItems(items []T) {
	m.items = items
	m.rebuild()
	// The one measurement this package takes on its own initiative. Every other
	// SetItems may be a two-second refresh, and a table that re-measured on
	// those would reshuffle its own columns while the user reads it.
	if !m.measuredOnce && len(m.visible) > 0 {
		m.Remeasure()
	}
}

// Remeasure re-reads the widest value in each column and lays the table out
// again. Call it at the moments a *user* changed what the table holds — a tab
// change, a drill-down, an explicit refresh — and never on a timer.
//
// This package cannot tell those apart on its own: a periodic reload and a
// change of population both arrive through SetItems, so the view is the only
// thing that knows. The cost of getting it wrong in the quiet direction is
// stated rather than discovered: a value that grew between two measurements
// stays truncated until the next one.
func (m *Model[T]) Remeasure() {
	m.measuredOnce = m.measuredOnce || len(m.visible) > 0
	m.Resize(m.width, 0)
}

// promote makes the current measurement the one the solver reads. Resize is the
// only caller: a resize is itself a measurement moment — the user just changed
// how much room there is, and the content has not moved — and routing every
// promotion through it is what keeps Remeasure to one line.
func (m *Model[T]) promote() {
	m.measured = append(m.measured[:0], m.natural...)
}

// Items returns everything held, filtered or not.
func (m *Model[T]) Items() []T { return m.items }

// Visible returns what is on screen, in the order it is on screen.
func (m *Model[T]) Visible() []T { return m.visible }

// Selected returns the item under the cursor. The bool is false when there is
// nothing to select, which is the only case a caller has to handle.
func (m *Model[T]) Selected() (T, bool) {
	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(m.visible) {
		var zero T
		return zero, false
	}
	return m.visible[cursor], true
}

// Cursor returns the selected row index.
func (m *Model[T]) Cursor() int { return m.table.Cursor() }

// SetCursor puts the cursor on a row, clamped to what exists. Views that
// remember a position across a reload — workspaces restores one per directory
// level on the way back up — call this; nothing else should need it.
func (m *Model[T]) SetCursor(i int) {
	if len(m.visible) == 0 {
		return
	}
	m.table.SetCursor(min(max(i, 0), len(m.visible)-1))
	m.afterCursorMove()
}

// GotoTop moves the cursor to the first row. Views that want the cursor reset
// on a change of scope — security resets it when the tab changes — call this
// explicitly, because SetItems deliberately does not.
func (m *Model[T]) GotoTop() {
	m.table.GotoTop()
	m.afterCursorMove()
}

// FilterBar exposes the bar for RenderFooter and GetFooterHeight (Rule 136).
func (m *Model[T]) FilterBar() *components.FilterBar { return &m.bar }

// InEditMode reports whether the search field has the keyboard.
func (m *Model[T]) InEditMode() bool { return m.bar.InEditMode() }

// Searchable reports whether anything can be filtered: a searchable column or a
// toggle token. Views use it to decide whether to advertise `/` (Rule 130).
func (m *Model[T]) Searchable() bool {
	if len(m.cfg.Tokens) > 0 {
		return true
	}
	for _, c := range m.cfg.Columns {
		if c.Search != nil {
			return true
		}
	}
	return false
}

// Table exposes the underlying table for rendering.
func (m *Model[T]) Table() *table.Model { return &m.table }

// SetHeight sets the number of rows shown. The window is re-clamped: a table
// that just got shorter can leave the cursor below its own last visible row.
func (m *Model[T]) SetHeight(h int) {
	m.table.SetHeight(max(h, 1))
	m.clampOffset()
}

// Focus and Blur move the keyboard between two tables sharing a viewport, which
// is what the status view does with its monitors and its certificates.
func (m *Model[T]) Focus() {
	m.table.Focus()
	m.afterCursorMove()
}

func (m *Model[T]) Blur() {
	m.table.Blur()
	m.setStyles(theme.BlurredTableStyles())
}

// Resize recomputes the column widths for a viewport of this width (Rule 116).
// The width passed is the full viewport width, borders included; the borders and
// the per-cell padding are subtracted here so no caller has to remember to.
func (m *Model[T]) Resize(width, height int) {
	m.width = width
	m.promote()
	widths := solveWidths(m.cfg.Columns, width, m.measured)

	cols := m.table.Columns()
	for i := range cols {
		cols[i].Width = widths[i]
		cols[i].Title = m.titleFor(i)
	}
	m.table.SetColumns(cols)
	m.bar.Resize(width)
	if height > 0 {
		m.SetHeight(height)
	}
	m.afterCursorMove() // the selected row is pinned to this width
}

// The sort arrows, and what they cost. Named because widths.go has to reserve
// room for them: a column's MinWidth is the view's statement about its content,
// and it knows nothing about two cells this package appends to the header.
const (
	sortArrowAsc  = " ▲"
	sortArrowDesc = " ▼"
)

// titleFor returns a column's header, with a sort arrow on the active one.
func (m *Model[T]) titleFor(i int) string {
	if i != m.sortColumn {
		return m.cfg.Columns[i].Title
	}
	if m.sortDesc {
		return m.cfg.Columns[i].Title + sortArrowDesc
	}
	return m.cfg.Columns[i].Title + sortArrowAsc
}

// CycleSort advances the sort: ascending, descending, then on to the next
// sortable column (Rule 111's `.`). Returns to no sort after the last one when
// the table opened unsorted, so the original order stays reachable.
func (m *Model[T]) CycleSort() {
	sortable := m.sortableColumns()
	if len(sortable) == 0 {
		return
	}
	switch {
	case m.sortColumn < 0:
		m.sortColumn, m.sortDesc = sortable[0], false
	case !m.sortDesc:
		m.sortDesc = true
	default:
		m.sortColumn, m.sortDesc = m.nextSortable(sortable), false
	}
	m.rebuild()
	m.Resize(m.width, 0) // the arrow moved
}

// nextSortable returns the column after the current one, wrapping to the first —
// or to -1 when the table opened unsorted, so "no sort" is a stop on the cycle.
//
// slices.Index rather than a loop with a trailing fallback: the current column
// is always in sortable (New guarantees it), and a fallback that cannot run is
// a branch nothing can ever check.
func (m *Model[T]) nextSortable(sortable []int) int {
	if at := slices.Index(sortable, m.sortColumn); at+1 < len(sortable) {
		return sortable[at+1]
	}
	if m.cfg.SortColumn < 0 {
		return -1
	}
	return sortable[0]
}

func (m *Model[T]) sortableColumns() []int {
	var out []int
	for i, c := range m.cfg.Columns {
		if c.Less != nil {
			out = append(out, i)
		}
	}
	return out
}

// SortState reports the column sorted by and its direction, for tests and for
// views that show it elsewhere. Column is -1 when nothing is sorted.
func (m *Model[T]) SortState() (column int, desc bool) { return m.sortColumn, m.sortDesc }

// SetSort puts the sort back where the table opened, or anywhere else the view
// can name. It exists for the one thing CycleSort cannot express: the registry
// browser starts a new search from the default order, and advancing `.` around
// the cycle until it comes back is not that.
//
// A column that cannot be sorted by leaves the table unsorted rather than
// silently sorted by something else — the same invariant New settles, so that
// nothing downstream has to re-check it.
func (m *Model[T]) SetSort(column int, desc bool) {
	if column < 0 || column >= len(m.cfg.Columns) || m.cfg.Columns[column].Less == nil {
		column, desc = -1, false
	}
	m.sortColumn, m.sortDesc = column, desc
	m.rebuild()
	m.Resize(m.width, 0) // the arrow moved
}

// Update handles the keys every table shares: navigation, `/` for the search,
// `.` for the sort, and whatever the filter bar takes while it is focused.
// Anything else is left alone, so the view keeps its own actions.
func (m *Model[T]) Update(msg tea.Msg) tea.Cmd {
	if m.bar.InEditMode() {
		var cmd tea.Cmd
		m.bar, cmd = m.bar.Update(msg)
		m.rebuild()
		// A search that keeps only short values has to give the room back, or
		// the filter buys nothing visually. It is a user action on a settled
		// population, which is exactly a measurement moment.
		m.Remeasure()
		return cmd
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "/":
		// A table with nothing to search does not claim the key: activating a
		// search that can only ever match nothing would empty the list, and the
		// bar the user needs to see why is not in that view's footer.
		if !m.Searchable() {
			return nil
		}
		return m.bar.ActivateSearch()
	case ".":
		m.CycleSort()
	// No bare letter is navigation (§3.26). The vim aliases h/j/k/l/g/G are
	// gone application-wide: home/end already covered g/G, and keeping j/k
	// alone would leave the one exception that makes the rule unverifiable.
	// The letters now belong to the action vocabulary — internal/ui/keymap.
	case "up":
		m.table.MoveUp(1)
		m.afterCursorMove()
	case "down":
		m.table.MoveDown(1)
		m.afterCursorMove()
	case "pgup":
		m.table.MoveUp(m.table.Height())
		m.afterCursorMove()
	case "pgdown":
		m.table.MoveDown(m.table.Height())
		m.afterCursorMove()
	case "home":
		m.table.GotoTop()
		m.afterCursorMove()
	case "end":
		m.table.GotoBottom()
		m.afterCursorMove()
	}
	return nil
}

// ── Rows an action is running on ─────────────────────────────────────────────
//
// A row says two things at once: what the object *is*, and what is *happening*
// to it. They have different sources of truth — the state comes back from
// docker on the next refresh, the transition is this application's own
// knowledge — and different lifetimes. Sharing one glyph is the point; sharing
// one function would make every table re-implement the precedence, so the rule
// lives here and there is one of it: **busy wins over state**.

// MarkBusy records that an action is running on the item with this key, and what
// to call it. Call it from Update, never from a Cmd (Rule 110).
//
// A no-op when the config declares no Key: without one there is nothing to
// match a row against, and silently keeping a label nothing can display would
// be worse than ignoring it.
func (m *Model[T]) MarkBusy(key, label string) {
	if m.cfg.Key == nil || key == "" {
		return
	}
	if m.busy == nil {
		m.busy = make(map[string]string, 1)
	}
	m.busy[key] = label
}

// ClearBusy forgets an action.
//
// It must be called on **every** outcome, the failures included. Clearing only
// on success leaves the row spinning for the life of the view, and — worse —
// hides the state it still has: a `docker stop` that failed has to read
// `running` again, not go on turning.
func (m *Model[T]) ClearBusy(key string) { delete(m.busy, key) }

// IsBusy reports whether an action is running on this key.
//
// It answers for an object whose row does not exist yet, which is what makes it
// the guard on a confirm path: without it a second keypress on a slow removal
// issues a second command, and the second one fails with "no such object" on an
// operation that in fact worked.
func (m *Model[T]) IsBusy(key string) bool {
	_, ok := m.busy[key]
	return ok
}

// BusyLabels returns what is running, sorted, for a view's footer line. Sorted
// because a map's order changes between frames and a footer that reshuffles
// itself is unreadable.
func (m *Model[T]) BusyLabels() []string {
	if len(m.busy) == 0 {
		return nil
	}
	out := make([]string, 0, len(m.busy))
	for _, label := range m.busy {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

// AdvanceSpinner moves the frame shown on busy rows one step on. Views call it
// from the spinner tick they already run.
func (m *Model[T]) AdvanceSpinner() {
	m.spinnerFrame = spinner.Dot.Frames[m.spinnerIdx%len(spinner.Dot.Frames)]
	m.spinnerIdx++
}

// busyLabel returns the action running on an item, and whether there is one.
func (m *Model[T]) busyLabel(item T) (string, bool) {
	if m.cfg.Key == nil || len(m.busy) == 0 {
		return "", false
	}
	label, ok := m.busy[m.cfg.Key(item)]
	return label, ok
}

// SetTokenActive toggles one of the bar's filter tokens and re-applies it.
func (m *Model[T]) SetTokenActive(label string, active bool) {
	m.bar.SetTokenActive(label, active)
	m.rebuild()
	m.Remeasure() // a toggle is a user action on a settled population
}

// IsTokenActive reports whether a toggle filter is on.
func (m *Model[T]) IsTokenActive(label string) bool { return m.bar.IsTokenActive(label) }

// rebuild filters, sorts, writes the rows and puts the cursor somewhere real.
func (m *Model[T]) rebuild() {
	m.visible = m.sorted(m.filtered())

	// The natural widths are taken here because the cells are already being
	// built: the measurement is a lipgloss.Width per cell on a walk that
	// happens anyway. Promoting them to the solver is a separate decision
	// (see Remeasure) — this only keeps the answer current.
	//
	// The header counts. A content column narrower than its own title truncates
	// the one cell that says what the column is, and "the width the content
	// wants" plainly includes being able to name itself.
	m.natural = make([]int, len(m.cfg.Columns))
	for j, c := range m.cfg.Columns {
		m.natural[j] = lipgloss.Width(c.Title)
	}

	rows := make([]table.Row, len(m.visible))
	for i, item := range m.visible {
		cells := make(table.Row, len(m.cfg.Columns))
		for j, c := range m.cfg.Columns {
			cells[j] = c.Cell(item)
			if w := lipgloss.Width(cells[j]); w > m.natural[j] {
				m.natural[j] = w
			}
		}
		rows[i] = cells
	}
	m.table.SetRows(rows)

	// bubbles/table leaves the cursor where it was, so a list that just got
	// shorter strands it past the end until the user presses a key. It also
	// reports -1 while there are no rows at all, and that has to be taken back
	// when rows return — a filter typed past its last match and then corrected
	// would otherwise leave the table showing rows none of which is selected.
	switch cursor := m.table.Cursor(); {
	case len(rows) == 0:
		// -1 is bubbles' own "nothing selected", and what it reports for a
		// table that never had rows. Leaving the old index would make an empty
		// list the one state where the cursor points past the end.
		m.table.SetCursor(-1)
	case cursor >= len(rows):
		m.table.SetCursor(len(rows) - 1)
	case cursor < 0:
		m.table.SetCursor(0)
	}
	m.afterCursorMove()
}

// afterCursorMove is what every cursor, height and item change ends with: the
// styles follow the selected item, and the scroll window follows the cursor.
// Keeping them on one call is what stops a new call site from remembering one
// and forgetting the other — the offset was invisible while bubbles owned it.
func (m *Model[T]) afterCursorMove() {
	m.clampOffset()
	m.applyStyles()
}

// applyStyles asks the view for the styles that go with the selected item.
func (m *Model[T]) applyStyles() {
	if !m.table.Focused() {
		return // Blur owns the styles until Focus takes them back
	}
	styles := theme.DefaultTableStyles()
	if item, ok := m.Selected(); ok {
		if m.cfg.SelectedStyles != nil {
			styles = m.cfg.SelectedStyles(item)
		}
		// Busy wins over whatever the view asked for, and this is the one place
		// it can be said: the highlight is applied to the joined row, so a
		// colour inside it would close with a reset and end the highlight
		// mid-row (see render.go). An `exited` container being removed is not
		// exited-and-that-is-that — the state is what the action is about to
		// change, so the transition is the newer fact.
		if _, busy := m.busyLabel(item); busy {
			styles.Selected = theme.TableStylesForState("busy").Selected
		}
	}
	// The background is the signal: only the plain "normal" look carries
	// ColorSeverityLow, so this is true exactly when no view-level state or
	// busy override took over the row — see cellStyle for what it changes.
	m.preserveColumnColors = styles.Selected.GetBackground() == theme.ColorSeverityLow
	// Pin the selected row to the full content width. Column widths are counted
	// in cells, and a Nerd Font icon does not always render as wide as it
	// counts, so the highlight otherwise stops short of the right border by
	// however much the row's icons disagreed. Padding to a width the row can
	// never exceed is a no-op when they agree.
	if m.width > 2 {
		styles.Selected = styles.Selected.Width(m.width - 2)
	}
	m.setStyles(styles)
}

// setStyles keeps bubbles' copy and this package's in step. The rendering reads
// the one here (render.go); bubbles' is still what Table() reports, which the
// tests and any view reaching for the raw table expect to be current.
func (m *Model[T]) setStyles(styles table.Styles) {
	m.styles = styles
	m.table.SetStyles(styles)
}

// filtered applies the search query and the toggle filters.
func (m *Model[T]) filtered() []T {
	query := strings.ToLower(strings.TrimSpace(m.bar.SearchQuery()))
	active := m.activeTokens()

	out := make([]T, 0, len(m.items))
	for _, item := range m.items {
		if m.cfg.TokenMatch != nil && !m.cfg.TokenMatch(item, active) {
			continue
		}
		if query != "" && !m.matchesQuery(item, query) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (m *Model[T]) activeTokens() map[string]bool {
	active := make(map[string]bool, len(m.cfg.Tokens))
	for _, tok := range m.cfg.Tokens {
		if m.bar.IsTokenActive(tok.Label) {
			active[tok.Label] = true
		}
	}
	return active
}

// matchesQuery tests the query against each searchable column and then against
// the row as a whole.
//
// The row-level pass is what keeps a query spanning two fields — "tcp 22" over a
// ports list — working: netdiag matched against a joined haystack while the
// other four filter loops matched per field, and per field alone would have
// quietly dropped those matches on migration. Matching both ways is a superset
// of either, so no view loses anything and they all gain the same thing.
func (m *Model[T]) matchesQuery(item T, query string) bool {
	var joined strings.Builder
	for _, c := range m.cfg.Columns {
		if c.Search == nil {
			continue
		}
		value := c.Search(item)
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
		if joined.Len() > 0 {
			joined.WriteByte(' ')
		}
		joined.WriteString(value)
	}
	return strings.Contains(strings.ToLower(joined.String()), query)
}

// sorted orders the visible slice. SliceStable, because sort.Slice is not and
// two rows equal on the sort column would otherwise swap between rebuilds.
func (m *Model[T]) sorted(items []T) []T {
	if m.sortColumn < 0 {
		return items
	}
	less := m.cfg.Columns[m.sortColumn].Less // never nil — see New
	desc := m.sortDesc
	sort.SliceStable(items, func(i, j int) bool {
		if desc {
			return less(items[j], items[i])
		}
		return less(items[i], items[j])
	})
	return items
}
