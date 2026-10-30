package agentusageview

import (
	"errors"
	"log"
	"os"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/agentusage"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case sessionListLoadedMsg:
		return m.handleListLoaded(msg)
	case usageDeltaMsg:
		return m.handleUsageDelta(msg)
	case usagePollTickMsg:
		return m.handlePollTick(msg)
	case agentsResumeMsg:
		return m.handleResume()
	case spinner.TickMsg:
		return m.handleSpinnerTick(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	m.footer.Handle(msg)
	return m, nil
}

// handleResize sizes both tables, so going back from the agents level finds the
// sessions table already fitted. The full viewport width, borders included
// (Rule 116).
func (m Model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width, m.height = msg.Width, max(msg.Height-1, 1)
	m.table.Resize(m.width, m.height)
	m.agentTable.Resize(m.width, m.height)
	return m, nil
}

// handleListLoaded takes a listing, or ignores one from a superseded read.
// A failed read keeps the rows already shown: they are dated, not wrong.
func (m Model) handleListLoaded(msg sessionListLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.gen {
		return m, nil
	}
	m.loading = false
	if msg.err != nil {
		// The error can name a path; it is logged, never shown (Rule 128).
		log.Printf("ERROR [agentusageview] list sessions: %v", msg.err)
		m.loadErr = true
		return m, nil
	}
	m.loadErr = false
	m.root = msg.root
	m.omitted = msg.listing.Omitted
	m.table.SetItems(rowsOf(msg))
	return m, nil
}

// rowsOf folds each project once, so no Cell does it per frame.
func rowsOf(msg sessionListLoadedMsg) []sessionRow {
	rows := make([]sessionRow, len(msg.listing.Sessions))
	for i, s := range msg.listing.Sessions {
		project := noProject
		if s.Project != "" {
			project = theme.FoldHome(s.Project)
		}
		rows[i] = sessionRow{Session: s, project: project}
	}
	return rows
}

// handleSpinnerTick advances the spinner only while a read or a parse is in
// flight, so the chain ends with the load.
func (m Model) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if !m.loading && !m.agentsLoading {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	m.footer.SetSpinnerFrame(m.spinner.View())
	return m, cmd
}

// handleUsageDelta takes the answer to the read in flight, or drops one that
// is for another generation or that nobody is waiting for. A failed read keeps
// the rows already shown.
func (m Model) handleUsageDelta(msg usageDeltaMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.agentGen || m.level != levelAgents || !m.reading || m.agg == nil {
		return m, nil
	}
	m.reading = false
	if msg.err != nil {
		return m.failRead(msg)
	}
	if err := m.agg.Apply(msg.delta); err != nil {
		// Not read from the current cursor: read again from where it is now.
		log.Printf("ERROR [agentusageview] apply delta: %v", err)
		m.reading = true
		return m, readDeltaCmd(m.agentGen, m.agg.Cursor())
	}
	return m.applyRead(msg)
}

// applyRead shows what a successful read brought and decides what comes next.
// The rows are rebuilt on every read, even an empty one: an agent turns idle
// precisely because nothing arrives, so the clock must move with each answer.
// Whether the session is settled is computed once, for the states and for the
// decision to stop polling.
func (m Model) applyRead(msg usageDeltaMsg) (tea.Model, tea.Cmd) {
	m.agentsErr = false
	// While a ctrl+r full read is pending, the session is shown as alive even if
	// the delta just applied would settle it: that state is transient and
	// intended. The full read about to start decides, and a header that flipped
	// to paused for one frame and back would read as a glitch.
	settled := !m.fullPending && m.agg.Activity().Settled(msg.at, quietPeriod)
	m.clock = statusClock{now: msg.at, alive: !settled}
	u := m.usage
	if msg.delta.Full() || !msg.delta.Empty() {
		u = m.agg.Usage()
	}
	m = m.withUsage(u, msg.delta.Full())
	if m.fullPending {
		m.fullPending = false
		return m, m.startRead(true)
	}
	if settled {
		m.pollStopped = true
		return m, nil // nothing left to wait for: the chain ends here
	}
	return m, pollTickCmd(m.agentGen)
}

// failRead records a failed read. Only the first failure of a streak is
// logged. The error names files by base name only; it never reaches the UI.
// A deleted transcript ends the poll chain; any other failure retries, after
// the full read a ctrl+r left pending, if there is one.
func (m Model) failRead(msg usageDeltaMsg) (tea.Model, tea.Cmd) {
	if !m.agentsErr {
		log.Printf("ERROR [agentusageview] read session: %v", msg.err)
	}
	m.agentsErr = true
	if errors.Is(msg.err, os.ErrNotExist) {
		m.agentsLoading, m.fullPending, m.pollStopped = false, false, true
		m.clock = statusClock{now: msg.at}
		return m.withRows(m.usage, false), nil
	}
	if m.fullPending {
		m.fullPending = false
		return m, m.startRead(true)
	}
	m.agentsLoading = false
	return m, pollTickCmd(m.agentGen)
}

// handlePollTick starts the incremental read a tick asks for, unless the tick
// belongs to a superseded chain or a read is already in flight. No spinner:
// polling is silent.
func (m Model) handlePollTick(msg usagePollTickMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.agentGen || m.level != levelAgents || m.agg == nil || m.reading || m.pollStopped {
		return m, nil
	}
	return m, m.startRead(false)
}

// Leave stops the open session's chain when the router switches away: the new
// generation drops any tick or read still in flight, and nothing is awaited
// any more. The session stays open and coming back resumes it (handleResume).
// A pending ctrl+r is kept, and so is a full read lost in flight (a ctrl+r, or
// the open's first read): the resume repeats it as a full read. It never refuses.
func (m Model) Leave() (tea.Model, tea.Cmd, bool) {
	if m.level == levelAgents {
		m.agentGen++
		m.fullPending = m.fullPending || (m.reading && m.agentsLoading)
		m.reading, m.agentsLoading = false, false
	}
	return m, nil, true
}

// handleResume restarts the open session's chain under a new generation, which
// drops the answers of any earlier chain. It reads from the cursor, silently;
// a ctrl+r left pending by Leave becomes a full read, with its spinner.
func (m Model) handleResume() (tea.Model, tea.Cmd) {
	if m.level != levelAgents || m.agg == nil {
		return m, nil
	}
	m.agentGen++
	m.pollStopped = false
	if m.fullPending {
		m.fullPending = false
		return m, tea.Batch(m.startRead(true), m.spinner.Tick)
	}
	return m, m.startRead(false)
}

// startRead marks a read in flight and returns it; full reads everything again.
func (m *Model) startRead(full bool) tea.Cmd {
	c := m.agg.Cursor()
	if full {
		c = c.Rewound()
		m.agentsLoading, m.fullPending, m.pollStopped = true, false, false
	}
	m.reading = true
	return readDeltaCmd(m.agentGen, c)
}

// handleKey gives the filter the keyboard first; ctrl+r refreshes the level on
// screen, → and enter open a session, ← and esc go back.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.InEditMode() {
		return m, m.activeTable(msg)
	}
	key := msg.String()
	if key == "ctrl+r" {
		return m.refresh()
	}
	a := m.availability()
	switch key {
	case "right", "enter":
		return m.guard(a.Open, m.openSelected)
	case "left", "esc":
		// Silent at the top level: a stray esc is not worth a footer line.
		if a.Close.Enabled() {
			return m.closeAgents()
		}
	case ".":
		if m.level == levelAgents {
			return m.cycleAgentSort(msg)
		}
	}
	return m, m.activeTable(msg)
}

// agentsTree reports whether the agents table shows the tree: no sort active.
func (m Model) agentsTree() bool {
	column, _ := m.agentTable.SortState()
	return column < 0
}

// cycleAgentSort hands `.` to the agents table, then rebuilds the rows so the
// branches follow the new order: a sorted list is flat, a branch there would
// lie about who is whose child.
func (m Model) cycleAgentSort(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	selected, hadSelection := m.selectedAgentKey()
	cmd := m.agentTable.Update(msg)
	m.agentTable.SetItems(agentRows(m.usage, m.agentsTree(), m.clock, m.prices))
	m.agentTable.Remeasure() // the Agent column narrows or widens by the branch
	if hadSelection {
		m.selectAgentKey(selected)
	}
	return m, cmd
}

// activeTable hands a key to the table of the level on screen.
func (m *Model) activeTable(msg tea.KeyMsg) tea.Cmd {
	if m.level == levelAgents {
		return m.agentTable.Update(msg)
	}
	return m.table.Update(msg)
}

// openSelected opens the selected session's agents. The table stays empty,
// headers included, until the parse answers (Rule 139).
func (m Model) openSelected() (tea.Model, tea.Cmd) {
	row, _ := m.table.Selected() // availability checked a row exists
	m.usage, m.clock = agentusage.Usage{}, statusClock{}
	m.level = levelAgents
	m.open = row.Session
	m.agentGen++
	m.agg = agentusage.NewAggregate(row.Parent)
	m.fullPending, m.agentsErr = false, false
	m.agentTable.FilterBar().ClearSearch() // a filter typed for another session
	m.agentTable.SetItems(nil)
	return m, tea.Batch(m.startRead(true), m.spinner.Tick)
}

// closeAgents returns to the sessions, cursor and filter as they were. Bumping
// agentGen drops a parse still in flight.
func (m Model) closeAgents() (tea.Model, tea.Cmd) {
	m.level = levelSessions
	m.agentGen++
	m.agentsLoading, m.agentsErr = false, false
	m.agg, m.reading, m.fullPending = nil, false, false
	m.usage, m.clock = agentusage.Usage{}, statusClock{}
	m.agentTable.SetItems(nil)
	return m, nil
}

// refresh starts a new read of the level on screen under a new generation.
// The rows stay on screen.
func (m Model) refresh() (tea.Model, tea.Cmd) {
	if m.level == levelAgents {
		return m.refreshAgents()
	}
	m.gen++
	m.loading = true
	m.loadErr = false
	return m, tea.Batch(listSessionsCmd(m.gen), m.spinner.Tick)
}

// refreshAgents reads the open session from the start. A read already in
// flight is never doubled (one per Aggregate): the full read follows its answer.
func (m Model) refreshAgents() (tea.Model, tea.Cmd) {
	if m.agg == nil {
		m.agg = agentusage.NewAggregate(m.open.Parent)
	}
	if m.reading {
		spin := !m.agentsLoading // a spinner chain already runs otherwise
		m.fullPending, m.agentsLoading = true, true
		if spin {
			return m, m.spinner.Tick
		}
		return m, nil
	}
	m.agentGen++
	return m, tea.Batch(m.startRead(true), m.spinner.Tick)
}

// InEditMode tells the router the filter has the keyboard.
func (m Model) InEditMode() bool { return m.activeFilterBar().InEditMode() }

// FilterBarVisible closes the viewport rectangle around the bar.
func (m Model) FilterBarVisible() bool { return m.activeFilterBar().IsVisible() }

// activeFilterBar is the bar of the level on screen; each level has its own.
func (m Model) activeFilterBar() *sharedcomponents.FilterBar {
	if m.level == levelAgents {
		return m.agentTable.FilterBar()
	}
	return m.table.FilterBar()
}
