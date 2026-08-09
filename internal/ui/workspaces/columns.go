package workspaces

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
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
	colRemoteMin      = 10
)

// numColumns is the number of columns in the workspace table
const numColumns = 11

// workspaceRow is one line of the table: the entry, plus the six scan cells.
// Those depend on the scan cache, on what is currently scanning and on the
// spinner frame — none of which lives on an Entry, and none of which the
// columns can reach, since they are built once in New. Same shape as
// oci_resources' imageRow, and for the same reason.
type workspaceRow struct {
	Entry Entry
	// GitStatus is the branch and its counts, or the spinner while a sync is
	// rewriting exactly those counts. Showing the stale numbers under a
	// spinner elsewhere in the row would be showing the state being replaced.
	GitStatus string
	Sensitive string
	Critical  string
	High      string
	Medium    string
	Low       string
	Scanned   string
}

// workspaceColumns describes the workspaces table. Nothing sorts: the order is
// the directory's, and `.` stays inert rather than being wired to a comparator
// no one asked for. Name and Remote are what the filter has always matched.
func workspaceColumns() []datatable.Column[workspaceRow] {
	text := func(title string, width int, cell func(workspaceRow) string) datatable.Column[workspaceRow] {
		return datatable.Column[workspaceRow]{Title: title, MinWidth: width, Cell: cell}
	}
	return []datatable.Column[workspaceRow]{
		{
			Title: "Name", MinWidth: colNameFixed,
			Cell:   func(r workspaceRow) string { return r.Entry.Name },
			Search: func(r workspaceRow) string { return r.Entry.Name },
		},
		{
			Title: "Remote", MinWidth: colRemoteMin, Flex: 1,
			Cell:   func(r workspaceRow) string { return r.Entry.GitRemote },
			Search: func(r workspaceRow) string { return r.Entry.GitRemote },
		},
		{
			Title: "Git Status", MinWidth: colGitFixed,
			Cell:  func(r workspaceRow) string { return r.GitStatus },
			Style: gitStatusStyle,
		},
		text("Type", colTypeFixed, func(r workspaceRow) string { return formatProjectType(r.Entry) }),
		{
			Title: "Secrets", MinWidth: colSensitiveFixed,
			Cell:  func(r workspaceRow) string { return r.Sensitive },
			Style: secretsStyle,
		},
		count("C", "CRITICAL", colCFixed, func(r workspaceRow) string { return r.Critical }),
		count("H", "HIGH", colHFixed, func(r workspaceRow) string { return r.High }),
		count("M", "MEDIUM", colMFixed, func(r workspaceRow) string { return r.Medium }),
		count("L", "LOW", colLFixed, func(r workspaceRow) string { return r.Low }),
		text("Scanned", colScannedFixed, func(r workspaceRow) string { return r.Scanned }),
		text("Modified", colModFixed, func(r workspaceRow) string { return timeAgo(r.Entry.ModTime) }),
	}
}

// count builds one of the four severity columns. The cell is already formatted
// by formatScanColumns, so the colour is decided from what it printed: "-" for
// a repository never scanned, "" for something that cannot be, and a number
// otherwise.
//
// Only a non-zero count is coloured. A clean repository showing four coloured
// zeroes reads as four problems at a glance, which is the opposite of what the
// colour is for.
func count(title, severity string, width int, cell func(workspaceRow) string) datatable.Column[workspaceRow] {
	return datatable.Column[workspaceRow]{
		Title: title, MinWidth: width,
		Cell: cell,
		Style: func(r workspaceRow) lipgloss.Style {
			switch cell(r) {
			case "", "-", "0":
				return theme.DimStyle
			}
			return theme.SeverityTextStyle(severity)
		},
	}
}

// gitStatusStyle warns when the working tree holds work that is not committed.
//
// It is the same condition `s` refuses to sync on, so the colour says in
// advance what the sync would have reported: a repository with uncommitted
// changes is skipped, untracked files included.
func gitStatusStyle(r workspaceRow) lipgloss.Style {
	switch {
	case r.Entry.GitBranch == "":
		return theme.DimStyle
	case r.Entry.GitModified > 0 || r.Entry.GitUntracked > 0:
		return theme.StatusWarningStyle
	}
	return lipgloss.NewStyle().Foreground(theme.ColorText)
}

// secretsStyle colours the one cell that reports something found rather than
// something counted: the icon is a verdict, and an untrusted repository is the
// only state in this table worth reading before the counts.
func secretsStyle(r workspaceRow) lipgloss.Style {
	switch r.Sensitive {
	case theme.IconWorkspaceUntrusted:
		return theme.StatusErrorStyle
	case theme.IconWorkspaceTrusted:
		return theme.StatusOKStyle
	}
	return theme.DimStyle
}

// rowsFor decorates the entries with the scan state the table shows.
func (m *Model) rowsFor(entries []Entry) []workspaceRow {
	frame := spinner.Dot.Frames[m.spinnerFrameIdx%len(spinner.Dot.Frames)]

	rows := make([]workspaceRow, 0, len(entries))
	for _, entry := range entries {
		sensitive, c, h, med, l, scanned := m.formatScanColumns(entry, frame)
		gitStatus := formatGitStatus(entry)
		if m.syncingPaths[entry.Path] {
			gitStatus = frame + " syncing"
		}
		rows = append(rows, workspaceRow{
			Entry:     entry,
			GitStatus: gitStatus,
			Sensitive: sensitive,
			Critical:  c,
			High:      h,
			Medium:    med,
			Low:       l,
			Scanned:   scanned,
		})
	}
	return rows
}
