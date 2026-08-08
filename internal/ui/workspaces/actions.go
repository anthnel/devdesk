package workspaces

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/scan"
)

// startSecurityScan launches a security scan on the selected entry using saved options.
// For git repos, scans the single repo. For directories with sub-repos, scans all sub-repos in parallel.
func (m Model) startSecurityScan() (tea.Model, tea.Cmd) {
	opts := scan.OptionsFromConfig(m.config.Scan)

	if len(m.table.Items()) == 0 {
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

	entry, ok := m.selectedEntry()
	if !ok || !entry.IsDir {
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
	return m, batchScanCmd(unscanned, scan.OptionsFromConfig(m.config.Scan))
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
	return m, tea.Batch(deleteScanCacheCmd(paths), batchScanCmd(paths, scan.OptionsFromConfig(m.config.Scan)))
}

// collectAllRepoPaths returns all git repo paths visible in the current view,
// including sub-repos nested within non-git directories.
func (m Model) collectAllRepoPaths() []string {
	var paths []string
	for _, entry := range m.entries() {
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
		m.refreshRows()
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
	m.refreshRows()
	return m, nil
}

// resolveTargetPath returns the path to open for terminal/IDE actions.
// Priority: selected directory entry > current browsed path > workspaces root.
func (m Model) resolveTargetPath() string {
	targetPath := m.getExpandedWorkspacesDir()
	if m.currentPath != "" {
		targetPath = m.currentPath
	}
	if entry, ok := m.selectedEntry(); ok && entry.IsDir {
		targetPath = entry.Path
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
	if len(m.table.Items()) == 0 {
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

	entry, ok := m.selectedEntry()
	if !ok {
		return m, nil
	}
	ideCmd := m.config.App.IDECommand

	return m, func() tea.Msg {
		cmd := exec.Command(ideCmd, entry.Path)
		err := cmd.Start()
		return IDEOpenedMsg{Error: err}
	}
}

// openInBrowser opens the selected git repo's remote URL in the default web browser
func (m Model) openInBrowser() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedEntry()
	if !ok {
		return m, nil
	}
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

// openScanDetails opens the security details view for the selected git repo if it has cached results
func (m Model) openScanDetails() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedEntry()
	if !ok {
		return m, nil
	}
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

// handleScanRequest rescans one repository by path, whoever asked.
//
// The router sends this when a cached scan's stored result has gone missing:
// the row is still in the list, and the scan that would replace it belongs
// here, next to the cache it writes. Rule 126's ctrl+s semantics — the entry is
// overwritten, not purged — because the result the user tried to open is
// precisely what is being replaced.
func (m Model) handleScanRequest(msg ScanRequestMsg) (tea.Model, tea.Cmd) {
	if msg.TargetPath == "" {
		return m, nil
	}
	if m.scanningPaths[msg.TargetPath] {
		m.footerInfo = "Scan already in progress"
		return m, clearFooterInfoCmd()
	}
	delete(m.scanCache, msg.TargetPath)
	return m, tea.Batch(
		deleteScanCacheCmd([]string{msg.TargetPath}),
		batchScanCmd([]string{msg.TargetPath}, scan.OptionsFromConfig(m.config.Scan)),
	)
}
