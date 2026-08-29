package explorer

import "github.com/anthnel/devdesk/internal/jobs"

// What each clone event tells the registry (jobs.Reporter).
//
// The mapping lives here and not in the router, for the reason Reporter exists:
// "a repository that was already on disk is a skip, and it is not news" is this
// package's vocabulary, and a router that knew it would grow a branch per view.
//
// cloneFound and cloneWalkFailed both set Discover, because both name a target
// the run has never heard of — the walk is what finds them (D3). The others
// address a row that exists.

func (m CloneEventMsg) Transition() jobs.Transition {
	t := jobs.Transition{Kind: jobs.KindClone, Target: m.event.path}
	switch m.event.kind {
	case cloneFound:
		t.State, t.Discover = jobs.ItemQueued, true
	case cloneBegan:
		t.State = jobs.ItemRunning
	case cloneEnded:
		switch {
		case m.event.err != nil:
			t.State, t.Detail = jobs.ItemFailed, m.event.err.Error()
		case m.event.skipped:
			// Not a failure and not work: the directory was already there, and
			// decision 2 says the explorer leaves what exists alone.
			t.State = jobs.ItemSkipped
		default:
			t.State = jobs.ItemDone
		}
	case cloneWalkFailed:
		// A group that could not be listed gets a row of its own rather than
		// being folded into a repository's error: the repositories under it
		// were never discovered, so no other row can stand for them. It is
		// discovered *as* a failure — there was never a queued state for it.
		t.State, t.Detail, t.Discover = jobs.ItemFailed, "discovery: "+m.event.err.Error(), true
	}
	return t
}

// Seal says the walk is over (jobs.Sealer). Until it is called the run is
// unsettled whatever its rows say: a walk that has found three repositories and
// cloned all three is not done.
func (CloneRunFinishedMsg) Seal() jobs.Kind { return jobs.KindClone }

// Compile-time proof that the two messages carry what the registry needs. One
// added without it would be routed, would update the view, and would leave its
// row spinning for the life of the screen.
var (
	_ jobs.Reporter = CloneEventMsg{}
	_ jobs.Sealer   = CloneRunFinishedMsg{}
)
