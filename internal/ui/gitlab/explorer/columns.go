package explorer

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/datatable"
)

// numColumns is the number of columns in the explorer table
const numColumns = 8

// Column minimum widths. The table used to size every column as a ratio of the
// available space, which reads as deliberate but is not: at 80 columns Type got
// 5 and Created got 8, neither wide enough for its own header. Floors plus a
// flexible Name and Slug say what actually matters when space is short.
//
// Type is wider than "Project" needs because the clone selection prefixes it
// with a checkbox, and a Nerd Font glyph does not always render as narrow as
// runewidth counts it.
const (
	colTypeMin       = 13
	colNameMin       = 20
	colSlugMin       = 16
	colVisibilityMin = 12
	colRoleMin       = 12
	colCreatedMin    = 12
	colActivityMin   = 12
	colCIMin         = 6
)

// columnName is the column the explorer opens sorted by.
const columnType = 0

// explorerColumns describes the explorer table. Slug, Role and CI do not sort:
// a slug orders the same as the name beside it, and neither a role nor a
// pipeline icon has an order anyone would recognise.
//
// The clone selection's checkbox rides on the Type cell rather than taking a
// column of its own: a column would cost four cells on every screen to say
// nothing on all but one of them, and at 80 columns the explorer has none to
// spare. Type is the left-most column, so the box still sits where a checkbox
// belongs.
func explorerColumns() []datatable.Column[explorerRow] {
	return []datatable.Column[explorerRow]{
		{
			Title: "Type", MinWidth: colTypeMin,
			Cell: func(r explorerRow) string {
				if !r.selecting {
					return nodeTypeLabel(r.node)
				}
				return checkboxIcon(r.check) + " " + nodeTypeLabel(r.node)
			},
			Less: func(a, b explorerRow) bool { return string(a.node.Type) < string(b.node.Type) },
		},
		{
			Title: "Name", MinWidth: colNameMin, Flex: 2,
			Cell:   func(r explorerRow) string { return r.node.Name },
			Less:   func(a, b explorerRow) bool { return strings.ToLower(a.node.Name) < strings.ToLower(b.node.Name) },
			Search: func(r explorerRow) string { return r.node.Name },
		},
		{
			Title: "Slug", MinWidth: colSlugMin, Flex: 1,
			Cell: func(r explorerRow) string { return nodeSlug(r.node.FullPath) },
			// The whole path, not the slug shown: a query naming a parent group
			// has always matched, and the slug is a suffix of it anyway.
			Search: func(r explorerRow) string { return r.node.FullPath },
		},
		{
			Title: "Visibility", MinWidth: colVisibilityMin,
			Cell: func(r explorerRow) string { return visibilityLabel(r.node) },
			Less: func(a, b explorerRow) bool {
				return strings.ToLower(a.node.Visibility) < strings.ToLower(b.node.Visibility)
			},
		},
		{
			Title: "Role", MinWidth: colRoleMin,
			Cell: func(r explorerRow) string { return r.node.AccessLevelName() },
		},
		{
			Title: "Created", MinWidth: colCreatedMin,
			Cell: func(r explorerRow) string { return timeAgo(r.node.CreatedAt) },
			Less: func(a, b explorerRow) bool { return timeBefore(a.node.CreatedAt, b.node.CreatedAt) },
		},
		{
			Title: "Activity", MinWidth: colActivityMin,
			Cell: func(r explorerRow) string { return timeAgo(r.node.LastActivityAt) },
			Less: func(a, b explorerRow) bool { return timeBefore(a.node.LastActivityAt, b.node.LastActivityAt) },
		},
		{
			Title: "CI", MinWidth: colCIMin,
			Cell:  func(r explorerRow) string { return pipelineStatusLabel(r.node) },
			Style: func(r explorerRow) lipgloss.Style { return pipelineStatusStyle(r.node) },
		},
	}
}

// timeBefore compares two nullable times (nil is considered "oldest")
func timeBefore(a, b *time.Time) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil {
		return true
	}
	if b == nil {
		return false
	}
	return a.Before(*b)
}
