package explorer

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The clone list is the item level of a clone run (D3).
//
// It used to keep its own five states, its own rows, its own index and its own
// spinner frame — a fifth bookkeeping beside the four internal/jobs replaced,
// and the one the plan named as the prototype to generalise. Those five states
// are where jobs.ItemState came from, so the mapping is direct:
//
//	queued        → ItemQueued
//	cloning       → ItemRunning
//	cloned        → ItemDone
//	already there → ItemSkipped
//	failed        → ItemFailed
//
// What this screen is now is a **rendering** of the run: the rows are the
// snapshot's items, in the order the walk found them, and the frame is the
// router's (D5). Nothing here is authoritative, which is what lets `:jobs` show
// the same run without either copy drifting from the other.

// cloneRow is one target of the run, with the shared frame stamped on.
//
// The frame rides on the row because the columns are built once and close over
// nothing — the same reason jobsview has runRow. It is text rather than a
// rendered spinner because a table cell must carry no escape sequence
// (Rule 122).
type cloneRow struct {
	item  jobs.Item
	frame string
}

// cloneList is the ModeCloning screen: the run in flight and the table showing
// it. It is the progress view and the report both — decision 9 leaves modals
// for yes/no confirmations only.
type cloneList struct {
	table datatable.Model[cloneRow]

	// run is the last snapshot of this screen's run. It is a copy, replaced
	// whole on every broadcast — there is nothing to reconcile, which is what
	// removed the index and the upsert.
	run    jobs.Run
	target string

	// events is the pipeline's channel, and the only thing this struct still
	// owns. It is not state: it is what waitForCloneEvent blocks on.
	events <-chan cloneEvent

	// cancelling is set by `esc` and cleared by nothing: a run that is stopping
	// cannot be restarted, only awaited. A second `esc` must not force, because
	// forcing is the half-cloned directory decision 12 avoids.
	//
	// It is kept here rather than read back from the run for one frame's worth
	// of honesty: `esc` asks the router, and the answer arrives in the next
	// broadcast. Without it the key would look ignored until then.
	cancelling bool
}

const (
	colCloneStatusMin = 16
	colClonePathMin   = 30
	colCloneDetailMin = 20
)

// The column order, named so a test asserting on a cell does not hard-code the
// position it happens to sit at today.
const (
	colCloneRepository = iota
	colCloneDetail
	colCloneStatus
)

func newCloneList(target string, events <-chan cloneEvent) *cloneList {
	return &cloneList{
		target: target,
		events: events,
		table: datatable.New(datatable.Config[cloneRow]{
			Columns: cloneColumns(),
			// Discovery order, deliberately: it is the order things happened, and
			// a list that reorders itself while it fills is unreadable. `.` still
			// sorts on demand.
			SortColumn: -1,
		}),
	}
}

// cloneColumns describes the clone list.
//
// The repository comes first and the status last. The rows are one long list of
// paths under a common prefix, so the name is what the eye runs down to find a
// line; a status column on the left pushes every one of them right by sixteen
// cells and puts the changing text where the stable text should be.
func cloneColumns() []datatable.Column[cloneRow] {
	return []datatable.Column[cloneRow]{
		{
			Title: "Repository", Sizing: datatable.SizingContent, MinWidth: colClonePathMin, Flex: 2, TruncateHead: true,
			Cell:   func(r cloneRow) string { return r.item.Name() },
			Less:   func(a, b cloneRow) bool { return strings.ToLower(a.item.Name()) < strings.ToLower(b.item.Name()) },
			Search: func(r cloneRow) string { return r.item.Name() },
		},
		{
			Title: "Detail", Sizing: datatable.SizingContent, Optional: true, MinWidth: colCloneDetailMin, Flex: 1,
			Cell:   func(r cloneRow) string { return r.item.Detail },
			Search: func(r cloneRow) string { return r.item.Detail },
		},
		{
			Title: "Status", Sizing: datatable.SizingFixed, MinWidth: colCloneStatusMin,
			Cell:  cloneStatusLabel,
			Style: cloneStatusStyle,
			Less:  func(a, b cloneRow) bool { return a.item.State < b.item.State },
		},
	}
}

// cloneStatusLabel is the status cell: an icon and a word, plain text.
//
// The words are the clone's, not the registry's: "already there" is what a skip
// means here, where a sync's skip means something else entirely. The state is
// shared, the wording is the view's — the same split Reporter draws.
func cloneStatusLabel(r cloneRow) string {
	switch r.item.State {
	case jobs.ItemRunning:
		return r.frame + " cloning"
	case jobs.ItemDone:
		return theme.IconOK + " cloned"
	case jobs.ItemSkipped:
		return theme.IconSkipped + " already there"
	case jobs.ItemFailed:
		return theme.IconError + " failed"
	default:
		return theme.IconPending + " queued"
	}
}

// cloneStatusStyle colours the status cell.
//
// The five states are the whole vocabulary of this screen, and they are the one
// thing a run of forty rows is read for: which ones failed, which are still
// going, which were already on disk and were left alone.
func cloneStatusStyle(r cloneRow) lipgloss.Style {
	switch r.item.State {
	case jobs.ItemDone:
		return theme.StatusOKStyle
	case jobs.ItemFailed:
		return theme.StatusErrorStyle
	case jobs.ItemRunning:
		return lipgloss.NewStyle().Foreground(theme.ColorHighlight)
	default: // queued, and already there — neither is news
		return theme.DimStyle
	}
}

// setRun replaces the screen's copy of the run and rebuilds the table.
//
// The frame is stamped on the running rows here rather than held on each item,
// because it belongs to the list and not to any repository: one spinner, on
// every row that is going.
func (l *cloneList) setRun(run jobs.Run, frame string) {
	l.run = run
	items := make([]cloneRow, len(run.Items))
	for i, item := range run.Items {
		row := cloneRow{item: item}
		if item.State == jobs.ItemRunning {
			row.frame = frame
		}
		items[i] = row
	}
	l.table.SetItems(items)
}

// finished reports whether the run has settled: the walk sealed, and every
// target done with.
func (l *cloneList) finished() bool { return l.run.Finished() }

// running counts the clones still in flight, which is what `esc` has to wait
// for and what the view states while it does.
func (l *cloneList) running() int {
	n := 0
	for _, item := range l.run.Items {
		if item.State == jobs.ItemRunning {
			n++
		}
	}
	return n
}

// counts is the header line: what has been found and how it went.
func (l *cloneList) counts() (found, cloned, present, failed int) {
	c := l.run.Counts()
	return l.run.Total(), c[jobs.ItemDone], c[jobs.ItemSkipped], c[jobs.ItemFailed]
}

// failures returns the failed paths, for the log and the footer. Decision 13
// discards the list on `esc`, and workspaces records only the successes — a
// failure that wrote nothing leaves nothing behind, so it has to be said while
// the view is still alive.
func (l *cloneList) failures() []string {
	var out []string
	for _, item := range l.run.Items {
		if item.State == jobs.ItemFailed {
			out = append(out, item.Target)
		}
	}
	return out
}

// cloneRunFrom picks this screen's run out of the snapshot.
//
// The newest clone run, because there is exactly one at a time: ModeCloning
// owns the display while it goes, so a second could only start after this one
// has been closed. It is the invariant Registry.openRun rests on, read from the
// other side.
//
// A settled run still matches, which is what the report needs: the screen stays
// up after the walk has sealed, and `esc` is what finally drops it.
func cloneRunFrom(runs []jobs.Run) (jobs.Run, bool) {
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Kind == jobs.KindClone {
			return runs[i], true
		}
	}
	return jobs.Run{}, false
}
