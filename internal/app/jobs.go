package app

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// JobsChangedMsg carries what the registry holds to every view that might show
// it. It is the whole of the broadcast (D1): a view keeps its own copy of a
// snapshot rather than reading a pointer the router writes, so nothing is read
// from View() while something else is writing it.
//
// It is sent on two occasions, and they are different in nature: a transition —
// something actually happened — and a spinner tick, where only Frame moved. The
// receiver does not have to tell them apart, which is why there is one message
// and not two.
type JobsChangedMsg struct {
	// Runs is a copy, oldest first, every context included. Filtering on the
	// current one is the view's business (D8, jobs.FilterContext).
	Runs []jobs.Run

	// Frame is the current spinner frame, **bare** — no escape sequence.
	// Rule 122: a table cell is measured before it is styled, so a styled frame
	// of seven visible cells measures twenty-eight and is truncated inside its
	// own escape, which then bleeds down the rest of the table. A footer wants
	// it the other way round; RenderedFrame is that.
	Frame string
}

// RenderedFrame is Frame with the spinner style applied, for a footer.
//
// Rule 128 asks for the *rendered* spinner in FooterMessage.SetSpinnerFrame,
// because measurement there goes through lipgloss.Width, which ignores escapes.
// The styling lives here rather than in each view so the two readings of the
// same frame cannot drift.
func (m JobsChangedMsg) RenderedFrame() string {
	if m.Frame == "" {
		return ""
	}
	return theme.SpinnerStyle().Render(m.Frame)
}

// Running counts the runs in the snapshot that have not settled. It saves every
// receiver the same three lines.
func (m JobsChangedMsg) Running() int {
	return len(jobs.Unfinished(m.Runs))
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
	msg := JobsChangedMsg{Runs: a.jobs.Snapshot(), Frame: a.jobFrame()}

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
	updated, cmd := view.Update(JobsChangedMsg{Runs: a.jobs.Snapshot(), Frame: a.jobFrame()})
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
