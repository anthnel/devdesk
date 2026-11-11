package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
)

// terminalProgress maps the jobs summary to the terminal tab's progress bar
// (DEVDESK-211). It is pure and returns a fresh pointer on every call: the
// renderer compares through the pointer it kept, so an in-place mutation would
// never be redrawn. nil when idle, which clears the bar.
//
// Error and an unmeasured Warning are drawn full (100): the state is the
// message there, and a full bar stays visible on terminals that hide a 0 value.
func terminalProgress(p jobs.Progress) *tea.ProgressBar {
	switch p.State {
	case jobs.ProgressIndeterminate:
		return tea.NewProgressBar(tea.ProgressBarIndeterminate, 0)
	case jobs.ProgressPercent:
		return tea.NewProgressBar(tea.ProgressBarDefault, p.Percent)
	case jobs.ProgressWarning:
		return tea.NewProgressBar(tea.ProgressBarWarning, p.Percent)
	case jobs.ProgressError:
		return tea.NewProgressBar(tea.ProgressBarError, 100)
	default:
		return nil
	}
}

// summarizeJobs scopes the snapshot to the current context (D8), acknowledges
// the failures when :jobs is on screen and the terminal has the focus (showing
// them is reading them, and a blurred terminal shows nothing to anyone), and
// stores the summary View() reads. Called from Update only (Rule 110).
func (a *App) summarizeJobs(msg jobs.ChangedMsg) {
	runs := jobs.Unfinished(jobs.FilterContext(msg.Runs, msg.Context))
	failures := jobs.FailuresIn(msg.Failures, msg.Context)
	if a.currentView == command.ViewJobs && !a.terminalBlurred {
		if a.failuresReadThrough == nil {
			a.failuresReadThrough = map[string]uint64{}
		}
		a.failuresReadThrough[msg.Context] = max(a.failuresReadThrough[msg.Context], jobs.LatestFailureSeq(failures))
	}
	a.progress = jobs.Summarize(runs, failures, a.failuresReadThrough[msg.Context])
	a.progressContext = msg.Context
}

// syncTerminalProgress is the level check run after every Update: it
// re-summarizes when the context changed since the last summary, or when :jobs
// is on screen, focused, with an unread failure (whatever path brought the view in).
// Otherwise it does nothing, so a tick copies no snapshot.
func (a *App) syncTerminalProgress() {
	if a.currentContext != a.progressContext ||
		(a.currentView == command.ViewJobs && !a.terminalBlurred && a.progress.Unread > 0) {
		a.summarizeJobs(a.changedMsg())
	}
}
