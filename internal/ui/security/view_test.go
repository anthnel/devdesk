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

	m := feed(t, NewWithPreloadedResult(testConfig(), result), testutil.Resize(160, 30))

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
		for _, row := range m.findingsTable.Table().Rows() {
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
		{"the inventory", func(t *testing.T) Model { return inventoryModel(t, inventoryFixtures()...) }},
		{"the inventory, filtering", func(t *testing.T) Model {
			return feed(t, inventoryModel(t, inventoryFixtures()...), testutil.Key("/"))
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
//
// This used to loop over three widths and `continue` when the sum was wrong,
// which asserted nothing at all.
func TestColumnsFitTheWidth(t *testing.T) {
	const titleColumn = 2

	for _, width := range []int{50, 80, 120, 200} {
		m := feed(t, scannedModel(t), testutil.Resize(width, 30))

		total := 0
		for _, col := range m.findingsTable.Table().Columns() {
			total += col.Width
			if col.Width < 0 {
				t.Errorf("at width %d, column %q is %d wide", width, col.Title, col.Width)
			}
		}
		if want := width - 2 - numColumns*2; total != want {
			t.Errorf("at width %d the columns total %d, want %d", width, total, want)
		}
	}

	narrow := feed(t, scannedModel(t), testutil.Resize(60, 30))
	wide := feed(t, scannedModel(t), testutil.Resize(200, 30))
	if wide.findingsTable.Table().Columns()[titleColumn].Width <= narrow.findingsTable.Table().Columns()[titleColumn].Width {
		t.Error("the title column did not grow with the terminal")
	}
}

// A long detail line from a tool must not push the stage row past the terminal
// edge, and truncation counts columns rather than bytes.
func TestEverySeverityRendersDistinctly(t *testing.T) {
	withTrueColor(t)
	m := inventoryModel(t, inventoryFixtures()...)

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

	if got := inventoryModel(t, inventoryFixtures()...).getSeverityStyle("NONSENSE").Render("x"); !strings.Contains(got, "\x1b") {
		t.Errorf("an unknown severity rendered unstyled: %q", got)
	}
}

// Rule 118: the selected row turns red on a critical finding, so the severity
// is visible without reading the cell.
func TestSelectionStyleFollowsTheSeverity(t *testing.T) {
	withTrueColor(t)

	first := scannedModel(t)
	critical := first.findingsTable.View()

	second := feed(t, scannedModel(t), testutil.Key("down"))
	medium := second.findingsTable.View()

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
