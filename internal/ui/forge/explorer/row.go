package explorer

import "github.com/anthnel/devdesk/internal/ui/theme"

// explorerRow is a node plus what the view knows about it and the node does not:
// whether it is ticked for cloning.
//
// The columns are built once in New and close over nothing, so a decoration that
// changes with a keystroke cannot be read from the model at render time — it has
// to travel on the row. That is the `imageRow` pattern, and it also means a
// column sorts by the same value it prints.
type explorerRow struct {
	node *TreeNode
	// selecting says whether the checkbox is shown at all. Outside the clone
	// selection mode the Type cell prints the label alone, so a check state left
	// over from a previous selection cannot appear.
	selecting bool
	check     theme.CheckState
}

// checkboxIcon is RenderCheckboxTri's glyph without its styling.
//
// RenderCheckboxTri renders through lipgloss, and a styled string in a table
// cell is truncated mid-escape and bleeds over every row below it (Rule 122).
// The glyphs are the shared ones; only the styling is left behind.
func checkboxIcon(state theme.CheckState) string {
	switch state {
	case theme.CheckAll:
		return theme.IconChecked
	case theme.CheckSome:
		return theme.IconCheckboxIndeterminate
	default:
		return theme.IconCheckbox
	}
}

// selectedNode returns the node under the cursor, which is what every action
// wants — none of them has anything to say about the decoration.
func (m Model) selectedNode() (*TreeNode, bool) {
	row, ok := m.table.Selected()
	if !ok {
		return nil, false
	}
	return row.node, true
}

// rowsFor wraps a level's nodes, stamping the current check state onto each.
//
// The state is read here rather than in the cell so that the table holds a
// settled value: `Space` rebuilds the rows, which is the one place the selection
// and what is on screen are brought back into agreement.
func (m Model) rowsFor(nodes []*TreeNode) []explorerRow {
	selecting := m.mode == ModeSelecting
	rows := make([]explorerRow, len(nodes))
	for i, node := range nodes {
		rows[i] = explorerRow{node: node, selecting: selecting}
		if selecting {
			rows[i].check = m.selection.state(node.FullPath)
		}
	}
	return rows
}
