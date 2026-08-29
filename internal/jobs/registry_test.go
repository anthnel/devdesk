package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/command"
)

// at returns a registry whose clock stands still, so StartedAt and EndedAt are
// assertions rather than tolerances.
func at(moment time.Time) *Registry {
	r := New()
	r.SetClock(func() time.Time { return moment })
	return r
}

var noon = time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

func TestStartStampsARunAndHandsBackItsID(t *testing.T) {
	r := at(noon)

	first := r.Start(run("a", "b"))
	second := r.Start(run("c"))

	if first == second {
		t.Errorf("two runs share the identifier %d", first)
	}
	snapshot := r.Snapshot()
	if got, want := len(snapshot), 2; got != want {
		t.Fatalf("the registry holds %d runs, want %d", got, want)
	}
	if !snapshot[0].StartedAt.Equal(noon) {
		t.Errorf("StartedAt = %v, want %v", snapshot[0].StartedAt, noon)
	}
	if !snapshot[0].EndedAt.IsZero() {
		t.Errorf("EndedAt = %v on a run that has not settled", snapshot[0].EndedAt)
	}
	if got, want := r.Running(), 2; got != want {
		t.Errorf("Running = %d, want %d", got, want)
	}
}

// The transitions are the whole life of an item. What matters here is that the
// run settles by itself when the last one lands — nothing tells the registry a
// run is over, it derives it.
func TestARunSettlesWhenItsLastItemDoes(t *testing.T) {
	r := at(noon)
	id := r.Start(run("a", "b"))

	if !r.Advance(id, "a", ItemRunning, "") {
		t.Fatal("the transition was not applied")
	}
	if got, want := r.Running(), 1; got != want {
		t.Errorf("Running = %d, want %d while an item is in flight", got, want)
	}

	r.Advance(id, "a", ItemDone, "")
	if got, want := r.Running(), 1; got != want {
		t.Errorf("Running = %d, want %d — one item is still queued", got, want)
	}

	r.Advance(id, "b", ItemFailed, "trivy: exit status 1")
	if got, want := r.Running(), 0; got != want {
		t.Errorf("Running = %d, want %d once every item settled", got, want)
	}

	settled := r.Snapshot()[0]
	if got := settled.State(); got != RunFailed {
		t.Errorf("state = %q, want %q", got, RunFailed)
	}
	if !settled.EndedAt.Equal(noon) {
		t.Errorf("EndedAt = %v, want it stamped when the run settled", settled.EndedAt)
	}
	if got, want := settled.Items[1].Detail, "trivy: exit status 1"; got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}

// A transition nobody registered is reported, not swallowed. It means a launch
// site emitted a message for work it never declared — and the visible symptom
// would be a row spinning for the life of the view, which is the failure this
// package exists to remove.
func TestATransitionForUnknownWorkIsRefused(t *testing.T) {
	r := at(noon)
	id := r.Start(run("a"))

	if r.Advance(id, "b", ItemDone, "") {
		t.Error("a transition for a target the run does not hold was applied")
	}
	if r.Advance(id+99, "a", ItemDone, "") {
		t.Error("a transition for a run that does not exist was applied")
	}
}

// EndedAt is stamped once. A run that keeps receiving transitions after it
// settled — a late message from a Cmd that was already in flight — must not
// have its end time moved forward.
func TestEndedAtIsStampedOnce(t *testing.T) {
	r := at(noon)
	id := r.Start(run("a"))
	r.Advance(id, "a", ItemDone, "")

	r.SetClock(func() time.Time { return noon.Add(time.Hour) })
	r.Advance(id, "a", ItemDone, "late")

	if got := r.Snapshot()[0].EndedAt; !got.Equal(noon) {
		t.Errorf("EndedAt = %v, want it left at %v", got, noon)
	}
}

// D8: the cap is on settled runs, and the oldest go first.
func TestTheRegistryKeepsTheLastTwentySettledRuns(t *testing.T) {
	r := at(noon)

	var ids []JobID
	for i := 0; i < MaxFinishedRuns+5; i++ {
		id := r.Start(run("a"))
		r.Advance(id, "a", ItemDone, "")
		ids = append(ids, id)
	}

	if got, want := r.Len(), MaxFinishedRuns; got != want {
		t.Fatalf("the registry holds %d runs, want the cap of %d", got, want)
	}

	held := make(map[JobID]bool, r.Len())
	for _, run := range r.Snapshot() {
		held[run.ID] = true
	}
	for _, dropped := range ids[:5] {
		if held[dropped] {
			t.Errorf("run %d should have been dropped as one of the oldest", dropped)
		}
	}
	for _, kept := range ids[5:] {
		if !held[kept] {
			t.Errorf("run %d was dropped although it is one of the last %d", kept, MaxFinishedRuns)
		}
	}
}

// A run still going is never dropped, whatever its age. The registry losing
// track of live work is the one failure that leaves a view spinning with
// nothing left to tell it otherwise.
func TestThePruneNeverDropsARunThatIsStillGoing(t *testing.T) {
	r := at(noon)
	live := r.Start(run("a"))
	r.Advance(live, "a", ItemRunning, "")

	for i := 0; i < MaxFinishedRuns+10; i++ {
		id := r.Start(run("b"))
		r.Advance(id, "b", ItemDone, "")
	}

	found := false
	for _, run := range r.Snapshot() {
		if run.ID == live {
			found = true
		}
	}
	if !found {
		t.Error("the run that is still going was pruned")
	}
	if got, want := r.Running(), 1; got != want {
		t.Errorf("Running = %d, want %d", got, want)
	}
}

// D1: the snapshot is data, not a window. Writing into what it hands back must
// not reach the registry, or every view would be a second writer.
func TestASnapshotIsACopy(t *testing.T) {
	r := at(noon)
	id := r.Start(run("a"))

	snapshot := r.Snapshot()
	if snapshot[0].ID != id {
		t.Fatalf("snapshot holds run %d, want %d", snapshot[0].ID, id)
	}
	snapshot[0].Items[0].State = ItemFailed
	snapshot[0].Label = "rewritten"

	held := r.Snapshot()[0]
	if held.Items[0].State != ItemQueued {
		t.Errorf("item state = %q, want the registry untouched by a snapshot's writer", held.Items[0].State)
	}
	if held.Label == "rewritten" {
		t.Error("the label was rewritten through a snapshot")
	}
	if r.Running() != 1 {
		t.Error("writing into a snapshot changed what the registry reports as running")
	}
}

// A snapshot cannot stop a job. The cancel functions are cleared on the way
// out, so cancelling has to go through the registry the router owns — the same
// asymmetry as the snapshot itself.
func TestASnapshotCannotCancelAnything(t *testing.T) {
	r := at(noon)
	id := r.Start(run("a"))
	r.AttachRun(id, func() {})
	r.Apply(Transition{Kind: KindScan, Target: "a", State: ItemRunning, Cancel: func() {}})

	snapshot := r.Snapshot()[0]

	if snapshot.cancel != nil {
		t.Error("the snapshot carries the run's cancel function")
	}
	if snapshot.Items[0].cancel != nil {
		t.Error("the snapshot carries an item's cancel function")
	}
}

// D7: cancelling stops the queue in every case. What differs by kind is whether
// work already in flight is cut, and that is Kind.Cancellable's business —
// the registry calls whatever cancel function it was handed.
func TestCancelStopsTheQueueAndCallsWhatItWasGiven(t *testing.T) {
	r := at(noon)
	id := r.Start(NewRun(KindScan, command.ViewWorkspaces, "default", "~/work", "a", "b", "c"))
	r.Advance(id, "a", ItemDone, "")

	runCancelled, itemCancelled := false, false
	r.AttachRun(id, func() { runCancelled = true })
	r.Apply(Transition{Kind: KindScan, Target: "b", State: ItemRunning, Cancel: func() { itemCancelled = true }})

	if !r.Cancel(id) {
		t.Fatal("the run refused to be cancelled")
	}

	if !runCancelled {
		t.Error("the run's cancel function was not called")
	}
	if !itemCancelled {
		t.Error("the in-flight item's cancel function was not called")
	}

	held := r.Snapshot()[0]
	if got := held.Items[2].State; got != ItemSkipped {
		t.Errorf("the queued item is %q, want it skipped — it will never run", got)
	}
	if got, want := held.Items[2].Detail, "cancelled"; got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
	if got := held.Items[1].State; got != ItemRunning {
		t.Errorf("the in-flight item is %q, want it left running until it reports", got)
	}
	if got := held.State(); got != RunRunning {
		t.Errorf("state = %q, want the run still going while an item is in flight", got)
	}

	// And when it does report, the run reads cancelled rather than failed.
	r.Advance(id, "b", ItemFailed, "context canceled")
	if got := r.Snapshot()[0].State(); got != RunCancelled {
		t.Errorf("state = %q, want %q", got, RunCancelled)
	}
	if got := r.Running(); got != 0 {
		t.Errorf("Running = %d, want 0", got)
	}
}

// Cancelling twice, or cancelling something already settled, is not an error
// worth a message — but it must not run the cancel functions again.
func TestCancellingASettledRunDoesNothing(t *testing.T) {
	r := at(noon)
	id := r.Start(run("a"))
	r.Advance(id, "a", ItemDone, "")

	calls := 0
	r.AttachRun(id, func() { calls++ })

	if r.Cancel(id) {
		t.Error("a settled run reported that it was cancelled")
	}
	if r.Cancel(id + 99) {
		t.Error("a run that does not exist reported that it was cancelled")
	}
	if calls != 0 {
		t.Errorf("the cancel function ran %d times on a settled run", calls)
	}
	if got := r.Snapshot()[0].State(); got != RunDone {
		t.Errorf("state = %q, want it left %q", got, RunDone)
	}
}

// A cancel function is dropped once its item settles: keeping it would let a
// later Cancel call cancel a context nobody is waiting on, and hold whatever it
// closes over for the session.
func TestASettledItemDropsItsCancelFunction(t *testing.T) {
	r := at(noon)
	id := r.Start(run("a"))

	_, cancel := context.WithCancel(context.Background())
	r.Apply(Transition{Kind: KindScan, Target: "a", State: ItemRunning, Cancel: cancel})
	r.Advance(id, "a", ItemDone, "")

	if r.run(id).Items[0].cancel != nil {
		t.Error("a settled item still holds its cancel function")
	}
	cancel()
}

// An empty run — a scan asked for on a directory holding no repository — is
// admitted and settles immediately, rather than sitting in the registry
// forever keeping the spinner chain alive.
func TestAnEmptyRunIsAdmittedAndSettled(t *testing.T) {
	r := at(noon)
	id := r.Start(run())

	if got := r.Running(); got != 0 {
		t.Errorf("Running = %d, want 0 for a run with nothing to do", got)
	}
	held := r.Snapshot()[0]
	if !held.EndedAt.Equal(noon) {
		t.Errorf("EndedAt = %v, want it stamped at once", held.EndedAt)
	}
	if r.Cancel(id) {
		t.Error("an already-settled empty run reported that it was cancelled")
	}
}
