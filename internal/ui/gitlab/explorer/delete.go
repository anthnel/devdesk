package explorer

import (
	"context"
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// handleDeleteStart handles ctrl+d key - start delete operation
func (m Model) handleDeleteStart() (tea.Model, tea.Cmd) {
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

	m.deleteConfirmModal = components.NewDeleteConfirmModal(title, message)
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

	return m, func() tea.Msg {
		var err error
		if nodeType == NodeTypeGroup {
			err = backend.DeleteNamespace(context.Background(), forge.Namespace{ID: id, Path: path}, permanentlyRemove)
		} else {
			err = backend.DeleteRepository(context.Background(), forge.Repository{ID: id, Path: path}, permanentlyRemove)
		}
		return DeleteCompleteMsg{Error: err, DeletedNode: node}
	}
}

// handleDeleteComplete handles DeleteCompleteMsg
func (m Model) handleDeleteComplete(msg DeleteCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		log.Printf("ERROR [explorer] delete: %v", msg.Error)
		return m, m.footer.Error("Delete failed — check logs")
	}
	m.footer.Clear()

	// Remove deleted node from local tree and stay in current group
	if msg.DeletedNode != nil {
		if m.currentGroupNode != nil {
			children := m.currentGroupNode.Children
			for i, child := range children {
				if child.ID == msg.DeletedNode.ID {
					m.currentGroupNode.Children = append(children[:i], children[i+1:]...)
					break
				}
			}
		} else {
			for i, n := range m.nodes {
				if n.ID == msg.DeletedNode.ID {
					m.nodes = append(m.nodes[:i], m.nodes[i+1:]...)
					break
				}
			}
		}
	}
	m.updateTableRows()
	return m, nil
}
