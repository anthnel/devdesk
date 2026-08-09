package workspaces

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// Init initialise le modèle
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadEntries(), loadScanCacheCmd(), m.spinner.Tick)
}

// InEditMode returns true if the view is in an edit mode (input, confirm, selection, or filter search)
func (m Model) InEditMode() bool {
	return m.mode != ModeNormal || m.table.InEditMode()
}

// FilterBarVisible returns true when the filter bar is visible (implements app.FilterBarView).
func (m Model) FilterBarVisible() bool {
	return m.table.FilterBar().IsVisible() && m.mode == ModeNormal && len(m.table.Items()) > 0 && m.error == ""
}

// Update gère les messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateTableSize()

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case EntriesLoadedMsg:
		m.setEntries(msg.Entries)
		m.error = ""
		m.refreshRows()
		if m.pendingCursor >= 0 {
			m.table.SetCursor(m.pendingCursor)
			m.pendingCursor = -1
		}

	case LoadErrorMsg:
		m.error = msg.Error.Error()

	case WorkspaceInputSubmitMsg:
		return m.handleInputSubmit(msg)

	case RenameInputSubmitMsg:
		return m.handleRenameSubmit(msg)

	case WorkspaceInputCancelMsg:
		m.mode = ModeNormal
		m.input = nil
		return m, nil

	case WorkspaceCreatedMsg:
		return m.handleWorkspaceCreated(msg)

	case EntryDeletedMsg:
		return m.handleEntryDeleted(msg)

	case EntryRenamedMsg:
		return m.handleEntryRenamed(msg)

	case sharedcomponents.ConfirmModalYesMsg:
		return m.handleConfirmDelete()

	case sharedcomponents.ConfirmModalNoMsg:
		m.mode = ModeNormal
		m.confirmModal = nil
		return m, nil

	case TerminalExitMsg:
		return m, m.loadEntries()

	case TerminalOpenedMsg:
		return m.handleTerminalOpened(msg)

	case IDEOpenedMsg:
		if msg.Error != nil {
			m.error = msg.Error.Error()
		}
		return m, nil

	case BrowserOpenedMsg:
		if msg.Error != nil {
			m.error = msg.Error.Error()
		}
		return m, nil

	case spinner.TickMsg:
		if len(m.scanningPaths) > 0 || len(m.syncingPaths) > 0 {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			m.spinnerFrameIdx = (m.spinnerFrameIdx + 1) % len(spinner.Dot.Frames)
			m.refreshRows()
			return m, cmd
		}

	case ScanCacheLoadedMsg:
		if msg.Cache != nil {
			m.scanCache = msg.Cache
			m.refreshRows()
		}
		return m, nil

	case ScanRequestMsg:
		return m.handleScanRequest(msg)

	case WorkspaceScanStartingMsg:
		tick := m.spinnerTickIfIdle()
		m.scanningPaths[msg.RepoPath] = true
		m.footerInfo = ""
		m.refreshRows()
		return m, tick

	case WorkspaceScanCompleteMsg:
		return m.handleWorkspaceScanComplete(msg)

	case WorkspaceSyncStartingMsg:
		tick := m.spinnerTickIfIdle()
		m.syncingPaths[msg.RepoPath] = true
		m.footerInfo = ""
		m.refreshRows()
		return m, tick

	case WorkspaceSyncCompleteMsg:
		return m.handleWorkspaceSyncComplete(msg)

	case clearSyncSummaryMsg:
		// Only a settled run is dropped: a second sync started inside the three
		// seconds must not have its progress wiped by the first one's timer.
		if m.sync != nil && m.sync.finished() {
			m.sync = nil
		}
		return m, nil

	case clearFooterInfoMsg:
		m.footerInfo = ""
		m.footerError = ""
	}

	// Update table only in normal mode
	if m.mode == ModeNormal {
		cmd = m.table.Update(msg)
	}

	return m, cmd
}

// handleKeyMsg handles keyboard input
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Filter bar search mode - delegate to filter bar
	if m.table.InEditMode() {
		return m, m.table.Update(msg)
	}

	// Mode input - delegate to input component
	if m.mode == ModeAdding && m.input != nil {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	// Mode confirm - delegate to confirm modal
	if m.mode == ModeConfirmingDelete && m.confirmModal != nil {
		var cmd tea.Cmd
		m.confirmModal, cmd = m.confirmModal.Update(msg)
		return m, cmd
	}

	// Mode renaming - delegate to input component
	if m.mode == ModeRenaming && m.input != nil {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	// Selection mode - pick a directory for security view
	if m.mode == ModeSelecting {
		return m.handleSelectionKeyMsg(msg)
	}

	// Normal mode
	switch msg.String() {
	case "/":
		return m, m.table.Update(msg)
	case "enter":
		return m.openScanDetails()
	case "ctrl+r":
		return m, m.loadEntries()
	case "ctrl+n":
		return m.startAdd()
	case "r":
		return m.startRename()
	case "ctrl+d":
		return m.startDelete()
	case "t":
		return m.openTerminalInPlace(m.resolveTargetPath())
	case "T":
		return m.openTerminalWindow()
	case "ctrl+o":
		return m.openIDE()
	case "ctrl+w":
		return m.openInBrowser()
	case "ctrl+s":
		return m.startSecurityScan()
	case "s":
		return m.startSync()
	case "A":
		return m.scanAllUnscanned()
	case "ctrl+a":
		return m.requestScanAll()
	case "left", "h":
		return m.navigateUp()
	case "right", "l":
		return m.navigateIn()
	case "esc":
		return m.navigateUp()
	case "up", "down", "k", "j", "pgup", "pgdown", "g", "home", "G", "end":
		return m, m.table.Update(msg)
	}

	return m, nil
}

// spinnerTickIfIdle restarts the spinner chain when nothing was keeping it
// alive, and is why both Starting handlers read it *before* recording their
// path.
//
// The TickMsg handler stops scheduling the next tick once nothing is running —
// there is no reason to rebuild the rows sixty times a second for a settled
// table — so the chain Init started dies on its first tick. Nothing brought it
// back, and a scan's spinner has therefore been frozen on frame zero since it
// was written: it looked like a marker rather than an animation, so it never
// read as broken. Starting a second chain alongside a live one is the opposite
// mistake, and makes the frames advance at twice the rate.
//
// One window is left, and deliberately: an action started before Init's first
// tick has arrived doubles the chain until the view is closed. It is the
// spinner's own frame interval wide, and the cost is a spinner that spins fast.
func (m Model) spinnerTickIfIdle() tea.Cmd {
	if len(m.scanningPaths) > 0 || len(m.syncingPaths) > 0 {
		return nil
	}
	return m.spinner.Tick
}

// tabCount returns the total number of tabs (home + navigation stack entries + current)
func (m Model) tabCount() int {
	count := 1 // home tab
	count += len(m.navigationStack)
	if m.currentPath != "" {
		count++ // current directory tab
	}
	return count
}

// navigateIn enters the selected directory
func (m Model) navigateIn() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedEntry()
	if !ok || !entry.IsDir {
		return m, nil
	}

	// Save cursor position before navigating down
	m.cursorStack = append(m.cursorStack, m.table.Cursor())
	// Push current path onto navigation stack
	if m.currentPath != "" {
		m.navigationStack = append(m.navigationStack, m.currentPath)
	}
	m.currentPath = entry.Path
	m.activeTabIndex = m.tabCount() - 1
	m.pendingCursor = -1
	return m, m.loadEntries()
}

// navigateUp goes to the parent directory
func (m Model) navigateUp() (tea.Model, tea.Cmd) {
	if m.currentPath == "" {
		return m, nil
	}

	if len(m.navigationStack) > 0 {
		// Pop from stack
		m.currentPath = m.navigationStack[len(m.navigationStack)-1]
		m.navigationStack = m.navigationStack[:len(m.navigationStack)-1]
	} else {
		// Go back to root
		m.currentPath = ""
	}

	// Restore cursor position at the parent level after entries load
	if len(m.cursorStack) > 0 {
		m.pendingCursor = m.cursorStack[len(m.cursorStack)-1]
		m.cursorStack = m.cursorStack[:len(m.cursorStack)-1]
	} else {
		m.pendingCursor = -1
	}

	m.activeTabIndex = m.tabCount() - 1
	return m, m.loadEntries()
}

// handleSelectionKeyMsg handles keyboard input in selection mode
func (m Model) handleSelectionKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.currentPath != "" {
			return m.navigateUp()
		}
		return m, func() tea.Msg { return SelectionCancelledMsg{} }
	case "enter":
		// Confirm selection: use the current browsing directory (not the highlighted entry).
		// Use → / l to navigate into a subdirectory first.
		path := m.currentPath
		if path == "" {
			path = m.getExpandedWorkspacesDir()
		}
		return m, func() tea.Msg { return DirectorySelectedMsg{Path: path} }
	case "left", "h":
		return m.navigateUp()
	case "right", "l":
		return m.navigateIn()
	case "up", "down", "k", "j", "pgup", "pgdown", "g", "home", "G", "end":
		return m, m.table.Update(msg)
	}
	return m, nil
}

// getExpandedWorkspacesDir returns the workspaces directory with tilde expanded
func (m Model) getExpandedWorkspacesDir() string {
	workspacesDir := m.config.App.WorkspacesDir
	if len(workspacesDir) >= 2 && workspacesDir[:2] == "~/" {
		home, _ := os.UserHomeDir()
		workspacesDir = filepath.Join(home, workspacesDir[2:])
	}
	return workspacesDir
}

// startAdd switches to input mode for creating a new workspace/folder
func (m Model) startAdd() (tea.Model, tea.Cmd) {
	m.mode = ModeAdding
	m.input = NewWorkspaceInput()
	return m, nil
}

// startDelete switches to confirm mode for deleting an entry
func (m Model) startDelete() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedEntry()
	if !ok {
		return m, nil
	}

	m.pendingEntry = &entry
	m.mode = ModeConfirmingDelete

	var title string
	if entry.IsDir {
		title = "Delete Directory"
	} else {
		title = "Delete File"
	}

	m.confirmModal = sharedcomponents.NewConfirmModal(
		title,
		fmt.Sprintf("Are you sure you want to delete '%s'?\n\nThis will permanently delete it and all its contents.", entry.Name),
	)
	return m, nil
}

// startRename switches to rename mode for the selected entry
func (m Model) startRename() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedEntry()
	if !ok {
		return m, nil
	}
	m.pendingEntry = &entry
	m.mode = ModeRenaming
	m.input = NewRenameInput(entry.Name)
	return m, nil
}

// handleRenameSubmit handles the rename form submission
func (m Model) handleRenameSubmit(msg RenameInputSubmitMsg) (tea.Model, tea.Cmd) {
	m.mode = ModeNormal
	m.input = nil
	if m.pendingEntry == nil {
		return m, nil
	}
	return m, m.renameEntry(m.pendingEntry.Path, msg.Name)
}

// renameEntry renames a filesystem entry
func (m Model) renameEntry(oldPath, newName string) tea.Cmd {
	newPath := filepath.Join(filepath.Dir(oldPath), newName)
	return func() tea.Msg {
		if err := os.Rename(oldPath, newPath); err != nil {
			return EntryRenamedMsg{Error: err}
		}
		return EntryRenamedMsg{OldPath: oldPath, NewPath: newPath}
	}
}

// handleEntryRenamed handles the result of entry renaming
func (m Model) handleEntryRenamed(msg EntryRenamedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		m.error = msg.Error.Error()
		return m, nil
	}
	return m, m.loadEntries()
}

// handleInputSubmit handles the workspace input form submission
func (m Model) handleInputSubmit(msg WorkspaceInputSubmitMsg) (tea.Model, tea.Cmd) {
	m.mode = ModeNormal
	m.input = nil
	return m, m.createWorkspace(msg.Name)
}

// handleConfirmDelete handles the confirmation of entry deletion
func (m Model) handleConfirmDelete() (tea.Model, tea.Cmd) {
	m.mode = ModeNormal
	m.confirmModal = nil

	if m.pendingEntry == nil {
		return m, nil
	}
	return m, m.deleteEntry(m.pendingEntry.Path)
}

// handleWorkspaceCreated handles the result of workspace creation
func (m Model) handleWorkspaceCreated(msg WorkspaceCreatedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		m.error = msg.Error.Error()
		return m, nil
	}
	return m, m.loadEntries()
}

// handleEntryDeleted handles the result of entry deletion
func (m Model) handleEntryDeleted(msg EntryDeletedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		m.error = msg.Error.Error()
		return m, nil
	}
	return m, m.loadEntries()
}
