package netdiag

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/netcheck"
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
	case StateRunning, StateResults:
		// The table stays on screen while the pipeline runs, filling in as each
		// stage lands. A body that swaps itself for a spinner loses its header
		// and its columns (Rule 139), and here it would also throw away the
		// rows already answered.
		return m.renderChecks()
	case StateDetails:
		return m.detailsViewport.View()
	}
	return ""
}

func (m *Model) renderInputForm() string {
	var b strings.Builder

	b.WriteString(m.renderTextInputField("Target", m.targetInput, fieldTarget))
	b.WriteString("\n")
	b.WriteString(m.renderTextInputField("Port", m.portInput, fieldPort))
	b.WriteString("\n")
	b.WriteString(m.renderTextInputField("DNS Server", m.dnsServerInput, fieldDNSServer))
	b.WriteString("\n\n")
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

// renderChecks draws the results table. The empty message is conditional on the
// run being over, or the table announces the absence of what it is fetching
// (Rule 139).
func (m *Model) renderChecks() string {
	if m.state == StateResults && len(m.checksTable.Visible()) == 0 {
		if m.filterBar.IsTokenActive(problemsToken) && len(m.results.All()) > 0 {
			return theme.DimStyle.Render("Nothing to report — every check came back clean")
		}
		if !m.filterBar.IsVisible() {
			return theme.DimStyle.Render("No checks")
		}
	}
	return m.checksTable.View()
}

// checkColumns describes the results table.
//
// A row is a question that got an answer, so the columns are the question, the
// answer, and what was observed — never the name of a tool.
func checkColumns() []datatable.Column[netcheck.Check] {
	return []datatable.Column[netcheck.Check]{
		{
			Title: "Check", MinWidth: 20,
			Cell: func(c netcheck.Check) string { return c.Title },
		},
		{
			Title: "Verdict", MinWidth: 9,
			Cell:  verdictCell,
			Style: verdictStyle,
		},
		{
			Title: "Observed", MinWidth: 20, Flex: 1,
			Cell: func(c netcheck.Check) string { return c.Summary },
		},
	}
}

// verdictCell is plain text: the colour is decided by Style (Rule 122).
func verdictCell(c netcheck.Check) string {
	switch c.Verdict {
	case netcheck.OK:
		return theme.IconOK + " OK"
	case netcheck.Warn:
		return theme.IconWarning + " WARN"
	case netcheck.Fail:
		return theme.IconError + " FAIL"
	case netcheck.NotApplicable:
		return theme.IconCanceled + " N/A"
	default:
		return theme.IconCanceled + " ?"
	}
}

// verdictStyle spends colour on what is worth spotting without reading.
//
// OK keeps the default text colour rather than taking green: it is the nominal
// majority state, and a colour that appears on every row informs nobody
// (Rule 122). N/A and Unknown are dim for the same reason a zero count is.
func verdictStyle(c netcheck.Check) lipgloss.Style {
	switch c.Verdict {
	case netcheck.Fail:
		return theme.SeverityTextStyle("CRITICAL")
	case netcheck.Warn:
		return theme.SeverityTextStyle("MEDIUM")
	case netcheck.NotApplicable, netcheck.Unknown:
		return theme.DimStyle
	default:
		return lipgloss.Style{}
	}
}

// renderDetailsContent draws one check: what was observed, what it means, what
// to do, and the facts behind it.
func (m *Model) renderDetailsContent(width int) string {
	if m.traceOutput != "" {
		return m.renderTrace(width)
	}

	c := m.selected
	e := netcheck.Explain(c)

	var lines []string
	lines = append(lines, theme.EmptyLineBg(width))
	lines = append(lines, section(width, "Observed", e.Observed)...)
	lines = append(lines, section(width, "What it means", e.Means)...)
	if e.Do != "" {
		lines = append(lines, section(width, "What to do", e.Do)...)
	}

	if len(c.Facts) > 0 {
		lines = append(lines, theme.PadWithBg(theme.SubTitleStyle.Render(theme.IconConfig+" Details"), width))
		lines = append(lines, theme.EmptyLineBg(width))
		for _, f := range c.Facts {
			line := theme.KeyStyle.Render("  "+f.Key+" "+theme.IconChevronRight+" ") + theme.Bg(f.Value)
			lines = append(lines, theme.PadWithBg(line, width))
		}
		lines = append(lines, theme.EmptyLineBg(width))
	}

	return strings.Join(lines, "\n")
}

// section renders a titled block of prose, wrapped to the pane.
func section(width int, title, body string) []string {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	lines := []string{
		theme.PadWithBg(theme.SubTitleStyle.Render(theme.IconConfig+" "+title), width),
		theme.EmptyLineBg(width),
	}
	wrapped := lipgloss.NewStyle().Width(max(width-4, 20)).Render(body)
	for line := range strings.SplitSeq(wrapped, "\n") {
		lines = append(lines, theme.PadWithBg(theme.Bg("  "+line), width))
	}
	lines = append(lines, theme.EmptyLineBg(width))
	return lines
}

// renderTrace draws a route trace, with the caveat that makes it readable.
func (m *Model) renderTrace(width int) string {
	title := "ICMP route"
	if m.traceTCP {
		title = "TCP route"
	}

	lines := []string{
		theme.EmptyLineBg(width),
		theme.PadWithBg(theme.SubTitleStyle.Render(theme.IconNetwork+" "+title), width),
		theme.EmptyLineBg(width),
	}
	// The trace is the one probe that still runs in a container, so it answers
	// for the container's network and can disagree with the checks above it.
	// Saying so is cheaper than a user reconciling two contradictory screens.
	lines = append(lines, theme.PadWithBg(
		theme.DimStyle.Render("  Traced from the Docker network tool container, not from this machine."), width))
	lines = append(lines, theme.EmptyLineBg(width))
	lines = append(lines, formatTracerouteOutput(m.traceOutput, width)...)
	return strings.Join(lines, "\n")
}

// rebuildChecksTable refills the table from the filtered results.
func (m *Model) rebuildChecksTable() {
	m.checksTable.SetItems(m.visibleChecks())
	m.checksTable.Resize(m.width, max(m.height-13, 3))
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

// progressLabel names the question being asked, so a wait on a slow stage is
// legible rather than mute.
func (m *Model) progressLabel() string {
	return fmt.Sprintf("%s (%d/%d)", netcheck.StageTitle(m.runStage), m.runStep+1, m.totalSteps)
}
