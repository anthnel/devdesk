package workspaces

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"gitlab.com/anthnell/devsecops/devdesk/internal/cache"
	"gitlab.com/anthnell/devsecops/devdesk/internal/config"
	"gitlab.com/anthnell/devsecops/devdesk/internal/scan"
	sharedcomponents "gitlab.com/anthnell/devsecops/devdesk/internal/ui/components"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/theme"
)

// ViewMode represents the current mode of the view
type ViewMode int

const (
	ModeNormal ViewMode = iota
	ModeAdding
	ModeConfirmingDelete
	ModeRenaming
	ModeSelecting // Selecting a directory for another view (e.g., security)
)

// Model représente le modèle de la vue Workspaces
type Model struct {
	config *config.Config
	width  int
	height int

	entries []Entry
	table   table.Model
	error   string

	// Navigation state (drill-down like explorer)
	currentPath     string   // Empty = root (workspaces list), otherwise = current directory path
	navigationStack []string // Stack of parent paths for breadcrumb tabs
	cursorStack     []int    // Cursor positions per level for restoration on navigate-up
	pendingCursor   int      // Cursor to restore after async loadEntries (-1 = none)

	// Tab navigation
	activeTabIndex int

	// Mode and components
	mode         ViewMode
	input        *WorkspaceInput
	confirmModal *sharedcomponents.ConfirmModal
	selectedIdx  int

	// Spinner for scanning animation
	spinner         spinner.Model
	spinnerFrameIdx int

	// Scan cache: keyed by absolute repo path
	scanCache map[string]cache.WorkspaceScanEntry

	// Paths currently being scanned (keyed by absolute path)
	scanningPaths map[string]bool

	// footerError holds a short scan error message for the footer info line (Rule 128)
	footerError string
	// footerInfo holds a transient informational message for the footer info line
	footerInfo string

	// selectionMessage is displayed in the footer when in ModeSelecting
	selectionMessage string

	// filterBar provides text search for the table (Rule 136)
	filterBar sharedcomponents.FilterBar
}

// Entry represents a file system entry with enriched metadata
type Entry struct {
	Name    string
	Path    string
	ModTime time.Time
	IsDir   bool

	// Git metadata (populated only for git repos)
	IsGitRepo    bool
	SubRepoPaths []string // git repos nested within this directory (if !IsGitRepo)
	GitBranch    string
	GitRemote    string // remote path without server URL (e.g. "group/project")
	GitRemoteURL string // full remote URL normalized for browser opening
	GitModified  int
	GitUntracked int
	GitUnpushed  int
	GitUnpulled  int

	// Project metadata
	ProjectType string // "Go", "Node", "Python", "Rust", "Java", etc.
}

// New crée une nouvelle instance du modèle workspaces
func New(cfg *config.Config) Model {
	columns := defaultColumns()

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(theme.DefaultTableStyles())

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.SpinnerStyle()

	return Model{
		config:        cfg,
		entries:       []Entry{},
		table:         t,
		mode:          ModeNormal,
		pendingCursor: -1,
		spinner:       s,
		scanCache:     make(map[string]cache.WorkspaceScanEntry),
		scanningPaths: make(map[string]bool),
		filterBar:     sharedcomponents.NewFilterBar(),
	}
}

// NewForSelection creates a workspaces view in selection mode for picking a directory
func NewForSelection(cfg *config.Config, message string) Model {
	m := New(cfg)
	m.mode = ModeSelecting
	m.selectionMessage = message
	return m
}

// Init initialise le modèle
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadEntries(), loadScanCacheCmd(), m.spinner.Tick)
}

// InEditMode returns true if the view is in an edit mode (input, confirm, selection, or filter search)
func (m Model) InEditMode() bool {
	return m.mode != ModeNormal || m.filterBar.InEditMode()
}

// FilterBarVisible returns true when the filter bar is visible (implements app.FilterBarView).
func (m Model) FilterBarVisible() bool {
	return m.filterBar.IsVisible() && m.mode == ModeNormal && len(m.entries) > 0 && m.error == ""
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
		m.entries = msg.Entries
		m.error = ""
		m.updateTableData()
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
		if len(m.scanningPaths) > 0 {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			m.spinnerFrameIdx = (m.spinnerFrameIdx + 1) % len(spinner.Dot.Frames)
			m.updateTableData()
			return m, cmd
		}

	case ScanCacheLoadedMsg:
		if msg.Cache != nil {
			m.scanCache = msg.Cache
			m.updateTableData()
		}
		return m, nil

	case WorkspaceScanStartingMsg:
		m.scanningPaths[msg.RepoPath] = true
		m.footerInfo = ""
		m.updateTableData()
		return m, nil

	case WorkspaceScanCompleteMsg:
		return m.handleWorkspaceScanComplete(msg)

	case clearFooterInfoMsg:
		m.footerInfo = ""
		m.footerError = ""
	}

	// Update table only in normal mode
	if m.mode == ModeNormal {
		m.table, cmd = m.table.Update(msg)
	}

	return m, cmd
}

// handleKeyMsg handles keyboard input
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Filter bar search mode - delegate to filter bar
	if m.filterBar.InEditMode() {
		var cmd tea.Cmd
		m.filterBar, cmd = m.filterBar.Update(msg)
		m.updateTableData()
		return m, cmd
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
		return m, m.filterBar.ActivateSearch()
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
	case "up", "down", "k", "j":
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	case "g", "home":
		m.table.GotoTop()
		return m, nil
	case "G", "end":
		m.table.GotoBottom()
		return m, nil
	}

	return m, nil
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
	if len(m.entries) == 0 {
		return m, nil
	}

	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.entries) {
		return m, nil
	}

	entry := m.entries[idx]
	if !entry.IsDir {
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
	case "up", "down", "k", "j":
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	case "g", "home":
		m.table.GotoTop()
		return m, nil
	case "G", "end":
		m.table.GotoBottom()
		return m, nil
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
	if len(m.entries) == 0 {
		return m, nil
	}

	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.entries) {
		return m, nil
	}

	m.selectedIdx = idx
	m.mode = ModeConfirmingDelete
	entry := m.entries[idx]

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
	if len(m.entries) == 0 {
		return m, nil
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.entries) {
		return m, nil
	}
	m.selectedIdx = idx
	m.mode = ModeRenaming
	m.input = NewRenameInput(m.entries[idx].Name)
	return m, nil
}

// handleRenameSubmit handles the rename form submission
func (m Model) handleRenameSubmit(msg RenameInputSubmitMsg) (tea.Model, tea.Cmd) {
	m.mode = ModeNormal
	m.input = nil
	if m.selectedIdx < 0 || m.selectedIdx >= len(m.entries) {
		return m, nil
	}
	entry := m.entries[m.selectedIdx]
	return m, m.renameEntry(entry.Path, msg.Name)
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

	if m.selectedIdx < 0 || m.selectedIdx >= len(m.entries) {
		return m, nil
	}

	entry := m.entries[m.selectedIdx]
	return m, m.deleteEntry(entry.Path)
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

// startSecurityScan launches a security scan on the selected entry using saved options.
// For git repos, scans the single repo. For directories with sub-repos, scans all sub-repos in parallel.
func (m Model) startSecurityScan() (tea.Model, tea.Cmd) {
	opts := m.getScanOptions()

	if len(m.entries) == 0 {
		// Fallback: scan the current directory path
		targetPath := m.currentPath
		if targetPath == "" {
			targetPath = m.getExpandedWorkspacesDir()
		}
		if targetPath == "" {
			return m, nil
		}
		if m.scanningPaths[targetPath] {
			m.footerInfo = "Scan already in progress"
			return m, clearFooterInfoCmd()
		}
		delete(m.scanCache, targetPath)
		return m, tea.Batch(deleteScanCacheCmd([]string{targetPath}), batchScanCmd([]string{targetPath}, opts))
	}

	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.entries) {
		return m, nil
	}

	entry := m.entries[idx]
	if !entry.IsDir {
		return m, nil
	}

	if entry.IsGitRepo {
		if m.scanningPaths[entry.Path] {
			m.footerInfo = "Scan already in progress"
			return m, clearFooterInfoCmd()
		}
		delete(m.scanCache, entry.Path)
		return m, tea.Batch(deleteScanCacheCmd([]string{entry.Path}), batchScanCmd([]string{entry.Path}, opts))
	}

	// Non-git directory: scan all non-scanning nested git repos in parallel
	if len(entry.SubRepoPaths) == 0 {
		return m, nil
	}

	var toScan []string
	for _, repoPath := range entry.SubRepoPaths {
		if !m.scanningPaths[repoPath] {
			toScan = append(toScan, repoPath)
		}
	}

	if len(toScan) == 0 {
		m.footerInfo = "Scan already in progress"
		return m, clearFooterInfoCmd()
	}

	for _, path := range toScan {
		delete(m.scanCache, path)
	}
	return m, tea.Batch(deleteScanCacheCmd(toScan), batchScanCmd(toScan, opts))
}

// scanAllUnscanned triggers batch scanning of all unscanned git repos visible in the current view.
func (m Model) scanAllUnscanned() (tea.Model, tea.Cmd) {
	paths := m.collectAllRepoPaths()
	var unscanned []string
	for _, path := range paths {
		if _, ok := m.scanCache[path]; !ok && !m.scanningPaths[path] {
			unscanned = append(unscanned, path)
		}
	}
	if len(unscanned) == 0 {
		return m, nil
	}
	return m, batchScanCmd(unscanned, m.getScanOptions())
}

// requestScanAll triggers batch scanning of all git repos visible in the current view,
// purging in-memory and disk cache first (Rule 126).
func (m Model) requestScanAll() (tea.Model, tea.Cmd) {
	paths := m.collectAllRepoPaths()
	if len(paths) == 0 {
		return m, nil
	}
	// Purge in-memory cache synchronously (Update() is thread-safe)
	for _, path := range paths {
		delete(m.scanCache, path)
	}
	return m, tea.Batch(deleteScanCacheCmd(paths), batchScanCmd(paths, m.getScanOptions()))
}

// collectAllRepoPaths returns all git repo paths visible in the current view,
// including sub-repos nested within non-git directories.
func (m Model) collectAllRepoPaths() []string {
	var paths []string
	for _, entry := range m.entries {
		if entry.IsGitRepo {
			paths = append(paths, entry.Path)
		} else if len(entry.SubRepoPaths) > 0 {
			paths = append(paths, entry.SubRepoPaths...)
		}
	}
	return paths
}

// handleWorkspaceScanComplete processes the result of a completed workspace scan (Rule 128).
func (m Model) handleWorkspaceScanComplete(msg WorkspaceScanCompleteMsg) (tea.Model, tea.Cmd) {
	delete(m.scanningPaths, msg.RepoPath)
	if msg.Error != nil {
		log.Printf("ERROR [workspaces] scan complete %s: %v", msg.RepoPath, msg.Error)
		m.footerError = "Scan failed — check logs"
		m.updateTableData()
		return m, clearFooterInfoCmd()
	}
	m.footerError = ""
	m.scanCache[msg.RepoPath] = cache.WorkspaceScanEntry{
		RepoPath:  msg.RepoPath,
		Critical:  msg.Critical,
		High:      msg.High,
		Medium:    msg.Medium,
		Low:       msg.Low,
		Sensitive: msg.Sensitive,
		ScannedAt: msg.ScannedAt,
	}
	m.updateTableData()
	return m, nil
}

// getScanOptions builds scan options from current configuration
func (m Model) getScanOptions() scan.ScanOptions {
	return scan.ScanOptions{
		EnableVuln:      m.config.Scan.EnableVuln,
		EnableSecret:    m.config.Scan.EnableSecret,
		EnableMisconfig: m.config.Scan.EnableMisconfig,
		EnableLicense:   m.config.Scan.EnableLicense,
		GenerateSBOM:    m.config.Scan.GenerateSBOM,
		SBOMOutputDir:   m.config.Scan.SBOMOutputDir,
		TrivyImage:      m.config.Scan.TrivyImage,
		GitleaksImage:   m.config.Scan.GitleaksImage,
		TrivyServer:     m.config.Scan.TrivyServer,
		IgnoreUnfixed:   m.config.Scan.IgnoreUnfixed,
		GitleaksHistory: m.config.Scan.GitleaksHistory,
		GitleaksConfig:  m.config.Scan.GitleaksConfig,
	}
}

// resolveTargetPath returns the path to open for terminal/IDE actions.
// Priority: selected directory entry > current browsed path > workspaces root.
func (m Model) resolveTargetPath() string {
	targetPath := m.getExpandedWorkspacesDir()
	if m.currentPath != "" {
		targetPath = m.currentPath
	}
	if len(m.entries) > 0 {
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.entries) {
			entry := m.entries[idx]
			if entry.IsDir {
				targetPath = entry.Path
			}
		}
	}
	return targetPath
}

// launchDetachedCmd returns a Cmd that starts bin with args in a detached process
// and reports the result as TerminalOpenedMsg.
func launchDetachedCmd(bin string, args []string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command(bin, args...)
		return TerminalOpenedMsg{Error: cmd.Start()}
	}
}

// openTerminalWindow opens a new terminal window at the target directory.
// Resolution order:
//  1. config.App.TerminalCommand override → detached process (non-blocking)
//  2. Auto-detect from environment → detached process (non-blocking)
//  3. No fallback — shows a footer error (does not work on WSL)
func (m Model) openTerminalWindow() (tea.Model, tea.Cmd) {
	targetPath := m.resolveTargetPath()

	// Config override: e.g. "kitty --directory"
	if m.config.App.TerminalCommand != "" {
		parts := strings.Fields(m.config.App.TerminalCommand)
		return m, launchDetachedCmd(parts[0], append(parts[1:], targetPath))
	}

	// Auto-detect terminal from environment
	if bin, args, ok := detectTerminalCmd(targetPath); ok {
		return m, launchDetachedCmd(bin, args)
	}

	// No terminal detected (common on WSL) — report to user
	m.footerError = "No terminal detected — set App.TerminalCommand in config"
	return m, clearFooterInfoCmd()
}

// openTerminalInPlace suspends the TUI and opens a shell in the given directory.
func (m Model) openTerminalInPlace(targetPath string) (tea.Model, tea.Cmd) {
	shell := detectShell()
	if shell == "/bin/sh" {
		if _, err := os.Stat("/bin/bash"); err == nil {
			shell = "/bin/bash"
		}
	}
	cmd := exec.Command(shell)
	cmd.Dir = targetPath
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			log.Printf("ERROR [workspaces] terminal exit: %v", err)
		}
		return TerminalExitMsg{}
	})
}

// handleTerminalOpened handles the result of a non-blocking terminal launch.
func (m Model) handleTerminalOpened(msg TerminalOpenedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		log.Printf("ERROR [workspaces] open terminal: %v", msg.Error)
		m.footerError = "Failed to open terminal — check logs"
		return m, clearFooterInfoCmd()
	}
	return m, nil
}

// openIDE opens the selected entry in the configured IDE
func (m Model) openIDE() (tea.Model, tea.Cmd) {
	if len(m.entries) == 0 {
		path := m.currentPath
		if path == "" {
			path = m.getExpandedWorkspacesDir()
		}
		ideCmd := m.config.App.IDECommand

		return m, func() tea.Msg {
			cmd := exec.Command(ideCmd, path)
			err := cmd.Start()
			return IDEOpenedMsg{Error: err}
		}
	}

	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.entries) {
		return m, nil
	}

	entry := m.entries[idx]
	ideCmd := m.config.App.IDECommand

	return m, func() tea.Msg {
		cmd := exec.Command(ideCmd, entry.Path)
		err := cmd.Start()
		return IDEOpenedMsg{Error: err}
	}
}

// openInBrowser opens the selected git repo's remote URL in the default web browser
func (m Model) openInBrowser() (tea.Model, tea.Cmd) {
	if len(m.entries) == 0 {
		return m, nil
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.entries) {
		return m, nil
	}
	entry := m.entries[idx]
	url := entry.GitRemoteURL
	if url == "" {
		return m, nil
	}
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

// createWorkspace creates a new workspace directory
func (m Model) createWorkspace(name string) tea.Cmd {
	workspacesDir := m.getExpandedWorkspacesDir()

	return func() tea.Msg {
		baseDir := workspacesDir
		if m.currentPath != "" {
			baseDir = m.currentPath
		}

		if err := os.MkdirAll(baseDir, 0755); err != nil {
			return WorkspaceCreatedMsg{Error: err}
		}

		wsPath := filepath.Join(baseDir, name)
		if err := os.MkdirAll(wsPath, 0755); err != nil {
			return WorkspaceCreatedMsg{Error: err}
		}

		return WorkspaceCreatedMsg{Path: wsPath}
	}
}

// deleteEntry deletes a file or directory recursively
func (m Model) deleteEntry(path string) tea.Cmd {
	return func() tea.Msg {
		err := os.RemoveAll(path)
		if err != nil {
			return EntryDeletedMsg{Error: err}
		}
		return EntryDeletedMsg{Path: path}
	}
}

// updateTableSize adjusts table dimensions
func (m *Model) updateTableSize() {
	m.filterBar.Resize(m.width)
	// Rule 124: footer (tab bar + filter bar) is outside the viewport; subtract table header only
	tableDataHeight := max(m.height-1, 1)
	m.table.SetHeight(tableDataHeight)
	m.table.SetColumns(m.calculateColumns())
	// Force the Selected row style to width contentWidth so the selected background
	// extends to the right viewport border, even when row content visually differs
	// from the calculated width (e.g. Nerd Font icon width discrepancies).
	styles := theme.DefaultTableStyles()
	styles.Selected = styles.Selected.Width(m.width - 2)
	m.table.SetStyles(styles)
}

// updateTableData refreshes table rows from entries, applying active filters
func (m *Model) updateTableData() {
	query := strings.ToLower(m.filterBar.SearchQuery())

	rows := make([]table.Row, 0, len(m.entries))
	for _, entry := range m.entries {
		// Apply text search filter
		if query != "" {
			nameMatch := strings.Contains(strings.ToLower(entry.Name), query)
			remoteMatch := strings.Contains(strings.ToLower(entry.GitRemote), query)
			if !nameMatch && !remoteMatch {
				continue
			}
		}

		typeIcon := formatProjectType(entry)
		// if typeWidth > 0 && typeIcon != "" {
		// 	typeIcon = theme.CenterInColumn(typeIcon, typeWidth)
		// }
		sensitive, c, h, med, l, scanned := m.formatScanColumns(entry)
		rows = append(rows, table.Row{
			entry.Name,
			entry.GitRemote,
			formatGitStatus(entry),
			typeIcon,
			sensitive,
			c,
			h,
			med,
			l,
			scanned,
			timeAgo(entry.ModTime),
		})
	}
	m.table.SetRows(rows)
}

// loadEntries loads the contents of the current directory with enriched metadata
func (m Model) loadEntries() tea.Cmd {
	currentPath := m.currentPath
	workspacesDir := m.getExpandedWorkspacesDir()

	return func() tea.Msg {
		targetDir := workspacesDir
		if currentPath != "" {
			targetDir = currentPath
		}

		// Check if directory exists, create it if it doesn't (only for workspaces root)
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			if currentPath == "" {
				if err := os.MkdirAll(targetDir, 0755); err != nil {
					return LoadErrorMsg{Error: err}
				}
				return EntriesLoadedMsg{Entries: []Entry{}}
			}
			return LoadErrorMsg{Error: err}
		}

		dirEntries, err := os.ReadDir(targetDir)
		if err != nil {
			return LoadErrorMsg{Error: err}
		}

		entries := make([]Entry, 0, len(dirEntries))
		for _, dirEntry := range dirEntries {
			// Skip hidden files/directories
			if strings.HasPrefix(dirEntry.Name(), ".") {
				continue
			}

			info, err := dirEntry.Info()
			if err != nil {
				continue
			}

			entry := Entry{
				Name:    dirEntry.Name(),
				Path:    filepath.Join(targetDir, dirEntry.Name()),
				ModTime: info.ModTime(),
				IsDir:   dirEntry.IsDir(),
			}

			// Enrich directory entries with git and project type info
			if entry.IsDir {
				enrichEntry(&entry)
			}

			entries = append(entries, entry)
		}

		return EntriesLoadedMsg{Entries: entries}
	}
}

// enrichEntry populates git and project type metadata for a directory entry
func enrichEntry(entry *Entry) {
	entry.ProjectType = detectProjectType(entry.Path)
	detectGitStatus(entry)
	if entry.IsGitRepo {
		return
	}
	// For non-git directories, find nested git repos (depth ≤ 3)
	entry.SubRepoPaths = detectSubRepoPaths(entry.Path, 3)
}

// detectProjectType detects the project type by looking for signature files
func detectProjectType(path string) string {
	signatures := []struct {
		file     string
		projType string
	}{
		{"go.mod", "Go"},
		{"Cargo.toml", "Rust"},
		{"package.json", "Node"},
		{"pyproject.toml", "Python"},
		{"requirements.txt", "Python"},
		{"pom.xml", "Java"},
		{"build.gradle", "Java"},
		{"Gemfile", "Ruby"},
		{"composer.json", "PHP"},
		{"mix.exs", "Elixir"},
		{"Makefile", "Make"},
		{"Dockerfile", "Docker"},
	}

	for _, sig := range signatures {
		if _, err := os.Stat(filepath.Join(path, sig.file)); err == nil {
			return sig.projType
		}
	}
	return ""
}

// detectGitStatus populates git-related fields on the entry
func detectGitStatus(entry *Entry) {
	gitDir := filepath.Join(entry.Path, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return
	}
	entry.IsGitRepo = true

	// Get current branch
	if out, err := execGit(entry.Path, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		entry.GitBranch = strings.TrimSpace(out)
	}

	// Get remote URL: store display path and full normalized URL
	if out, err := execGit(entry.Path, "remote", "get-url", "origin"); err == nil {
		rawURL := strings.TrimSpace(out)
		entry.GitRemote = extractRemotePath(rawURL)
		entry.GitRemoteURL = normalizeRemoteURL(rawURL)
	}

	// Get modified/untracked counts from porcelain status
	if out, err := execGit(entry.Path, "status", "--porcelain"); err == nil {
		for line := range strings.SplitSeq(out, "\n") {
			if len(line) < 2 {
				continue
			}
			xy := line[:2]
			if xy == "??" {
				entry.GitUntracked++
			} else {
				entry.GitModified++
			}
		}
	}

	// Get unpushed commits count
	if out, err := execGit(entry.Path, "rev-list", "--count", "@{u}..HEAD"); err == nil {
		_, _ = fmt.Sscanf(strings.TrimSpace(out), "%d", &entry.GitUnpushed)
	}

	// Get unpulled commits count
	if out, err := execGit(entry.Path, "rev-list", "--count", "HEAD..@{u}"); err == nil {
		_, _ = fmt.Sscanf(strings.TrimSpace(out), "%d", &entry.GitUnpulled)
	}
}

// extractRemotePath strips the server from a git remote URL and returns only the path.
// Examples:
//
//	"https://gitlab.com/group/project.git" → "group/project"
//	"git@gitlab.com:group/project.git"     → "group/project"
func extractRemotePath(rawURL string) string {
	// SCP-like syntax: git@host:path — colon must not be followed by "//"
	if idx := strings.Index(rawURL, ":"); idx != -1 && !strings.Contains(rawURL[:idx], "/") {
		after := rawURL[idx+1:]
		if !strings.HasPrefix(after, "//") {
			return strings.TrimSuffix(after, ".git")
		}
	}
	// URL syntax: scheme://host/path
	if idx := strings.Index(rawURL, "://"); idx != -1 {
		rest := rawURL[idx+3:]
		if slash := strings.Index(rest, "/"); slash != -1 {
			return strings.TrimSuffix(rest[slash+1:], ".git")
		}
	}
	return rawURL
}

// normalizeRemoteURL converts any git remote URL to a plain HTTPS URL suitable for a browser.
// Examples:
//
//	"git@gitlab.com:group/project.git"            → "https://gitlab.com/group/project"
//	"https://token@gitlab.com/group/project.git"  → "https://gitlab.com/group/project"
func normalizeRemoteURL(rawURL string) string {
	// SCP-like syntax: git@host:path
	if idx := strings.Index(rawURL, ":"); idx != -1 && !strings.Contains(rawURL[:idx], "/") {
		after := rawURL[idx+1:]
		if !strings.HasPrefix(after, "//") {
			host := rawURL[:idx]
			if at := strings.Index(host, "@"); at != -1 {
				host = host[at+1:]
			}
			return "https://" + host + "/" + strings.TrimSuffix(after, ".git")
		}
	}
	// URL syntax: strip credentials and .git suffix
	if idx := strings.Index(rawURL, "://"); idx != -1 {
		scheme := rawURL[:idx]
		rest := rawURL[idx+3:]
		if at := strings.Index(rest, "@"); at != -1 {
			if slash := strings.Index(rest, "/"); slash == -1 || at < slash {
				rest = rest[at+1:]
			}
		}
		return scheme + "://" + strings.TrimSuffix(rest, ".git")
	}
	return rawURL
}

// execGit runs a git command in the given directory and returns stdout
func execGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// detectSubRepoPaths walks a directory up to maxDepth and returns paths of git repos found.
func detectSubRepoPaths(basePath string, maxDepth int) []string {
	var repos []string
	walkSubRepos(basePath, 0, maxDepth, &repos)
	return repos
}

// walkSubRepos is the recursive helper for detectSubRepoPaths
func walkSubRepos(dir string, depth, maxDepth int, repos *[]string) {
	if depth > maxDepth {
		return
	}
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		*repos = append(*repos, dir)
		return // don't recurse into nested repos
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		walkSubRepos(filepath.Join(dir, e.Name()), depth+1, maxDepth, repos)
	}
}

// openScanDetails opens the security details view for the selected git repo if it has cached results
func (m Model) openScanDetails() (tea.Model, tea.Cmd) {
	if len(m.entries) == 0 {
		return m, nil
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.entries) {
		return m, nil
	}
	entry := m.entries[idx]
	if !entry.IsGitRepo {
		return m, nil
	}
	if _, ok := m.scanCache[entry.Path]; !ok {
		return m, nil // no cached result, nothing to show
	}
	return m, func() tea.Msg {
		return ScanDetailsRequestMsg{RepoPath: entry.Path}
	}
}

// Messages

// EntriesLoadedMsg is sent when entries are loaded
type EntriesLoadedMsg struct {
	Entries []Entry
}

// LoadErrorMsg est envoyé en cas d'erreur
type LoadErrorMsg struct {
	Error error
}

// WorkspaceCreatedMsg est envoyé quand un workspace est créé
type WorkspaceCreatedMsg struct {
	Path  string
	Error error
}

// EntryDeletedMsg is sent when an entry is deleted
type EntryDeletedMsg struct {
	Path  string
	Error error
}

// EntryRenamedMsg is sent when an entry is renamed
type EntryRenamedMsg struct {
	OldPath string
	NewPath string
	Error   error
}

// IDEOpenedMsg est envoyé quand l'IDE est ouvert
type IDEOpenedMsg struct {
	Error error
}

// BrowserOpenedMsg is sent when the default browser has been launched
type BrowserOpenedMsg struct {
	Error error
}

// ScanRequestMsg is sent when user requests a security scan on a directory
type ScanRequestMsg struct {
	TargetPath string
}

// DirectorySelectedMsg is sent when a directory is selected in selection mode
type DirectorySelectedMsg struct {
	Path string
}

// SelectionCancelledMsg is sent when user cancels directory selection
type SelectionCancelledMsg struct{}

// WorkspaceScanStartingMsg is sent when a workspace scan begins for a repo path
type WorkspaceScanStartingMsg struct {
	RepoPath string
}

// WorkspaceScanCompleteMsg is sent by the security view when a workspace scan finishes
type WorkspaceScanCompleteMsg struct {
	RepoPath  string
	Critical  int
	High      int
	Medium    int
	Low       int
	Sensitive bool
	ScannedAt time.Time
	Error     error
}

// ScanDetailsRequestMsg is sent when the user wants to view scan details for a repo
type ScanDetailsRequestMsg struct {
	RepoPath string
}

// ScanCacheLoadedMsg is sent when the scan cache has been loaded from disk
type ScanCacheLoadedMsg struct {
	Cache map[string]cache.WorkspaceScanEntry
}

// TerminalExitMsg is sent when the interactive terminal process exits (in-place fallback)
type TerminalExitMsg struct{}

// TerminalOpenedMsg is sent after a non-blocking terminal launch attempt
type TerminalOpenedMsg struct {
	Error error
}

// clearFooterInfoMsg is sent after a delay to clear the transient footer info message.
type clearFooterInfoMsg struct{}
