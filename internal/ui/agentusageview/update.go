package agentusageview

import (
	"log"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// The full viewport width, borders included (Rule 116).
		m.table.Resize(msg.Width, max(msg.Height-1, 1))
		return m, nil
	case sessionListLoadedMsg:
		return m.handleListLoaded(msg)
	case spinner.TickMsg:
		return m.handleSpinnerTick(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	m.footer.Handle(msg)
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

// handleSpinnerTick advances the spinner only while a read is in flight, so the
// chain ends with the load.
func (m Model) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if !m.loading {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	m.footer.SetSpinnerFrame(m.spinner.View())
	return m, cmd
}

// handleKey gives the filter the keyboard first; ctrl+r refreshes.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.InEditMode() && msg.String() == "ctrl+r" {
		return m.refresh()
	}
	return m, m.table.Update(msg)
}

// refresh starts a new read under a new generation. The rows stay on screen.
func (m Model) refresh() (tea.Model, tea.Cmd) {
	m.gen++
	m.loading = true
	m.loadErr = false
	return m, tea.Batch(listSessionsCmd(m.gen), m.spinner.Tick)
}

// InEditMode tells the router the filter has the keyboard.
func (m Model) InEditMode() bool { return m.table.FilterBar().InEditMode() }

// FilterBarVisible closes the viewport rectangle around the bar.
func (m Model) FilterBarVisible() bool { return m.table.FilterBar().IsVisible() }
