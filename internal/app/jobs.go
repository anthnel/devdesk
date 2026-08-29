package app

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// changedMsg is the snapshot the router broadcasts. It fills both readings of
// the frame — bare for a table cell (Rule 122), styled for a footer
// (Rule 128) — so the two cannot drift and internal/jobs needs to know nothing
// about styling.
func (a *App) changedMsg() jobs.ChangedMsg {
	frame := a.jobFrame()
	return jobs.ChangedMsg{
		Runs:          a.jobs.Snapshot(),
		Frame:         frame,
		RenderedFrame: theme.SpinnerStyle().Render(frame),
	}
}

// handleStartJobs admits a run a view built and asks the registry for it.
//
// The context is stamped here rather than by the view: the router is what knows
// which context is current, and a view reading it back from the configuration
// would be reading it again — which is the shape of the defect the stamp exists
// to close (a batch outliving a switch and writing into the new context's
// cache).
func (a *App) handleStartJobs(msg jobs.StartMsg) (tea.Model, tea.Cmd) {
	run := msg.Run
	run.Context = a.currentContext
	a.jobs.Start(run)
	// The work goes out after the registration, which is the whole reason it
	// travels on the message: a transition naming a run the registry has not
	// admitted yet is refused, and the row would spin for the life of the view.
	return a, tea.Batch(a.jobsChanged(), msg.Work)
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
	_, cmd := a.routeToView(target, msg)
	return a, tea.Batch(append(cmds, cmd)...)
}

// jobTickMsg advances the one spinner frame the whole application shares.
//
// It is the router's own message type rather than a spinner.TickMsg on purpose.
// bubbles keeps chains apart with an id and a tag, which works, but it means a
// stray tick from any view is something this handler would have to recognise
// and decline. A message nobody else sends cannot be confused with anything.
type jobTickMsg struct {
	// seq identifies the chain. A tick from an older one is dropped, which is
	// what makes a second chain impossible rather than merely unlikely — the
	// failure the four hand-stamped spinners kept producing was two chains
	// advancing the same frame at twice the rate.
	seq int
}

// jobSpinnerInterval is the cadence of the shared frame. It is taken from the
// same spinner the views used, so the animation does not change speed on the
// day a view stops stamping its own.
//
// It is a var rather than a const for one reason: testutil.FastTimers shortens
// it for the length of a test, the way components.FooterMsgDuration is. A test
// that drains the chain would otherwise sleep a tenth of a second per tick.
var jobSpinnerInterval = spinner.Dot.FPS

func jobTick(seq int) tea.Cmd {
	return tea.Tick(jobSpinnerInterval, func(time.Time) tea.Msg {
		return jobTickMsg{seq: seq}
	})
}

// jobFrame returns the current bare frame.
func (a *App) jobFrame() string {
	frames := spinner.Dot.Frames
	return frames[a.jobFrameIdx%len(frames)]
}

// jobsChanged is what a launch site or a transition calls once it has written
// to the registry: it tells every view, and makes sure the frame is moving.
//
// Both halves are needed and neither implies the other — a run that starts
// while another is already going changes the snapshot without needing a second
// chain, and a run that settles changes the snapshot while the chain must be
// allowed to die.
func (a *App) jobsChanged() tea.Cmd {
	return tea.Batch(a.broadcastJobs(), a.ensureJobTick())
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

// ensureJobTick starts the shared chain if work is running and no chain is
// alive. It is the one place a chain is ever started (D5).
func (a *App) ensureJobTick() tea.Cmd {
	if a.jobTicking || a.jobs.Running() == 0 {
		return nil
	}
	a.jobTicking = true
	a.jobTickSeq++
	return jobTick(a.jobTickSeq)
}

// handleJobTick advances the shared frame and schedules the next one, for as
// long as there is something to animate.
//
// The chain stops by not being renewed, and `jobTicking` is what lets the next
// run start a new one. Renewing it against an empty registry would rebuild
// every view's rows ten times a second for a settled application — which is the
// reason each view stopped its own chain, and, having no way to restart it,
// left its spinner frozen on the frame it died at.
func (a *App) handleJobTick(msg jobTickMsg) (tea.Model, tea.Cmd) {
	if msg.seq != a.jobTickSeq {
		return a, nil
	}
	if a.jobs.Running() == 0 {
		a.jobTicking = false
		return a, nil
	}
	a.jobFrameIdx = (a.jobFrameIdx + 1) % len(spinner.Dot.Frames)
	return a, tea.Batch(a.broadcastJobs(), jobTick(a.jobTickSeq))
}
