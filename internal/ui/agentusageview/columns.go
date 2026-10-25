package agentusageview

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

const (
	// noProject stands for a session whose transcript named no working directory.
	noProject = "-"
	// lastActiveLayout is the local date and time of the Last active column.
	lastActiveLayout = "2006-01-02 15:04"
	// sessionIDPrefix is how much of the ID the Session column shows.
	sessionIDPrefix = 8
)

// sessionColumns describes the sessions table.
//
// Project is the full path, home folded, cut from the left when it does not fit
// so the leaf — the part that names the project — stays visible. Session is the
// first thing to go on a narrow terminal.
func sessionColumns() []datatable.Column[sessionRow] {
	return []datatable.Column[sessionRow]{
		{
			Title: "Project", Sizing: datatable.SizingContent,
			MinWidth: 20, Flex: 3, TruncateHead: true,
			Cell:   projectCell,
			Style:  projectStyle,
			Search: func(r sessionRow) string { return r.project },
			Less:   func(a, b sessionRow) bool { return a.project < b.project },
		},
		{
			Title: "Last active", Sizing: datatable.SizingFixed, MinWidth: len(lastActiveLayout),
			Cell: func(r sessionRow) string { return r.ModTime.Local().Format(lastActiveLayout) },
			Less: func(a, b sessionRow) bool { return a.ModTime.Before(b.ModTime) },
		},
		{
			Title: "Session", Sizing: datatable.SizingFixed, Optional: true, MinWidth: sessionIDPrefix,
			Cell: sessionIDCell,
			// The full ID is searched even though only its start is shown.
			Search: func(r sessionRow) string { return r.ID },
		},
	}
}

func projectCell(r sessionRow) string {
	if r.project == "" {
		return noProject
	}
	return r.project
}

// projectStyle dims the placeholder (Rule 122).
func projectStyle(r sessionRow) lipgloss.Style {
	if r.project == noProject {
		return theme.DimStyle
	}
	return lipgloss.NewStyle()
}

// sessionIDCell is the start of the ID; an ID shorter than that shows whole.
func sessionIDCell(r sessionRow) string {
	id := []rune(r.ID)
	return string(id[:min(sessionIDPrefix, len(id))])
}
