package security

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-754 (SC2, step 8): the group level and the detail for a Misconfigurations
// group. enter on a group lists its findings, enter there opens the same detail as
// from the flat table, ctrl+o in that detail is the flat path's ctrl+o (same
// confirmation, same write, same re-scan), and the re-scan is the truth: the
// groups are read again from the result that comes back, never marked done. Every
// change happens in Update (Rule 110): these tests feed messages.

// ── Helpers ──────────────────────────────────────────────────────────────────

// mgRescanned is the result of the same directory with the findings whose message
// is in gone left out, as a re-scan after a fix reports it.
func mgRescanned(m Model, gone ...string) *scan.Result {
	var kept []scan.Finding
	for _, f := range m.result.Findings {
		if !slices.Contains(gone, f.Message) {
			kept = append(kept, f)
		}
	}
	return resultOn(m.result.Target, kept...)
}

// mgOpened presses enter on the group of file, rule and Fix label.
func mgOpened(t *testing.T, m Model, file, rule, label string) Model {
	t.Helper()
	m = feed(t, mgCursor(t, m, file, rule, label), testutil.Key("enter"))
	if m.state != StateGroup {
		t.Fatalf("enter on %s %s did not open its group: state %v", file, rule, m.state)
	}
	return m
}

// mgCursorOnMember moves the cursor of the group's table down to the message.
func mgCursorOnMember(t *testing.T, m Model, message string) Model {
	t.Helper()
	for range 14 {
		if f, ok := m.groupTable.Selected(); ok && f.Message == message {
			return m
		}
		m = feed(t, m, testutil.Key("down"))
	}
	t.Fatalf("the cursor of the group never reached %q", message)
	return m
}

func memberMessages(m Model) []string {
	out := messagesOf(m.groupTable.Visible())
	slices.Sort(out)
	return out
}

// mgDetailsOfTheSameFinding opens "stage test" of Dockerfile twice from one
// model: through the group, and from the flat table.
func mgDetailsOfTheSameFinding(t *testing.T) (grouped, flat Model) {
	t.Helper()
	base := mgModel(t, 160)
	flat = feed(t, mgCursorOnFinding(t, base, "stage test"), testutil.Key("enter"))
	if flat.state != StateDetails {
		t.Fatalf("enter on the flat table: state %v", flat.state)
	}
	m := mgOpened(t, feed(t, base, testutil.Key(groupToggleKey)), "Dockerfile", "DS002", labelAvailable)
	grouped = feed(t, mgCursorOnMember(t, m, "stage test"), testutil.Key("enter"))
	if grouped.state != StateDetails || grouped.selectedFinding == nil || grouped.selectedFinding.Message != "stage test" {
		t.Fatalf("enter in the group: state %v", grouped.state)
	}
	return grouped, flat
}

// ── The level ────────────────────────────────────────────────────────────────

func TestEnterOnAMisconfigGroupOpensTheListOfItsFindings(t *testing.T) {
	m := mgGrouped(t, 160)
	m = mgCursor(t, m, "Dockerfile", "DS002", labelAvailable)
	key := selectedGroup(t, m).Key
	m = feed(t, m, testutil.Key("enter"))
	if m.state != StateGroup {
		t.Fatalf("enter did not open the group: state %v", m.state)
	}
	if got, want := memberMessages(m), []string{"stage build", "stage final", "stage test"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the group lists %v, want %v", got, want)
	}
	if m.openGroup == nil || *m.openGroup != key {
		t.Errorf("openGroup = %+v, want the key of the Dockerfile group %+v", m.openGroup, key)
	}
	header := firstLine(ansi.Strip(m.Render()))
	for _, title := range []string{"Severity", "ID", "Title"} {
		if !strings.Contains(header, title) {
			t.Errorf("the group's header %q lacks %q: it uses the findings' columns", header, title)
		}
	}
	if strings.Contains(header, "Findings") || strings.Contains(header, "Rule") {
		t.Errorf("the group's header %q is the grouped list's", header)
	}
}

// What one group holds is everything the file has for that fix, whatever a filter
// hides in the findings table.
func TestAMisconfigGroupListsItsHiddenMembersToo(t *testing.T) {
	m := feed(t, mgGrouped(t, 160), testutil.Key("m"))
	m = mgOpened(t, m, "deploy.yaml", "KSV-0012", "Manual")
	if got := len(m.groupTable.Visible()); got != 3 {
		t.Errorf("the group lists %d findings under a MEDIUM filter, want its 3 (one is LOW)", got)
	}
}

func TestTheMisconfigGroupLevelHasATitleThatNamesTheGroup(t *testing.T) {
	m := mgOpened(t, mgGrouped(t, 160), "Dockerfile", "DS002", labelAvailable)
	title := ansi.Strip(m.GetTitle())
	for _, want := range []string{"Security Scanner", "Dockerfile", "DS002"} {
		if !strings.Contains(title, want) {
			t.Errorf("title %q lacks %q", title, want)
		}
	}
	requireTerminalSafe(t, "title", title)
}

func TestTheMisconfigGroupLevelHasFourKeys(t *testing.T) {
	m := mgOpened(t, mgGrouped(t, 160), "Dockerfile", "DS002", labelAvailable)
	got := m.GetShortcuts()
	if want := []string{"esc", "enter", keymap.Copy, "?"}; !slices.Equal(testutil.ShortcutKeys(got), want) {
		t.Fatalf("the column is %v, want %v", testutil.ShortcutKeys(got), want)
	}
	// This group has no command: Y is in the column and greyed (Rule 130).
	if !testutil.ShortcutDisabled(got, keymap.Copy) || !testutil.ShortcutEnabled(got, "enter") {
		t.Errorf("Y must be greyed and enter lit: %v", got)
	}
}

func TestTheMisconfigGroupLevelKeepsTheHeaderFigures(t *testing.T) {
	results := mgGrouped(t, 160)
	level := mgOpened(t, results, "Dockerfile", "DS002", labelAvailable)
	for _, key := range []string{"Context", "Findings", "Fixable"} {
		if got, want := headerValue(t, level, key), headerValue(t, results, key); got != want {
			t.Errorf("%s = %q in the group level, %q in the list", key, got, want)
		}
	}
}

func TestTheMisconfigGroupLevelHasNoEditModeAndDropsAPaste(t *testing.T) {
	m := mgOpened(t, mgGrouped(t, 160), "Dockerfile", "DS002", labelAvailable)
	if m.InEditMode() {
		t.Error("the group level claims the keyboard")
	}
	before := ansi.Strip(m.Render())
	m = feed(t, m, testutil.Paste("some pasted text"))
	if m.state != StateGroup || ansi.Strip(m.Render()) != before || m.footer.Text() != "" {
		t.Error("a paste changed the group level (Rule 111)")
	}
}

// What belongs to the list and the tabs does nothing one level down, ctrl+o and p
// included: the fix and the filter act on one finding or on the list, not on a
// group.
func TestKeysOfTheListAreInertInTheMisconfigGroupLevel(t *testing.T) {
	for _, key := range []string{"tab", "shift+tab", groupToggleKey, "c", "h", "m", "l", pssFilterKey, "/", ".", "space", writeRemediationKey, keymap.Scan, keymap.Exclude} {
		t.Run(key, func(t *testing.T) {
			m := mgOpened(t, mgGrouped(t, 160), "Dockerfile", "DS002", labelAvailable)
			tab, tokens := m.activeTab, activeTokens(m)
			next, _ := step(t, m, testutil.Key(key))
			if next.state != StateGroup || next.activeTab != tab || next.InEditMode() || next.confirmModal != nil || next.scanAllModal != nil || next.misconfigPending != nil {
				t.Errorf("%s acted in the group level: state %v, tab %d", key, next.state, next.activeTab)
			}
			if !reflect.DeepEqual(activeTokens(next), tokens) || !next.isGrouped() {
				t.Errorf("%s changed the list behind the group", key)
			}
			if got := fixFile(t, next.result.Target); got != fixableDockerfile {
				t.Errorf("%s wrote the file:\n%s", key, got)
			}
		})
	}
}

func TestTheMisconfigGroupLevelFitsAtEveryWidth(t *testing.T) {
	for _, width := range []int{80, 120, 175} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			m := mgOpened(t, mgGrouped(t, width), "Dockerfile", "DS002", labelAvailable)
			if got, want := m.groupTable.RenderedWidth(), width-2; got != want {
				t.Errorf("RenderedWidth = %d, want %d (Rule 116)", got, want)
			}
			for i, line := range strings.Split(m.Render(), "\n") {
				if w := ansi.StringWidth(line); w > width-2 {
					t.Errorf("line %d is %d cells, the viewport holds %d", i, w, width-2)
				}
			}
			if footer := strings.Split(m.RenderFooter(width), "\n"); len(footer) != m.GetFooterHeight() {
				t.Errorf("the footer is %d lines, GetFooterHeight says %d", len(footer), m.GetFooterHeight())
			}
			m = feed(t, m, testutil.Resize(80, 30))
			if got, want := m.groupTable.RenderedWidth(), 78; got != want {
				t.Errorf("after a resize to 80: RenderedWidth = %d, want %d", got, want)
			}
		})
	}
}

// ── The whole path ───────────────────────────────────────────────────────────

func TestFromTheMisconfigGroupedListToOneFindingAndBack(t *testing.T) {
	m := mgGrouped(t, 160)
	m = feed(t, m, testutil.Key("h")) // a filter state to find again
	tokens := activeTokens(m)
	m = mgOpened(t, m, "Dockerfile", "DS002", labelAvailable)
	m = mgCursorOnMember(t, m, "stage test")

	m = feed(t, m, testutil.Key("enter"))
	if m.state != StateDetails || m.selectedFinding == nil || m.selectedFinding.Message != "stage test" || m.detailsParent != StateGroup {
		t.Fatalf("enter in the group: state %v, parent %v", m.state, m.detailsParent)
	}

	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateGroup || m.openGroup == nil {
		t.Fatalf("esc in the detail went to state %v, want the group", m.state)
	}
	if f, ok := m.groupTable.Selected(); !ok || f.Message != "stage test" {
		t.Errorf("back in the group the cursor is on %+v, want the finding that was open", f)
	}

	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateResults || !m.isGrouped() || m.openGroup != nil {
		t.Fatalf("esc in the group went to state %v (grouped %v), want the grouped list", m.state, m.isGrouped())
	}
	if g := selectedGroup(t, m); g.Subject != "Dockerfile" || g.Rule != "DS002" {
		t.Errorf("the grouped list is on %s %s, want the group that was open", g.Subject, g.Rule)
	}
	if !reflect.DeepEqual(activeTokens(m), tokens) {
		t.Errorf("tokens = %v, were %v", activeTokens(m), tokens)
	}
}

// ── The detail is the same detail ────────────────────────────────────────────

func TestTheMisconfigDetailOpenedFromAGroupIsTheFlatDetail(t *testing.T) {
	grouped, flat := mgDetailsOfTheSameFinding(t)
	if got, want := plainLines(grouped), plainLines(flat); !slices.Equal(got, want) {
		t.Errorf("the detail differs:\ngrouped %q\nflat    %q", got, want)
	}
	if !slices.Equal(testutil.ShortcutKeys(grouped.GetShortcuts()), testutil.ShortcutKeys(flat.GetShortcuts())) {
		t.Fatalf("the detail's column differs: %v and %v", testutil.ShortcutKeys(grouped.GetShortcuts()), testutil.ShortcutKeys(flat.GetShortcuts()))
	}
	for i, s := range grouped.GetShortcuts() {
		if f := flat.GetShortcuts()[i]; s.Disabled != f.Disabled || s.Description != f.Description {
			t.Errorf("%s: grouped %+v, flat %+v", s.Key, s, f)
		}
	}
	if a, b := grouped.canFixMisconfig(), flat.canFixMisconfig(); a != b {
		t.Errorf("canFixMisconfig: grouped %+v, flat %+v", a, b)
	}
	if !reflect.DeepEqual(grouped.changePreview, flat.changePreview) {
		t.Errorf("changePreview: grouped %+v, flat %+v", grouped.changePreview, flat.changePreview)
	}
}

// ── ctrl+o from the detail of a finding that came from a group ───────────────

// The same confirmation, the same diff, the same default: the group changes the
// way to the finding, not what is done to it.
func TestCtrlOInAGroupsDetailOpensTheConfirmationOfTheFlatPath(t *testing.T) {
	grouped, flat := mgDetailsOfTheSameFinding(t)
	fromGroup, fromFlat := preparedFrom(t, grouped), preparedFrom(t, flat)
	if fromGroup.confirmModal == nil || fromFlat.confirmModal == nil {
		t.Fatalf("no confirmation (group %v, flat %v)", fromGroup.confirmModal != nil, fromFlat.confirmModal != nil)
	}
	if got, want := fromGroup.confirmModal.View(), fromFlat.confirmModal.View(); got != want {
		t.Errorf("the confirmations differ:\ngroup:\n%s\nflat:\n%s", got, want)
	}
	if fromGroup.state != StateDetails || fromGroup.detailsParent != StateGroup {
		t.Errorf("ctrl+o left the detail: state %v, parent %v", fromGroup.state, fromGroup.detailsParent)
	}
	if got := fixFile(t, fromGroup.result.Target); got != fixableDockerfile {
		t.Errorf("the file was written before the answer:\n%s", got)
	}
	// Answering No leaves the file, the detail and its way back alone.
	no := feed(t, fromGroup, components.ConfirmModalNoMsg{})
	if no.confirmModal != nil || no.misconfigPending != nil || no.state != StateDetails || no.detailsParent != StateGroup {
		t.Errorf("No left the screen changed: state %v, parent %v", no.state, no.detailsParent)
	}
	if got := fixFile(t, no.result.Target); got != fixableDockerfile {
		t.Errorf("No wrote the file:\n%s", got)
	}
}

// mgFixedFromAGroup is the detail of "stage test" opened from the Dockerfile
// group, fixed through the confirmation, and waiting for the verification.
func mgFixedFromAGroup(t *testing.T) Model {
	t.Helper()
	grouped, _ := mgDetailsOfTheSameFinding(t)
	m := writeFix(t, grouped, grouped.result.Target)
	if got := fixFile(t, m.result.Target); !strings.Contains(got, "USER appuser") {
		t.Fatalf("the file was not written:\n%s", got)
	}
	if m.misconfigVerifying == nil || m.state != StateDetails || m.detailsParent != StateGroup {
		t.Fatalf("verifying %v, state %v, parent %v: the write waits on the re-scan under its group", m.misconfigVerifying, m.state, m.detailsParent)
	}
	return m
}

func mgVerdict(m Model, cleared bool, result *scan.Result) MisconfigVerifiedMsg {
	return MisconfigVerifiedMsg{Target: m.result.Target, Rule: "DS002", File: "Dockerfile", Cleared: cleared, Result: result}
}

// The re-scan is the truth: one finding gone, the group is read again with two, and
// the user is back in it, not thrown out to the results.
func TestAFixFromAGroupsDetailRebuildsTheGroupFromTheRescan(t *testing.T) {
	m := mgFixedFromAGroup(t)
	m = feed(t, m, mgVerdict(m, true, mgRescanned(m, "stage test")))

	if m.state != StateGroup || m.openGroup == nil || m.selectedFinding != nil {
		t.Fatalf("state %v, openGroup %v, selectedFinding %v: want the group the detail was opened from", m.state, m.openGroup, m.selectedFinding)
	}
	if got, want := memberMessages(m), []string{"stage build", "stage final"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the group lists %v, want %v", got, want)
	}
	if !strings.Contains(footerText(m, 160), "cleared") {
		t.Errorf("the footer does not give the verdict:\n%s", footerText(m, 160))
	}
	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateResults || !m.isGrouped() {
		t.Fatalf("esc: state %v, grouped %v", m.state, m.isGrouped())
	}
	if body := ansi.Strip(m.Render()); !regexp.MustCompile(`Dockerfile\s+DS002\s+2\s+Built-in fix available`).MatchString(body) {
		t.Errorf("the Dockerfile row does not say 2 after the re-scan:\n%s", body)
	}
	if g := selectedGroup(t, m); g.Subject != "Dockerfile" {
		t.Errorf("the cursor is on %q, want the group that was open", g.Subject)
	}
}

func TestAFixFromAGroupsDetailThatClearsTheWholeGroupReturnsToTheGroupedList(t *testing.T) {
	m := mgFixedFromAGroup(t)
	m = feed(t, m, mgVerdict(m, true, mgRescanned(m, "stage build", "stage test", "stage final")))

	if m.state != StateResults || !m.isGrouped() || m.openGroup != nil || m.selectedFinding != nil {
		t.Fatalf("state %v, grouped %v, openGroup %v: want the grouped list", m.state, m.isGrouped(), m.openGroup)
	}
	for _, r := range mgRows(m) {
		if r.file == "Dockerfile" {
			t.Errorf("the Dockerfile group is still listed: %v", mgRows(m))
		}
	}
	if !strings.Contains(footerText(m, 160), "cleared") {
		t.Errorf("the footer does not give the verdict:\n%s", footerText(m, 160))
	}
}

// A finding the re-scan still reports keeps the detail open, under its group, on
// what the re-scan read; the group still counts it.
func TestAFixFromAGroupsDetailThatSurvivesKeepsTheDetailUnderItsGroup(t *testing.T) {
	m := mgFixedFromAGroup(t)
	again := mgRescanned(m)
	for i := range again.Findings {
		if again.Findings[i].Message == "stage test" {
			again.Findings[i].Line += 4
			again.Findings[i].Description = "Reworded after the fix"
		}
	}
	m = feed(t, m, mgVerdict(m, false, again))

	if m.state != StateDetails || m.detailsParent != StateGroup || m.selectedFinding == nil || m.selectedFinding.Line != 5 {
		t.Fatalf("state %v, parent %v: the finding is still reported, the detail stays under its group", m.state, m.detailsParent)
	}
	if !strings.Contains(footerText(m, 160), "still reported") {
		t.Errorf("the footer does not carry the warning:\n%s", footerText(m, 160))
	}
	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateGroup || len(m.groupTable.Visible()) != 3 {
		t.Errorf("esc: state %v, %d findings: the group still holds its 3", m.state, len(m.groupTable.Visible()))
	}
}

// ── The re-scan is the truth, from the grouped list ──────────────────────────

func TestARescanThatClearsOneFindingShrinksItsMisconfigRowAndKeepsTheCursorOnItsGroup(t *testing.T) {
	m := mgCursor(t, mgGrouped(t, 160), "pod.yaml", "KSV-0104", labelAvailable)
	m.setResult(mgRescanned(m, "stage test"))

	if !m.isGrouped() || m.state != StateResults {
		t.Fatalf("the re-scan left the grouped list: grouped %v, state %v", m.isGrouped(), m.state)
	}
	if body := ansi.Strip(m.Render()); !regexp.MustCompile(`HIGH\s+Dockerfile\s+DS002\s+2\s+Built-in fix available`).MatchString(body) {
		t.Errorf("the Dockerfile row does not say 2:\n%s", body)
	}
	if g := selectedGroup(t, m); g.Subject != "pod.yaml" || g.Status.State != fixBuiltIn {
		t.Errorf("the cursor is on %s %v, want pod.yaml: it is kept by key, not by index", g.Subject, g.Status.State)
	}
}

func TestAMisconfigGroupClearedByTheRescanIsGoneAndTheCursorStaysNearby(t *testing.T) {
	m := mgCursor(t, mgGrouped(t, 160), "deploy.yaml", "KSV-0012", "Manual")
	m.setResult(mgRescanned(m, "Container 'api' may run as root", "Container 'sidecar' may run as root", "Container 'init' may run as root"))

	if len(m.groups.Visible()) != 5 {
		t.Fatalf("groups = %v, want 5", mgRows(m))
	}
	for _, r := range mgRows(m) {
		if r.file == "deploy.yaml" {
			t.Errorf("a cleared group is still listed: %v", mgRows(m))
		}
	}
	if g := selectedGroup(t, m); g.Subject != "pod.yaml" {
		t.Errorf("the cursor is on %q, want the group that took its place", g.Subject)
	}
}

// The re-scan can move a finding from one state of the fix to another: then it is
// another row, and the old one is gone. Nothing remembers what the row said.
func TestARescanThatChangesTheStateOfAFixMovesTheFindingToAnotherRow(t *testing.T) {
	m := mgGrouped(t, 160)
	res := mgRescanned(m)
	for i := range res.Findings {
		if res.Findings[i].Message == "Pod 'job' sets no seccomp profile" {
			res.Findings[i].SeccompOrigin = "unconfined"
		}
	}
	m.setResult(res)
	declined := fixStatusOf(unconfinedFinding(), true).Label()
	var pod []mgRow
	for _, r := range mgRows(m) {
		if r.file == "pod.yaml" {
			pod = append(pod, r)
		}
	}
	if want := []mgRow{{"pod.yaml", "KSV-0104", 2, declined}}; !reflect.DeepEqual(pod, want) {
		t.Errorf("pod.yaml rows = %v, want %v", pod, want)
	}
}

func TestARescanThatClearsTheWholeLevelsGroupStepsBackToTheGroupedList(t *testing.T) {
	m := mgOpened(t, mgGrouped(t, 160), "Dockerfile", "DS002", labelAvailable)
	m.setResult(mgRescanned(m, "stage build", "stage test", "stage final"))
	if m.state != StateResults || !m.isGrouped() || m.openGroup != nil {
		t.Fatalf("state %v, grouped %v, openGroup %v: want the grouped list", m.state, m.isGrouped(), m.openGroup)
	}
}

func TestARescanRefreshesTheMisconfigGroupLevelByKey(t *testing.T) {
	m := mgCursorOnMember(t, mgOpened(t, mgGrouped(t, 160), "Dockerfile", "DS002", labelAvailable), "stage final")
	m.setResult(mgRescanned(m, "stage build"))
	if m.state != StateGroup {
		t.Fatalf("state = %v, the group still exists", m.state)
	}
	if got, want := memberMessages(m), []string{"stage final", "stage test"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the group lists %v, want %v", got, want)
	}
	if f, ok := m.groupTable.Selected(); !ok || f.Message != "stage final" {
		t.Errorf("the cursor is on %+v, want the finding the user was on", f)
	}
}

// With no misconfiguration left, the grouped list is a header and no rows, and g is
// still the way back (Rule 139, Rule 130).
func TestARescanWithNoMisconfigurationLeavesAnEmptyTableAndTheWayBack(t *testing.T) {
	m := mgGrouped(t, 160)
	var rest []scan.Finding
	for _, f := range m.result.Findings {
		if scan.Categorize(f) != scan.CategoryMisconfiguration {
			rest = append(rest, f)
		}
	}
	m.setResult(resultOn(m.result.Target, rest...))
	if !m.isGrouped() || len(m.groups.Visible()) != 0 {
		t.Fatalf("grouped %v, groups %v", m.isGrouped(), mgRows(m))
	}
	if header := firstLine(ansi.Strip(m.Render())); !strings.Contains(header, "Findings") {
		t.Errorf("the body lost its header: %q", header)
	}
	if !testutil.ShortcutEnabled(m.GetShortcuts(), groupToggleKey) {
		t.Fatal("g is greyed in an empty grouped list: no way back")
	}
	if m = feed(t, m, testutil.Key(groupToggleKey)); m.isGrouped() {
		t.Error("g did not bring the findings back")
	}
}
