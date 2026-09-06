package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/keymap"
)

// commandModeKey enters command mode from anywhere, including from a focused
// text input.
//
// It is not "ctrl+:", and the reason still holds: a terminal encodes Ctrl only
// for ASCII 0x40-0x5F, and ":" is 0x3A, so that combination never reaches the
// application. But the fallback chosen then — "alt+:" — had the mirror defect.
// On Terminal.app and iTerm2, Option is not Meta unless the user turns it on:
// Option+Shift+; emits a literal character and the key never arrives either.
// That was worse than a key that plainly does not exist, because every view
// advertised it in GetShortcuts() and it was the only way in from a focused
// field.
//
// ctrl+p is free application-wide, is no tty control character, is neither
// screen's prefix (ctrl+a) nor tmux's (ctrl+b), and its meaning is already
// learned — palette. See internal/ui/keymap.
const commandModeKey = keymap.CommandMode

// handleKeyMsg processes keyboard input
func (a *App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If an overlay is displayed, handle its navigation
	if a.showHelp {
		return a.handleHelpKeyMsg(msg)
	}
	if a.showContextList {
		return a.handleContextListKeyMsg(msg)
	}

	if a.commandMode {
		return a.handleCommandMode(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		return a, tea.Quit
	case "q":
		cmd := a.maybeQuitApplication(msg)
		return a, cmd
	case "?":
		return a.maybeOpenHelp(msg)
	case commandModeKey:
		// The authoritative way in: handled here, before any InEditMode() check,
		// so no text input can claim it. A bare ":" cannot play that role — it is
		// an ordinary character in a field holding https://trivy-server:4954.
		return a, a.enterCommandMode()
	case ":":
		cmd := a.maybeEnterInCommandMode(msg)
		return a, cmd
	// esc is deliberately absent: it belongs to the view. The router used to
	// answer it whenever the view was not editing, which made esc-to-go-back dead
	// code everywhere and forced views that wanted it to claim they were editing
	// — a predicate about focused fields, overloaded into one about key
	// ownership (§1.3 D15).
	//
	// Nothing is lost by handing it over: command mode is already closed by the
	// time this switch runs (handleCommandMode answers first), so the router had
	// nothing left to close.
	default:
		return a, a.forwardToActiveView(msg)
	}
}

// forwardToActiveView hands a message to the current view and re-measures the
// layout when the view's footer changed shape in response (Rule 124) — opening
// a filter bar is the case that matters.
func (a *App) forwardToActiveView(msg tea.Msg) tea.Cmd {
	view, ok := a.views[a.currentView]
	if !ok {
		return nil
	}
	updatedView, cmd := view.Update(msg)
	a.views[a.currentView] = updatedView
	if newFooterHeight := a.getFooterHeight(); newFooterHeight != a.lastFooterHeight {
		a.resize(a.width, a.height)
	}
	return cmd
}

// enterCommandMode opens the command line unconditionally and asks for a
// re-layout, since the command line replaces the inactive prompt.
//
// Focus is taken here rather than while rendering: a bubbles/textinput drops
// every key it receives while blurred, so granting focus from View() would make
// the command line depend on a render having happened first (Rule 110 — View()
// is read-only).
func (a *App) enterCommandMode() tea.Cmd {
	a.commandMode = true
	a.commandInput.Reset()
	a.commandInput.Focus()
	return a.requestResize()
}

// inEditMode reports whether the active view has a focused field, which is what
// decides whether the router claims a key or passes it on.
func (a *App) inEditMode() bool {
	view, ok := a.views[a.currentView]
	if !ok {
		return false
	}
	formView, implements := view.(FormView)
	return implements && formView.InEditMode()
}

// maybeEnterInCommandMode answers a bare ":", which only opens the command line
// when nothing is being edited — inside a text input ":" is an ordinary
// character. Use altCommandModeKey to get in from anywhere.
func (a *App) maybeEnterInCommandMode(msg tea.Msg) tea.Cmd {
	if _, ok := a.views[a.currentView]; !ok {
		return nil
	}
	if !a.inEditMode() {
		return a.enterCommandMode()
	}
	return a.forwardToActiveView(msg)
}

func (a *App) maybeQuitApplication(msg tea.Msg) tea.Cmd {
	if _, ok := a.views[a.currentView]; !ok {
		return nil
	}
	if !a.inEditMode() {
		return tea.Quit
	}
	return a.forwardToActiveView(msg)
}
