package explorer

import (
	"log"
	"os/exec"
	"runtime"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
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
		return m.handleSpinnerTick(msg)

	case RootGroupsLoadedMsg:
		m.loading = false
		m.firstLoadDone = true
		m.nodes = carryOverCreating(m.nodes, msg.Nodes)
		m.error = ""
		m.updateTableRows()
		m.table.GotoTop()

	case ChildrenLoadedMsg:
		return m.handleChildrenLoaded(msg)

	case LoadErrorMsg:
		return m.handleLoadError(msg)

	case CloneDestinationSelectedMsg:
		return m.handleCloneDestinationSelected(msg)

	case CloneSelectionCancelledMsg:
		// The destination was refused, not the selection: stay in ModeSelecting
		// so the ticks the user made are still there.
		return m, nil

	case CloneEventMsg:
		return m.handleCloneEvent(msg)

	case CloneRunFinishedMsg:
		return m.handleCloneRunFinished()

	case jobs.ChangedMsg:
		return m.handleJobsChanged(msg)

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

	case components.OptionConfirmModalYesMsg:
		return m.handleDeleteConfirmed(msg.Option)

	case components.OptionConfirmModalNoMsg:
		m.mode = ModeNormal
		m.deleteConfirmModal = nil
		m.deleteTargetNode = nil
		return m, nil

	case DeleteCompleteMsg:
		return m.handleDeleteComplete(msg)

	case BrowserOpenedMsg:
		if msg.Error != nil {
			log.Printf("ERROR [explorer] open browser: %v", msg.Error)
			return m, m.footer.Error("Failed to open browser — check logs")
		}
		return m, nil
	}

	m.footer.Handle(msg)
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
	case ModeCloning:
		return m.handleCloningKey(msg)
	case ModeSelecting:
		return m.handleSelectingKey(msg)
	case ModeLoadingTemplates:
		// Ignore input while loading templates
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
	case "left":
		return m.handleDrillUp()
	case "right":
		return m.handleDrillDown()
	case "esc":
		return m.handleDrillUp()
	case "ctrl+r":
		return m.handleRefresh()
	case keymap.Clone:
		return m.handleCloneStart()
	case keymap.New:
		return m.handleCreateResource()
	case keymap.Delete:
		return m.handleDeleteStart()
	case keymap.Web:
		return m.handleOpenInBrowser()
	}

	// Navigation, `/` and `.` are the table's, not the view's.
	return m, m.table.Update(msg)
}

// handleSelectingKey is the clone selection mode (Rule 135 unchanged): `space`
// ticks, `←→` still drill, `enter` confirms.
//
// `esc` leaves the mode rather than drilling up, because cancelling is what the
// key means when there is something to cancel; `←` is still the way up.
func (m Model) handleSelectingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case " ":
		return m.handleSelectionToggle()
	case "enter":
		return m.handleSelectionConfirm()
	case "esc":
		return m.handleSelectionCancel()
	case "left":
		return m.handleDrillUp()
	case "right":
		return m.handleDrillDown()
	}
	return m, m.table.Update(msg)
}

// handleCloningKey is the clone list: `esc` cancels, then closes. Navigation,
// `/` and `.` are the list's own table.
func (m Model) handleCloningKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.clone == nil {
		m.mode = ModeNormal
		return m, nil
	}
	// The list has its own filter bar, and its `esc` closes the search. Testing
	// the key first would cancel the run from inside the search box.
	if m.clone.table.InEditMode() {
		return m, m.clone.table.Update(msg)
	}
	if msg.String() == "esc" {
		return m.handleCloneEsc()
	}
	return m, m.clone.table.Update(msg)
}

// handleSpinnerTick advances the spinner the view owns, which is now the
// **load** and nothing else.
//
// It used to drive the clone rows as well, and that was the fourth hand-stamped
// chain D5 removed: the rows take the router's frame from the broadcast now, so
// a clone keeps turning whether or not this view is on screen — and cannot
// freeze on frame zero if this chain dies.
func (m Model) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if !m.loading {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	// The load is reported in the footer, so the frame has to reach it — a
	// spinner stuck on frame zero reads as a hang.
	m.footer.SetSpinnerFrame(m.spinner.View())
	return m, cmd
}

// handleOpenInBrowser opens the selected node's web URL in the default browser
func (m Model) handleOpenInBrowser() (tea.Model, tea.Cmd) {
	if browse := m.browsable(); !browse.Enabled() {
		return m, m.footer.Warn(browse.Reason)
	}
	node, _ := m.selectedNode()
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
