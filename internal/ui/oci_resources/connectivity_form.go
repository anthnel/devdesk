package ociresources

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

type connectivityTestType int

const (
	testPing   connectivityTestType = iota
	testHTTP                        // HTTP GET via wget
	testNetcat                      // Port check via nc
)

var testTypeLabels = []string{"Ping (ICMP)", "HTTP GET (wget)", "Port Check (nc)"}

type connectivityFormState int

const (
	connectivityStateInput   connectivityFormState = iota
	connectivityStateRunning                       // test in progress, input blocked
	connectivityStateResults                       // results ready
)

const (
	cFieldTarget = 0
	cFieldType   = 1
	cFieldPort   = 2 // visible only for curl/netcat; doubles as submit when testPing
	cFieldSubmit = 3 // only exists when port is visible
)

// ConnectivityTestForm allows the user to run a network diagnostic from an ephemeral container.
type ConnectivityTestForm struct {
	sourceContainer  string
	networkID        string
	diagnosticImage  string                    // Docker image used for the ephemeral test container
	containers       []docker.NetworkContainer // available targets from network inspect
	containerListIdx int                       // -1 = no selection, 0+ = highlighted suggestion
	targetInput      textinput.Model
	portInput        textinput.Model
	testType         connectivityTestType
	focusedField     int
	state            connectivityFormState
	result           string
	resultErr        string
	resultExitCode   bool // true = non-zero exit but output exists (warning, not hard failure)
	resultScroll     int  // scroll offset for result lines
	width            int
	height           int
}

// newConnectivityTestForm creates a ConnectivityTestForm pre-filled with the source container.
func newConnectivityTestForm(sourceContainer, networkID, diagnosticImage string, containers []docker.NetworkContainer, width, height int) *ConnectivityTestForm {
	targetInput := textinput.New()
	targetInput.Placeholder = "IP or container name  (↑↓ to browse)"
	targetInput.CharLimit = 100
	theme.StyleTextInput(&targetInput)
	targetInput.Focus()

	portInput := textinput.New()
	portInput.Placeholder = "80"
	portInput.CharLimit = 6
	theme.StyleTextInput(&portInput)

	return &ConnectivityTestForm{
		sourceContainer:  sourceContainer,
		networkID:        networkID,
		diagnosticImage:  diagnosticImage,
		containers:       containers,
		containerListIdx: -1,
		targetInput:      targetInput,
		portInput:        portInput,
		testType:         testPing,
		focusedField:     cFieldTarget,
		state:            connectivityStateInput,
		width:            width,
		height:           height,
	}
}

// isPortActive reports whether the port field should be shown (curl or netcat mode).
func (f *ConnectivityTestForm) isPortActive() bool {
	return f.testType == testHTTP || f.testType == testNetcat
}

// numFields returns the total number of navigable fields based on the current test type.
func (f *ConnectivityTestForm) numFields() int {
	if f.isPortActive() {
		return 4 // target, type, port, submit
	}
	return 3 // target, type, submit
}

// submitIdx returns the field index of the submit button.
func (f *ConnectivityTestForm) submitIdx() int {
	if f.isPortActive() {
		return cFieldSubmit
	}
	return cFieldPort // submit is at index 2 when port is hidden
}

// updateFocus applies focus/blur to text inputs based on focusedField.
func (f *ConnectivityTestForm) updateFocus() {
	f.targetInput.Blur()
	f.portInput.Blur()
	switch f.focusedField {
	case cFieldTarget:
		f.targetInput.Focus()
	case cFieldPort:
		if f.isPortActive() {
			f.portInput.Focus()
		}
	}
}

// applyContainerSelection fills the target input with the highlighted container's IPv4.
func (f *ConnectivityTestForm) applyContainerSelection() {
	if f.containerListIdx >= 0 && f.containerListIdx < len(f.containers) {
		f.targetInput.SetValue(f.containers[f.containerListIdx].IPv4)
	}
}

// buildCommand returns the CLI arguments for the selected test type and inputs.
func (f *ConnectivityTestForm) buildCommand() []string {
	target := strings.TrimSpace(f.targetInput.Value())
	port := strings.TrimSpace(f.portInput.Value())
	if port == "" {
		port = "80"
	}
	switch f.testType {
	case testHTTP:
		// wget rather than curl, because it is in *both* images rather than in
		// one of them: busybox has no curl, and netshoot has both. That is what
		// made busybox affordable as the default (§3.47) — verified side by
		// side, the two produce the same status line and the same headers.
		return []string{"wget", "-S", "-O-", "-T", "5", "http://" + target + ":" + port}
	case testNetcat:
		return []string{"nc", "-zv", "-w", "5", target, port}
	default: // testPing
		return []string{"ping", "-c", "3", target}
	}
}

// SetResult updates the form with the diagnostic test result (called from parent Update).
// If both output and an error are present, the test ran but exited with a non-zero code
// (wget exits 1 on an HTTP error status, nc non-zero on a closed port). We treat this as
// a warning rather than a hard failure: the output is the answer either way.
func (f *ConnectivityTestForm) SetResult(output string, err error) {
	f.state = connectivityStateResults
	f.result = output
	f.resultScroll = 0
	if err != nil {
		// Strip the "diagnostic command failed: " wrapper added by RunDiagnosticContainer
		errMsg := strings.TrimPrefix(err.Error(), "diagnostic command failed: ")
		f.resultErr = errMsg
		// If there is output, the container ran but exited non-zero (e.g. wget/nc exit codes)
		f.resultExitCode = output != ""
	} else {
		f.resultErr = ""
		f.resultExitCode = false
	}
}

// Update handles input events for the connectivity form.
func (f *ConnectivityTestForm) Update(msg tea.Msg) (*ConnectivityTestForm, tea.Cmd) {
	// Block input while running
	if f.state == connectivityStateRunning {
		return f, nil
	}

	// Results state: scroll with ↑/↓, enter resets to input
	if f.state == connectivityStateResults {
		if key, ok := msg.(tea.KeyMsg); ok {
			resultLines := strings.Split(f.result, "\n")
			visibleLines := f.resultVisibleLines()
			switch key.String() {
			case "enter":
				f.state = connectivityStateInput
				f.result = ""
				f.resultErr = ""
				f.resultExitCode = false
				f.resultScroll = 0
			case "up":
				f.resultScroll = max(f.resultScroll-1, 0)
			case "down":
				f.resultScroll = min(f.resultScroll+1, max(len(resultLines)-visibleLines, 0))
			// This pane is not a table, so it cannot forward to one: pgup and
			// pgdown have to be written here. They were missing entirely, which
			// is the same hole as the tabs' allow-lists and the same cause.
			case "pgup":
				f.resultScroll = max(f.resultScroll-visibleLines, 0)
			case "pgdown":
				f.resultScroll = min(f.resultScroll+visibleLines, max(len(resultLines)-visibleLines, 0))
			case "home":
				f.resultScroll = 0
			case "end":
				f.resultScroll = max(len(resultLines)-visibleLines, 0)
			}
		}
		return f, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			// On target field with containers: scroll suggestion list up
			if f.focusedField == cFieldTarget && len(f.containers) > 0 && f.containerListIdx >= 0 {
				if f.containerListIdx > 0 {
					f.containerListIdx--
				} else {
					f.containerListIdx = -1
				}
				f.applyContainerSelection()
				return f, nil
			}
			// Otherwise: navigate to previous field (Rule 135)
			f.containerListIdx = -1
			n := f.numFields()
			f.focusedField = (f.focusedField - 1 + n) % n
			f.updateFocus()
			return f, nil

		case "down":
			// On target field with containers: scroll suggestion list down
			if f.focusedField == cFieldTarget && len(f.containers) > 0 && f.containerListIdx < len(f.containers)-1 {
				f.containerListIdx++
				f.applyContainerSelection()
				return f, nil
			}
			// Otherwise: navigate to next field (Rule 135)
			f.containerListIdx = -1
			f.focusedField = (f.focusedField + 1) % f.numFields()
			f.updateFocus()
			return f, nil

		// Cycling the type can hide the port field, taking numFields() from 4 to
		// 3. Both handlers used to clamp the focus against that, and neither
		// clamp could ever fire (D21): cycling only happens inside
		// `focusedField == cFieldType`, so the focus is 1, and numFields() is
		// never below 3. Reinstating them defensively would put back code no
		// test can reach — `TestCyclingTheTypeNeverStrandsTheFocus` is what
		// keeps the invariant they were guarding true.

		case "left":
			if f.focusedField == cFieldType {
				n := len(testTypeLabels)
				f.testType = connectivityTestType((int(f.testType) - 1 + n) % n)
				f.updateFocus()
			}
			return f, nil

		case "right":
			if f.focusedField == cFieldType {
				n := len(testTypeLabels)
				f.testType = connectivityTestType((int(f.testType) + 1) % n)
				f.updateFocus()
			}
			return f, nil

		case "enter":
			if f.focusedField == f.submitIdx() {
				target := strings.TrimSpace(f.targetInput.Value())
				if target == "" {
					return f, nil
				}
				f.state = connectivityStateRunning
				networkID := f.networkID
				command := f.buildCommand()
				return f, runDiagnosticContainerCmd(networkID, f.diagnosticImage, command)
			}
		}
	}

	// Forward unhandled keys to the focused text input
	// Any key reaching this point resets the container suggestion selection
	var cmd tea.Cmd
	switch f.focusedField {
	case cFieldTarget:
		if _, ok := msg.(tea.KeyMsg); ok {
			f.containerListIdx = -1
		}
		f.targetInput, cmd = f.targetInput.Update(msg)
	case cFieldPort:
		if f.isPortActive() {
			f.portInput, cmd = f.portInput.Update(msg)
		}
	}
	return f, cmd
}

// View renders the connectivity test form (full viewport, Rule 112).
func (f *ConnectivityTestForm) View() string {
	w := f.width
	lines := []string{theme.EmptyLineBg(w)} // Rule 131: top padding

	// Source (read-only)
	sourceVal := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Render(f.sourceContainer)
	lines = append(lines,
		theme.PadWithBg(theme.Bg("  Source "+theme.IconChevronRight+" ")+sourceVal, w),
		theme.EmptyLineBg(w),
	)

	switch f.state {
	case connectivityStateRunning:
		lines = append(lines, theme.PadWithBg(theme.DimStyle.Render("  Running diagnostic test..."), w))
	case connectivityStateResults:
		lines = append(lines, f.renderResults(w)...)
	default:
		lines = append(lines, f.renderInputFields(w)...)
	}

	return strings.Join(lines, "\n")
}

func (f *ConnectivityTestForm) renderInputFields(w int) []string {
	var lines []string

	// Target field (Rule 120)
	targetLabel := " Target " + theme.IconChevronRight + " "
	var targetLine string
	if f.focusedField == cFieldTarget {
		targetLine = theme.PadWithBg(theme.KeyStyle.Render(theme.IconCircleSmall+" "+targetLabel)+f.targetInput.View(), w)
	} else {
		targetLine = theme.PadWithBg(theme.Bg("  "+targetLabel)+f.targetInput.View(), w)
	}
	lines = append(lines, targetLine)

	// Suggestion list — shown when target field is focused and containers are available
	if f.focusedField == cFieldTarget && len(f.containers) > 0 {
		lines = append(lines, f.renderSuggestions(w)...)
	}

	lines = append(lines, theme.EmptyLineBg(w))

	// Test type — cycle field (Rule 132)
	typeLabel := " Test " + theme.IconSelect + " " + theme.IconChevronRight + " "
	typeValue := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Render(testTypeLabels[f.testType])
	var typeLine string
	if f.focusedField == cFieldType {
		typeLine = theme.PadWithBg(theme.KeyStyle.Render(theme.IconCircleSmall+" "+typeLabel)+typeValue, w)
	} else {
		typeLine = theme.PadWithBg(theme.Bg("  "+typeLabel)+typeValue, w)
	}
	lines = append(lines, typeLine, theme.EmptyLineBg(w))

	// Port field (curl / netcat only)
	if f.isPortActive() {
		portLabel := " Port " + theme.IconChevronRight + " "
		var portLine string
		if f.focusedField == cFieldPort {
			portLine = theme.PadWithBg(theme.KeyStyle.Render(theme.IconCircleSmall+" "+portLabel)+f.portInput.View(), w)
		} else {
			portLine = theme.PadWithBg(theme.Bg("  "+portLabel)+f.portInput.View(), w)
		}
		lines = append(lines, portLine, theme.EmptyLineBg(w))
	}

	// Submit button
	submitFocused := f.focusedField == f.submitIdx()
	lines = append(lines, theme.PadWithBg(theme.Bg("  ")+theme.RenderButton("Run Test", submitFocused, "primary"), w))
	return lines
}

// renderSuggestions renders the navigable container list below the target input.
func (f *ConnectivityTestForm) renderSuggestions(w int) []string {
	lines := make([]string, 0, len(f.containers)+1)
	// Section header
	lines = append(lines, theme.PadWithBg(theme.DimStyle.Render("    Containers on this network:"), w))
	for i, c := range f.containers {
		name := c.Name
		ip := c.IPv4
		if ip == "" {
			ip = "—"
		}
		selected := i == f.containerListIdx
		// Name padded to align IP column
		namePadded := name
		if len(namePadded) < 24 {
			namePadded += strings.Repeat(" ", 24-len(namePadded))
		}
		entry := namePadded + "  " + ip
		var line string
		if selected {
			line = theme.PadWithBg(
				lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorHighlight).Bold(true).Render("    "+theme.IconCircleSmall+" "+entry),
				w,
			)
		} else {
			line = theme.PadWithBg(theme.DimStyle.Render("      "+entry), w)
		}
		lines = append(lines, line)
	}
	return lines
}

// resultVisibleLines returns how many output lines fit in the viewport.
// Header (3) + status (1) + empty (1) + empty (1) = 6 fixed lines (no inline help per Rule 134).
func (f *ConnectivityTestForm) resultVisibleLines() int {
	return max(f.height-6, 3)
}

// truncateResultLine truncates a plain-text line to maxRunes visible characters,
// appending "…" when truncation occurs. CLI output has no ANSI codes, so rune
// counting is sufficient and safe.
func truncateResultLine(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-1]) + "…"
}

func (f *ConnectivityTestForm) renderResults(w int) []string {
	var lines []string

	// Status header
	switch {
	case f.resultErr != "" && !f.resultExitCode:
		// Hard failure: no output, only error
		lines = append(lines, theme.PadWithBg(theme.StatusErrorStyle.Render("  "+theme.IconError+" "+f.resultErr), w))
	case f.resultErr != "" && f.resultExitCode:
		// Non-zero exit but output present (e.g. curl exit 52, nc exit 1)
		warnStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorWarn)
		lines = append(lines, theme.PadWithBg(warnStyle.Render("  "+theme.IconWarning+" "+f.resultErr), w))
	default:
		okStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorOK)
		lines = append(lines, theme.PadWithBg(okStyle.Render("  "+theme.IconOK+" Test completed"), w))
	}
	lines = append(lines, theme.EmptyLineBg(w))

	// Scrollable output
	if f.result != "" {
		allLines := strings.Split(f.result, "\n")
		visible := f.resultVisibleLines()
		end := min(f.resultScroll+visible, len(allLines))
		maxContent := w - 3 // 2 indent + 1 for "…"
		for _, line := range allLines[f.resultScroll:end] {
			line = strings.TrimRight(line, "\r") // strip Windows-style CR
			line = truncateResultLine(line, maxContent)
			lines = append(lines, theme.PadWithBg(theme.Bg("  "+line), w))
		}
		// Scroll indicator when content overflows
		if len(allLines) > visible {
			indicator := theme.DimStyle.Render(
				fmt.Sprintf("  [%d–%d / %d lines]", f.resultScroll+1, end, len(allLines)),
			)
			lines = append(lines, theme.PadWithBg(indicator, w))
		}
	}

	lines = append(lines, theme.EmptyLineBg(w))
	return lines
}
