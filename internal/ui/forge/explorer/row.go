package explorer

import (
	"github.com/charmbracelet/lipgloss"

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
	// frame is the registry's spinner frame, set only while the forge is being
	// asked something about this row. Text rather than a rendered spinner
	// because a table cell must carry no escape sequence (Rule 122) — the same
	// reason cloneRow holds one.
	frame string
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

// iconCell is the first column: the spinner while the forge is being asked
// something about the row, the checkbox while a clone selection is open, the
// kind glyph otherwise.
//
// One column for three things, because Rule 125 fixes an icon column at two
// cells and a checkbox beside a glyph needs four. What makes the sharing work
// is that the colour does not switch with the shape: iconStyle paints the kind
// in all three, so a ticked row still says group or repository — by hue rather
// than by glyph, which is the whole reason the theme grew icon roles.
//
// The spinner wins over the checkbox because the two cannot both be true: a row
// held by a create or a delete is refused by the selection toggle, so a ticked
// row is never a working one.
func iconCell(r explorerRow) string {
	if r.frame != "" {
		return r.frame
	}
	if r.selecting {
		return checkboxIcon(r.check)
	}
	return nodeKindIcon(r.node)
}

// iconStyle colours the cell above by the node's kind, in both modes.
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
		// The frame is read here rather than in the cell for the same reason
		// the check state is: the table holds a settled value, and the cell
		// closes over nothing.
		if m.busy(node.FullPath) {
			rows[i].frame = m.jobFrame
		}
	}
	return rows
}
