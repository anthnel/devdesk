package explorer

import (
	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

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
	// selection mode the icon column prints the kind glyph alone, so a check
	// state left over from a previous selection cannot appear.
	selecting bool
	check     theme.CheckState
	// Activity is the state column 0 draws, stamped by rowsFor from the router
	// snapshot. The blink phase is read by the table at render, so a frame
	// rebuilds nothing.
	Activity indicator.State
}

// restIcon is the activity column's resting cell: the checkbox while a clone
// selection is open, the kind glyph otherwise. One column for both, because
// Rule 125 fixes an icon column at two cells and a checkbox beside a glyph
// needs four; iconStyle paints the kind in both, so a ticked row still says
// group or repository by hue.
func restIcon(r explorerRow) string {
	if r.selecting {
		return checkboxIcon(r.check)
	}
	return nodeKindIcon(r.node)
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

// iconStyle colours the resting cell by the node's kind, in both modes.
//
// Under the cursor it has no effect: datatable hands the selected row whole to
// styles.Selected, and a colour inside it would close with a reset that takes
// the selection background with it (Rule 122). That is the behaviour of every
// coloured column in the application, not an exception here.
func iconStyle(r explorerRow) lipgloss.Style {
	return theme.IconStyle(nodeKindRole(r.node))
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
		rows[i].Activity = m.rowState(node)
	}
	return rows
}

// rowState is the indicator state of a row. In selection mode a failed row is
// shown at rest so its checkbox, and whether it is ticked, stays visible; the
// running and queued glyphs stay because the toggle refuses those rows.
func (m Model) rowState(node *TreeNode) indicator.State {
	state := m.rowActivity(node).State
	if state == indicator.Failed && m.mode == ModeSelecting {
		return indicator.Idle
	}
	return state
}
