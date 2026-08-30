package explorer

import (
	tea "github.com/charmbracelet/bubbletea"
)

// handleDrillDown handles enter key - navigate into a group
func (m Model) handleDrillDown() (tea.Model, tea.Cmd) {
	node, ok := m.selectedNode()
	if !ok || node.Type != NodeTypeGroup {
		return m, nil
	}
	// A placeholder has no identifier, so listing its children would ask the
	// forge about the empty string — which is a different question, not an
	// error the backend would refuse.
	if node.Creating {
		return m, m.footer.Warn(reasonNotCreatedYet)
	}

	// Save cursor position before navigating down
	m.cursorStack = append(m.cursorStack, m.table.Cursor())
	// Push current group onto navigation stack
	m.navigationStack = append(m.navigationStack, m.currentGroupNode)
	m.currentGroupNode = node
	m.activeTabIndex = m.tabCount() - 1

	// Load children if not yet loaded
	if node.Children == nil {
		node.Loading = true
		m.loading = true
		m.updateTableRows()
		return m, tea.Batch(m.spinner.Tick, m.loadChildren(node))
	}
	m.updateTableRows()
	m.table.GotoTop()
	return m, nil
}

// handleDrillUp handles backspace key - navigate back to parent
func (m Model) handleDrillUp() (tea.Model, tea.Cmd) {
	if len(m.navigationStack) == 0 {
		return m, nil
	}
	// Pop from stack
	m.currentGroupNode = m.navigationStack[len(m.navigationStack)-1]
	m.navigationStack = m.navigationStack[:len(m.navigationStack)-1]
	m.activeTabIndex = m.tabCount() - 1
	m.updateTableRows()
	// Restore cursor position at the parent level
	if len(m.cursorStack) > 0 {
		cursor := m.cursorStack[len(m.cursorStack)-1]
		m.cursorStack = m.cursorStack[:len(m.cursorStack)-1]
		m.table.SetCursor(cursor)
	} else {
		m.table.GotoTop()
	}
	return m, nil
}

// updateTableRows refills the table with the current drill-down level.
//
// The sort, the filter, the sort arrows and the cursor clamp were all written
// out in table.go; they are the component's now. What is left is the one thing
// this view knows and it does not: which level the table is showing.
func (m *Model) updateTableRows() {
	m.table.SetItems(m.rowsFor(m.currentItems()))
	// A drill-down is a different population, and a checkbox toggle widens the
	// Type cell by two — both are user actions on a settled list, which is what
	// Remeasure is for. datatable cannot tell either from a periodic reload:
	// they all arrive through SetItems.
	m.table.Remeasure()
}

// currentItems returns the children of the current drill-down group (or root nodes)
func (m Model) currentItems() []*TreeNode {
	if m.currentGroupNode == nil {
		return m.nodes
	}
	if m.currentGroupNode.Children == nil {
		return nil
	}
	return m.currentGroupNode.Children
}

// handleRefresh handles r key
func (m Model) handleRefresh() (tea.Model, tea.Cmd) {
	m.loading = true
	// The placeholders survive the wipe: a create in flight is not something
	// the refresh can re-read, so dropping it here would take its row away
	// while its request was still out (see carryOverCreating).
	m.nodes = carryOverCreating(m.nodes, nil)
	m.currentGroupNode = nil
	m.navigationStack = nil
	m.cursorStack = nil
	m.activeTabIndex = 0
	// The rows the placeholders need, not nil: a create in flight keeps its
	// line through the refresh, and SetItems(nil) would blank it for the length
	// of the reload.
	m.updateTableRows()
	return m, tea.Batch(m.spinner.Tick, m.loadRootGroups())
}

// handleChildrenLoaded handles ChildrenLoadedMsg
func (m Model) handleChildrenLoaded(msg ChildrenLoadedMsg) (tea.Model, tea.Cmd) {
	msg.ParentNode.Children = carryOverCreating(msg.ParentNode.Children, msg.Children)
	msg.ParentNode.Expanded = true
	msg.ParentNode.Loading = false
	m.loading = false

	m.updateTableRows()
	m.table.GotoTop()
	return m, nil
}

// handleLoadError handles LoadErrorMsg
func (m Model) handleLoadError(msg LoadErrorMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	m.firstLoadDone = true
	m.error = msg.Error.Error()
	if msg.ParentNode != nil {
		msg.ParentNode.Loading = false
	}
	return m, nil
}

// expandToPath went with pendingSelectPath (§3.59). It existed to find a
// freshly created resource again after the refresh that followed a creation:
// the node could be several levels down, so the tree was re-listed and then
// drilled back into. There is no refresh any more — the row is put where the
// user made it and settles in place — so there is nothing to chase.
