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
	colNameMin        = 16
	colGitFixed       = 28
	colSensitiveFixed = 7
	colCFixed         = 4
	colHFixed         = 4
	colMFixed         = 4
	colLFixed         = 4
	colScannedFixed   = 14
	colModFixed       = 15
	colRemoteMin      = 10
	// colCIFixed is four cells for one letter. The title is what costs: "CI
	// Score" is eight to show an A, which is exactly why the four severity
	// columns are called C H M L. A fifth counting column beside them inherits
	// their convention rather than opening a second one.
	colCIFixed = 4
)

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
	Sensitive secretsCell
	CI        ciCell
	Critical  string
	High      string
	Medium    string
	Low       string
	Scanned   string
}

// workspaceColumns describes the workspaces table. Nothing sorts: the order is
// the directory's, and `.` stays inert rather than being wired to a comparator
// no one asked for. Name and Remote are what the filter has always matched.
// ciColumn is the CI grade. It exists only when scan.enable_ci_score is on:
// off, it would be four cells of nothing on every row for the life of the view,
// which says less than no column at all. The setting is read at construction
// and the router drops this view on a save, so the column follows.
//
// It does **not** declare Optional. Its four states include two absences that
// mean different things, and a column that disappeared when the terminal got
// narrow would add a third — "not shown" — indistinguishable from the other
// two. Fewer columns that are right beats every column wrong (§3.45), and this
// one is a verdict rather than a detail.
func ciColumn() datatable.Column[workspaceRow] {
	return datatable.Column[workspaceRow]{
		Title: "CI", Sizing: datatable.SizingFixed, MinWidth: colCIFixed,
		Cell:  func(r workspaceRow) string { return theme.CIScoreCell(r.CI.State, r.CI.Score) },
		Style: func(r workspaceRow) lipgloss.Style { return theme.CIScoreStyle(r.CI.State, r.CI.Score) },
	}
}

// ciCell is what the column prints and what decides its colour, kept apart so
// the colour is never read back off the rendered string — which is what the
// Secrets column used to do and what had to be undone.
type ciCell struct {
	State theme.CIScoreState
	Score *string
}

func workspaceColumns(withCI bool) []datatable.Column[workspaceRow] {
	text := func(title string, width int, cell func(workspaceRow) string) datatable.Column[workspaceRow] {
		return datatable.Column[workspaceRow]{Title: title, Sizing: datatable.SizingFixed, Optional: true, MinWidth: width, Cell: cell}
	}
	cols := []datatable.Column[workspaceRow]{
		{
			// No title: the column carries a glyph, and a header over it would
			// name something the user reads at a glance anyway — eza does not
			// print one either. It declares neither Less nor Search: it adds no
			// text anyone could type, so the filter stays on Name and Remote.
			Title: "", Sizing: datatable.SizingFixed, MinWidth: datatable.IconColumnWidth,
			Cell: func(r workspaceRow) string { return entryIcon(r.Entry) },
		},
		{
			Title: "Name", Sizing: datatable.SizingContent, MinWidth: colNameMin,
			Cell:   func(r workspaceRow) string { return r.Entry.Name },
			Search: func(r workspaceRow) string { return r.Entry.Name },
		},
		{
			Title: "Remote", Sizing: datatable.SizingContent, TruncateHead: true, MinWidth: colRemoteMin, Flex: 1,
			Cell:   func(r workspaceRow) string { return r.Entry.GitRemote },
			Search: func(r workspaceRow) string { return r.Entry.GitRemote },
		},
		{
			Title: "Git Status", Sizing: datatable.SizingFixed, MinWidth: colGitFixed,
			Cell:  func(r workspaceRow) string { return r.GitStatus },
			Style: gitStatusStyle,
		},
		{
			Title: "Secrets", Sizing: datatable.SizingFixed, MinWidth: colSensitiveFixed,
			Cell:  func(r workspaceRow) string { return r.Sensitive.Text },
			Style: func(r workspaceRow) lipgloss.Style { return theme.SecretsStyle(r.Sensitive.State) },
		},
		count("C", "CRITICAL", colCFixed, func(r workspaceRow) string { return r.Critical }),
		count("H", "HIGH", colHFixed, func(r workspaceRow) string { return r.High }),
		count("M", "MEDIUM", colMFixed, func(r workspaceRow) string { return r.Medium }),
		count("L", "LOW", colLFixed, func(r workspaceRow) string { return r.Low }),
		text("Scanned", colScannedFixed, func(r workspaceRow) string { return r.Scanned }),
		text("Modified", colModFixed, func(r workspaceRow) string { return timeAgo(r.Entry.ModTime) }),
	}
	if withCI {
		// Beside the four severity counters, which is where a reader looks for
		// what a scan concluded — not at the end, past Scanned and Modified.
		cols = append(cols[:len(cols)-2], append([]datatable.Column[workspaceRow]{ciColumn()}, cols[len(cols)-2:]...)...)
	}
	return cols
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
		Title: title, Sizing: datatable.SizingFixed, MinWidth: width,
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
	// Aucune opinion : c'est la table qui pose la couleur de texte du thème.
	return lipgloss.NewStyle()
}

// rowsFor decorates the entries with the scan state the table shows.
func (m *Model) rowsFor(entries []Entry) []workspaceRow {
	frame := spinner.Dot.Frames[m.spinnerFrameIdx%len(spinner.Dot.Frames)]

	rows := make([]workspaceRow, 0, len(entries))
	for _, entry := range entries {
		sensitive, c, h, med, l, scanned := m.formatScanColumns(entry, frame)
		gitStatus := formatGitStatus(entry)
		// A row can only be held by one of the two — busy() is what keeps them
		// apart — so the order below decides nothing. The delete spends this
		// cell for the same reason the sync does, and with less to lose: a
		// directory on its way out has no git status left to announce.
		switch {
		case m.deletingPaths[entry.Path]:
			gitStatus = frame + " deleting"
		case m.syncingPaths[entry.Path]:
			gitStatus = frame + " syncing"
		}
		rows = append(rows, workspaceRow{
			Entry:     entry,
			GitStatus: gitStatus,
			Sensitive: sensitive,
			CI:        m.ciCellFor(entry),
			Critical:  c,
			High:      h,
			Medium:    med,
			Low:       l,
			Scanned:   scanned,
		})
	}
	return rows
}
