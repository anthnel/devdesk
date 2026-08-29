package security

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
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
	m := New(testConfig(), nil)

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

	name := target.shortName()

	if !strings.Contains(name, "~") || strings.Contains(name, "/home/dev/") {
		t.Errorf("shortName() = %q, want the home directory folded to ~", name)
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

// Rule 130 greys the row actions on an empty inventory, and the keys still
// arrive — the router forwards every one of them. Each is refused with a reason
// rather than swallowed: a key that looks pressable and answers nothing is what
// this replaced.
func TestRowActionsOnAnEmptyInventoryAreRefusedWithAReason(t *testing.T) {
	for _, tt := range []struct{ key, reason string }{
		{"enter", reasonNoTarget},
		{keymap.Scan, reasonNoTarget},
		{keymap.ScanAll, reasonEmptyList},
	} {
		t.Run(tt.key, func(t *testing.T) {
			m, _ := step(t, inventoryModel(t), testutil.Key(tt.key))

			if m.state != StateInventory {
				t.Errorf("%q left the inventory for state %v", tt.key, m.state)
			}
			if got := m.footer.Text(); !strings.Contains(got, tt.reason) {
				t.Errorf("%q was declined with %q, want it to carry %q", tt.key, got, tt.reason)
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

	m, cmd := step(t, m, testutil.Key(keymap.Scan))

	wantScanRun(t, cmd, "nexus/api:1.4")

	m = scanning(t, m, "nexus/api:1.4")
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

	m, cmd := scanAll(t, m, true)
	wantScanRun(t, cmd, "nexus/api:1.4", "/home/dev/workspaces/devdesk")
	m = scanning(t, m, "nexus/api:1.4", "/home/dev/workspaces/devdesk")

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
	m = scanning(t, m, "nexus/api:1.4", "/home/dev/workspaces/devdesk")
	scannedAt := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	// One target settles; the other is still running. The router applies the
	// transition before handing the message on, so the snapshot comes first.
	half := scanningRun("nexus/api:1.4", "/home/dev/workspaces/devdesk")
	half.Items[0].State = jobs.ItemDone
	m = withJobs(t, m, half)
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
	m = scanning(t, m, "nexus/api:1.4")

	m = withJobs(t, m, settledScanRun("nexus/api:1.4"))
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
//
// It used to be carried across by hand in handleInventoryLoaded. It is now
// structural — the flag is derived from the registry in setInventory, so a
// reload has nothing to clear — and that is what this pins.
func TestAReloadDoesNotSettleARowStillBeingScanned(t *testing.T) {
	m := scanning(t, inventoryModel(t, inventoryFixtures()...), "nexus/api:1.4")

	m = feed(t, m, InventoryLoadedMsg{Targets: inventoryFixtures()})

	for _, target := range m.inventory.Items() {
		if target.Name == "nexus/api:1.4" && !target.Scanning {
			t.Error("the reload cleared the in-flight scan marker")
		}
	}
}

// The inventory lists exactly what `ws` and the images tab scan, so a rescan
// launched from either of them is the same work on the same cache entry. The
// per-view flag could not see it; the registry can.
func TestAScanStartedElsewhereMarksTheInventoryRow(t *testing.T) {
	fromWorkspaces := jobs.NewRun(jobs.KindScan, command.ViewWorkspaces, "default", "~/work", "/home/dev/workspaces/devdesk")
	fromWorkspaces.Items[0].State = jobs.ItemRunning

	m := withJobs(t, inventoryModel(t, inventoryFixtures()...), fromWorkspaces)

	if !m.inventoryScanning() {
		t.Error("the inventory reports itself idle while one of its targets is being scanned")
	}
	for _, target := range m.inventory.Items() {
		if target.Name == "/home/dev/workspaces/devdesk" && !target.Scanning {
			t.Error("a scan started from the workspaces view is invisible in the inventory")
		}
	}
}

// Rule 130: an empty inventory greys the per-row actions, and advertises the
// same keys in the same order as a full one — the first scan of a context used
// to make four entries appear at once.
func TestTheInventoryGreysWhatTheSelectedRowCannotDo(t *testing.T) {
	empty := inventoryModel(t)
	filled := inventoryModel(t, inventoryFixtures()...)

	emptyKeys := testutil.ShortcutKeys(empty.GetShortcuts())
	filledKeys := testutil.ShortcutKeys(filled.GetShortcuts())
	if strings.Join(emptyKeys, " ") != strings.Join(filledKeys, " ") {
		t.Errorf("an empty inventory advertises %v, want the same keys as a full one %v", emptyKeys, filledKeys)
	}

	for _, key := range []string{"enter", keymap.Scan, keymap.ScanAll, "/"} {
		if !testutil.ShortcutDisabled(empty.GetShortcuts(), key) {
			t.Errorf("%q is offered on an empty inventory", key)
		}
		if !testutil.ShortcutEnabled(filled.GetShortcuts(), key) {
			t.Errorf("%q is greyed on a row that supports it", key)
		}
	}
	if !testutil.ShortcutEnabled(empty.GetShortcuts(), "ctrl+r") {
		t.Error("an empty inventory cannot be refreshed")
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
// the router draws from this method. Both tables have a bar, so what is
// reported is the bar of whichever one is on screen — and a state that opens
// with no filter of its own reports none.
func TestTheReportedFilterBarFollowsTheTableOnScreen(t *testing.T) {
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
		{"opened on the inventory", New(testConfig(), nil)},
		{"opened on a result", NewWithPreloadedResult(testConfig(), nil, resultFixture())},
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
// The scan frame comes from the router now, which holds the one chain for the
// whole application (D5). The rows carry it, so a snapshot that moves the frame
// has to restamp them — one that did not would freeze the spinner while the
// chain kept ticking.
func TestASnapshotRestampsTheScanningRowsWithItsFrame(t *testing.T) {
	settled := inventoryModel(t, inventoryFixtures()...)
	if _, cmd := step(t, settled, spinner.TickMsg{}); cmd != nil {
		t.Error("a settled inventory scheduled another spinner frame of its own")
	}

	run := scanningRun("nexus/api:1.4")
	m := withFrame(t, settled, "one", run)
	m = withFrame(t, m, "two", run)

	for _, target := range m.inventory.Items() {
		if target.Name != "nexus/api:1.4" {
			continue
		}
		if target.SpinnerFrame != "two" {
			t.Errorf("the scanning row carries %q, want the frame the snapshot brought", target.SpinnerFrame)
		}
	}
}

// D65 : l'inventaire affichait « Nothing scanned yet » puis la liste. Le message
// est une affirmation sur ce que les caches contiennent, et la vue ne l'a pas
// encore lue — Rule 139 : la table reste à l'écran, le footer dit qu'on charge,
// et le message n'est vrai qu'une fois la réponse arrivée.
func TestNothingScannedYetWaitsForTheCachesToAnswer(t *testing.T) {
	m := feed(t, New(testConfig(), nil), tea.WindowSizeMsg{Width: 160, Height: 30})

	if got := m.View(); strings.Contains(got, "Nothing scanned yet") {
		t.Errorf("the inventory claims nothing was scanned before reading the caches:\n%s", got)
	}
	if got := m.View(); !strings.Contains(got, "Target") {
		t.Errorf("the table left the screen while loading, Rule 139 keeps it:\n%s", got)
	}
	if got := m.RenderFooter(160); !strings.Contains(got, "Loading scan inventory") {
		t.Errorf("the footer does not say the inventory is loading:\n%s", got)
	}
}

// Et l'inverse : la réponse arrivée, le message redevient vrai et le footer se
// tait. Sans cette moitié, une vue qui ne quitterait jamais l'état de chargement
// passerait le test précédent.
func TestTheEmptyMessageAppearsOnceTheCachesAnswer(t *testing.T) {
	m := inventoryModel(t)

	if got := m.View(); !strings.Contains(got, "Nothing scanned yet") {
		t.Errorf("the loaded, empty inventory says nothing about where scans come from:\n%s", got)
	}
	if got := m.RenderFooter(160); strings.Contains(got, "Loading scan inventory") {
		t.Errorf("the footer still says loading after the caches answered:\n%s", got)
	}
}

// La chaîne du spinner s'arrêtait dès que rien n'était en cours de scan, donc un
// spinner de chargement serait resté sur la frame zéro — ce qui se lit comme un
// blocage (Rule 139).
func TestTheSpinnerKeepsTickingWhileTheInventoryLoads(t *testing.T) {
	m := feed(t, New(testConfig(), nil), tea.WindowSizeMsg{Width: 160, Height: 30})

	if m.inventoryScanning() {
		t.Fatal("the fixture is meant to be loading, not scanning")
	}
	updated, cmd := m.handleSpinnerTick(spinner.TickMsg{})
	if cmd == nil {
		t.Error("the spinner chain stopped while the inventory was still loading")
	}
	got := updated.(Model)
	if frame := strings.TrimSpace(got.spinner.View()); !strings.Contains(got.RenderFooter(160), frame) {
		t.Errorf("the footer does not render the spinner frame %q:\n%s", frame, got.RenderFooter(160))
	}
}

// Le glyphe est une colonne, pas un préfixe — le motif de la vue ws, et celui
// que toute table à icône en première colonne suit (Rule 125). Collé dans
// Target, il dépensait la largeur de la colonne identifiante pour ce qui n'est
// pas le nom, et la mesure de contenu comptait le glyphe avec.
func TestTheKindGlyphIsItsOwnColumn(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)

	cols := m.inventory.Table().Columns()
	if cols[0].Title != "" {
		t.Errorf("the first column is titled %q, want the untitled glyph column", cols[0].Title)
	}

	row := m.inventory.Table().Rows()[0]
	if strings.TrimSpace(row[0]) == "" {
		t.Error("the glyph column renders nothing")
	}
	target := row[columnIndex(t, cols, "Target")]
	if strings.Contains(target, theme.IconDocker) || strings.Contains(target, theme.IconWorkspace) {
		t.Errorf("Target cell = %q still carries the kind glyph", target)
	}
}

// The two things this inventory holds are two colours. The glyphs differ too,
// so the colour is reinforcement — but a column with exactly two values is
// where two hues a notch apart would be read as one.
func TestTheInventoryTellsAnImageFromARepositoryByColour(t *testing.T) {
	image := scanTarget{Kind: kindImage, Name: "nginx:latest"}
	repo := scanTarget{Kind: kindRepo, Name: "/home/ada/api"}

	if got := image.kindIconRole(); got != theme.IconRoleImage {
		t.Errorf("an image has role %q, want the image role", got)
	}
	if got := repo.kindIconRole(); got != theme.IconRoleRepository {
		t.Errorf("a repository has role %q, want the repository role", got)
	}
	if theme.IconColor(image.kindIconRole()) == theme.IconColor(repo.kindIconRole()) {
		t.Error("the two kinds are painted the same colour")
	}
}

// The repository here, in workspaces and in the explorer are one role — the
// same object listed by three views.
func TestTheInventoryRepositoryTakesTheSharedRole(t *testing.T) {
	role := scanTarget{Kind: kindRepo}.kindIconRole()
	if theme.IconColor(role) != theme.ColorIconRepository {
		t.Error("a scanned repository is not painted the repository colour")
	}
}

// Rule 122: the colour is the column's Style, never the cell's text.
func TestTheKindGlyphCarriesNoEscapeSequence(t *testing.T) {
	for _, target := range []scanTarget{{Kind: kindImage}, {Kind: kindRepo}} {
		if strings.Contains(target.kindIcon(), "\x1b") {
			t.Errorf("kindIcon carries an escape sequence: %q", target.kindIcon())
		}
	}
}
