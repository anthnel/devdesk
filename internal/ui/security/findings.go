package security

import (
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Tab constants for results view
const (
	TabCVE       = 0
	TabSecrets   = 1
	TabLicense   = 2
	TabMisconfig = 3
)

// updateFindingsTable populates the findings table based on active tab and filters
func (m *Model) updateFindingsTable() {
	if m.result == nil {
		return
	}

	// Filter findings by tab type
	m.filteredFindings = m.filterFindingsByTab()

	// Apply severity filter (for CVE, License, and Misconfig tabs)
	if (m.activeTab == TabCVE || m.activeTab == TabLicense || m.activeTab == TabMisconfig) && m.severityFilter != "all" {
		m.filteredFindings = m.filterFindingsBySeverity(m.filteredFindings)
	}

	// Calculate dynamic column widths based on available width
	columns := m.calculateColumns()
	m.findingsTable.SetColumns(columns)

	// Build rows from filtered findings
	rows := make([]table.Row, 0, len(m.filteredFindings))
	titleWidth := m.getTitleColumnWidth()
	for _, f := range m.filteredFindings {
		rows = append(rows, table.Row{
			string(f.Severity),
			f.ID,
			theme.TruncateWidth(f.Title, titleWidth-3),
			m.getSourceDisplay(f),
		})
	}
	m.findingsTable.SetRows(rows)
	// Reset cursor to first row when data changes
	m.findingsTable.GotoTop()
	m.refreshSelectionStyle()
}

// filterFindingsByTab returns findings filtered by the active tab
func (m *Model) filterFindingsByTab() []scan.Finding {
	if m.result == nil {
		return nil
	}

	var filtered []scan.Finding
	for _, f := range m.result.Findings {
		switch m.activeTab {
		case TabCVE:
			// CVE: vulnerabilities from trivy (not secrets, not licenses)
			if f.Source == "trivy" && f.PkgName != "" && f.Match == "" {
				filtered = append(filtered, f)
			}
		case TabSecrets:
			// Secrets: from gitleaks or trivy secrets
			if f.Source == "gitleaks" || (f.Source == "trivy" && f.Match != "") {
				filtered = append(filtered, f)
			}
		case TabLicense:
			// Licenses: from trivy-license source
			if f.Source == "trivy-license" {
				filtered = append(filtered, f)
			}
		case TabMisconfig:
			// Misconfigurations: from trivy-misconfig source
			if f.Source == "trivy-misconfig" {
				filtered = append(filtered, f)
			}
		}
	}
	return filtered
}

// filterFindingsBySeverity filters findings by selected severity level
func (m *Model) filterFindingsBySeverity(findings []scan.Finding) []scan.Finding {
	if m.severityFilter == "all" {
		return findings
	}

	var filtered []scan.Finding
	for _, f := range findings {
		switch m.severityFilter {
		case "critical":
			if f.Severity == scan.SeverityCritical {
				filtered = append(filtered, f)
			}
		case "high":
			if f.Severity == scan.SeverityCritical || f.Severity == scan.SeverityHigh {
				filtered = append(filtered, f)
			}
		case "medium":
			if f.Severity == scan.SeverityCritical || f.Severity == scan.SeverityHigh || f.Severity == scan.SeverityMedium {
				filtered = append(filtered, f)
			}
		case "low":
			filtered = append(filtered, f)
		}
	}
	return filtered
}

// calculateColumns returns table columns with dynamic widths
func (m *Model) calculateColumns() []table.Column {
	// Minimum widths
	severityWidth := 10
	idWidth := 18
	sourceWidth := 14

	// Calculate title width using remaining space
	titleWidth := m.getTitleColumnWidth()

	return []table.Column{
		{Title: "Severity", Width: severityWidth},
		{Title: "ID", Width: idWidth},
		{Title: "Title", Width: titleWidth},
		{Title: "Source", Width: sourceWidth},
	}
}

// getTitleColumnWidth calculates the title column width based on terminal width
func (m *Model) getTitleColumnWidth() int {
	// Fixed widths: severity(10) + id(18) + source(14)
	// Overhead: viewport borders(2) + cell padding(4 columns × 2 = 8) = 10
	fixedWidth := 10 + 18 + 14 + 10
	titleWidth := max(m.width-fixedWidth, 20)
	return titleWidth
}

// getSourceDisplay returns a display string for the source column
func (m *Model) getSourceDisplay(f scan.Finding) string {
	switch f.Source {
	case "trivy-license":
		return "license"
	case "trivy-misconfig":
		return "misconfig"
	case "gitleaks":
		return "secret"
	case "trivy":
		if f.Match != "" {
			return "secret"
		}
		return "vuln"
	default:
		return f.Source
	}
}

// countFindingsByTab returns the count of findings for each tab
func (m *Model) countFindingsByTab() (cve, secrets, licenses, misconfigs int) {
	if m.result == nil {
		return 0, 0, 0, 0
	}

	for _, f := range m.result.Findings {
		switch {
		case f.Source == "trivy" && f.PkgName != "" && f.Match == "":
			cve++
		case f.Source == "gitleaks" || (f.Source == "trivy" && f.Match != ""):
			secrets++
		case f.Source == "trivy-license":
			licenses++
		case f.Source == "trivy-misconfig":
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

// handleIgnoreSecret prompts confirmation to ignore a secret finding
func (m *Model) handleIgnoreSecret() {
	if m.activeTab != TabSecrets || len(m.filteredFindings) == 0 {
		return
	}
	idx := m.findingsTable.Cursor()
	if idx < len(m.filteredFindings) {
		finding := m.filteredFindings[idx]
		m.findingToIgnore = &finding
		m.confirmModal = sharedcomponents.NewConfirmModal(
			"Ignore Secret",
			fmt.Sprintf("Add this secret to .gitleaksignore?\n\nFile: %s\nRule: %s", finding.File, finding.ID),
		)
	}
}

// handleResultsState processes input in results state
func (m Model) handleResultsState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// If no origin view is set (opened directly), go back to the scan form
		if m.OriginView == "" {
			m.state = StateInput
			return m, nil
		}
		return m, func() tea.Msg { return BackToOriginMsg{Origin: m.OriginView} }
	case "enter":
		if len(m.filteredFindings) > 0 {
			m.selectedIdx = m.findingsTable.Cursor()
			m.state = StateDetails
			m.detailsViewport.Width = m.width
			m.detailsViewport.Height = m.height
			m.detailsViewport.SetContent(m.buildDetailsContent())
			m.detailsViewport.GotoTop()
		}
		return m, nil
	case "ctrl+r":
		m.state = StateInput
		m.activeTab = TabCVE
		m.severityFilter = "all"
		return m, nil
	case "tab":
		m.switchTab((m.activeTab + 1) % 4)
		return m, nil
	case "shift+tab":
		m.switchTab((m.activeTab + 3) % 4)
		return m, nil
	case "1":
		m.switchTab(TabCVE)
		return m, nil
	case "2":
		m.switchTab(TabSecrets)
		return m, nil
	case "3":
		m.switchTab(TabLicense)
		return m, nil
	case "4":
		m.switchTab(TabMisconfig)
		return m, nil
	case ".":
		if m.activeTab == TabCVE || m.activeTab == TabLicense || m.activeTab == TabMisconfig {
			m.cycleSeverityFilter()
			m.updateFindingsTable()
		}
		return m, nil
	case "i":
		m.handleIgnoreSecret()
		return m, nil
	case "up", "down", "k", "j":
		var cmd tea.Cmd
		m.findingsTable, cmd = m.findingsTable.Update(msg)
		m.refreshSelectionStyle()
		return m, cmd
	case "g", "home":
		m.findingsTable.GotoTop()
		m.refreshSelectionStyle()
		return m, nil
	case "G", "end":
		m.findingsTable.GotoBottom()
		m.refreshSelectionStyle()
		return m, nil
	}
	return m, nil
}

// cycleSeverityFilter cycles through severity filter options
func (m *Model) cycleSeverityFilter() {
	filters := []string{"all", "critical", "high", "medium", "low"}
	for i, f := range filters {
		if f == m.severityFilter {
			m.severityFilter = filters[(i+1)%len(filters)]
			return
		}
	}
	m.severityFilter = "all"
}

// refreshSelectionStyle updates the table selection color to match the currently selected row's severity
func (m *Model) refreshSelectionStyle() {
	if len(m.filteredFindings) == 0 {
		return
	}
	cursor := m.findingsTable.Cursor()
	if cursor >= 0 && cursor < len(m.filteredFindings) {
		m.findingsTable.SetStyles(theme.TableStylesForSeverity(string(m.filteredFindings[cursor].Severity)))
	}
}
