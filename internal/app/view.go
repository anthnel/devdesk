package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/ui/overlay"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// View is the program's frame. The router is the only place that sets the
// terminal state (alternate screen now), the views only provide content.
func (a *App) View() tea.View {
	v := tea.NewView(a.Render())
	v.AltScreen = true
	v.ReportFocus = true // mode 1004: BlurMsg/FocusMsg (DEVDESK-210)
	v.BackgroundColor = terminalBackground(a.colorProfile, theme.ColorBackground)
	v.ProgressBar = terminalProgress(a.progress)                 // DEVDESK-211
	v.WindowTitle = windowTitle(a.currentView, a.currentContext) // DEVDESK-257
	return v
}

// Render renders the interface: header, title line, viewport, footer (Rule 124).
// An open overlay replaces the whole thing.
func (a *App) Render() string {
	if overlay := a.activeOverlay(); overlay != "" {
		return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, overlay,
			lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(theme.ColorBackground)))
	}

	header := a.renderHeader() + "\n" + a.renderTitleLine()
	body := a.renderBody()
	footer := a.renderViewFooter()
	inner := fitHeight(header, body, footer, a.height)

	placed := lipgloss.Place(a.width, a.height, lipgloss.Left, lipgloss.Top, inner,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(theme.ColorBackground)))
	return a.withPalette(placed)
}

// withPalette draws the open command palette over the final screen. The
// composition happens after fitHeight and Place, on exactly height lines of
// width cells, so the geometry is in screen rows whatever the view is.
func (a *App) withPalette(screen string) string {
	lines := a.paletteLines()
	if len(lines) == 0 {
		return screen
	}
	r := a.paletteRect()
	composed := overlay.Compose(strings.Split(screen, "\n"), lines, r.X, r.Y, a.width)
	return strings.Join(composed, "\n")
}

// fitHeight stacks header, body and footer in at most height lines.
// lipgloss.Place pads a short block but never crops a tall one, so on a very
// small terminal (header 8 + title 1 + the viewport's minimum + footer) the
// frame would be taller than the screen. The surplus is taken from the body
// first, bottom lines first: the footer carries the messages, so it outranks
// viewport rows. If header and footer alone are taller than the terminal, the
// stack is cut to its first height lines, the simplest floor. At normal
// heights nothing is cut.
func fitHeight(header, body, footer string, height int) string {
	parts := []string{header}
	if body != "" {
		parts = append(parts, body)
	}
	if footer != "" {
		parts = append(parts, footer)
	}
	lines := strings.Split(strings.Join(parts, "\n"), "\n")
	excess := len(lines) - height
	if height <= 0 || excess <= 0 {
		return strings.Join(lines, "\n")
	}

	headerLen := strings.Count(header, "\n") + 1
	bodyLen := 0
	if body != "" {
		bodyLen = strings.Count(body, "\n") + 1
	}
	cut := min(excess, bodyLen)
	if cut > 0 {
		end := headerLen + bodyLen
		lines = append(lines[:end-cut], lines[end:]...)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// activeOverlay returns the overlay to draw instead of the view, or "" when
// there is none. The order matches the one handleKeyMsg dispatches in, so the
// overlay holding the keyboard is the one on screen.
func (a *App) activeOverlay() string {
	switch {
	case a.switchingTo != "":
		return a.renderContextSwitchingOverlay()
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
		content = view.Render()
	}

	innerWidth := a.viewport.Width() - 2 // -2 for the borders
	if a.frameless() {
		innerWidth = a.viewport.Width()
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
	return theme.RenderBorderTitleTrail(title, a.frameTrail(title), a.width)
}

// frameTrail asks the active view for its border trail, within the budget the
// theme leaves after title. It returns "" when there is no room, the view has
// no trail, or the trail is wider than its budget.
func (a *App) frameTrail(title string) string {
	view, ok := a.views[a.currentView]
	if !ok {
		return ""
	}
	tv, ok := view.(FrameTrailView)
	if !ok {
		return ""
	}
	budget := theme.BorderTrailBudget(title, a.width)
	if budget <= 0 {
		return ""
	}
	trail := tv.FrameTrail(budget)
	if lipgloss.Width(trail) > budget {
		return ""
	}
	return trail
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
