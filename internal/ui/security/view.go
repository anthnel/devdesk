package security

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// View renders the UI
func (m Model) View() string {
	// Show confirm modal if active
	if m.confirmModal != nil {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.confirmModal.View(),
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

	var b strings.Builder

	// Warnings replace the table and hide the tab bar (no partial results to browse)
	if len(m.result.Errors) > 0 {
		b.WriteString(m.renderWarningsPanel())
		if m.statusMessage != "" {
			b.WriteString("\n")
			b.WriteString(theme.StatusOKStyle.Render(m.statusMessage))
		}
		return b.String()
	}

	b.WriteString(m.findingsTable.View())

	return b.String()
}

// GetFooterHeight returns the footer height for this view (Rule 124).
func (m Model) GetFooterHeight() int {
	if m.showsResultTabs() {
		return 3 // tab bar + empty line + info line
	}
	if m.state == StateInventory {
		return 2 + m.inventory.FilterBar().ExtraHeight() // Rule 136
	}
	return 2 // empty line + info line
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

// FilterBarVisible reports whether the filter bar is on screen, which is what
// closes the viewport's bottom border around it (implements app.FilterBarView,
// Rule 136). Only the inventory has one — the findings table filters by tab and
// severity, not by query.
func (m Model) FilterBarVisible() bool {
	return m.state == StateInventory && m.inventory.FilterBar().IsVisible()
}

// showsResultTabs reports whether the findings tab bar is on screen. Warnings
// replace the table and hide the tabs — there are no partial results to browse.
func (m Model) showsResultTabs() bool {
	return m.state == StateResults && m.result != nil && len(m.result.Errors) == 0
}

// renderInfoLine is the footer's message line, always rendered even when empty
// (Rule 124). The message expires on its own after three seconds (Rule 128).
func (m Model) renderInfoLine(width int) string {
	if m.statusMessage == "" {
		return theme.EmptyLineBg(width)
	}
	return theme.PadWithBg(theme.StatusOKStyle.Render(m.statusMessage), width)
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
