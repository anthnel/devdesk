package security

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
)

// tabBarModel is a results view whose five finding tabs each hold n findings,
// and whose Remediation tab, once discovered, counts n base images: the widest
// tab bar the view can draw for a given digit count.
func tabBarModel(t *testing.T, n int, discovered bool) Model {
	t.Helper()
	sources := []string{
		scan.SourceTrivy, scan.SourceGitleaks, scan.SourceTrivyLicense,
		scan.SourceTrivyMisconfig, scan.SourcePlumber,
	}
	r := &scan.Result{Target: "/repos/devdesk", TargetType: scan.TargetDirectory}
	for _, src := range sources {
		for i := range n {
			r.Findings = append(r.Findings, scan.Finding{
				ID: fmt.Sprintf("%s-%d", src, i), Source: src, Severity: scan.SeverityHigh,
			})
		}
	}
	r.CountFindings()
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, r), tea.WindowSizeMsg{Width: 160, Height: 30})
	if discovered {
		m.remediation.phase = phaseReady
		for i := range n {
			m.remediation.entries = append(m.remediation.entries,
				remediation.Entry{File: "Dockerfile", Image: fmt.Sprintf("img:%d", i)})
		}
	}
	return m
}

// tabBarLine is the footer line that carries the tab bar: the third from the
// bottom, above the blank line and the info line (Rule 124).
func tabBarLine(t *testing.T, m Model, width int) string {
	t.Helper()
	footer := strings.Split(m.RenderFooter(width), "\n")
	if len(footer) != m.GetFooterHeight() {
		t.Fatalf("the footer is %d lines, GetFooterHeight says %d (Rule 124)", len(footer), m.GetFooterHeight())
	}
	return footer[len(footer)-3]
}

var tabBarNames = []string{"CVE", "Secrets", "Licenses", "Misconfig", "CI", "Remediation"}

// The tab bar is one line that fits the window, whatever the counts and
// whichever tab is open. Nothing cuts a line the view draws too wide: the
// router never crops, and a line as wide as the screen leaves every other line
// unpadded, so the bar has to fit by itself (DEVDESK-765).
func TestTheTabBarFitsTheWindow(t *testing.T) {
	for _, width := range []int{80, 120, 175} {
		for _, n := range []int{1, 12, 123} {
			for _, discovered := range []bool{false, true} {
				m := tabBarModel(t, n, discovered)
				for tab := TabCVE; tab <= TabRemediation; tab++ {
					m.activeTab = tab
					t.Run(fmt.Sprintf("width %d count %d discovered %v tab %d", width, n, discovered, tab), func(t *testing.T) {
						line := tabBarLine(t, m, width)
						if w := ansi.StringWidth(line); w > width {
							t.Errorf("the tab bar is %d cells wide, the window holds %d: %q", w, width, ansi.Strip(line))
						}
					})
				}
			}
		}
	}
}

// There are always six tabs, each named in full: when room is short the counts
// go, not the tabs and not their names. The open tab keeps its count, which with
// its bold is what marks it without relying on colour.
func TestTheTabBarKeepsSixNamedTabsAndTheOpenCount(t *testing.T) {
	for _, width := range []int{80, 120, 175} {
		for _, n := range []int{1, 12, 123} {
			m := tabBarModel(t, n, true)
			for tab := TabCVE; tab <= TabRemediation; tab++ {
				m.activeTab = tab
				t.Run(fmt.Sprintf("width %d count %d tab %d", width, n, tab), func(t *testing.T) {
					got := ansi.Strip(tabBarLine(t, m, width))
					for _, name := range tabBarNames {
						if !strings.Contains(got, " "+name+" ") && !strings.Contains(got, " "+name+" (") {
							t.Errorf("the tab bar lost %q: %q", name, got)
						}
					}
					open := fmt.Sprintf(" %s (%d) ", tabBarNames[tab], n)
					if !strings.Contains(got, open) {
						t.Errorf("the open tab lost its count %q: %q", open, got)
					}
					if width >= 120 && strings.Count(got, fmt.Sprintf("(%d)", n)) != len(tabBarNames) {
						t.Errorf("at %d columns every tab keeps its count: %q", width, got)
					}
				})
			}
		}
	}
}

// At 80 columns the two-digit and three-digit bars drop the counts of the tabs
// that are not open and nothing else.
func TestTheTabBarHidesTheInactiveCountsWhenRoomIsShort(t *testing.T) {
	m := tabBarModel(t, 123, true)
	m.activeTab = TabSecrets
	got := ansi.Strip(tabBarLine(t, m, 80))
	if n := strings.Count(got, "("); n != 1 {
		t.Errorf("%d counts shown, want only the open tab's: %q", n, got)
	}
	if !strings.Contains(got, " Secrets (123) ") {
		t.Errorf("the open tab lost its count: %q", got)
	}
	for _, name := range tabBarNames {
		if !strings.Contains(got, name) {
			t.Errorf("a label was shortened, %q is missing: %q", name, got)
		}
	}
}

// The compact bar can leave no count at all, when the open tab is Remediation
// before its discovery has finished. The open tab is then marked by a text
// marker, so it is never told apart by bold and colour alone.
func TestTheCompactTabBarMarksTheOpenTabWithoutStyle(t *testing.T) {
	m := tabBarModel(t, 12, false)
	m.activeTab = TabRemediation
	line := tabBarLine(t, m, 80)
	got := ansi.Strip(line)
	if strings.Contains(got, "(") {
		t.Fatalf("this case is meant to have no count at all: %q", got)
	}
	if !strings.Contains(got, compactOpenMarker+" Remediation ") {
		t.Errorf("the open tab is not marked in the compact bar: %q", got)
	}
	if n := strings.Count(got, compactOpenMarker); n != 1 {
		t.Errorf("%d markers, want one: %q", n, got)
	}
}

// Every tab, open in turn, gets exactly one marker when the bar is compact and
// none when the full bar fits; the compact bar stays within the window.
func TestTheCompactTabBarMarkerIsOnTheOpenTabOnly(t *testing.T) {
	for _, width := range []int{80, 120, 175} {
		for _, n := range []int{1, 12, 123} {
			for _, discovered := range []bool{false, true} {
				m := tabBarModel(t, n, discovered)
				for tab := TabCVE; tab <= TabRemediation; tab++ {
					m.activeTab = tab
					t.Run(fmt.Sprintf("width %d count %d discovered %v tab %d", width, n, discovered, tab), func(t *testing.T) {
						line := tabBarLine(t, m, width)
						got := ansi.Strip(line)
						if w := ansi.StringWidth(line); w > width {
							t.Errorf("the bar is %d cells wide, the window holds %d: %q", w, width, got)
						}
						markers := strings.Count(got, compactOpenMarker)
						compact := strings.Count(got, "(") <= 1 // the full bar shows five counts or more
						if compact && markers != 1 {
							t.Errorf("%d markers in a compact bar, want one: %q", markers, got)
						}
						if markers == 1 && !strings.Contains(got, compactOpenMarker+" "+tabBarNames[tab]+" ") {
							t.Errorf("the marker is not on the open tab %q: %q", tabBarNames[tab], got)
						}
						if markers > 1 {
							t.Errorf("%d markers: %q", markers, got)
						}
					})
				}
			}
		}
	}
}
