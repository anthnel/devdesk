package explorer

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// cloneState is what one row of the clone list is doing.
//
// There are five, and only five, because decision 2 draws the line: the
// explorer creates what is missing and never reconciles what exists. A dirty
// working copy, a diverged branch, a stale remote — none of them is a state
// here, they belong to the workspaces view (§3.17).
type cloneState int

const (
	cloneQueued cloneState = iota
	cloneRunning
	cloneCloned
	cloneAlreadyThere
	cloneFailed
)

// cloneRow is one repository, or one group whose walk failed.
type cloneRow struct {
	path  string
	state cloneState
	// frame is the spinner's current character, stamped on while the row is
	// running. It is text rather than a rendered spinner because a table cell
	// must carry no escape sequences (Rule 122).
	frame  string
	detail string
}

// cloneList is the ModeCloning screen: the pipeline in flight and the table
// showing it. It is the progress view and the report both — decision 9 leaves
// modals for yes/no confirmations only.
type cloneList struct {
	table datatable.Model[cloneRow]
	// rows is the authoritative order: discovery order, which is the order
	// things happened. The table may sort it, but the list does not.
	rows  []cloneRow
	index map[string]int

	run    *cloneRun
	target string
	// frameIdx indexes spinner.Dot.Frames directly rather than reading the
	// view's spinner: spinner.Model.View() renders through its style, and a
	// styled string in a table cell bleeds over every row below it (Rule 122).
	frameIdx int

	// cancelling is set by `esc` and cleared by nothing: a run that is stopping
	// cannot be restarted, only awaited. A second `esc` must not force, because
	// forcing is the half-cloned directory decision 12 avoids.
	cancelling bool
	finished   bool
}

const (
	colCloneStatusMin = 16
	colClonePathMin   = 30
	colCloneDetailMin = 20
)

func newCloneList(target string, run *cloneRun) *cloneList {
	return &cloneList{
		target: target,
		run:    run,
		index:  map[string]int{},
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
func cloneColumns() []datatable.Column[cloneRow] {
	return []datatable.Column[cloneRow]{
		{
			Title: "Status", MinWidth: colCloneStatusMin,
			Cell:  func(r cloneRow) string { return cloneStatusLabel(r) },
			Style: cloneStatusStyle,
			Less:  func(a, b cloneRow) bool { return a.state < b.state },
		},
		{
			Title: "Repository", MinWidth: colClonePathMin, Flex: 2,
			Cell:   func(r cloneRow) string { return r.path },
			Less:   func(a, b cloneRow) bool { return strings.ToLower(a.path) < strings.ToLower(b.path) },
			Search: func(r cloneRow) string { return r.path },
		},
		{
			Title: "Detail", MinWidth: colCloneDetailMin, Flex: 1,
			Cell:   func(r cloneRow) string { return r.detail },
			Search: func(r cloneRow) string { return r.detail },
		},
	}
}

// cloneStatusLabel is the status cell: an icon and a word, plain text.
func cloneStatusLabel(r cloneRow) string {
	switch r.state {
	case cloneRunning:
		return r.frame + " cloning"
	case cloneCloned:
		return theme.IconOK + " cloned"
	case cloneAlreadyThere:
		return theme.IconSkipped + " already there"
	case cloneFailed:
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
	switch r.state {
	case cloneCloned:
		return theme.StatusOKStyle
	case cloneFailed:
		return theme.StatusErrorStyle
	case cloneRunning:
		return lipgloss.NewStyle().Foreground(theme.ColorHighlight)
	default: // queued, and already there — neither is news
		return theme.DimStyle
	}
}

// apply folds one pipeline event into the list.
//
// An event for a path that has no row yet creates one. That is not defensive
// padding: cloneWalkFailed names a group, which was never found as a
// repository and so has no row to update.
func (l *cloneList) apply(event cloneEvent) {
	switch event.kind {
	case cloneFound:
		l.upsert(event.path, cloneQueued, "")
	case cloneBegan:
		l.upsert(event.path, cloneRunning, "")
	case cloneEnded:
		switch {
		case event.err != nil:
			l.upsert(event.path, cloneFailed, event.err.Error())
		case event.skipped:
			l.upsert(event.path, cloneAlreadyThere, "")
		default:
			l.upsert(event.path, cloneCloned, "")
		}
	case cloneWalkFailed:
		l.upsert(event.path, cloneFailed, "discovery: "+event.err.Error())
	}
	l.sync()
}

func (l *cloneList) upsert(path string, state cloneState, detail string) {
	if i, ok := l.index[path]; ok {
		l.rows[i].state = state
		l.rows[i].detail = detail
		return
	}
	l.index[path] = len(l.rows)
	l.rows = append(l.rows, cloneRow{path: path, state: state, detail: detail})
}

// advance moves the spinner shown on the running rows one frame on.
func (l *cloneList) advance() {
	l.frameIdx = (l.frameIdx + 1) % len(spinner.Dot.Frames)
	l.sync()
}

// sync hands the table a copy with the current spinner frame stamped on.
//
// The frame is stamped here rather than held on the row because it belongs to
// the list, not to any repository: one spinner, on every row that is running.
func (l *cloneList) sync() {
	frame := spinner.Dot.Frames[l.frameIdx%len(spinner.Dot.Frames)]
	items := make([]cloneRow, len(l.rows))
	copy(items, l.rows)
	for i := range items {
		if items[i].state == cloneRunning {
			items[i].frame = frame
		}
	}
	l.table.SetItems(items)
}

// running counts the clones still in flight, which is what `esc` has to wait
// for and what the view states while it does.
func (l *cloneList) running() int {
	n := 0
	for _, r := range l.rows {
		if r.state == cloneRunning {
			n++
		}
	}
	return n
}

// counts is the header line: what has been found and how it went.
func (l *cloneList) counts() (found, cloned, present, failed int) {
	for _, r := range l.rows {
		found++
		switch r.state {
		case cloneCloned:
			cloned++
		case cloneAlreadyThere:
			present++
		case cloneFailed:
			failed++
		case cloneQueued, cloneRunning:
		}
	}
	return found, cloned, present, failed
}

// failures returns the failed paths, for the log and the footer. Decision 13
// discards the list on `esc`, and workspaces records only the successes — a
// failure that wrote nothing leaves nothing behind, so it has to be said while
// the view is still alive.
func (l *cloneList) failures() []string {
	var out []string
	for _, r := range l.rows {
		if r.state == cloneFailed {
			out = append(out, r.path)
		}
	}
	return out
}
