package security

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

func ciResult(mutate func(*scan.Result)) *scan.Result {
	r := &scan.Result{
		Target: "/repos/devdesk", TargetType: scan.TargetDirectory,
		Findings: []scan.Finding{{
			ID: "ISSUE-501", Title: "ISSUE-501: branchMustBeProtected",
			Severity: scan.SeverityCritical, Source: scan.SourcePlumber,
		}},
	}
	if mutate != nil {
		mutate(r)
	}
	r.CountFindings()
	return r
}

func ciModel(t *testing.T, result *scan.Result) Model {
	t.Helper()
	return feed(t, NewWithPreloadedResult(testConfig(), nil, result),
		tea.WindowSizeMsg{Width: 160, Height: 30})
}

// The grade is not a finding, so it has no row: it goes above the table, on its
// own tab.
func TestTheScoreLineStatesTheGradeOnTheCITab(t *testing.T) {
	m := ciModel(t, ciResult(func(r *scan.Result) {
		r.CIScanned, r.CIScore, r.CIPoints = true, "C", 61
	}))

	line := plain(m.renderCIScoreLine(160))
	if !strings.Contains(line, "C") || !strings.Contains(line, "61/100") {
		t.Errorf("the score line does not carry the grade: %q", line)
	}
	// Rule 120: the separator is the chevron, never a colon.
	if strings.Contains(line, "Score:") {
		t.Errorf("the score line uses a colon: %q", line)
	}
}

// The one that matters. plumber writes a letter on a withheld run and it
// flatters — a control that did not run found nothing — so the line says why
// instead of quoting it.
func TestAWithheldRunSaysWhyRatherThanShowingALetter(t *testing.T) {
	m := ciModel(t, ciResult(func(r *scan.Result) {
		r.CIScanned, r.CIWithheld = true, true
		r.CIReasons = []string{"branch protection could not be fetched"}
	}))

	line := plain(m.renderCIScoreLine(160))
	if !strings.Contains(line, "withheld") {
		t.Errorf("a withheld run does not say so: %q", line)
	}
	if !strings.Contains(line, "branch protection could not be fetched") {
		t.Errorf("the reason is missing: %q", line)
	}
}

// A repository with no pipeline has not scored badly; it has nothing to score.
func TestARepositoryWithNoPipelineSaysSoRatherThanScoringZero(t *testing.T) {
	m := ciModel(t, ciResult(func(r *scan.Result) {
		r.CIScanned, r.CIMissing = true, true
	}))

	line := plain(m.renderCIScoreLine(160))
	if !strings.Contains(line, "no pipeline") {
		t.Errorf("a repository with no CI does not say so: %q", line)
	}
	if strings.Contains(line, "/100") {
		t.Errorf("a points figure appeared for a repository with no pipeline: %q", line)
	}
}

func TestAResultNothingGradedSaysNotGraded(t *testing.T) {
	line := plain(ciModel(t, ciResult(nil)).renderCIScoreLine(160))

	if !strings.Contains(line, "not graded") {
		t.Errorf("an ungraded result does not say so: %q", line)
	}
}

// The line is on the CI tab and nowhere else: a fact about one stage while the
// other four are being read is what Rule 134 argues against, one layer down.
func TestTheScoreLineAppearsOnTheCITabAlone(t *testing.T) {
	m := ciModel(t, ciResult(func(r *scan.Result) {
		r.CIScanned, r.CIScore, r.CIPoints = true, "C", 61
	}))

	for _, tab := range []int{TabCVE, TabSecrets, TabLicense, TabMisconfig} {
		m.switchTab(tab)
		if strings.Contains(plain(m.renderResultsView()), "61/100") {
			t.Errorf("tab %d shows the CI score", tab)
		}
	}

	m.switchTab(TabCIScore)
	if !strings.Contains(plain(m.renderResultsView()), "61/100") {
		t.Error("the CI tab does not show the score")
	}
}

// The tab label carries the issue count like its four neighbours. A letter
// there would break the only column the tab bar keeps aligned, and the grade
// has a line of its own below.
func TestTheCITabLabelCarriesACountRatherThanTheLetter(t *testing.T) {
	m := ciModel(t, ciResult(func(r *scan.Result) {
		r.CIScanned, r.CIScore = true, "C"
	}))

	tabs := plain(m.renderTabs())
	if !strings.Contains(tabs, "CI (1)") {
		t.Errorf("the tab bar does not carry the CI count: %q", tabs)
	}
	if strings.Contains(tabs, "CI (C)") {
		t.Errorf("the tab bar carries the letter: %q", tabs)
	}
}

// A plumber finding reaches the CI tab and no other, which is what classifying
// on the source alone buys (§3.12).
func TestAPlumberFindingLandsInTheCITab(t *testing.T) {
	m := ciModel(t, ciResult(func(r *scan.Result) { r.CIScanned = true }))

	m.switchTab(TabCIScore)
	items := m.findingsTable.Visible()
	if len(items) != 1 || items[0].Source != scan.SourcePlumber {
		t.Fatalf("the CI tab holds %+v, want the one plumber finding", items)
	}

	for _, tab := range []int{TabCVE, TabSecrets, TabLicense, TabMisconfig} {
		m.switchTab(tab)
		if len(m.findingsTable.Visible()) != 0 {
			t.Errorf("tab %d also shows the plumber finding", tab)
		}
	}
}

// tab reaches the fifth one: the numeric jumps are gone, so cycling is the only
// way in.
func TestTabReachesTheCITab(t *testing.T) {
	m := ciModel(t, ciResult(func(r *scan.Result) { r.CIScanned = true }))

	for range tabCount - 1 {
		m = feed(t, m, testutil.Key("tab"))
	}
	if m.activeTab != TabCIScore {
		t.Errorf("four tabs from the first landed on %d, want the CI tab", m.activeTab)
	}
}

// plain returns a rendering with its escape sequences removed, so an assertion
// compares what is on screen rather than how it is coloured.
func plain(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}
