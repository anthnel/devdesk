package netdiag

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// View implements tea.Model
func (m *Model) View() string {
	switch m.activeTab {
	case tabPorts:
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

func (m *Model) rebuildResultsTable() {
	if m.state != StateResults {
		return
	}

	w := max(m.width-2, 30)

	numCols := 3
	available := w - numCols*2
	col1W := 22
	col2W := 6
	col3W := max(available-col1W-col2W, 10)

	cols := []table.Column{
		{Title: "Test", Width: col1W},
		{Title: "Status", Width: col2W},
		{Title: "Output", Width: col3W},
	}

	var rows []table.Row
	for _, name := range m.resultOrder {
		res, ok := m.results[name]
		if !ok {
			continue
		}
		var statusIcon string
		switch {
		case res.cancelled:
			statusIcon = theme.IconCanceled + " —"
		case res.success:
			statusIcon = theme.IconOK + " OK"
		default:
			statusIcon = theme.IconError + " FAIL"
		}
		firstLine := firstOutputLine(res.output, col3W)
		rows = append(rows, table.Row{name, statusIcon, firstLine})
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(max(m.height-13, 3)),
	)
	t.SetStyles(theme.DefaultTableStyles())
	m.resultsTable = t
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

// firstOutputLine returns the first non-empty line truncated to maxLen terminal
// columns. The result goes into a table cell, so it must be valid UTF-8 (Rule
// 122 / theme helpers per Rule 117) — slicing bytes here would cut a multibyte
// rune in half and bleed into the rows below.
func firstOutputLine(output string, maxLen int) string {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return theme.TruncateWidth(line, maxLen)
		}
	}
	return ""
}
