package workspaces

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
)

// Init initialise le modèle
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadEntries(), loadScanCacheCmd()}
	// A view lent for a selection offers neither S nor A, so resolving the
	// scanners for it would be three subprocesses spawned to answer a question
	// nobody asks.
	if m.mode == ModeNormal {
		cmds = append(cmds, checkDepsCmd(m.config.Scan))
	}
	return tea.Batch(cmds...)
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

	case DepsCheckedMsg:
		deps := msg.Deps
		m.deps = &deps

	case EntriesLoadedMsg:
		// A listing of somewhere the user has already left: two loads were in
		// flight and this is the loser. Displaying it would put one directory's
		// rows under another's breadcrumb.
		if msg.Path != m.currentPath {
			return m, nil
		}
		m.listingPath = msg.Path
		m.setEntries(msg.Entries)
		m.error = ""
		m.refreshRows()
		if m.pendingCursor >= 0 {
			m.table.SetCursor(m.pendingCursor)
			m.pendingCursor = -1
		}
		// Anything an agent asked for before this view had read the directory
		// waited for exactly this (§3.61) — see deferUntilLoaded.
		if cmd := m.drainPendingRequests(); cmd != nil {
			return m, cmd
		}

	case LoadErrorMsg:
		if msg.Path != m.currentPath {
			return m, nil
		}
		m.error = msg.Error.Error()
		if cmd := m.refusePendingRequests("Cannot read " + msg.Path + ": " + msg.Error.Error()); cmd != nil {
			return m, cmd
		}

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

	case sharedcomponents.OptionConfirmModalYesMsg:
		m.mode = ModeNormal
		m.scanAllModal = nil
		return m.scanAll(msg.Option)

	case sharedcomponents.OptionConfirmModalNoMsg:
		m.mode = ModeNormal
		m.scanAllModal = nil
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

	case PathCopiedMsg:
		return m.handlePathCopied(msg)

	case jobs.ChangedMsg:
		return m.handleJobsChanged(msg)

	case ScanCacheLoadedMsg:
		if msg.Cache != nil {
			m.scanCache = msg.Cache
			m.refreshRows()
		}
		return m, nil

	case ScanRequestMsg:
		return m.handleScanRequest(msg)

	// An agent asks the way a key does (§3.61) — see mcp.go.
	case ScanRequestedMsg:
		return m.handleScanRequested(msg)

	case SyncRequestedMsg:
		return m.handleSyncRequested(msg)

	case WorkspaceScanStartingMsg:
		// The row is already spinning: the router recorded the transition
		// before handing the message on, and the snapshot that carried it
		// rebuilt the rows. All that is left is to drop whatever the footer
		// still said about the previous attempt.
		m.footer.Clear()
		return m, nil

	case WorkspaceScanCompleteMsg:
		return m.handleWorkspaceScanComplete(msg)

	case WorkspaceSyncStartingMsg:
		m.footer.Clear()
		return m, nil

	case WorkspaceSyncCompleteMsg:
		return m.handleWorkspaceSyncComplete(msg)

	}

	m.footer.Handle(msg)

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

	// Mode scan-all - delegate to the option modal
	if m.mode == ModeConfirmingScanAll && m.scanAllModal != nil {
		var cmd tea.Cmd
		m.scanAllModal, cmd = m.scanAllModal.Update(msg)
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

	// Normal mode. The actions the header greys out are refused here, from the
	// same actionSet — one calculation, two readers, so a greyed key that still
	// acts is not expressible.
	a := m.actions()

	switch msg.String() {
	case "/":
		return m, m.table.Update(msg)
	case "enter":
		return m.guard(a.Enter, m.openScanDetails)
	case "ctrl+r":
		return m, m.loadEntries()
	case keymap.New:
		return m.startAdd()
	case keymap.Rename:
		return m.guard(a.Rename, m.startRename)
	case keymap.Delete:
		return m.guard(a.Delete, m.startDelete)
	// The "new window" variant is a setting, not a second key: the capability
	// depends on the environment — the help already says it does not exist
	// under WSL, and through SSH there is no window to open (§3.26).
	case keymap.Terminal:
		return m.openTerminal()
	case keymap.IDE:
		return m.openIDE()
	case keymap.Web:
		return m.guard(a.Web, m.openInBrowser)
	case keymap.Scan:
		return m.guard(a.Scan, m.startSecurityScan)
	case keymap.Fetch:
		return m.guard(a.Sync, m.startSync)
	case keymap.ScanAll:
		return m.guard(a.All, m.confirmScanAll)
	case keymap.Copy:
		return m.guard(a.Copy, m.copyPath)
	case "left":
		return m.navigateUp()
	case "right":
		return m.navigateIn()
	case "esc":
		return m.navigateUp()
	case "up", "down", "pgup", "pgdown", "home", "end":
		return m, m.table.Update(msg)
	}

	return m, nil
}

// handlePathCopied reports the clipboard write (Rule 128).
//
// The confirmation says "full path" rather than repeating the entry's name: the
// row is under the cursor, so naming it again would be the same fact twice on
// one screen. What is not on screen is the *form* of what landed — the absolute
// path, not the name — and that is what the line says.
func (m Model) handlePathCopied(msg PathCopiedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		log.Printf("ERROR [workspaces] copy path %s to clipboard: %v", msg.Path, msg.Error)
		return m, m.footer.Error("Failed to copy the path — check logs")
	}
	return m, m.footer.Info("Full path copied to the clipboard")
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

// navigateIn enters the selected directory.
//
// It does nothing while the table still holds the previous directory's rows.
// A load is a Cmd, so holding `→` used to drill twice from one listing: the
// second press read the same row, pushed the *new* currentPath onto the stack,
// and the breadcrumb grew a duplicate — `devsecops devsecops devex devex`. With
// the cursor moved in between it was worse than duplication: a sibling of the
// directory just entered was pushed as if it were nested inside it.
//
// Refusing is the honest answer rather than a hazard to remember: the rows in
// hand are not this directory's, so there is nothing here to enter yet. `←` is
// deliberately not guarded — it reads the stack, not the table, and a listing
// it overtakes is dropped on arrival.
func (m Model) navigateIn() (tea.Model, tea.Cmd) {
	if m.listingPath != m.currentPath {
		return m, nil
	}

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
		// Use → to navigate into a subdirectory first.
		path := m.currentPath
		if path == "" {
			path = m.getExpandedWorkspacesDir()
		}
		return m, func() tea.Msg { return DirectorySelectedMsg{Path: path} }
	case "left":
		return m.navigateUp()
	case "right":
		return m.navigateIn()
	case "up", "down", "pgup", "pgdown", "home", "end":
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
	// Refused before the modal rather than after it: asking the question and
	// then declining the answer is the one order that wastes the user's time.
	if m.busy(entry.Path) {
		return m, m.footer.Warn(busyMessage)
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
	path := m.pendingEntry.Path
	// Checked again here, not only in startDelete: a batch sync marks its
	// repositories from a Cmd, so one can take this path while the
	// confirmation is on screen. This is where the irreversible call is
	// issued, so this is where the answer has to be current.
	if m.busy(path) {
		return m, m.footer.Warn(busyMessage)
	}

	m.footer.Clear()
	return m, jobs.Start(m.deleteRun(path), m.deleteEntry(path))
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
	// The marker is the registry's now, and it is cleared on every outcome —
	// failures included — by the transition the router applied before this ran.
	// One left behind would hold the path against every other action for the
	// life of the view.
	if msg.Error != nil {
		log.Printf("ERROR [workspaces] delete %s: %v", msg.Path, msg.Error)
		m.error = msg.Error.Error()
		return m, nil
	}
	return m, m.loadEntries()
}
