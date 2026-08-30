package explorer

import (
	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
)

// What this view knows about the work it has started is a snapshot the router
// hands it, and nothing else (D1).
//
// The clone was already registered; the create and the delete were not, and
// they are the two that talk to a forge over the network. Left outside, each
// would have needed its own flag, its own spinner chain and its own guard —
// which is the fourth bookkeeping the package doc of internal/jobs opens by
// describing, and the spinner chain is the half that fails silently: this
// view's own chain stops on `!m.loading` (handleSpinnerTick), so a frame
// stamped from it would freeze on frame zero the moment the tree settled.

// createRun and deleteRun are single-target runs. The label is the name a human
// gave the thing, not the path: it is what `:jobs` shows, and the path is
// already the target.
func createRun(target, label string) jobs.Run {
	return jobs.NewRun(jobs.KindCreate, command.ViewGitExplorer, "", label, target)
}

func deleteRun(target, label string) jobs.Run {
	return jobs.NewRun(jobs.KindDelete, command.ViewGitExplorer, "", label, target)
}

// liveRuns returns the runs still going, whatever view started them.
//
// Filtered to this view's kinds, which is the difference from workspaces: a
// scan running in `ws` names a directory on disk, and the paths in this tree
// are the forge's. Two identically-named strings from two namespaces would
// otherwise spin a row here for work that has nothing to do with it.
func (m Model) liveRuns() []jobs.Run {
	out := make([]jobs.Run, 0, len(m.jobs))
	for _, run := range jobs.Unfinished(m.jobs) {
		if run.Kind == jobs.KindCreate || run.Kind == jobs.KindDelete {
			out = append(out, run)
		}
	}
	return out
}

// workingKind reports whether a path is queued or running under one kind.
func (m Model) workingKind(kind jobs.Kind, target string) bool {
	for _, run := range m.liveRuns() {
		if run.Kind != kind {
			continue
		}
		for _, item := range run.Items {
			if item.Target == target && !item.State.Terminal() {
				return true
			}
		}
	}
	return false
}

// creating and deleting are the two readings the rows and the guards need.
func (m Model) creating(path string) bool { return m.workingKind(jobs.KindCreate, path) }
func (m Model) deleting(path string) bool { return m.workingKind(jobs.KindDelete, path) }

// busyMessage is what an action says when the forge is already being asked
// something about this row. One message rather than two, because the user's
// next move is the same either way: wait.
const busyMessage = "Already busy — a create or delete is running here"

// busy reports whether the row is held by work in flight.
//
// It answers from the registry, so it sees work started anywhere — which for
// this view means a second window on the same tree cannot issue the delete a
// first one is already running.
func (m Model) busy(path string) bool {
	return m.creating(path) || m.deleting(path)
}

// ── What each message tells the registry (jobs.Reporter) ─────────────────────
//
// The mapping lives here rather than in the router for the reason Reporter
// exists: "a project whose template failed still exists" is this package's
// vocabulary, and a router that knew it would grow a branch per view.

func (m GroupCreatedMsg) Transition() jobs.Transition {
	t := jobs.Transition{Kind: jobs.KindCreate, Target: m.Target, State: jobs.ItemDone}
	if m.Error != nil {
		t.State = jobs.ItemFailed
		t.Detail = "create failed — check logs"
	}
	return t
}

func (m ProjectCreatedMsg) Transition() jobs.Transition {
	t := jobs.Transition{Kind: jobs.KindCreate, Target: m.Target, State: jobs.ItemDone}
	switch {
	case m.Error != nil:
		t.State = jobs.ItemFailed
		t.Detail = "create failed — check logs"
	case m.TemplateError != nil:
		// The project exists; only the template did not apply. That is neither
		// a success worth nothing said nor a failure — the row is there, and
		// what did not happen is worth keeping on the run.
		t.Detail = "template failed"
	}
	return t
}

func (m DeleteCompleteMsg) Transition() jobs.Transition {
	t := jobs.Transition{Kind: jobs.KindDelete, Target: m.Target, State: jobs.ItemDone}
	if m.Error != nil {
		t.State = jobs.ItemFailed
		t.Detail = "delete failed — check logs"
	}
	return t
}

// Compile-time proof that every message reporting on registered work can be
// applied to the registry. One added without it would be routed, would update
// the view, and would leave its row spinning for the life of the view — which
// is precisely the failure the registry exists to remove.
var (
	_ jobs.Reporter = GroupCreatedMsg{}
	_ jobs.Reporter = ProjectCreatedMsg{}
	_ jobs.Reporter = DeleteCompleteMsg{}
)
