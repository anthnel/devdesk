package security

import (
	"fmt"
	"log"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/jobs"

	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle confirm modal messages first (before passing to modal)
	switch msg := msg.(type) {
	case sharedcomponents.ConfirmModalYesMsg:
		// User confirmed to ignore the secret
		if m.findingToIgnore != nil {
			finding := *m.findingToIgnore
			targetDir := m.targetPath
			m.confirmModal = nil
			m.findingToIgnore = nil
			return m, func() tea.Msg {
				err := scan.AddToGitleaksIgnore(targetDir, finding)
				return SecretIgnoredMsg{Finding: finding, Error: err}
			}
		}
		m.confirmModal = nil
		m.findingToIgnore = nil
		return m, nil

	case sharedcomponents.ConfirmModalNoMsg:
		// User cancelled
		m.confirmModal = nil
		m.findingToIgnore = nil
		return m, nil

	case sharedcomponents.OptionConfirmModalYesMsg:
		m.scanAllModal = nil
		return m.rescanAll(msg.Option)

	case sharedcomponents.OptionConfirmModalNoMsg:
		m.scanAllModal = nil
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Widths and height in one call; SetHeight accounts for the header row
		// internally, so m.height goes through as it is. Rows no longer depend
		// on the width — bubbles truncates each cell to its own column — so
		// there is nothing to rebuild here.
		m.resizeFindings()
		m.inventory.Resize(m.width, max(m.height, 5))
		// Update details viewport size and refresh content
		m.detailsViewport.Width = msg.Width
		m.detailsViewport.Height = msg.Height
		if m.state == StateDetails {
			m.detailsViewport.SetContent(m.buildDetailsContent())
		}
		return m, nil

	case tea.KeyMsg:
		// Whichever modal is open takes every key before the view sees one.
		if m.confirmModal != nil {
			var cmd tea.Cmd
			m.confirmModal, cmd = m.confirmModal.Update(msg)
			return m, cmd
		}
		if m.scanAllModal != nil {
			var cmd tea.Cmd
			m.scanAllModal, cmd = m.scanAllModal.Update(msg)
			return m, cmd
		}
		return m.handleKeyMsg(msg)

	case SecretIgnoredMsg:
		if msg.Error != nil {
			log.Printf("ERROR [security] ignore secret %s: %v", msg.Finding.File, msg.Error)
			return m, m.footer.Error("Failed to ignore secret — check logs")
		}
		return m, m.footer.Info(fmt.Sprintf("Added %s to .gitleaksignore", msg.Finding.File))

	case InventoryLoadedMsg:
		return m.handleInventoryLoaded(msg)

	case InventoryResultLoadedMsg:
		return m.handleInventoryResultLoaded(msg)

	case InventoryScanFinishedMsg:
		return m.handleInventoryScanFinished(msg)

	case InventoryScanStartingMsg:
		// The row is already spinning: the router recorded the transition
		// before handing the message on, and the snapshot rebuilt the rows.
		return m, nil

	case jobs.ChangedMsg:
		return m.handleJobsChanged(msg)

	case spinner.TickMsg:
		return m.handleSpinnerTick(msg)
	}

	m.footer.Handle(msg)
	return m, nil
}

// handleSpinnerTick advances the spinner while the caches are being read.
//
// It animates a *load* and nothing else now. A scan's frame comes from the
// registry, which holds the one chain for the whole application (D5), and the
// rows are restamped when its snapshot arrives — see handleJobsChanged.
func (m Model) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if !m.spinnerAlive() {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	m.spinnerFrameIdx++
	// The footer renders the frame, not a raw one: the spinner already carries
	// theme.SpinnerStyle() and restyling it would nest one sequence in another
	// (Rule 128).
	m.footer.SetSpinnerFrame(m.spinner.View())
	return m, cmd
}

// handleKeyMsg processes keyboard input
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case StateInventory:
		return m.handleInventoryState(msg)
	case StateResults:
		return m.handleResultsState(msg)
	case StateDetails:
		return m.handleDetailsState(msg)
	}
	return m, nil
}
