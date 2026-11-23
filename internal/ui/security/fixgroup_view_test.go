package security

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
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

// DEVDESK-754 (SC1, step 3): `g` switches the CVE tab between the findings table
// and the grouped list, one row per cause. The findings table is never rebuilt by
// the switch: its filter, its sort, its query and its cursor are what the user
// left (AC4). Footer timers are never run: the footer is read through Text and
// Level. User decisions pinned here: `g` is the third key of the results column;
// the mode is held per tab for the life of the view; the CVE tab and, since SC2,
// the Misconfigurations tab group.

// ── Fixtures ─────────────────────────────────────────────────────────────────

// groupFindings are the findings of the grouped tests. The CVEs are interleaved
// on purpose, so that the first visible member of a group is not the first row of
// the table. Groups, worst first: openssl (3 CVEs, CRITICAL), golang.org/x/net
// (1, HIGH), zlib (2, MEDIUM, no fix published), libfoo (1, LOW, two candidate
// versions).
func groupFindings() []scan.Finding {
	const (
		crit = scan.SeverityCritical
		high = scan.SeverityHigh
		med  = scan.SeverityMedium
		low  = scan.SeverityLow
	)
	net := cve("CVE-4", "golang.org/x/net", "gomod", "0.10.0", "0.23.0", high)
	net.File = "go.mod"
	net.FixCommand = "go get golang.org/x/net@v0.23.0"
	return []scan.Finding{
		cve("CVE-5", "zlib", "alpine", "1.2", "", med),
		net,
		cve("CVE-2", "openssl", "alpine", "1.1.1k", "1.1.1w", high),
		cve("CVE-6", "zlib", "alpine", "1.2", "", med),
		cve("CVE-1", "openssl", "alpine", "1.1.1k", "1.1.1w", crit),
		cve("CVE-7", "libfoo", "npm", "1.0.0", "abc, def", low),
		cve("CVE-3", "openssl", "alpine", "1.1.1k", "1.1.1w", low),
		{ID: "aws-access-token", Title: "AWS key committed", Severity: high, Source: scan.SourceGitleaks, File: "config/prod.env", Line: 12, Match: "AKIA..."},
		{ID: "GPL-3.0", Title: "Copyleft licence", Severity: low, Source: scan.SourceTrivyLicense, File: "go.mod"},
		{ID: "DS002", Title: "Container runs as root", Severity: high, Source: scan.SourceTrivyMisconfig, File: "Dockerfile"},
	}
}

// groupResultOn is the scan of dir holding findings.
func groupResultOn(dir string, findings []scan.Finding) *scan.Result {
	return &scan.Result{Target: dir, TargetType: scan.TargetDirectory, Findings: findings}
}

// groupDir is a scanned directory with the one file the CVE of golang.org/x/net
// points at.
func groupDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// groupModelOf is the results of findings, laid out, on the CVE tab, flat.
func groupModelOf(t *testing.T, findings []scan.Finding, width int) Model {
	t.Helper()
	m := NewWithPreloadedResult(testConfig(), nil, groupResultOn(groupDir(t), findings))
	return feed(t, m, testutil.Resize(width, 30))
}

func groupModel(t *testing.T, width int) Model {
	t.Helper()
	return groupModelOf(t, groupFindings(), width)
}

// groupedModel is groupModel with `g` pressed once.
func groupedModel(t *testing.T, width int) Model {
	t.Helper()
	m := feed(t, groupModel(t, width), testutil.Key(groupToggleKey))
	if !m.isGrouped() {
		t.Fatal("g did not switch the CVE tab to the grouped list")
	}
	return m
}

// onTab presses tab until the active tab is tab.
func onTab(t *testing.T, m Model, tab int) Model {
	t.Helper()
	for range tabCount {
		if m.activeTab == tab {
			return m
		}
		m = feed(t, m, testutil.Key("tab"))
	}
	t.Fatalf("tab never reached tab %d", tab)
	return m
}

func groupSubjects(m Model) []string {
	out := []string{}
	for _, g := range m.groups.Visible() {
		out = append(out, g.Subject)
	}
	return out
}

// selectedGroup is the group under the cursor of the grouped list.
func selectedGroup(t *testing.T, m Model) fixGroup {
	t.Helper()
	g, ok := m.groups.Selected()
	if !ok {
		t.Fatal("no group is selected")
	}
	return g
}

// cursorOnGroup moves the cursor of the grouped list to subject: to the top, then
// down. The grouped list opens on the group of the selected finding (the first
// finding's group is zlib, the third of four), so a walk that only went down could
// never reach the groups above it.
func cursorOnGroup(t *testing.T, m Model, subject string) Model {
	t.Helper()
	m = feed(t, m, testutil.Key("home"))
	for range 12 {
		if g, ok := m.groups.Selected(); ok && g.Subject == subject {
			return m
		}
		m = feed(t, m, testutil.Key("down"))
	}
	t.Fatalf("the cursor never reached the group %q (groups: %v)", subject, groupSubjects(m))
	return m
}

// cursorOnFinding moves the cursor of the findings table down to the finding id.
func cursorOnFinding(t *testing.T, m Model, id string) Model {
	t.Helper()
	for range 12 {
		if f, ok := m.findingsTable.Selected(); ok && f.ID == id {
			return m
		}
		m = feed(t, m, testutil.Key("down"))
	}
	t.Fatalf("the cursor never reached %s", id)
	return m
}

// lineWith is the first line of text that holds want.
func lineWith(t *testing.T, text, want string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	t.Fatalf("no line holds %q:\n%s", want, text)
	return ""
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

func activeTokens(m Model) map[string]bool {
	out := map[string]bool{}
	for _, label := range []string{"critical", "high", "medium", "low", pssToken} {
		out[label] = m.findingsTable.IsTokenActive(label)
	}
	return out
}

// ── The key ──────────────────────────────────────────────────────────────────

// The user's decision: g is the third entry of the results column, so that it is
// among the seven that show at 80 columns, and h moves to the hidden part.
func TestGroupKeyIsTheThirdOfTheResultsColumn(t *testing.T) {
	keys := testutil.ShortcutKeys(groupModel(t, 160).GetShortcuts())
	if len(keys) < 3 || !slices.Equal(keys[:3], []string{"tab", "enter", groupToggleKey}) {
		t.Fatalf("the column starts %v, want tab, enter, g", keys)
	}
	if i := slices.Index(keys, "h"); i < 7 {
		t.Errorf("h is entry %d of the column; only the first seven show at 80 columns, and g takes the place h had", i)
	}
	// Nothing else moved: the rest of the column is the one it was.
	rest := slices.Delete(slices.Clone(keys), 2, 3)
	for i, want := range []string{"tab", "enter", "space", writeRemediationKey, keymap.Scan, "c", "h", "m", "l"} {
		if rest[i] != want {
			t.Errorf("without g, entry %d is %q, want %q", i, rest[i], want)
		}
	}
}

func TestGroupShortcutIsEnabledOnTheCVETabAndNamesTheModeItSwitchesTo(t *testing.T) {
	m := groupModel(t, 160)
	if !testutil.ShortcutEnabled(m.GetShortcuts(), groupToggleKey) {
		t.Fatal("g is not offered on the CVE tab")
	}
	if got := groupShortcutWord(t, m, groupToggleKey); got != "Group by fix" {
		t.Errorf("flat: g reads %q, want Group by fix", got)
	}
	m = feed(t, m, testutil.Key(groupToggleKey))
	if !testutil.ShortcutEnabled(m.GetShortcuts(), groupToggleKey) {
		t.Fatal("g is not offered in the grouped list: there would be no way back")
	}
	if got := groupShortcutWord(t, m, groupToggleKey); got != "List by finding" {
		t.Errorf("grouped: g reads %q, want List by finding", got)
	}
}

func groupShortcutWord(t *testing.T, m Model, key string) string {
	t.Helper()
	for _, s := range m.GetShortcuts() {
		if s.Key == key {
			return s.Description
		}
	}
	t.Fatalf("%q is not in the column %v", key, testutil.ShortcutKeys(m.GetShortcuts()))
	return ""
}

// Rule 130: a column is the same list on every tab, grouped or not; what varies
// is what is greyed.
func TestTheResultsColumnHoldsTheSameKeysOnEveryTabGroupedOrNot(t *testing.T) {
	flat := groupModel(t, 160)
	want := testutil.ShortcutKeys(flat.GetShortcuts())
	if !slices.Contains(want, groupToggleKey) || !slices.Contains(want, keymap.Copy) {
		t.Fatalf("the column %v lacks g or Y", want)
	}
	m := flat
	for tab := TabSecrets; tab <= TabRemediation; tab++ {
		m = onTab(t, m, tab)
		if got := testutil.ShortcutKeys(m.GetShortcuts()); !slices.Equal(got, want) {
			t.Errorf("tab %d: the column is %v, want %v", tab, got, want)
		}
	}
	grouped := groupedModel(t, 160)
	if got := testutil.ShortcutKeys(grouped.GetShortcuts()); !slices.Equal(got, want) {
		t.Errorf("grouped: the column is %v, want %v", got, want)
	}
}

// g covers the CVE tab and, since SC2, the Misconfigurations tab
// (fixgroup_misconfig_view_test.go): everywhere else it is greyed, and pressing it
// says why. The six tabs stay six.
func TestGroupIsGreyedWithItsReasonOutsideTheCVEAndMisconfigTabs(t *testing.T) {
	for _, tab := range []int{TabSecrets, TabLicense, TabCIScore, TabRemediation} {
		t.Run(fmt.Sprintf("tab %d", tab), func(t *testing.T) {
			m := onTab(t, groupModel(t, 160), tab)
			if !testutil.HasShortcut(m.GetShortcuts(), groupToggleKey) {
				t.Fatal("g disappeared instead of being greyed (Rule 130)")
			}
			if !testutil.ShortcutDisabled(m.GetShortcuts(), groupToggleKey) {
				t.Error("g is offered where it cannot act")
			}
			a := m.canGroup()
			if a.Enabled() || a.Reason != reasonGroupNotHere {
				t.Errorf("canGroup = %+v, want %q", a, reasonGroupNotHere)
			}
			before := ansi.Strip(m.Render())
			m, _ = step(t, m, testutil.Key(groupToggleKey))
			if m.isGrouped() {
				t.Error("g grouped a tab that has no grouped list")
			}
			if m.footer.Text() != reasonGroupNotHere || m.footer.Level() != components.LevelWarning {
				t.Errorf("footer = %q (level %v), want the Warn %q", m.footer.Text(), m.footer.Level(), reasonGroupNotHere)
			}
			if after := ansi.Strip(m.Render()); after != before {
				t.Error("the body changed although g was refused")
			}
		})
	}
	if tabCount != 6 {
		t.Errorf("tabCount = %d: Group by fix is a mode, not a seventh tab", tabCount)
	}
}

func TestTheReasonOfAGreyedGroupKeyNamesTheCVETab(t *testing.T) {
	if !strings.Contains(reasonGroupNotHere, "CVE") {
		t.Errorf("%q does not say which tab groups", reasonGroupNotHere)
	}
}

func TestGroupReasonsAreTextAndTellApartWhatTheyRefuse(t *testing.T) {
	reasons := []string{
		reasonGroupNotHere, reasonNothingToGroup, reasonOpenTheGroup, reasonNoGroupSelected,
		reasonNoGroupCommand, reasonGroupHasNoFix, reasonCopyNeedsGroup,
	}
	seen := map[string]bool{}
	for _, r := range reasons {
		if strings.TrimSpace(r) == "" {
			t.Error("a reason is empty: an empty reason means available")
		}
		if seen[r] {
			t.Errorf("two reasons read %q", r)
		}
		seen[r] = true
	}
}

// A result with no vulnerability has nothing to group: g says so. A filter that
// hides them all does not change that there are some.
func TestGroupHasNothingToGroupWithoutAVulnerability(t *testing.T) {
	m := groupModelOf(t, []scan.Finding{groupFindings()[7], groupFindings()[9]}, 160)
	a := m.canGroup()
	if a.Enabled() || a.Reason != reasonNothingToGroup {
		t.Errorf("canGroup = %+v, want %q", a, reasonNothingToGroup)
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), groupToggleKey) {
		t.Error("g is offered on a CVE tab with no CVE")
	}

	hidden := feed(t, groupModel(t, 160), testutil.Key("/"))
	hidden = feed(t, hidden, testutil.Type("zzzz")...)
	hidden = feed(t, hidden, testutil.Key("enter"))
	if len(hidden.findingsTable.Visible()) != 0 {
		t.Fatalf("the search left %d findings", len(hidden.findingsTable.Visible()))
	}
	if a := hidden.canGroup(); !a.Enabled() {
		t.Errorf("canGroup = %+v: a search that hides every CVE does not make them vanish", a)
	}
}

// ── The grouped list (AC1) ───────────────────────────────────────────────────

func TestGroupedListShowsOneRowPerCauseWithTheDeltaAndTheCount(t *testing.T) {
	m := groupModel(t, 160)
	before := m.findingsTable.Visible()
	cursor := m.findingsTable.Cursor()
	flat := ansi.Strip(m.Render())
	if !strings.Contains(firstLine(flat), "Title") || strings.Contains(firstLine(flat), "Package") {
		t.Fatalf("the flat table's header is %q", firstLine(flat))
	}

	m = feed(t, m, testutil.Key(groupToggleKey))
	if !m.isGrouped() {
		t.Fatal("g did not group the CVE tab")
	}
	body := ansi.Strip(m.Render())
	header := firstLine(body)
	for _, title := range []string{"Severity", "Package", "Change", "CVEs", "Fix"} {
		if !strings.Contains(header, title) {
			t.Errorf("the grouped header %q lacks %q", header, title)
		}
	}
	if strings.Contains(header, "Title") {
		t.Errorf("the grouped header %q still shows the findings' columns", header)
	}
	if want := []string{"openssl", "golang.org/x/net", "zlib", "libfoo"}; !reflect.DeepEqual(groupSubjects(m), want) {
		t.Errorf("groups = %v, want %v: worst severity first", groupSubjects(m), want)
	}
	rows := map[string]string{
		"openssl":          `CRITICAL\s+openssl\s+1\.1\.1k -> 1\.1\.1w\s+3\s+Manual`,
		"golang.org/x/net": `HIGH\s+golang\.org/x/net\s+0\.10\.0 -> 0\.23\.0\s+1\s+Manual`,
		"zlib":             `MEDIUM\s+zlib\s+1\.2 -> none\s+2\s+No fix published`,
		"libfoo":           `LOW\s+libfoo\s+1\.0\.0 -> abc or def\s+1\s+Manual`,
	}
	for subject, pattern := range rows {
		if line := lineWith(t, body, subject); !regexp.MustCompile(pattern).MatchString(line) {
			t.Errorf("the row of %s reads %q, want it to match %s", subject, strings.TrimSpace(line), pattern)
		}
	}

	// Showing the groups touched nothing of the table behind them.
	if !reflect.DeepEqual(m.findingsTable.Visible(), before) || m.findingsTable.Cursor() != cursor {
		t.Error("the findings table changed when the grouped list opened")
	}
}

// Rule 139: a grouped list with nothing to show is a header and no rows, not a
// message in the body.
func TestGroupedBodyStaysATableWhenNothingMatches(t *testing.T) {
	m := groupedModel(t, 160)
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("zzzz")...)
	m = feed(t, m, testutil.Key("enter"))

	if len(m.groups.Visible()) != 0 {
		t.Fatalf("groups = %v after a search that matches nothing", groupSubjects(m))
	}
	body := ansi.Strip(m.Render())
	if !strings.Contains(firstLine(body), "Package") {
		t.Errorf("the body lost its header: %q", firstLine(body))
	}
	for _, text := range []string{"No ", "no ", "Nothing", "nothing"} {
		if strings.Contains(body, text) {
			t.Errorf("the body carries a message (%q): the footer and the header say what the table does not\n%s", text, body)
		}
	}
	if enter := m.GetShortcuts(); testutil.ShortcutEnabled(enter, "enter") {
		t.Error("enter is offered with no group to open")
	}
}

// ── Filters and search drive the findings table ──────────────────────────────

func TestSeverityTokensStillFilterAndTheGroupsFollow(t *testing.T) {
	m := groupedModel(t, 160)
	m = feed(t, m, testutil.Key("c"))
	if !m.findingsTable.IsTokenActive("critical") {
		t.Fatal("c did not drive the findings table's token in the grouped list")
	}
	if want := []string{"openssl"}; !reflect.DeepEqual(groupSubjects(m), want) {
		t.Errorf("groups under CRITICAL = %v, want %v: a group is listed when one member is visible", groupSubjects(m), want)
	}
	// The count is what one move closes, not what the filter shows.
	if line := lineWith(t, ansi.Strip(m.Render()), "openssl"); !regexp.MustCompile(`openssl\s+1\.1\.1k -> 1\.1\.1w\s+3\s`).MatchString(line) {
		t.Errorf("the row reads %q: the filter hides two of the three CVEs but one move still closes them all", strings.TrimSpace(line))
	}

	m = feed(t, m, testutil.Key("c"))
	if len(m.groups.Visible()) != 4 {
		t.Errorf("groups with the token off = %v, want all four", groupSubjects(m))
	}
	for _, key := range []string{"h", "m", "l"} {
		m = feed(t, m, testutil.Key(key))
	}
	if len(m.groups.Visible()) != 4 {
		t.Errorf("groups with every severity on = %v, want all four", groupSubjects(m))
	}
}

func TestSearchStillDrivesTheFindingsTableAndKeepsItsKeyboard(t *testing.T) {
	m := groupedModel(t, 160)
	m = feed(t, m, testutil.Key("/"))
	if !m.InEditMode() {
		t.Fatal("/ did not give the keyboard to the search in the grouped list")
	}
	// A character typed in the query is not the toggle.
	m = feed(t, m, testutil.Type("zlib")...)
	if !m.isGrouped() {
		t.Error("typing in the search left the grouped list")
	}
	m = feed(t, m, testutil.Key("enter"))
	if m.InEditMode() {
		t.Error("enter did not confirm the search")
	}
	if want := []string{"zlib"}; !reflect.DeepEqual(groupSubjects(m), want) {
		t.Errorf("groups for the query zlib = %v, want %v", groupSubjects(m), want)
	}

	flat := feed(t, groupModel(t, 160), testutil.Key("/"), testutil.Key(groupToggleKey))
	if flat.isGrouped() || !flat.InEditMode() {
		t.Error("g typed into the search toggled the display")
	}
}

// A paste into the search bar changes what the findings table shows, so the
// grouped list follows at once, as it does for a typed query.
func TestPastingInTheSearchFiltersTheGroupedListAtOnce(t *testing.T) {
	m := groupedModel(t, 160)
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Paste("zlib"))
	if !m.isGrouped() {
		t.Fatal("a paste left the grouped list")
	}
	if want := []string{"zlib"}; !reflect.DeepEqual(groupSubjects(m), want) {
		t.Errorf("groups after pasting zlib = %v, want %v", groupSubjects(m), want)
	}
}

func TestDotSortsTheGroupsAndLeavesTheFindingsSortAlone(t *testing.T) {
	m := groupedModel(t, 160)
	findingsCol, findingsDesc := m.findingsTable.SortState()
	groupsCol, groupsDesc := m.groups.SortState()

	m = feed(t, m, testutil.Key("."))
	if col, desc := m.groups.SortState(); col == groupsCol && desc == groupsDesc {
		t.Error(". did not sort the groups")
	}
	if col, desc := m.findingsTable.SortState(); col != findingsCol || desc != findingsDesc {
		t.Errorf("the findings table's sort went from (%d,%v) to (%d,%v): . belongs to the list on screen", findingsCol, findingsDesc, col, desc)
	}
}

// ── Back to the findings (AC4) ───────────────────────────────────────────────

func TestTogglingBackKeepsFilterSortAndQueryAndSelectsTheGroupsFirstVisibleMember(t *testing.T) {
	m := groupModel(t, 160)
	// LOW is off: CVE-3 and CVE-7 are hidden. The sort moves the order away from
	// the scanner's, and the query stays.
	m = feed(t, m, testutil.Key("c"), testutil.Key("h"), testutil.Key("m"), testutil.Key("."))
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("CVE")...)
	m = feed(t, m, testutil.Key("enter"))
	col, desc := m.findingsTable.SortState()
	if col == -1 {
		t.Fatal("the sort did not move")
	}
	tokens, query := activeTokens(m), m.findingsTable.FilterBar().SearchQuery()
	if query != "CVE" || !tokens["critical"] || !tokens["high"] || !tokens["medium"] || tokens["low"] {
		t.Fatalf("setup: query %q, tokens %v", query, tokens)
	}

	m = feed(t, m, testutil.Key(groupToggleKey))
	m = cursorOnGroup(t, m, "openssl")
	m = feed(t, m, testutil.Key(groupToggleKey))

	if m.isGrouped() {
		t.Fatal("g did not come back to the findings")
	}
	if c, d := m.findingsTable.SortState(); c != col || d != desc {
		t.Errorf("the sort is (%d,%v), was (%d,%v)", c, d, col, desc)
	}
	if got := activeTokens(m); !reflect.DeepEqual(got, tokens) {
		t.Errorf("tokens = %v, were %v", got, tokens)
	}
	if got := m.findingsTable.FilterBar().SearchQuery(); got != query {
		t.Errorf("query = %q, was %q", got, query)
	}
	want := slices.IndexFunc(m.findingsTable.Visible(), func(f scan.Finding) bool { return f.PkgName == "openssl" })
	if want <= 0 {
		t.Fatalf("the fixture needs openssl below the first row; first visible member at %d", want)
	}
	if got := m.findingsTable.Cursor(); got != want {
		t.Errorf("cursor = %d, want %d: the first visible member of the group, not the top of the table", got, want)
	}
	if f, _ := m.findingsTable.Selected(); f.PkgName != "openssl" || f.Severity == scan.SeverityLow {
		t.Errorf("selected %s %s: a hidden member cannot be selected", f.ID, f.PkgName)
	}

	// And forward again: the group of the selected finding.
	m = feed(t, m, testutil.Key(groupToggleKey))
	if g := selectedGroup(t, m); g.Subject != "openssl" {
		t.Errorf("g again selected %q, want the group of the selected finding", g.Subject)
	}
}

func TestGroupedListOpensOnTheGroupOfTheSelectedFinding(t *testing.T) {
	m := cursorOnFinding(t, groupModel(t, 160), "CVE-4")
	m = feed(t, m, testutil.Key(groupToggleKey))
	if g := selectedGroup(t, m); g.Subject != "golang.org/x/net" {
		t.Errorf("the grouped list selects %q, want the group of CVE-4", g.Subject)
	}
}

// ── Per tab, for the life of the view ────────────────────────────────────────

func TestGroupedModeIsHeldPerTab(t *testing.T) {
	m := groupedModel(t, 160)
	m = onTab(t, m, TabSecrets)
	if m.isGrouped() {
		t.Error("the Secrets tab is grouped")
	}
	if header := firstLine(ansi.Strip(m.Render())); !strings.Contains(header, "Title") || strings.Contains(header, "Package") {
		t.Errorf("the Secrets tab does not show the findings table: %q", header)
	}
	for m.activeTab != TabCVE {
		m = feed(t, m, testutil.Key("shift+tab"))
	}
	if !m.isGrouped() {
		t.Error("coming back to the CVE tab lost the grouped list")
	}
	if header := firstLine(ansi.Strip(m.Render())); !strings.Contains(header, "Package") {
		t.Errorf("the CVE tab does not show the groups: %q", header)
	}
}

func TestGroupedModeSurvivesReopeningAResult(t *testing.T) {
	m := groupedModel(t, 160)
	m = feed(t, m, testutil.Key("ctrl+r"))
	if m.state != StateInventory {
		t.Fatalf("ctrl+r left the state at %v", m.state)
	}

	other := groupResultOn(groupDir(t), []scan.Finding{
		cve("CVE-90", "curl", "alpine", "8.0", "8.1", scan.SeverityHigh),
		cve("CVE-91", "curl", "alpine", "8.0", "8.1", scan.SeverityLow),
	})
	m = feed(t, m, InventoryResultLoadedMsg{Name: other.Target, Result: other})
	if m.state != StateResults || m.activeTab != TabCVE {
		t.Fatalf("the result did not open on the CVE tab: state %v, tab %d", m.state, m.activeTab)
	}
	if !m.isGrouped() {
		t.Fatal("reopening a result lost the grouped list: it lasts as long as the view")
	}
	if want := []string{"curl"}; !reflect.DeepEqual(groupSubjects(m), want) {
		t.Errorf("groups = %v, want the new result's %v", groupSubjects(m), want)
	}
}

// ── What does not act in a list of groups ────────────────────────────────────

func TestKeysThatActOnOneFindingAreGreyedInTheGroupedListWithTheirReason(t *testing.T) {
	m := groupedModel(t, 160)
	for _, key := range []string{"space", writeRemediationKey, keymap.Scan} {
		if !testutil.HasShortcut(m.GetShortcuts(), key) {
			t.Errorf("%s disappeared instead of being greyed", key)
		}
		if !testutil.ShortcutDisabled(m.GetShortcuts(), key) {
			t.Errorf("%s is offered in the grouped list", key)
		}
		next, _ := step(t, m, testutil.Key(key))
		if next.footer.Text() != reasonOpenTheGroup || next.footer.Level() != components.LevelWarning {
			t.Errorf("%s: footer = %q (level %v), want the Warn %q", key, next.footer.Text(), next.footer.Level(), reasonOpenTheGroup)
		}
		if next.state != StateResults || next.confirmModal != nil || !next.isGrouped() {
			t.Errorf("%s changed the screen: state %v", key, next.state)
		}
	}
}

// ── The footer says what the selected group does ─────────────────────────────

func TestGroupSentence(t *testing.T) {
	groups := cveGroups([]scan.Finding{
		cve("CVE-1", "openssl", "alpine", "1.1.1k", "1.1.1w", scan.SeverityHigh),
		cve("CVE-2", "openssl", "alpine", "1.1.1k", "1.1.1w", scan.SeverityHigh),
		cve("CVE-3", "openssl", "alpine", "1.1.1k", "1.1.1w", scan.SeverityHigh),
		cve("CVE-4", "golang.org/x/net", "gomod", "0.10.0", "0.23.0", scan.SeverityHigh),
		cve("CVE-5", "zlib", "alpine", "1.2", "", scan.SeverityHigh),
		cve("CVE-6", "zlib", "alpine", "1.2", "", scan.SeverityHigh),
		cve("CVE-7", "musl", "alpine", "1.0", "", scan.SeverityHigh),
		cve("CVE-8", "libfoo", "npm", "1.0.0", "abc, def", scan.SeverityHigh),
	})
	exact := map[string]string{
		"openssl":          "3 CVEs closed by one move to 1.1.1w",
		"golang.org/x/net": "1 CVE closed by one move to 0.23.0",
		"zlib":             "No fix published for 2 CVEs",
		"musl":             "No fix published for 1 CVE",
	}
	for subject, want := range exact {
		if got := groupSentence(groupBySubject(t, groups, subject)); got != want {
			t.Errorf("%s: sentence = %q, want %q", subject, got, want)
		}
	}
	// Two candidate versions: no single move is promised.
	got := groupSentence(groupBySubject(t, groups, "libfoo"))
	if !strings.Contains(got, "1 CVE") || strings.Contains(got, "closed by one move") {
		t.Errorf("libfoo: sentence = %q: it names the count and promises no single move", got)
	}
}

func TestFooterFollowsTheSelectedGroup(t *testing.T) {
	m := groupedModel(t, 160)
	want := []struct{ subject, text string }{
		{"openssl", "3 CVEs closed by one move to 1.1.1w"},
		{"golang.org/x/net", "1 CVE closed by one move to 0.23.0"},
		{"zlib", "No fix published for 2 CVEs"},
	}
	for _, w := range want {
		m = cursorOnGroup(t, m, w.subject)
		if got := m.status().Text; got != w.text {
			t.Errorf("%s: footer status = %q, want %q", w.subject, got, w.text)
		}
		if footer := ansi.Strip(m.RenderFooter(160)); !strings.Contains(footer, w.text) {
			t.Errorf("%s: the footer does not show %q:\n%s", w.subject, w.text, footer)
		}
	}
	// Flat, the status is what it was: nothing to say about a selection.
	if got := groupModel(t, 160).status().Text; got != "" {
		t.Errorf("flat status = %q", got)
	}
}

// ── The header (AC5, AC6) ────────────────────────────────────────────────────

func groupModeHeader(m Model) (string, bool) {
	for _, h := range m.GetHeaderInfo("ctx") {
		if h.Value == "Group by fix" {
			return h.Key, true
		}
	}
	return "", false
}

func TestHeaderNamesTheGroupedMode(t *testing.T) {
	if _, ok := groupModeHeader(groupModel(t, 160)); ok {
		t.Error("the flat list says Group by fix")
	}
	m := groupedModel(t, 160)
	key, ok := groupModeHeader(m)
	if !ok {
		t.Fatalf("the grouped list does not say so in the header: %v", headerText(m))
	}
	if key == "" {
		t.Error("the mode has no key")
	}
	// Nothing else left the header.
	for _, k := range []string{"Context", "Findings", "Fixable"} {
		headerValue(t, m, k)
	}
	flat := groupModel(t, 160)
	if headerValue(t, m, "Findings") != headerValue(t, flat, "Findings") || headerValue(t, m, "Fixable") != headerValue(t, flat, "Fixable") {
		t.Error("grouping changed the Findings or Fixable figure of the header")
	}
	// On another tab the mode is not on screen, so it is not announced.
	if _, ok := groupModeHeader(onTab(t, m, TabSecrets)); ok {
		t.Error("the Secrets tab announces Group by fix")
	}
}

// AC5: a group nobody can fix is said in words and is not counted as fixable,
// grouped or not, and nothing is ever marked done.
func TestUnfixedGroupsAreNotCountedInFixable(t *testing.T) {
	flat := groupModel(t, 160)
	grouped := cveGroups(flat.result.Findings)
	want := 0
	for _, g := range grouped {
		if !g.Key.Unfixed {
			want += len(g.Members)
		}
	}
	if s := remediation.Summarize(flat.result.Findings); s.Fixable() != want {
		t.Fatalf("the invariant of step 1 is broken: Fixable = %d, groups = %d", s.Fixable(), want)
	}
	for name, m := range map[string]Model{"flat": flat, "grouped": groupedModel(t, 160)} {
		value := headerValue(t, m, "Fixable")
		got, err := strconv.Atoi(strings.Fields(value)[0])
		if err != nil || got != want {
			t.Errorf("%s: Fixable = %q, want %d (the 2 CVEs of zlib are not in it)", name, value, want)
		}
	}
	body := ansi.Strip(groupedModel(t, 160).Render())
	for _, word := range []string{"No fix published", "Manual"} {
		if !strings.Contains(body, word) {
			t.Errorf("the grouped list does not say %q in words", word)
		}
	}
}

func TestNoGroupCarriesADoneMark(t *testing.T) {
	typ := reflect.TypeFor[fixGroup]()
	for i := range typ.NumField() {
		switch name := strings.ToLower(typ.Field(i).Name); {
		case strings.Contains(name, "done"), strings.Contains(name, "fixed"), strings.Contains(name, "cleared"), strings.Contains(name, "closed"):
			t.Errorf("fixGroup.%s: a group is derived from the last scan and never marked done", typ.Field(i).Name)
		}
	}
	for _, c := range cveGroupColumns() {
		if strings.EqualFold(c.Title, "Done") || strings.EqualFold(c.Title, "Fixed") {
			t.Errorf("column %q marks a group done", c.Title)
		}
	}
}

// ── Help (Rule 114) ──────────────────────────────────────────────────────────

func TestHelpDocumentsGroupByFix(t *testing.T) {
	content := groupModel(t, 160).GetHelpContent()
	var g, y bool
	for _, kb := range content.KeyBindings {
		switch kb.Key {
		case groupToggleKey:
			g = strings.Contains(strings.ToLower(kb.Description), "group")
		case keymap.Copy:
			y = y || strings.Contains(strings.ToLower(kb.Description), "group")
		}
	}
	if !g {
		t.Error("the help has no g binding that talks about the group")
	}
	if !y {
		t.Error("the help does not say that Y copies a group's command")
	}
	found := false
	for _, s := range content.Sections {
		found = found || (s.Title == "Group by fix" && strings.TrimSpace(s.Body) != "")
	}
	if !found {
		t.Error("the help has no \"Group by fix\" section")
	}
}

// ── Width (Rule 116, Rule 124, AC6) ──────────────────────────────────────────

// Every line the grouped view draws fits the window: the table, the filter bar,
// the info line. The tab bar is out of scope here (a separate bug: it already
// passes 80 columns with two-digit counts).
func TestGroupedViewFitsAtEveryWidth(t *testing.T) {
	for _, width := range []int{80, 120, 175} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			m := feed(t, groupedModel(t, width), testutil.Key("c"))
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
			for _, title := range []string{"Severity", "Package", "Change", "CVEs", "Fix"} {
				if !strings.Contains(header, title) {
					t.Errorf("at %d columns the header %q lost %q", width, header, title)
				}
			}
			footer := strings.Split(m.RenderFooter(width), "\n")
			if len(footer) != m.GetFooterHeight() {
				t.Errorf("the footer is %d lines, GetFooterHeight says %d (Rule 124)", len(footer), m.GetFooterHeight())
			}
			// The last line is the info line, the one before the blank; the tab
			// bar sits above them.
			for i, line := range footer {
				if i == len(footer)-3 && m.showsResultTabs() {
					continue
				}
				if w := ansi.StringWidth(line); w > width {
					t.Errorf("footer line %d is %d cells wide, the window holds %d: %q", i, w, width, ansi.Strip(line))
				}
			}
		})
	}
}

// A long package, long versions and a long sentence still fit.
func TestGroupedViewFitsWithLongNames(t *testing.T) {
	long := strings.Repeat("deeply/nested/", 6) + "module"
	findings := []scan.Finding{
		cve("CVE-1", long, "gomod", strings.Repeat("1", 30)+".0", strings.Repeat("2", 30)+".0", scan.SeverityCritical),
		cve("CVE-2", long+"2", "gomod", "1.0.0", "", scan.SeverityHigh),
	}
	for _, width := range []int{80, 120, 175} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			m := feed(t, groupModelOf(t, findings, width), testutil.Key(groupToggleKey))
			if got, want := m.groups.RenderedWidth(), width-2; got != want {
				t.Errorf("RenderedWidth = %d, want %d", got, want)
			}
			for i, line := range strings.Split(m.Render(), "\n") {
				if w := ansi.StringWidth(line); w > width-2 {
					t.Errorf("body line %d is %d cells, the viewport holds %d", i, w, width-2)
				}
			}
			for _, group := range []string{long, long + "2"} {
				m = cursorOnGroup(t, m, group)
				if w := ansi.StringWidth(m.renderInfoLine(width)); w > width {
					t.Errorf("the info line of %q is %d cells wide, the window holds %d", group, w, width)
				}
			}
		})
	}
}

func TestResizingKeepsTheGroupedListInTheViewport(t *testing.T) {
	m := groupedModel(t, 175)
	for _, width := range []int{80, 120, 175} {
		m = feed(t, m, testutil.Resize(width, 30))
		if got, want := m.groups.RenderedWidth(), width-2; got != want {
			t.Errorf("after a resize to %d: RenderedWidth = %d, want %d", width, got, want)
		}
	}
}
