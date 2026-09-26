package workspaces

import (
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Column fixed widths for the workspace table.
// colGitFixed: branch(~20) + " " + a couple of starship markers (mark+count) = 28
const (
	colNameMin        = 16
	colGitFixed       = 28
	colLastTagFixed   = 10
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
	// colMisconfigFixed is five cells: "CFG" costs three, and the value costs
	// four at worst — three digits and the "?" that says the count is partial.
	colMisconfigFixed = 5
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
	// branchCut is how many runes of GitStatus are the branch, which Style
	// colours; TailStyle colours the markers after it. The whole cell while a
	// spinner holds it — there is no branch then, only a status.
	branchCut int
	Sensitive secretsCell
	CI        ciCell
	Misconfig misconfigCell
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

// misconfigColumn is the misconfiguration count. Like ciColumn it exists only
// when its category is on — off, it would be a dash on every row for the life
// of the view — and like it, it does not declare Optional: a column that
// disappeared on a narrow terminal would be a second absence looking like the
// "-" that means "nobody looked".
//
// It sits after the four severity counters, which count vulnerabilities only,
// and before the grade: that is the order the security inventory uses, and one
// reading order for the same six columns across two views is worth more than
// either placement on its own.
func misconfigColumn() datatable.Column[workspaceRow] {
	return datatable.Column[workspaceRow]{
		Title: "CFG", Sizing: datatable.SizingFixed, MinWidth: colMisconfigFixed,
		Cell: func(r workspaceRow) string { return r.Misconfig.Text },
		Style: func(r workspaceRow) lipgloss.Style {
			return theme.MisconfigStyle(r.Misconfig.State, r.Misconfig.Worst)
		},
	}
}

// misconfigCell is the column's two halves, on secretsCell's model: what it
// prints, and what colours it. The two are not derived from one another — the
// text has cases the state does not, a file and a directory holding no
// repository print nothing at all while a repository nobody scanned prints a
// dash, and all of them colour the same way.
type misconfigCell struct {
	Text  string
	State theme.MisconfigState
	Worst string
}

func workspaceColumns(withCI, withMisconfig bool) []datatable.Column[workspaceRow] {
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
			Cell:  func(r workspaceRow) string { return entryIcon(r.Entry) },
			Style: func(r workspaceRow) lipgloss.Style { return theme.IconStyle(entryIconRole(r.Entry)) },
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
			Cell:      func(r workspaceRow) string { return r.GitStatus },
			Style:     gitBranchStyle,
			Cut:       func(r workspaceRow) int { return r.branchCut },
			TailStyle: gitMarkersStyle,
		},
		{
			Title: "Last Tag", Sizing: datatable.SizingFixed, Optional: true, MinWidth: colLastTagFixed,
			Cell:  func(r workspaceRow) string { return r.Entry.GitLastTag },
			Style: lastTagStyle,
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
	// Beside the four severity counters, which is where a reader looks for what
	// a scan concluded — not at the end, past Scanned and Modified. Both go in
	// before the last two columns, misconfigurations first, so the order matches
	// the security inventory's.
	var verdicts []datatable.Column[workspaceRow]
	if withMisconfig {
		verdicts = append(verdicts, misconfigColumn())
	}
	if withCI {
		verdicts = append(verdicts, ciColumn())
	}
	if len(verdicts) > 0 {
		tail := append(verdicts, cols[len(cols)-2:]...)
		cols = append(cols[:len(cols)-2], tail...)
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

// gitBranchStyle colours the branch half of the Git Status cell, and the
// whole cell while a spinner holds it. A branch name is not a verdict, so it
// keeps the table's text colour; only an empty cell is dimmed.
func gitBranchStyle(r workspaceRow) lipgloss.Style {
	if r.Entry.GitBranch == "" {
		return theme.DimStyle
	}
	// No opinion here: the table sets the theme's text color.
	return lipgloss.NewStyle()
}

// gitMarkersStyle warns when F would refuse the repository — uncommitted
// work, untracked files included, or a branch that diverged — so the colour
// says in advance what the sync would report. Anything else is information,
// not a warning, and keeps the text colour: a repository one commit behind is
// the reason F exists, not a problem.
func gitMarkersStyle(r workspaceRow) lipgloss.Style {
	e := r.Entry
	if e.GitModified > 0 || e.GitUntracked > 0 || (e.GitUnpushed > 0 && e.GitUnpulled > 0) {
		return theme.StatusWarningStyle
	}
	return lipgloss.NewStyle()
}

// lastTagStyle dims the cell for the common case — a repository with no tag
// reachable from HEAD — the same way count() dims a zero count, rather than
// leaving an untagged repository looking identical to a tagged one in
// ordinary text color.
func lastTagStyle(r workspaceRow) lipgloss.Style {
	if r.Entry.GitLastTag == "" {
		return theme.DimStyle
	}
	return lipgloss.NewStyle()
}

// rowsFor decorates the entries with the scan state the table shows.
func (m *Model) rowsFor(entries []Entry) []workspaceRow {
	frame := m.jobFrame

	rows := make([]workspaceRow, 0, len(entries))
	for _, entry := range entries {
		sensitive, misc, c, h, med, l, scanned := m.formatScanColumns(entry, frame)
		gitStatus := formatGitStatus(entry)
		branchCut := utf8.RuneCountInString(entry.GitBranch)
		// A row can only be held by one of the two — busy() is what keeps them
		// apart — so the order below decides nothing. The delete spends this
		// cell for the same reason the sync does, and with less to lose: a
		// directory on its way out has no git status left to announce.
		switch {
		case m.deleting(entry.Path):
			gitStatus = frame + " deleting"
			branchCut = utf8.RuneCountInString(gitStatus)
		case m.syncing(entry.Path):
			gitStatus = frame + " syncing"
			branchCut = utf8.RuneCountInString(gitStatus)
		}
		rows = append(rows, workspaceRow{
			Entry:     entry,
			GitStatus: gitStatus,
			branchCut: branchCut,
			Sensitive: sensitive,
			CI:        m.ciCellFor(entry),
			Misconfig: misc,
			Critical:  c,
			High:      h,
			Medium:    med,
			Low:       l,
			Scanned:   scanned,
		})
	}
	return rows
}
