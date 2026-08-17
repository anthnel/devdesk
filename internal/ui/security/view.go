package security

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// View renders the UI
func (m Model) View() string {
	// Show whichever modal is active (Rule 112: modals confirm, forms fill the
	// viewport).
	if modal := m.modalView(); modal != "" {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			modal,
			lipgloss.WithWhitespaceBackground(theme.ColorBackground),
		)
	}

	switch m.state {
	case StateInventory:
		return m.renderInventoryView()
	case StateResults:
		return m.renderResultsView()
	case StateDetails:
		return m.renderDetailsView()
	}
	return ""
}

// renderResultsView renders the results summary
func (m Model) renderResultsView() string {
	if m.result == nil {
		return "No results"
	}

	// Warnings replace the table and hide the tab bar (no partial results to browse)
	//
	// The status message used to be repeated here, under the panel. The footer
	// already carries it, and a message printed twice is a message the two
	// copies can disagree about (Rule 134's reasoning, one layer down).
	if len(m.result.Errors) > 0 {
		return m.renderWarningsPanel()
	}

	return m.findingsTable.View()
}

// GetFooterHeight returns the footer height for this view (Rule 124).
func (m Model) GetFooterHeight() int {
	height := 2 // empty line + info line
	if m.showsResultTabs() {
		height = 3 // tab bar + empty line + info line
	}
	if bar := m.activeFilterBar(); bar != nil {
		height += bar.ExtraHeight() // Rule 136
	}
	return height
}

// RenderFooter returns the footer content rendered below the viewport (Rule 124).
func (m Model) RenderFooter(width int) string {
	if m.showsResultTabs() {
		return theme.PadWithBg(theme.Bg(" ")+m.renderTabs(), width) + "\n" +
			theme.EmptyLineBg(width) + "\n" + m.renderInfoLine(width)
	}
	if m.state == StateInventory {
		var parts []string
		if bar := m.inventory.FilterBar(); bar.IsVisible() {
			parts = append(parts, bar.View())
		}
		parts = append(parts, theme.EmptyLineBg(width), m.renderInfoLine(width))
		return strings.Join(parts, "\n")
	}
	return theme.EmptyLineBg(width) + "\n" + m.renderInfoLine(width)
}

// modalView renders whichever modal is open, or "" when none is.
func (m Model) modalView() string {
	switch {
	case m.confirmModal != nil:
		return m.confirmModal.View()
	case m.scanAllModal != nil:
		return m.scanAllModal.View()
	}
	return ""
}

// FilterBarVisible reports whether the filter bar is on screen, which is what
// closes the viewport's bottom border around it (implements app.FilterBarView,
// Rule 136). Both tables have one now: the findings table gained the four
// severity tokens that used to be a cycle on `.`.
func (m Model) FilterBarVisible() bool {
	return m.activeFilterBar() != nil
}

// activeFilterBar is the bar of whichever table is on screen, or nil when the
// state has no table. One resolution rather than three, so GetFooterHeight and
// RenderFooter cannot disagree about whether the bar is there — which is the
// one way to make Rule 124's arithmetic wrong by a line.
func (m Model) activeFilterBar() *sharedcomponents.FilterBar {
	var bar *sharedcomponents.FilterBar
	switch {
	case m.state == StateInventory:
		bar = m.inventory.FilterBar()
	case m.showsResultTabs():
		bar = m.findingsTable.FilterBar()
	}
	if bar == nil || !bar.IsVisible() {
		return nil
	}
	return bar
}

// showsResultTabs reports whether the findings tab bar is on screen. Warnings
// replace the table and hide the tabs — there are no partial results to browse.
func (m Model) showsResultTabs() bool {
	return m.state == StateResults && m.result != nil && len(m.result.Errors) == 0
}

// renderInfoLine is the footer's message line, always rendered even when empty
// (Rule 124). The message expires on its own after three seconds (Rule 128).
func (m Model) renderInfoLine(width int) string {
	return m.footer.View(width, sharedcomponents.Status{})
}

// renderTabs renders the tab bar
func (m Model) renderTabs() string {
	cve, secrets, licenses, misconfigs := m.countFindingsByTab()

	return theme.RenderTabs([]theme.TabItem{
		{Label: fmt.Sprintf("CVE (%d)", cve)},
		{Label: fmt.Sprintf("Secrets (%d)", secrets)},
		{Label: fmt.Sprintf("Licenses (%d)", licenses)},
		{Label: fmt.Sprintf("Misconfig (%d)", misconfigs)},
	}, m.activeTab)
}
