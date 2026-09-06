package app

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// helpViewportMinHeight keeps the overlay usable on a short window; below it
// the border and the scroll hint would leave no room for content.
const helpViewportMinHeight = 10

// helpOverlayMargin is the room the overlay box's border and padding take from
// the window, horizontally and vertically.
const helpOverlayMargin = 6

// maybeOpenHelp opens the help overlay for the active view, unless a field is
// focused — there "?" is a character to type.
func (a *App) maybeOpenHelp(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := a.views[a.currentView]; !ok {
		return a, nil
	}
	if a.inEditMode() {
		return a, a.forwardToActiveView(msg)
	}

	provider, implements := a.views[a.currentView].(help.Provider)
	if !implements {
		return a, nil
	}

	height := max(a.height-helpOverlayMargin, helpViewportMinHeight)
	a.helpViewport = viewport.New(a.width-helpOverlayMargin, height)
	a.helpViewport.KeyMap = arrowOnlyScroll()
	a.helpViewport.SetContent(help.Render(provider.GetHelpContent(), a.width))
	a.showHelp = true
	return a, nil
}

// arrowOnlyScroll strips the vim aliases bubbles/viewport binds by default.
//
// This overlay is the one place that hands a raw key to a viewport, and the
// library's own KeyMap answers j/k/u/d/b/f behind the application's back — so
// the help would have kept scrolling on letters the rest of the application no
// longer treats as navigation (§3.26). A letter is an action now, and an
// overlay is not an exemption from that.
func arrowOnlyScroll() viewport.KeyMap {
	return viewport.KeyMap{
		Up:       key.NewBinding(key.WithKeys("up")),
		Down:     key.NewBinding(key.WithKeys("down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup")),
		PageDown: key.NewBinding(key.WithKeys("pgdown")),
	}
}

// handleHelpKeyMsg handles key presses in the help overlay
func (a *App) handleHelpKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "?":
		a.showHelp = false
		return a, nil
	default:
		// Delegate to the viewport for scrolling (up/down / PgUp / PgDn)
		var cmd tea.Cmd
		a.helpViewport, cmd = a.helpViewport.Update(msg)
		return a, cmd
	}
}

// renderHelpOverlay renders an overlay with the current view's help
func (a *App) renderHelpOverlay() string {
	return theme.OverlayBoxStyle().Render(a.helpViewport.View() + "\n")
}
