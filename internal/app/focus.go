package app

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// handleTerminalBlur records that the terminal lost the focus. Nothing else
// happens here: the live animation chain's in-flight tick reaches
// handleAnimationTick, wantedCadence says no, and the chain is not renewed
// (DEVDESK-210). No generation bump is needed.
func (a *App) handleTerminalBlur() (tea.Model, tea.Cmd) {
	a.terminalBlurred = true
	return a, nil
}

// handleTerminalFocus clears the blur. A focus with no prior blur changes
// nothing (an unknown focus means focused). The Update wrapper's ensureTick
// restarts the one chain when something needs it.
func (a *App) handleTerminalFocus() (tea.Model, tea.Cmd) {
	a.terminalBlurred = false
	return a, nil
}

// wantedCadence is cadence() for the router's current state: while the terminal
// is blurred no chain is wanted, whatever the views or the jobs ask for.
func (a *App) wantedCadence() (time.Duration, bool) {
	if a.terminalBlurred {
		return 0, false
	}
	return cadence(a.jobs.Running() > 0, a.wantsBlink(), a.motionStatic)
}
