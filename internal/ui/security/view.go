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

	// Warnings replace the table only when there is nothing to browse.
	//
	// They used to replace it whenever there were any, on the stated grounds of
	// "no partial results to browse". That was true while an error meant the
	// scan had produced nothing; it stopped being true the day a stage could
	// fail beside three that succeeded. A plumber failure then hid every CVE
	// and every secret the same scan had found — findings that exist and are
	// invisible, which is the worst way for a result to be wrong.
	//
	// The status message used to be repeated here, under the panel. The footer
	// already carries it, and a message printed twice is a message the two
	// copies can disagree about (Rule 134's reasoning, one layer down).
	if len(m.result.Errors) > 0 && m.result.TotalFindings() == 0 {
		return m.renderWarningsPanel()
	}

	// What goes above the table. The warnings when a stage failed beside others
	// that did not — that is about the scan rather than about the tab being
	// read, so it shows on all five. The grade is not a finding either, so it
	// has no row, but it belongs to one tab: the header would show it on all
	// five, and a withheld run's reason is a sentence, which buildInfoLines
	// cannot carry (it aligns short values on seven lines and drops the eighth
	// in silence).
	var head []string
	if len(m.result.Errors) > 0 {
		head = append(head, m.renderWarningsPanel(), theme.EmptyLineBg(m.width))
	}
	if m.activeTab == TabCIScore {
		head = append(head, m.renderCIScoreLine(m.width), theme.EmptyLineBg(m.width))
	}
	if len(head) == 0 {
		return m.findingsTable.View()
	}
	return strings.Join(append(head, m.findingsTable.View()), "\n")
}

// resultsHeadLines is what the head above the table costs it.
//
// The warnings part is measured from the rendered text rather than counted by
// hand: the panel wraps, so its height depends on the terminal width and on how
// much the tool had to say. A constant would be right at one width and wrong at
// every other.
func (m Model) resultsHeadLines() int {
	if m.result == nil {
		return 0
	}
	lines := 0
	if len(m.result.Errors) > 0 && m.result.TotalFindings() > 0 {
		lines += strings.Count(m.renderWarningsPanel(), "\n") + 1
	}
	if m.activeTab == TabCIScore {
		lines += ciScoreHeadLines
	}
	return lines
}

// ciScoreHeadLines is what the head costs the table on the CI tab: the line and
// the blank under it.
const ciScoreHeadLines = 2

// renderCIScoreLine states the grade, or why there is none.
//
// Rule 120's separator, never a colon. The letter is deliberately absent on a
// withheld run: plumber writes one anyway and it flatters — a control that did
// not run found nothing — so what is printed is the reason instead.
func (m Model) renderCIScoreLine(width int) string {
	label := theme.KeyStyle.Render(theme.Bg("Score " + theme.IconChevronRight + " "))

	switch {
	case m.result == nil || !m.result.CIScanned:
		return theme.BgLine(theme.Bg("  ")+label+theme.DimStyle.Render("not graded"), width)
	case m.result.CIMissing:
		return theme.BgLine(theme.Bg("  ")+label+theme.DimStyle.Render("no pipeline in this repository"), width)
	case m.result.CIWithheld:
		reason := "the analysis ran on incomplete data"
		if len(m.result.CIReasons) > 0 {
			reason = strings.Join(m.result.CIReasons, "; ")
		}
		return theme.BgLine(theme.Bg("  ")+label+theme.DimStyle.Render("withheld — "+reason), width)
	default:
		grade := theme.CIScoreStyle(theme.CIScoreGraded, &m.result.CIScore).
			Render(fmt.Sprintf("%s · %d/100", m.result.CIScore, m.result.CIPoints))
		return theme.BgLine(theme.Bg("  ")+label+grade, width)
	}
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
//
// The bar comes first whatever the state, because its top edge *is* the
// viewport's bottom border (Rule 136) — the same order oci_resources uses for
// the one bar it draws above a tab bar.
//
// It is resolved through activeFilterBar, like the height. The results branch
// drew no bar at all while GetFooterHeight counted one, so the router took two
// lines off the viewport and nothing filled them: the table's bottom border
// climbed the moment a severity token went on, and the tokens the user had just
// switched on were nowhere on screen.
func (m Model) RenderFooter(width int) string {
	var parts []string
	if bar := m.activeFilterBar(); bar != nil {
		parts = append(parts, bar.View())
	}
	if m.showsResultTabs() {
		parts = append(parts, theme.PadWithBg(theme.Bg(" ")+m.renderTabs(), width))
	}
	parts = append(parts, theme.EmptyLineBg(width), m.renderInfoLine(width))
	return strings.Join(parts, "\n")
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

// showsResultTabs reports whether the findings tab bar is on screen.
//
// It follows the table: hidden when the warnings have replaced it — there is
// nothing to browse and the tabs would suggest otherwise — and shown as soon as
// some stage produced findings, failure or no failure.
func (m Model) showsResultTabs() bool {
	return m.state == StateResults && m.result != nil &&
		(len(m.result.Errors) == 0 || m.result.TotalFindings() > 0)
}

// renderInfoLine is the footer's message line, always rendered even when empty
// (Rule 124). The message expires on its own after three seconds (Rule 128).
func (m Model) renderInfoLine(width int) string {
	return m.footer.View(width, sharedcomponents.Status{})
}

// renderTabs renders the tab bar
func (m Model) renderTabs() string {
	cve, secrets, licenses, misconfigs, ci := m.countFindingsByTab()

	return theme.RenderTabs([]theme.TabItem{
		{Label: fmt.Sprintf("CVE (%d)", cve)},
		{Label: fmt.Sprintf("Secrets (%d)", secrets)},
		{Label: fmt.Sprintf("Licenses (%d)", licenses)},
		{Label: fmt.Sprintf("Misconfig (%d)", misconfigs)},
		// The count, like its four neighbours — not the letter. A letter in a
		// column of counts would break the only thing the tab bar keeps
		// aligned, and the grade already has a line of its own below.
		{Label: fmt.Sprintf("CI (%d)", ci)},
	}, m.activeTab)
}
