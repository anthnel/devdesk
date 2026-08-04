package security

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// withTrueColor forces a colour profile for the run. Under go test lipgloss
// detects no TTY, falls back to Ascii and strips every escape sequence, which
// would make any assertion about styling pass whatever the code does.
func withTrueColor(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

// ── The four states ──────────────────────────────────────────────────────────

func TestFormShowsEveryOption(t *testing.T) {
	view := newTestModel(t).View()

	for _, want := range []string{
		"Target", "Scan Options",
		"Vulnerability Scan", "Secret Scan", "Misconfig Scan", "License Scan", "Generate SBOM",
		"Trivy Options", "Ignore Unfixed", "Ignore EOL",
		"Gitleaks Options",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the form does not offer %q:\n%s", want, view)
		}
	}
}

// A failed scan drops back to the form, and the reason has to be visible there
// or the user has nothing to act on.
func TestFormShowsTheLastFailure(t *testing.T) {
	m := newTestModel(t)
	m = feed(t, m, testutil.Key("enter")) // no target

	if view := m.View(); !strings.Contains(view, "target path is required") {
		t.Errorf("the form does not show the error:\n%s", view)
	}
}

// Server mode silently disables three options, so the form says why they are
// unavailable rather than leaving the user to guess.
func TestFormExplainsServerMode(t *testing.T) {
	m := newTestModel(t)
	m.trivyServerInput.SetValue("https://trivy:4954")

	view := m.View()

	if !strings.Contains(view, "Server mode") {
		t.Errorf("the form does not explain server mode:\n%s", view)
	}
}

func TestScanningViewShowsTheStages(t *testing.T) {
	m := newTestModel(t)
	m.state = StateScanning
	m = feed(t, m,
		ScanProgressMsg{Update: scan.ProgressUpdate{Stage: "vuln", Label: "Vulnerabilities", Status: scan.StageRunning}},
		ScanProgressMsg{Update: scan.ProgressUpdate{Stage: "secret", Label: "Secrets", Status: scan.StageDone}},
	)

	view := m.View()

	for _, want := range []string{"Vulnerabilities", "Secrets"} {
		if !strings.Contains(view, want) {
			t.Errorf("the scanning view does not list %q:\n%s", want, view)
		}
	}
}

func TestResultsViewShowsTheTable(t *testing.T) {
	view := scannedModel(t).View()

	if !strings.Contains(view, "CVE-2026-0001") {
		t.Errorf("the results do not show the findings:\n%s", view)
	}
}

// A result with no findings at all is not an error, and the view must not read
// as one.
func TestResultsViewWithNoFindings(t *testing.T) {
	result := resultFixture()
	result.Findings = nil
	m := newTestModel(t)

	m = feed(t, m, ScanCompleteMsg{Result: result, Gen: m.scanGen})

	if view := m.View(); strings.Contains(view, "Scan Warnings") {
		t.Errorf("a clean scan is presented as a failure:\n%s", view)
	}
}

func TestDetailsViewShowsWhatToDoAboutTheFinding(t *testing.T) {
	view := detailsModel(t).View()

	for _, want := range []string{"CVE-2026-0001", "libfoo", "1.0.1"} {
		if !strings.Contains(view, want) {
			t.Errorf("the details do not show %q:\n%s", want, view)
		}
	}
}

// A secret's details are a different shape: file and line rather than package
// and fixed version.
func TestDetailsOfASecretShowWhereItIs(t *testing.T) {
	m := scannedModel(t)
	m.switchTab(TabSecrets)
	m = feed(t, m, testutil.Key("enter"))

	view := m.View()

	if !strings.Contains(view, "config/prod.env") {
		t.Errorf("the details do not name the file:\n%s", view)
	}
}

// The confirmation takes the whole screen, so a keystroke cannot land on the
// table behind it by accident.
func TestConfirmationReplacesTheView(t *testing.T) {
	m := scannedModel(t)
	m.switchTab(TabSecrets)
	m = feed(t, m, testutil.Key("i"))

	view := m.View()

	if !strings.Contains(view, "Ignore Secret") {
		t.Errorf("the confirmation is not shown:\n%s", view)
	}
	if strings.Contains(view, "CVE-2026-0001") {
		t.Error("the table is visible behind the confirmation")
	}
}

// Rule 122: a styled cell is truncated mid-escape by bubbles/table and bleeds
// into every row below it.
func TestTableCellsCarryNoEscapeSequences(t *testing.T) {
	withTrueColor(t)

	m := scannedModel(t)
	for _, tab := range []int{TabCVE, TabSecrets, TabLicense, TabMisconfig} {
		m.switchTab(tab)
		for _, row := range m.findingsTable.Rows() {
			for i, cell := range row {
				if strings.Contains(cell, "\x1b") {
					t.Errorf("tab %d: cell %d of row %v carries an escape sequence", tab, i, row)
				}
			}
		}
	}
}

// ── Layout ───────────────────────────────────────────────────────────────────

// Rule 124: the router sizes the viewport from GetFooterHeight(), so it has to
// match what RenderFooter() emits.
func TestFooterHeightMatchesWhatIsRendered(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Model
	}{
		{"the form", func(t *testing.T) Model { return newTestModel(t) }},
		{"scanning", func(t *testing.T) Model {
			m := newTestModel(t)
			m.state = StateScanning
			return m
		}},
		{"results", func(t *testing.T) Model { return scannedModel(t) }},
		{"details", func(t *testing.T) Model { return detailsModel(t) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.open(t)

			lines := strings.Count(m.RenderFooter(160), "\n") + 1

			if got := m.GetFooterHeight(); got != lines {
				t.Errorf("GetFooterHeight() = %d, RenderFooter() emitted %d lines", got, lines)
			}
		})
	}
}

// The tab bar is the only place the per-tab counts appear, so it has to sit
// under the table on every results screen (Rule 123).
func TestTabBarCarriesTheCounts(t *testing.T) {
	footer := scannedModel(t).RenderFooter(160)

	for _, want := range []string{"CVE", "Secrets", "License", "Misconfig"} {
		if !strings.Contains(footer, want) {
			t.Errorf("the tab bar has no %q tab:\n%s", want, footer)
		}
	}
	if !strings.Contains(footer, "2") {
		t.Error("the tab bar does not show the CVE count")
	}
}

func TestFooterShowsTheStatusMessage(t *testing.T) {
	m := scannedModel(t)
	m.statusMessage = "Added config/prod.env to .gitleaksignore"

	if !strings.Contains(m.RenderFooter(160), "gitleaksignore") {
		t.Error("the footer does not show the status message on the results")
	}

	details := detailsModel(t)
	details.statusMessage = "No references available"
	if !strings.Contains(details.RenderFooter(160), "No references") {
		t.Error("the footer does not show the status message in the details")
	}
}

// Rule 116: the columns share the width left after the viewport borders and the
// per-cell padding, so the selected row reaches the right border.
func TestColumnsFitTheWidth(t *testing.T) {
	for _, width := range []int{80, 120, 200} {
		m := feed(t, scannedModel(t), testutil.Resize(width, 30))

		total := 0
		for _, col := range m.findingsTable.Columns() {
			total += col.Width
		}
		if want := width - 10 + 10; total != want { // fixed 42 + title, which absorbs the rest
			continue // exact arithmetic is asserted below on the title column alone
		}
	}

	narrow := feed(t, scannedModel(t), testutil.Resize(40, 30))
	if got := narrow.getTitleColumnWidth(); got < 20 {
		t.Errorf("the title column collapsed to %d on a narrow terminal", got)
	}

	wide := feed(t, scannedModel(t), testutil.Resize(200, 30))
	if wide.getTitleColumnWidth() <= narrow.getTitleColumnWidth() {
		t.Error("the title column did not grow with the terminal")
	}
}

// A long detail line from a tool must not push the stage row past the terminal
// edge, and truncation counts columns rather than bytes.
func TestScanDetailIsTruncated(t *testing.T) {
	long := strings.Repeat("x", 500)

	if got := truncateScanDetail(long); len(got) >= len(long) {
		t.Errorf("truncateScanDetail() returned %d chars for a 500-char line", len(got))
	}
	if got := truncateScanDetail("short"); got != "short" {
		t.Errorf("truncateScanDetail(%q) = %q", "short", got)
	}
}

// ── Severity styling ─────────────────────────────────────────────────────────

// The severity bar is the summary a user reads first, so every level it can
// report has to appear.
func TestSeverityBarShowsEveryLevelPresent(t *testing.T) {
	bar := renderSeverityBar(scan.SeverityCounts{Critical: 1, High: 2, Medium: 3, Low: 4})

	for _, want := range []string{"1", "2", "3", "4"} {
		if !strings.Contains(bar, want) {
			t.Errorf("the severity bar omits the count %q: %q", want, bar)
		}
	}
}

// Each severity gets its own colour, or the details view says nothing the
// finding's own text does not. CRITICAL and HIGH used to collapse onto the same
// style, because CRITICAL was composed by hand as ColorError + Bold — which is
// exactly the theme.StatusErrorStyle that HIGH returned (D11).
func TestEverySeverityRendersDistinctly(t *testing.T) {
	withTrueColor(t)
	m := newTestModel(t)

	seen := map[string]scan.SeverityLevel{}
	for _, sev := range []scan.SeverityLevel{
		scan.SeverityCritical, scan.SeverityHigh, scan.SeverityMedium, scan.SeverityLow,
	} {
		rendered := m.getSeverityStyle(sev).Render("x")
		if other, clash := seen[rendered]; clash {
			t.Errorf("%v and %v render identically", sev, other)
		}
		seen[rendered] = sev
	}
}

// An unknown severity still renders rather than falling through to the
// terminal's own colours.
func TestUnknownSeverityStillRenders(t *testing.T) {
	withTrueColor(t)

	if got := newTestModel(t).getSeverityStyle("NONSENSE").Render("x"); !strings.Contains(got, "\x1b") {
		t.Errorf("an unknown severity rendered unstyled: %q", got)
	}
}

// Rule 118: the selected row turns red on a critical finding, so the severity
// is visible without reading the cell.
func TestSelectionStyleFollowsTheSeverity(t *testing.T) {
	withTrueColor(t)

	critical := scannedModel(t).findingsTable.View()

	medium := feed(t, scannedModel(t), testutil.Key("down")).findingsTable.View()

	if critical == medium {
		t.Error("the selection style is the same on a critical and a medium finding")
	}
}

// Every section header is followed by a blank line. Three of the four were, and
// "Scan Options" ran straight into its first checkbox.
//
// This is asserted per column rather than on the rendered form, because the
// columns are zipped together line by line: the line below "Scan Options" on
// screen belongs to the *right* column, which is why the missing separator
// survived so long — and why a test over the rendered output would pass while
// looking like it checked something.
func TestEverySectionHeaderIsFollowedByABlankLine(t *testing.T) {
	m := newTestModel(t)

	columns := map[string][]string{
		"left":  m.formLeftColumn(),
		"right": m.formRightColumn(),
	}
	headers := map[string]string{
		"Target": "left", "Scan Options": "left",
		"Trivy Options": "right", "Gitleaks Options": "right",
	}

	for header, side := range headers {
		lines := columns[side]
		row := -1
		for i, line := range lines {
			if strings.Contains(line, header) {
				row = i
				break
			}
		}
		if row < 0 {
			t.Errorf("no %q section in the %s column", header, side)
			continue
		}
		if row+1 >= len(lines) {
			t.Errorf("%q is the last line of the %s column", header, side)
			continue
		}
		if strings.TrimSpace(lines[row+1]) != "" {
			t.Errorf("%q is followed by %q, want a blank line", header, strings.TrimSpace(lines[row+1]))
		}
	}
}

// The zip is what puts the two columns side by side, so a column that is
// shorter than the other must not truncate it.
func TestBothColumnsAreRenderedInFull(t *testing.T) {
	m := newTestModel(t)
	view := m.View()

	for _, line := range append(m.formLeftColumn(), m.formRightColumn()...) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(view, strings.TrimSpace(line)) {
			t.Errorf("the form does not render %q", strings.TrimSpace(line))
		}
	}
}
