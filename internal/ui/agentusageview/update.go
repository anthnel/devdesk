package agentusageview

import (
	"log"

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
	case sessionUsageLoadedMsg:
		return m.handleUsageLoaded(msg)
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

// handleUsageLoaded takes a parsed session, or ignores one for a session that
// was left or re-read since. A failed refresh keeps the rows already shown.
func (m Model) handleUsageLoaded(msg sessionUsageLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.agentGen || m.level != levelAgents {
		return m, nil
	}
	m.agentsLoading = false
	if msg.err != nil {
		// The error names the file by base name only; the path stays out.
		log.Printf("ERROR [agentusageview] read session: %v", msg.err)
		m.agentsErr = true
		return m, nil
	}
	m.agentsErr = false
	m.usage = msg.usage
	m.agentTable.SetItems(agentRows(msg.usage))
	m.agentTable.Remeasure() // another session is another population
	return m, nil
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
		if !a.Sort.Enabled() {
			return m, m.footer.Warn(a.Sort.Reason)
		}
	}
	return m, m.activeTable(msg)
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
	m.usage = agentusage.Usage{}
	m.level = levelAgents
	m.open = row.Session
	m.agentGen++
	m.agentsLoading, m.agentsErr = true, false
	m.agentTable.FilterBar().ClearSearch() // a filter typed for another session
	m.agentTable.SetItems(nil)
	return m, tea.Batch(parseSessionCmd(m.agentGen, m.open.Parent), m.spinner.Tick)
}

// closeAgents returns to the sessions, cursor and filter as they were. Bumping
// agentGen drops a parse still in flight.
func (m Model) closeAgents() (tea.Model, tea.Cmd) {
	m.level = levelSessions
	m.agentGen++
	m.agentsLoading, m.agentsErr = false, false
	m.usage = agentusage.Usage{}
	m.agentTable.SetItems(nil)
	return m, nil
}

// refresh starts a new read of the level on screen under a new generation.
// The rows stay on screen.
func (m Model) refresh() (tea.Model, tea.Cmd) {
	if m.level == levelAgents {
		m.agentGen++
		m.agentsLoading = true
		return m, tea.Batch(parseSessionCmd(m.agentGen, m.open.Parent), m.spinner.Tick)
	}
	m.gen++
	m.loading = true
	m.loadErr = false
	return m, tea.Batch(listSessionsCmd(m.gen), m.spinner.Tick)
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
