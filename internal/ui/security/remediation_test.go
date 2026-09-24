package security

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/dockerfile"
	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Fixtures ─────────────────────────────────────────────────────────────────

func baseEntries() []remediation.Entry {
	return []remediation.Entry{
		{
			File: "Dockerfile", StageLabel: "build", Image: "golang:1.21",
			Stage:      dockerfile.Stage{Image: "golang:1.21"},
			Candidates: []string{"golang:1.23", "golang:1.22"},
		},
		{
			File: "Dockerfile", StageLabel: "#2", Image: "gcr.io/distroless/static:nonroot",
			Stage:  dockerfile.Stage{Image: "gcr.io/distroless/static:nonroot"},
			Reason: "the tag carries no version to move from",
		},
	}
}

// onRemediation opens the results on a repository, goes to the Remediation tab
// and hands it the entries — what reading the Dockerfiles would have found.
// The Cmds are dropped, so nothing reaches a registry or a scanner.
func onRemediation(t *testing.T, entries []remediation.Entry, results map[string]cache.RemediationEntry) Model {
	t.Helper()
	m := scannedModel(t)
	for range TabRemediation {
		m = feed(t, m, testutil.Key("tab"))
	}
	if results == nil {
		results = map[string]cache.RemediationEntry{}
	}
	return feed(t, m, RemediationDiscoveredMsg{Target: m.result.Target, Entries: entries, Results: results})
}

func scannedAt(critical, high int) cache.RemediationEntry {
	return cache.RemediationEntry{Critical: critical, High: high, ScannedAt: time.Now()}
}

// ── Rows ─────────────────────────────────────────────────────────────────────

func TestEachBaseImageIsFollowedByItsCandidates(t *testing.T) {
	rows := remediationRows(baseEntries(), nil, nil, nil)
	var images []string
	for _, r := range rows {
		images = append(images, strings.TrimSpace(r.Image))
	}
	want := []string{"golang:1.21", "golang:1.23", "golang:1.22", "gcr.io/distroless/static:nonroot"}
	if !reflect.DeepEqual(images, want) {
		t.Errorf("rows = %v, want %v", images, want)
	}
	if !rows[0].Current || rows[1].Current || rows[2].Current || !rows[3].Current {
		t.Errorf("Current flags = %v %v %v %v", rows[0].Current, rows[1].Current, rows[2].Current, rows[3].Current)
	}
}

func TestACandidateIsIndentedAndItsFileAndStageAreLeftBlank(t *testing.T) {
	rows := remediationRows(baseEntries(), nil, nil, nil)
	if !strings.HasPrefix(rows[1].Image, "  ") || rows[1].File != "" || rows[1].Stage != "" {
		t.Errorf("candidate row = %+v", rows[1])
	}
	if rows[0].File != "Dockerfile" || rows[0].Stage != "build" {
		t.Errorf("current row = %+v", rows[0])
	}
}

// An image with no candidates says why, in its own row.
func TestAnImageWithNoCandidateCarriesItsReason(t *testing.T) {
	rows := remediationRows(baseEntries(), nil, nil, nil)
	if rows[3].Note != "the tag carries no version to move from" {
		t.Errorf("Note = %q", rows[3].Note)
	}
	if rows[0].Note != "" {
		t.Errorf("an image with candidates has a note: %q", rows[0].Note)
	}
}

func TestTheDeltaIsTheChangeInCriticalPlusHigh(t *testing.T) {
	results := map[string]cache.RemediationEntry{
		"golang:1.21": scannedAt(2, 8),
		"golang:1.23": scannedAt(0, 3),
		"golang:1.22": scannedAt(3, 9),
	}
	rows := remediationRows(baseEntries(), results, nil, nil)

	if _, ok := rows[0].delta(); ok {
		t.Error("the current image has a delta against itself")
	}
	if d, ok := rows[1].delta(); !ok || d != -7 {
		t.Errorf("1.23 delta = %d, %v; want -7 (3 against 10)", d, ok)
	}
	if d, ok := rows[2].delta(); !ok || d != 2 {
		t.Errorf("1.22 delta = %d, %v; want +2 (12 against 10)", d, ok)
	}
}

// A delta needs both sides: with the current image unmeasured, a candidate's
// count says nothing about a bump.
func TestThereIsNoDeltaUntilBothSidesAreScanned(t *testing.T) {
	onlyCandidate := map[string]cache.RemediationEntry{"golang:1.23": scannedAt(0, 3)}
	rows := remediationRows(baseEntries(), onlyCandidate, nil, nil)
	if _, ok := rows[1].delta(); ok {
		t.Error("a candidate has a delta against an image nobody scanned")
	}

	onlyCurrent := map[string]cache.RemediationEntry{"golang:1.21": scannedAt(2, 8)}
	rows = remediationRows(baseEntries(), onlyCurrent, nil, nil)
	if _, ok := rows[1].delta(); ok {
		t.Error("an unscanned candidate has a delta")
	}
}

func TestAnImageBeingScannedIsMarked(t *testing.T) {
	rows := remediationRows(baseEntries(), nil, map[string]bool{"golang:1.23": true}, nil)
	if !rows[1].Scanning || rows[0].Scanning || rows[2].Scanning {
		t.Errorf("Scanning flags = %v %v %v", rows[0].Scanning, rows[1].Scanning, rows[2].Scanning)
	}
}

// ── The tab ──────────────────────────────────────────────────────────────────

func TestTabReachesTheRemediationTabAndCyclesBack(t *testing.T) {
	m := scannedModel(t)
	for range TabRemediation {
		m = feed(t, m, testutil.Key("tab"))
	}
	if m.activeTab != TabRemediation {
		t.Fatalf("landed on tab %d, want Remediation", m.activeTab)
	}
	m = feed(t, m, testutil.Key("tab"))
	if m.activeTab != TabCVE {
		t.Errorf("one more tab landed on %d, want the first", m.activeTab)
	}
	if m = feed(t, m, testutil.Key("shift+tab")); m.activeTab != TabRemediation {
		t.Errorf("shift+tab from the first landed on %d, want Remediation", m.activeTab)
	}
}

// Opening the tab starts reading the Dockerfiles — once, and only for a
// repository.
func TestOpeningTheTabReadsTheDockerfilesOnce(t *testing.T) {
	m := scannedModel(t)
	for range TabRemediation - 1 {
		m = feed(t, m, testutil.Key("tab"))
	}
	m, cmd := step(t, m, testutil.Key("tab"))
	if cmd == nil || m.remediation.phase != phaseLoading {
		t.Fatalf("opening the tab: cmd %v, phase %d; want a read in flight", cmd != nil, m.remediation.phase)
	}
	if m.remediation.target != "/tmp/repo" {
		t.Errorf("target = %q, want the repository", m.remediation.target)
	}

	// Away and back: nothing is read again.
	m = feed(t, m, testutil.Key("tab"))
	for range tabCount - 1 {
		m = feed(t, m, testutil.Key("tab"))
	}
	if m.activeTab != TabRemediation {
		t.Fatalf("landed on %d", m.activeTab)
	}
	if _, cmd := step(t, feed(t, m, testutil.Key("tab")), testutil.Key("shift+tab")); cmd != nil {
		t.Error("returning to the tab read the Dockerfiles again")
	}
}

func TestAnImageHasNoDockerfileAndTheTabSaysSo(t *testing.T) {
	result := &scan.Result{Target: "nexus/api:1.4", TargetType: scan.TargetImage, Findings: findingFixtures()}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})
	for range TabRemediation - 1 {
		m = feed(t, m, testutil.Key("tab"))
	}
	m, cmd := step(t, m, testutil.Key("tab"))

	if cmd != nil {
		t.Error("an image scan started reading Dockerfiles")
	}
	status := m.status()
	if !strings.Contains(status.Text, "no Dockerfile") {
		t.Errorf("status = %q, want it to say an image has no Dockerfile", status.Text)
	}
}

func TestTheDiscoveredEntriesFillTheTable(t *testing.T) {
	m := onRemediation(t, baseEntries(), nil)
	if got := len(m.remediation.table.Items()); got != 4 {
		t.Errorf("table holds %d rows, want 4", got)
	}
	if m.remediation.phase != phaseReady {
		t.Errorf("phase = %d, want ready", m.remediation.phase)
	}
	if got := m.remediationTabLabel(); got != "Remediation (2)" {
		t.Errorf("tab label = %q, want the base image count", got)
	}
}

// An answer about another repository is not shown on this one.
func TestADiscoveryForAnotherTargetIsDropped(t *testing.T) {
	m := scannedModel(t)
	for range TabRemediation {
		m = feed(t, m, testutil.Key("tab"))
	}
	m = feed(t, m, RemediationDiscoveredMsg{Target: "/somewhere/else", Entries: baseEntries()})
	if len(m.remediation.table.Items()) != 0 || m.remediation.phase != phaseLoading {
		t.Errorf("a stale answer landed: %d rows, phase %d", len(m.remediation.table.Items()), m.remediation.phase)
	}
}

func TestAFailedDiscoveryIsReportedInTheFooter(t *testing.T) {
	m := scannedModel(t)
	for range TabRemediation {
		m = feed(t, m, testutil.Key("tab"))
	}
	m = feed(t, m, RemediationDiscoveredMsg{Target: m.result.Target, Err: errors.New("boom")})
	if m.footer.Level() != sharedcomponents.LevelError || !strings.Contains(m.footer.Text(), "Dockerfiles") {
		t.Errorf("footer = %q at level %v", m.footer.Text(), m.footer.Level())
	}
}

// The body is always the table (Rule 139): loading and empty are the footer's.
func TestTheBodyIsAlwaysTheTable(t *testing.T) {
	m := scannedModel(t)
	for range TabRemediation {
		m = feed(t, m, testutil.Key("tab"))
	}
	for name, view := range map[string]string{
		"loading": plain(m.View()),
		"empty":   plain(feed(t, m, RemediationDiscoveredMsg{Target: m.result.Target}).View()),
	} {
		if !strings.Contains(view, "Image") || !strings.Contains(view, "vs now") {
			t.Errorf("%s: the body lost the table's header:\n%s", name, view)
		}
	}
}

func TestTheFooterSaysWhatTheEmptyTableMeans(t *testing.T) {
	m := onRemediation(t, nil, nil)
	if got := m.status().Text; !strings.Contains(got, "No Dockerfile") {
		t.Errorf("status = %q", got)
	}
}

func TestTheFooterSaysItIsReading(t *testing.T) {
	m := scannedModel(t)
	for range TabRemediation {
		m = feed(t, m, testutil.Key("tab"))
	}
	if st := m.status(); !st.Spinner || !strings.Contains(st.Text, "Reading") {
		t.Errorf("status = %+v, want a spinner reading the Dockerfiles", st)
	}
}

// A new result forgets the tab: it belongs to the repository it was read for.
func TestOpeningAnotherResultResetsTheTab(t *testing.T) {
	m := onRemediation(t, baseEntries(), nil)
	m = feed(t, m, InventoryResultLoadedMsg{Name: "/tmp/other", Result: &scan.Result{
		Target: "/tmp/other", TargetType: scan.TargetDirectory, Findings: findingFixtures(),
	}})
	if m.remediation.phase != phaseIdle || len(m.remediation.entries) != 0 || len(m.remediation.table.Items()) != 0 {
		t.Errorf("the tab kept %d entries in phase %d", len(m.remediation.entries), m.remediation.phase)
	}
}

// ── Scanning ─────────────────────────────────────────────────────────────────

func TestScanIsRefusedOffTheRemediationTabWithItsReason(t *testing.T) {
	m := scannedModel(t)
	m, cmd := step(t, m, testutil.Key(keymap.Scan))
	if m.footer.Text() != reasonNotRemediationTab || m.footer.Level() != sharedcomponents.LevelWarning {
		t.Errorf("footer = %q at level %v, want the reason as a warning", m.footer.Text(), m.footer.Level())
	}
	if cmd == nil {
		t.Error("the refusal has no timer, so it would never disappear")
	}
	if len(m.remediation.scanning) != 0 {
		t.Error("a scan started off the tab")
	}
}

func TestScanStartsEveryUnmeasuredImageOnce(t *testing.T) {
	m := onRemediation(t, baseEntries(), nil)
	m, cmd := step(t, m, testutil.Key(keymap.Scan))

	if cmd == nil {
		t.Fatal("S started nothing")
	}
	want := map[string]bool{"golang:1.21": true, "golang:1.23": true, "golang:1.22": true, "gcr.io/distroless/static:nonroot": true}
	if !reflect.DeepEqual(m.remediation.scanning, want) {
		t.Errorf("scanning = %v, want %v", m.remediation.scanning, want)
	}
	if st := m.status(); !st.Spinner || !strings.Contains(st.Text, "4 image") {
		t.Errorf("status = %+v", st)
	}
	if !m.spinnerAlive() {
		t.Error("nothing keeps the spinner going while images are scanned")
	}
}

// Two stages on one base are one scan.
func TestAnImageNamedTwiceIsScannedOnce(t *testing.T) {
	entries := []remediation.Entry{
		{File: "a/Dockerfile", StageLabel: "#1", Image: "alpine:3.18", Candidates: []string{"alpine:3.20"}},
		{File: "b/Dockerfile", StageLabel: "#1", Image: "alpine:3.18", Candidates: []string{"alpine:3.20"}},
	}
	m := onRemediation(t, entries, nil)
	if got := m.refsToScan(time.Now()); len(got) != 2 {
		t.Errorf("refs = %v, want alpine:3.18 and alpine:3.20 once each", got)
	}
}

func TestAFreshResultIsNotScannedAgainButAStaleOneIs(t *testing.T) {
	stale := cache.RemediationEntry{ScannedAt: time.Now().Add(-remediationFreshFor - time.Hour)}
	m := onRemediation(t, baseEntries()[:1], map[string]cache.RemediationEntry{
		"golang:1.21": scannedAt(1, 1), // fresh
		"golang:1.23": stale,
	})
	got := m.refsToScan(time.Now())
	if want := []string{"golang:1.23", "golang:1.22"}; !reflect.DeepEqual(got, want) {
		t.Errorf("refs = %v, want %v (the fresh one skipped, the stale one and the unscanned kept)", got, want)
	}
}

// A floating tag's result is about what the tag pointed to then, which the
// registry may have replaced: S measures it again, however fresh (§3.79).
func TestAFloatingImageIsScannedAgainWhateverItsAge(t *testing.T) {
	entries := []remediation.Entry{
		{File: "Dockerfile", StageLabel: "#1", Image: "dhi.io/node:dev", Floating: true, Reason: remediation.ReasonFloating},
		{File: "Dockerfile", StageLabel: "#2", Image: "alpine:3.20"},
	}
	m := onRemediation(t, entries, map[string]cache.RemediationEntry{
		"dhi.io/node:dev": scannedAt(0, 1),
		"alpine:3.20":     scannedAt(0, 1),
	})
	if got, want := m.refsToScan(time.Now()), []string{"dhi.io/node:dev"}; !reflect.DeepEqual(got, want) {
		t.Errorf("refs = %v, want %v (the floating one only)", got, want)
	}
	if a := m.canScanCandidates(); !a.Enabled() {
		t.Errorf("S is refused with %q while a floating image can be measured again", a.Reason)
	}
	rows := remediationRows(entries, m.remediation.results, nil, nil)
	if rows[0].Note != remediation.ReasonFloating || !rows[0].Scanned {
		t.Errorf("floating row = %+v, want its earlier count shown with the reason", rows[0])
	}
}

func TestScanIsRefusedWhenEverythingIsMeasured(t *testing.T) {
	m := onRemediation(t, baseEntries()[:1], map[string]cache.RemediationEntry{
		"golang:1.21": scannedAt(1, 1), "golang:1.23": scannedAt(1, 1), "golang:1.22": scannedAt(1, 1),
	})
	if a := m.canScanCandidates(); a.Enabled() || a.Reason != reasonAllMeasured {
		t.Errorf("availability = %+v, want the reason", a)
	}
}

func TestScanIsRefusedWithNoBaseImage(t *testing.T) {
	m := onRemediation(t, nil, nil)
	if a := m.canScanCandidates(); a.Enabled() || a.Reason != reasonNoBaseImage {
		t.Errorf("availability = %+v, want %q", a, reasonNoBaseImage)
	}
}

// An unresolvable FROM is a row but not an image: there is nothing to scan.
func TestAnUnresolvedReferenceIsNotScanned(t *testing.T) {
	m := onRemediation(t, []remediation.Entry{
		{File: "Dockerfile", StageLabel: "#1", Stage: dockerfile.Stage{Written: "${BASE}"}, Reason: "the reference uses a build arg with no default"},
	}, nil)
	if got := m.refsToScan(time.Now()); len(got) != 0 {
		t.Errorf("refs = %v, want none", got)
	}
	if got := m.remediation.table.Items()[0].Image; got != "${BASE}" {
		t.Errorf("the row shows %q, want what was written", got)
	}
}

func TestAFinishedScanFillsItsRowAndTheComparison(t *testing.T) {
	m := onRemediation(t, baseEntries()[:1], map[string]cache.RemediationEntry{"golang:1.21": scannedAt(2, 8)})
	m, _ = step(t, m, testutil.Key(keymap.Scan))

	m = feed(t, m, RemediationScanFinishedMsg{Target: m.remediation.target, Ref: "golang:1.23", Entry: scannedAt(0, 3)})

	if m.remediation.scanning["golang:1.23"] {
		t.Error("the image is still marked as scanning")
	}
	row := m.remediation.table.Items()[1]
	if !row.Scanned || row.Counts.High != 3 {
		t.Errorf("row = %+v", row)
	}
	if d, ok := row.delta(); !ok || d != -7 {
		t.Errorf("delta = %d, %v", d, ok)
	}
}

func TestTheLastFinishedScanSaysSo(t *testing.T) {
	m := onRemediation(t, baseEntries()[:1], map[string]cache.RemediationEntry{
		"golang:1.21": scannedAt(1, 1), "golang:1.23": scannedAt(1, 1),
	})
	m, _ = step(t, m, testutil.Key(keymap.Scan)) // only golang:1.22 is left
	m = feed(t, m, RemediationScanFinishedMsg{Target: m.remediation.target, Ref: "golang:1.22", Entry: scannedAt(0, 0)})
	if m.footer.Text() != "Candidate scans finished" || m.footer.Level() != sharedcomponents.LevelInfo {
		t.Errorf("footer = %q at level %v", m.footer.Text(), m.footer.Level())
	}
}

func TestAFailedScanIsReportedAndDoesNotStickTheRow(t *testing.T) {
	m := onRemediation(t, baseEntries()[:1], nil)
	m, _ = step(t, m, testutil.Key(keymap.Scan))
	m = feed(t, m, RemediationScanFinishedMsg{Target: m.remediation.target, Ref: "golang:1.23", Err: errors.New("boom")})

	if m.footer.Level() != sharedcomponents.LevelError || !strings.Contains(m.footer.Text(), "golang:1.23") {
		t.Errorf("footer = %q at level %v", m.footer.Text(), m.footer.Level())
	}
	if m.remediation.scanning["golang:1.23"] {
		t.Error("a failed scan leaves its row spinning")
	}
	if m.remediation.table.Items()[1].Scanned {
		t.Error("a failed scan marks its row as measured")
	}
}

func TestAScanResultForAnotherTargetIsDropped(t *testing.T) {
	m := onRemediation(t, baseEntries()[:1], nil)
	m = feed(t, m, RemediationScanFinishedMsg{Target: "/elsewhere", Ref: "golang:1.23", Entry: scannedAt(9, 9)})
	if _, ok := m.remediation.results["golang:1.23"]; ok {
		t.Error("a scan of another repository's image landed here")
	}
}

// ── Keys and shortcuts ───────────────────────────────────────────────────────

// Rule 130: the column does not change from one tab to another — the keys that
// do not apply are greyed, S included.
func TestTheShortcutColumnIsTheSameOnEveryTab(t *testing.T) {
	m := scannedModel(t)
	first := testutil.ShortcutKeys(m.GetShortcuts())
	for tab := 1; tab < tabCount; tab++ {
		m = feed(t, m, testutil.Key("tab"))
		if got := testutil.ShortcutKeys(m.GetShortcuts()); !reflect.DeepEqual(got, first) {
			t.Errorf("tab %d advertises %v, want %v", tab, got, first)
		}
	}
}

func TestTheFindingsKeysAreGreyedOnTheRemediationTab(t *testing.T) {
	m := onRemediation(t, baseEntries(), nil)
	shortcuts := m.GetShortcuts()
	for _, key := range []string{"c", "h", "m", "l", "/", "."} {
		if !testutil.ShortcutDisabled(shortcuts, key) {
			t.Errorf("%q is not greyed on the Remediation tab", key)
		}
	}
	if !testutil.ShortcutEnabled(shortcuts, keymap.Scan) {
		t.Error("S is not offered on the Remediation tab")
	}
	if testutil.ShortcutEnabled(scannedModel(t).GetShortcuts(), keymap.Scan) {
		t.Error("S is offered on a findings tab")
	}
}

// The keys that work the findings table are refused on this tab, with the
// reason, and never reach it: a search opened here would take the keyboard for a
// table nobody can see.
func TestAFindingsKeyIsRefusedOnTheRemediationTab(t *testing.T) {
	for _, key := range []string{"/", "c", "."} {
		m := onRemediation(t, baseEntries(), nil)
		m = feed(t, m, testutil.Key(key))
		if m.footer.Text() != reasonFindingsOnly || m.footer.Level() != sharedcomponents.LevelWarning {
			t.Errorf("%q: footer = %q at level %v", key, m.footer.Text(), m.footer.Level())
		}
		if m.InEditMode() || m.findingsTable.IsTokenActive("critical") {
			t.Errorf("%q reached the findings table behind the tab", key)
		}
	}
}

func TestTheCursorMovesThroughTheRemediationRows(t *testing.T) {
	m := onRemediation(t, baseEntries(), nil)
	m = feed(t, m, testutil.Key("down"), testutil.Key("down"))
	if got := m.remediation.table.Cursor(); got != 2 {
		t.Errorf("cursor = %d, want 2", got)
	}
}

// The tab's table is its own, so the findings filter bar is not drawn for it.
func TestNoFilterBarIsShownOnTheRemediationTab(t *testing.T) {
	m := scannedModel(t)
	m = feed(t, m, testutil.Key("c")) // a severity token: the bar is visible on a findings tab
	if !m.FilterBarVisible() {
		t.Fatal("setup: the findings bar should be visible")
	}
	for range TabRemediation {
		m = feed(t, m, testutil.Key("tab"))
	}
	if m.FilterBarVisible() {
		t.Error("the findings filter bar is drawn under the Remediation table")
	}
}

// esc still leaves, and ctrl+r still goes home: the tab does not take them.
func TestLeavingKeysStillWorkOnTheRemediationTab(t *testing.T) {
	m := onRemediation(t, baseEntries(), nil)
	if m = feed(t, m, testutil.Key("ctrl+r")); m.state != StateInventory {
		t.Errorf("ctrl+r left the view in state %d, want the inventory", m.state)
	}
}
