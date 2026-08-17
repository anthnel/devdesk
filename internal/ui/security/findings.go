package security

import (
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// numColumns is the number of columns in the findings table
const numColumns = 4

// findingColumns describes the findings table.
//
// Nothing sorts and nothing searches: the severity order is the one the scanner
// reported and `.` is the severity *filter* on this view, not Rule 111's sort.
// The Title is not pre-truncated either — bubbles cuts every cell to its column
// width with the same ellipsis, and doing it by hand at width-3 first only cost
// three characters of title.
func findingColumns() []datatable.Column[scan.Finding] {
	return []datatable.Column[scan.Finding]{
		{
			Title: "Severity", MinWidth: 10,
			Cell: func(f scan.Finding) string { return string(f.Severity) },
			// The same palette the selected row uses, so a severity reads the
			// same colour whether or not the cursor is on it.
			Style: func(f scan.Finding) lipgloss.Style { return theme.SeverityTextStyle(string(f.Severity)) },
		},
		{Title: "ID", MinWidth: 18, Cell: func(f scan.Finding) string { return f.ID }},
		{Title: "Title", MinWidth: 20, Flex: 1, Cell: func(f scan.Finding) string { return f.Title }},
		{Title: "Source", MinWidth: 14, Cell: sourceDisplay},
	}
}

// findingSelectedStyles colours the selected row by the severity under the
// cursor. It is what refreshSelectionStyle did, minus its own bounds check.
func findingSelectedStyles(f scan.Finding) table.Styles {
	return theme.TableStylesForSeverity(string(f.Severity))
}

// Tab constants for results view
const (
	TabCVE       = 0
	TabSecrets   = 1
	TabLicense   = 2
	TabMisconfig = 3
)

// updateFindingsTable populates the findings table based on active tab and filters.
//
// The tab is this view's own filter, not the component's: it selects which
// findings exist at all, where a FilterBar query narrows a list that is already
// settled. The severity went the other way — it is four cumulative tokens the
// table owns (Rule 136), which is what gave `.` back to the sort.
func (m *Model) updateFindingsTable() {
	if m.result == nil {
		return
	}

	m.findingsTable.SetItems(m.filterFindingsByTab())
	// A change of tab or severity is a change of scope, not a shorter list, so
	// the cursor goes back to the top. SetItems deliberately leaves it alone.
	m.findingsTable.GotoTop()
}

// tabCategory maps a tab to the finding category it shows.
//
// The tabs and scan.Result's counters are now the same classification, applied
// once in scan.Categorize: they were two rules that disagreed on three inputs,
// each of which produced a finding counted in the header but present in no tab
// at all — visible nowhere, which is the worst way for a finding to be wrong.
var tabCategory = map[int]scan.Category{
	TabCVE:       scan.CategoryVulnerability,
	TabSecrets:   scan.CategorySecret,
	TabLicense:   scan.CategoryLicense,
	TabMisconfig: scan.CategoryMisconfiguration,
}

// filterFindingsByTab returns findings filtered by the active tab
func (m *Model) filterFindingsByTab() []scan.Finding {
	if m.result == nil {
		return nil
	}

	want, ok := tabCategory[m.activeTab]
	if !ok {
		return nil
	}
	var filtered []scan.Finding
	for _, f := range m.result.Findings {
		if scan.Categorize(f) == want {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

// sourceDisplay returns a display string for the source column.
//
// Which tool found it, not which category it is — the tab already says the
// category, and telling gitleaks from trivy-secret in the Secrets tab is the
// point of showing both there.
func sourceDisplay(f scan.Finding) string {
	switch f.Source {
	case scan.SourceTrivyLicense:
		return "license"
	case scan.SourceTrivyMisconfig:
		return "misconfig"
	case scan.SourceGitleaks:
		return "gitleaks"
	case scan.SourceTrivySecret:
		return "trivy"
	case scan.SourceTrivy:
		return "vuln"
	default:
		return f.Source
	}
}

// countFindingsByTab returns the count of findings for each tab. These are the
// numbers on the tab labels, and scan.Result's counters are the same ones.
func (m *Model) countFindingsByTab() (cve, secrets, licenses, misconfigs int) {
	if m.result == nil {
		return 0, 0, 0, 0
	}

	for _, f := range m.result.Findings {
		switch scan.Categorize(f) {
		case scan.CategoryVulnerability:
			cve++
		case scan.CategorySecret:
			secrets++
		case scan.CategoryLicense:
			licenses++
		case scan.CategoryMisconfiguration:
			misconfigs++
		}
	}
	return cve, secrets, licenses, misconfigs
}

// switchTab switches to the given tab index and refreshes the table
func (m *Model) switchTab(tab int) {
	m.activeTab = tab
	m.statusMessage = ""
	m.updateFindingsTable()
}

// handleIgnoreSecret prompts confirmation to ignore a secret finding.
//
// Gitleaks findings only. .gitleaksignore is matched on a Gitleaks fingerprint,
// which a Trivy secret does not have — AddToGitleaksIgnore would fabricate one
// from the file and rule, write it, and report success for a line Gitleaks will
// never match and Trivy never reads. Refusing says so instead.
func (m Model) handleIgnoreSecret() (tea.Model, tea.Cmd) {
	finding, ok := m.findingsTable.Selected()
	if m.activeTab != TabSecrets || !ok {
		return m, nil
	}
	if finding.Source != scan.SourceGitleaks {
		m.statusMessage = "Only Gitleaks findings can be added to .gitleaksignore"
		return m, clearStatusCmd()
	}
	m.findingToIgnore = &finding
	m.confirmModal = sharedcomponents.NewConfirmModal(
		"Ignore Secret",
		fmt.Sprintf("Add this secret to .gitleaksignore?\n\nFile: %s\nRule: %s", finding.File, finding.ID),
	)
	return m, nil
}

// handleResultsState processes input in results state
func (m Model) handleResultsState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// No origin view means this view was opened directly, so esc stays
		// inside it and goes back to whichever landing state it opened on.
		if m.OriginView == "" {
			return m.goHome()
		}
		return m, func() tea.Msg { return BackToOriginMsg{Origin: m.OriginView} }
	case "enter":
		if finding, ok := m.findingsTable.Selected(); ok {
			// The details view is about a finding, not about a row index — an
			// index would name a different one the moment the list changed
			// under it, which is how workspaces got D24.
			m.selectedFinding = &finding
			m.state = StateDetails
			m.detailsViewport.Width = m.width
			m.detailsViewport.Height = m.height
			m.detailsViewport.SetContent(m.buildDetailsContent())
			m.detailsViewport.GotoTop()
		}
		return m, nil
	case "ctrl+r":
		m.activeTab = TabCVE
		return m.goHome()
	case "tab":
		m.switchTab((m.activeTab + 1) % 4)
		return m, nil
	case "shift+tab":
		m.switchTab((m.activeTab + 3) % 4)
		return m, nil
	// 1-4 jumped straight to a tab. They were the application's only numeric
	// bindings, and an exception in a single view is precisely what §3.26
	// dismantles — tab reaches all four.
	case "c", "h", "m", "l":
		return m.toggleSeverity(msg.String())
	case keymap.Exclude:
		return m.handleIgnoreSecret()
	}
	// `.` is the sort again, and the search and the severity tokens are the
	// table's (Rule 136).
	return m, m.findingsTable.Update(msg)
}

// toggleSeverity flips one severity token. Cumulative: c and h together ask for
// "CRITICAL or HIGH", which is the question a threshold cycle could not put.
func (m Model) toggleSeverity(key string) (tea.Model, tea.Cmd) {
	label := map[string]string{"c": "critical", "h": "high", "m": "medium", "l": "low"}[key]
	m.findingsTable.SetTokenActive(label, !m.findingsTable.IsTokenActive(label))
	m.findingsTable.GotoTop()
	return m, nil
}
