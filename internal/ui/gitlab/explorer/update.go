package explorer

import (
	"log"
	"os/exec"
	"runtime"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/components"
)

// Update gère les messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize(msg.Width, msg.Height)

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case RootGroupsLoadedMsg:
		m.loading = false
		m.firstLoadDone = true
		m.nodes = msg.Nodes
		m.error = ""
		m.updateTableRows()
		m.table.GotoTop()
		// If there's a pending selection, expand path to it
		if m.pendingSelectPath != "" {
			return m.expandToPath(m.pendingSelectPath)
		}

	case ChildrenLoadedMsg:
		return m.handleChildrenLoaded(msg)

	case LoadErrorMsg:
		return m.handleLoadError(msg)

	case PullDestinationSelectedMsg:
		return m.handlePullDestinationSelected(msg)

	case PullSelectionCancelledMsg:
		m.pullTargetNode = nil
		return m, nil

	case PullCompleteMsg:
		return m.handlePullComplete(msg)

	case components.ReportModalCloseMsg:
		m.mode = ModeNormal
		m.reportModal = nil
		return m, nil

	case components.CreationFormSubmitMsg:
		return m.handleCreationSubmit(msg)

	case components.CreationFormCancelMsg:
		m.mode = ModeNormal
		m.creationForm = nil
		return m, nil

	case TemplatesLoadedMsg:
		return m.handleTemplatesLoaded(msg)

	case GroupCreatedMsg:
		return m.handleGroupCreated(msg)

	case ProjectCreatedMsg:
		return m.handleProjectCreated(msg)

	case components.DeleteConfirmModalYesMsg:
		return m.handleDeleteConfirmed(msg.PermanentlyRemove)

	case components.DeleteConfirmModalNoMsg:
		m.mode = ModeNormal
		m.deleteConfirmModal = nil
		m.deleteTargetNode = nil
		return m, nil

	case DeleteCompleteMsg:
		return m.handleDeleteComplete(msg)

	case BrowserOpenedMsg:
		if msg.Error != nil {
			log.Printf("ERROR [explorer] open browser: %v", msg.Error)
			m.footerError = "Failed to open browser — check logs"
			return m, clearFooterErrorCmd()
		}
		return m, nil

	case clearFooterErrorMsg:
		m.footerError = ""
		return m, nil
	}

	return m, nil
}

// View is implemented in view.go

// handleKeyMsg handles keyboard input
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Filter bar search mode - prioritaire
	if m.table.InEditMode() {
		return m, m.table.Update(msg)
	}

	// Handle mode-specific input
	switch m.mode {
	case ModeShowingReport:
		if m.reportModal != nil {
			var cmd tea.Cmd
			m.reportModal, cmd = m.reportModal.Update(msg)
			return m, cmd
		}
	case ModePulling, ModeLoadingTemplates:
		// Ignore input while pulling or loading templates
		return m, nil
	case ModeCreatingProject:
		if m.creationForm != nil {
			var cmd tea.Cmd
			m.creationForm, cmd = m.creationForm.Update(msg)
			return m, cmd
		}
	case ModeConfirmingDelete:
		if m.deleteConfirmModal != nil {
			var cmd tea.Cmd
			m.deleteConfirmModal, cmd = m.deleteConfirmModal.Update(msg)
			return m, cmd
		}
	}

	// Normal mode. Actions resolve the cursor through the table, which resolves
	// it against the very slice its rows were built from.
	switch msg.String() {
	case "left", "h":
		return m.handleDrillUp()
	case "right", "l":
		return m.handleDrillDown()
	case "esc":
		return m.handleDrillUp()
	case "ctrl+r":
		return m.handleRefresh()
	case "p":
		return m.handlePullStart()
	case "ctrl+n":
		return m.handleCreateResource()
	case "ctrl+d":
		return m.handleDeleteStart()
	case "ctrl+w":
		return m.handleOpenInBrowser()
	}

	// Navigation, `/` and `.` are the table's, not the view's.
	return m, m.table.Update(msg)
}

// handleOpenInBrowser opens the selected node's web URL in the default browser
func (m Model) handleOpenInBrowser() (tea.Model, tea.Cmd) {
	node, ok := m.table.Selected()
	if !ok || node.WebURL == "" {
		return m, nil
	}
	url := node.WebURL
	return m, func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		return BrowserOpenedMsg{Error: cmd.Start()}
	}
}
