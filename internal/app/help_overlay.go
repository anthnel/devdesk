package app

import (
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
	a.helpViewport.SetContent(help.Render(provider.GetHelpContent(), a.width))
	a.showHelp = true
	return a, nil
}

// handleHelpKeyMsg gère les touches clavier dans l'overlay d'aide
func (a *App) handleHelpKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "?":
		a.showHelp = false
		return a, nil
	default:
		// Déléguer au viewport pour le scroll (↑↓/jk/PgUp/PgDn/g/G)
		var cmd tea.Cmd
		a.helpViewport, cmd = a.helpViewport.Update(msg)
		return a, cmd
	}
}

// renderHelpOverlay affiche un overlay avec l'aide de la vue courante
func (a *App) renderHelpOverlay() string {
	return theme.OverlayBoxStyle().Render(a.helpViewport.View() + "\n")
}
