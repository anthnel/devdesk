package security

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-754 (SC2, step 8): `g` on the Misconfigurations tab. One row per file and
// per fix, a plain count of findings, the Fix in the closed vocabulary of 751. The
// mode is held per tab: the CVE tab and this one group independently. The keys are
// the same grouped or not (Rule 130). Footer timers are never run: the footer is
// read through Text and Level.
//
// Hypotheses frozen here, besides those of fixgroup_misconfig_test.go:
//   - m.groups, m.groupsAll and m.groupTable hold the groups of the active tab,
//     whichever it is (the tests read them through the helpers below, the one place
//     to change if the coder keeps one table per tab);
//   - the Fix column shows "Built-in fix unavailable" whole from 120 columns up;
//   - the footer sentence of a group names the count of findings and the Fix label,
//     and never says closed, cleared, fixed or done.

// ── Fixtures ─────────────────────────────────────────────────────────────────

// mgDir is a scanned directory holding the Dockerfile the built-in fix writes.
func mgDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(fixableDockerfile), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// mgResultModel is the results of findings on the CVE tab, laid out, flat.
func mgResultModel(t *testing.T, findings []scan.Finding, width int) Model {
	t.Helper()
	m := NewWithPreloadedResult(testConfig(), nil, resultOn(mgDir(t), findings...))
	return feed(t, m, testutil.Resize(width, 30))
}

// mgModelOf is mgResultModel on the Misconfigurations tab, flat.
func mgModelOf(t *testing.T, findings []scan.Finding, width int) Model {
	t.Helper()
	return onTab(t, mgResultModel(t, findings, width), TabMisconfig)
}

func mgModel(t *testing.T, width int) Model { return mgModelOf(t, mgAllFindings(), width) }

// mgGroupedOf is mgModelOf with `g` pressed once.
func mgGroupedOf(t *testing.T, findings []scan.Finding, width int) Model {
	t.Helper()
	m := feed(t, mgModelOf(t, findings, width), testutil.Key(groupToggleKey))
	if !m.isGrouped() || m.activeTab != TabMisconfig {
		t.Fatalf("g did not group the Misconfigurations tab (grouped %v, tab %d)", m.isGrouped(), m.activeTab)
	}
	return m
}

func mgGrouped(t *testing.T, width int) Model { return mgGroupedOf(t, mgAllFindings(), width) }

// mgImageModelOf is the Misconfigurations tab of an image: its findings name no
// file, and nothing can be written.
func mgImageModelOf(t *testing.T, findings []scan.Finding, width int) Model {
	t.Helper()
	result := &scan.Result{Target: "nginx:1.25", TargetType: scan.TargetImage, Findings: findings}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), testutil.Resize(width, 30))
	return onTab(t, m, TabMisconfig)
}

func mgImageFindings() []scan.Finding {
	return []scan.Finding{
		mgFinding("DS002", "", "stage build", mgHigh),
		mgFinding("DS026", "", "no healthcheck", mgLow),
		mgFinding("DS002", "", "stage final", mgHigh),
	}
}

// mgRows are the rows of the grouped list on screen.
func mgRows(m Model) []mgRow { return rowsOf(m.groups.Visible()) }

// cursorOnGroupWhere moves the cursor of the grouped list to the first group that
// satisfies pred: to the top, then down.
func cursorOnGroupWhere(t *testing.T, m Model, desc string, pred func(fixGroup) bool) Model {
	t.Helper()
	m = feed(t, m, testutil.Key("home"))
	for range 14 {
		if g, ok := m.groups.Selected(); ok && pred(g) {
			return m
		}
		m = feed(t, m, testutil.Key("down"))
	}
	t.Fatalf("the cursor never reached %s (groups: %v)", desc, mgRows(m))
	return m
}

func mgCursor(t *testing.T, m Model, file, rule, label string) Model {
	t.Helper()
	return cursorOnGroupWhere(t, m, file+" "+rule+" "+label, func(g fixGroup) bool {
		return g.Subject == file && g.Rule == rule && g.Status.Label() == label
	})
}

// mgCursorOnFinding moves the cursor of the findings table to the message.
func mgCursorOnFinding(t *testing.T, m Model, message string) Model {
	t.Helper()
	for range 14 {
		if f, ok := m.findingsTable.Selected(); ok && f.Message == message {
			return m
		}
		m = feed(t, m, testutil.Key("down"))
	}
	t.Fatalf("the cursor never reached %q", message)
	return m
}

// mgRuleFiles lists the groups as "rule@file", sorted.
func mgRuleFiles(m Model) []string {
	var out []string
	for _, g := range m.groups.Visible() {
		out = append(out, g.Rule+"@"+g.Subject)
	}
	sort.Strings(out)
	return out
}

func sorted(in []string) []string {
	out := slices.Clone(in)
	sort.Strings(out)
	return out
}

const (
	labelAvailable = "Built-in fix available"
	labelNoGuide   = "No guidance available"
)

// ── The key ──────────────────────────────────────────────────────────────────

func TestGroupIsEnabledOnTheMisconfigTabAndNamesTheModeItSwitchesTo(t *testing.T) {
	m := mgModel(t, 160)
	if a := m.canGroup(); !a.Enabled() {
		t.Fatalf("canGroup = %+v on the Misconfigurations tab", a)
	}
	if !testutil.ShortcutEnabled(m.GetShortcuts(), groupToggleKey) {
		t.Fatal("g is greyed on the Misconfigurations tab")
	}
	if got := groupShortcutWord(t, m, groupToggleKey); got != "Group by fix" {
		t.Errorf("flat: g reads %q, want Group by fix", got)
	}
	m = feed(t, m, testutil.Key(groupToggleKey))
	if !m.isGrouped() {
		t.Fatal("g did not group the tab")
	}
	if !testutil.ShortcutEnabled(m.GetShortcuts(), groupToggleKey) {
		t.Fatal("g is greyed in the grouped list: there would be no way back")
	}
	if got := groupShortcutWord(t, m, groupToggleKey); got != "List by finding" {
		t.Errorf("grouped: g reads %q, want List by finding", got)
	}
	m = feed(t, m, testutil.Key(groupToggleKey))
	if m.isGrouped() {
		t.Error("a second g did not bring the findings back")
	}
}

// The tab names the two places `g` covers, and the other four still refuse with it.
func TestTheReasonOfAGreyedGroupKeyNamesBothTabs(t *testing.T) {
	for _, word := range []string{"CVE", "Misconfig"} {
		if !strings.Contains(reasonGroupNotHere, word) {
			t.Errorf("%q does not name %q", reasonGroupNotHere, word)
		}
	}
	for _, tab := range []int{TabSecrets, TabLicense, TabCIScore, TabRemediation} {
		t.Run(fmt.Sprintf("tab %d", tab), func(t *testing.T) {
			m := onTab(t, mgModel(t, 160), tab)
			if a := m.canGroup(); a.Enabled() || a.Reason != reasonGroupNotHere {
				t.Errorf("canGroup = %+v, want %q", a, reasonGroupNotHere)
			}
			m, _ = step(t, m, testutil.Key(groupToggleKey))
			if m.isGrouped() || m.footer.Text() != reasonGroupNotHere || m.footer.Level() != components.LevelWarning {
				t.Errorf("grouped %v, footer %q (level %v), want the Warn %q", m.isGrouped(), m.footer.Text(), m.footer.Level(), reasonGroupNotHere)
			}
		})
	}
}

// A result with no misconfiguration has nothing to group on this tab, and the
// reason says misconfigurations, not vulnerabilities. The CVE tab is unaffected.
func TestTheMisconfigTabWithNoMisconfigurationHasNothingToGroup(t *testing.T) {
	m := mgModelOf(t, []scan.Finding{cve("CVE-1", "openssl", "alpine", "1.1.1k", "1.1.1w", mgHigh)}, 160)
	a := m.canGroup()
	if a.Enabled() {
		t.Fatal("g is offered on a Misconfigurations tab with no misconfiguration")
	}
	if !strings.Contains(strings.ToLower(a.Reason), "misconfiguration") || a.Reason == reasonNothingToGroup || a.Reason == reasonGroupNotHere {
		t.Errorf("reason = %q, want one that names misconfigurations", a.Reason)
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), groupToggleKey) {
		t.Error("g is lit")
	}
	m, _ = step(t, m, testutil.Key(groupToggleKey))
	if m.isGrouped() || m.footer.Text() != a.Reason || m.footer.Level() != components.LevelWarning {
		t.Errorf("grouped %v, footer %q (level %v), want the Warn %q", m.isGrouped(), m.footer.Text(), m.footer.Level(), a.Reason)
	}
	if cveTab := onTab(t, m, TabCVE); !cveTab.canGroup().Enabled() {
		t.Error("the CVE tab lost g because the other tab has nothing to group")
	}
}

// A search that hides every misconfiguration does not make them vanish.
func TestAMisconfigSearchThatMatchesNothingKeepsGroupEnabled(t *testing.T) {
	m := feed(t, mgModel(t, 160), testutil.Key("/"))
	m = feed(t, m, testutil.Type("zzzz")...)
	m = feed(t, m, testutil.Key("enter"))
	if len(m.findingsTable.Visible()) != 0 {
		t.Fatalf("the search left %d findings", len(m.findingsTable.Visible()))
	}
	if !m.canGroup().Enabled() {
		t.Errorf("canGroup = %+v", m.canGroup())
	}
}

// Rule 130: one list of keys on every tab, grouped or not.
func TestTheResultsColumnHoldsTheSameKeysOnTheMisconfigTabGroupedOrNot(t *testing.T) {
	flat := mgModel(t, 160)
	want := testutil.ShortcutKeys(flat.GetShortcuts())
	if got := testutil.ShortcutKeys(mgGrouped(t, 160).GetShortcuts()); !slices.Equal(got, want) {
		t.Errorf("grouped: the column is %v, want %v", got, want)
	}
	if got := testutil.ShortcutKeys(mgImageModelOf(t, mgImageFindings(), 160).GetShortcuts()); !slices.Equal(got, want) {
		t.Errorf("image: the column is %v, want %v", got, want)
	}
	cveGrouped := feed(t, mgResultModel(t, mgAllFindings(), 160), testutil.Key(groupToggleKey))
	if got := testutil.ShortcutKeys(cveGrouped.GetShortcuts()); !slices.Equal(got, want) {
		t.Errorf("CVE grouped: the column is %v, want %v", got, want)
	}
	// g is still the third, and ctrl+o keeps its Misconfigurations label.
	if len(want) < 3 || !slices.Equal(want[:3], []string{"tab", "enter", groupToggleKey}) {
		t.Errorf("the column starts %v, want tab, enter, g", want[:min(3, len(want))])
	}
	if got := groupShortcutWord(t, mgGrouped(t, 160), writeRemediationKey); got != "Apply built-in fix" {
		t.Errorf("ctrl+o reads %q in the grouped list, want the tab's label", got)
	}
}

// ── The grouped list ─────────────────────────────────────────────────────────

func TestMisconfigGroupedListShowsOneRowPerFileAndFix(t *testing.T) {
	m := mgModel(t, 160)
	before, cursor := m.findingsTable.Visible(), m.findingsTable.Cursor()
	flatHeader := firstLine(ansi.Strip(m.Render()))
	if !strings.Contains(flatHeader, "Title") || !strings.Contains(flatHeader, "File") || strings.Contains(flatHeader, "Rule") {
		t.Fatalf("the flat table's header is %q", flatHeader)
	}

	m = feed(t, m, testutil.Key(groupToggleKey))
	body := ansi.Strip(m.Render())
	header := firstLine(body)
	for _, title := range []string{"Severity", "File", "Rule", "Findings", "Fix"} {
		if !strings.Contains(header, title) {
			t.Errorf("the grouped header %q lacks %q", header, title)
		}
	}
	for _, title := range []string{"Title", "Package", "Change", "CVEs", "Source"} {
		if strings.Contains(header, title) {
			t.Errorf("the grouped header %q still shows %q", header, title)
		}
	}
	rows := []string{
		`CRITICAL\s+app\.yaml\s+KSV-0003\s+1\s+No guidance available`,
		`HIGH\s+Dockerfile\s+DS002\s+3\s+Built-in fix available`,
		`HIGH\s+svc/Dockerfile\s+DS002\s+1\s+Built-in fix available`,
		`MEDIUM\s+deploy\.yaml\s+KSV-0012\s+3\s+Manual`,
		`MEDIUM\s+pod\.yaml\s+KSV-0104\s+1\s+Built-in fix unavailable: \S`,
		`LOW\s+pod\.yaml\s+KSV-0104\s+1\s+Built-in fix available`,
	}
	for _, pattern := range rows {
		if !regexp.MustCompile(pattern).MatchString(body) {
			t.Errorf("no row matches %s:\n%s", pattern, body)
		}
	}
	if n := len(m.groups.Visible()); n != len(rows) {
		t.Errorf("%d groups, want %d: a CVE or a secret became a row", n, len(rows))
	}
	// The count is a plain number: no word after it that promises anything.
	for _, word := range []string{"closed", "closes", "cleared", "done"} {
		if strings.Contains(strings.ToLower(body), word) {
			t.Errorf("the body says %q: ctrl+o fixes one finding at a time", word)
		}
	}
	if !reflect.DeepEqual(m.findingsTable.Visible(), before) || m.findingsTable.Cursor() != cursor {
		t.Error("the findings table changed when the grouped list opened")
	}
}

// The grouped list is a table either way (Rule 139): nothing to show is a header
// and no rows.
func TestMisconfigGroupedBodyStaysATableWhenNothingMatches(t *testing.T) {
	m := mgGrouped(t, 160)
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("zzzz")...)
	m = feed(t, m, testutil.Key("enter"))
	if len(m.groups.Visible()) != 0 {
		t.Fatalf("groups = %v after a search that matches nothing", mgRows(m))
	}
	body := ansi.Strip(m.Render())
	if !strings.Contains(firstLine(body), "Findings") {
		t.Errorf("the body lost its header: %q", firstLine(body))
	}
	for _, text := range []string{"No ", "no ", "Nothing", "nothing"} {
		if strings.Contains(body, text) {
			t.Errorf("the body carries a message (%q)", text)
		}
	}
	if testutil.ShortcutEnabled(m.GetShortcuts(), "enter") {
		t.Error("enter is offered with no group to open")
	}
}

// ── Filters and search drive the findings table ──────────────────────────────

func TestSeverityTokensStillFilterTheMisconfigGroups(t *testing.T) {
	m := mgGrouped(t, 160)
	m = feed(t, m, testutil.Key("m"))
	want := []mgRow{
		{"deploy.yaml", "KSV-0012", 3, "Manual"},
		{"pod.yaml", "KSV-0104", 1, fixStatusOf(unconfinedFinding(), true).Label()},
	}
	if got := mgRows(m); !reflect.DeepEqual(got, want) {
		t.Errorf("groups under MEDIUM = %v, want %v: a group is listed when one member shows, and its count is what the group holds", got, want)
	}
	m = feed(t, m, testutil.Key("m"))
	if n := len(m.groups.Visible()); n != 6 {
		t.Errorf("%d groups with the token off, want 6", n)
	}
	m = feed(t, m, testutil.Key("l"))
	if got, want := mgRuleFiles(m), []string{"KSV-0012@deploy.yaml", "KSV-0104@pod.yaml"}; !reflect.DeepEqual(got, want) {
		t.Errorf("groups under LOW = %v, want %v (a LOW finding in each)", got, want)
	}
}

func TestSearchStillDrivesTheFindingsTableUnderMisconfigGroups(t *testing.T) {
	m := mgGrouped(t, 160)
	m = feed(t, m, testutil.Key("/"))
	if !m.InEditMode() {
		t.Fatal("/ did not give the keyboard to the search")
	}
	m = feed(t, m, testutil.Type("Dockerfile")...)
	if !m.isGrouped() {
		t.Error("typing in the search left the grouped list")
	}
	m = feed(t, m, testutil.Key("enter"))
	want := []mgRow{{"Dockerfile", "DS002", 3, labelAvailable}, {"svc/Dockerfile", "DS002", 1, labelAvailable}}
	if got := mgRows(m); !reflect.DeepEqual(got, want) {
		t.Errorf("groups for the query = %v, want %v", got, want)
	}
	// A paste into the bar is a query too.
	p := feed(t, mgGrouped(t, 160), testutil.Key("/"), testutil.Paste("pod.yaml"))
	if got := mgRuleFiles(p); !reflect.DeepEqual(got, []string{"KSV-0104@pod.yaml", "KSV-0104@pod.yaml"}) {
		t.Errorf("groups after pasting pod.yaml = %v", got)
	}
}

func TestDotSortsTheMisconfigGroupsAndLeavesTheFindingsSortAlone(t *testing.T) {
	m := mgGrouped(t, 160)
	findingsCol, findingsDesc := m.findingsTable.SortState()
	groupsCol, groupsDesc := m.groups.SortState()
	m = feed(t, m, testutil.Key("."))
	if col, desc := m.groups.SortState(); col == groupsCol && desc == groupsDesc {
		t.Error(". did not sort the groups")
	}
	if col, desc := m.findingsTable.SortState(); col != findingsCol || desc != findingsDesc {
		t.Errorf("the findings table's sort went from (%d,%v) to (%d,%v)", findingsCol, findingsDesc, col, desc)
	}
}

// ── The Pod Security filter (p) ──────────────────────────────────────────────

// A group is listed when at least one member passes the filter, as for the CVEs.
// Under baseline, p hides the restricted rules: KSV-0030, KSV-0104 (absent) and
// the rule read from Trivy's anchor.
func TestThePodSecurityFilterAppliesToTheMisconfigGroups(t *testing.T) {
	m := feed(t, onMisconfig(t, "baseline", pssFixtures()), testutil.Key(groupToggleKey))
	if !m.isGrouped() {
		t.Fatal("g did not group the tab")
	}
	if got, want := mgRuleFiles(m), sorted(allPSSKeys); !reflect.DeepEqual(got, want) {
		t.Fatalf("groups = %v, want one per rule and file: %v", got, want)
	}

	m = feed(t, m, testutil.Key(pssFilterKey))
	if !m.findingsTable.IsTokenActive(pssToken) {
		t.Fatal("p did not drive the findings table's token in the grouped list")
	}
	if got, want := mgRuleFiles(m), sorted(without(allPSSKeys, restrictedPSSKeys...)); !reflect.DeepEqual(got, want) {
		t.Errorf("groups under the filter = %v, want %v", got, want)
	}
	if _, ok := podSecurityHeader(m); !ok {
		t.Error("the Pod Security header left with the grouped list")
	}

	m = feed(t, m, testutil.Key(pssFilterKey))
	if got := len(m.groups.Visible()); got != len(allPSSKeys) {
		t.Errorf("%d groups with the filter off, want %d", got, len(allPSSKeys))
	}

	// The filter belongs to one visit of the tab, and the groups follow it out.
	m = feed(t, m, testutil.Key(pssFilterKey))
	m = onTab(t, onTab(t, m, TabSecrets), TabMisconfig)
	if m.findingsTable.IsTokenActive(pssToken) || len(m.groups.Visible()) != len(allPSSKeys) {
		t.Errorf("coming back: token %v, %d groups: the groups kept a filter the tab dropped", m.findingsTable.IsTokenActive(pssToken), len(m.groups.Visible()))
	}
}

// ── Toggling back ────────────────────────────────────────────────────────────

func TestTogglingBackKeepsTheMisconfigTableAndSelectsTheFirstMemberOfTheGroup(t *testing.T) {
	m := mgModel(t, 160)
	m = feed(t, m, testutil.Key("h")) // a filter state to find again
	tokens := activeTokens(m)
	col, desc := m.findingsTable.SortState()

	m = feed(t, m, testutil.Key(groupToggleKey))
	m = mgCursor(t, m, "Dockerfile", "DS002", labelAvailable)
	m = feed(t, m, testutil.Key(groupToggleKey))
	if m.isGrouped() {
		t.Fatal("g did not bring the findings back")
	}
	if f, ok := m.findingsTable.Selected(); !ok || f.File != "Dockerfile" || f.Message != "stage build" {
		t.Errorf("the findings cursor is on %+v, want the first member of the Dockerfile group", f)
	}
	if !reflect.DeepEqual(activeTokens(m), tokens) {
		t.Errorf("tokens = %v, were %v", activeTokens(m), tokens)
	}
	if c, d := m.findingsTable.SortState(); c != col || d != desc {
		t.Errorf("the findings sort went from (%d,%v) to (%d,%v)", col, desc, c, d)
	}
}

func TestMisconfigGroupedListOpensOnTheGroupOfTheSelectedFinding(t *testing.T) {
	m := mgCursorOnFinding(t, mgModel(t, 160), "svc stage")
	m = feed(t, m, testutil.Key(groupToggleKey))
	if g := selectedGroup(t, m); g.Subject != "svc/Dockerfile" || g.Rule != "DS002" {
		t.Errorf("the grouped list selects %s %s, want the group of the finding that was selected", g.Subject, g.Rule)
	}
}

// ── One mode per tab, for the life of the view ───────────────────────────────

func TestTheCVEAndMisconfigTabsGroupIndependently(t *testing.T) {
	m := mgResultModel(t, mgAllFindings(), 160)
	m = feed(t, m, testutil.Key(groupToggleKey))
	if !m.isGrouped() || !reflect.DeepEqual(groupSubjects(m), []string{"openssl"}) {
		t.Fatalf("the CVE tab: grouped %v, groups %v", m.isGrouped(), groupSubjects(m))
	}

	m = onTab(t, m, TabMisconfig)
	if m.isGrouped() {
		t.Fatal("grouping the CVE tab grouped the Misconfigurations tab")
	}
	if header := firstLine(ansi.Strip(m.Render())); !strings.Contains(header, "Title") || strings.Contains(header, "Package") {
		t.Errorf("the flat Misconfigurations tab shows %q", header)
	}

	m = feed(t, m, testutil.Key(groupToggleKey))
	if header := firstLine(ansi.Strip(m.Render())); !strings.Contains(header, "Findings") || strings.Contains(header, "Package") {
		t.Errorf("the grouped Misconfigurations tab shows %q", header)
	}
	if n := len(m.groups.Visible()); n != 6 {
		t.Errorf("%d misconfiguration groups, want 6", n)
	}

	m = onTab(t, m, TabCVE)
	if !m.isGrouped() {
		t.Fatal("coming back to the CVE tab lost its grouped list")
	}
	if header := firstLine(ansi.Strip(m.Render())); !strings.Contains(header, "Package") || strings.Contains(header, "Rule") {
		t.Errorf("the CVE tab shows %q after the other tab was grouped", header)
	}
	if !reflect.DeepEqual(groupSubjects(m), []string{"openssl"}) {
		t.Errorf("the CVE tab lists %v: it must show its own groups", groupSubjects(m))
	}

	m = onTab(t, m, TabMisconfig)
	if !m.isGrouped() || len(m.groups.Visible()) != 6 {
		t.Fatalf("coming back to the Misconfigurations tab: grouped %v, %d groups", m.isGrouped(), len(m.groups.Visible()))
	}
	m = feed(t, m, testutil.Key(groupToggleKey))
	if m.isGrouped() {
		t.Error("g did not leave the grouped list")
	}
	if m = onTab(t, m, TabCVE); !m.isGrouped() {
		t.Error("leaving the grouped list on the Misconfigurations tab took the CVE tab's with it")
	}
}

func TestBothGroupedModesSurviveReopeningAResult(t *testing.T) {
	m := feed(t, mgResultModel(t, mgAllFindings(), 160), testutil.Key(groupToggleKey))
	m = feed(t, onTab(t, m, TabMisconfig), testutil.Key(groupToggleKey))
	m = feed(t, m, testutil.Key("ctrl+r"))
	if m.state != StateInventory {
		t.Fatalf("ctrl+r left the state at %v", m.state)
	}

	other := resultOn(mgDir(t),
		cve("CVE-90", "curl", "alpine", "8.0", "8.1", mgHigh),
		mgFinding("DS002", "Dockerfile", "stage build", mgHigh),
		mgFinding("DS002", "Dockerfile", "stage test", mgLow),
	)
	m = feed(t, m, InventoryResultLoadedMsg{Name: other.Target, Result: other})
	if m.state != StateResults || m.activeTab != TabCVE || !m.isGrouped() {
		t.Fatalf("state %v, tab %d, grouped %v: the CVE tab was grouped", m.state, m.activeTab, m.isGrouped())
	}
	if !reflect.DeepEqual(groupSubjects(m), []string{"curl"}) {
		t.Errorf("CVE groups = %v, want the new result's", groupSubjects(m))
	}
	m = onTab(t, m, TabMisconfig)
	if !m.isGrouped() {
		t.Fatal("reopening a result lost the Misconfigurations tab's grouped list")
	}
	if want := []mgRow{{"Dockerfile", "DS002", 2, labelAvailable}}; !reflect.DeepEqual(mgRows(m), want) {
		t.Errorf("groups = %v, want %v from the new result", mgRows(m), want)
	}
}

// A result that arrives while the user is on another tab is what the next visit
// shows.
func TestAResultThatArrivesOnAnotherTabIsWhatTheMisconfigGroupsShow(t *testing.T) {
	m := mgGrouped(t, 160)
	m = onTab(t, m, TabSecrets)
	m.setResult(mgRescanned(m, "stage build", "stage test", "stage final"))
	m = onTab(t, m, TabMisconfig)
	if !m.isGrouped() {
		t.Fatal("the mode was lost")
	}
	for _, r := range mgRows(m) {
		if r.file == "Dockerfile" {
			t.Errorf("the Dockerfile group is still listed: %v", mgRows(m))
		}
	}
}

// ── What does not act in a list of groups ────────────────────────────────────

// ctrl+o, space and S act on one finding. In a list of groups they are greyed with
// the same reason as on the CVE tab, and nothing opens: the built-in fix would
// otherwise act on whatever finding the table behind the list has selected.
func TestKeysThatActOnOneFindingAreGreyedInTheMisconfigGroupedList(t *testing.T) {
	for _, key := range []string{"space", writeRemediationKey, keymap.Scan} {
		t.Run(key, func(t *testing.T) {
			// The finding selected in the flat table is one the built-in fix can
			// write, so ctrl+o is lit there: in the grouped list it must not be.
			flat := mgCursorOnFinding(t, mgModel(t, 160), "stage build")
			if !testutil.ShortcutEnabled(flat.GetShortcuts(), writeRemediationKey) {
				t.Fatalf("setup: ctrl+o is greyed on a DS002 finding of a directory: %v", flat.canFixMisconfig())
			}
			m := feed(t, flat, testutil.Key(groupToggleKey))
			if g := selectedGroup(t, m); g.Subject != "Dockerfile" {
				t.Fatalf("setup: the grouped list is on %q", g.Subject)
			}
			if !testutil.HasShortcut(m.GetShortcuts(), key) {
				t.Fatalf("%s disappeared instead of being greyed", key)
			}
			if !testutil.ShortcutDisabled(m.GetShortcuts(), key) {
				t.Errorf("%s is offered in the grouped list", key)
			}
			next, _ := step(t, m, testutil.Key(key))
			if next.footer.Text() != reasonOpenTheGroup || next.footer.Level() != components.LevelWarning {
				t.Errorf("footer = %q (level %v), want the Warn %q", next.footer.Text(), next.footer.Level(), reasonOpenTheGroup)
			}
			if next.state != StateResults || next.confirmModal != nil || next.misconfigPending != nil || !next.isGrouped() {
				t.Errorf("%s changed the screen: state %v", key, next.state)
			}
			if got := fixFile(t, next.result.Target); got != fixableDockerfile {
				t.Errorf("%s wrote the file:\n%s", key, got)
			}
		})
	}
}

// X (exclude) is the Secrets tab's, and greyed here as on the flat list.
func TestExcludeStaysGreyedInTheMisconfigGroupedList(t *testing.T) {
	m := mgGrouped(t, 160)
	if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Exclude) {
		t.Error("X is offered in the grouped Misconfigurations list")
	}
	next, _ := step(t, m, testutil.Key(keymap.Exclude))
	if next.confirmModal != nil {
		t.Error("X opened a confirmation")
	}
}

// ── Y copies the group's command, only when there is one ─────────────────────

func mgCommandFindings() []scan.Finding {
	const cmd = "chmod 600 app.conf"
	return []scan.Finding{
		withCommand(mgFinding("KSV-0003", "perm.yaml", "one", mgHigh), cmd),
		withCommand(mgFinding("KSV-0003", "perm.yaml", "two", mgHigh), cmd),
		withCommand(mgFinding("KSV-0003", "perm.yaml", "three", mgHigh), cmd),
		mgFinding("KSV-0003", "other.yaml", "four", mgLow),
	}
}

func TestYInTheMisconfigGroupedListCopiesTheGroupsCommandWholeAndRunsNothing(t *testing.T) {
	for name, open := range map[string]func(*testing.T) Model{
		"the grouped list": func(t *testing.T) Model {
			return mgCursor(t, mgGroupedOf(t, mgCommandFindings(), 160), "perm.yaml", "KSV-0003", "Manual")
		},
		"the group level": func(t *testing.T) Model {
			m := mgCursor(t, mgGroupedOf(t, mgCommandFindings(), 160), "perm.yaml", "KSV-0003", "Manual")
			return feed(t, m, testutil.Key("enter"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			testutil.FastTimers(t, &components.FooterMsgDuration)
			written := stubClipboard(t, nil)
			m := open(t)
			if !testutil.ShortcutEnabled(m.GetShortcuts(), keymap.Copy) {
				t.Fatal("Y is greyed on a group with a command")
			}
			m, cmd := step(t, m, testutil.Key(keymap.Copy))
			if len(*written) != 0 {
				t.Fatalf("Update wrote %q to the clipboard: the write belongs in the Cmd (Rule 110)", *written)
			}
			msg, ok := testutil.MsgOf[FixCommandCopiedMsg](cmd)
			if !ok {
				t.Fatal("Y produced no FixCommandCopiedMsg")
			}
			if want := []string{"chmod 600 app.conf"}; !reflect.DeepEqual(*written, want) || msg.Command != want[0] || msg.Err != nil {
				t.Errorf("clipboard %q, message %+v, want %q", *written, msg, want)
			}
			m = feed(t, m, msg)
			if m.footer.Text() != "Command copied to the clipboard" || m.footer.Level() != components.LevelInfo {
				t.Errorf("footer = %q (level %v)", m.footer.Text(), m.footer.Level())
			}
		})
	}
}

// Same screen, same keys: Y stays in the column, greyed, with the reason, for a
// group that has no command, in the list and one level down.
func TestYIsGreyedForAMisconfigGroupWithNoCommand(t *testing.T) {
	for name, enter := range map[string]bool{"the grouped list": false, "the group level": true} {
		t.Run(name, func(t *testing.T) {
			testutil.FastTimers(t, &components.FooterMsgDuration)
			written := stubClipboard(t, nil)
			m := mgCursor(t, mgGroupedOf(t, mgCommandFindings(), 160), "other.yaml", "KSV-0003", labelNoGuide)
			if enter {
				m = feed(t, m, testutil.Key("enter"))
			}
			if !testutil.HasShortcut(m.GetShortcuts(), keymap.Copy) || !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Copy) {
				t.Fatalf("Y must be advertised and greyed: %v", m.GetShortcuts())
			}
			if a := m.canCopyGroupCommand(); a.Enabled() || a.Reason != reasonNoGroupCommand {
				t.Errorf("canCopyGroupCommand = %+v, want %q", a, reasonNoGroupCommand)
			}
			m, cmd := step(t, m, testutil.Key(keymap.Copy))
			drained := testutil.Msgs(cmd)
			if m.footer.Text() != reasonNoGroupCommand || m.footer.Level() != components.LevelWarning {
				t.Errorf("footer = %q (level %v), want the Warn %q", m.footer.Text(), m.footer.Level(), reasonNoGroupCommand)
			}
			if len(*written) != 0 {
				t.Errorf("the clipboard got %q for a group with no command", *written)
			}
			for _, msg := range drained {
				if _, ok := msg.(FixCommandCopiedMsg); ok {
					t.Error("Y produced a copy for a group with no command")
				}
			}
		})
	}
}

// ── The footer says what the selected group is ───────────────────────────────

func TestFooterFollowsTheSelectedMisconfigGroup(t *testing.T) {
	declined := fixStatusOf(unconfinedFinding(), true).Label()
	cases := []struct {
		file, rule, label string
		count             string
	}{
		{"Dockerfile", "DS002", labelAvailable, "3 findings"},
		{"app.yaml", "KSV-0003", labelNoGuide, "1 finding"},
		{"deploy.yaml", "KSV-0012", "Manual", "3 findings"},
		{"pod.yaml", "KSV-0104", declined, "1 finding"},
	}
	m := mgGrouped(t, 160)
	for _, tc := range cases {
		m = mgCursor(t, m, tc.file, tc.rule, tc.label)
		text := m.status().Text
		if !strings.Contains(text, tc.count) || strings.Contains(text, "1 findings") {
			t.Errorf("%s %s: footer status = %q, want the count %q", tc.file, tc.rule, text, tc.count)
		}
		if !strings.Contains(text, tc.label) {
			t.Errorf("%s %s: footer status = %q, want the Fix said in words: %q", tc.file, tc.rule, text, tc.label)
		}
		for _, word := range []string{"closed", "closes", "cleared", "resolved", "fixed", "done"} {
			if strings.Contains(strings.ToLower(text), word) {
				t.Errorf("%s %s: footer status %q promises an outcome (%q)", tc.file, tc.rule, text, word)
			}
		}
		if footer := ansi.Strip(m.RenderFooter(160)); !strings.Contains(footer, tc.count) {
			t.Errorf("%s %s: the footer does not show %q:\n%s", tc.file, tc.rule, tc.count, footer)
		}
	}
	// Flat, a selection has nothing to say.
	if got := mgModel(t, 160).status().Text; got != "" {
		t.Errorf("flat status = %q", got)
	}
}

// ── The header ───────────────────────────────────────────────────────────────

// A group nobody can fix is said in words and is not counted in Fixable: that
// figure is the vulnerabilities', grouped or not.
func TestGroupingTheMisconfigTabChangesNeitherFindingsNorFixable(t *testing.T) {
	findings := mgAllFindings()
	flat, grouped := mgModelOf(t, findings, 160), mgGroupedOf(t, findings, 160)
	wantFixable := remediation.Summarize(findings).Fixable()
	if wantFixable == 0 {
		t.Fatal("the fixtures hold no fixable CVE: the test would pass on anything")
	}
	for name, m := range map[string]Model{"flat": flat, "grouped": grouped} {
		got, err := strconv.Atoi(strings.Fields(headerValue(t, m, "Fixable"))[0])
		if err != nil || got != wantFixable {
			t.Errorf("%s: Fixable = %q, want %d: the misconfiguration groups are not in it", name, headerValue(t, m, "Fixable"), wantFixable)
		}
	}
	if headerValue(t, flat, "Findings") != headerValue(t, grouped, "Findings") {
		t.Error("grouping changed the Findings figure")
	}
	only := mgGroupedOf(t, mgFindings(), 160)
	if got := headerValue(t, only, "Fixable"); got != "0" {
		t.Errorf("a result of misconfigurations has Fixable = %q, want 0", got)
	}
	if body := ansi.Strip(only.Render()); !strings.Contains(body, labelNoGuide) || !strings.Contains(body, "Manual") {
		t.Error("the groups nobody can fix automatically are not said in words")
	}
}

func TestHeaderNamesTheGroupedModeOnTheMisconfigTab(t *testing.T) {
	if _, ok := groupModeHeader(mgModel(t, 160)); ok {
		t.Error("the flat tab says Group by fix")
	}
	m := mgGrouped(t, 160)
	if _, ok := groupModeHeader(m); !ok {
		t.Fatalf("the grouped tab does not say so in the header: %v", headerText(m))
	}
	if _, ok := groupModeHeader(onTab(t, m, TabSecrets)); ok {
		t.Error("the Secrets tab announces Group by fix because another tab is grouped")
	}
	// The other tab is flat: it does not announce what it does not show.
	if _, ok := groupModeHeader(onTab(t, m, TabCVE)); ok {
		t.Error("the flat CVE tab announces Group by fix")
	}
}

func TestHelpSaysGroupByFixCoversTheMisconfigurationsTab(t *testing.T) {
	content := mgModel(t, 160).GetHelpContent()
	var g bool
	for _, kb := range content.KeyBindings {
		if kb.Key == groupToggleKey {
			g = g || strings.Contains(strings.ToLower(kb.Description), "misconfig")
		}
	}
	if !g {
		t.Error("the g binding of the help does not mention the Misconfigurations tab")
	}
	found := false
	for _, s := range content.Sections {
		found = found || (s.Title == "Group by fix" && strings.Contains(strings.ToLower(s.Body), "misconfig"))
	}
	if !found {
		t.Error("the \"Group by fix\" section of the help does not mention the Misconfigurations tab")
	}
}

// ── A target with no file (an image) ─────────────────────────────────────────

func TestAnImageGroupsItsMisconfigurationsPerRuleWithoutAFile(t *testing.T) {
	m := mgImageModelOf(t, mgImageFindings(), 160)
	if !m.canGroup().Enabled() {
		t.Fatalf("g is greyed on an image's Misconfigurations tab: %+v", m.canGroup())
	}
	m = feed(t, m, testutil.Key(groupToggleKey))
	if !m.isGrouped() {
		t.Fatal("g did not group the image's tab")
	}
	rows := mgRows(m)
	if len(rows) != 2 || rows[0].rule != "DS002" || rows[0].count != 2 || rows[1].rule != "DS026" || rows[1].count != 1 {
		t.Fatalf("groups = %v, want DS002 x2 then DS026 x1", rows)
	}
	for _, r := range rows {
		if r.label != "Cannot fix here" {
			t.Errorf("%s reads %q on an image", r.rule, r.label)
		}
	}
	if body := ansi.Strip(m.Render()); !strings.Contains(body, "Cannot fix here") {
		t.Errorf("the body does not say it:\n%s", body)
	}
	// The level opens and closes without a file to name, and its detail refuses the
	// fix with the flat path's reason.
	m = feed(t, m, testutil.Key("enter"))
	if m.state != StateGroup || len(m.groupTable.Visible()) != 2 {
		t.Fatalf("enter: state %v, %d members", m.state, len(m.groupTable.Visible()))
	}
	m = feed(t, m, testutil.Key("enter"))
	if m.state != StateDetails || m.detailsParent != StateGroup {
		t.Fatalf("enter: state %v, parent %v", m.state, m.detailsParent)
	}
	if a := m.canFixMisconfig(); a.Enabled() || a.Reason != reasonFileNotOnDisk {
		t.Errorf("canFixMisconfig = %+v, want %q", a, reasonFileNotOnDisk)
	}
	m = feed(t, m, testutil.Key("esc"), testutil.Key("esc"))
	if m.state != StateResults || !m.isGrouped() {
		t.Errorf("esc twice: state %v, grouped %v", m.state, m.isGrouped())
	}
}

// ── Width (Rule 116, Rule 124) ───────────────────────────────────────────────

func TestMisconfigGroupedViewFitsAtEveryWidth(t *testing.T) {
	for _, width := range []int{80, 120, 175} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			m := feed(t, mgGrouped(t, width), testutil.Key("m"))
			if got, want := m.groups.RenderedWidth(), width-2; got != want {
				t.Errorf("RenderedWidth = %d, want %d (Rule 116)", got, want)
			}
			body := m.Render()
			for i, line := range strings.Split(body, "\n") {
				if w := ansi.StringWidth(line); w > width-2 {
					t.Errorf("body line %d is %d cells, the viewport holds %d: %q", i, w, width-2, ansi.Strip(line))
				}
			}
			header := firstLine(ansi.Strip(body))
			for _, title := range []string{"Severity", "File", "Rule", "Findings", "Fix"} {
				if !strings.Contains(header, title) {
					t.Errorf("at %d columns the header %q lost %q", width, header, title)
				}
			}
			if width >= 120 {
				// The reason may be cut, the word of the vocabulary may not.
				if plain := ansi.Strip(body); !regexp.MustCompile(`pod\.yaml\s+KSV-0104\s+1\s+Built-in fix unavailable`).MatchString(plain) {
					t.Errorf("at %d columns the Fix column cuts its label:\n%s", width, plain)
				}
			}
			footer := strings.Split(m.RenderFooter(width), "\n")
			if len(footer) != m.GetFooterHeight() {
				t.Errorf("the footer is %d lines, GetFooterHeight says %d (Rule 124)", len(footer), m.GetFooterHeight())
			}
			for i, line := range footer {
				if i == len(footer)-3 && m.showsResultTabs() {
					continue // the tab bar: a separate bug past 80 columns
				}
				if w := ansi.StringWidth(line); w > width {
					t.Errorf("footer line %d is %d cells wide, the window holds %d: %q", i, w, width, ansi.Strip(line))
				}
			}
		})
	}
}

// A deep path, a long rule and text from the scan still fit; the file keeps its
// tail, which is what tells two files apart.
func TestMisconfigGroupedViewFitsWithLongNames(t *testing.T) {
	long := strings.Repeat("deeply/nested/", 6) + "Dockerfile"
	findings := []scan.Finding{
		mgFinding("KSV-"+strings.Repeat("9", 60), long, "x", mgCrit),
		mgFinding("DS002", long+"2", "y", mgHigh),
		mgFinding("DS002", "svc/"+hostileText+"Dockerfile", "z", mgLow),
	}
	for _, width := range []int{80, 120, 175} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			m := mgGroupedOf(t, findings, width)
			if got, want := m.groups.RenderedWidth(), width-2; got != want {
				t.Errorf("RenderedWidth = %d, want %d", got, want)
			}
			for i, line := range strings.Split(m.Render(), "\n") {
				if w := ansi.StringWidth(line); w > width-2 {
					t.Errorf("body line %d is %d cells, the viewport holds %d", i, w, width-2)
				}
			}
			if body := ansi.Strip(m.Render()); !strings.Contains(body, "/Dockerfile") {
				t.Errorf("the file lost its tail:\n%s", body)
			}
			for range 3 {
				if w := ansi.StringWidth(m.renderInfoLine(width)); w > width {
					t.Errorf("the info line is %d cells wide, the window holds %d", w, width)
				}
				if status := m.status().Text; status != ansi.Strip(status) {
					t.Errorf("the footer status %q holds an escape sequence", status)
				}
				requireTerminalSafe(t, "footer status", m.status().Text)
				m = feed(t, m, testutil.Key("down"))
			}
		})
	}
}

func TestResizingKeepsTheMisconfigGroupedListInTheViewport(t *testing.T) {
	m := mgGrouped(t, 175)
	for _, width := range []int{80, 120, 175} {
		m = feed(t, m, testutil.Resize(width, 30))
		if got, want := m.groups.RenderedWidth(), width-2; got != want {
			t.Errorf("after a resize to %d: RenderedWidth = %d, want %d", width, got, want)
		}
	}
}
