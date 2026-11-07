package jobs

import (
	"fmt"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/command"
)

// The failure memory is written by real transitions only: Start, Advance,
// Apply, Discover, Cancel. Nothing here touches a private field.

const scanFailed = "scan failed — check logs"

// startRun admits a run of kind in contextName over targets.
func startRun(r *Registry, kind Kind, contextName string, targets ...string) JobID {
	return r.Start(NewRun(kind, command.ViewWorkspaces, contextName, "label", targets...))
}

// failItem runs one target of a fresh run through running to failed.
func failItem(r *Registry, kind Kind, contextName, target, detail string) JobID {
	id := startRun(r, kind, contextName, target)
	r.Advance(id, target, ItemRunning, "")
	r.Advance(id, target, ItemFailed, detail)
	return id
}

func keyOf(kind Kind, target, contextName string) FailureKey {
	return FailureKey{Kind: kind, Target: target, Context: contextName}
}

// has reports whether the registry remembers a failure under key.
func has(r *Registry, key FailureKey) bool {
	for _, f := range r.Failures() {
		if f.FailureKey == key {
			return true
		}
	}
	return false
}

func TestANewRegistryRemembersNoFailure(t *testing.T) {
	if got := New().Failures(); len(got) != 0 {
		t.Errorf("a new registry holds %d failures, want 0", len(got))
	}
}

func TestEveryKindIsRemembered(t *testing.T) {
	for _, kind := range Kinds() {
		if !kind.Remembered() {
			t.Errorf("Kind(%q).Remembered() = false, want true for every kind", kind)
		}
	}
}

func TestAFailedItemIsRememberedWithItsReadableFields(t *testing.T) {
	r := at(noon)
	id := r.Start(NewRun(KindScan, command.ViewWorkspaces, "ctx", "~/w", "/w/app").
		WithDisplay("/w/app", "~/w/app"))
	r.Advance(id, "/w/app", ItemRunning, "")
	r.Advance(id, "/w/app", ItemFailed, scanFailed)

	got := r.Failures()
	if len(got) != 1 {
		t.Fatalf("Failures() holds %d entries, want 1: %+v", len(got), got)
	}
	f := got[0]
	if f.FailureKey != keyOf(KindScan, "/w/app", "ctx") {
		t.Errorf("key = %+v, want scan /w/app ctx", f.FailureKey)
	}
	if f.Detail != scanFailed {
		t.Errorf("Detail = %q, want %q", f.Detail, scanFailed)
	}
	if f.Display != "~/w/app" {
		t.Errorf("Display = %q, want %q", f.Display, "~/w/app")
	}
	if !f.At.Equal(noon) {
		t.Errorf("At = %v, want the registry clock %v", f.At, noon)
	}
}

func TestAFailureThroughApplyIsRemembered(t *testing.T) {
	r := at(noon)
	startRun(r, KindSync, "ctx", "/w/app")

	if !r.Apply(Transition{Kind: KindSync, Target: "/w/app", State: ItemFailed, Detail: "diverged"}) {
		t.Fatal("the transition was not applied")
	}
	if !has(r, keyOf(KindSync, "/w/app", "ctx")) {
		t.Errorf("Apply(failed) left nothing in the memory: %+v", r.Failures())
	}
}

func TestOneFailurePerKeyAndTheNewestReplacesTheOld(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "ctx", "/w/app", "first")
	later := noon.Add(time.Hour)
	r.SetClock(func() time.Time { return later })
	failItem(r, KindScan, "ctx", "/w/app", "second")

	got := r.Failures()
	if len(got) != 1 {
		t.Fatalf("Failures() holds %d entries for one key, want 1: %+v", len(got), got)
	}
	if got[0].Detail != "second" || !got[0].At.Equal(later) {
		t.Errorf("got Detail=%q At=%v, want the latest (second, %v)", got[0].Detail, got[0].At, later)
	}
}

func TestFailuresOfDifferentKindsTargetsOrContextsAreDistinct(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "a", "/w/app", "x")
	failItem(r, KindSync, "a", "/w/app", "x")
	failItem(r, KindScan, "b", "/w/app", "x")
	failItem(r, KindScan, "a", "/w/other", "x")

	if got := len(r.Failures()); got != 4 {
		t.Errorf("Failures() holds %d entries, want 4 distinct keys", got)
	}
}

func TestFailuresAreOldestFirst(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "ctx", "/w/a", "x")
	failItem(r, KindScan, "ctx", "/w/b", "x")
	failItem(r, KindScan, "ctx", "/w/c", "x")

	got := r.Failures()
	want := []string{"/w/a", "/w/b", "/w/c"}
	for i := range want {
		if got[i].Target != want[i] {
			t.Errorf("Failures()[%d].Target = %q, want %q", i, got[i].Target, want[i])
		}
	}
}

func TestAFailureKeyUsesTheNormalizedTarget(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "ctx", "/w/app/", "x")

	if !has(r, keyOf(KindScan, "/w/app", "ctx")) {
		t.Errorf("the key was not normalized with TargetKey: %+v", r.Failures())
	}
}

func TestAFailureOutlivesItsRunAndMaxFinishedRuns(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "ctx", "/w/app", scanFailed)

	for i := 0; i < MaxFinishedRuns+5; i++ {
		id := startRun(r, KindScan, "ctx", fmt.Sprintf("/w/other%d", i))
		r.Advance(id, fmt.Sprintf("/w/other%d", i), ItemDone, "")
	}

	if r.Len() > MaxFinishedRuns {
		t.Fatalf("the registry holds %d runs, the test did not prune the failed one", r.Len())
	}
	if !has(r, keyOf(KindScan, "/w/app", "ctx")) {
		t.Error("the failure went away with its pruned run")
	}
}

func TestStartingTheSameKeyClearsTheFailureBeforeAnythingRuns(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "ctx", "/w/app", scanFailed)

	startRun(r, KindScan, "ctx", "/w/app") // still queued

	if has(r, keyOf(KindScan, "/w/app", "ctx")) {
		t.Error("the failure survived a new attempt at Start")
	}
}

func TestStartingAnotherKindOrContextKeepsTheFailure(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "a", "/w/app", scanFailed)

	startRun(r, KindScan, "b", "/w/app")
	startRun(r, KindSync, "a", "/w/app")

	if !has(r, keyOf(KindScan, "/w/app", "a")) {
		t.Error("a run of another kind or context cleared the failure")
	}
}

func TestADoneItemClearsTheFailure(t *testing.T) {
	r := at(noon)
	// Both runs are admitted before the failure, so Start cannot be what
	// clears it: only the done transition can.
	first := startRun(r, KindScan, "ctx", "/w/app")
	second := startRun(r, KindScan, "ctx", "/w/app")
	r.Advance(first, "/w/app", ItemFailed, scanFailed)
	if !has(r, keyOf(KindScan, "/w/app", "ctx")) {
		t.Fatal("precondition: the failure was not posed")
	}

	r.Advance(second, "/w/app", ItemDone, "")

	if has(r, keyOf(KindScan, "/w/app", "ctx")) {
		t.Error("a successful attempt left the failure behind")
	}
}

func TestADoneItemKeepsTheFailuresOfOtherKeys(t *testing.T) {
	r := at(noon)
	failItem(r, KindSync, "ctx", "/w/app", "x")
	id := startRun(r, KindScan, "ctx", "/w/app")
	r.Advance(id, "/w/app", ItemDone, "")

	if !has(r, keyOf(KindSync, "/w/app", "ctx")) {
		t.Error("a scan success cleared the sync failure")
	}
}

func TestASkippedItemPosesNoFailure(t *testing.T) {
	r := at(noon)
	id := startRun(r, KindSync, "ctx", "/w/app")
	r.Advance(id, "/w/app", ItemSkipped, "dirty tree")

	if got := r.Failures(); len(got) != 0 {
		t.Errorf("a skip posed %+v", got)
	}
}

func TestACancelPosesNoFailureAndDoesNotRestoreAClearedOne(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "ctx", "/w/app", scanFailed)
	id := startRun(r, KindScan, "ctx", "/w/app", "/w/b")

	r.Cancel(id)

	if got := r.Failures(); len(got) != 0 {
		t.Errorf("a cancelled retry left %+v, want nothing", got)
	}
}

func TestACancelledItemPosesNoFailure(t *testing.T) {
	r := at(noon)
	id := startRun(r, KindScan, "ctx", "/w/app", "/w/b")
	r.CancelItem(id, "/w/app")

	if got := r.Failures(); len(got) != 0 {
		t.Errorf("CancelItem posed %+v", got)
	}
}

func TestADeleteThatSucceedsForgetsTheTargetForEveryKind(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "ctx", "/w/app", "x")
	failItem(r, KindSync, "ctx", "/w/app", "x")
	failItem(r, KindScan, "ctx", "/w/app/api", "x")
	failItem(r, KindScan, "ctx", "/w/application", "x")
	failItem(r, KindScan, "other", "/w/app", "x")

	id := startRun(r, KindDelete, "ctx", "/w/app")
	r.Advance(id, "/w/app", ItemRunning, "")
	r.Advance(id, "/w/app", ItemDone, "")

	for _, key := range []FailureKey{
		keyOf(KindScan, "/w/app", "ctx"),
		keyOf(KindSync, "/w/app", "ctx"),
		keyOf(KindScan, "/w/app/api", "ctx"),
	} {
		if has(r, key) {
			t.Errorf("%+v survived the delete", key)
		}
	}
	for _, key := range []FailureKey{
		keyOf(KindScan, "/w/application", "ctx"), // sibling, not a child
		keyOf(KindScan, "/w/app", "other"),       // another context
	} {
		if !has(r, key) {
			t.Errorf("%+v was dropped by a delete that does not concern it", key)
		}
	}
}

func TestAFailedDeleteForgetsNothing(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "ctx", "/w/app", "x")
	failItem(r, KindDelete, "ctx", "/w/app", "permission denied")

	if !has(r, keyOf(KindScan, "/w/app", "ctx")) {
		t.Error("a delete that failed cleared the scan failure")
	}
	if !has(r, keyOf(KindDelete, "/w/app", "ctx")) {
		t.Error("the delete failure itself was not remembered")
	}
}

func TestADeleteOfAReferenceDoesNotClearItsNamesake(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "ctx", "nginx", "x")
	failItem(r, KindScan, "ctx", "nginx/foo", "x")

	id := startRun(r, KindDelete, "ctx", "nginx")
	r.Advance(id, "nginx", ItemDone, "")

	if has(r, keyOf(KindScan, "nginx", "ctx")) {
		t.Error("the deleted reference kept its failure")
	}
	if !has(r, keyOf(KindScan, "nginx/foo", "ctx")) {
		t.Error("only filesystem paths have descendants; nginx/foo is not under nginx")
	}
}

func TestForgetIsScopedToOneContextAndReportsWhatItDropped(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "a", "nginx:1.27", "x")
	failItem(r, KindPull, "a", "nginx:1.27", "x")
	failItem(r, KindScan, "b", "nginx:1.27", "x")

	if !r.Forget("nginx:1.27", "a") {
		t.Fatal("Forget returned false although two failures were held")
	}
	if has(r, keyOf(KindScan, "nginx:1.27", "a")) || has(r, keyOf(KindPull, "nginx:1.27", "a")) {
		t.Error("Forget left a failure of the context behind")
	}
	if !has(r, keyOf(KindScan, "nginx:1.27", "b")) {
		t.Error("Forget reached another context")
	}
	if r.Forget("nginx:1.27", "a") {
		t.Error("a second Forget returned true with nothing left to drop")
	}
}

func TestForgetMatchesReferencesExactly(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "ctx", "nginx/foo:1", "x")

	if r.Forget("nginx", "ctx") {
		t.Error("Forget(nginx) reported a drop")
	}
	if !has(r, keyOf(KindScan, "nginx/foo:1", "ctx")) {
		t.Error("Forget(nginx) removed nginx/foo:1")
	}
}

func TestForgetDropsDescendantsOfAPathButNotASiblingPrefix(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "ctx", "/w/app/api", "x")
	failItem(r, KindScan, "ctx", "/w/application", "x")

	if !r.Forget("/w/app/", "ctx") {
		t.Fatal("Forget returned false")
	}
	if has(r, keyOf(KindScan, "/w/app/api", "ctx")) {
		t.Error("a path below the forgotten one survived")
	}
	if !has(r, keyOf(KindScan, "/w/application", "ctx")) {
		t.Error("/w/application was treated as a child of /w/app")
	}
}

func TestTheCloneFailureClearsWhenTheTargetIsDiscoveredAgain(t *testing.T) {
	r := at(noon)
	r.Start(NewOpenRun(KindClone, command.ViewGitExplorer, "ctx", "/ws"))
	r.Discover(KindClone, "/ws/t", ItemFailed, "auth")
	r.Seal(KindClone)
	if !has(r, keyOf(KindClone, "/ws/t", "ctx")) {
		t.Fatal("precondition: the clone failure was not posed through Discover")
	}

	r.Start(NewOpenRun(KindClone, command.ViewGitExplorer, "ctx", "/ws"))
	r.Discover(KindClone, "/ws/t", ItemQueued, "")
	if has(r, keyOf(KindClone, "/ws/t", "ctx")) {
		t.Error("rediscovering the target did not admit a new attempt")
	}

	r.Discover(KindClone, "/ws/g", ItemFailed, "unreachable")
	if !has(r, keyOf(KindClone, "/ws/g", "ctx")) {
		t.Error("a target discovered as failed was not remembered")
	}
}

func TestADiscoveredTargetThatFailsLaterIsRemembered(t *testing.T) {
	r := at(noon)
	r.Start(NewOpenRun(KindClone, command.ViewGitExplorer, "ctx", "/ws"))
	r.Discover(KindClone, "/ws/t", ItemQueued, "")
	r.Discover(KindClone, "/ws/t", ItemFailed, "disk full") // advances the same row

	got := r.Failures()
	if len(got) != 1 || got[0].Detail != "disk full" {
		t.Errorf("Failures() = %+v, want one with Detail %q", got, "disk full")
	}
}

func TestAFailureKeepsTheContextOfItsRunAfterAnotherContextStarted(t *testing.T) {
	r := at(noon)
	slow := startRun(r, KindScan, "a", "/w/app")
	startRun(r, KindScan, "b", "/w/other") // the user switched context meanwhile

	r.Advance(slow, "/w/app", ItemFailed, scanFailed)

	if !has(r, keyOf(KindScan, "/w/app", "a")) {
		t.Errorf("the failure was not filed under the run's context: %+v", r.Failures())
	}
}

func TestTheFailureMemoryIsCappedAtFiveHundredOldestFirst(t *testing.T) {
	r := at(noon)
	for i := 1; i <= MaxFailures+1; i++ {
		failItem(r, KindScan, "ctx", fmt.Sprintf("/w/t%d", i), "x")
	}

	got := r.Failures()
	if len(got) != MaxFailures {
		t.Fatalf("Failures() holds %d entries, want %d", len(got), MaxFailures)
	}
	if got[0].Target != "/w/t2" {
		t.Errorf("oldest = %q, want /w/t2 (t1 should have been evicted)", got[0].Target)
	}
	if has(r, keyOf(KindScan, "/w/t1", "ctx")) {
		t.Error("the oldest failure was kept")
	}
	if got[len(got)-1].Target != fmt.Sprintf("/w/t%d", MaxFailures+1) {
		t.Errorf("newest = %q", got[len(got)-1].Target)
	}
	if r.Len() > MaxFinishedRuns {
		t.Errorf("the registry holds %d runs: the cap must not depend on them", r.Len())
	}
}

func TestARefreshedFailureIsTheLastToBeEvicted(t *testing.T) {
	r := at(noon)
	for i := 1; i <= MaxFailures; i++ {
		failItem(r, KindScan, "ctx", fmt.Sprintf("/w/t%d", i), "x")
	}
	failItem(r, KindScan, "ctx", "/w/t1", "again") // refresh #1
	failItem(r, KindScan, "ctx", fmt.Sprintf("/w/t%d", MaxFailures+1), "x")

	if !has(r, keyOf(KindScan, "/w/t1", "ctx")) {
		t.Error("the refreshed failure was evicted")
	}
	if has(r, keyOf(KindScan, "/w/t2", "ctx")) {
		t.Error("t2 should have been the one evicted")
	}
	if got := len(r.Failures()); got != MaxFailures {
		t.Errorf("Failures() holds %d entries, want %d", got, MaxFailures)
	}
}

func TestFailuresReturnsACopy(t *testing.T) {
	r := at(noon)
	failItem(r, KindScan, "ctx", "/w/a", "kept")
	failItem(r, KindScan, "ctx", "/w/b", "kept")

	got := r.Failures()
	got[0].Detail = "mutated"
	got[0].Target = "/mutated"
	_ = append(got[:1], Failure{})
	got = got[:0]
	_ = got

	again := r.Failures()
	if len(again) != 2 || again[0].Detail != "kept" || again[0].Target != "/w/a" || again[1].Target != "/w/b" {
		t.Errorf("mutating the result changed the registry: %+v", again)
	}
}
