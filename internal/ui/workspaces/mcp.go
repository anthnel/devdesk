package workspaces

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/jobs"
)

// What an agent asks this view to do (§3.61).
//
// The messages exist so that the router can reach the view the way a key does:
// the view is the only place that knows how to turn "scan these paths" into
// targets, scan options and a cache to purge, and having the router build that
// itself would undo what §3.58 spent an entry unifying.
//
// The view does not know who asked. Invocation is a string it carries onto the
// run and nothing more; what refuses, and with which sentence, is the same
// Availability the header greys the shortcut with (Rule 130).

// ScanRequestedMsg asks for a scan of the named repositories, or of everything
// in view when none are named.
type ScanRequestedMsg struct {
	// Paths are absolute repository paths. Empty means every repository
	// currently listed, which is what `A` does.
	Paths []string
	// Invocation names the MCP call, and is stamped onto the run.
	Invocation string
}

// SyncRequestedMsg asks for a fast-forward of the named repositories.
type SyncRequestedMsg struct {
	Paths      []string
	Invocation string
}

// handleScanRequested is `S` and `A` reached from an agent instead of a key.
//
// It does not purge: `A` scans what has never been scanned, and the purging
// variant is behind a modal with a checkbox because it is the closest this
// application comes to losing data by accident (§3.26). A tool call has no
// modal, so it gets the non-destructive half.
func (m Model) handleScanRequested(msg ScanRequestedMsg) (tea.Model, tea.Cmd) {
	if state := m.scannerState(); !state.Enabled() {
		return m, jobs.Refuse(msg.Invocation, state.Reason)
	}

	targets := msg.Paths
	if len(targets) == 0 {
		targets = m.collectAllRepoPaths()
	}

	known := m.knownRepoPaths()
	var queue []string
	for _, path := range targets {
		if !known[path] {
			return m, jobs.Refuse(msg.Invocation, "No repository at "+path+" — workspaces_list is what this context knows about")
		}
		if !m.busy(path) {
			queue = append(queue, path)
		}
	}
	if len(queue) == 0 {
		return m, jobs.Refuse(msg.Invocation, scanNothingToDo(targets))
	}

	return m, jobs.WithInvocation(msg.Invocation, jobs.StartInContext(
		m.scanRun(queue),
		func(contextName string) tea.Cmd { return batchScanCmd(queue, m.scanOptions(), contextName) },
	))
}

// handleSyncRequested is `F` reached from an agent.
func (m Model) handleSyncRequested(msg SyncRequestedMsg) (tea.Model, tea.Cmd) {
	targets := msg.Paths
	if len(targets) == 0 {
		targets = m.collectAllRepoPaths()
	}

	known := m.knownRepoPaths()
	var queue []string
	for _, path := range targets {
		if !known[path] {
			return m, jobs.Refuse(msg.Invocation, "No repository at "+path+" — workspaces_list is what this context knows about")
		}
		if !m.busy(path) {
			queue = append(queue, path)
		}
	}
	if len(queue) == 0 {
		return m, jobs.Refuse(msg.Invocation, syncNothingToDo(targets))
	}

	return m, jobs.WithInvocation(msg.Invocation,
		jobs.Start(m.syncRun(queue), batchSyncCmd(queue, m.syncSpec())))
}

// knownRepoPaths is what this view will act on, as a set.
//
// A path the view does not list is refused rather than passed to git: the
// alternative is an agent typo becoming a scan of somewhere else on the disk,
// and "no such repository" is a better answer than a run that finds nothing.
func (m Model) knownRepoPaths() map[string]bool {
	paths := m.collectAllRepoPaths()
	known := make(map[string]bool, len(paths))
	for _, path := range paths {
		known[path] = true
	}
	return known
}

// scanNothingToDo and syncNothingToDo separate "there is nothing here" from
// "it is already running" — an agent told only "nothing to do" would retry.
func scanNothingToDo(targets []string) string {
	if len(targets) == 0 {
		return "No git repository in this context's workspaces directory"
	}
	return busyMessage
}

func syncNothingToDo(targets []string) string {
	if len(targets) == 0 {
		return "No git repository in this context's workspaces directory"
	}
	return busyMessage
}
