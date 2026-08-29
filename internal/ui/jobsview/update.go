package jobsview

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
)

// Init returns nothing.
//
// The view fetches nothing and starts no chain: its rows arrive in the
// broadcast, and switchView hands it the snapshot on the way in (sendJobsTo).
// A view that asked on arrival would be reading a second answer to a question
// the router already holds.
func (m Model) Init() tea.Cmd { return nil }

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case jobs.ChangedMsg:
		return m.handleJobsChanged(msg)

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}

	m.footer.Handle(msg)
	return m, nil
}

// handleJobsChanged takes the router's snapshot and rebuilds both tables.
//
// Both, not just the one on screen: the item table is what the cursor stands in
// while a target settles under it, and rebuilding it only on the way in would
// freeze it at the moment it was opened.
func (m Model) handleJobsChanged(msg jobs.ChangedMsg) (tea.Model, tea.Cmd) {
	m.runs = msg.Runs
	m.frame = msg.Frame
	m.footer.SetSpinnerFrame(msg.RenderedFrame)
	m.refreshRows()
	return m, nil
}

// visibleRuns is what this view shows: the runs of the current context (D8).
// The table decides the order; this decides which exist.
func (m Model) visibleRuns() []jobs.Run {
	return jobs.FilterContext(m.runs, m.contextName)
}

// openedRun returns the run the item level is showing, and whether it is still
// in the snapshot.
//
// It can stop being: the registry keeps the last MaxFinishedRuns settled runs,
// so a run left open long enough is eventually pruned out from under the
// cursor. refreshRows is what answers for that.
func (m Model) openedRun() (jobs.Run, bool) {
	for _, run := range m.visibleRuns() {
		if run.ID == m.openRun {
			return run, true
		}
	}
	return jobs.Run{}, false
}

// refreshRows rebuilds both tables from the snapshot, and falls back to the
// list when the run being shown has been pruned.
func (m *Model) refreshRows() {
	runs := m.visibleRuns()
	rows := make([]runRow, len(runs))
	for i, run := range runs {
		rows[i] = runRow{Run: run, Frame: m.frame}
	}
	m.runTable.SetItems(rows)

	run, ok := m.openedRun()
	if m.level == levelItems && !ok {
		// Back to the list rather than an empty table: the run is gone, so
		// there is nothing to say about it, and a breadcrumb naming a label
		// that no longer exists would be worse than no breadcrumb.
		m.level = levelRuns
		m.openRun = 0
		m.itemTable.SetItems(nil)
		return
	}

	items := make([]itemRow, len(run.Items))
	for i, item := range run.Items {
		items[i] = itemRow{Item: item, Frame: m.frame}
	}
	m.itemTable.SetItems(items)
}

// handleKeyMsg routes a key. The filter comes first (Rule 136): while it has
// the keyboard, every key is text.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.inFilter() {
		return m.forwardToTable(msg)
	}

	switch msg.String() {
	case "right":
		return m.openSelected()

	case "left", "esc":
		return m.closeItems()

	case keymap.Kill:
		return m.stopSelected()

	case ".":
		return m.cycleSort()

	case "/":
		return m.activateSearch()
	}

	return m.forwardToTable(msg)
}

// forwardToTable hands the key to whichever level is on screen.
//
// The two tables are datatable.Model over different types, so this is a branch
// rather than an interface: a common interface would have to be declared over
// FilterBar and Update in a package that owns neither, for two call sites.
func (m Model) forwardToTable(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.level == levelItems {
		return m, m.itemTable.Update(msg)
	}
	return m, m.runTable.Update(msg)
}

// stopSelected asks the router to stop what the cursor is on: the run at the
// top level, one target inside a run.
//
// It asks rather than acts, like every other write to the registry: the router
// owns it, and a view holding a pointer to it is the thing D1 keeps out. What
// the view holds is the identifier, read off the row it is rendering.
func (m Model) stopSelected() (tea.Model, tea.Cmd) {
	if stop := m.availability().Stop; !stop.Enabled() {
		return m, m.footer.Warn(stop.Reason)
	}
	if m.level == levelItems {
		row, _ := m.itemTable.Selected()
		return m, jobs.CancelItem(m.openRun, row.Item.Target)
	}
	row, _ := m.runTable.Selected()
	return m, jobs.Cancel(row.Run.ID)
}

// cycleSort moves the runs table on to its next sortable column.
//
// It is guarded rather than forwarded, so the refusal and the greying come from
// one calculation (Rule 130): the item table declares no comparator, and a `.`
// that silently did nothing there would be the `return m, nil` the rule exists
// to remove.
func (m Model) cycleSort() (tea.Model, tea.Cmd) {
	if sort := m.availability().Sort; !sort.Enabled() {
		return m, m.footer.Warn(sort.Reason)
	}
	m.runTable.CycleSort()
	return m, nil
}

func (m Model) activateSearch() (tea.Model, tea.Cmd) {
	if m.level == levelItems {
		return m, m.itemTable.FilterBar().ActivateSearch()
	}
	return m, m.runTable.FilterBar().ActivateSearch()
}

// filterBar is the bar of the level on screen. The footer renders it and
// GetFooterHeight budgets for it (Rule 136).
func (m Model) filterBar() *sharedcomponents.FilterBar {
	if m.level == levelItems {
		return m.itemTable.FilterBar()
	}
	return m.runTable.FilterBar()
}

func (m Model) inFilter() bool { return m.filterBar().InEditMode() }

// InEditMode tells the router the filter has the keyboard (app.FormView).
func (m Model) InEditMode() bool { return m.inFilter() }

// FilterBarVisible closes the viewport rectangle around the bar
// (app.FilterBarView).
func (m Model) FilterBarVisible() bool { return m.filterBar().IsVisible() }

// openSelected drills into the run under the cursor (Rule 111: → descends).
func (m Model) openSelected() (tea.Model, tea.Cmd) {
	if open := m.availability().Open; !open.Enabled() {
		return m, m.footer.Warn(open.Reason)
	}
	row, _ := m.runTable.Selected()
	m.openRun = row.Run.ID
	m.level = levelItems
	m.itemTable.GotoTop()
	m.refreshRows()
	m.resize(m.width, m.height)
	return m, nil
}

// closeItems goes back up.
//
// At the top level there is nowhere to go, and that is not a refusal worth
// naming: `esc` is what a user presses to leave a screen, this view has none
// behind it, and a footer line on every stray esc would be noise. Rule 130
// greys the entry instead, which says the same thing before the key is pressed.
func (m Model) closeItems() (tea.Model, tea.Cmd) {
	if m.level == levelRuns {
		return m, nil
	}
	m.level = levelRuns
	m.openRun = 0
	m.resize(m.width, m.height)
	return m, nil
}

// resize lays both tables out.
//
// Both, again: the item table is measured for the size it will be opened at, so
// → cannot show one frame of a table sized for a window that has since changed.
//
// The breadcrumb costs a line at the item level (Rule 123), and it is not
// subtracted here: it is rendered in the footer, so GetFooterHeight already
// asks the router for it, and taking it off the table too would take it twice.
func (m *Model) resize(width, height int) {
	m.width = width
	m.height = height

	m.runTable.Resize(width, max(height-1, 1))
	m.itemTable.Resize(width, max(height-1, 1))
}
