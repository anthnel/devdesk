package workspaces

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/jobs"
)

// This view's knowledge of what is running is a snapshot the router hands it,
// and nothing else (D1). It replaced three maps — scanningPaths, syncingPaths,
// deletingPaths — and the syncRun that counted a batch alongside them.
//
// What the three maps could not do is the whole point: they knew only what this
// view had started. A scan launched on the same repository from `:sec` was
// invisible to busy(), so the guard let a second one through and the two wrote
// the same cache entry. The registry is one bookkeeping, so the guard now
// answers for work started anywhere.

// scanRun builds the run behind S, A and ctrl+a. Label is what `:jobs` shows,
// and the directory being worked in is what identifies the batch to a human.
func (m Model) scanRun(targets []string) jobs.Run {
	return jobs.NewRun(jobs.KindScan, command.ViewWorkspaces, "", m.runLabel(), targets...)
}

func (m Model) syncRun(targets []string) jobs.Run {
	return jobs.NewRun(jobs.KindSync, command.ViewWorkspaces, "", m.runLabel(), targets...)
}

func (m Model) deleteRun(target string) jobs.Run {
	return jobs.NewRun(jobs.KindDelete, command.ViewWorkspaces, "", pathBaseName(target), target)
}

// runLabel names a batch by the directory it was launched in.
func (m Model) runLabel() string {
	if m.currentPath != "" {
		return pathBaseName(m.currentPath)
	}
	return pathBaseName(m.getExpandedWorkspacesDir())
}

// handleJobsChanged takes the router's snapshot. It is the only writer of the
// two fields, and there is nothing to reconcile: the snapshot replaces what was
// held rather than being merged into it.
func (m Model) handleJobsChanged(msg jobs.ChangedMsg) (tea.Model, tea.Cmd) {
	m.jobs = msg.Runs
	m.jobFrame = msg.Frame
	m.footer.SetSpinnerFrame(msg.RenderedFrame)
	m.refreshRows()
	return m, nil
}

// liveRuns returns the runs this view is showing: the ones still going, whatever
// view started them.
//
// Not filtered by origin — that is the point. A scan `:sec` launched on a
// repository listed here spins on its row, because it is the same repository
// and the same work.
func (m Model) liveRuns() []jobs.Run {
	return jobs.Unfinished(m.jobs)
}

// itemState returns where a target stands in the live runs of a kind, and
// whether it is there at all.
func (m Model) itemState(kind jobs.Kind, target string) (jobs.ItemState, bool) {
	for _, run := range m.liveRuns() {
		if run.Kind != kind {
			continue
		}
		for _, item := range run.Items {
			if item.Target == target && !item.State.Terminal() {
				return item.State, true
			}
		}
	}
	return "", false
}

// working reports whether a target is queued or running under any kind.
func (m Model) working(target string) bool {
	for _, run := range m.liveRuns() {
		for _, item := range run.Items {
			if item.Target == target && !item.State.Terminal() {
				return true
			}
		}
	}
	return false
}

// anyWorking reports whether anything at all is going, which is what decides
// whether the footer's progress line carries a spinner.
func (m Model) anyWorking() bool { return len(m.liveRuns()) > 0 }

// workingKind reports whether a target is queued or running under one kind.
func (m Model) workingKind(kind jobs.Kind, target string) bool {
	_, ok := m.itemState(kind, target)
	return ok
}

// scanning, syncing and deleting are the three readings the rows need. They
// replace the three maps one for one, and they read the same source.
func (m Model) scanning(path string) bool { return m.workingKind(jobs.KindScan, path) }
func (m Model) syncing(path string) bool  { return m.workingKind(jobs.KindSync, path) }
func (m Model) deleting(path string) bool { return m.workingKind(jobs.KindDelete, path) }

// ── The footer line (D9) ─────────────────────────────────────────────────────

// jobsStatusLine is what the footer says while work runs.
//
// It is derived every frame rather than posted as a message, and Rule 128 says
// why: a batch outlives the three seconds a footer message gets, so a line set
// on the first repository would vanish while the tenth was still going.
//
// Three forms, and the third is a deliberate surrender:
//
//	Scanning — 3/12                     one kind, started here
//	Scanning — 3/12 · Syncing — 1/4      two kinds, both started here
//	3 jobs running — :jobs for details   anything else
//
// The degraded form is what a footer can honestly say about work it does not
// own. One line cannot carry four batches launched from three views, and a line
// that showed only the part it recognised would be worse than one that admits
// there is more.
func (m Model) jobsStatusLine() string {
	live := m.liveRuns()
	if len(live) == 0 {
		return ""
	}

	mine := make([]jobs.Run, 0, len(live))
	for _, run := range live {
		if run.Origin == command.ViewWorkspaces {
			mine = append(mine, run)
		}
	}

	// Anything started elsewhere, or more than one batch of a kind, and the
	// detailed line stops being able to tell the truth.
	if len(mine) != len(live) || !oneRunPerKind(mine) {
		return plural(len(live), "job", "jobs") + " running — :jobs for details"
	}

	parts := make([]string, 0, len(mine))
	for _, kind := range jobs.Kinds() {
		for _, run := range mine {
			if run.Kind == kind {
				parts = append(parts, kindVerb(kind)+" — "+
					strconv.Itoa(run.Done())+"/"+strconv.Itoa(run.Total()))
			}
		}
	}
	return strings.Join(parts, " · ")
}

// oneRunPerKind reports whether no two runs share a kind. Two scans at once are
// two counters, and "Scanning — 3/12 · Scanning — 1/2" reads as a bug.
func oneRunPerKind(runs []jobs.Run) bool {
	seen := make(map[jobs.Kind]bool, len(runs))
	for _, run := range runs {
		if seen[run.Kind] {
			return false
		}
		seen[run.Kind] = true
	}
	return true
}

// kindVerb is what the footer calls a kind while it runs. Present participle,
// English US (Rule 129).
func kindVerb(kind jobs.Kind) string {
	switch kind {
	case jobs.KindScan:
		return "Scanning"
	case jobs.KindSync:
		return "Syncing"
	case jobs.KindDelete:
		return "Deleting"
	case jobs.KindClone:
		return "Cloning"
	case jobs.KindPull:
		return "Pulling"
	}
	return "Working"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// ── What each message tells the registry (jobs.Reporter) ─────────────────────
//
// The mapping lives here and not in the router: "a sync that was refused
// because the tree is dirty is a skip, and the reason is worth keeping" is this
// package's vocabulary, and a router that knew it would grow one branch per
// view.

// syncDetailUpdated marks the one settled outcome the item state cannot carry.
// A sync that fast-forwarded and one that had nothing to do are both done; the
// summary counts them apart, and this is the only thing that tells them apart.
const syncDetailUpdated = "updated"

func (m WorkspaceScanStartingMsg) Transition() jobs.Transition {
	return jobs.Transition{Kind: jobs.KindScan, Target: m.RepoPath, State: jobs.ItemRunning}
}

func (m WorkspaceScanCompleteMsg) Transition() jobs.Transition {
	t := jobs.Transition{Kind: jobs.KindScan, Target: m.RepoPath, State: jobs.ItemDone}
	if m.Error != nil {
		t.State = jobs.ItemFailed
		t.Detail = "scan failed — check logs"
	}
	return t
}

func (m WorkspaceSyncStartingMsg) Transition() jobs.Transition {
	return jobs.Transition{Kind: jobs.KindSync, Target: m.RepoPath, State: jobs.ItemRunning}
}

func (m WorkspaceSyncCompleteMsg) Transition() jobs.Transition {
	t := jobs.Transition{Kind: jobs.KindSync, Target: m.RepoPath, State: jobs.ItemDone}
	switch {
	case m.Error != nil:
		t.State = jobs.ItemFailed
		t.Detail = "sync failed — check logs"
	case m.Outcome == git.SyncSkipped:
		// A refusal is not a failure: the fetch happened, the merge did not,
		// and the reason is what the summary names one repository by.
		t.State = jobs.ItemSkipped
		t.Detail = m.Reason
	case m.Outcome == git.SyncUpdated:
		t.Detail = syncDetailUpdated
	}
	return t
}

func (m EntryDeletedMsg) Transition() jobs.Transition {
	t := jobs.Transition{Kind: jobs.KindDelete, Target: m.Path, State: jobs.ItemDone}
	if m.Error != nil {
		t.State = jobs.ItemFailed
		t.Detail = "delete failed — check logs"
	}
	return t
}

// Compile-time proof that every progress message this view emits can be applied
// to the registry. A message added without one would be routed, would update
// the view, and would leave its row spinning for the life of the view — which
// is precisely the failure the registry exists to remove.
var (
	_ jobs.Reporter = WorkspaceScanStartingMsg{}
	_ jobs.Reporter = WorkspaceScanCompleteMsg{}
	_ jobs.Reporter = WorkspaceSyncStartingMsg{}
	_ jobs.Reporter = WorkspaceSyncCompleteMsg{}
	_ jobs.Reporter = EntryDeletedMsg{}
)

// ── The sync summary ─────────────────────────────────────────────────────────

// syncSummary is what the footer says once a sync batch has settled.
//
// It is posted as a footer message rather than derived like the progress line,
// and the difference is Rule 128's own: progress is a state, which is why it
// cannot carry a timer, and an outcome is an event, which is exactly what a
// footer message is. The old code derived both from one struct because they
// shared a function; once progress comes from the registry, they stop needing
// to be the same thing.
func (m Model) syncSummary(run jobs.Run) string {
	var updated, upToDate, skipped, failed int
	var firstSkipped, firstSkippedReason, firstFailed string

	for _, item := range run.Items {
		switch {
		case item.State == jobs.ItemFailed:
			failed++
			if firstFailed == "" {
				firstFailed = pathBaseName(item.Target)
			}
		case item.State == jobs.ItemSkipped:
			// A cancellation lands here too, and its Detail says so: the
			// registry writes "cancelled" on an item that never ran, which
			// reads correctly in "1 skipped (api: cancelled)".
			skipped++
			if firstSkipped == "" {
				firstSkipped = pathBaseName(item.Target)
				firstSkippedReason = item.Detail
			}
		case item.Detail == syncDetailUpdated:
			updated++
		default:
			upToDate++
		}
	}

	var parts []string
	if updated > 0 {
		parts = append(parts, plural(updated, "repository", "repositories")+" updated")
	}
	if upToDate > 0 {
		parts = append(parts, strconv.Itoa(upToDate)+" up to date")
	}
	if skipped > 0 {
		parts = append(parts, strconv.Itoa(skipped)+" skipped ("+firstSkipped+": "+firstSkippedReason+")")
	}
	if failed > 0 {
		parts = append(parts, strconv.Itoa(failed)+" failed ("+firstFailed+") — check logs")
	}
	if m.syncUnreadable > 0 {
		parts = append(parts, plural(m.syncUnreadable, "directory", "directories")+" unreadable — check logs")
	}
	if len(parts) == 0 {
		return "Nothing to sync"
	}
	return strings.Join(parts, " · ")
}
