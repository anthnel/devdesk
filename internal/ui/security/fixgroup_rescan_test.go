package security

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-754 (SC1, step 5): a re-scan after a fix is the only truth. The groups
// are rebuilt from the new result, the cursor stays on its group by key, a group
// that is gone is gone, and the level the user was in follows or steps back to
// the nearest one that still exists. setResult is what a verification re-scan
// hands back; the tests call it as Update does.

// rescanned is the result of the same directory with the findings of the given
// IDs gone, as a re-scan after a fix reports it.
func rescanned(m Model, gone ...string) *scan.Result {
	var kept []scan.Finding
	for _, f := range m.result.Findings {
		if !slices.Contains(gone, f.ID) {
			kept = append(kept, f)
		}
	}
	return groupResultOn(m.result.Target, kept)
}

func TestARescanThatClearsOneCVEShrinksItsRowAndKeepsTheCursorOnItsGroup(t *testing.T) {
	m := cursorOnGroup(t, groupedModel(t, 160), "libfoo")

	m.setResult(rescanned(m, "CVE-1"))

	if !m.isGrouped() || m.state != StateResults {
		t.Fatalf("the re-scan left the grouped list: grouped %v, state %v", m.isGrouped(), m.state)
	}
	line := lineWith(t, ansi.Strip(m.Render()), "openssl")
	if !regexp.MustCompile(`HIGH\s+openssl\s+1\.1\.1k -> 1\.1\.1w\s+2\s+Manual`).MatchString(line) {
		t.Errorf("the openssl row reads %q: two CVEs left, the worst now HIGH", strings.TrimSpace(line))
	}
	if g := selectedGroup(t, m); g.Subject != "libfoo" {
		t.Errorf("the cursor is on %q, want libfoo: it is kept by key, not by index", g.Subject)
	}
}

func TestAGroupClearedByTheRescanIsGoneAndTheCursorStaysNearby(t *testing.T) {
	m := cursorOnGroup(t, groupedModel(t, 160), "zlib")

	m.setResult(rescanned(m, "CVE-5", "CVE-6"))

	if want := []string{"openssl", "golang.org/x/net", "libfoo"}; !reflect.DeepEqual(groupSubjects(m), want) {
		t.Fatalf("groups = %v, want %v", groupSubjects(m), want)
	}
	if body := ansi.Strip(m.Render()); strings.Contains(body, "zlib") || strings.Contains(body, "No fix published") {
		t.Errorf("a cleared group is still on screen:\n%s", body)
	}
	if g := selectedGroup(t, m); g.Subject != "libfoo" {
		t.Errorf("the cursor is on %q, want the group that took its place", g.Subject)
	}
	// What the footer says is about the group now selected.
	if got, want := m.status().Text, groupSentence(selectedGroup(t, m)); got != want || want == "" {
		t.Errorf("the footer says %q, want the sentence of the group now selected (%q)", got, want)
	}
}

func TestARescanWithNoVulnerabilityLeavesAnEmptyTableAndTheWayBack(t *testing.T) {
	m := groupedModel(t, 160)
	var cves []string
	for _, f := range m.result.Findings {
		if scan.Categorize(f) == scan.CategoryVulnerability {
			cves = append(cves, f.ID)
		}
	}

	m.setResult(rescanned(m, cves...))

	if !m.isGrouped() {
		t.Fatal("the grouped mode went with the last group")
	}
	if len(m.groups.Visible()) != 0 {
		t.Errorf("groups = %v", groupSubjects(m))
	}
	if !strings.Contains(firstLine(ansi.Strip(m.Render())), "Package") {
		t.Error("the body stopped being the grouped table (Rule 139)")
	}
	if !testutil.ShortcutEnabled(m.GetShortcuts(), groupToggleKey) {
		t.Fatal("g is greyed in an empty grouped list: there would be no way back")
	}
	m = feed(t, m, testutil.Key(groupToggleKey))
	if m.isGrouped() || !strings.Contains(firstLine(ansi.Strip(m.Render())), "Title") {
		t.Error("g did not bring the findings table back")
	}
}

func TestAFilterAndTheNewResultAreBothApplied(t *testing.T) {
	m := feed(t, groupedModel(t, 160), testutil.Key("c"))
	m.setResult(rescanned(m, "CVE-1")) // the only CRITICAL one

	if !m.findingsTable.IsTokenActive("critical") {
		t.Error("the re-scan dropped the filter the user was looking through")
	}
	if len(m.groups.Visible()) != 0 {
		t.Errorf("groups = %v: no CRITICAL finding is left", groupSubjects(m))
	}
}

// A result that arrives while the list is flat is what the next g shows.
func TestGroupsAreBuiltFromTheResultOnScreenNotFromAMemory(t *testing.T) {
	m := groupedModel(t, 160)
	m = feed(t, m, testutil.Key(groupToggleKey))
	m.setResult(rescanned(m, "CVE-1", "CVE-3"))
	m = feed(t, m, testutil.Key(groupToggleKey))
	line := lineWith(t, ansi.Strip(m.Render()), "openssl")
	if !regexp.MustCompile(`openssl\s+1\.1\.1k -> 1\.1\.1w\s+1\s`).MatchString(line) {
		t.Errorf("the openssl row reads %q, want the new result's single CVE", strings.TrimSpace(line))
	}
}

// ── The group level ──────────────────────────────────────────────────────────

func TestARescanRefreshesTheGroupLevelByKey(t *testing.T) {
	m := cursorOnMember(t, openedGroup(t, groupedModel(t, 160), "openssl"), "CVE-3")

	m.setResult(rescanned(m, "CVE-1"))

	if m.state != StateGroup {
		t.Fatalf("state = %v, the group still exists", m.state)
	}
	if got, want := memberIDs(m.groupTable.Visible()), []string{"CVE-2", "CVE-3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the group lists %v, want %v", got, want)
	}
	if f, ok := m.groupTable.Selected(); !ok || f.ID != "CVE-3" {
		t.Errorf("the cursor is on %+v, want CVE-3: the finding the user was on", f)
	}
}

func TestARescanThatClearsTheWholeGroupStepsBackToTheGroupedList(t *testing.T) {
	m := openedGroup(t, groupedModel(t, 160), "openssl")

	m.setResult(rescanned(m, "CVE-1", "CVE-2", "CVE-3"))

	if m.state != StateResults || !m.isGrouped() || m.openGroup != nil {
		t.Fatalf("state %v, grouped %v, openGroup %v: want the grouped list", m.state, m.isGrouped(), m.openGroup)
	}
	if want := []string{"golang.org/x/net", "zlib", "libfoo"}; !reflect.DeepEqual(groupSubjects(m), want) {
		t.Errorf("groups = %v, want %v", groupSubjects(m), want)
	}
}

// ── The detail opened from a group ───────────────────────────────────────────

func detailOfOpenssl(t *testing.T) Model {
	t.Helper()
	m := cursorOnMember(t, openedGroup(t, groupedModel(t, 160), "openssl"), "CVE-2")
	m = feed(t, m, testutil.Key("enter"))
	if m.state != StateDetails || m.selectedFinding == nil || m.selectedFinding.ID != "CVE-2" {
		t.Fatalf("setup: state %v", m.state)
	}
	return m
}

func TestARescanThatKeepsTheFindingRebuildsTheDetailUnderItsGroup(t *testing.T) {
	m := detailOfOpenssl(t)
	res := rescanned(m)
	for i := range res.Findings {
		if res.Findings[i].ID == "CVE-2" {
			res.Findings[i].Description = "Reworded by the re-scan"
		}
	}

	m.setResult(res)

	if m.state != StateDetails || m.detailsParent != StateGroup {
		t.Fatalf("state %v, parent %v: the detail stays under its group", m.state, m.detailsParent)
	}
	if got := ansi.Strip(m.buildDetailsContent()); !strings.Contains(got, "Reworded by the re-scan") {
		t.Error("the detail does not follow the new reading")
	}
}

func TestARescanThatClearsTheFindingReturnsToItsGroupNotToTheResults(t *testing.T) {
	m := detailOfOpenssl(t)

	m.setResult(rescanned(m, "CVE-2"))

	if m.state != StateGroup {
		t.Fatalf("state = %v, want the group the detail was opened from", m.state)
	}
	if m.selectedFinding != nil {
		t.Error("a closed detail still holds its finding")
	}
	if got, want := memberIDs(m.groupTable.Visible()), []string{"CVE-1", "CVE-3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the group lists %v, want %v", got, want)
	}
}

func TestARescanThatClearsTheGroupOfAnOpenDetailReturnsToTheGroupedList(t *testing.T) {
	m := detailOfOpenssl(t)

	m.setResult(rescanned(m, "CVE-1", "CVE-2", "CVE-3"))

	if m.state != StateResults || !m.isGrouped() || m.openGroup != nil || m.selectedFinding != nil {
		t.Fatalf("state %v, grouped %v, openGroup %v: want the grouped list", m.state, m.isGrouped(), m.openGroup)
	}
}

// The parent is set by the way in, never kept from an earlier visit.
func TestAFlatDetailAfterAGroupVisitReturnsToTheResults(t *testing.T) {
	m := detailOfOpenssl(t)
	m = feed(t, m, testutil.Key("esc"), testutil.Key("esc"), testutil.Key(groupToggleKey))
	if m.state != StateResults || m.isGrouped() {
		t.Fatalf("setup: state %v, grouped %v", m.state, m.isGrouped())
	}
	m = feed(t, m, testutil.Key("enter"))
	if m.state != StateDetails || m.detailsParent != StateResults {
		t.Fatalf("state %v, parent %v: a flat enter has the results for parent", m.state, m.detailsParent)
	}
	m.setResult(rescanned(m, m.selectedFinding.ID))
	if m.state != StateResults {
		t.Errorf("a cleared flat detail went to state %v, want the results", m.state)
	}
}
