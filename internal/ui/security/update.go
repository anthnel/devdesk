package security

import (
	"fmt"
	"log"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

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

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// SetHeight accounts for the header row internally, so pass m.height directly
		m.findingsTable.SetHeight(max(m.height, 5))
		// Always recalculate columns on resize
		m.findingsTable.SetColumns(m.calculateColumns())
		// Rebuild rows if in results or details state (to apply new title width truncation)
		if (m.state == StateResults || m.state == StateDetails) && m.result != nil {
			m.updateFindingsTable()
		}
		// Update details viewport size and refresh content
		m.detailsViewport.Width = msg.Width
		m.detailsViewport.Height = msg.Height
		if m.state == StateDetails {
			m.detailsViewport.SetContent(m.buildDetailsContent())
		}
		return m, nil

	case tea.KeyMsg:
		// Pass key messages to confirm modal if active
		if m.confirmModal != nil {
			var cmd tea.Cmd
			m.confirmModal, cmd = m.confirmModal.Update(msg)
			return m, cmd
		}
		return m.handleKeyMsg(msg)

	case DepsCheckedMsg:
		m.deps = msg.Deps
		return m, nil

	case ScanCompleteMsg:
		return m.handleScanComplete(msg)

	case SecretIgnoredMsg:
		if msg.Error != nil {
			log.Printf("ERROR [security] ignore secret %s: %v", msg.Finding.File, msg.Error)
			m.statusMessage = "Failed to ignore secret — check logs"
		} else {
			m.statusMessage = fmt.Sprintf("Added %s to .gitleaksignore", msg.Finding.File)
		}
		return m, clearStatusCmd()

	case clearStatusMsg:
		m.statusMessage = ""
		return m, nil

	case SelectionResultMsg:
		m.targetPath = msg.Path
		m.targetInput.SetValue(msg.Path)
		return m, nil

	case SelectionCancelledMsg:
		return m, nil

	case StartScanMsg:
		return m.startScan()

	case ScanProgressMsg:
		return m.handleScanProgress(msg)

	case spinner.TickMsg:
		if m.state == StateScanning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

// handleKeyMsg processes keyboard input
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case StateInput:
		return m.handleInputState(msg)
	case StateScanning:
		if msg.String() == "esc" || msg.String() == "ctrl+c" {
			return m.cancelCurrentScan()
		}
		return m, nil
	case StateResults:
		return m.handleResultsState(msg)
	case StateDetails:
		return m.handleDetailsState(msg)
	}
	return m, nil
}
