package explorer

import (
	"log"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forge"
)

// The clone flow, end to end (§3.16):
//
//	c              → ModeSelecting, the tree with checkboxes
//	space          → tick the row; ←→ still drill, so deselecting means going in
//	enter          → borrow the workspaces view for a destination
//	                 (the selection survives the round trip — it names paths)
//	destination    → ModeCloning, the pipeline starts
//	esc            → cancel: discovery stops, running clones are awaited
//	esc (finished) → back to the tree, list discarded (decision 13)

// handleCloneStart enters the selection mode.
func (m Model) handleCloneStart() (tea.Model, tea.Cmd) {
	if !m.shared.IsAuthenticated || len(m.nodes) == 0 {
		return m, nil
	}
	m.mode = ModeSelecting
	m.selection = newCloneSelection()
	m.selectionNodes = map[string]*TreeNode{}
	m.updateTableRows()
	return m, nil
}

// handleSelectionToggle ticks or unticks the row under the cursor (Space,
// Rule 135).
//
// The node is kept beside the path because the walk needs somewhere to start
// and a root ticked three levels down is not reachable from the current level
// once the user has come back up. The selection itself stays paths-only — it
// has to answer for descendants nobody has fetched.
func (m Model) handleSelectionToggle() (tea.Model, tea.Cmd) {
	node, ok := m.selectedNode()
	if !ok {
		return m, nil
	}

	m.selection.toggle(node.FullPath)
	if m.selection.isRoot(node.FullPath) {
		m.selectionNodes[node.FullPath] = node
	} else {
		delete(m.selectionNodes, node.FullPath)
	}
	m.updateTableRows()
	return m, nil
}

// handleSelectionConfirm asks the app for a destination directory.
func (m Model) handleSelectionConfirm() (tea.Model, tea.Cmd) {
	if m.selection.isEmpty() {
		return m, m.footer.Warn("Nothing selected — press space to tick a group or a project")
	}
	return m, func() tea.Msg { return CloneSelectionRequestMsg{} }
}

// handleSelectionCancel leaves the selection mode, dropping the selection.
func (m Model) handleSelectionCancel() (tea.Model, tea.Cmd) {
	m.mode = ModeNormal
	m.selection = newCloneSelection()
	m.selectionNodes = nil
	m.updateTableRows()
	return m, nil
}

// handleCloneDestinationSelected starts the pipeline.
func (m Model) handleCloneDestinationSelected(msg CloneDestinationSelectedMsg) (tea.Model, tea.Cmd) {
	roots := m.rootNodes()
	if len(roots) == 0 {
		// The selection survived the round trip but named nothing reachable,
		// which is a bug rather than a user action — say so instead of opening
		// an empty list.
		log.Printf("ERROR [explorer] clone: the selection resolved to no nodes")
		cancelled, _ := m.handleSelectionCancel()
		m = cancelled.(Model)
		return m, m.footer.Error("Nothing to clone — check logs")
	}

	run := startCloneRun(cloneSpec{
		backend:         m.shared.Forge,
		roots:           roots,
		selection:       m.selection,
		target:          msg.Path,
		secrets:         m.shared.Secrets.Storage,
		cloneMethod:     forge.CloneMethod(m.config.Forge.CloneMethod),
		gitlabURL:       m.config.Forge.URL,
		jobs:            m.config.Forge.Pull.ParallelJobs,
		includeArchived: m.config.Forge.Pull.IncludeArchived,
	})

	m.mode = ModeCloning
	m.clone = newCloneList(msg.Path, run)
	m.clone.table.Resize(m.width, max(m.height-1, 1))
	return m, tea.Batch(m.spinner.Tick, waitForCloneEvent(run.events))
}

// rootNodes resolves the ticked paths to the nodes the walk starts from, in a
// stable order so two runs of the same selection read the same way.
func (m Model) rootNodes() []*TreeNode {
	paths := m.selection.rootPaths()
	nodes := make([]*TreeNode, 0, len(paths))
	for _, path := range paths {
		if node, ok := m.selectionNodes[path]; ok {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// handleCloneEvent folds one pipeline event in and waits for the next.
func (m Model) handleCloneEvent(msg CloneEventMsg) (tea.Model, tea.Cmd) {
	if m.clone == nil {
		return m, nil
	}
	if msg.event.kind == cloneEnded && msg.event.err != nil {
		log.Printf("ERROR [explorer] clone %s: %v", msg.event.path, msg.event.err)
	}
	if msg.event.kind == cloneWalkFailed {
		log.Printf("ERROR [explorer] discover %s: %v", msg.event.path, msg.event.err)
	}
	m.clone.apply(msg.event)
	return m, waitForCloneEvent(m.clone.run.events)
}

// handleCloneRunFinished is the closed channel: every clone has returned.
func (m Model) handleCloneRunFinished() (tea.Model, tea.Cmd) {
	if m.clone == nil {
		return m, nil
	}
	m.clone.finished = true

	// Decision 13 keeps only what workspaces can show, and a failed clone wrote
	// nothing. Naming the failures now is the only record there will be.
	if failed := m.clone.failures(); len(failed) > 0 {
		return m, m.footer.Error(failureSummary(failed))
	}
	return m, nil
}

// failureSummary names one failure and counts the rest — the footer is one line.
func failureSummary(failed []string) string {
	if len(failed) == 1 {
		return "Failed to clone " + failed[0] + " — check logs"
	}
	return "Failed to clone " + failed[0] + " and " +
		plural(len(failed)-1, "other") + " — check logs"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// handleCloneEsc cancels a running pipeline, or closes a finished one.
//
// The two are one key because they are one intent — "I am done with this" — and
// separating them would need the user to know which state the list is in. A
// second press while cancelling does nothing: forcing would mean killing a
// `git clone` mid-write, which is the half-repository decision 12 rules out.
func (m Model) handleCloneEsc() (tea.Model, tea.Cmd) {
	if m.clone == nil {
		m.mode = ModeNormal
		return m, nil
	}
	if m.clone.finished {
		return m.handleCloneClose()
	}
	if m.clone.cancelling {
		return m, nil
	}

	m.clone.cancelling = true
	m.clone.run.cancel()
	return m, nil
}

// handleCloneClose discards the list and returns to the tree.
func (m Model) handleCloneClose() (tea.Model, tea.Cmd) {
	m.clone = nil
	m.selection = newCloneSelection()
	m.selectionNodes = nil
	m.mode = ModeNormal
	m.updateTableRows()
	return m, nil
}

// cloneStatusLine is what the view puts under the list: what the run is doing,
// then what it did.
func (m Model) cloneStatusLine() string {
	if m.clone == nil {
		return ""
	}
	found, cloned, present, failed := m.clone.counts()

	var b strings.Builder
	switch {
	case m.clone.finished:
		b.WriteString("Done — ")
	case m.clone.cancelling:
		b.WriteString("Cancelling — " + plural(m.clone.running(), "clone") + " finishing · ")
	default:
		b.WriteString(strconv.Itoa(found) + " found · ")
	}
	b.WriteString(strconv.Itoa(cloned) + " cloned · " + strconv.Itoa(present) + " already there")
	if failed > 0 {
		b.WriteString(" · " + strconv.Itoa(failed) + " failed")
	}
	return b.String()
}
