package filebrowser

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/fsbrowse"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/fileicon"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

const (
	colNameMin       = 16
	colSizeFixed     = 10 // "1023.9 MiB"
	colModifiedFixed = 12 // "30 days ago"
)

func columns() []datatable.Column[row] {
	return []datatable.Column[row]{
		{
			// Rule 125: a glyph is a column, untitled, two cells, coloured by
			// role. Directory and file are the two roles because they are the
			// two sets of actions a row offers (→ versus enter).
			Title: "", Sizing: datatable.SizingFixed, MinWidth: datatable.IconColumnWidth,
			Cell:  func(r row) string { return rowIcon(r) },
			Style: func(r row) lipgloss.Style { return theme.IconStyle(rowIconRole(r)) },
		},
		{
			Title: "Name", Sizing: datatable.SizingContent, MinWidth: colNameMin, Flex: 1,
			Cell:   func(r row) string { return r.Name },
			Search: func(r row) string { return r.Name },
		},
		{
			// A directory's size is its inode, not what it holds: a dash says
			// "not a number worth reading" where 4.0 KiB would mislead.
			Title: "Size", Sizing: datatable.SizingFixed, Optional: true, MinWidth: colSizeFixed,
			Cell: func(r row) string {
				if r.IsDir {
					return "-"
				}
				return fsbrowse.HumanSize(r.Size)
			},
			Style: func(r row) lipgloss.Style {
				if r.IsDir {
					return theme.DimStyle
				}
				return lipgloss.NewStyle()
			},
		},
		{
			Title: "Modified", Sizing: datatable.SizingFixed, Optional: true, MinWidth: colModifiedFixed,
			Cell: func(r row) string {
				if r.self {
					return ""
				}
				return theme.TimeAgo(r.ModTime)
			},
		},
	}
}

func rowIcon(r row) string {
	if r.IsDir {
		return theme.IconDirectory
	}
	return fileicon.For(r.Name)
}

func rowIconRole(r row) theme.IconRole {
	if r.IsDir {
		return theme.IconRoleDirectory
	}
	return theme.IconRoleFile
}
