package jobs

import (
	"testing"

	"github.com/anthnel/devdesk/internal/command"
)

// D7 has two halves and they are not the same question.
//
// Stopping a *run* stops its queue: nothing further starts, whatever the kind.
// Cutting one target already in flight is only offered where cutting leaves
// nothing behind — a half-written clone on disk is worse than one that
// finished, and a half-deleted directory worse still.

func kindRun(kind Kind, states ...ItemState) Run {
	targets := make([]string, len(states))
	for i := range states {
		targets[i] = string(rune('a' + i))
	}
	r := NewRun(kind, command.ViewWorkspaces, "default", "~/work", targets...)
	for i, state := range states {
		r.Items[i].State = state
	}
	return r
}

// Stoppable is the question a view asks before offering the key (Rule 130), and
// the delete is the case it exists for: one item, in flight, of a kind that
// must never be cut. `!Finished()` would offer the key and then refuse it.
func TestStoppableAnswersForTheRunRatherThanItsKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  Run
		want bool
	}{
		{"a scan in flight", kindRun(KindScan, ItemRunning), true},
		{"a scan already done", kindRun(KindScan, ItemDone), false},
		{"a delete in flight", kindRun(KindDelete, ItemRunning), false},
		{"a sync with a queue left", kindRun(KindSync, ItemRunning, ItemQueued), true},
		{"a sync with nothing queued", kindRun(KindSync, ItemRunning), false},
		{"a clone still walking", NewOpenRun(KindClone, command.ViewGitExplorer, "default", "/ws"), true},
		{"a clone sealed and done", kindRun(KindClone, ItemDone), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.run.Stoppable(); got != tc.want {
				t.Errorf("Stoppable = %v, want %v", got, tc.want)
			}
		})
	}
}

// The item question is narrower, and it is the kind that answers it.
func TestItemStoppableFollowsTheKind(t *testing.T) {
	for _, tc := range []struct {
		kind Kind
		want bool
	}{
		{KindScan, true},
		{KindPull, true},
		{KindSync, false},
		{KindClone, false},
		{KindDelete, false},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			run := kindRun(tc.kind, ItemRunning)
			if got := run.ItemStoppable(run.Items[0]); got != tc.want {
				t.Errorf("ItemStoppable = %v, want %v", got, tc.want)
			}
			// A settled target is never stoppable, whatever the kind: there is
			// nothing left to stop.
			settled := kindRun(tc.kind, ItemDone)
			if settled.ItemStoppable(settled.Items[0]) {
				t.Error("a settled target reports itself stoppable")
			}
		})
	}
}

// A queued target is skipped outright — it will not run — and the rest of the
// batch carries on. That is the difference from cancelling the run.
func TestCancelItemSkipsAQueuedTargetAndLeavesTheRestGoing(t *testing.T) {
	r := at(noon)
	id := r.Start(kindRun(KindScan, ItemRunning, ItemQueued, ItemQueued))

	if !r.CancelItem(id, "b") {
		t.Fatal("the target refused to be cancelled")
	}

	items := r.Snapshot()[0].Items
	if items[1].State != ItemSkipped || items[1].Detail != "cancelled" {
		t.Errorf("the cancelled target = %+v, want it skipped with its reason", items[1])
	}
	if items[0].State != ItemRunning {
		t.Errorf("the target in flight reads %q, want it left alone", items[0].State)
	}
	if items[2].State != ItemQueued {
		t.Errorf("another queued target reads %q, want it still queued", items[2].State)
	}
	if r.Snapshot()[0].Cancelled() {
		t.Error("cancelling one target marked the whole run cancelled")
	}
}

// A running target is *asked* to stop and left running: it reports its own
// outcome when it gets there, and claiming it settled here would race the
// message that says how it actually ended.
func TestCancelItemCallsTheCancelAndLeavesTheTargetRunning(t *testing.T) {
	r := at(noon)
	stopped := false
	id := r.Start(kindRun(KindScan, ItemQueued))
	r.Apply(Transition{Kind: KindScan, Target: "a", State: ItemRunning, Cancel: func() { stopped = true }})

	if !r.CancelItem(id, "a") {
		t.Fatal("the target refused to be cancelled")
	}

	if !stopped {
		t.Error("the target's cancel function was never called")
	}
	if got := r.Snapshot()[0].Items[0].State; got != ItemRunning {
		t.Errorf("state = %q, want it left running until it says otherwise", got)
	}
	// Called once, and only once: a second press must not cancel a context
	// nobody is waiting on any more.
	stopped = false
	r.CancelItem(id, "a")
	if stopped {
		t.Error("the cancel function was called twice")
	}
}

// The last unsettled target of a run being skipped settles the run, like any
// other transition — nothing tells the registry a run is over, it derives it.
func TestCancellingTheLastQueuedTargetSettlesTheRun(t *testing.T) {
	r := at(noon)
	id := r.Start(kindRun(KindScan, ItemDone, ItemQueued))

	r.CancelItem(id, "b")

	snapshot := r.Snapshot()[0]
	if !snapshot.Finished() {
		t.Error("the run did not settle when its last target was skipped")
	}
	if snapshot.EndedAt.IsZero() {
		t.Error("the settled run was not stamped with an end time")
	}
	// Skipped is not failed: nobody stopped this run, one of its targets was
	// dropped. The state has to say so.
	if got := snapshot.State(); got != RunDone {
		t.Errorf("state = %q, want done — a skipped target is not a failure", got)
	}
}

func TestCancelItemRefusesWhatItCannotDo(t *testing.T) {
	r := at(noon)
	id := r.Start(kindRun(KindScan, ItemDone))

	if r.CancelItem(id, "a") {
		t.Error("a settled target was cancelled")
	}
	if r.CancelItem(id, "nothing-by-that-name") {
		t.Error("a target the run does not hold was cancelled")
	}
	if r.CancelItem(JobID(999), "a") {
		t.Error("a run that does not exist was cancelled")
	}
}
