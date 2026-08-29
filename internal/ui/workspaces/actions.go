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
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
	"github.com/anthnel/devdesk/internal/viewer"
)

// startSecurityScan launches a security scan on the selected entry using saved options.
// For git repos, scans the single repo. For directories with sub-repos, scans all sub-repos in parallel.
func (m Model) startSecurityScan() (tea.Model, tea.Cmd) {
	opts := m.scanOptions()

	if len(m.table.Items()) == 0 {
		// Fallback: scan the current directory path
		targetPath := m.currentPath
		if targetPath == "" {
			targetPath = m.getExpandedWorkspacesDir()
		}
		if targetPath == "" {
			return m, nil
		}
		if m.busy(targetPath) {
			return m, m.footer.Warn(busyMessage)
		}
		delete(m.scanCache, targetPath)
		return m, purgeAndScan(m.scanRun([]string{targetPath}), []string{targetPath}, opts)
	}

	entry, ok := m.selectedEntry()
	if !ok || !entry.IsDir {
		return m, nil
	}

	if entry.IsGitRepo {
		if m.busy(entry.Path) {
			return m, m.footer.Warn(busyMessage)
		}
		delete(m.scanCache, entry.Path)
		return m, purgeAndScan(m.scanRun([]string{entry.Path}), []string{entry.Path}, opts)
	}

	// Non-git directory: scan all non-scanning nested git repos in parallel
	if len(entry.SubRepoPaths) == 0 {
		return m, nil
	}

	var toScan []string
	for _, repoPath := range entry.SubRepoPaths {
		if !m.busy(repoPath) {
			toScan = append(toScan, repoPath)
		}
	}

	if len(toScan) == 0 {
		return m, m.footer.Warn(busyMessage)
	}

	for _, path := range toScan {
		delete(m.scanCache, path)
	}
	return m, tea.Batch(
		purgeAndScan(m.scanRun(toScan), toScan, opts),
		m.warnSkipped(entry.SubRepoSkipped),
	)
}

// warnSkipped says how much of the tree the walk behind this action could not
// look at, and says nothing when it could.
//
// A Warn rather than an Error (Rule 128): nothing failed, the request simply
// cannot be honoured in full. That number is what turns D59 from silent into
// merely annoying — the footer could say how many repositories were about to be
// scanned and nothing at all about how many it had failed to look for, so a
// missing repository was indistinguishable from a tree that has none.
//
// Silence when skipped is zero is the point: a line on every scan is a line
// nobody reads by the third one.
func (m *Model) warnSkipped(skipped int) tea.Cmd {
	if skipped == 0 {
		return nil
	}
	return m.footer.Warn(sharedcomponents.Plural(skipped, "directory", "directories") +
		" could not be read — some repositories were missed")
}

// skippedInView totals what every walk in the current listing had to leave out.
// A and ctrl+a act on the view rather than on a row, so the number they answer
// for is the view's.
func (m Model) skippedInView() int {
	total := 0
	for _, entry := range m.entries() {
		total += entry.SubRepoSkipped
	}
	return total
}

// confirmScanAll asks before scanning everything, and the purge is the modal's
// checkbox rather than a second key.
//
// A and ctrl+a differed only by a modifier, and nothing in their shape said
// which one purged the cache — the closest this application came to losing data
// by accident (§3.26). The destructive half is a deliberate gesture now.
func (m Model) confirmScanAll() (tea.Model, tea.Cmd) {
	if len(m.collectAllRepoPaths()) == 0 {
		return m, nil
	}
	m.mode = ModeConfirmingScanAll
	m.scanAllModal = sharedcomponents.NewOptionConfirmModal(
		"Scan All",
		"Scan every repository below this level?",
		"Purge cached results first (rescans everything)",
	)
	return m, nil
}

// scanAll scans what is below the cursor: everything when the cache was purged,
// only what has never been scanned otherwise.
func (m Model) scanAll(purge bool) (tea.Model, tea.Cmd) {
	if purge {
		return m.requestScanAll()
	}
	return m.scanAllUnscanned()
}

// scanAllUnscanned triggers batch scanning of all unscanned git repos visible in the current view.
func (m Model) scanAllUnscanned() (tea.Model, tea.Cmd) {
	paths := m.collectAllRepoPaths()
	var unscanned []string
	for _, path := range paths {
		if _, ok := m.scanCache[path]; !ok && !m.busy(path) {
			unscanned = append(unscanned, path)
		}
	}
	if len(unscanned) == 0 {
		return m, nil
	}
	return m, tea.Batch(
		jobs.StartInContext(m.scanRun(unscanned), func(contextName string) tea.Cmd {
			return batchScanCmd(unscanned, m.scanOptions(), contextName)
		}),
		m.warnSkipped(m.skippedInView()),
	)
}

// requestScanAll triggers batch scanning of all git repos visible in the current view,
// purging in-memory and disk cache first (Rule 126).
func (m Model) requestScanAll() (tea.Model, tea.Cmd) {
	// A busy repository is left out of the purge as well as the rescan: purging
	// it would blank its counts with nothing on the way to replace them.
	var paths []string
	for _, path := range m.collectAllRepoPaths() {
		if !m.busy(path) {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return m, nil
	}
	// Purge in-memory cache synchronously (Update() is thread-safe)
	for _, path := range paths {
		delete(m.scanCache, path)
	}
	return m, tea.Batch(
		purgeAndScan(m.scanRun(paths), paths, m.scanOptions()),
		m.warnSkipped(m.skippedInView()),
	)
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
	if msg.Error != nil {
		log.Printf("ERROR [workspaces] scan complete %s: %v", msg.RepoPath, msg.Error)
		m.refreshRows()
		return m, m.footer.Error("Scan failed — check logs")
	}
	m.footer.Clear()
	m.scanCache[msg.RepoPath] = cache.WorkspaceScanEntry{
		RepoPath:  msg.RepoPath,
		Critical:  msg.Critical,
		High:      msg.High,
		Medium:    msg.Medium,
		Low:       msg.Low,
		Sensitive: msg.Sensitive,
		CIScore:   msg.CIScore,
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

// openTerminal opens a shell at the target directory, in place or in a window
// of its own depending on app.terminal_new_window.
//
// One key, one meaning: `t` and `T` used to be the two variants, which spent a
// second letter on a choice that is a property of the environment rather than
// of the moment — there is no window to open under WSL or through SSH (§3.26).
func (m Model) openTerminal() (tea.Model, tea.Cmd) {
	if m.config.App.TerminalNewWindow {
		return m.openTerminalWindow()
	}
	return m.openTerminalInPlace(m.resolveTargetPath())
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
	return m, m.footer.Warn("No terminal detected — set App.TerminalCommand in config")
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
		return m, m.footer.Error("Failed to open terminal — check logs")
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

// copyPath puts the selected entry's absolute path on the system clipboard.
//
// It is the selected row and nothing else — no fallback to the browsed
// directory, the way T and O have one. Those act on a place, so "here" is a
// sensible answer when no row is selected; this one answers "what is that
// row", and a copy that silently hands back the parent directory is a wrong
// answer rather than a missing one. Rule 130 hides Y when there is no row.
func (m Model) copyPath() (tea.Model, tea.Cmd) {
	path, ok := m.copyTarget()
	if !ok {
		return m, nil
	}
	return m, copyPathCmd(path)
}

// copyTarget names what Y will put on the clipboard. It is split from copyPath
// so a test can check which row was chosen without running the Cmd: running it
// would write to the machine's real clipboard, which is neither the test's to
// take nor available on a headless runner.
func (m Model) copyTarget() (string, bool) {
	entry, ok := m.selectedEntry()
	if !ok {
		return "", false
	}
	return entry.Path, true
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

// deleteEntry deletes a file or directory recursively.
//
// One construction site rather than a branch per outcome: Path is what Update
// clears the deleting marker by, so a message that omitted it on the failure
// would strand the row as busy for the life of the view. Written this way the
// omission is unexpressible.
func (m Model) deleteEntry(path string) tea.Cmd {
	return func() tea.Msg {
		return EntryDeletedMsg{Path: path, Error: os.RemoveAll(path)}
	}
}

// openScanDetails is what `enter` does: it opens a file in the document viewer,
// or a scanned repository's findings.
//
// The two cannot collide — a file is never a git repository — which is why one
// key carries both rather than a second one being found for reading a file.
func (m Model) openScanDetails() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedEntry()
	if !ok {
		return m, nil
	}
	if !entry.IsDir {
		// The read itself happens in the viewer's Init, so what is too large or
		// not text is decided and reported in one place rather than at each
		// producer.
		source := viewer.NewFileSource(entry.Path)
		return m, func() tea.Msg { return uiviewer.OpenRequestMsg{Source: source} }
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
	if m.busy(msg.TargetPath) {
		return m, m.footer.Warn(busyMessage)
	}
	delete(m.scanCache, msg.TargetPath)
	targets := []string{msg.TargetPath}
	return m, purgeAndScan(m.scanRun(targets), targets, m.scanOptions())
}

// purgeAndScan is the launch every ctrl+s and ctrl+a path shares: the disk
// entries go, the run is registered, and the scan that replaces them starts.
//
// The three travel together because they must reach the same cache. The purge
// used to sit beside the run in a tea.Batch, reading the current context on its
// own goroutine, which left one command of a launch answering to a name the
// other two did not share (D68).
func purgeAndScan(run jobs.Run, paths []string, opts scan.ScanOptions) tea.Cmd {
	return jobs.StartInContext(run, func(contextName string) tea.Cmd {
		return tea.Batch(
			deleteScanCacheCmd(paths, contextName),
			batchScanCmd(paths, opts, contextName),
		)
	})
}
