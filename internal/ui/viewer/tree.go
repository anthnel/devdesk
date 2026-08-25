package viewer

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/anthnel/devdesk/internal/viewer"
)

// treeRow is a node with what the screen knows about it.
//
// It exists for the reason imageRow and explorerRow do: datatable's columns are
// built once, in New, and close over nothing, so anything a cell needs beyond
// the domain object — the depth it renders at, whether it is expanded — has to
// travel on the row.
type treeRow struct {
	Node     *viewer.Node
	Depth    int
	Expanded bool
	// Highlight is off when `c` is off, and the row then renders in the plain
	// text colour. It rides on the row for the same reason as the rest.
	Highlight bool
}

// indentUnit is one level of nesting. Two spaces: a JSON document nests deeply
// and four would push the value column off a narrow terminal by the third level.
const indentUnit = "  "

// treeColumns are the two the tree shows. Neither declares Less or Search, and
// both omissions are deliberate:
//
//   - Sorting would destroy the order the file gave it, which is why the parsers
//     read a token stream in the first place. A JSON object read back
//     alphabetised is a different document.
//   - A text filter would match a child and hide its parents, leaving rows at a
//     depth with nothing above them to explain it.
//
// datatable advertises `/` and `.` from these fields, so leaving them nil is
// also what keeps the two keys off the shortcut list (Rule 138).
func treeColumns() []datatable.Column[treeRow] {
	return []datatable.Column[treeRow]{
		{
			Title: "Key", Sizing: datatable.SizingContent, MinWidth: 20, Flex: 2,
			Cell:  treeKeyCell,
			Style: treeKeyStyle,
		},
		{
			Title: "Value", Sizing: datatable.SizingContent, MinWidth: 20, Flex: 3,
			Cell:  func(r treeRow) string { return r.Node.Value },
			Style: treeValueStyle,
		},
	}
}

// treeKeyCell is the indentation, the chevron and the name — plain text, because
// it is measured and truncated before anything is applied to it (Rule 122).
func treeKeyCell(r treeRow) string {
	return strings.Repeat(indentUnit, r.Depth) + chevronFor(r) + " " + r.Node.Key
}

// chevronFor is the glyph without its colour: a leaf gets a space rather than a
// chevron, because a chevron promising a level that is not there is worse than
// none.
func chevronFor(r treeRow) string {
	switch {
	case !r.Node.HasChildren():
		return " "
	case r.Expanded:
		return theme.IconChevronDown
	default:
		return theme.IconChevronRight
	}
}

func treeKeyStyle(r treeRow) lipgloss.Style {
	if !r.Highlight {
		return lipgloss.NewStyle()
	}
	return syntaxStyle(keyClass(r.Node.Kind))
}

func treeValueStyle(r treeRow) lipgloss.Style {
	if !r.Highlight {
		return lipgloss.NewStyle()
	}
	return nodeStyle(r.Node.Kind)
}

// treeRows flattens the document for a given expansion state.
func (m *Model) treeRows() []treeRow {
	flat := viewer.Flatten(m.doc.Root, m.collapsed)
	rows := make([]treeRow, 0, len(flat))
	for _, node := range flat {
		rows = append(rows, treeRow{
			Node:      node.Node,
			Depth:     node.Depth,
			Expanded:  !m.collapsed[node.Node.ID],
			Highlight: m.highlight,
		})
	}
	return rows
}

// rebuildTree puts the current expansion state on screen, keeping the cursor
// where it was. datatable clamps it, so a collapse that removes the rows below
// the cursor leaves it on the last row that still exists rather than nowhere.
func (m *Model) rebuildTree() {
	m.tree.SetItems(m.treeRows())
	// Expanding a node puts deeper keys on screen, and the indentation is part
	// of the cell — so the width the Key column wants changes with the
	// expansion, and only the user moves that.
	m.tree.Remeasure()
}

// toggleNode expands or collapses the selected node.
//
// Returning whether anything happened is what lets the caller leave `←` and `→`
// free to mean nothing on a leaf, rather than swallowing the key.
func (m *Model) toggleNode(expand bool) bool {
	row, ok := m.tree.Selected()
	if !ok || !row.Node.HasChildren() {
		return false
	}
	if m.collapsed[row.Node.ID] == !expand {
		return false // already in the asked-for state
	}
	if expand {
		delete(m.collapsed, row.Node.ID)
	} else {
		m.collapsed[row.Node.ID] = true
	}
	m.rebuildTree()
	return true
}
