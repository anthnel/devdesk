package security

import (
	"log"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/browser"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// handleDetailsState processes input in details state
func (m Model) handleDetailsState(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	// backspace was an alias of esc, and the only one in the application.
	case "esc":
		m.state = StateResults
		m.changePreview = changePreview{}
		return m, nil
	case keymap.Web:
		return m.handleDetailsOpenReference()
	case "enter":
		return m.openFindingFile()
	case writeRemediationKey:
		return m.prepareMisconfigFix()
	case keymap.Copy:
		return m.copyFixCommand()
	case keymap.Exclude:
		return m.handleIgnoreSecret()
	case "up":
		m.detailsViewport.ScrollUp(1)
	case "down":
		m.detailsViewport.ScrollDown(1)
	case "pgup":
		m.detailsViewport.HalfPageUp()
	case "pgdown":
		m.detailsViewport.HalfPageDown()
	case "home":
		m.detailsViewport.GotoTop()
	case "end":
		m.detailsViewport.GotoBottom()
	}
	return m, nil
}

// handleDetailsOpenReference opens the first reference URL in the default browser.
func (m Model) handleDetailsOpenReference() (tea.Model, tea.Cmd) {
	if m.selectedFinding == nil {
		return m, nil
	}
	f := *m.selectedFinding
	if len(f.References) == 0 {
		return m, m.footer.Warn("No references available")
	}
	url := f.References[0]
	return m, func() tea.Msg {
		if err := browser.Open(url); err != nil {
			log.Printf("ERROR [security] open URL %s: %v", url, err)
		}
		return nil
	}
}

// sizeDetails gives the details viewport the room the router leaves it: the
// view's width less the two border cells the viewport frame takes, so a line
// never reaches under the right border. The padding takes one more cell on each
// side, which is the text width (detailsTextWidth).
func (m *Model) sizeDetails() {
	m.detailsViewport.SetWidth(max(m.width-2, 1))
	m.detailsViewport.SetHeight(m.height)
}

// detailsTextWidth is how many cells a line of the detail text may span: one
// column, never wider than maxColumnWidth.
func (m Model) detailsTextWidth() int { return min(max(m.width-4, 1), maxColumnWidth) }

// scanTarget is the scanned path, for display only: it is what shortPaths cuts
// off a path, and an image reference there is ignored.
func (m Model) scanTarget() string {
	if m.result == nil {
		return ""
	}
	return m.result.Target
}

// targetOnDisk says whether the scanned target is a directory, whose files a
// built-in fix can write.
func (m Model) targetOnDisk() bool {
	return m.result != nil && m.result.TargetType == scan.TargetDirectory
}

// buildDetailsContent builds the full scrollable content for the details view.
//
// What to do comes before why: the Fix block (status, Where, Do, Run, Effect),
// the Change it would make, then Why, the scanner's Output and the Refs. Each section is emitted only when
// it has something to say, and sections are joined by exactly one blank line.
func (m Model) buildDetailsContent() string {
	if m.selectedFinding == nil {
		return theme.Bg("No finding selected")
	}

	f := *m.selectedFinding
	t := newDetailsText(m.detailsTextWidth())

	var sections []string
	for _, section := range [][]string{
		m.detailsHeader(f, t),
		m.detailsFix(f, t),
		m.detailsChange(f, t),
		m.detailsWhy(f, t),
		m.detailsOutput(f, t),
		m.detailsReferences(f, t),
	} {
		if len(section) > 0 {
			sections = append(sections, strings.Join(section, "\n"))
		}
	}

	return lipgloss.NewStyle().
		Padding(1).
		Background(theme.ColorBackground).
		Render(strings.Join(sections, "\n\n"))
}

// detailsHeader is the severity and ID, then the title on one line: a title can
// carry a whole stderr line, and the Message below has the rest.
func (m Model) detailsHeader(f scan.Finding, t detailsText) []string {
	sev := string(f.Severity)
	header := m.getSeverityStyle(f.Severity).Render(sev) + theme.Bg(" ") +
		theme.TitleStyle.Render(oneLine(f.ID, t.width-ansi.StringWidth(sev)-1))
	title := oneLine(shortPaths(f.Title, m.scanTarget()), t.width)
	return []string{header, t.text.Render(title)}
}

// detailsFix is the Fix block: the status in words, then, with no blank line
// between them, where the finding is, what to do, the command to run and what the
// fix does. It is written in ColorText alone (DEVDESK-751, AC7): ColorDim, the
// sub-title and the warning colours fall under 4.5:1 on several shipped themes,
// so the word "Warning:" carries the warning.
func (m Model) detailsFix(f scan.Finding, t detailsText) []string {
	status := m.fixStatus(f)
	lines := t.field("Fix: ", status.Label(), t.text)
	lines = append(lines, m.detailsWhere(f, t)...)
	lines = append(lines, t.list("Do: ", fixSteps(f), f.Resolution, t.text)...)
	lines = append(lines, detailsRun(f, t)...)
	lines = append(lines, detailsRepro(f, t)...)
	lines = append(lines, t.field("Effect: ", status.Effect, t.text)...)
	return append(lines, t.field("Warning: ", status.Warning, t.text)...)
}

// detailsWhere is the file, then the other facts that place the finding, each on
// its own line under the first.
func (m Model) detailsWhere(f scan.Finding, t detailsText) []string {
	const label = "Where: "
	facts := whereFacts(f, m.scanTarget())
	if len(facts) == 0 {
		return nil
	}
	first := facts[0].Value
	if facts[0].Label != "File" {
		first = facts[0].Label + ": " + first
	}
	lines := t.field(label, first, t.text)
	for _, fc := range facts[1:] {
		lines = append(lines, t.fieldAt(ansi.StringWidth(label), fc.Label+": ", fc.Value, t.text)...)
	}
	return lines
}

// detailsRun is the command on one line, cut when it is wider than the column:
// the copy key puts the whole of it on the clipboard.
func detailsRun(f scan.Finding, t detailsText) []string {
	const label = "Run: "
	cmd, ok := runCommand(f)
	if !ok {
		return nil
	}
	return []string{t.heading(label) + t.text.Render(oneLine("$ "+cmd, t.width-ansi.StringWidth(label)))}
}

// detailsWhy is the reason the finding matters: the scanner's description, the
// Pod Security profile, where the finding comes from and its type.
func (m Model) detailsWhy(f scan.Finding, t detailsText) []string {
	var lines []string
	if strings.TrimSpace(f.Description) != "" {
		lines = t.block(f.Description, t.text)
	}
	lines = append(lines, t.field("Pod Security: ", pssLabel(f), t.text)...)
	lines = append(lines, t.field("Source: ", f.Source, t.text)...)
	lines = append(lines, t.field("Type: ", f.IaCType, t.text)...)
	if len(lines) == 0 {
		return nil
	}
	return append([]string{t.heading("Why:")}, lines...)
}

// detailsOutput is what the scanner said about this instance, capped: the start
// of it is what tells two findings of one rule apart, and the rest is in the log.
// A message that only repeats the title or the description adds nothing.
func (m Model) detailsOutput(f scan.Finding, t detailsText) []string {
	msg := strings.TrimSpace(f.Message)
	if msg == "" || msg == strings.TrimSpace(f.Title) || msg == strings.TrimSpace(f.Description) {
		return nil
	}
	lines, hidden := cappedLines(dropBlankLines(shortPaths(msg, m.scanTarget())), t.width, maxMessageLines)
	if len(lines) == 0 {
		return nil
	}
	out := []string{t.heading("Output:")}
	for _, line := range lines {
		out = append(out, t.text.Render(line))
	}
	if more := moreLines(hidden); more != "" {
		out = append(out, theme.DimStyle.Render(more))
	}
	return out
}

// detailsReferences are the reference URLs, one per line.
func (m Model) detailsReferences(f scan.Finding, t detailsText) []string {
	if len(f.References) == 0 {
		return nil
	}
	lines := []string{t.heading("Refs:")}
	for _, ref := range f.References {
		lines = append(lines, t.indented("  ", ref, t.text)...)
	}
	return lines
}

// renderDetailsView renders the scrollable details viewport.
func (m Model) renderDetailsView() string {
	return m.detailsViewport.View()
}

// getSeverityStyle returns style for severity level (Rule 102: the palette
// lives in the theme, not here).
func (m Model) getSeverityStyle(sev scan.SeverityLevel) lipgloss.Style {
	return theme.SeverityTextStyle(string(sev))
}

// HeaderView interface implementation

// packageKind says what has to move to clear a vulnerability, from the class
// Trivy reported. Empty for a result cached before the class was recorded: it
// is unknown, and is not shown rather than guessed.
func packageKind(class string) string {
	switch class {
	case scan.ClassOSPackages:
		return "Base image package"
	case scan.ClassLangPackages:
		return "Application dependency"
	}
	return ""
}
