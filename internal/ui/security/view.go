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

	// A stage that failed is **not** rendered here. It goes to the log, and the
	// footer says to look there (Rule 128).
	//
	// It used to be a panel that replaced the whole table whenever
	// Result.Errors was non-empty, on the stated grounds that a failed scan has
	// no partial results to browse. That held while an error meant nothing had
	// run; it stopped holding the day a stage could fail beside three that
	// succeeded, and a plumber failure then took every CVE and every secret
	// down with it. Folding it to a banner above the table only moved the
	// problem — five tabs, permanently, for a message read once — and a tool's
	// stderr reformatted for a viewport is what a log is for.
	//
	// Nothing is lost by it: internal/scan logs every stage failure at the
	// point it records one, which it did not do before. The only copy of the
	// reason used to be on screen.
	//
	// The grade had a line of its own above the table on the CI tab, and it is
	// gone: it cost that tab two of its rows — the line and its blank — on
	// every open, to state a letter the inventory's own CI column carries per
	// target. A tab that shows fewer findings than its four neighbours, for a
	// value already on the previous screen, is not a trade worth making.
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
// A failed stage no longer takes it away, because nothing replaces the table
// any more: the tabs are what the results state is browsed with, and an empty
// one is an honest answer — the footer says which stage failed and where to
// read why.
func (m Model) showsResultTabs() bool {
	return m.state == StateResults && m.result != nil
}

// renderInfoLine is the footer's message line, always rendered even when empty
// (Rule 124). The message expires on its own after three seconds (Rule 128).
func (m Model) renderInfoLine(width int) string {
	return m.footer.View(width, m.status())
}

// status is what the footer says when no message is in flight.
//
// A stage that failed is a **state** of the result rather than an event: it is
// true for as long as the result is on screen, and a message would expire after
// three seconds — the distinction Rule 128 draws between the two. It carries
// the error level, because that is what happened, and it names the stages so
// the log can be searched for them.
func (m Model) status() sharedcomponents.Status {
	if m.state != StateResults || m.result == nil || len(m.result.Errors) == 0 {
		return sharedcomponents.Status{}
	}
	return sharedcomponents.Status{
		Text:  failedStages(m.result.Errors) + " — check logs",
		Level: sharedcomponents.LevelError,
	}
}

// failedStages names what failed, reading the "<stage>: <reason>" that
// scan.recordStageError writes. The reason stays in the log: a footer is one
// line, and a tool's stderr is not.
func failedStages(errs []string) string {
	stages := make([]string, 0, len(errs))
	for _, e := range errs {
		stage := e
		if idx := strings.Index(e, ": "); idx != -1 {
			stage = e[:idx]
		}
		stages = append(stages, stage)
	}
	return strings.Join(stages, ", ") + " failed"
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
