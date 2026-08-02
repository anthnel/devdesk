package workspaces

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

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

	// bubbles does not clamp the cursor when the row count shrinks, so drilling
	// into a smaller directory — or narrowing the filter — would leave it past
	// the end. Nothing is highlighted then, and every action that resolves the
	// selection (enter, ctrl+d, r, ctrl+s) silently does nothing.
	if m.table.Cursor() >= len(rows) {
		m.table.SetCursor(max(len(rows)-1, 0))
	}
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
