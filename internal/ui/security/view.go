package security

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/scan"
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
	case StateInput:
		return m.renderInputView()
	case StateScanning:
		return m.renderScanningView()
	case StateResults:
		return m.renderResultsView()
	case StateDetails:
		return m.renderDetailsView()
	}
	return ""
}

// The two columns are built as independent lists of lines and only zipped
// together at the end, so the spacing of one is invisible in the other's
// rendering — "Scan Options" ran straight into its first checkbox for as long
// as it did because the line below it on screen belongs to the right column.
// Each column is therefore built where it can be read, and checked, on its own.

// formLeftColumn builds the target and scan-option lines.
func (m Model) formLeftColumn() []string {
	return []string{
		theme.SubTitleStyle.Render(theme.IconTarget + " Target"),
		"",
		m.renderTargetTypeField(),
		m.renderTargetPathField(),
		"",
		theme.SubTitleStyle.Render(theme.IconConfig + " Scan Options"),
		"",
		m.renderCheckbox(m.enableVuln, "Vulnerability Scan (Trivy)", 2),
		m.renderCheckbox(m.enableSecret, "Secret Scan (Gitleaks)", 3),
		m.renderCheckbox(m.enableMisconfig, "Misconfig Scan (Trivy)", 4),
		m.renderCheckbox(m.enableLicense, "License Scan (Trivy)", 5),
		m.renderCheckbox(m.generateSBOM, "Generate SBOM (CycloneDX)", 6),
	}
}

// formRightColumn builds the per-tool option lines.
func (m Model) formRightColumn() []string {
	right := []string{
		theme.SubTitleStyle.Render(theme.IconConfig + " Trivy Options"),
		"",
		m.renderAdvancedTextInput(theme.IconServer+" Server ", m.trivyServerInput, 7),
	}
	if m.isServerMode() {
		right = append(right, theme.DimStyle.Render("  Server mode: misconfig, license, SBOM unavailable"))
	}
	return append(right,
		m.renderCheckbox(m.ignoreUnfixed, "Ignore Unfixed", 8),
		m.renderCheckbox(m.ignoreEOL, "Ignore EOL", 9),
		"",
		theme.SubTitleStyle.Render(theme.IconConfig+" Gitleaks Options"),
		"",
		m.renderAdvancedTextInput(theme.IconToml+" Config ", m.gitleaksConfigInput, 10),
		m.renderCheckbox(m.gitleaksHistory, "Scan Git History", 11),
	)
}

// renderInputView renders the form in a 2-column layout with padding
func (m Model) renderInputView() string {
	var b strings.Builder

	// Error message
	if m.err != nil {
		b.WriteString(lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorError).Render("Error: "+m.err.Error()) + "\n\n")
	}

	left := m.formLeftColumn()
	right := m.formRightColumn()

	// Combine left and right columns line-by-line
	leftWidth := max(m.width/2-2, 40) // -2 for outer padding
	totalLines := max(len(left), len(right))
	for i := range totalLines {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		b.WriteString(theme.PadWithBg(l, leftWidth) + theme.Bg("    ") + r + "\n")
	}

	// Start button below both columns
	b.WriteString("\n")
	b.WriteString(" " + m.renderStartButton())

	return lipgloss.NewStyle().
		Padding(1).
		Background(theme.ColorBackground).
		Render(b.String())
}

// renderAdvancedTextInput renders a text input field for advanced options
func (m Model) renderAdvancedTextInput(label string, input textinput.Model, fieldIdx int) string {
	highlightStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorHighlight).Bold(true)
	if m.focusedField == fieldIdx {
		return highlightStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + input.View()
	}
	return theme.Bg("  "+label+theme.IconChevronRight+" ") + input.View()
}

// renderTargetTypeField renders target type selection
func (m Model) renderTargetTypeField() string {
	label := "Target Type " + theme.IconSelect + " "
	textStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText)
	value := textStyle.Render(m.targetType)

	highlightStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorHighlight).Bold(true)
	if m.focusedField == 0 {
		return highlightStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + value
	}
	return theme.Bg("  "+label+theme.IconChevronRight+" ") + value
}

// renderTargetPathField renders target path input
func (m Model) renderTargetPathField() string {
	label := "Target "
	highlightStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorHighlight).Bold(true)
	if m.focusedField == 1 {
		return highlightStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + m.targetInput.View()
	}
	return theme.Bg("  "+label+theme.IconChevronRight+" ") + m.targetInput.View()
}

// renderCheckbox renders a checkbox. When the field is incompatible with Trivy server mode
// and a server is configured, it renders as a locked/disabled option.
func (m Model) renderCheckbox(checked bool, label string, fieldIdx int) string {
	if m.isServerMode() && m.isServerIncompatibleField(fieldIdx) {
		return theme.RenderCheckboxDisabled(label)
	}
	return theme.RenderCheckbox(checked, label, m.focusedField == fieldIdx)
}

// renderStartButton renders the start scan button
func (m Model) renderStartButton() string {
	canStart := m.deps.TrivyAvailable || m.deps.GitleaksAvailable
	if !canStart {
		return theme.DimStyle.Render("[ No scanners available ]")
	}
	return " " + theme.RenderButton("Start Scan", m.focusedField == 12, "primary")
}

// renderScanningView renders the scanning progress with per-stage status rows.
func (m Model) renderScanningView() string {
	elapsed := time.Since(m.scanStartTime).Round(time.Second)

	var b strings.Builder
	b.WriteString(theme.DimStyle.Render(fmt.Sprintf("Scanning %s — elapsed: %s", m.targetPath, elapsed)))
	b.WriteString("\n\n")

	if len(m.scanStages) == 0 {
		b.WriteString(theme.SpinnerMessage(m.spinner.View(), "Initializing..."))
		b.WriteString("\n")
	} else {
		for _, stage := range m.scanStages {
			b.WriteString(m.renderScanStageRow(stage))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(theme.HelpStyle.Render("[esc] cancel scan"))

	return lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Padding(1).
		Render(b.String())
}

// renderScanStageRow renders a single stage row with icon, label, and optional detail.
// Each segment uses theme.Bg() for gaps to avoid black backgrounds (Rule 115).
// The icon column is fixed at width 2 to align spinner (1 cell) with Nerd Font icons (2 cells).
func (m Model) renderScanStageRow(stage scan.ProgressUpdate) string {
	var iconContent string
	switch stage.Status {
	case scan.StageDone:
		iconContent = theme.StatusOKStyle.Render(theme.IconOK) + theme.Bg(" ") // match spinner's built-in trailing space
	case scan.StageError:
		iconContent = theme.StatusErrorStyle.Render(theme.IconError) + theme.Bg(" ") // match spinner's built-in trailing space
	default: // StageRunning
		iconContent = m.spinner.View() // spinner frames (e.g. "⡿ ") already include a trailing space
	}

	labelStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Width(20)
	label := labelStyle.Render(stage.Label)

	row := theme.Bg("  ") + iconContent + theme.Bg("  ") + label
	if stage.Detail != "" {
		row += theme.Bg("  ") + theme.DimStyle.Render(truncateScanDetail(stage.Detail))
	}
	return row
}

// truncateScanDetail extracts the most meaningful part of a trivy/gitleaks stderr line
// and truncates it to fit on one line.
func truncateScanDetail(line string) string {
	// Try to extract the message from a structured log line (timestamp + level + msg)
	// Format: "2006-01-02T... LEVEL  message"
	if len(line) > 10 && line[4] == '-' && line[7] == '-' {
		// Skip timestamp
		rest := line
		if idx := strings.IndexAny(rest, " \t"); idx >= 0 {
			rest = strings.TrimLeft(rest[idx:], " \t")
			// Skip level
			if idx2 := strings.IndexAny(rest, " \t"); idx2 >= 0 {
				msg := strings.TrimLeft(rest[idx2:], " \t")
				if msg != "" {
					line = msg
				}
			}
		}
	}
	// Skip download progress bar lines
	if strings.Contains(line, " MiB /") || strings.Contains(line, " p/s ") {
		return ""
	}
	// Skip all trivy component log messages (e.g. "[vuln] ...", "[checks-client] ...")
	if strings.HasPrefix(line, "[") {
		return ""
	}
	const maxLen = 55
	if len(line) > maxLen {
		return line[:maxLen-3] + "..."
	}
	return line
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
	if m.statusMessage == "" || m.state == StateInput || m.state == StateScanning {
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
