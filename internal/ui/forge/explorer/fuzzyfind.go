package explorer

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forgeindex"
	"github.com/anthnel/devdesk/internal/ui/fuzzy"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// "g" is the workspaces view's prompt (internal/ui/fuzzy), over the forge
// index instead of a directory walk: every namespace and repository the
// session can see, at any depth, matched by path. The candidates cost nothing
// to find here — the router already holds them — which is the whole reason the
// index exists beside the lazily-drilled tree.

// reasonNoIndexEntry is why a jump did not happen: the path the index offered
// is not where the index says any more. It happens when a level was re-read
// between the prompt opening and Enter, and what was there has gone.
const reasonNoIndexEntry = "That entry is no longer listed — ctrl+r to refresh"

// startFuzzyFind opens the prompt. Without an index yet the prompt still
// opens, saying it is waiting, and the router's next ForgeIndexChangedMsg
// fills it.
func (m Model) startFuzzyFind() (tea.Model, tea.Cmd) {
	if a := m.connected(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	m.mode = ModeFuzzyFinding
	m.finder = fuzzy.New("Indexing " + m.vocab().Name + "...")
	// Best-effort sizing; the router notices the footer height changed and
	// re-lays-out right after this returns (Rule 124).
	m.finder.Resize(m.width, m.height)
	if m.shared.ForgeIndex != nil {
		m.finder.SetCandidates(m.fuzzyCandidates(), m.shared.ForgeIndex.Skipped())
	}
	return m, nil
}

// fuzzyCandidates is every entry of the index, keyed and labelled by path.
func (m Model) fuzzyCandidates() []fuzzy.Candidate {
	entries := m.shared.ForgeIndex.All()
	out := make([]fuzzy.Candidate, len(entries))
	for i, e := range entries {
		icon, role := theme.IconRepository, theme.IconRoleRepository
		if e.Kind == forgeindex.KindNamespace {
			icon, role = theme.IconNamespace, theme.IconRoleNamespace
		}
		out[i] = fuzzy.Candidate{Key: e.Path, Label: e.Path, Icon: icon, Role: role}
	}
	return out
}

// jumpTo lands on path: inside it for a namespace, on its row for a
// repository.
//
// Every level on the way is in the index, so the navigation stack a drill-down
// would have built one → at a time is built in one go — reusing a node where
// the tree already holds one, taking the index's where it does not. No
// request is made to get there. The level landed on is then read from the
// forge like any other, behind what is shown.
func (m Model) jumpTo(path string) (tea.Model, tea.Cmd) {
	ix := m.shared.ForgeIndex
	target, ok := ix.Lookup(path)
	chain, reached := ix.Ancestors(path)
	if !ok || !reached {
		return m, m.footer.Warn(reasonNoIndexEntry)
	}
	if target.Kind == forgeindex.KindNamespace {
		chain = append(chain, target)
	}

	if len(m.nodes) == 0 {
		if roots, known := m.indexedLevel(nil); known {
			m.nodes = roots
			m.firstLoadDone = true
		}
	}

	// groups[i] is the node for chain[i]; the root level is m.nodes.
	groups := make([]*TreeNode, 0, len(chain))
	level := m.nodes
	var parent *TreeNode
	for _, e := range chain {
		node := findNode(level, e.Path)
		if node == nil {
			return m, m.footer.Warn(reasonNoIndexEntry)
		}
		if node.Children == nil {
			children, known := m.indexedLevel(node)
			if !known {
				return m, m.footer.Warn(reasonNoIndexEntry)
			}
			node.Children = children
		}
		groups = append(groups, node)
		parent, level = node, node.Children
	}

	// A drill-down pushes the level it leaves, and the root level is pushed
	// as nil — the stack for depth d is [nil, g1 … g(d-1)].
	m.navigationStack = nil
	m.currentGroupNode = parent
	if len(groups) > 0 {
		m.navigationStack = append([]*TreeNode{nil}, groups[:len(groups)-1]...)
	}
	// No cursor history exists for levels the user never browsed through, so
	// each restores to the top.
	m.cursorStack = make([]int, len(m.navigationStack))
	m.activeTabIndex = m.tabCount() - 1
	m.updateTableRows()
	m.table.GotoTop()
	if target.Kind == forgeindex.KindRepository {
		m.selectRow(path)
	}

	if parent != nil && !parent.Fresh {
		return m, m.refreshLevel(parent)
	}
	return m, nil
}

// findNode finds a node by path in one level.
func findNode(level []*TreeNode, path string) *TreeNode {
	for _, node := range level {
		if node.FullPath == path {
			return node
		}
	}
	return nil
}
