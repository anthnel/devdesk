package jobs

import (
	"context"
	"time"
)

// MaxFinishedRuns is how many settled runs the registry keeps. Runs live for
// the session (D8) — they are not written anywhere and do not survive a
// restart — but a session that scans all day would otherwise grow a list
// nobody scrolls to the end of.
//
// Twenty is the count at which the oldest entry has stopped being useful: the
// question a jobs view answers about a finished run is "did that one work?",
// and it is asked about the last few.
const MaxFinishedRuns = 20

// Registry is the one place that knows what is running.
//
// It is **not safe for concurrent use, deliberately**. It is owned by the
// router and mutated from Update alone, which is where Rule 110 puts every
// write to application state; a mutex here would say a Cmd may touch it, and
// that is exactly the thing that must stay untrue. What a Cmd does is return a
// message, and Update turns that into a transition.
type Registry struct {
	runs   []*Run
	nextID JobID
	// clock is time.Now in production and a fixed hand in tests, so a run's
	// StartedAt is an assertion rather than a tolerance.
	clock func() time.Time
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{clock: time.Now}
}

// SetClock replaces the registry's idea of now. It exists for tests.
func (r *Registry) SetClock(clock func() time.Time) { r.clock = clock }

// Start admits a run, stamps it and returns its identifier.
//
// The run arrives with every item queued and is registered from Update, at the
// moment the commands are dispatched — never from inside one (D10). Every
// launch site knows its full list of targets at that point, which is what makes
// "8 waiting" sayable at all.
func (r *Registry) Start(run Run) JobID {
	r.nextID++
	run.ID = r.nextID
	if run.StartedAt.IsZero() {
		run.StartedAt = r.now()
	}
	if run.Finished() {
		run.EndedAt = run.StartedAt
	}
	r.runs = append(r.runs, &run)
	r.prune()
	return run.ID
}

// Advance moves one item of a run to a new state.
//
// A transition addressed to a run or a target the registry does not hold is
// reported rather than ignored: it means a launch site emitted a message for
// work it never registered, and the visible symptom would be a row that spins
// for the life of the view — the failure this package exists to remove.
func (r *Registry) Advance(id JobID, target string, state ItemState, detail string) bool {
	return r.advance(id, target, state, detail, nil)
}

// advance is Advance with the cancel function a starting transition carries.
//
// It arrives here and not through a call of its own because the two are one
// event: the context is created inside the Cmd, so the function reaches Update
// in the very message that says the item started. Two calls would leave a
// window where the item is running and cannot be stopped.
func (r *Registry) advance(id JobID, target string, state ItemState, detail string, cancel context.CancelFunc) bool {
	run := r.run(id)
	if run == nil {
		return false
	}
	for i := range run.Items {
		if run.Items[i].Target != target {
			continue
		}
		run.Items[i].State = state
		run.Items[i].Detail = detail
		switch {
		case state.Terminal():
			run.Items[i].cancel = nil
		case cancel != nil:
			run.Items[i].cancel = cancel
		}
		r.settle(run)
		return true
	}
	return false
}

// Transition is what a progress message says about one target.
//
// It exists so the router can apply a message without knowing the vocabulary of
// the view that sent it. A sync that was refused because the tree is dirty is a
// skip with a reason; only the workspaces package knows that, and it says so by
// answering this — see Reporter.
type Transition struct {
	Kind   Kind
	Target string
	State  ItemState
	Detail string

	// Cancel stops the work this transition reports as started, and is nil for
	// every other transition and for the kinds that cannot be cut (D7).
	//
	// It travels on the transition rather than through a call of its own
	// because the two are one event: the context is created inside the Cmd, so
	// the function reaches Update in the message that says the item started.
	Cancel context.CancelFunc

	// Discover says the target has no row yet and belongs to the open run of
	// this kind (D3). Only a kind whose targets arrive progressively sets it —
	// the clone, whose walk is what finds them — and for every other kind it
	// stays false because the full list was known at launch.
	Discover bool
}

// Reporter is implemented by a message that reports on registered work.
//
// The alternative was a switch in the router mapping every view's outcome
// vocabulary onto item states, which puts each view's semantics in a file that
// belongs to none of them, and grows by one branch per view.
type Reporter interface {
	Transition() Transition
}

// Sealer is implemented by the message that says a kind's open run has found
// everything it is going to (D3).
//
// It is a second interface rather than a field on Transition because sealing
// names no target: the message that carries it is the closed event channel, and
// there is nothing for it to be about.
type Sealer interface {
	Seal() Kind
}

// Apply routes a transition to the run it belongs to. It reports whether one
// was found, which is how the router knows there is something new to broadcast.
func (r *Registry) Apply(t Transition) bool {
	if t.Discover {
		return r.Discover(t.Kind, t.Target, t.State, t.Detail)
	}
	id, ok := r.FindItem(t.Kind, t.Target)
	if !ok {
		return false
	}
	return r.advance(id, t.Target, t.State, t.Detail, t.Cancel)
}

// Discover adds a target to the open run of a kind (D3).
//
// A target already there is advanced instead of duplicated: the walk reports a
// group it could not list as a target of its own, and that path may also have
// been found as a repository. One row, whichever arrives first.
func (r *Registry) Discover(kind Kind, target string, state ItemState, detail string) bool {
	run := r.openRun(kind)
	if run == nil {
		return false
	}
	for i := range run.Items {
		if run.Items[i].Target == target {
			run.Items[i].State = state
			run.Items[i].Detail = detail
			r.settle(run)
			return true
		}
	}
	run.Items = append(run.Items, Item{Target: target, State: state, Detail: detail})
	r.settle(run)
	return true
}

// Seal says the open run of a kind has discovered everything it is going to.
//
// Until it is called the run is unsettled whatever its items say, which is the
// point: a walk that has found three repositories and cloned all three is not
// done. Sealing is what lets the run finish, stamp EndedAt and let the spinner
// chain die.
func (r *Registry) Seal(kind Kind) bool {
	run := r.openRun(kind)
	if run == nil {
		return false
	}
	run.open = false
	r.settle(run)
	return true
}

// CancelOpen stops the open run of a kind.
//
// It exists because a progressive run is the one case a view can name without
// a target: there is exactly one open run per kind at a time — the screen that
// owns it is exclusive while it goes — so "the clone I am looking at" resolves
// without the view holding a JobID, which is the thing D1 keeps out of views.
func (r *Registry) CancelOpen(kind Kind) bool {
	run := r.openRun(kind)
	if run == nil {
		return false
	}
	return r.Cancel(run.ID)
}

// openRun is the run of a kind that is still discovering, if there is one.
//
// At most one can exist: an open run belongs to a screen that owns the display
// while it goes, so a second could only be started by leaving that screen —
// which closes the first. The newest wins if that invariant is ever broken, so
// the answer is the run a user is looking at rather than one they have left.
func (r *Registry) openRun(kind Kind) *Run {
	for i := len(r.runs) - 1; i >= 0; i-- {
		if r.runs[i].Kind == kind && r.runs[i].open {
			return r.runs[i]
		}
	}
	return nil
}

// FindItem names the run a transition belongs to.
//
// The launch sites do not carry a JobID in their messages, and deliberately:
// a view would have to hold one, which means holding registry state — the thing
// D1 keeps out of the views — and it would have to hold it before the registry
// had allocated it. The router looks the run up instead, from what the message
// already says: the kind is decided by which message arrived, and the target is
// in it.
//
// The oldest unsettled run wins. Two live runs of one kind cannot hold the same
// target — that is what the busy() guards prevent, and they are answered from
// this registry now, so the exclusion is enforced rather than hoped for.
func (r *Registry) FindItem(kind Kind, target string) (JobID, bool) {
	for _, run := range r.runs {
		if run.Kind != kind || run.Finished() {
			continue
		}
		for i := range run.Items {
			if run.Items[i].Target == target && !run.Items[i].State.Terminal() {
				return run.ID, true
			}
		}
	}
	return 0, false
}

// AttachRun stores the cancel function that stops the run's queue, as opposed
// to one item's work.
func (r *Registry) AttachRun(id JobID, cancel context.CancelFunc) {
	if run := r.run(id); run != nil {
		run.cancel = cancel
	}
}

// Cancel asks a run to stop.
//
// What that means differs by kind, and the difference is not cosmetic (D7):
// the queue always stops, so nothing further starts, but work already in flight
// is only interrupted where interrupting it leaves nothing behind. A `git
// clone` is never cut — a cancelled one leaves half a repository on disk — and
// a delete is not cancellable at all, which is a guard the view carries
// (Kind.Cancellable).
//
// Queued items are marked skipped straight away: they will not run, and leaving
// them queued would keep the run reading as running forever.
func (r *Registry) Cancel(id JobID) bool {
	run := r.run(id)
	if run == nil || run.Finished() {
		return false
	}

	run.cancelled = true
	// A cancelled walk finds nothing more, so the run is sealed here: leaving
	// it open would keep it unsettled for the life of the session, spinner
	// chain and all, with nothing left that could ever close it.
	run.open = false
	if run.cancel != nil {
		run.cancel()
		run.cancel = nil
	}

	for i := range run.Items {
		switch run.Items[i].State {
		case ItemQueued:
			run.Items[i].State = ItemSkipped
			run.Items[i].Detail = "cancelled"
		case ItemRunning:
			// Asked to stop, not declared stopped: the item reports its own
			// outcome when it gets there. Claiming it settled here would race
			// the message that says how it actually ended.
			if run.Items[i].cancel != nil {
				run.Items[i].cancel()
				run.Items[i].cancel = nil
			}
		}
	}

	r.settle(run)
	return true
}

// CancelItem stops one target of a run, leaving the rest going.
//
// It is the narrow half of D7: the run-level Cancel stops the queue whatever
// the kind, and this cuts a single piece of work — which is only offered where
// cutting leaves nothing behind. The caller decides that (Run.ItemStoppable);
// this refuses only what it cannot do.
//
// A queued target is skipped outright, because it will not run. A running one
// is *asked* to stop and left running: it reports its own outcome when it gets
// there, and claiming it settled here would race the message that says how it
// actually ended — the same reason Cancel gives.
func (r *Registry) CancelItem(id JobID, target string) bool {
	run := r.run(id)
	if run == nil || run.Finished() {
		return false
	}
	for i := range run.Items {
		if run.Items[i].Target != target || run.Items[i].State.Terminal() {
			continue
		}
		if run.Items[i].State == ItemQueued {
			run.Items[i].State = ItemSkipped
			run.Items[i].Detail = "cancelled"
		} else if run.Items[i].cancel != nil {
			run.Items[i].cancel()
			run.Items[i].cancel = nil
		}
		r.settle(run)
		return true
	}
	return false
}

// Snapshot returns a copy of every run, oldest first.
//
// It is what JobsChangedMsg carries, and copying is what makes the broadcast
// honest: a view holds data, not a window onto state someone else is writing.
// The cancel functions are cleared on the way out — a snapshot cannot stop a
// job, which has to go through Cancel on the registry the router owns.
func (r *Registry) Snapshot() []Run {
	out := make([]Run, 0, len(r.runs))
	for _, run := range r.runs {
		copied := *run
		copied.cancel = nil
		copied.Items = make([]Item, len(run.Items))
		copy(copied.Items, run.Items)
		for i := range copied.Items {
			copied.Items[i].cancel = nil
		}
		out = append(out, copied)
	}
	return out
}

// Running counts the runs that have not settled. It is what decides whether the
// router keeps its spinner chain alive, and what the degraded footer line
// counts (D9).
func (r *Registry) Running() int {
	n := 0
	for _, run := range r.runs {
		if !run.Finished() {
			n++
		}
	}
	return n
}

// Len returns how many runs the registry holds, settled ones included.
func (r *Registry) Len() int { return len(r.runs) }

// run finds a run by identifier.
func (r *Registry) run(id JobID) *Run {
	for _, run := range r.runs {
		if run.ID == id {
			return run
		}
	}
	return nil
}

// settle stamps EndedAt the first time a run is found finished, and drops the
// oldest settled runs once there are more than the cap allows.
func (r *Registry) settle(run *Run) {
	if run.Finished() && run.EndedAt.IsZero() {
		run.EndedAt = r.now()
	}
	r.prune()
}

// prune keeps at most MaxFinishedRuns settled runs, dropping the oldest first.
// A run still going is never dropped, whatever its age: the registry losing
// track of live work is the one failure that would leave a view spinning.
func (r *Registry) prune() {
	finished := 0
	for _, run := range r.runs {
		if run.Finished() {
			finished++
		}
	}
	excess := finished - MaxFinishedRuns
	if excess <= 0 {
		return
	}

	kept := make([]*Run, 0, len(r.runs)-excess)
	for _, run := range r.runs {
		if excess > 0 && run.Finished() {
			excess--
			continue
		}
		kept = append(kept, run)
	}
	r.runs = kept
}

func (r *Registry) now() time.Time {
	if r.clock == nil {
		return time.Now()
	}
	return r.clock()
}
