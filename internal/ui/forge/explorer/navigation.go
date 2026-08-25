package explorer

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// handleDrillDown handles enter key - navigate into a group
func (m Model) handleDrillDown() (tea.Model, tea.Cmd) {
	node, ok := m.selectedNode()
	if !ok || node.Type != NodeTypeGroup {
		return m, nil
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
	m.nodes = []*TreeNode{}
	m.currentGroupNode = nil
	m.navigationStack = nil
	m.cursorStack = nil
	m.activeTabIndex = 0
	m.table.SetItems(nil)
	return m, tea.Batch(m.spinner.Tick, m.loadRootGroups())
}

// handleChildrenLoaded handles ChildrenLoadedMsg
func (m Model) handleChildrenLoaded(msg ChildrenLoadedMsg) (tea.Model, tea.Cmd) {
	msg.ParentNode.Children = msg.Children
	msg.ParentNode.Expanded = true
	msg.ParentNode.Loading = false
	m.loading = false

	m.updateTableRows()
	m.table.GotoTop()

	// Continue expanding if there's a pending path
	if m.pendingSelectPath != "" {
		return m.expandToPath(m.pendingSelectPath)
	}

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

// expandToPath navigates to and selects a node at the given path after refresh
func (m Model) expandToPath(targetPath string) (tea.Model, tea.Cmd) {
	// Check if the target is on screen. The rows, not currentItems(): under a
	// filter or a non-default sort those are two different orderings, and the
	// cursor indexes the one being shown.
	for i, row := range m.table.Visible() {
		if row.node.FullPath == targetPath {
			m.table.SetCursor(i)
			m.pendingSelectPath = ""
			return m, nil
		}
	}

	// Find an ancestor that needs to be drilled into
	for _, node := range m.currentItems() {
		if node.Type == NodeTypeGroup && strings.HasPrefix(targetPath, node.FullPath+"/") {
			// Drill into this group
			m.navigationStack = append(m.navigationStack, m.currentGroupNode)
			m.currentGroupNode = node

			if node.Children == nil {
				node.Loading = true
				m.updateTableRows()
				return m, m.loadChildren(node)
			}
			node.Expanded = true
			m.updateTableRows()
			return m.expandToPath(targetPath)
		}
	}

	// Could not find path - clear pending and stay where we are
	m.pendingSelectPath = ""
	return m, nil
}
