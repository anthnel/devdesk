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
	case tabInterfaces:
		return m.interfacesModel.view()
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

// renderChecks draws the results table. An empty table — the run produced
// none, or the "problems" filter hid every one because they all passed —
// stays a table (Rule 139): its header and no rows. The Verdict header field
// already answers "did everything pass?" once there is a run to summarise.
func (m *Model) renderChecks() string {
	return m.checksTable.View()
}

// checkColumns describes the results table.
//
// A row is a question that got an answer, so the columns are the question, the
// answer, and what was observed — never the name of a tool.
func checkColumns() []datatable.Column[netcheck.Check] {
	return []datatable.Column[netcheck.Check]{
		{
			Title: "Check", Sizing: datatable.SizingContent, MinWidth: 20,
			Cell: func(c netcheck.Check) string { return c.Title },
		},
		{
			Title: "Verdict", Sizing: datatable.SizingFixed, MinWidth: 9,
			Cell:  verdictCell,
			Style: verdictStyle,
		},
		{
			Title: "Observed", Sizing: datatable.SizingContent, MinWidth: 20, Flex: 1,
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

// verdictStyle gives each verdict its own colour.
//
// Rule 122 reserves colour for what is worth spotting without reading, and its
// worked example — a container list where nearly every row is `running` —
// argues against colouring the nominal state. This column is the other case,
// and the difference is what the reader is doing: a diagnostic is read once,
// end to end, to find where it broke. Every row is a distinct question, there
// is no majority state to drown in, and the column *is* the answer. So the four
// outcomes are told apart at a glance, as Rule 121 has status indicators do.
//
// The one that is not a colour choice is UNKNOWN. It must not share N/A's dim:
// "no meaning here" and "we could not look" are the distinction the whole
// package is built on — the reason Verdict's zero value is Unknown and the
// reason SecretVerdict returns a *bool. Rendering them alike on screen would
// give back exactly what the types take care to keep apart.
func verdictStyle(c netcheck.Check) lipgloss.Style {
	switch c.Verdict {
	case netcheck.Fail:
		return theme.SeverityTextStyle("CRITICAL")
	case netcheck.Warn:
		return theme.SeverityTextStyle("MEDIUM")
	case netcheck.OK:
		return theme.StatusOKStyle
	case netcheck.Unknown:
		return theme.SeverityTextStyle("HIGH")
	default: // NotApplicable — nothing was asked, so nothing is worth spotting
		return theme.DimStyle
	}
}

// renderDetailsContent draws one check: what was observed, what it means, what
// to do, and the facts behind it.
func (m *Model) renderDetailsContent(width int) string {
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
