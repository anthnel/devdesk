// Package updatecol is the "Update" column three tables share (§3.88): oci's
// Images tab, the containers list and the Remediation tab of a scan result.
//
// It lives apart from theme because it builds a datatable column, which theme
// cannot import, and apart from each view so the three read one definition.
package updatecol

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/imageupdate"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Title is the column's header.
const Title = "Update"

// minWidth is what the column never shrinks under: the arrow and "new build",
// the one label known in advance. The column follows its content, and a
// content column gives up width before any column is dropped — floored at the
// title alone, the label was cut to "new…", which says nothing. A patch tag
// longer than this may still be cut, from its end: the version stays readable.
func minWidth() int {
	return max(lipgloss.Width(theme.UpdateCell(true, imageupdate.Status{Kind: imageupdate.NewBuild}.Label())), len(Title))
}

// Column is the column for rows whose status get returns. optional lets it
// give way first when the terminal is narrow, for a table where other columns
// matter more.
func Column[T any](optional bool, get func(T) imageupdate.Status) datatable.Column[T] {
	return datatable.Column[T]{
		Title: Title, Sizing: datatable.SizingContent, MinWidth: minWidth(), MaxWidth: 20, Optional: optional,
		Cell: func(r T) string {
			s := get(r)
			return theme.UpdateCell(s.Available(), s.Label())
		},
		Style: func(r T) lipgloss.Style { return theme.UpdateStyle(get(r).Available()) },
		// No comparator: `datatable` reserves two cells for a sortable column's
		// arrow, and the containers table is the widest there is. A filter on
		// the label does what a sort would.
		Search: func(r T) string { return get(r).Label() },
	}
}
