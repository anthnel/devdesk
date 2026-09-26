package explorer

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forgeindex"
	"github.com/anthnel/devdesk/internal/shared"
)

// The explorer reads the router's forge index (shared.State.ForgeIndex) to put
// a level on screen before the forge has answered for it, and then asks the
// forge anyway: the index carries neither the role nor the CI status, and it
// may be as old as the last session. What the forge answers replaces the level
// in place and corrects the index — so the index is a head start, never the
// last word, and a level is never shown for long in a state nobody asked the
// forge about.

// nodeFromEntry builds a tree node from an index entry. It has no role and no
// CI status: those come with the level's decorated read.
func nodeFromEntry(e forgeindex.Entry, parent *TreeNode) *TreeNode {
	node := &TreeNode{
		ID:                e.ID,
		Name:              e.Name,
		FullPath:          e.Path,
		Type:              NodeTypeProject,
		Parent:            parent,
		Visibility:        e.Visibility,
		CreatedAt:         e.CreatedAt,
		LastActivityAt:    e.LastActivityAt,
		MarkedForDeletion: e.DeletionScheduled,
		WebURL:            e.WebURL,
	}
	if e.Kind == forgeindex.KindNamespace {
		node.Type = NodeTypeGroup
	}
	return node
}

// entryFromNode is the reverse, for telling the index about a node the forge
// just answered for.
func entryFromNode(node *TreeNode, parent string) forgeindex.Entry {
	kind := forgeindex.KindRepository
	if node.Type == NodeTypeGroup {
		kind = forgeindex.KindNamespace
	}
	return forgeindex.Entry{
		ID:                node.ID,
		Path:              node.FullPath,
		Name:              node.Name,
		Parent:            parent,
		Kind:              kind,
		Visibility:        node.Visibility,
		CreatedAt:         node.CreatedAt,
		LastActivityAt:    node.LastActivityAt,
		WebURL:            node.WebURL,
		DeletionScheduled: node.MarkedForDeletion,
	}
}

// pathOf is a node's path, empty for the root level.
func pathOf(node *TreeNode) string {
	if node == nil {
		return ""
	}
	return node.FullPath
}

// indexedLevel returns the children of parent as the index knows them, and
// whether it knows them at all.
func (m Model) indexedLevel(parent *TreeNode) ([]*TreeNode, bool) {
	entries, known := m.shared.ForgeIndex.Children(pathOf(parent))
	if !known {
		return nil, false
	}
	nodes := make([]*TreeNode, len(entries))
	for i, e := range entries {
		nodes[i] = nodeFromEntry(e, parent)
	}
	return nodes, true
}

// showIndexedLevel puts the index's children of node on screen. A blocking
// load of that level still in flight becomes the refresh behind them: the
// user is no longer waiting on it, so it stops holding the view in loading.
func (m *Model) showIndexedLevel(node *TreeNode, children []*TreeNode) {
	node.Children = children
	if node.Loading && node.Blocking {
		node.Blocking = false
		m.loading = false
		m.refreshing++
	}
}

// mergeLevel lays what the forge answered over what is on screen.
//
// A node that stayed keeps its pointer — and with it its children, its
// freshness, and its place in the navigation stack if the user is inside it —
// and takes the forge's fields. One that went is dropped, one that appeared is
// added, in the forge's order. Placeholders of creates still in flight survive
// (carryOverCreating's reasoning).
func mergeLevel(existing, fresh []*TreeNode) []*TreeNode {
	byPath := make(map[string]*TreeNode, len(existing))
	for _, node := range existing {
		if !node.Creating {
			byPath[node.FullPath] = node
		}
	}
	out := make([]*TreeNode, 0, len(fresh))
	for _, f := range fresh {
		old, ok := byPath[f.FullPath]
		if !ok {
			out = append(out, f)
			continue
		}
		old.ID = f.ID
		old.Name = f.Name
		old.Type = f.Type
		old.Visibility = f.Visibility
		old.Role = f.Role
		old.CreatedAt = f.CreatedAt
		old.LastActivityAt = f.LastActivityAt
		old.PipelineStatus = f.PipelineStatus
		old.MarkedForDeletion = f.MarkedForDeletion
		old.WebURL = f.WebURL
		out = append(out, old)
	}
	return carryOverCreating(existing, out)
}

// editIndex asks the router to change the shared index. See
// shared.ForgeIndexEditMsg for why it is a function.
func editIndex(edit func(*forgeindex.Index) *forgeindex.Index) tea.Cmd {
	return func() tea.Msg { return shared.ForgeIndexEditMsg{Edit: edit} }
}

// replaceIndexedLevel tells the index what the forge just said a level holds.
func replaceIndexedLevel(parent *TreeNode, nodes []*TreeNode) tea.Cmd {
	path := pathOf(parent)
	entries := make([]forgeindex.Entry, 0, len(nodes))
	for _, node := range nodes {
		if !node.Creating {
			entries = append(entries, entryFromNode(node, path))
		}
	}
	return editIndex(func(ix *forgeindex.Index) *forgeindex.Index {
		return ix.ReplaceLevel(path, entries)
	})
}

// refreshLevel asks the forge for a level that is already on screen, without
// taking it off screen. The table stays, the footer spins (Rules 128, 139).
func (m *Model) refreshLevel(parent *TreeNode) tea.Cmd {
	var load tea.Cmd
	if parent == nil {
		if m.refreshingRoots {
			return nil
		}
		m.refreshingRoots = true
		load = m.loadRootGroups()
	} else {
		if parent.Loading {
			return nil
		}
		parent.Loading = true
		load = m.loadChildren(parent)
	}
	m.refreshing++
	return tea.Batch(m.spinner.Tick, load)
}

// levelChanged records that the forge confirmed a create or a delete in the
// level under parent. A refresh of that level already in flight may have been
// read before the change, so its answer is marked to be discarded (Stale).
func (m *Model) levelChanged(parent *TreeNode) {
	if parent == nil {
		if m.refreshingRoots {
			m.rootsStale = true
		}
		return
	}
	if parent.Loading && !parent.Blocking {
		parent.Stale = true
	}
}

// settleRefresh is the bookkeeping side of a level's answer arriving.
func (m *Model) settleRefresh(parent *TreeNode) {
	if parent == nil {
		m.refreshingRoots = false
	} else {
		parent.Loading = false
	}
	if m.refreshing > 0 {
		m.refreshing--
	}
}

// seedFromIndex puts the index's roots on screen when the view is built, so
// the explorer opens on a list rather than on a spinner. Init's request for
// the roots then refreshes them in place.
func (m *Model) seedFromIndex() {
	if !m.shared.IsAuthenticated {
		return
	}
	roots, known := m.indexedLevel(nil)
	if !known {
		return
	}
	m.nodes = roots
	m.firstLoadDone = true
	m.refreshingRoots = true
	m.refreshing++
	m.updateTableRows()
}

// handleForgeIndexChanged is the router saying a new index is in place: the
// prompt reranks against it, and a first load still waiting on the forge shows
// the roots the index already has.
func (m Model) handleForgeIndexChanged() (tea.Model, tea.Cmd) {
	if m.finder != nil {
		m.finder.SetCandidates(m.fuzzyCandidates(), m.shared.ForgeIndex.Skipped())
	}
	// Only a first load still waiting on the forge takes the roots from here;
	// anything already on screen is the forge's or the index's own answer.
	if m.firstLoadDone || len(m.nodes) > 0 || !m.shared.IsAuthenticated {
		return m, nil
	}
	roots, known := m.indexedLevel(nil)
	if !known {
		return m, nil
	}
	// The root request is still out; it is now a refresh of what is shown.
	m.nodes = carryOverCreating(m.nodes, roots)
	m.loading = false
	m.firstLoadDone = true
	m.refreshingRoots = true
	m.refreshing++
	m.updateTableRows()
	m.table.GotoTop()
	return m, nil
}

// handleRootGroupsLoaded lays the decorated roots over what is shown.
func (m Model) handleRootGroupsLoaded(msg RootGroupsLoadedMsg) (tea.Model, tea.Cmd) {
	background := m.refreshingRoots
	if background {
		m.settleRefresh(nil)
		if m.rootsStale {
			m.rootsStale = false
			return m, m.refreshLevel(nil)
		}
	}
	selected := m.selectedPath()
	m.loading = false
	m.firstLoadDone = true
	m.nodes = mergeLevel(m.nodes, msg.Nodes)
	m.error = ""
	m.updateTableRows()
	if background && m.currentGroupNode == nil {
		m.selectRow(selected)
	} else if !background {
		m.table.GotoTop()
	}
	return m, replaceIndexedLevel(nil, m.nodes)
}

// selectedPath is the path under the cursor, empty when there is none. It is
// what keeps the cursor on the same row when a level is re-read in place.
func (m Model) selectedPath() string {
	if node, ok := m.selectedNode(); ok {
		return node.FullPath
	}
	return ""
}

// handleBackgroundLoadError reports a failed refresh of a level that stays on
// screen from the index: the rows are still true as of the last walk, so they
// stay, and the footer says the forge could not be asked.
func (m Model) handleBackgroundLoadError(msg LoadErrorMsg) (tea.Model, tea.Cmd) {
	m.settleRefresh(msg.ParentNode)
	log.Printf("ERROR [explorer] refresh level: %v", msg.Error)
	return m, m.footer.Error("Could not refresh from " + m.vocab().Name + " — showing the last known list")
}

// isBackground reports whether a load error is for a level already shown.
func (m Model) isBackground(msg LoadErrorMsg) bool {
	if msg.ParentNode == nil {
		return m.refreshingRoots
	}
	return !msg.ParentNode.Blocking
}
