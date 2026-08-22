package explorer

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forge"
)

// The listing, the pagination and the decoration used to live here, against
// *gitlabclient.Client. They are the backend's now (§3.6 step 3); what is left
// is the conversion from the forge's vocabulary to the tree the view renders,
// and the two Cmds that carry it into Update.

// nodeFromNamespace converts a namespace, keeping the view state on the node.
func nodeFromNamespace(ns forge.Namespace, parent *TreeNode) *TreeNode {
	return &TreeNode{
		ID:         ns.ID,
		Name:       ns.Name,
		FullPath:   ns.Path,
		Type:       NodeTypeGroup,
		Parent:     parent,
		Expanded:   false,
		Visibility: ns.Visibility,
		Role:       ns.Role,
		CreatedAt:  ns.CreatedAt,
		WebURL:     ns.WebURL,
	}
}

// nodeFromRepository converts a repository.
func nodeFromRepository(repo forge.Repository, parent *TreeNode) *TreeNode {
	return &TreeNode{
		ID:                repo.ID,
		Name:              repo.Name,
		FullPath:          repo.Path,
		Type:              NodeTypeProject,
		Parent:            parent,
		Expanded:          false,
		Visibility:        repo.Visibility,
		Role:              repo.Role,
		CreatedAt:         repo.CreatedAt,
		LastActivityAt:    repo.LastActivityAt,
		PipelineStatus:    repo.CIStatus,
		MarkedForDeletion: repo.DeletionScheduled,
		WebURL:            repo.WebURL,
	}
}

// discoverChildren walks a namespace for the recursive clone: the nodes it
// returns carry a type and a path, which is all a clone reads.
//
// It asks for no decoration, which is what keeps a clone from paying two extra
// requests per repository for a CI badge and a role it never looks at. The
// nodes are therefore **not** interchangeable with the ones the explorer
// browses, and must not be stored on the tree the view renders: the role and CI
// columns would go blank for every group a clone had walked through.
func discoverChildren(ctx context.Context, backend forge.Forge, parent *TreeNode, includeArchived bool) ([]*TreeNode, error) {
	children, err := backend.Children(ctx, parent.ID, forge.BrowseOptions{IncludeArchived: includeArchived})
	if err != nil {
		return nil, err
	}

	nodes := make([]*TreeNode, 0, len(children.Namespaces)+len(children.Repositories))
	for _, ns := range children.Namespaces {
		nodes = append(nodes, nodeFromNamespace(ns, parent))
	}
	for _, repo := range children.Repositories {
		nodes = append(nodes, nodeFromRepository(repo, parent))
	}
	return nodes, nil
}

// loadRootGroups charge les groupes racine, décorés.
func (m Model) loadRootGroups() tea.Cmd {
	backend := m.shared.Forge

	return func() tea.Msg {
		namespaces, err := backend.RootNamespaces(context.Background(), forge.BrowseOptions{Decorated: true})
		if err != nil {
			return LoadErrorMsg{Error: err}
		}

		nodes := make([]*TreeNode, len(namespaces))
		for i, ns := range namespaces {
			nodes[i] = nodeFromNamespace(ns, nil)
		}
		return RootGroupsLoadedMsg{Nodes: nodes}
	}
}

// loadChildren charge les sous-groupes et projets d'un groupe, décorés : la vue
// affiche le rôle et le statut CI en colonnes.
//
// Elle liste les dépôts archivés — l'explorer montre ce qui est là.
// `gitlab.pull.include_archived` ne concerne que le clone, et c'est
// discoverChildren qui le lit.
func (m Model) loadChildren(parentNode *TreeNode) tea.Cmd {
	backend := m.shared.Forge

	return func() tea.Msg {
		children, err := backend.Children(context.Background(), parentNode.ID, forge.BrowseOptions{
			IncludeArchived: true,
			Decorated:       true,
		})
		if err != nil {
			return LoadErrorMsg{Error: err, ParentNode: parentNode}
		}

		nodes := make([]*TreeNode, 0, len(children.Namespaces)+len(children.Repositories))
		for _, ns := range children.Namespaces {
			nodes = append(nodes, nodeFromNamespace(ns, parentNode))
		}
		for _, repo := range children.Repositories {
			nodes = append(nodes, nodeFromRepository(repo, parentNode))
		}

		return ChildrenLoadedMsg{ParentNode: parentNode, Children: nodes}
	}
}
