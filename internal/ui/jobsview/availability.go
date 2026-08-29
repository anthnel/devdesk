package jobsview

import "github.com/anthnel/devdesk/internal/ui/shortcut"

// The reasons a key is refused, as named constants: the header reads them to
// grey, the handler reads them to refuse, and a test reads them to check the
// two agree (Rule 130).
const (
	reasonNoRun         = "No run selected"
	reasonEmptyRun      = "This run has no targets"
	reasonInsideRun     = "Already inside a run"
	reasonTopLevel      = "Already at the top level"
	reasonItemsUnsorted = "Targets keep the order they were dispatched in"
)

// availability is what the selected row and the current level permit, computed
// once and read by both halves of the view.
//
// One field per key rather than a bool and a string apart: an empty reason
// means available, so the two cannot disagree.
type availability struct {
	Open  shortcut.Availability
	Close shortcut.Availability
	Sort  shortcut.Availability
}

// availability answers for the level, and inside it for the selected row.
//
// The list is the same three entries at both levels, and only the greying
// changes (Rule 130): a shortcut column that reorganised itself every time the
// reader pressed → would be unreadable exactly when they are moving around.
func (m Model) availability() availability {
	var a availability

	if m.level == levelItems {
		// The drill is one level deep by construction — there is nothing under
		// a target — so → has nowhere left to go, and `.` has no column to
		// cycle because the item table opens unsorted on purpose (see New).
		a.Open = shortcut.Unavailable(reasonInsideRun)
		a.Sort = shortcut.Unavailable(reasonItemsUnsorted)
		return a
	}

	a.Close = shortcut.Unavailable(reasonTopLevel)
	switch row, ok := m.runTable.Selected(); {
	case !ok:
		a.Open = shortcut.Unavailable(reasonNoRun)
	case row.Run.Total() == 0:
		// A run with no targets is a real thing: a batch launched when every
		// candidate was already busy. It settles at once, and there is nothing
		// behind it to open.
		a.Open = shortcut.Unavailable(reasonEmptyRun)
	}
	return a
}
