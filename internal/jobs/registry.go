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
		if state.Terminal() {
			run.Items[i].cancel = nil
		}
		r.settle(run)
		return true
	}
	return false
}

// Attach stores the cancel function of an item that has just started.
//
// It is separate from Advance because the two do not arrive together: the
// context is created inside the Cmd, so the function reaches Update in the
// message that says the item started, and only the kinds that can be cancelled
// send one (D7).
func (r *Registry) Attach(id JobID, target string, cancel context.CancelFunc) {
	run := r.run(id)
	if run == nil {
		return
	}
	for i := range run.Items {
		if run.Items[i].Target == target {
			run.Items[i].cancel = cancel
			return
		}
	}
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
