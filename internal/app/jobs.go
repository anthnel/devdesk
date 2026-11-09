package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
)

// changedMsg is the snapshot the router broadcasts.
func (a *App) changedMsg() jobs.ChangedMsg {
	return jobs.ChangedMsg{
		Runs:     a.jobs.Snapshot(),
		Failures: a.jobs.Failures(),
		Context:  a.currentContext,
	}
}

// handleStartJobs admits a run a view built and asks the registry for it.
//
// The context is stamped here rather than by the view: the router is what knows
// which context is current, and a view reading it back from the configuration
// would be reading it again — which is the shape of the defect the stamp exists
// to close (a batch outliving a switch and writing into the new context's
// cache).
//
// The stamp is then handed to the work, which is the half that was missing
// (D68): a run carrying the right name while its commands re-read the current
// one told `:jobs` the truth and wrote the cache anyway. Building the work here
// means the name is read once, in Update, and every command of the batch shares
// the string — a switch three repositories later cannot reach it.
func (a *App) handleStartJobs(msg jobs.StartMsg) (tea.Model, tea.Cmd) {
	run := msg.Run
	run.Context = a.currentContext
	id := a.jobs.Start(run)
	if msg.Cancel != nil {
		a.jobs.AttachRun(id, msg.Cancel)
	}
	// The identifier exists here and nowhere else, which is why an MCP call
	// waiting for one is answered from this handler (§3.61).
	a.settleInvocation(msg.Invocation, id, nil)
	// The work goes out after the registration, which is the whole reason it
	// travels on the message: a transition naming a run the registry has not
	// admitted yet is refused, and the row would spin for the life of the view.
	cmds := []tea.Cmd{a.jobsChanged()}
	if msg.Work != nil {
		cmds = append(cmds, msg.Work(run.Context))
	}
	return a, tea.Batch(cmds...)
}

// routeWork applies a progress message to the registry and hands it to the view
// that owns it.
//
// The registry is written **before** the view sees the message, so the view is
// reading a current snapshot when it handles it — which is what lets it ask
// "has my batch finished?" rather than counting alongside.
//
// A message that reports on work nobody registered is still routed: the view
// has its own reasons to see it, and Apply already returned false rather than
// inventing a run for it.
func (a *App) routeWork(target command.ViewType, msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if reporter, ok := msg.(jobs.Reporter); ok {
		if a.jobs.Apply(reporter.Transition()) {
			cmds = append(cmds, a.jobsChanged())
		}
	}
	// A sealer says a progressive run has found everything it is going to
	// (D3). It is a second interface because sealing names no target — the
	// message carrying it is a closed channel, and there is nothing for it to
	// be about.
	if sealer, ok := msg.(jobs.Sealer); ok {
		if a.jobs.Seal(sealer.Seal()) {
			cmds = append(cmds, a.jobsChanged())
		}
	}
	_, cmd := a.routeToView(target, msg)
	return a, tea.Batch(append(cmds, cmd)...)
}

// handleCancelOpen stops the open run of a kind and tells every view.
//
// The view that asked is updated by the broadcast like any other, so it does
// not have to keep its own idea of "cancelling" in step with the registry's.
func (a *App) handleCancelOpen(msg jobs.CancelOpenMsg) (tea.Model, tea.Cmd) {
	if !a.jobs.CancelOpen(msg.Kind) {
		return a, nil
	}
	return a, a.jobsChanged()
}

// handleCancel stops a run named by identifier, and handleCancelItem one of its
// targets (D7).
//
// The identifier comes from the snapshot the asking view is rendering, which is
// what makes it addressable at all: a launch site could not hold one — it would
// have to before the registry allocated it — but `:jobs` reads it off the row
// the cursor is on.
//
// A refusal is silent here, and it is not a swallowed error: the view asked
// only because its own guard said it could (Rule 130), so a false answer means
// the run settled between the keypress and this handler. There is nothing to
// tell the user that the next broadcast will not say better.
func (a *App) handleCancel(msg jobs.CancelMsg) (tea.Model, tea.Cmd) {
	if !a.jobs.Cancel(msg.ID) {
		return a, nil
	}
	return a, a.jobsChanged()
}

func (a *App) handleCancelItem(msg jobs.CancelItemMsg) (tea.Model, tea.Cmd) {
	if !a.jobs.CancelItem(msg.ID, msg.Target) {
		return a, nil
	}
	return a, a.jobsChanged()
}

// handleForgetTarget drops what the registry remembers about a target a view
// removed outside any run, in the current context only, and broadcasts only
// when something went.
func (a *App) handleForgetTarget(msg jobs.ForgetTargetMsg) (tea.Model, tea.Cmd) {
	if !a.jobs.Forget(msg.Target, a.currentContext) {
		return a, nil
	}
	return a, a.jobsChanged()
}

// jobsChanged is what a launch site or a transition calls once it has written
// to the registry: it tells every view, hands the view on screen the current
// frame, and makes sure the animation chain is alive.
//
// The three are separate and none implies another: a run that starts while
// another is already going changes the snapshot without needing a second chain,
// and a run that settles changes the snapshot while the chain must be allowed
// to die.
func (a *App) jobsChanged() tea.Cmd {
	return tea.Batch(a.broadcastJobs(), a.sendFrameTo(a.currentView), a.ensureTick())
}

// broadcastJobs hands the current snapshot to every view the router holds, on
// screen or not.
//
// Every view and not just the active one: a view is updated where it stands so
// that coming back to it shows what happened while it was away, without it
// having to ask. The cost is one Update per held view per frame, and a view's
// Update returns on the first type switch for a message it does not know.
//
// The layout is re-measured once at the end rather than per view, for the same
// reason forwardToActiveView measures at all: a footer that grew a line has to
// be given it (Rule 124). Only the view on screen can change the height, so one
// check covers the loop.
func (a *App) broadcastJobs() tea.Cmd {
	msg := a.changedMsg()

	cmds := make([]tea.Cmd, 0, len(a.views))
	for name, view := range a.views {
		updated, cmd := view.Update(msg)
		a.views[name] = updated
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if newFooterHeight := a.getFooterHeight(); newFooterHeight != a.lastFooterHeight {
		a.resize(a.width, a.height)
	}
	return tea.Batch(cmds...)
}

// enterView hands a view that is entering the screen the snapshot and the
// current frame. Every path that installs a view without going through
// switchView calls it too, otherwise the view shows stale rows until the next
// change.
func (a *App) enterView(target command.ViewType) tea.Cmd {
	return tea.Batch(a.sendJobsTo(target), a.sendFrameTo(target))
}

// sendJobsTo hands the snapshot to one view, which is what a view entering the
// screen needs: it was updated where it stood, but a view built lazily on the
// way in was not there for any of it.
func (a *App) sendJobsTo(target command.ViewType) tea.Cmd {
	view, ok := a.views[target]
	if !ok {
		return nil
	}
	updated, cmd := view.Update(a.changedMsg())
	a.views[target] = updated
	return cmd
}
