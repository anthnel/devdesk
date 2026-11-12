package explorer

import (
	"log"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/datatable"
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
	// Rule 130: C is greyed without a session, and a greyed key that is pressed
	// anyway says why rather than doing nothing. An empty level is not a
	// refusal worth a line — there is simply nothing to tick.
	if a := m.connected(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	if len(m.nodes) == 0 {
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
	// A row the forge has not confirmed cannot be cloned — there is nothing at
	// that path yet — and refusing it here is what lets the icon column give
	// the activity dot priority over the checkbox without the two ever competing.
	if act := m.actionable(); !act.Enabled() {
		return m, m.footer.Warn(act.Reason)
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
	m.clone = newCloneList(msg.Path, run.events)
	m.clone.table.SetBlinkPhase(m.table.BlinkPhase())
	m.clone.table.Resize(m.width, max(m.height-1, 1))
	// An **open** run: the walk that finds repositories is itself the slow
	// part, so there is no target list to register (D3). The cancel travels
	// with it because the pipeline was started here, in Update — which is also
	// what makes `esc` reach the registry rather than the channel behind its
	// back.
	return m, jobs.StartCancellable(m.cloneJobRun(msg.Path), waitForCloneEvent(run.events), run.cancel)
}

// cloneRun describes the batch to the registry. The destination names it: it is
// what a human recognises a clone by, and the roots are already in the rows.
func (m Model) cloneJobRun(target string) jobs.Run {
	return jobs.NewOpenRun(jobs.KindClone, command.ViewGitExplorer, "", target)
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

// handleCloneEvent logs what deserves logging and waits for the next event.
//
// It no longer folds anything in: the router applied the event to the registry
// before handing it here, and the rows arrive in the broadcast that follows
// (D1). What is left is the log and the re-issued read.
func (m Model) handleCloneEvent(msg CloneEventMsg) (tea.Model, tea.Cmd) {
	if m.clone == nil {
		return m, nil
	}
	if msg.event.kind == cloneEnded && msg.event.err != nil {
		log.Printf("ERROR [explorer] clone %s: %s", msg.event.path, jobs.SafeCause(msg.event.err))
	}
	if msg.event.kind == cloneWalkFailed {
		log.Printf("ERROR [explorer] discover %s: %s", msg.event.path, jobs.SafeCause(msg.event.err))
	}
	return m, waitForCloneEvent(m.clone.events)
}

// handleCloneRunFinished is the closed channel: the walk is over and every
// clone has returned. The router has sealed the run by the time this runs.
func (m Model) handleCloneRunFinished() (tea.Model, tea.Cmd) {
	if m.clone == nil {
		return m, nil
	}

	// Decision 13 keeps only what workspaces can show, and a failed clone wrote
	// nothing. Naming the failures now is the only record there will be.
	if failed := m.clone.failures(); len(failed) > 0 {
		return m, m.footer.Error(failureSummary(failed))
	}
	return m, nil
}

// handleJobsChanged takes the router's snapshot.
//
// Two screens read it. The clone list reads its own run. The tree reads, per
// row, the runs and failures of its own context (rowActivity filters by
// m.contextName and by kind), so a scan in `ws` or another context's failure
// paints nothing here.
//
// It is the only writer of m.jobs, m.failures and m.contextName, and nothing is
// reconciled: the snapshot replaces what was held rather than being merged.
func (m Model) handleJobsChanged(msg jobs.ChangedMsg) (tea.Model, tea.Cmd) {
	m.jobs = msg.Runs
	m.failures = slices.Clone(msg.Failures)
	m.contextName = msg.Context

	if m.clone != nil {
		if run, ok := cloneRunFrom(msg.Runs); ok {
			m.clone.setRun(run)
		}
		return m, nil
	}
	// The rows are rebuilt from the new snapshot.
	m.updateTableRows()
	return m, nil
}

// handleFrameMsg takes the animation's blink phase. The tree table reads it at
// render, and so does the clone list: no row is rebuilt. The footer load line
// takes the same phase for its activity dot.
func (m Model) handleFrameMsg(msg jobs.FrameMsg) (tea.Model, tea.Cmd) {
	m.table.SetBlinkPhase(msg.Blink)
	m.footer.SetBlinkPhase(msg.Blink)

	if m.clone != nil {
		m.clone.table.SetBlinkPhase(msg.Blink)
	}
	return m, nil
}

// ActivityTables returns the read-only face of the tree and clone tables, keyed
// by the names the activity-column guard (internal/app) knows them by. The clone
// table is reported without a run too, built by the same constructor, so the
// guard can check it at any width.
func (m Model) ActivityTables() map[string]datatable.ActivityTable {
	clone := m.clone
	if clone == nil {
		t := newCloneTable()
		t.Resize(m.width, max(m.height-1, 1))
		return map[string]datatable.ActivityTable{"explorer/tree": &m.table, "explorer/clone-list": &t}
	}
	return map[string]datatable.ActivityTable{"explorer/tree": &m.table, "explorer/clone-list": &clone.table}
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
	if m.clone.finished() {
		return m.handleCloneClose()
	}
	if m.clone.cancelling {
		return m, nil
	}

	// The registry stops the run: it holds the cancel, and it is what marks the
	// run cancelled so `:jobs` says "cancelled" rather than "done". Calling the
	// pipeline's cancel from here would stop the walk behind the registry's
	// back, and leave the record claiming the run finished on its own.
	m.clone.cancelling = true
	return m, jobs.CancelOpen(jobs.KindClone)
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
	case m.clone.finished():
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
