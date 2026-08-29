package jobs

import (
	"testing"

	"github.com/anthnel/devdesk/internal/command"
)

// An **open** run is one whose target list is still being discovered (D3). It
// exists for the clone, whose walk is itself the slow part: a run that waited
// for the full list would show nothing for the minutes that matter.

func openClone(label string) Run {
	return NewOpenRun(KindClone, command.ViewGitExplorer, "default", label)
}

// The property everything else rests on: an open run is never finished, however
// its items stand. A walk that has found three repositories and cloned all
// three is still going.
func TestAnOpenRunIsNeverFinished(t *testing.T) {
	r := at(noon)
	r.Start(openClone("/ws"))

	if got := r.Snapshot()[0].State(); got != RunQueued {
		t.Errorf("a freshly opened run reads %q, want it queued", got)
	}
	if r.Running() != 1 {
		t.Error("an open run with no targets is not counted as running")
	}

	r.Discover(KindClone, "alpha/api", ItemQueued, "")
	r.Apply(Transition{Kind: KindClone, Target: "alpha/api", State: ItemDone})

	if got := r.Snapshot()[0].State(); got != RunRunning {
		t.Errorf("state = %q with every target settled but the walk still going, want running", got)
	}
	if !r.Snapshot()[0].EndedAt.IsZero() {
		t.Error("an open run was stamped with an end time")
	}
}

// Sealing is what lets it settle, and it is a separate act because the message
// that carries it names no target — it is a closed channel.
func TestSealingLetsAnOpenRunSettle(t *testing.T) {
	r := at(noon)
	r.Start(openClone("/ws"))
	r.Discover(KindClone, "alpha/api", ItemQueued, "")
	r.Apply(Transition{Kind: KindClone, Target: "alpha/api", State: ItemDone})

	if !r.Seal(KindClone) {
		t.Fatal("Seal found no open run")
	}

	snapshot := r.Snapshot()[0]
	if got := snapshot.State(); got != RunDone {
		t.Errorf("state = %q after sealing, want done", got)
	}
	if snapshot.Open() {
		t.Error("the run still reports itself open")
	}
	if snapshot.EndedAt.IsZero() {
		t.Error("the settled run was not stamped with an end time")
	}
}

// A seal with nothing open is not an error, and it is not silence either: the
// caller is told, so the router knows whether there is anything to broadcast.
func TestSealingWithNothingOpenIsRefused(t *testing.T) {
	r := at(noon)
	r.Start(run("a"))

	if r.Seal(KindClone) {
		t.Error("Seal claimed to close a run that does not exist")
	}
	if r.Seal(KindScan) {
		t.Error("Seal closed a scan, which is never open")
	}
}

// Discover is how a target reaches a run that did not know about it. A
// transition without it is refused, which is the guard that catches a launch
// site reporting on work it never registered.
func TestDiscoverAddsATargetAndRefusesWithoutAnOpenRun(t *testing.T) {
	r := at(noon)

	if r.Discover(KindClone, "alpha/api", ItemQueued, "") {
		t.Error("Discover added a target with no open run to add it to")
	}

	r.Start(openClone("/ws"))
	if !r.Discover(KindClone, "alpha/api", ItemQueued, "") {
		t.Fatal("Discover refused a target for the open run")
	}
	if got := r.Snapshot()[0].Total(); got != 1 {
		t.Errorf("the run holds %d targets, want 1", got)
	}

	// Without Discover the same target would be unknown to Apply, because it is
	// looked up rather than created.
	if r.Apply(Transition{Kind: KindClone, Target: "beta/web", State: ItemRunning}) {
		t.Error("a plain transition created a target out of nothing")
	}
}

// The walk reports a group it could not list as a target of its own, and that
// path may also turn up as a repository. One row, whichever arrives first.
func TestDiscoveringATargetTwiceAdvancesItRatherThanDuplicatingIt(t *testing.T) {
	r := at(noon)
	r.Start(openClone("/ws"))

	r.Discover(KindClone, "alpha/api", ItemQueued, "")
	r.Discover(KindClone, "alpha/api", ItemFailed, "discovery: 403")

	run := r.Snapshot()[0]
	if run.Total() != 1 {
		t.Fatalf("the run holds %d targets, want the one", run.Total())
	}
	if run.Items[0].State != ItemFailed || run.Items[0].Detail != "discovery: 403" {
		t.Errorf("item = %+v, want the second report to have replaced the first", run.Items[0])
	}
}

// Cancelling an open run seals it. Leaving it open would keep it unsettled for
// the life of the session — spinner chain and all — with nothing left that
// could ever close it, because the walk that would have is what was stopped.
func TestCancellingAnOpenRunSealsIt(t *testing.T) {
	r := at(noon)
	stopped := false
	id := r.Start(openClone("/ws"))
	r.AttachRun(id, func() { stopped = true })
	r.Discover(KindClone, "alpha/api", ItemQueued, "")

	if !r.CancelOpen(KindClone) {
		t.Fatal("CancelOpen found no open run")
	}

	if !stopped {
		t.Error("the run's cancel function was not called")
	}
	snapshot := r.Snapshot()[0]
	if snapshot.Open() {
		t.Error("a cancelled run is still open — nothing left could ever seal it")
	}
	if got := snapshot.State(); got != RunCancelled {
		t.Errorf("state = %q, want cancelled — `:jobs` has to tell that from done", got)
	}
	if r.Running() != 0 {
		t.Error("a cancelled run still counts as running")
	}
}

// CancelOpen names a kind rather than an identifier, which is what lets a view
// stop the run it is looking at without holding registry state (D1).
func TestCancelOpenIgnoresSettledRunsOfTheSameKind(t *testing.T) {
	r := at(noon)
	first := r.Start(openClone("/ws/one"))
	r.Seal(KindClone)

	second := r.Start(openClone("/ws/two"))
	if !r.CancelOpen(KindClone) {
		t.Fatal("CancelOpen found no open run")
	}

	snapshot := r.Snapshot()
	if snapshot[0].ID != first || snapshot[0].State() != RunDone {
		t.Errorf("the settled run was touched: %+v", snapshot[0])
	}
	if snapshot[1].ID != second || snapshot[1].State() != RunCancelled {
		t.Errorf("the open run was not the one cancelled: %+v", snapshot[1])
	}
}

// A queued target of a cancelled walk is skipped with a reason, like every
// other kind — the wording is what the report reads back.
func TestCancellingAWalkSkipsWhatItHadNotStarted(t *testing.T) {
	r := at(noon)
	r.Start(openClone("/ws"))
	r.Discover(KindClone, "alpha/api", ItemQueued, "")
	r.Discover(KindClone, "beta/web", ItemQueued, "")
	r.Apply(Transition{Kind: KindClone, Target: "alpha/api", State: ItemRunning})

	r.CancelOpen(KindClone)

	items := r.Snapshot()[0].Items
	if items[0].State != ItemRunning {
		t.Errorf("the clone in flight reads %q, want it left running — it reports its own outcome", items[0].State)
	}
	if items[1].State != ItemSkipped || items[1].Detail != "cancelled" {
		t.Errorf("the queued target = %+v, want it skipped with its reason", items[1])
	}
}
