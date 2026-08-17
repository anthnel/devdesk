package netdiag

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// View implements tea.Model
func (m *Model) View() string {
	switch m.activeTab {
	case tabPorts:
		// The kill confirmation is centred over the tab, like every other
		// modal (Rule 112).
		if modal := m.portsModel.confirmModal; modal != nil {
			return lipgloss.Place(
				m.width, m.height,
				lipgloss.Center, lipgloss.Center,
				modal.View(),
				lipgloss.WithWhitespaceBackground(theme.ColorBackground),
			)
		}
		return m.portsModel.view()
	case tabTopology:
		return m.topologyModel.view()
	}
	switch m.state {
	case StateInput:
		return m.renderInputForm()
	case StateRunning:
		return m.renderRunning()
	case StateResults:
		return m.renderResults()
	case StateDetails:
		return m.detailsViewport.View()
	}
	return ""
}

func (m *Model) renderInputForm() string {
	var b strings.Builder

	// Target field
	b.WriteString(m.renderTextInputField("Target", m.targetInput, fieldTarget))
	b.WriteString("\n")

	// Port field
	b.WriteString(m.renderTextInputField("Port", m.portInput, fieldPort))
	b.WriteString("\n")

	// DNS Server field
	b.WriteString(m.renderTextInputField("DNS Server", m.dnsServerInput, fieldDNSServer))
	b.WriteString("\n\n")

	// Tests section
	b.WriteString(theme.SubTitleStyle.Render(theme.IconConfig + " Diagnostic Tests"))
	b.WriteString("\n\n")

	for _, t := range m.tests {
		b.WriteString(theme.RenderCheckbox(t.enabled, t.name, m.focusedField == t.fieldIdx))
		b.WriteString("\n")
	}

	// Start button
	b.WriteString("\n")
	b.WriteString("  " + theme.RenderButton("Run Diagnostics", m.focusedField == fieldButton, "primary"))

	return lipgloss.NewStyle().
		Padding(1).
		Background(theme.ColorBackground).
		Render(b.String())
}

func (m *Model) renderTextInputField(label string, input textinput.Model, fieldIdx int) string {
	prefix := "  "
	if m.focusedField == fieldIdx {
		prefix = theme.IconCircleSmall + " "
	}
	return theme.KeyStyle.Render(prefix+label+" "+theme.IconChevronRight+" ") + input.View()
}

func (m *Model) renderRunning() string {
	w := max(m.width-2, 20)

	var lines []string
	lines = append(lines, theme.EmptyLineBg(w))

	title := theme.SpinnerMessage(m.spinner.View(), "Running diagnostics...")
	lines = append(lines, theme.PadWithBg(theme.Bg("  ")+title, w))
	lines = append(lines, theme.EmptyLineBg(w))

	for _, name := range m.resultOrder {
		res := m.results[name]
		var label string
		switch {
		case !res.done:
			label = theme.Bg("  ") + theme.SpinnerMessage(m.spinner.View(), name)
		case res.cancelled:
			label = theme.Bg("  ") + theme.DimStyle.Render(theme.IconCanceled+" "+name)
		case res.success:
			label = theme.Bg("  ") + theme.StatusOKStyle.Render(theme.IconOK) + theme.Bg(" "+name)
		default:
			label = theme.Bg("  ") + theme.StatusErrorStyle.Render(theme.IconError) + theme.Bg(" "+name)
		}
		lines = append(lines, theme.PadWithBg(label, w))
	}

	lines = append(lines, theme.EmptyLineBg(w))
	prog := fmt.Sprintf("  %d / %d tests completed", m.doneTests, m.totalTests)
	lines = append(lines, theme.PadWithBg(theme.Bg(prog), w))

	return strings.Join(lines, "\n")
}

func (m *Model) renderResults() string {
	return m.resultsTable.View()
}

func (m *Model) renderDetailsContent(res testResult, width int) string {
	var lines []string
	lines = append(lines, theme.EmptyLineBg(width))

	output := res.output
	if output == "" {
		output = "(no output)"
	}

	switch {
	case !m.rawDetails && (res.name == "Traceroute" || res.name == "TCP Traceroute"):
		lines = append(lines, formatTracerouteOutput(output, width)...)
	case !m.rawDetails && (res.name == "DNS Resolution" || res.name == "Reverse DNS"):
		lines = append(lines, formatDNSOutput(output, width)...)
	default:
		lineStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText)
		for line := range strings.SplitSeq(output, "\n") {
			lines = append(lines, theme.PadWithBg(lineStyle.Render(line), width))
		}
	}

	return strings.Join(lines, "\n")
}

// resultColumns describes the diagnostics results table.
func resultColumns() []datatable.Column[testResult] {
	return []datatable.Column[testResult]{
		{
			Title: "Test", MinWidth: 22,
			Cell: func(r testResult) string { return r.name },
		},
		{
			Title: "Status", MinWidth: 6,
			// No Style: the icon already says which way the test went, and the
			// migration is meant to change nothing but the text colour.
			Cell: resultStatusCell,
		},
		{
			Title: "Output", MinWidth: 10, Flex: 1,
			Cell: func(r testResult) string { return firstOutputLine(r.output) },
		},
	}
}

// resultStatusCell is the status cell: an icon and a word, plain text (Rule 122).
func resultStatusCell(r testResult) string {
	switch {
	case r.cancelled:
		return theme.IconCanceled + " —"
	case r.success:
		return theme.IconOK + " OK"
	default:
		return theme.IconError + " FAIL"
	}
}

// rebuildResultsTable refills the results table in the order the tests were
// started. The table itself is built once, in New: rebuilding it here is what
// dropped the cursor back to the top on every update.
func (m *Model) rebuildResultsTable() {
	if m.state != StateResults {
		return
	}

	items := make([]testResult, 0, len(m.resultOrder))
	for _, name := range m.resultOrder {
		if res, ok := m.results[name]; ok {
			items = append(items, res)
		}
	}
	m.resultsTable.SetItems(items)
	m.resultsTable.Resize(m.width, max(m.height-13, 3))
}

func (m *Model) resizeInputs() {
	// Viewport border (2) + form padding (2) + prefix (2) + longest label "DNS Server " + chevron + spaces (~15)
	const labelOverhead = 21
	wide := max(m.width-labelOverhead, 20)
	m.targetInput.Width = wide
	m.dnsServerInput.Width = wide
	m.portInput.Width = 10
}

func (m *Model) resizeDetailsViewport() {
	// Header ~9 lines + tab footer 3 lines + viewport border 2 lines = 14 overhead
	m.detailsViewport = viewport.New(max(m.width-4, 20), max(m.height-13, 3))
	m.detailsViewport.Style = lipgloss.NewStyle().Background(theme.ColorBackground)
}

// firstOutputLine returns the first non-empty line of a test's output.
//
// It no longer truncates: the column width belongs to the renderer, and
// datatable measures and cuts on rune boundaries there (render.go). Truncating
// here meant the cell had to know a width the Cell function is not given, which
// is the coupling the migration removes.
func firstOutputLine(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
