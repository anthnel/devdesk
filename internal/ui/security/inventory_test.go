package security

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// columnIndex resolves a column by its header rather than by its position, so a
// column inserted elsewhere moves an assertion instead of breaking it. La
// comparaison est exacte, la flèche de tri retirée : un préfixe ferait répondre
// une colonne voisine dont le titre commence pareil.
func columnIndex(t *testing.T, cols []table.Column, title string) int {
	t.Helper()
	for i, col := range cols {
		if strings.TrimSpace(strings.TrimRight(col.Title, "▲▼")) == title {
			return i
		}
	}
	t.Fatalf("no column titled %q among %d columns", title, len(cols))
	return -1
}

// The inventory is what ":sec" lands on. Nothing here reaches a cache file or a
// scanner: the loaders and the scan runners are commands, so the tests feed the
// messages those commands return and assert on the rows.

func TestSecOpensOnTheInventoryRatherThanAForm(t *testing.T) {
	m := New(testConfig())

	if m.state != StateInventory {
		t.Errorf("state = %v on open, want the inventory", m.state)
	}
}

// Sorted by CRITICAL descending: the point of the view is the worst target, and
// reaching it by cycling `.` past ascending on every open is not a default.
func TestTheInventoryOpensOnTheWorstTargetFirst(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)

	visible := m.inventory.Visible()
	if len(visible) != 2 {
		t.Fatalf("%d rows, want both cached targets", len(visible))
	}
	if visible[0].Name != "nexus/api:1.4" {
		t.Errorf("first row = %q, want the 3-critical target", visible[0].Name)
	}
	if column, desc := m.inventory.SortState(); column != inventoryColumnCritical || !desc {
		t.Errorf("sorted on column %d desc=%v, want CRITICAL descending", column, desc)
	}
}

func TestARepositoryRowFoldsTheHomeDirectory(t *testing.T) {
	target := scanTarget{Kind: kindRepo, Name: "/home/dev/ws/devdesk"}
	t.Setenv("HOME", "/home/dev")
	t.Setenv("USERPROFILE", "/home/dev")

	name := target.displayName()

	if !strings.Contains(name, "~") || strings.Contains(name, "/home/dev/") {
		t.Errorf("displayName() = %q, want the home directory folded to ~", name)
	}
}

// The cache key stays searchable even though the row shows a folded path: a
// query for the path a user copied out of a shell has to find its row.
func TestTheFilterMatchesTheCachedKeyNotOnlyWhatIsShown(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)

	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("/home/dev/workspaces")...)

	visible := m.inventory.Visible()
	if len(visible) != 1 || visible[0].Kind != kindRepo {
		t.Fatalf("%d rows match the absolute path, want the one repository", len(visible))
	}
}

func TestFilteringTheInventoryTakesTheKeyboard(t *testing.T) {
	m := feed(t, inventoryModel(t, inventoryFixtures()...), testutil.Key("/"))

	if !m.InEditMode() {
		t.Error("InEditMode() is false while the filter has the keyboard — the router would claim ':' and 'q'")
	}
}

func TestEnterOpensTheStoredFindings(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)

	_, cmd := step(t, m, testutil.Key("enter"))

	msg, ok := testutil.MsgOf[InventoryResultLoadedMsg](cmd)
	if !ok {
		t.Fatal("enter on a scanned row loaded nothing")
	}
	if msg.Name != "nexus/api:1.4" {
		t.Errorf("loaded %q, want the selected row", msg.Name)
	}

	m = feed(t, m, InventoryResultLoadedMsg{Name: msg.Name, Result: resultFixture()})
	if m.state != StateResults {
		t.Errorf("state = %v once the result arrived, want the results", m.state)
	}
	if m.targetPath != "nexus/api:1.4" {
		t.Errorf("targetPath = %q, want the row the findings belong to", m.targetPath)
	}
}

// A row purged by ctrl+a has no result to open until its rescan returns.
func TestEnterOnAPurgedRowSaysThereIsNothingToOpenYet(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m, _ = scanAll(t, m, true)

	m, cmd := step(t, m, testutil.Key("enter"))

	if m.state != StateInventory {
		t.Errorf("state = %v, want to stay on the inventory", m.state)
	}
	if !strings.Contains(m.footer.Text(), "No result yet") {
		t.Errorf("statusMessage = %q, want it to say the result is not there yet", m.footer.Text())
	}
	if cmd == nil {
		t.Error("the message was set without a timer to clear it (Rule 128)")
	}
}

// Rule 130 hides the row actions on an empty inventory, but the keys still
// arrive — the router forwards every one of them.
func TestRowActionsOnAnEmptyInventoryDoNothing(t *testing.T) {
	for _, key := range []string{"enter", keymap.Scan, keymap.ScanAll} {
		t.Run(key, func(t *testing.T) {
			m, cmd := step(t, inventoryModel(t), testutil.Key(key))

			if m.state != StateInventory || m.footer.IsSet() || cmd != nil {
				t.Errorf("%q on an empty inventory produced state %v, message %q, cmd %v",
					key, m.state, m.footer.Text(), cmd != nil)
			}
		})
	}
}

func TestAFilledInventoryRendersItsRows(t *testing.T) {
	rendered := inventoryModel(t, inventoryFixtures()...).View()

	for _, want := range []string{"nexus/api:1.4", "CRIT", "Scanned"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the inventory does not show %q:\n%s", want, rendered)
		}
	}
}

// A result file can outlive its metadata entry and be deleted underneath it.
// The row is still there and ctrl+s still rescans it, so this is a message, not
// a dead end (Rule 128).
func TestAMissingResultIsReportedRatherThanOpened(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)

	m, cmd := step(t, m, InventoryResultLoadedMsg{Name: "nexus/api:1.4", Err: errors.New("no such file")})

	if m.state != StateInventory {
		t.Errorf("state = %v after a missing result, want to stay on the inventory", m.state)
	}
	if !strings.Contains(m.footer.Text(), "nexus/api:1.4") {
		t.Errorf("statusMessage = %q, want it to name the target", m.footer.Text())
	}
	// The command's identity, not its message: it is a three-second tea.Tick,
	// and running it here would make the test take three seconds.
	if cmd == nil {
		t.Error("the message was set without a timer to clear it (Rule 128)")
	}
}

// Esc from a result opened out of the inventory goes back to it and reloads:
// the rescan that was just run changed the caches this view reads.
func TestEscFromAResultReturnsToTheInventoryAndReloadsIt(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m = feed(t, m, InventoryResultLoadedMsg{Name: "nexus/api:1.4", Result: resultFixture()})

	m, cmd := step(t, m, testutil.Key("esc"))

	if m.state != StateInventory {
		t.Errorf("state = %v after esc, want the inventory", m.state)
	}
	if _, ok := testutil.MsgOf[InventoryLoadedMsg](cmd); !ok {
		t.Error("esc did not reload the inventory — a rescan run from here would not show")
	}
}

func TestRescanningOneRowMarksOnlyThatRow(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)

	m, _ = step(t, m, testutil.Key(keymap.Scan))

	for _, target := range m.inventory.Items() {
		wantScanning := target.Name == "nexus/api:1.4"
		if target.Scanning != wantScanning {
			t.Errorf("%s: Scanning = %v, want %v", target.Name, target.Scanning, wantScanning)
		}
		if !target.Scanned {
			t.Errorf("%s: ctrl+s cleared the counts; it overwrites, it does not purge (Rule 126)", target.Name)
		}
	}
}

// Rule 126: ctrl+a purges before rescanning. The rows themselves stay — they are
// the list of what is known to have been scanned, and dropping them would empty
// the view for as long as the scans take.
func TestRescanningAllPurgesTheCountsButKeepsTheTargets(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)

	m, _ = scanAll(t, m, true)

	if len(m.inventory.Items()) != 2 {
		t.Fatalf("%d rows after ctrl+a, want both targets kept", len(m.inventory.Items()))
	}
	for _, target := range m.inventory.Items() {
		if !target.Scanning || target.Scanned {
			t.Errorf("%s: Scanning=%v Scanned=%v, want a purged row with a scan running",
				target.Name, target.Scanning, target.Scanned)
		}
		if target.Counts != (scan.SeverityCounts{}) {
			t.Errorf("%s: counts survived the purge: %+v", target.Name, target.Counts)
		}
	}
}

// A purged row prints "-", not "0". Nothing found and nothing known are
// different answers, and zero is the one that reads as clean.
func TestAPurgedRowPrintsNoCountRatherThanZero(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)

	m, _ = scanAll(t, m, true)

	critical := columnIndex(t, m.inventory.Table().Columns(), "CRIT")
	for _, row := range m.inventory.Table().Rows() {
		if row[critical] != "-" {
			t.Errorf("CRITICAL cell = %q while purged, want %q", row[critical], "-")
		}
	}
}

func TestAFinishedRescanUpdatesItsRowAlone(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m, _ = scanAll(t, m, true)
	scannedAt := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	m = feed(t, m, InventoryScanFinishedMsg{
		Name:      "nexus/api:1.4",
		Counts:    scan.SeverityCounts{Critical: 7},
		ScannedAt: scannedAt,
	})

	for _, target := range m.inventory.Items() {
		if target.Name != "nexus/api:1.4" {
			if target.Scanned || !target.Scanning {
				t.Errorf("%s changed on another target's result", target.Name)
			}
			continue
		}
		if target.Scanning || !target.Scanned || target.Counts.Critical != 7 {
			t.Errorf("the rescanned row = %+v, want it settled on the new counts", target)
		}
		if !target.ScannedAt.Equal(scannedAt) {
			t.Errorf("ScannedAt = %v, want the scan's own end time %v", target.ScannedAt, scannedAt)
		}
	}
}

func TestAFailedRescanMarksTheRowAndSaysSo(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m, _ = step(t, m, testutil.Key(keymap.Scan))

	m, cmd := step(t, m, InventoryScanFinishedMsg{Name: "nexus/api:1.4", Err: errors.New("no such image")})

	target := m.inventory.Items()[0]
	for _, candidate := range m.inventory.Items() {
		if candidate.Name == "nexus/api:1.4" {
			target = candidate
		}
	}
	if target.Scanning || !target.Failed {
		t.Errorf("the failed row = %+v, want it marked failed and no longer scanning", target)
	}
	if !strings.Contains(m.footer.Text(), "nexus/api:1.4") {
		t.Errorf("statusMessage = %q, want it to name the target", m.footer.Text())
	}
	if cmd == nil {
		t.Error("the error message was set without a timer to clear it (Rule 128)")
	}
}

// A reload landing mid-rescan must not clear the spinner: the cache says nothing
// about a scan that has not finished writing to it, so the row would look
// settled while its scan is still running.
func TestAReloadDoesNotSettleARowStillBeingScanned(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m, _ = step(t, m, testutil.Key(keymap.Scan))

	m = feed(t, m, InventoryLoadedMsg{Targets: inventoryFixtures()})

	for _, target := range m.inventory.Items() {
		if target.Name == "nexus/api:1.4" && !target.Scanning {
			t.Error("the reload cleared the in-flight scan marker")
		}
	}
}

// Two concurrent chains make the frames advance at twice the rate.
func TestASecondRescanDoesNotStartASecondSpinnerChain(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)

	if m.spinnerTickIfIdle() == nil {
		t.Fatal("nothing is scanning, so the first rescan must start the spinner")
	}
	m, _ = step(t, m, testutil.Key(keymap.Scan))
	if m.spinnerTickIfIdle() != nil {
		t.Error("a rescan was already running, so a second must not start another chain")
	}
}

// Rule 130: an empty inventory offers none of the per-row actions.
func TestTheInventoryAdvertisesOnlyWhatTheSelectedRowCanDo(t *testing.T) {
	empty := inventoryModel(t)
	filled := inventoryModel(t, inventoryFixtures()...)

	if has(empty.GetShortcuts(), "enter") || has(empty.GetShortcuts(), keymap.Scan) {
		t.Errorf("an empty inventory advertises row actions: %v", empty.GetShortcuts())
	}
	if !has(empty.GetShortcuts(), "ctrl+r") {
		t.Error("an empty inventory cannot be refreshed")
	}
	for _, key := range []string{"enter", keymap.Scan, keymap.ScanAll, "/"} {
		if !has(filled.GetShortcuts(), key) {
			t.Errorf("%q is not advertised on a row that supports it", key)
		}
	}
}

// The caches are per context, so two contexts hold different inventories whose
// rows look identical. The title is what tells them apart.
func TestTheInventoryTitleNamesItsContext(t *testing.T) {
	title := inventoryModel(t).GetTitle()

	if !strings.Contains(title, "Inventory") || !strings.Contains(title, "default") {
		t.Errorf("GetTitle() = %q, want the inventory and its context named", title)
	}
}

func TestAnEmptyInventorySaysWhereScansComeFrom(t *testing.T) {
	rendered := inventoryModel(t).View()

	for _, want := range []string{"Nothing scanned yet", "OCI resources", "workspaces"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the empty inventory does not mention %q:\n%s", want, rendered)
		}
	}
}

// Rule 136: the bar lives in the footer and its height is declared there.
func TestTheFilterBarIsAccountedForInTheFooter(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	base := m.GetFooterHeight()

	m = feed(t, m, testutil.Key("/"))

	if m.GetFooterHeight() <= base {
		t.Errorf("GetFooterHeight() = %d with the bar open, want more than %d", m.GetFooterHeight(), base)
	}
	if !strings.Contains(m.RenderFooter(160), strings.TrimRight(m.inventory.FilterBar().View(), "\n")) {
		t.Error("RenderFooter() does not include the filter bar")
	}
}

func has(shortcuts shortcut.Shortcuts, key string) bool {
	for _, s := range shortcuts {
		if s.Key == key {
			return true
		}
	}
	return false
}

// Rule 136: the bar and the viewport border form one closed rectangle, which
// the router draws from this method. The findings table has no bar to close
// around — it filters by tab and severity, not by query.
func TestOnlyTheInventoryReportsAVisibleFilterBar(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)

	if m.FilterBarVisible() {
		t.Error("the bar is reported visible before any filter was opened")
	}
	m = feed(t, m, testutil.Key("/"))
	if !m.FilterBarVisible() {
		t.Error("the bar is open but not reported, so the viewport border stays broken")
	}

	m = feed(t, m, InventoryResultLoadedMsg{Name: "nexus/api:1.4", Result: resultFixture()})
	if m.FilterBarVisible() {
		t.Error("the results report the inventory's filter bar")
	}
}

// Init loads the inventory whatever the opening state, so a view opened on a
// stored result has something to return to on esc.
func TestInitLoadsTheInventory(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model Model
	}{
		{"opened on the inventory", New(testConfig())},
		{"opened on a result", NewWithPreloadedResult(testConfig(), resultFixture())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := testutil.MsgOf[InventoryLoadedMsg](tc.model.Init()); !ok {
				t.Error("Init() did not load the inventory")
			}
		})
	}
}

// The spinner only advances while a rescan is running: SetItems re-filters and
// re-sorts, and there is no reason to do that sixty times a second for a table
// with nothing running.
func TestTheSpinnerAdvancesOnlyWhileARescanRuns(t *testing.T) {
	settled := inventoryModel(t, inventoryFixtures()...)
	if _, cmd := step(t, settled, spinner.TickMsg{}); cmd != nil {
		t.Error("a settled inventory scheduled another spinner frame")
	}

	scanning, _ := step(t, settled, testutil.Key(keymap.Scan))
	before := scanning.spinner.View()

	next, cmd := step(t, scanning, spinner.TickMsg{ID: scanning.spinner.ID()})

	if cmd == nil {
		t.Error("a running rescan did not schedule the next frame")
	}
	if next.spinner.View() == before {
		t.Error("the frame did not advance, so the row reads as a hung scan")
	}
	// The rows carry the frame, so they have to be restamped with it.
	for _, target := range next.inventory.Items() {
		if target.Scanning && target.SpinnerFrame != next.spinner.View() {
			t.Errorf("the scanning row still carries %q, want the new frame", target.SpinnerFrame)
		}
	}
}
