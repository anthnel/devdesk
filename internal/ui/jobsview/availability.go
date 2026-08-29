package jobsview

import (
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// The reasons a key is refused, as named constants: the header reads them to
// grey, the handler reads them to refuse, and a test reads them to check the
// two agree (Rule 130).
const (
	reasonNoRun         = "No run selected"
	reasonNoTarget      = "No target selected"
	reasonEmptyRun      = "This run has no targets"
	reasonInsideRun     = "Already inside a run"
	reasonTopLevel      = "Already at the top level"
	reasonItemsUnsorted = "Targets keep the order they were dispatched in"
	reasonRunSettled    = "This run has already finished"
	reasonTargetSettled = "This target has already finished"
)

// stopRefusal names why a run cannot be stopped, in the run's own words.
//
// The wording is per kind because the reason is: a delete in flight must never
// be cut, and saying so with the verb the row already shows is what makes the
// greyed key readable without opening the help.
func stopRefusal(kind jobs.Kind) string {
	return "A " + string(kind) + " cannot be stopped once it has started"
}

// availability is what the selected row and the current level permit, computed
// once and read by both halves of the view.
//
// One field per key rather than a bool and a string apart: an empty reason
// means available, so the two cannot disagree.
type availability struct {
	Open  shortcut.Availability
	Close shortcut.Availability
	Sort  shortcut.Availability
	Stop  shortcut.Availability
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
		a.Stop = m.itemStoppable()
		return a
	}

	a.Close = shortcut.Unavailable(reasonTopLevel)
	switch row, ok := m.runTable.Selected(); {
	case !ok:
		a.Open = shortcut.Unavailable(reasonNoRun)
		a.Stop = shortcut.Unavailable(reasonNoRun)
	case row.Run.Total() == 0:
		// A run with no targets is a real thing: a batch launched when every
		// candidate was already busy. It settles at once, and there is nothing
		// behind it to open.
		a.Open = shortcut.Unavailable(reasonEmptyRun)
		a.Stop = m.runStoppable(row.Run)
	default:
		a.Stop = m.runStoppable(row.Run)
	}
	return a
}

// runStoppable answers for `K` on a run: the queue stops whatever the kind, so
// what is left to say is whether there is anything left to stop (D7).
func (m Model) runStoppable(run jobs.Run) shortcut.Availability {
	switch {
	case run.Finished():
		return shortcut.Unavailable(reasonRunSettled)
	case !run.Stoppable():
		return shortcut.Unavailable(stopRefusal(run.Kind))
	}
	return shortcut.Availability{}
}

// itemStoppable answers for `K` on one target.
//
// Narrower than the run, and deliberately: stopping a run stops its queue, but
// cutting a single piece of work in flight is only offered where cutting leaves
// nothing behind. A half-written clone is the case that decides it.
func (m Model) itemStoppable() shortcut.Availability {
	run, ok := m.openedRun()
	if !ok {
		return shortcut.Unavailable(reasonNoRun)
	}
	row, ok := m.itemTable.Selected()
	if !ok {
		return shortcut.Unavailable(reasonNoTarget)
	}
	switch {
	case row.Item.State.Terminal():
		return shortcut.Unavailable(reasonTargetSettled)
	case !run.ItemStoppable(row.Item):
		return shortcut.Unavailable(stopRefusal(run.Kind))
	}
	return shortcut.Availability{}
}
