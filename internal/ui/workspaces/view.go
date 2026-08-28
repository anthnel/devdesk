package workspaces

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/git"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/fileicon"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The column widths, defaultColumns and calculateColumns used to live here.
// calculateColumns clamped Modified at 15 *after* handing it the remainder, so
// the columns overflowed the viewport by up to 34 at narrow widths — the shape
// datatable's TestTheWorkspacesLayoutFitsANarrowTerminal was written against.
// The widths are in columns.go now and the arithmetic is the solver's.

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

	// Mode scan-all - show the option modal overlay
	if m.mode == ModeConfirmingScanAll && m.scanAllModal != nil {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.scanAllModal.View(),
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

	if len(m.table.Items()) == 0 {
		if m.currentPath == "" {
			return contentStyle.Render(theme.HelpStyle.Render("\nNo workspaces found\n"))
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
		if len(m.table.Items()) > 0 && m.error == "" {
			// filter bar (when visible) + breadcrumb tab bar + empty line + info line
			return 3 + m.table.FilterBar().ExtraHeight()
		}
	}
	return 2 // empty line + info line
}

// RenderFooter returns the footer content rendered below the viewport (Rule 124).
func (m Model) RenderFooter(width int) string {
	if m.mode == ModeSelecting {
		infoLine := m.footer.View(width, sharedcomponents.Status{Text: m.selectionMessage})
		return m.renderTabBar() + "\n" + theme.EmptyLineBg(width) + "\n" + infoLine
	}
	if m.mode == ModeNormal && len(m.table.Items()) > 0 && m.error == "" {
		var parts []string
		if bar := m.table.FilterBar(); bar.IsVisible() {
			parts = append(parts, bar.View())
		}
		parts = append(parts, m.renderTabBar(), theme.EmptyLineBg(width), m.renderInfoLine(width))
		return strings.Join(parts, "\n")
	}
	// An empty listing, or one that failed to load, still gets its info line:
	// Rule 124 budgets one whatever it holds, and this branch used to render a
	// second blank line instead — so a footer message posted here was
	// swallowed. That is exactly where a refusal lands (Y on an empty listing
	// has nothing to copy), and a refusal nobody can read is worse than none.
	return theme.EmptyLineBg(width) + "\n" + m.renderInfoLine(width)
}

// renderInfoLine is the footer's one line of state.
//
// A sync's progress is read from the run rather than from footerInfo because a
// batch outlives the three seconds a footer message gets: a line set when the
// first repository started would clear while the tenth was still fetching. An
// error still wins over it — it is the thing that needs answering.
func (m Model) renderInfoLine(width int) string {
	return m.footer.View(width, sharedcomponents.Status{Text: m.syncStatusLine()})
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

// pathBaseName returns the last component of a filesystem path.
//
// It splits with filepath, not on "/". The paths here are the ones os.ReadDir
// and filepath.Join produced, so on Windows they are separated by backslashes —
// and a split on "/" alone found nothing to cut, leaving the breadcrumb reading
// `󰉋 C:\Users\anthoni\workspaces\anthnell  󰉋 C:\Users\anthoni\workspaces\anthnell\devsecops`
// instead of `󰉋 anthnell  󰉋 devsecops`. Three tabs of that overflow the line
// and say less than one word each would.
func pathBaseName(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Base(path)
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

// entryIcon is the glyph in the leftmost column: what this row *is*.
//
// It replaced a Type column that showed a *project* type — detectProjectType
// looked for go.mod or package.json and answered "this is a Go project", which
// is a different question and was judged not worth a column of its own.
//
// Three genres, and the first is the one that earns the column. Half the keys
// in this view act on IsGitRepo — S, F, A, D, enter — and Rule 130 shows or
// hides them accordingly, so the user watched the shortcuts change with nothing
// on the row saying why. Git Status betrays a repository only when it has a
// readable branch: one in detached HEAD, one that is empty, one git refuses to
// read all showed an empty cell and looked like any other directory.
//
// A directory holding *nested* repositories still reads as a plain directory.
// S, F and A act on it too, so a fourth glyph would be defensible; three is
// what was asked for, and the row already has no other way to say it either.
func entryIcon(entry Entry) string {
	switch {
	case entry.IsGitRepo:
		return theme.IconGitBranch
	case entry.IsDir:
		return theme.IconDirectory
	default:
		return fileicon.For(entry.Name)
	}
}

// formatScanColumns returns the SENSITIVE, C, H, M, L, SCANNED column values for an entry.
// For git repos: looks up the scan cache directly.
// For directories: aggregates sub-repo scan entries.
func (m *Model) formatScanColumns(entry Entry, frame string) (sensitive secretsCell, c, h, med, l, scanned string) {
	dash := "-"
	empty := ""
	none := secretsCell{}
	unscanned := secretsCell{Text: dash}

	if entry.IsGitRepo {
		if m.scanningPaths[entry.Path] {
			return none, dash, dash, dash, dash, frame + " scanning"
		}
		scanEntry, ok := m.scanCache[entry.Path]
		if !ok {
			return unscanned, dash, dash, dash, dash, dash
		}
		return secretsFor(theme.SecretsVerdict(scanEntry.Sensitive, true)),
			fmt.Sprintf("%d", scanEntry.Critical),
			fmt.Sprintf("%d", scanEntry.High),
			fmt.Sprintf("%d", scanEntry.Medium),
			fmt.Sprintf("%d", scanEntry.Low),
			theme.IconPending + " " + timeAgo(scanEntry.ScannedAt)
	}

	// Files are not scannable — return empty columns
	if !entry.IsDir {
		return none, empty, empty, empty, empty, empty
	}

	// Directory: aggregate sub-repos
	if len(entry.SubRepoPaths) == 0 {
		return secretsFor(theme.SecretsUnknown), empty, empty, empty, empty, empty
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
		return unscanned, empty, empty, empty, empty, dash
	}
	if len(scannedEntries) == 0 && scanningCount > 0 {
		return none, dash, dash, dash, dash, frame + " scanning"
	}

	totalC, totalH, totalM, totalL := 0, 0, 0, 0
	verdicts := make([]theme.SecretsState, 0, len(scannedEntries))
	for _, e := range scannedEntries {
		totalC += e.Critical
		totalH += e.High
		totalM += e.Medium
		totalL += e.Low
		verdicts = append(verdicts, theme.SecretsVerdict(e.Sensitive, true))
	}

	scannedCount := len(scannedEntries)
	totalCount := len(entry.SubRepoPaths)
	scanned = theme.IconDirectory + " " + fmt.Sprintf("%d/%d", scannedCount, totalCount)

	// Le répertoire ne porte que les dépôts scannés : ceux qui ne le sont pas
	// entrent dans le verdict par le compte ci-dessous, pas par un verdict à eux.
	if scannedCount < totalCount {
		verdicts = append(verdicts, theme.SecretsUnknown)
	}

	return secretsFor(foldSecrets(verdicts)),
		fmt.Sprintf("%d", totalC),
		fmt.Sprintf("%d", totalH),
		fmt.Sprintf("%d", totalM),
		fmt.Sprintf("%d", totalL),
		scanned
}

// ciCellFor is the CI grade of one row.
//
// **Only a repository is graded.** A directory aggregates its sub-repositories
// for the severity counters, because counts add up; letters do not — the worst
// of three grades is not the grade of anything, and an average is arithmetic on
// a scale that has none. So a directory shows nothing rather than a number
// nobody could act on.
//
// A repository whose remote is not this context's forge is *unavailable* rather
// than *never scanned*: a dash means "not yet", and this one never will be from
// here (§3.42). The two absences are told apart by the state, never by the
// printed cell.
func (m *Model) ciCellFor(entry Entry) ciCell {
	if !entry.IsGitRepo {
		return ciCell{State: theme.CIScoreUnavailable}
	}
	// GitRemoteURL, not GitRemote: the latter is the display path the Remote
	// column shows (`anthnel/devdesk`), which carries no host at all — a test
	// caught it reading every repository as ungradeable.
	gradeable := git.SameHost(entry.GitRemoteURL, m.config.Forge.URL)
	scanEntry, scanned := m.scanCache[entry.Path]
	if !scanned {
		return ciCell{State: theme.CIScoreVerdict(nil, false, gradeable)}
	}
	return ciCell{
		State: theme.CIScoreVerdict(scanEntry.CIScore, true, gradeable),
		Score: scanEntry.CIScore,
	}
}

// secretsCell is the Secrets column's two halves: what it prints, and the
// verdict that colours it.
//
// Les deux ne se déduisent pas l'un de l'autre. Le texte a des cas que le
// verdict n'a pas — un tiret pour un dépôt jamais scanné, rien du tout pour un
// fichier, rien non plus pendant un scan — et ces trois-là se colorent pareil,
// en dim, parce qu'ils disent tous « pas de verdict ».
type secretsCell struct {
	Text  string
	State theme.SecretsState
}

// secretsFor is the cell of a target that has a verdict. Texte brut (Rule 122) :
// la couleur passe par Style.
func secretsFor(state theme.SecretsState) secretsCell {
	return secretsCell{Text: theme.SecretsIcon(state), State: state}
}

// foldSecrets is the verdict a directory row carries for the repositories under
// it. Un secret trouvé l'emporte sur tout ; un dépôt que personne n'a regardé
// l'emporte sur « propre », parce qu'un parent ne peut pas être plus sûr que ce
// qu'on ignore de ses enfants.
func foldSecrets(states []theme.SecretsState) theme.SecretsState {
	out := theme.SecretsClean
	if len(states) == 0 {
		return theme.SecretsUnknown
	}
	for _, state := range states {
		switch state {
		case theme.SecretsFound:
			return theme.SecretsFound
		case theme.SecretsUnknown:
			out = theme.SecretsUnknown
		}
	}
	return out
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

	// Normal mode — every action of this mode is listed, in one order, and the
	// ones that do not apply to the current row are greyed out (Rule 130).
	//
	// The key sequence is therefore the same on every row: this column is read
	// out of the corner of the eye, and one that re-orders itself as the cursor
	// moves cannot be. What varies is Disabled, plus enter's wording — it opens
	// a file in the viewer and a scanned repository's findings, which are two
	// actions that cannot both apply.
	a := m.actions()

	enterDescription := "Scan details"
	if entry, ok := m.selectedEntry(); ok && !entry.IsDir {
		enterDescription = "View file"
	}

	return []shortcut.Shortcut{
		{Key: "←→", Description: "Open/Back"},
		{Key: "enter", Description: enterDescription, Disabled: !a.Enter.Enabled()},
		{Key: keymap.Terminal, Description: "Terminal"},
		{Key: keymap.IDE, Description: "IDE"},
		{Key: keymap.Web, Description: "Browser", Disabled: !a.Web.Enabled()},
		{Key: keymap.Scan, Description: "Scan", Disabled: !a.Scan.Enabled()},
		{Key: keymap.Fetch, Description: "Sync", Disabled: !a.Sync.Enabled()},
		{Key: keymap.ScanAll, Description: "Scan all", Disabled: !a.All.Enabled()},
		{Key: keymap.New, Description: "New directory"},
		{Key: keymap.Copy, Description: "Copy path", Disabled: !a.Copy.Enabled()},
		{Key: keymap.Rename, Description: "Rename", Disabled: !a.Rename.Enabled()},
		{Key: keymap.Delete, Description: "Delete", Disabled: !a.Delete.Enabled()},
		{Key: "ctrl+r", Description: "Refresh"},
		{Key: "/", Description: "Search"},
		{Key: "ctrl+p", Description: "Command"},
		{Key: "?", Description: "Help"},
	}
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
			{Key: "↑ / ↓", Description: "Move the selection"},
			{Key: "→", Description: "Enter selected directory"},
			{Key: "←", Description: "Go to parent directory"},
			{Key: "Esc", Description: "Go to parent directory"},
			{Key: "enter", Description: "Open a file in the viewer, or view scan details for a scanned git repo"},
			{Key: keymap.New, Description: "Create a new directory (at current level)"},
			{Key: keymap.Rename, Description: "Rename the selected entry (mv)"},
			{Key: keymap.Delete, Description: "Delete the selected entry (with confirmation). A large tree takes seconds: the row spins in the Git Status column meanwhile, and every action on it — a second D included — is refused until it finishes"},
			{Key: keymap.Terminal, Description: "Open a terminal at the selected directory. In place (suspends the TUI) or in a new window, per app.terminal_new_window — the window variant is not supported on WSL"},
			{Key: keymap.IDE, Description: "Open in configured IDE"},
			{Key: keymap.Web, Description: "Open git repo remote URL in the default web browser"},
			{Key: keymap.Scan, Description: "Launch a security scan on the selected directory (or all sub-repos for non-git dirs)"},
			{Key: keymap.Fetch, Description: "Sync the selected git repo (or all sub-repos for non-git dirs): fetch, then fast-forward"},
			{Key: keymap.Copy, Description: "Copy the selected entry's absolute path to the system clipboard — a directory or a file alike. It needs a selected row and has no fallback to the browsed directory, so it is offered only when there is one"},
			{Key: keymap.ScanAll, Description: "Scan every git repo in the current view. The confirmation carries a checkbox to purge the cached results first — unchecked, only what has never been scanned is scanned"},
			{Key: "ctrl+r", Description: "Refresh the list"},
			{Key: "/", Description: "Filter the list by name or git remote"},
			{Key: "ctrl+p", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Terminal",
				Body:  "Press T to open a terminal at the selected directory. What that means is a setting, not a second key: with app.terminal_new_window off (the default) the shell opens in place and the TUI suspends until you exit it; with it on, a separate terminal window is launched — auto-detected from the environment, or set app.terminal_command yourself. The window variant has nothing to open under WSL or through SSH, which is why it is a setting and off there.",
			},
			{
				Title: "Hidden files",
				Body:  "Entries whose name starts with a dot are left out by default. Set app.show_hidden_files in the configuration view (:config, app tab) to list them. There is no key for it here: the setting governs both what this view lists and the nested-repo discovery behind S, F and A, so with it on, a scan of a directory reaches repositories vendored under .venv or .terraform too.",
			},
			{
				Title: "Navigation",
				Body:  "The browser uses a drill-down model. Press → on a directory to see its contents. Press ← to go back. Tabs at the bottom show your current path.",
			},
			{
				Title: "Git Status",
				Body:  "Directories that are git repositories display their branch name and status indicators: modified files, untracked files, unpushed commits, and unpulled commits. The unpulled count comes from the local remote-tracking ref, so it is only as fresh as the last fetch — press F to bring it up to date.",
			},
			{
				Title: "Sync",
				Body:  "Press F on a git repo to fetch its remote and fast-forward the current branch. On a non-git directory, F syncs every nested git repo. Sync never merges, rebases, stashes or pushes: a repository with uncommitted changes, with local commits the remote does not have, or on a detached HEAD is fetched and then left exactly as it was, and the footer says which one it was and why. The fetch happens either way, so a repository it declines still ends up showing how far behind it really is.",
			},
			{
				Title: "Open in Browser",
				Body:  "Press W on a git repo to open its remote URL in the default web browser. The Remote column shows the path (without the server hostname); W opens the full URL.",
			},
			{
				Title: "CI Score",
				Body:  "With scan.enable_ci_score on, a CI column shows the grade plumber gave the repository's pipeline configuration: A to E. It is graded only when its remote is this context's forge — a repository hosted elsewhere shows an empty cell rather than a dash, because a dash means \"not scanned yet\" and this one never will be from here. A dash is a repository nobody has scanned; a question mark is a run that could not collect everything, and the CI tab of the results says which. A directory shows nothing: counts add up across nested repositories, letters do not.",
			},
			{
				Title: "Security Scan",
				Body:  "Press S on a git repo to scan it. On a non-git directory, S scans all nested git repos in parallel. Press A to scan every repo in the current view — its confirmation carries a checkbox to purge the cached results first, and without it only the never-scanned repos are scanned. Scans run in parallel using up to half the available CPU cores. Results are saved to disk; the table shows CVE counts (C/H/M/L), a Secrets indicator, and the scan timestamp.",
			},
			{
				Title: "View Scan Details",
				Body:  "Press Enter on a git repo that has been scanned to open the Security view in detail mode, showing all findings from the cached scan result.",
			},
			{
				Title: "Viewing files",
				Body: "Press Enter on a file to open it read-only in the viewer. A .json or .xml opens on a navigable tree — → expands a node, ← collapses it, f shows the raw text instead — and everything else opens as text. " +
					"c turns syntax coloring on and off, w wraps long lines, / searches, ctrl+r re-reads the file from disk, and Esc comes back here with the cursor where you left it. " +
					"A file over 5 MB or one that is not text is refused, and the footer says which; a malformed JSON or XML is not refused — it opens as text with a note, because it is the file you need to look at.",
			},
		},
	}
}
