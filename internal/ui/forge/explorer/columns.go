package explorer

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Column minimum widths. The table used to size every column as a ratio of the
// available space, which reads as deliberate but is not: at 80 columns Type got
// 5 and Created got 8, neither wide enough for its own header. Floors plus a
// flexible Name and Slug say what actually matters when space is short.
//
// colVisibilityMin is the width of the word "Visibility" and nothing more. The
// cell below it is one glyph, so the header is all the column costs — and there
// is no sort arrow to make room for any more, because the column no longer
// sorts (see below).
const (
	colNameMin       = 20
	colSlugMin       = 16
	colVisibilityMin = 10
	colRoleMin       = 12
	colCreatedMin    = 12
	colActivityMin   = 12
	colCIMin         = 6
)

// Column indices the view and its tests name rather than count. Only the two
// that are referred to by position are declared: the rest are read by title,
// which is what stops a reorder from silently moving an assertion.
const (
	columnName    = 1
	columnCreated = 6
)

// explorerColumns describes the explorer table, in the order it renders:
// icon, Name, Slug, Visibility, Role, CI, Created, Activity.
//
// **CI sits beside Role rather than at the end**, which is where a reader looks
// for what the forge says about a repository — the same argument that put the
// workspaces CI grade beside the severity counters instead of past Modified.
//
// Only Name, Created and Activity sort. A slug orders the same as the name
// beside it; a role has no order anyone would recognise; and neither has a
// visibility — is public before private, or after? Three values in an order
// nobody would agree on are not a sort, and dropping the comparator is what
// lets the header be the whole width of the column: askFor reserves two cells
// for an arrow on every sortable column, sorted or not.
//
// It takes no vocabulary any more. The forge's own words — Group/Project or
// Organization/Repository — left the table with the Type column, and the glyph
// that replaced it belongs to no forge. They are not lost: nodeTypeLabel still
// resolves them for the help legend, which is where a sentence has room to be
// right about a personal account.
//
// The clone selection's checkbox rides on the **icon** cell (row.go:iconCell):
// a column of its own would cost four cells on every screen to say nothing on
// all but one of them, and at 80 columns the explorer has none to spare. What
// makes the sharing honest is the colour — iconStyle paints the node's kind
// whether the glyph is a checkbox or not.
func explorerColumns() []datatable.Column[explorerRow] {
	return []datatable.Column[explorerRow]{
		{
			// No title: the column carries a glyph, and a header over it would
			// name something read at a glance anyway. It declares neither Less
			// nor Search — it adds no text anyone could type, so the filter
			// stays on Name and Slug (Rule 125).
			Title: "", Sizing: datatable.SizingFixed, MinWidth: datatable.IconColumnWidth,
			Cell:  iconCell,
			Style: iconStyle,
		},
		{
			Title: "Name", Sizing: datatable.SizingContent, MinWidth: colNameMin, Flex: 2,
			Cell:   func(r explorerRow) string { return r.node.Name },
			Less:   func(a, b explorerRow) bool { return strings.ToLower(a.node.Name) < strings.ToLower(b.node.Name) },
			Search: func(r explorerRow) string { return r.node.Name },
		},
		{
			Title: "Slug", Sizing: datatable.SizingContent, Optional: true, MinWidth: colSlugMin, Flex: 1,
			Cell: func(r explorerRow) string { return nodeSlug(r.node.FullPath) },
			// The whole path, not the slug shown: a query naming a parent group
			// has always matched, and the slug is a suffix of it anyway.
			Search: func(r explorerRow) string { return r.node.FullPath },
		},
		{
			// A glyph under the whole word. The word is what makes the three
			// glyphs decodable without a legend, and it costs nothing extra:
			// the header is the column's width either way.
			Title: "Visibility", Sizing: datatable.SizingFixed, Optional: true, MinWidth: colVisibilityMin,
			Cell:  func(r explorerRow) string { return visibilityIcon(r.node) },
			Style: func(r explorerRow) lipgloss.Style { return theme.IconStyle(visibilityRole(r.node)) },
		},
		{
			Title: "Role", Sizing: datatable.SizingFixed, Optional: true, MinWidth: colRoleMin,
			Cell: func(r explorerRow) string { return r.node.Role },
		},
		{
			Title: "CI", Sizing: datatable.SizingFixed, Optional: true, MinWidth: colCIMin,
			Cell:  func(r explorerRow) string { return pipelineStatusLabel(r.node) },
			Style: func(r explorerRow) lipgloss.Style { return pipelineStatusStyle(r.node) },
		},
		{
			Title: "Created", Sizing: datatable.SizingFixed, Optional: true, MinWidth: colCreatedMin,
			Cell: func(r explorerRow) string { return timeAgo(r.node.CreatedAt) },
			Less: func(a, b explorerRow) bool { return timeBefore(a.node.CreatedAt, b.node.CreatedAt) },
		},
		{
			Title: "Activity", Sizing: datatable.SizingFixed, Optional: true, MinWidth: colActivityMin,
			Cell: func(r explorerRow) string { return timeAgo(r.node.LastActivityAt) },
			Less: func(a, b explorerRow) bool { return timeBefore(a.node.LastActivityAt, b.node.LastActivityAt) },
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
