package security

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A failed stage does not reach the viewport. It used to be a panel that
// replaced the whole table, so a plumber failure hid every CVE and every secret
// the same scan had found — and folding it to a banner above the table only
// moved the problem to all five tabs at once.
func TestAFailedStageIsNotRenderedInTheViewport(t *testing.T) {
	result := resultFixture()
	result.Errors = []string{
		"plumber: plumber failed: exit status 2: Error: --project is required (could not auto-detect from git remote)",
	}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})

	for tab := range tabCount {
		m.switchTab(tab)
		view := plain(m.renderResultsView())
		if strings.Contains(view, "Scan Warnings") || strings.Contains(view, "--project is required") {
			t.Errorf("tab %d renders the failure in the viewport:\n%s", tab, view)
		}
	}
}

// It reaches the footer instead, with the level of what happened and a pointer
// to where the reason is.
func TestTheFooterNamesTheFailedStageAndSendsToTheLogs(t *testing.T) {
	result := resultFixture()
	result.Errors = []string{"plumber: plumber failed: exit status 2"}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})

	status := m.status()
	if !strings.Contains(status.Text, "plumber") {
		t.Errorf("the footer does not name the stage: %q", status.Text)
	}
	if !strings.Contains(status.Text, "check logs") {
		t.Errorf("the footer does not say where the reason is: %q", status.Text)
	}
	// The tool's own stderr is not folded into one line; it is in the log.
	if strings.Contains(status.Text, "exit status 2") {
		t.Errorf("the footer carries the tool's stderr: %q", status.Text)
	}
	if !strings.Contains(plain(m.RenderFooter(160)), "check logs") {
		t.Error("the rendered footer does not carry the status")
	}
}

// It is a state and not an event: a message would expire after three seconds
// and the user would be left with a result that looks complete (Rule 128).
func TestTheFailureSurvivesInTheFooterRatherThanExpiring(t *testing.T) {
	result := resultFixture()
	result.Errors = []string{"gitleaks: gitleaks failed"}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})

	// Whatever the footer's own message does, the status is derived per frame.
	m.footer.Clear()
	if !strings.Contains(m.status().Text, "gitleaks failed") {
		t.Errorf("the failure is gone from the footer: %q", m.status().Text)
	}
}

// Several failures are named together rather than one of them standing for the
// rest.
func TestEveryFailedStageIsNamed(t *testing.T) {
	if got := failedStages([]string{"plumber: boom", "gitleaks: boom"}); got != "plumber, gitleaks failed" {
		t.Errorf("failedStages = %q", got)
	}
	if got := failedStages([]string{"trivy vuln: boom"}); got != "trivy vuln failed" {
		t.Errorf("failedStages = %q", got)
	}
}

// The findings are browsable: the tab bar stays, and the table is what the
// results state shows whether or not a stage failed.
func TestAFailedStageDoesNotHideWhatTheOthersFound(t *testing.T) {
	result := resultFixture()
	result.Errors = []string{"plumber: plumber failed"}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})

	if !strings.Contains(plain(m.View()), "CVE-2026-0001") {
		t.Errorf("the findings are hidden by a failure that is not about them:\n%s", plain(m.View()))
	}
	if !m.showsResultTabs() {
		t.Error("the tab bar is hidden although there are findings to browse")
	}
}

// A scan where everything failed shows an empty table rather than a panel: the
// footer says what happened, and an empty table is what "nothing was found"
// looks like everywhere else in the application.
func TestAScanThatFoundNothingStillShowsItsTable(t *testing.T) {
	result := resultFixture()
	result.Findings = nil
	result.CountFindings()
	result.Errors = []string{"trivy vuln: trivy failed"}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})

	if !m.showsResultTabs() {
		t.Error("the tab bar is gone, so there is no way to move around the result")
	}
	if !strings.Contains(m.status().Text, "trivy vuln failed") {
		t.Errorf("the footer does not say why the table is empty: %q", m.status().Text)
	}
}

// Nothing to report is nothing said: a clean result leaves the line to whatever
// message is in flight.
func TestACleanResultSaysNothingInTheFooter(t *testing.T) {
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, resultFixture()), tea.WindowSizeMsg{Width: 160, Height: 30})

	if got := m.status(); got.Text != "" {
		t.Errorf("a clean result posts %q", got.Text)
	}
}
