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
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// findingColumns describes the findings table.
//
// The table opens on the order the scanner reported — `SortColumn: -1`, which
// datatable keeps as a stop on the cycle, so `.` always leads back to it. Every
// column sorts, because `.` was handed back to the sort when the severity floor
// became four tokens (§3.26) and then no column was ever given a `Less`:
// CycleSort returned on its first line and the header advertised a key that did
// nothing at all.
//
// Three columns search, and that is not optional either. Declaring tokens makes
// `Searchable()` true, so `/` already opened a query — against no searchable
// column, which matches nothing and empties the table. The bar's own left half
// says `/ search...` whether or not anything answers it.
//
// Severity searches nothing on purpose: c/h/m/l are the severity filter, and a
// second way to ask one question is what this codebase spends its time
// removing. The Title is not pre-truncated either — bubbles cuts every cell to
// its column width with the same ellipsis, and doing it by hand at width-3
// first only cost three characters of title.
func findingColumns() []datatable.Column[scan.Finding] {
	return []datatable.Column[scan.Finding]{
		{
			Title: "Severity", Sizing: datatable.SizingFixed, MinWidth: 10,
			Cell: func(f scan.Finding) string { return string(f.Severity) },
			// The same palette the selected row uses, so a severity reads the
			// same colour whether or not the cursor is on it.
			Style: func(f scan.Finding) lipgloss.Style { return theme.SeverityTextStyle(string(f.Severity)) },
			Less:  func(a, b scan.Finding) bool { return severityRank(a.Severity) < severityRank(b.Severity) },
		},
		{
			Title: "ID", Sizing: datatable.SizingContent, MinWidth: 18,
			Cell:   func(f scan.Finding) string { return f.ID },
			Less:   func(a, b scan.Finding) bool { return a.ID < b.ID },
			Search: func(f scan.Finding) string { return f.ID },
		},
		{
			Title: "Title", Sizing: datatable.SizingContent, MinWidth: 20, Flex: 1,
			Cell:   func(f scan.Finding) string { return f.Title },
			Less:   func(a, b scan.Finding) bool { return a.Title < b.Title },
			Search: func(f scan.Finding) string { return f.Title + " " + f.PkgName + " " + f.File },
		},
		{
			Title: "Source", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 14,
			Cell:   sourceDisplay,
			Less:   func(a, b scan.Finding) bool { return sourceDisplay(a) < sourceDisplay(b) },
			Search: sourceDisplay,
		},
	}
}

// severityRank orders the levels by how bad they are, which is the only order
// worth sorting this column by: alphabetically, CRITICAL lands between no two
// levels it belongs with — HIGH, LOW, MEDIUM — so a descending sort would put
// MEDIUM at the top and bury the finding the user opened the view for.
//
// Ascending is least-severe-first, so `.`'s second press (descending, Rule 111)
// is the one that answers "what is worst here".
//
// UNKNOWN ranks below LOW rather than above CRITICAL: it is what a scanner
// says when it has no score, not a claim that something is worse than critical.
// It has no token either, for the same reason — see severityToken.
func severityRank(s scan.SeverityLevel) int {
	switch s {
	case scan.SeverityCritical:
		return 4
	case scan.SeverityHigh:
		return 3
	case scan.SeverityMedium:
		return 2
	case scan.SeverityLow:
		return 1
	default:
		return 0
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
	TabCIScore   = 4
	tabCount     = 5
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
	// the cursor goes back to the top and the columns are measured again.
	// SetItems deliberately does neither on its own.
	m.findingsTable.GotoTop()
	m.findingsTable.Remeasure()
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
	TabCIScore:   scan.CategoryCIScore,
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
func (m *Model) countFindingsByTab() (cve, secrets, licenses, misconfigs, ci int) {
	if m.result == nil {
		return 0, 0, 0, 0, 0
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
		case scan.CategoryCIScore:
			ci++
		}
	}
	return cve, secrets, licenses, misconfigs, ci
}

// switchTab switches to the given tab index and refreshes the table
func (m *Model) switchTab(tab int) {
	m.activeTab = tab
	// The CI tab carries a score line above the table, so the height changes
	// with the tab and not only with the window.
	m.resizeFindings()
	m.footer.Clear()
	m.updateFindingsTable()
}

// handleIgnoreSecret prompts confirmation to ignore a secret finding.
//
// Gitleaks findings only. .gitleaksignore is matched on a Gitleaks fingerprint,
// which a Trivy secret does not have — AddToGitleaksIgnore would fabricate one
// from the file and rule, write it, and report success for a line Gitleaks will
// never match and Trivy never reads. Refusing says so instead.
func (m Model) handleIgnoreSecret() (tea.Model, tea.Cmd) {
	if exclude := m.canExclude(); !exclude.Enabled() {
		return m, m.footer.Warn(exclude.Reason)
	}
	finding, _ := m.findingsTable.Selected()
	m.findingToIgnore = &finding
	m.confirmModal = sharedcomponents.NewConfirmModal(
		"Ignore Secret",
		fmt.Sprintf("Add this secret to .gitleaksignore?\n\nFile: %s\nRule: %s", finding.File, finding.ID),
	)
	return m, nil
}

// The two findings actions, and why each does not apply (Rule 130).
const (
	reasonNoFinding    = "No finding selected"
	reasonNotASecret   = ".gitleaksignore only holds secrets — open the Secrets tab"
	reasonNotGitleaks  = "Only Gitleaks findings can be added to .gitleaksignore"
	reasonNothingToSee = "No finding to open"
)

// canOpenFinding reports whether enter has a row to detail.
func (m Model) canOpenFinding() shortcut.Availability {
	if _, ok := m.findingsTable.Selected(); !ok {
		return shortcut.Unavailable(reasonNothingToSee)
	}
	return shortcut.Availability{}
}

// canExclude reports whether X applies to the selected finding.
//
// .gitleaksignore is matched on a Gitleaks fingerprint, which a Trivy secret
// does not have — fabricating one would report success for a line nothing will
// ever match. The tab is checked first because it is the coarser answer: on
// the CVE tab the key means nothing at all, whatever the row.
func (m Model) canExclude() shortcut.Availability {
	if m.activeTab != TabSecrets {
		return shortcut.Unavailable(reasonNotASecret)
	}
	finding, ok := m.findingsTable.Selected()
	switch {
	case !ok:
		return shortcut.Unavailable(reasonNoFinding)
	case finding.Source != scan.SourceGitleaks:
		return shortcut.Unavailable(reasonNotGitleaks)
	}
	return shortcut.Availability{}
}

// handleResultsState processes input in results state.
//
// The search answers first, and every key below is unreachable while it has the
// keyboard: "c" is a character in a query, "esc" cancels the search rather than
// leaving the results, and "enter" confirms it rather than opening a finding.
// The inventory has had this guard since it gained a bar; this state gained one
// the day the severity floor became tokens, and never got the guard with it.
func (m Model) handleResultsState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.findingsTable.InEditMode() {
		return m, m.findingsTable.Update(msg)
	}
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
		m.switchTab((m.activeTab + 1) % tabCount)
		return m, nil
	case "shift+tab":
		m.switchTab((m.activeTab + tabCount - 1) % tabCount)
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

// resizeFindings lays the findings table out. Every tab gets the whole height:
// the CI tab used to give two rows to a score line above the table, and losing
// two findings on one tab out of five — for a letter the inventory already
// carries per target — was not worth the jump on every tab switch.
func (m *Model) resizeFindings() {
	m.findingsTable.Resize(m.width, max(m.height, 5))
}
