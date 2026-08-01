package workspaces

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"gitlab.com/anthnell/devsecops/devdesk/internal/cache"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/help"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/shortcut"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/theme"
)

// Column fixed widths for the workspace table.
// colGitFixed: icon(1) + " "(1) + branch(~20) + up to 3 indicators × (icon+count+space)(4) = 28
const (
	colNameFixed      = 36
	colGitFixed       = 28
	colTypeFixed      = 4
	colSensitiveFixed = 7
	colCFixed         = 4
	colHFixed         = 4
	colMFixed         = 4
	colLFixed         = 4
	colScannedFixed   = 14
	colModFixed       = 15
)

// numColumns is the number of columns in the workspace table
const numColumns = 11

// defaultColumns returns the initial column definitions
func defaultColumns() []table.Column {
	return []table.Column{
		{Title: "Name", Width: colNameFixed},
		{Title: "Remote", Width: 20},
		{Title: "Git Status", Width: colGitFixed},
		{Title: "Type", Width: colTypeFixed},
		{Title: "Secrets", Width: colSensitiveFixed},
		{Title: "C", Width: colCFixed},
		{Title: "H", Width: colHFixed},
		{Title: "M", Width: colMFixed},
		{Title: "L", Width: colLFixed},
		{Title: "Scanned", Width: colScannedFixed},
		{Title: "Modified", Width: colModFixed},
	}
}

// calculateColumns returns table columns with dynamic widths based on terminal width
func (m *Model) calculateColumns() []table.Column {
	// Rule 116: contentWidth = width - 2, available = contentWidth - numColumns*2
	contentWidth := m.width - 2
	available := contentWidth - numColumns*2

	// Reserve all fixed column widths; Remote absorbs the remaining flex.
	// The LAST column (Modified) absorbs any leftover so that
	// sum(col_widths) == available exactly, ensuring the selected row background
	// extends to the right viewport border.
	fixedExceptRemoteAndMod := colNameFixed + colGitFixed + colTypeFixed + colSensitiveFixed + colCFixed + colHFixed + colMFixed + colLFixed + colScannedFixed
	remoteWidth := max(available-fixedExceptRemoteAndMod-colModFixed, 10)

	cols := m.table.Columns()
	if len(cols) < numColumns {
		cols = defaultColumns()
	}

	cols[0].Width = colNameFixed
	cols[1].Width = remoteWidth
	cols[2].Width = colGitFixed
	cols[3].Width = colTypeFixed
	cols[4].Width = colSensitiveFixed
	cols[5].Width = colCFixed
	cols[6].Width = colHFixed
	cols[7].Width = colMFixed
	cols[8].Width = colLFixed
	cols[9].Width = colScannedFixed
	// Last column absorbs any rounding/leftover so sum == available exactly
	cols[10].Width = available - colNameFixed - remoteWidth - colGitFixed - colTypeFixed - colSensitiveFixed - colCFixed - colHFixed - colMFixed - colLFixed - colScannedFixed
	if cols[10].Width < colModFixed {
		cols[10].Width = colModFixed
	}

	cols[1].Title = "Remote"
	cols[3].Title = "Type"
	cols[4].Title = "Secrets"
	cols[5].Title = "C"
	cols[6].Title = "H"
	cols[7].Title = "M"
	cols[8].Title = "L"
	cols[10].Title = "Modified"

	return cols
}

// View rend la vue
func (m Model) View() string {
	// Mode input - show input form overlay
	if m.mode == ModeAdding && m.input != nil {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.input.View(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground),
		)
	}

	// Mode confirm - show confirm modal overlay
	if m.mode == ModeConfirmingDelete && m.confirmModal != nil {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.confirmModal.View(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground),
		)
	}

	// Mode renaming - show rename input overlay
	if m.mode == ModeRenaming && m.input != nil {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.input.View(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground),
		)
	}

	// Content style with left padding (for non-table states only)
	contentStyle := lipgloss.NewStyle().Background(theme.ColorBackground).PaddingLeft(1)

	// Normal mode
	if m.error != "" {
		errorStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorError)
		return contentStyle.Render(errorStyle.Render(theme.IconError + " Error: " + m.error))
	}

	if len(m.entries) == 0 {
		if m.currentPath == "" {
			return contentStyle.Render(theme.HelpStyle.Render("\nNo workspaces found\n\nPress [n] to create a new workspace."))
		}
		return contentStyle.Render(theme.HelpStyle.Render("\nEmpty directory\n"))
	}

	return m.renderTable()
}

// renderTable renders the table content
func (m Model) renderTable() string {
	return m.table.View()
}

// GetFooterHeight returns the footer height for this view (Rule 124).
func (m Model) GetFooterHeight() int {
	switch m.mode {
	case ModeSelecting:
		return 3 // tab bar + empty line + info line (mirrors RenderFooter in ModeSelecting)
	case ModeNormal:
		if len(m.entries) > 0 && m.error == "" {
			return 3 + m.filterBar.ExtraHeight() // filter bar (when visible) + breadcrumb tab bar + empty line + info line
		}
	}
	return 2 // empty line + info line
}

// RenderFooter returns the footer content rendered below the viewport (Rule 124).
func (m Model) RenderFooter(width int) string {
	if m.mode == ModeSelecting {
		infoLine := theme.EmptyLineBg(width)
		if m.selectionMessage != "" {
			infoLine = lipgloss.NewStyle().
				Foreground(theme.ColorHighlight).
				Background(theme.ColorBackground).
				Width(width).
				Align(lipgloss.Center).
				Render(m.selectionMessage)
		}
		return m.renderTabBar() + "\n" + theme.EmptyLineBg(width) + "\n" + infoLine
	}
	if m.mode == ModeNormal && len(m.entries) > 0 && m.error == "" {
		var parts []string
		if m.filterBar.IsVisible() {
			parts = append(parts, m.filterBar.View())
		}
		infoLine := theme.EmptyLineBg(width)
		if m.footerError != "" {
			infoLine = theme.PadWithBg(theme.StatusErrorStyle.Render(m.footerError), width)
		} else if m.footerInfo != "" {
			infoLine = lipgloss.NewStyle().
				Foreground(theme.ColorHighlight).
				Background(theme.ColorBackground).
				Width(width).
				Align(lipgloss.Center).
				Render(m.footerInfo)
		}
		parts = append(parts, m.renderTabBar(), theme.EmptyLineBg(width), infoLine)
		return strings.Join(parts, "\n")
	}
	return theme.EmptyLineBg(width) + "\n" + theme.EmptyLineBg(width)
}

// renderTabBar renders the breadcrumb tab bar at the bottom of the viewport
func (m Model) renderTabBar() string {
	tabs := []theme.TabItem{{Label: theme.IconHome + " home"}}

	// Add navigation stack entries as tabs
	for _, path := range m.navigationStack {
		tabs = append(tabs, theme.TabItem{Label: theme.IconDirectory + " " + pathBaseName(path)})
	}

	// Add current directory as last tab (if not at root)
	if m.currentPath != "" {
		tabs = append(tabs, theme.TabItem{Label: theme.IconDirectory + " " + pathBaseName(m.currentPath)})
	}

	return theme.PadWithBg(theme.Bg(" ")+theme.RenderTabs(tabs, m.activeTabIndex), m.width)
}

// pathBaseName returns the last component of a path
func pathBaseName(path string) string {
	parts := strings.Split(path, string('/'))
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}

// formatGitStatus formats the git status column for a table row
func formatGitStatus(entry Entry) string {
	if entry.GitBranch == "" {
		if !entry.IsDir {
			return ""
		}
		return ""
	}

	var parts []string
	parts = append(parts, theme.IconGitBranch+" "+entry.GitBranch)

	if entry.GitModified > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", theme.IconGitModified, entry.GitModified))
	}
	if entry.GitUntracked > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", theme.IconGitUntracked, entry.GitUntracked))
	}
	if entry.GitUnpushed > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", theme.IconGitUnpushed, entry.GitUnpushed))
	}
	if entry.GitUnpulled > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", theme.IconGitUnpulled, entry.GitUnpulled))
	}

	return strings.Join(parts, " ")
}

// formatProjectType formats the project type column with Nerd Font icons
func formatProjectType(entry Entry) string {
	switch entry.ProjectType {
	case "Go":
		return theme.IconGo
	case "Rust":
		return theme.IconRust
	case "Node":
		return theme.IconNode
	case "Python":
		return theme.IconPython
	case "Java":
		return theme.IconJava
	case "Ruby":
		return theme.IconRuby
	case "PHP":
		return theme.IconPHP
	case "Elixir":
		return theme.IconElixir
	case "Make":
		return theme.IconTools
	case "Docker":
		return theme.IconDocker
	default:
		return ""
	}
}

// formatScanColumns returns the SENSITIVE, C, H, M, L, SCANNED column values for an entry.
// For git repos: looks up the scan cache directly.
// For directories: aggregates sub-repo scan entries.
func (m *Model) formatScanColumns(entry Entry) (sensitive, c, h, med, l, scanned string) {
	dash := "-"
	empty := ""

	if entry.IsGitRepo {
		if m.scanningPaths[entry.Path] {
			frame := spinner.Dot.Frames[m.spinnerFrameIdx%len(spinner.Dot.Frames)]
			return empty, dash, dash, dash, dash, frame + " scanning"
		}
		scanEntry, ok := m.scanCache[entry.Path]
		if !ok {
			return dash, dash, dash, dash, dash, dash
		}
		return formatSecrets(scanEntry.Sensitive, true),
			fmt.Sprintf("%d", scanEntry.Critical),
			fmt.Sprintf("%d", scanEntry.High),
			fmt.Sprintf("%d", scanEntry.Medium),
			fmt.Sprintf("%d", scanEntry.Low),
			theme.IconPending + " " + timeAgo(scanEntry.ScannedAt)
	}

	// Files are not scannable — return empty columns
	if !entry.IsDir {
		return empty, empty, empty, empty, empty, empty
	}

	// Directory: aggregate sub-repos
	if len(entry.SubRepoPaths) == 0 {
		return theme.IconWorkspaceUnknown, empty, empty, empty, empty, empty
	}

	// Count how many sub-repos are currently scanning
	scanningCount := 0
	for _, repoPath := range entry.SubRepoPaths {
		if m.scanningPaths[repoPath] {
			scanningCount++
		}
	}

	var scannedEntries []cache.WorkspaceScanEntry
	for _, repoPath := range entry.SubRepoPaths {
		if e, ok := m.scanCache[repoPath]; ok {
			scannedEntries = append(scannedEntries, e)
		}
	}

	if len(scannedEntries) == 0 && scanningCount == 0 {
		return dash, empty, empty, empty, empty, dash
	}
	if len(scannedEntries) == 0 && scanningCount > 0 {
		frame := spinner.Dot.Frames[m.spinnerFrameIdx%len(spinner.Dot.Frames)]
		return empty, dash, dash, dash, dash, frame + " scanning"
	}

	totalC, totalH, totalM, totalL := 0, 0, 0, 0
	hasSensitive := false
	for _, e := range scannedEntries {
		totalC += e.Critical
		totalH += e.High
		totalM += e.Medium
		totalL += e.Low
		if e.Sensitive {
			hasSensitive = true
		}
	}

	scannedCount := len(scannedEntries)
	totalCount := len(entry.SubRepoPaths)
	scanned = theme.IconDirectory + " " + fmt.Sprintf("%d/%d", scannedCount, totalCount)

	return formatSecrets(hasSensitive, scannedCount > 0),
		fmt.Sprintf("%d", totalC),
		fmt.Sprintf("%d", totalH),
		fmt.Sprintf("%d", totalM),
		fmt.Sprintf("%d", totalL),
		scanned
}

// formatSecrets returns the Secrets column icon as plain text (Rule 122: no ANSI in table cells)
func formatSecrets(sensitive bool, scanDone bool) string {
	if !scanDone {
		return theme.IconWorkspaceUnknown
	}
	if sensitive {
		return theme.IconWorkspaceUntrusted
	}
	return theme.IconWorkspaceTrusted
}

// timeAgo is a local alias for theme.TimeAgo (Rule 127).
func timeAgo(t time.Time) string { return theme.TimeAgo(t) }

// HeaderView interface implementation

func (m Model) GetShortcuts() shortcut.Shortcuts {
	// Mode input
	if m.mode == ModeAdding {
		return []shortcut.Shortcut{
			{Key: "enter", Description: "Create"},
			{Key: "esc", Description: "Cancel"},
		}
	}

	// Mode renaming
	if m.mode == ModeRenaming {
		return []shortcut.Shortcut{
			{Key: "enter", Description: "Rename"},
			{Key: "esc", Description: "Cancel"},
		}
	}

	// Mode confirm
	if m.mode == ModeConfirmingDelete {
		return []shortcut.Shortcut{
			{Key: "y/n", Description: "Confirm"},
			{Key: "esc", Description: "Cancel"},
		}
	}

	// Selection mode
	if m.mode == ModeSelecting {
		return []shortcut.Shortcut{
			{Key: "←→", Description: "Open/Back"},
			{Key: "enter", Description: "Select"},
			{Key: "esc", Description: "Back/Cancel"},
		}
	}

	// Normal mode — determine selected entry state for dynamic shortcuts (Rule 130)
	var selectedEntry *Entry
	if len(m.entries) > 0 {
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.entries) {
			selectedEntry = &m.entries[idx]
		}
	}

	isGitRepo := selectedEntry != nil && selectedEntry.IsGitRepo
	var hasScanResult bool
	if isGitRepo {
		_, hasScanResult = m.scanCache[selectedEntry.Path]
	}
	hasSubRepos := selectedEntry != nil && !isGitRepo && len(selectedEntry.SubRepoPaths) > 0

	shortcuts := []shortcut.Shortcut{
		{Key: "←→", Description: "Open/Back"},
	}

	// enter: only shown for git repos with cached scan results
	if hasScanResult {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "enter", Description: "Scan details"})
	}

	shortcuts = append(shortcuts,
		shortcut.Shortcut{Key: "t", Description: "Terminal (in-place)"},
		shortcut.Shortcut{Key: "T", Description: "Terminal (new window)"},
		shortcut.Shortcut{Key: "ctrl+o", Description: "IDE"},
	)

	// ctrl+w: only shown for git repos
	if isGitRepo {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "ctrl+w", Description: "Browser"})
	}

	// ctrl+s: shown for git repos and directories with nested repos
	if isGitRepo || hasSubRepos {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "ctrl+s", Description: "Scan"})
	}

	shortcuts = append(shortcuts,
		shortcut.Shortcut{Key: "A", Description: "Scan unscanned"},
		shortcut.Shortcut{Key: "ctrl+a", Description: "Scan all"},
	)

	// ctrl+n: hidden when a git repo is selected
	if !isGitRepo {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "ctrl+n", Description: "New directory"})
	}

	shortcuts = append(shortcuts,
		shortcut.Shortcut{Key: "r", Description: "Rename"},
		shortcut.Shortcut{Key: "ctrl+d", Description: "Delete"},
		shortcut.Shortcut{Key: "ctrl+r", Description: "Refresh"},
		shortcut.Shortcut{Key: "/", Description: "Search"},
		shortcut.Shortcut{Key: ":", Description: "Command"},
		shortcut.Shortcut{Key: "?", Description: "Help"},
	)

	return shortcuts
}

func (m Model) GetTitle() string {
	return theme.IconWorkspace + " Workspaces"
}

func (m Model) GetIcon() string {
	return ""
}

// GetHeaderInfo returns the key-value info for the header
func (m Model) GetHeaderInfo(context string) []shortcut.HeaderInfo {
	return []shortcut.HeaderInfo{
		{Key: "Context", Value: context, Style: theme.HeaderValueStyle},
	}
}

// GetHelpContent retourne le contenu d'aide de la vue Workspaces
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "Workspaces",
		Description: "A drill-down file browser for your local working directories. Workspaces are folders within the configured directory (" + m.config.App.WorkspacesDir + "). Navigate into directories with → and go back with ←. Tabs at the bottom show your current path.",
		KeyBindings: []help.KeyBinding{
			{Key: "↑/k", Description: "Move selection up"},
			{Key: "↓/j", Description: "Move selection down"},
			{Key: "→/l", Description: "Enter selected directory"},
			{Key: "←/h", Description: "Go to parent directory"},
			{Key: "g/Home", Description: "Go to top of list"},
			{Key: "G/End", Description: "Go to bottom of list"},
			{Key: "Esc", Description: "Go to parent directory"},
			{Key: "Enter", Description: "View scan details for selected git repo (requires cached scan result)"},
			{Key: "n", Description: "Create a new directory (at current level)"},
			{Key: "r", Description: "Rename the selected entry"},
			{Key: "ctrl+d", Description: "Delete the selected entry (with confirmation)"},
			{Key: "t", Description: "Open an in-place terminal at the selected directory (suspends TUI)"},
			{Key: "T", Description: "Open a new terminal window at the selected directory (not supported on WSL)"},
			{Key: "ctrl+o", Description: "Open in configured IDE"},
			{Key: "ctrl+w", Description: "Open git repo remote URL in the default web browser"},
			{Key: "ctrl+s", Description: "Launch a security scan on the selected directory (or all sub-repos for non-git dirs)"},
			{Key: "A", Description: "Scan all unscanned git repos visible in the current view"},
			{Key: "ctrl+a", Description: "Scan all git repos in the current view, purging cached results first"},
			{Key: "ctrl+r", Description: "Refresh the list"},
			{Key: ":", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Terminal",
				Body:  "Press t to open an in-place terminal at the selected directory (TUI suspends until you exit the shell). Press T (Shift+T) to open a new terminal window — auto-detected from the environment or set via App.TerminalCommand in config. Note: new window mode does not work on WSL.",
			},
			{
				Title: "Navigation",
				Body:  "The browser uses a drill-down model. Press → on a directory to see its contents. Press ← to go back. Tabs at the bottom show your current path.",
			},
			{
				Title: "Git Status",
				Body:  "Directories that are git repositories display their branch name and status indicators: modified files, untracked files, unpushed commits, and unpulled commits.",
			},
			{
				Title: "Open in Browser",
				Body:  "Press Ctrl+W on a git repo to open its remote URL in the default web browser. The Remote column shows the path (without the server hostname); Ctrl+W opens the full URL.",
			},
			{
				Title: "Security Scan",
				Body:  "Press Ctrl+S on a git repo to scan it. On a non-git directory, Ctrl+S scans all nested git repos in parallel. Press A to scan all unscanned repos visible in the current view, or Ctrl+A to rescan everything (purges cached results first). Scans run in parallel using up to half the available CPU cores. Results are saved to disk; the table shows CVE counts (C/H/M/L), a Secrets indicator, and the scan timestamp.",
			},
			{
				Title: "View Scan Details",
				Body:  "Press Enter on a git repo that has been scanned to open the Security view in detail mode, showing all findings from the cached scan result.",
			},
		},
	}
}
