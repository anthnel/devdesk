package explorer

import (
	"context"
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/forgeindex"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// handleDeleteStart handles ctrl+d key - start delete operation
func (m Model) handleDeleteStart() (tea.Model, tea.Cmd) {
	// Rule 130: the key is greyed for every one of these — no session, no row,
	// a row the forge has not confirmed, a row already busy — and pressing it
	// anyway says which one it is rather than doing nothing. The check comes
	// before the lookup so a missing row is named too.
	if act := m.actionable(); !act.Enabled() {
		return m, m.footer.Warn(act.Reason)
	}
	node, ok := m.selectedNode()
	if !ok {
		return m, nil
	}

	m.deleteTargetNode = node
	m.mode = ModeConfirmingDelete

	// Project already scheduled for deletion: only permanent removal is possible.
	if node.Type == NodeTypeProject && node.MarkedForDeletion {
		title := "Permanently Delete Project"
		message := fmt.Sprintf("'%s' is already scheduled for deletion.\n\nConfirm to permanently delete it now.", node.Name)
		m.deleteConfirmModal = components.NewDeleteConfirmModalLocked(title, message)
		return m, nil
	}

	var title, message string
	if node.Type == NodeTypeGroup {
		title = "Delete Group"
		message = fmt.Sprintf("Are you sure you want to delete the group '%s'?\n\nThis will delete the group and ALL its contents\n(subgroups, projects, issues, etc.).", node.Name)
	} else {
		title = "Delete Project"
		message = fmt.Sprintf("Are you sure you want to delete the project '%s'?\n\nThis will delete the project and all its data\n(code, issues, merge requests, etc.).", node.Name)
	}

	offerImmediate := m.shared.Forge != nil && m.shared.Forge.Shape().PermanentDelete
	m.deleteConfirmModal = components.NewDeleteConfirmModal(title, message, offerImmediate)
	return m, nil
}

// handleDeleteConfirmed handles confirmation of delete
func (m Model) handleDeleteConfirmed(permanentlyRemove bool) (tea.Model, tea.Cmd) {
	m.mode = ModeNormal
	m.deleteConfirmModal = nil

	node := m.deleteTargetNode
	m.deleteTargetNode = nil

	if node == nil {
		return m, nil
	}

	backend := m.shared.Forge
	if backend == nil {
		m.error = "Not connected to a forge"
		return m, nil
	}

	// The whole value goes to the backend, not just an identifier: a permanent
	// deletion addresses the path the forge renames the object to, so the call
	// needs both (see forge.Forge.DeleteNamespace).
	nodeType := node.Type
	id, path := node.ID, node.FullPath

	if m.busy(path) {
		return m, m.footer.Warn(busyMessage)
	}

	work := func() tea.Msg {
		var err error
		if nodeType == NodeTypeGroup {
			err = backend.DeleteNamespace(context.Background(), forge.Namespace{ID: id, Path: path}, permanentlyRemove)
		} else {
			err = backend.DeleteRepository(context.Background(), forge.Repository{ID: id, Path: path}, permanentlyRemove)
		}
		return DeleteCompleteMsg{Error: err, Target: path, DeletedNode: node}
	}
	// The row keeps its place and takes the spinner while the forge works — the
	// same treatment a create gets, and for the same reason: a delete is a
	// network call, and a row that vanished before the answer came would be
	// claiming something that has not happened yet.
	return m, jobs.Start(deleteRun(path, node.Name), work)
}

// removeNode drops node from level, by identity first and by ID as a fallback
// (a refresh may have replaced the pointer since the delete was asked).
func removeNode(level []*TreeNode, node *TreeNode) []*TreeNode {
	for i, n := range level {
		if n == node || (n.ID != "" && n.ID == node.ID) {
			return append(level[:i], level[i+1:]...)
		}
	}
	return level
}

// handleDeleteComplete handles DeleteCompleteMsg
func (m Model) handleDeleteComplete(msg DeleteCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		log.Printf("ERROR [explorer] delete: %v", msg.Error)
		return m, m.footer.Error("Delete failed — check logs")
	}
	m.footer.Clear()

	if msg.DeletedNode == nil {
		m.updateTableRows()
		return m, nil
	}

	// The row leaves the level it belongs to — its parent's, which is the
	// current one unless the user drilled elsewhere while the forge worked.
	// Removing it from whatever level was on screen instead would leave it in
	// the tree, and take the wrong row if an ID ever matched.
	deleted := msg.DeletedNode
	if deleted.Parent == nil {
		m.nodes = removeNode(m.nodes, deleted)
	} else {
		deleted.Parent.Children = removeNode(deleted.Parent.Children, deleted)
	}
	m.levelChanged(deleted.Parent)
	m.updateTableRows()
	gone := deleted.FullPath
	return m, editIndex(func(ix *forgeindex.Index) *forgeindex.Index { return ix.Without(gone) })
}
