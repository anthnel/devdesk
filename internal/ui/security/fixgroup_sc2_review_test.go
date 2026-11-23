package security

import (
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-754 (SC2 review): the grouped list of one tab keeps its sort and its
// cursor across a tour of the tabs; a finding with no file is "(image)" only on an
// image; the keys that act on one finding say why they do nothing in the group
// level; a group that mixes two spellings of a rule shows one of them whatever
// the order of the scan.

// bothFindings is a result with CVE groups and Misconfigurations groups.
func bothFindings() []scan.Finding {
	return append(groupFindings(), mgFindings()...)
}

func bothModel(t *testing.T) Model {
	t.Helper()
	return groupModelOf(t, bothFindings(), 160)
}

// aFullTourOfTheTabs presses tab once per tab, ending where it started.
func aFullTourOfTheTabs(t *testing.T, m Model) Model {
	t.Helper()
	for range tabCount {
		m = feed(t, m, testutil.Key("tab"))
	}
	return m
}

func TestTheGroupedListsKeepTheirSortAndCursorAcrossATourOfTheTabs(t *testing.T) {
	m := bothModel(t)
	m = feed(t, m, testutil.Key(groupToggleKey), testutil.Key("."), testutil.Key("down"))
	cveCol, cveDesc := m.groups.SortState()
	if cveCol < 0 {
		t.Fatal(". did not sort the CVE groups")
	}
	cveKey := selectedGroup(t, m).Key
	cveCursor := m.groups.Cursor()

	m = onTab(t, m, TabMisconfig)
	m = feed(t, m, testutil.Key(groupToggleKey), testutil.Key("."), testutil.Key("."), testutil.Key("down"))
	mgCol, mgDesc := m.groups.SortState()
	mgKey := selectedGroup(t, m).Key
	mgCursor := m.groups.Cursor()

	m = onTab(t, m, TabCVE)
	m = aFullTourOfTheTabs(t, m)
	if col, desc := m.groups.SortState(); col != cveCol || desc != cveDesc {
		t.Errorf("the CVE groups' sort went from (%d,%v) to (%d,%v) after a tour", cveCol, cveDesc, col, desc)
	}
	if g := selectedGroup(t, m); g.Key != cveKey || m.groups.Cursor() != cveCursor {
		t.Errorf("the CVE cursor went from %d (%v) to %d (%v) after a tour", cveCursor, cveKey, m.groups.Cursor(), g.Key)
	}

	m = onTab(t, m, TabMisconfig)
	if col, desc := m.groups.SortState(); col != mgCol || desc != mgDesc {
		t.Errorf("the Misconfigurations groups' sort went from (%d,%v) to (%d,%v)", mgCol, mgDesc, col, desc)
	}
	if g := selectedGroup(t, m); g.Key != mgKey || m.groups.Cursor() != mgCursor {
		t.Errorf("the Misconfigurations cursor went from %d (%v) to %d (%v)", mgCursor, mgKey, m.groups.Cursor(), g.Key)
	}
}

func TestAFlatMisconfigurationsTabDoesNotResetTheCVEGroupedList(t *testing.T) {
	m := bothModel(t)
	m = feed(t, m, testutil.Key(groupToggleKey), testutil.Key("."), testutil.Key("down"))
	col, desc := m.groups.SortState()
	key := selectedGroup(t, m).Key
	m = onTab(t, m, TabMisconfig)
	if m.isGrouped() {
		t.Fatal("the Misconfigurations tab is grouped")
	}
	m = onTab(t, m, TabCVE)
	if c, d := m.groups.SortState(); c != col || d != desc || selectedGroup(t, m).Key != key {
		t.Errorf("the CVE groups lost their sort or cursor passing through a flat tab: (%d,%v) -> (%d,%v)", col, desc, c, d)
	}
}

// ── "(image)" is said of an image only ───────────────────────────────────────

func TestAFindingWithNoFileIsNamedAfterTheTarget(t *testing.T) {
	findings := []scan.Finding{mgFinding("DS002", "", "stage build", mgHigh)}
	if got := misconfigGroups(findings, false)[0].Subject; got != "(image)" {
		t.Errorf("on an image the File column says %q, want (image)", got)
	}
	got := misconfigGroups(findings, true)[0].Subject
	if got != "(no file)" {
		t.Errorf("on a directory the File column says %q, want (no file)", got)
	}
	requireTerminalSafe(t, "Subject", got)
}

// ── The group level says why a key does nothing ──────────────────────────────

func TestKeysThatActOnOneFindingAreRefusedInTheGroupLevelWithAReason(t *testing.T) {
	m := feed(t, groupedModel(t, 160), testutil.Key("enter"))
	if m.state != StateGroup {
		t.Fatalf("enter: state %v", m.state)
	}
	for _, key := range []string{writeRemediationKey, keymap.Space, keymap.Scan} {
		next, _ := step(t, m, testutil.Key(key))
		if next.footer.Text() == "" || next.footer.Level() != components.LevelWarning {
			t.Errorf("%s in the group level: footer = %q (level %v), want a Warn", key, next.footer.Text(), next.footer.Level())
		}
		if next.state != StateGroup || next.confirmModal != nil {
			t.Errorf("%s changed the screen: state %v", key, next.state)
		}
	}
}

// ── One spelling of a rule, whatever the order of the scan ───────────────────

func TestAMixedGroupShowsTheSameRuleIDInAnyOrder(t *testing.T) {
	a := mgFinding("AVD-DS-0002", "Dockerfile", "a", mgHigh)
	b := mgFinding("DS002", "Dockerfile", "b", mgHigh)
	for _, findings := range [][]scan.Finding{{a, b}, {b, a}} {
		groups := misconfigGroups(findings, true)
		if len(groups) != 1 {
			t.Fatalf("%d groups, want 1 for two spellings of one rule", len(groups))
		}
		if groups[0].Rule != "AVD-DS-0002" {
			t.Errorf("Rule = %q for %v, want the smallest ID, AVD-DS-0002", groups[0].Rule, slices.Collect(func(yield func(string) bool) {
				for _, f := range findings {
					if !yield(f.ID) {
						return
					}
				}
			}))
		}
	}
}
