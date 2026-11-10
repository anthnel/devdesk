package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/shared"
)

// handleTerminalBlur records that the terminal lost the focus. Nothing else
// happens here: the live animation chain's in-flight tick reaches
// handleAnimationTick, wantedCadence says no, and the chain is not renewed
// (DEVDESK-210). No generation bump is needed. On the edge only, the view on
// screen is told.
func (a *App) handleTerminalBlur() (tea.Model, tea.Cmd) {
	if a.terminalBlurred {
		return a, nil
	}
	a.terminalBlurred = true
	return a, a.sendFocusTo(a.currentView, false)
}

// handleTerminalFocus clears the blur. A focus with no prior blur changes
// nothing (an unknown focus means focused) and tells nobody. After a blur, the
// view on screen is asked to resume. The Update wrapper's ensureTick restarts
// the one chain when something needs it.
func (a *App) handleTerminalFocus() (tea.Model, tea.Cmd) {
	if !a.terminalBlurred {
		return a, nil
	}
	a.terminalBlurred = false
	return a, a.sendFocusTo(a.currentView, true)
}

// sendFocusTo hands one view the terminal focus state. It never calls Init,
// starts no scan and sets no footer message; the view decides what to do with
// it. resume is true only when the focus comes back after a blur.
func (a *App) sendFocusTo(target command.ViewType, resume bool) tea.Cmd {
	view, ok := a.views[target]
	if !ok {
		return nil
	}
	updated, cmd := view.Update(shared.TerminalFocusMsg{Focused: !a.terminalBlurred, Resume: resume && !a.terminalBlurred})
	a.setView(target, updated)
	return cmd
}

// wantedCadence is cadence() for the router's current state: while the terminal
// is blurred no chain is wanted, whatever the views or the jobs ask for.
func (a *App) wantedCadence() (time.Duration, bool) {
	if a.terminalBlurred {
		return 0, false
	}
	return cadence(a.jobs.Running() > 0, a.wantsBlink(), a.motionStatic)
}
