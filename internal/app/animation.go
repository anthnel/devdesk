package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
)

// blinkHalfPeriod is the blink's half period (D7: 1 Hz). A const: the phase formula
// must not change when a test shortens the chain's interval.
const blinkHalfPeriod = 500 * time.Millisecond

// animationBlinkInterval is the chain's interval, a var so testutil.FastTimers can
// shorten it.
var animationBlinkInterval = blinkHalfPeriod

// blinkPhase reads the phase off the tick's own time, so every view blinks in step
// whatever the interval the chain currently runs at.
func blinkPhase(t time.Time) bool {
	return (t.UnixMilli()/blinkHalfPeriod.Milliseconds())%2 == 0
}

// cadence returns the interval the animation chain should run at, and false when
// nothing needs it. Running jobs need it (the views refresh their durations, and
// static motion included); a blink alone needs it unless static motion is
// requested (then the glyph stays steady and no chain runs).
func cadence(running, wantsBlink, static bool) (time.Duration, bool) {
	switch {
	case running, wantsBlink && !static:
		return animationBlinkInterval, true
	default:
		return 0, false
	}
}

// animationState is the one chain that moves anything on a row (D5, renamed D2).
type animationState struct {
	seq      int           // identifies the live chain; a tick from another is dropped
	ticking  bool          // a chain is alive
	interval time.Duration // cadence of the live chain
	blinkOn  bool          // true whenever no chain runs
}

// animationTickMsg advances the animation. at is the tick's own time, from which
// the blink phase is read.
type animationTickMsg struct {
	seq int
	at  time.Time
}

// animationEvery is tea.Every, a var so tests record the interval and inject the time.
var animationEvery = tea.Every

// animationTick asks for the next tick of chain seq. It is on the clock's
// boundaries (tea.Every) so the blink phase alternates strictly. seq and d are
// captured by value: the Cmd never touches the router (Rule 110).
func animationTick(seq int, d time.Duration) tea.Cmd {
	return animationEvery(d, func(t time.Time) tea.Msg {
		return animationTickMsg{seq: seq, at: t}
	})
}

// frameMsg builds the animation message: the blink phase, nothing else.
func (a *App) frameMsg() jobs.FrameMsg {
	return jobs.FrameMsg{Blink: a.anim.blinkOn}
}

// sendFrameTo hands the current frame to one view: the view on screen on every
// tick, and a view entering the screen. No resize check: a frame never changes
// a footer's height.
func (a *App) sendFrameTo(target command.ViewType) tea.Cmd {
	view, ok := a.views[target]
	if !ok {
		return nil
	}
	updated, cmd := view.Update(a.frameMsg())
	a.setView(target, updated)
	return cmd
}

// ensureTick starts the chain when something needs one and none is alive. A
// chain already running is kept.
// It returns nil otherwise, which makes it idempotent within one Update. It is
// the one place a chain is ever started (D5).
func (a *App) ensureTick() tea.Cmd {
	d, ok := a.wantedCadence()
	if !ok || a.anim.ticking {
		return nil
	}
	a.anim.ticking = true
	a.anim.seq++
	a.anim.interval = d
	return animationTick(a.anim.seq, d)
}

// handleAnimationTick advances the animation, reads the blink phase off the
// tick's own time, hands it to the view on screen, and renews the chain for as
// long as something needs it, at the cadence the need has now.
//
// The chain stops by not being renewed. It never stops on an "off" phase: a row
// is left on a steady "on" glyph.
func (a *App) handleAnimationTick(msg animationTickMsg) (tea.Model, tea.Cmd) {
	if msg.seq != a.anim.seq {
		return a, nil
	}
	d, ok := a.wantedCadence()
	if !ok {
		a.anim.ticking = false
		// A blur always hands the view one last pinned frame: the tick that
		// lands after it is the last the view will see for a while.
		if a.anim.blinkOn && !a.suspended() {
			return a, nil
		}
		a.anim.blinkOn = true
		return a, a.sendFrameTo(a.currentView)
	}
	a.anim.blinkOn = a.motionStatic || blinkPhase(msg.at)
	a.anim.interval = d
	return a, tea.Batch(a.sendFrameTo(a.currentView), animationTick(a.anim.seq, d))
}
