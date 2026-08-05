package explorer

import (
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/ui/datatable"
)

// numColumns is the number of columns in the explorer table
const numColumns = 8

// Column minimum widths. The table used to size every column as a ratio of the
// available space, which reads as deliberate but is not: at 80 columns Type got
// 5 and Created got 8, neither wide enough for its own header. Floors plus a
// flexible Name and Slug say what actually matters when space is short.
const (
	colTypeMin       = 10
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
func explorerColumns() []datatable.Column[*TreeNode] {
	return []datatable.Column[*TreeNode]{
		{
			Title: "Type", MinWidth: colTypeMin,
			Cell: nodeTypeLabel,
			Less: func(a, b *TreeNode) bool { return string(a.Type) < string(b.Type) },
		},
		{
			Title: "Name", MinWidth: colNameMin, Flex: 2,
			Cell:   func(n *TreeNode) string { return n.Name },
			Less:   func(a, b *TreeNode) bool { return strings.ToLower(a.Name) < strings.ToLower(b.Name) },
			Search: func(n *TreeNode) string { return n.Name },
		},
		{
			Title: "Slug", MinWidth: colSlugMin, Flex: 1,
			Cell: func(n *TreeNode) string { return nodeSlug(n.FullPath) },
			// The whole path, not the slug shown: a query naming a parent group
			// has always matched, and the slug is a suffix of it anyway.
			Search: func(n *TreeNode) string { return n.FullPath },
		},
		{
			Title: "Visibility", MinWidth: colVisibilityMin,
			Cell: visibilityLabel,
			Less: func(a, b *TreeNode) bool { return strings.ToLower(a.Visibility) < strings.ToLower(b.Visibility) },
		},
		{
			Title: "Role", MinWidth: colRoleMin,
			Cell: func(n *TreeNode) string { return n.AccessLevelName() },
		},
		{
			Title: "Created", MinWidth: colCreatedMin,
			Cell: func(n *TreeNode) string { return timeAgo(n.CreatedAt) },
			Less: func(a, b *TreeNode) bool { return timeBefore(a.CreatedAt, b.CreatedAt) },
		},
		{
			Title: "Activity", MinWidth: colActivityMin,
			Cell: func(n *TreeNode) string { return timeAgo(n.LastActivityAt) },
			Less: func(a, b *TreeNode) bool { return timeBefore(a.LastActivityAt, b.LastActivityAt) },
		},
		{
			Title: "CI", MinWidth: colCIMin,
			Cell: pipelineStatusLabel,
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
