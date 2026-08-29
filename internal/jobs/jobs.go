// Package jobs holds what the application knows about work that outlives the
// moment it was started: a scan, a sync, a clone, a pull, a delete.
//
// Before it, four bookkeepings ran in parallel and none could see the others —
// workspaces kept three maps of paths, oci_resources a map of image names, the
// security inventory a flag per row, and the clone screen five states per
// repository. A scan started from `:sec` was therefore invisible to the `busy()`
// guard in `ws`, and the two wrote the same cache entry. Each of them also
// stamped its own spinner frame, which is four chains to keep alive and four
// ways for one to die unnoticed.
//
// The registry is owned by the router and read by the views through a snapshot
// carried in a message — never through a shared pointer. See D1 in
// .claude/plans/jobs-registry.plan.md for why, and internal/app/jobs.go for the
// broadcast.
package jobs

import (
	"context"
	"time"

	"github.com/anthnel/devdesk/internal/command"
)

// Kind is what a run does. It decides the icon, the footer verb and — at
// poste 8 — whether the run can be cancelled at all.
type Kind string

const (
	KindScan   Kind = "scan"
	KindSync   Kind = "sync"
	KindClone  Kind = "clone"
	KindPull   Kind = "pull"
	KindDelete Kind = "delete"
)

// Kinds returns every declared kind, in the order a view should offer them.
// It exists to be walked by a test: a kind without an icon or without a verb
// is a hole that only shows up on the screen that needed it.
func Kinds() []Kind {
	return []Kind{KindScan, KindSync, KindClone, KindPull, KindDelete}
}

// Cancellable reports whether stopping a run of this kind leaves the machine in
// a state the user asked for.
//
// The queue always stops — nothing further starts, whatever the kind — so this
// answers the narrower question of whether work already in flight can be cut.
// It is the table from D7, kept next to the kinds so a new kind cannot be added
// without answering it:
//
//	scan    yes  Trivy and Gitleaks read; cutting leaves nothing behind
//	pull    yes  a docker pull resumes by layer
//	sync    no   the queue stops, a fast-forward in flight is waited out
//	clone   no   a cut git clone leaves half a repository on disk
//	delete  no   half deleted is worse than deleted
//
// A run whose kind answers no still accepts Cancel: the queue stopping is worth
// having on its own. What the answer gates is whether a view offers the key on
// a single item (Rule 130 — greyed, with a named reason, never silent).
func (k Kind) Cancellable() bool {
	return k == KindScan || k == KindPull
}

// Verb is what a kind is called while it runs — present participle, English US
// (Rule 129). It lives here rather than in a view because two now say it: the
// footer of the view that launched the run, and the `:jobs` list.
//
// A kind that reaches the default is a kind added without answering this, which
// is what walking Kinds() in a test catches.
func (k Kind) Verb() string {
	switch k {
	case KindScan:
		return "Scanning"
	case KindSync:
		return "Syncing"
	case KindClone:
		return "Cloning"
	case KindPull:
		return "Pulling"
	case KindDelete:
		return "Deleting"
	}
	return "Working"
}

// ItemState is where one target stands.
//
// queued and running are distinguished on purpose (D6): the semaphore already
// knows the difference, the clone screen already shows it, and "4 running, 8
// waiting" is the one sentence a jobs view exists to say. Collapsing them would
// make the clone screen the only honest view in the application.
type ItemState string

const (
	ItemQueued  ItemState = "queued"
	ItemRunning ItemState = "running"
	ItemDone    ItemState = "done"
	ItemSkipped ItemState = "skipped"
	ItemFailed  ItemState = "failed"
)

// Terminal reports whether the state is one the item will not leave.
func (s ItemState) Terminal() bool {
	return s == ItemDone || s == ItemSkipped || s == ItemFailed
}

// RunState is derived from the items, never stored. A field would be a second
// source of truth for a question the items already answer, and the two would
// drift the first time a transition was missed.
type RunState string

const (
	RunQueued    RunState = "queued"
	RunRunning   RunState = "running"
	RunDone      RunState = "done"
	RunFailed    RunState = "failed"
	RunCancelled RunState = "cancelled"
)

// JobID identifies a run for the life of the session.
type JobID int

// Item is one target inside a run: a repository path, an image reference, a
// group path.
type Item struct {
	// Target is the identity transitions are addressed to, and it is unique
	// within a run.
	Target string
	// Display is what a view shows — a registry alias applied, a path
	// shortened. Empty means "show the target".
	Display string
	State   ItemState
	// Detail carries what the state alone cannot say: "already there", the
	// reason a scan failed.
	Detail string

	// cancel is unexported and deliberately does not travel: Snapshot clears it
	// on the copies it hands out, so a view holding a snapshot cannot stop a job
	// behind the router's back. Cancelling goes through Registry.Cancel, which
	// the owner runs — the same asymmetry as the snapshot itself (D1).
	cancel context.CancelFunc
}

// Name returns what a view should print for the item.
func (i Item) Name() string {
	if i.Display != "" {
		return i.Display
	}
	return i.Target
}

// Run is one batch of work, started in one go from one view.
type Run struct {
	ID   JobID
	Kind Kind
	// Origin is the view that started the run: where `:jobs` sends the user
	// back, and what decides whether a footer keeps its detailed line (D9).
	Origin command.ViewType
	// Context is stamped at launch and never re-read. It is what lets `:jobs`
	// filter on the current context instead of purging on a switch — which
	// would contradict keeping runs for the session (D8) — and it is what stops
	// a batch outliving a context switch from writing its results into the
	// wrong cache.
	Context string
	// Label names the run for a human: "~/work/perso", "nexus.example.com/team".
	Label     string
	Items     []Item
	StartedAt time.Time
	EndedAt   time.Time

	// cancelled records that someone asked for this run to stop. Without it a
	// cancelled run reads as done once its items settle, and a jobs view that
	// cannot tell "finished" from "you stopped it" is missing the one fact the
	// user is looking for.
	cancelled bool

	// open says the target list is still being discovered (D3). An open run is
	// never finished, however its items stand: a clone whose walk has found
	// three repositories and cloned all three is not done — the walk is still
	// going, and settling it there would stop the spinner chain and print a
	// summary the next repository contradicts.
	//
	// Every other kind knows its full list when it is admitted, which is what
	// makes "8 waiting" sayable at all (D10). The clone is the exception
	// because its walk *is* the work that finds them.
	open bool

	// cancel stops the run's queue. Cleared by Snapshot, like Item.cancel.
	cancel context.CancelFunc
}

// NewRun builds a run with every target queued. StartedAt is left to the
// registry, which stamps it when the run is admitted.
func NewRun(kind Kind, origin command.ViewType, contextName, label string, targets ...string) Run {
	items := make([]Item, len(targets))
	for i, target := range targets {
		items[i] = Item{Target: target, State: ItemQueued}
	}
	return Run{
		Kind:    kind,
		Origin:  origin,
		Context: contextName,
		Label:   label,
		Items:   items,
	}
}

// NewOpenRun builds a run whose targets are not known yet.
//
// It is the clone's shape and only the clone's: the walk that discovers
// repositories is itself the slow part, so a run that waited for the full list
// would show nothing for the minutes that matter. Targets arrive through
// Registry.Discover, and Registry.Seal says there will be no more.
func NewOpenRun(kind Kind, origin command.ViewType, contextName, label string) Run {
	return Run{
		Kind:    kind,
		Origin:  origin,
		Context: contextName,
		Label:   label,
		open:    true,
	}
}

// Open reports whether the run is still discovering its targets.
func (r Run) Open() bool { return r.open }

// WithDisplay names how one target should be printed. It is separate from
// NewRun because only some kinds have one — an image reference goes through a
// registry alias, a repository path does not.
func (r Run) WithDisplay(target, display string) Run {
	items := make([]Item, len(r.Items))
	copy(items, r.Items)
	for i := range items {
		if items[i].Target == target {
			items[i].Display = display
		}
	}
	r.Items = items
	return r
}

// State derives where the run stands from its items.
//
// The order of the three settled answers is the order in which they matter: a
// run that was cancelled reads cancelled even though its items failed for that
// very reason, and one failure is enough to make the whole run a failure.
func (r Run) State() RunState {
	var running, terminal, failed int
	for _, item := range r.Items {
		switch {
		case item.State == ItemRunning:
			running++
		case item.State.Terminal():
			terminal++
			if item.State == ItemFailed {
				failed++
			}
		}
	}

	// An open run is unsettled whatever its items say: more of them are coming.
	if r.open || terminal < len(r.Items) {
		if running > 0 || terminal > 0 {
			return RunRunning
		}
		return RunQueued
	}

	switch {
	case r.cancelled:
		return RunCancelled
	case failed > 0:
		return RunFailed
	default:
		return RunDone
	}
}

// Finished reports whether every item has settled. An empty *sealed* run is
// finished: there is nothing left for it to do, and reporting it as running
// would leave the spinner chain alive forever. An empty *open* one is not — it
// has found nothing yet, which is where every clone starts.
func (r Run) Finished() bool {
	state := r.State()
	return state != RunRunning && state != RunQueued
}

// Cancelled reports whether the run was asked to stop, whether or not its items
// have settled yet.
func (r Run) Cancelled() bool { return r.cancelled }

// Total is the first half of the "3/12" a footer prints.
func (r Run) Total() int { return len(r.Items) }

// Done is the second half. Both are derived rather than counted alongside,
// which is what removes the need for a scanRun struct beside the existing
// syncRun — the two were the same pair of integers maintained by hand.
//
// It counts the items that have settled, whichever way they settled: a skipped
// repository and a failed one are both finished with, which is what a progress
// line reports on.
func (r Run) Done() int {
	n := 0
	for _, item := range r.Items {
		if item.State.Terminal() {
			n++
		}
	}
	return n
}

// Counts returns how many items sit in each state, for a view that wants to say
// more than "3/12".
func (r Run) Counts() map[ItemState]int {
	out := make(map[ItemState]int, 5)
	for _, item := range r.Items {
		out[item.State]++
	}
	return out
}

// FilterContext keeps the runs stamped with the given context.
//
// It is a function over a snapshot rather than a second registry method: the
// snapshot is what travels in the message, so this is where every view that
// needs the filter already has the data (D8).
func FilterContext(runs []Run, contextName string) []Run {
	out := make([]Run, 0, len(runs))
	for _, run := range runs {
		if run.Context == contextName {
			out = append(out, run)
		}
	}
	return out
}

// Unfinished keeps the runs that are still going.
func Unfinished(runs []Run) []Run {
	out := make([]Run, 0, len(runs))
	for _, run := range runs {
		if !run.Finished() {
			out = append(out, run)
		}
	}
	return out
}
