package jobs

import (
	"testing"

	"github.com/anthnel/devdesk/internal/command"
)

func run(targets ...string) Run {
	return NewRun(KindScan, command.ViewWorkspaces, "default", "~/work", targets...)
}

// state applies a list of transitions to a fresh run and returns what it
// derives, which is the shape almost every case below wants.
func state(t *testing.T, targets []string, moves map[string]ItemState) RunState {
	t.Helper()
	r := run(targets...)
	for target, s := range moves {
		found := false
		for i := range r.Items {
			if r.Items[i].Target == target {
				r.Items[i].State = s
				found = true
			}
		}
		if !found {
			t.Fatalf("no item named %q to move", target)
		}
	}
	return r.State()
}

// A run's state is derived, so the whole of its behaviour is this table. The
// cases that matter are the mixed ones: a run holding both settled and queued
// items is running even though nothing is running at that instant, because the
// queue has not emptied.
func TestARunStateIsDerivedFromItsItems(t *testing.T) {
	targets := []string{"a", "b", "c"}

	cases := []struct {
		name  string
		moves map[string]ItemState
		want  RunState
	}{
		{"nothing has started", nil, RunQueued},
		{"one is running", map[string]ItemState{"a": ItemRunning}, RunRunning},
		{
			"one is done and the rest wait",
			map[string]ItemState{"a": ItemDone},
			RunRunning,
		},
		{
			"one failed and the rest wait",
			map[string]ItemState{"a": ItemFailed},
			RunRunning,
		},
		{
			"all done",
			map[string]ItemState{"a": ItemDone, "b": ItemDone, "c": ItemDone},
			RunDone,
		},
		{
			"one failure settles the run as failed",
			map[string]ItemState{"a": ItemDone, "b": ItemFailed, "c": ItemSkipped},
			RunFailed,
		},
		{
			"skipped is a settled state, not a pending one",
			map[string]ItemState{"a": ItemSkipped, "b": ItemSkipped, "c": ItemSkipped},
			RunDone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := state(t, targets, tc.moves); got != tc.want {
				t.Errorf("state = %q, want %q", got, tc.want)
			}
		})
	}
}

// An empty run is finished. Reporting it as running would keep the router's
// spinner chain alive with nothing to animate, and there is no transition that
// could ever settle it.
func TestAnEmptyRunIsFinished(t *testing.T) {
	r := run()
	if got := r.State(); got != RunDone {
		t.Errorf("state = %q, want the run settled", got)
	}
	if !r.Finished() {
		t.Error("an empty run reports itself as still going")
	}
}

// Cancelled wins over failed: the items of a cancelled scan fail because their
// context was cut, and reporting that as a failure would blame the tool for
// doing what it was told.
func TestACancelledRunDoesNotReadAsFailed(t *testing.T) {
	r := run("a", "b")
	r.cancelled = true
	r.Items[0].State = ItemFailed
	r.Items[1].State = ItemSkipped

	if got := r.State(); got != RunCancelled {
		t.Errorf("state = %q, want %q", got, RunCancelled)
	}
	if !r.Cancelled() {
		t.Error("the run does not report that it was cancelled")
	}
}

// A cancelled run is still running while its in-flight items settle: asking is
// not the same as having stopped.
func TestACancelledRunStaysRunningUntilItsItemsSettle(t *testing.T) {
	r := run("a", "b")
	r.cancelled = true
	r.Items[0].State = ItemRunning
	r.Items[1].State = ItemSkipped

	if got := r.State(); got != RunRunning {
		t.Errorf("state = %q, want it still running while an item is in flight", got)
	}
}

// Total and Done are what a footer prints. Done counts every settled item,
// however it settled: a skipped repository is finished with, and a progress
// line that ignored it would never reach its total.
func TestDoneCountsEverySettledItem(t *testing.T) {
	r := run("a", "b", "c", "d")
	r.Items[0].State = ItemDone
	r.Items[1].State = ItemFailed
	r.Items[2].State = ItemSkipped
	r.Items[3].State = ItemRunning

	if got, want := r.Total(), 4; got != want {
		t.Errorf("Total = %d, want %d", got, want)
	}
	if got, want := r.Done(), 3; got != want {
		t.Errorf("Done = %d, want %d — a skipped item is finished with", got, want)
	}
	if got, want := r.Counts()[ItemRunning], 1; got != want {
		t.Errorf("Counts[running] = %d, want %d", got, want)
	}
}

// An item prints its display name when it has one, and its target otherwise.
// It is what applies a registry alias without the target losing the identity
// transitions are addressed to.
func TestAnItemPrintsItsDisplayNameWhenItHasOne(t *testing.T) {
	r := run("nexus.example.com/team/api:1.2").
		WithDisplay("nexus.example.com/team/api:1.2", "nexus/team/api:1.2")

	if got, want := r.Items[0].Name(), "nexus/team/api:1.2"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	if got, want := r.Items[0].Target, "nexus.example.com/team/api:1.2"; got != want {
		t.Errorf("Target = %q, want %q — untouched by the display name", got, want)
	}

	plain := run("/repos/devdesk")
	if got, want := plain.Items[0].Name(), "/repos/devdesk"; got != want {
		t.Errorf("Name = %q, want the target when no display name is set", got)
	}
}

// WithDisplay returns a new run rather than writing into the one it was given:
// a run under construction is a value, and a helper that mutated its receiver's
// backing array would reach runs already registered.
func TestWithDisplayLeavesTheOriginalAlone(t *testing.T) {
	original := run("a")
	renamed := original.WithDisplay("a", "A")

	if original.Items[0].Display != "" {
		t.Errorf("the original run was modified: display = %q", original.Items[0].Display)
	}
	if renamed.Items[0].Display != "A" {
		t.Errorf("the copy did not take the display name: %q", renamed.Items[0].Display)
	}
}

// D8: runs are filtered by the context they were stamped with, not purged when
// the context changes — purging would contradict keeping them for the session.
func TestFilterContextKeepsOnlyTheCurrentContext(t *testing.T) {
	runs := []Run{
		NewRun(KindScan, command.ViewWorkspaces, "default", "one", "a"),
		NewRun(KindSync, command.ViewWorkspaces, "prod", "two", "b"),
		NewRun(KindScan, command.ViewSecurity, "default", "three", "c"),
	}

	kept := FilterContext(runs, "default")

	if got, want := len(kept), 2; got != want {
		t.Fatalf("kept %d runs, want %d", got, want)
	}
	for _, r := range kept {
		if r.Context != "default" {
			t.Errorf("kept a run stamped %q", r.Context)
		}
	}
	if got := len(FilterContext(runs, "staging")); got != 0 {
		t.Errorf("a context with no runs kept %d", got)
	}
}

func TestUnfinishedKeepsWhatIsStillGoing(t *testing.T) {
	going := run("a")
	going.Items[0].State = ItemRunning
	settled := run("b")
	settled.Items[0].State = ItemDone

	kept := Unfinished([]Run{going, settled})

	if got, want := len(kept), 1; got != want {
		t.Fatalf("kept %d runs, want %d", got, want)
	}
	if kept[0].Items[0].Target != "a" {
		t.Errorf("kept the settled run instead of the running one")
	}
}

// D7 is a table, and a kind added without an entry in it would silently answer
// "not cancellable" — the safe answer, which is exactly why nothing would ever
// notice. Walking Kinds() makes the omission a compile-time-shaped question
// instead.
func TestEveryKindAnswersWhetherItCanBeCancelled(t *testing.T) {
	want := map[Kind]bool{
		KindScan:   true,
		KindPull:   true,
		KindSync:   false,
		KindClone:  false,
		KindCreate: false,
		KindDelete: false,
	}

	kinds := Kinds()
	if got, expected := len(kinds), len(want); got != expected {
		t.Fatalf("Kinds() returns %d kinds, want %d — the D7 table is out of step", got, expected)
	}
	for _, kind := range kinds {
		expected, declared := want[kind]
		if !declared {
			t.Errorf("kind %q is not in the D7 cancellation table", kind)
			continue
		}
		if got := kind.Cancellable(); got != expected {
			t.Errorf("%q.Cancellable() = %v, want %v", kind, got, expected)
		}
	}
}
