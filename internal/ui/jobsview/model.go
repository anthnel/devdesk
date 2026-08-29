// Package jobsview is the `:jobs` screen: what is running, and what has run
// this session.
//
// It owns no work and starts none. Every row comes from the snapshot the router
// broadcasts (jobs.ChangedMsg), which is what lets this view exist at all — a
// screen that had to ask each view what it was doing would be the fifth
// bookkeeping internal/jobs removed.
package jobsview

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// level is which of the two tables is on screen (D2).
//
// A level rather than a tab: the second table is *about* a row of the first, so
// it is reached with → and left with ← or esc, the way every drill-down in the
// application is (Rule 111). Tabs would say the two are siblings.
type level int

const (
	levelRuns level = iota
	levelItems
)

// runRow and itemRow carry the spinner frame onto the row.
//
// The frame changes ten times a second and the columns are built once, in New,
// closing over nothing — so a Cell cannot reach the model to ask for it. Putting
// it on the row is what oci_resources does with imageRow, and for the same
// reason.
//
// It is the router's frame (D5): the one chain, taken bare from jobs.ChangedMsg
// because a table cell is measured before it is styled (Rule 122). Using
// datatable's own AdvanceSpinner here would be a second chain beside it.
type runRow struct {
	Run   jobs.Run
	Frame string
}

type itemRow struct {
	Item  jobs.Item
	Frame string
}

// Model is the jobs view.
type Model struct {
	// runs is the snapshot, every context included. contextName is what the
	// tables filter it by: a run is kept for the session and stamped, so a
	// context switch hides its runs rather than dropping them (D8).
	runs        []jobs.Run
	contextName string

	// frame is the router's bare spinner frame.
	frame string

	level   level
	openRun jobs.JobID

	runTable  datatable.Model[runRow]
	itemTable datatable.Model[itemRow]

	footer sharedcomponents.FooterMessage

	width, height int
}

// Column indexes of the runs table.
//
// Only runColumnStarted is referred to — it is what the table opens sorted by —
// and the rest of the block is what makes it right: written as `= 5` it would
// go on compiling, and start naming another column, the day one is inserted
// above it. That is the failure the containers table records in the note above
// columnPorts.
const (
	runColumnState = iota
	runColumnKind
	runColumnLabel
	runColumnProgress
	runColumnStateText
	runColumnStarted
)

// New builds the view. The context comes from the router.
//
// It is passed rather than read back from the configuration, for D68's reason
// one screen over: the router is what knows which context is current, and a
// view reading it again would be a second answer to a settled question. The
// router also drops every view on a switch (reinitializeViews), so the name
// cannot go stale.
func New(_ *config.Config, contextName string) Model {
	return Model{
		contextName: contextName,
		runTable: datatable.New(datatable.Config[runRow]{
			Columns: runColumns(),
			// Newest first. A jobs list is read to find out what is happening
			// now, and ascending would pin the session's first run to the top
			// for the rest of the session.
			SortColumn: runColumnStarted,
			SortDesc:   true,
		}),
		itemTable: datatable.New(datatable.Config[itemRow]{
			Columns: itemColumns(),
			// The order the batch was dispatched in. It is the only order that
			// means anything here — the targets of a run have no natural rank,
			// and sorting by state would move a row out from under the cursor
			// every time one settled.
			SortColumn: -1,
		}),
	}
}

// runColumns describes the runs table.
//
// The first column is the run's **state**, not its kind, and it is also where
// the spinner goes. That is the containers table's shape and it is deliberate:
// a jobs list is scanned to find the one that failed and the one still going,
// so the glyph answers the question the reader arrived with, and the Kind
// column says in a word what sort of work it was.
//
// A state glyph column colours by the semantic status styles rather than by a
// theme.IconStyle role, which is the choice the containers table already makes.
// Roles exist for icons that name an *object* — a namespace, a repository, an
// image — where a palette should be able to tell them apart. A state has a
// colour already, and five new roles would give a theme five ways to make
// "failed" not red.
//
// The glyph and the State word are not the redundancy that a glyph beside its
// own name usually is: the glyph column doubles as the spinner while the run
// goes, and the word is what separates the four settled outcomes — "cancelled"
// from "failed" above all, which is the distinction Run.cancelled exists for.
func runColumns() []datatable.Column[runRow] {
	return []datatable.Column[runRow]{
		{
			Title: "", Sizing: datatable.SizingFixed, MinWidth: datatable.IconColumnWidth,
			Cell:  func(r runRow) string { return runStateGlyph(r) },
			Style: func(r runRow) lipgloss.Style { return runStateStyle(r.Run.State()) },
		},
		{
			Title: "Kind", Sizing: datatable.SizingFixed, MinWidth: 8,
			Cell: func(r runRow) string { return string(r.Run.Kind) },
			Less: func(a, b runRow) bool { return a.Run.Kind < b.Run.Kind },
			// The state is searchable here rather than through its own column,
			// so `/failed` and `/scan` both work while the glyph column stays
			// out of the filter (Rule 125).
			Search: func(r runRow) string { return string(r.Run.Kind) + " " + string(r.Run.State()) },
		},
		{
			Title: "Label", Sizing: datatable.SizingContent,
			MinWidth: 14, MaxWidth: 40, Flex: 2, TruncateHead: true,
			Cell:   func(r runRow) string { return r.Run.Label },
			Less:   func(a, b runRow) bool { return strings.ToLower(a.Run.Label) < strings.ToLower(b.Run.Label) },
			Search: func(r runRow) string { return r.Run.Label },
		},
		{
			Title: "Progress", Sizing: datatable.SizingFixed, MinWidth: 9,
			Cell: func(r runRow) string { return progressCell(r.Run) },
			// By how far along, not by how many: 1/2 is further than 3/12, and
			// comparing the numerators would rank the bigger batch higher for
			// having more left to do.
			Less: func(a, b runRow) bool { return fraction(a.Run) < fraction(b.Run) },
		},
		{
			Title: "State", Sizing: datatable.SizingFixed, MinWidth: 10,
			Cell:  func(r runRow) string { return string(r.Run.State()) },
			Style: func(r runRow) lipgloss.Style { return runStateStyle(r.Run.State()) },
			Less:  func(a, b runRow) bool { return a.Run.State() < b.Run.State() },
		},
		{
			Title: "Started", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 12,
			Cell: func(r runRow) string { return theme.TimeAgo(r.Run.StartedAt) },
			Less: func(a, b runRow) bool { return a.Run.StartedAt.Before(b.Run.StartedAt) },
		},
	}
}

// itemColumns describes one run's targets.
//
// No Started column: an item has no timestamp of its own, and every target of a
// batch shares the run's. What an item has that a run does not is Detail — the
// reason a sync was skipped, the fact a scan failed — and that is what the
// width is worth spending on here.
func itemColumns() []datatable.Column[itemRow] {
	return []datatable.Column[itemRow]{
		{
			Title: "", Sizing: datatable.SizingFixed, MinWidth: datatable.IconColumnWidth,
			Cell:  func(r itemRow) string { return itemStateGlyph(r) },
			Style: func(r itemRow) lipgloss.Style { return itemStateStyle(r.Item.State) },
		},
		{
			Title: "Target", Sizing: datatable.SizingContent,
			MinWidth: 20, MaxWidth: 60, Flex: 3, TruncateHead: true,
			Cell:   func(r itemRow) string { return r.Item.Name() },
			Search: func(r itemRow) string { return r.Item.Name() },
		},
		{
			Title: "State", Sizing: datatable.SizingFixed, MinWidth: 10,
			Cell:   func(r itemRow) string { return string(r.Item.State) },
			Style:  func(r itemRow) lipgloss.Style { return itemStateStyle(r.Item.State) },
			Search: func(r itemRow) string { return string(r.Item.State) },
		},
		{
			Title: "Detail", Sizing: datatable.SizingContent, MinWidth: 10, Flex: 2,
			Cell:   func(r itemRow) string { return dashIfEmpty(r.Item.Detail) },
			Style:  func(r itemRow) lipgloss.Style { return detailStyle(r.Item) },
			Search: func(r itemRow) string { return r.Item.Detail },
		},
	}
}

// progressCell is the "7/12" of a run.
//
// A settled run keeps it. "12/12" beside "3/12" is what makes the size of a
// batch readable at a glance, and blanking it once the run is over would empty
// the column for most of the list.
func progressCell(r jobs.Run) string {
	return strconv.Itoa(r.Done()) + "/" + strconv.Itoa(r.Total())
}

// fraction is how far along a run is, in [0,1]. An empty run is finished.
func fraction(r jobs.Run) float64 {
	if r.Total() == 0 {
		return 1
	}
	return float64(r.Done()) / float64(r.Total())
}

// dashIfEmpty is the placeholder for a cell with nothing in it — a dash rather
// than a blank, so the column still reads as a column (Rule 122).
func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// detailStyle dims the placeholder and leaves a real detail alone.
//
// "already there" or "scan failed — check logs" is what the column exists for,
// so dimming it would grey out exactly what the reader came to read. What is
// dim is the absence, which is Rule 122's rule for a zero.
func detailStyle(i jobs.Item) lipgloss.Style {
	if i.Detail == "" {
		return theme.DimStyle
	}
	return lipgloss.NewStyle()
}
