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

// The grade has no line of its own any more. It cost the CI tab two of its
// rows to state a letter the inventory's CI column already carries per target,
// and it was the only tab that showed fewer findings than its neighbours.
func TestTheCITabShowsNoScoreLine(t *testing.T) {
	m := ciModel(t, ciResult(func(r *scan.Result) {
		r.CIScanned, r.CIScore, r.CIPoints = true, "C", 61
	}))

	m.switchTab(TabCIScore)
	view := plain(m.renderResultsView())
	for _, forbidden := range []string{"61.0/100", "Score"} {
		if strings.Contains(view, forbidden) {
			t.Errorf("the CI tab still carries %q", forbidden)
		}
	}
}

// The five tabs get the same table height. The CI tab used to lose two rows,
// so the layout jumped on every switch onto it and back.
func TestEveryTabGivesTheTableTheSameHeight(t *testing.T) {
	m := ciModel(t, ciResult(func(r *scan.Result) {
		r.CIScanned, r.CIScore, r.CIPoints = true, "C", 61
	}))

	m.switchTab(TabCVE)
	m.resizeFindings()
	want := m.findingsTable.Table().Height()

	for _, tab := range []int{TabSecrets, TabLicense, TabMisconfig, TabCIScore} {
		m.switchTab(tab)
		m.resizeFindings()
		if got := m.findingsTable.Table().Height(); got != want {
			t.Errorf("tab %d gives the table %d rows, want %d", tab, got, want)
		}
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
