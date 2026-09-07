package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// View renders the interface: header, title line, viewport, footer (Rule 124).
// An open overlay replaces the whole thing.
func (a *App) View() string {
	if overlay := a.activeOverlay(); overlay != "" {
		return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, overlay,
			lipgloss.WithWhitespaceBackground(theme.ColorBackground))
	}

	inner := a.renderHeader() + "\n" + a.renderTitleLine() + "\n" + a.renderBody()
	if footer := a.renderViewFooter(); footer != "" {
		inner += "\n" + footer
	}

	return lipgloss.Place(a.width, a.height, lipgloss.Left, lipgloss.Top, inner,
		lipgloss.WithWhitespaceBackground(theme.ColorBackground))
}

// activeOverlay returns the overlay to draw instead of the view, or "" when
// there is none. The order matches the one handleKeyMsg dispatches in, so the
// overlay holding the keyboard is the one on screen.
func (a *App) activeOverlay() string {
	switch {
	case a.showHelp:
		return a.renderHelpOverlay()
	case a.showContextList:
		return a.renderContextListOverlay()
	}
	return ""
}

// renderBody fills the viewport with the active view's content, padded to the
// full inner width so no line shows the terminal's own background (Rule 115).
func (a *App) renderBody() string {
	content := "View not found: " + string(a.currentView)
	if view, ok := a.views[a.currentView]; ok {
		content = view.View()
	}

	innerWidth := a.viewport.Width - 2 // -2 for the borders
	if a.frameless() {
		innerWidth = a.viewport.Width
	}
	a.viewport.SetContent(lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorText).
		Width(innerWidth).
		Render(content))

	body := a.viewport.View()

	// Rule 136: with a filter bar below it, the viewport's bottom corners become
	// T-junctions so the two form one closed rectangle.
	if view, ok := a.views[a.currentView]; ok {
		if fbv, ok := view.(FilterBarView); ok && fbv.FilterBarVisible() {
			body = replaceViewportBottomCorners(body)
		}
	}
	return body
}

// renderTitleLine draws the viewport's top border carrying the view's title —
// or, for a frameless view, a titled rule with no corners: GetTitle() keeps a
// reader oriented where a top border would draw the top of a nonexistent box.
func (a *App) renderTitleLine() string {
	title := ""
	if view, ok := a.views[a.currentView]; ok {
		if hv, implements := view.(HeaderView); implements {
			title = hv.GetTitle()
		}
	}
	if a.frameless() {
		return theme.RenderTitledRule(title, a.width)
	}
	return theme.RenderBorderTitle(title, a.width)
}

// frameless reports whether the active view draws its own frames (see
// FramelessView). The default is framed.
func (a *App) frameless() bool {
	view, ok := a.views[a.currentView]
	if !ok {
		return false
	}
	fv, implements := view.(FramelessView)
	return implements && fv.Frameless()
}

// renderViewFooter returns the active view's footer, or "" when it has none.
func (a *App) renderViewFooter() string {
	if view, ok := a.views[a.currentView]; ok {
		if fv, ok := view.(FooterView); ok {
			return fv.RenderFooter(a.width)
		}
	}
	return ""
}

// replaceViewportBottomCorners turns the viewport's bottom └ and ┘ into the
// T-junctions ├ and ┤, so the filter bar rendered below it in the footer joins
// the border into one closed rectangle instead of two stacked boxes.
func replaceViewportBottomCorners(body string) string {
	lastNewline := strings.LastIndex(body, "\n")
	if lastNewline == -1 {
		return body
	}
	lastLine := body[lastNewline+1:]
	lastLine = strings.Replace(lastLine, "└", "├", 1)
	lastLine = strings.Replace(lastLine, "┘", "┤", 1)
	return body[:lastNewline+1] + lastLine
}
