package workspaces

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
	viewerpkg "github.com/anthnel/devdesk/internal/viewer"
)

// ── Construction ─────────────────────────────────────────────────────────────

func TestNewStartsAtTheRootInNormalMode(t *testing.T) {
	m := newTestModel(t)

	if m.mode != ModeNormal {
		t.Errorf("mode = %d on a new model, want ModeNormal", m.mode)
	}
	if m.currentPath != "" {
		t.Errorf("currentPath = %q on a new model, want the root", m.currentPath)
	}
	if m.pendingCursor != -1 {
		t.Errorf("pendingCursor = %d on a new model, want -1 (none)", m.pendingCursor)
	}
	if m.InEditMode() {
		t.Error("InEditMode() is true on a fresh normal-mode model")
	}
}

func TestNewForSelectionStartsInSelectionMode(t *testing.T) {
	m := NewForSelection(testConfig(), "Pick a repository to scan")

	if m.mode != ModeSelecting {
		t.Errorf("mode = %d, want ModeSelecting", m.mode)
	}
	if m.selectionMessage != "Pick a repository to scan" {
		t.Errorf("selectionMessage = %q, want the caller's message", m.selectionMessage)
	}
	// Selection mode is an edit mode: enter and esc mean something local.
	if !m.InEditMode() {
		t.Error("InEditMode() is false in selection mode, so esc would close the view")
	}
}

func TestInitLoadsEntriesAndTheScanCache(t *testing.T) {
	if cmd := New(testConfig(), nil).Init(); cmd == nil {
		t.Fatal("Init() returned no command, so the view never populates")
	}
}

// ── Loading ──────────────────────────────────────────────────────────────────

func TestEntriesLoadedPopulatesTheTable(t *testing.T) {
	m := loadedModel(t)

	if got := rowNames(m.table.Table().Rows()); len(got) != 5 {
		t.Errorf("the table holds %v, want the five fixtures", got)
	}
	if m.error != "" {
		t.Errorf("error = %q after a successful load", m.error)
	}
}

func TestLoadErrorSurfaces(t *testing.T) {
	m := feed(t, newTestModel(t), LoadErrorMsg{Error: errors.New("permission denied")})

	if m.error == "" {
		t.Error("a failed load left the error empty")
	}
	if !strings.Contains(m.View(), "permission denied") {
		t.Error("the load failure is not shown")
	}
}

// A reload after a successful action must clear a stale error.
func TestSuccessfulLoadClearsAPreviousError(t *testing.T) {
	m := feed(t, newTestModel(t), LoadErrorMsg{Error: errors.New("boom")})

	m = feed(t, m, EntriesLoadedMsg{Entries: entryFixtures()})

	if m.error != "" {
		t.Errorf("error = %q after a successful reload", m.error)
	}
}

// ── Navigation ───────────────────────────────────────────────────────────────

func TestDrillDownAndBackRestoresTheCursor(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(2) // clients, a directory

	m, cmd := step(t, m, testutil.Key("right"))
	if m.currentPath != "/tmp/workspaces/clients" {
		t.Fatalf("currentPath = %q after drilling in, want the clients directory", m.currentPath)
	}
	if cmd == nil {
		t.Error("drilling in did not reload the entries")
	}

	m, cmd = step(t, m, testutil.Key("left"))
	if m.currentPath != "" {
		t.Errorf("currentPath = %q after drilling up, want the root", m.currentPath)
	}
	if cmd == nil {
		t.Error("drilling up did not reload the entries")
	}
	// The cursor is restored once the parent's entries arrive, not before.
	if m.pendingCursor != 2 {
		t.Errorf("pendingCursor = %d, want the cursor the user left at (2)", m.pendingCursor)
	}

	m = feed(t, m, EntriesLoadedMsg{Entries: entryFixtures()})
	if got := m.table.Cursor(); got != 2 {
		t.Errorf("cursor = %d after the parent reloaded, want it restored to 2", got)
	}
	if m.pendingCursor != -1 {
		t.Error("pendingCursor was not consumed")
	}
}

func TestDrillDownIsRefusedOnFilesAndEmptyLists(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(4) // notes.md, a file

	m, cmd := step(t, m, testutil.Key("right"))
	if m.currentPath != "" {
		t.Errorf("currentPath = %q after entering a file, want the root", m.currentPath)
	}
	if cmd != nil {
		t.Error("entering a file triggered a reload")
	}

	empty := newTestModel(t)
	_, cmd = step(t, empty, testutil.Key("right"))
	if cmd != nil {
		t.Error("entering with no entries triggered a reload")
	}
}

func TestDrillUpAtTheRootIsInert(t *testing.T) {
	m := loadedModel(t)

	m, cmd := step(t, m, testutil.Key("left"))

	if cmd != nil {
		t.Error("drilling up from the root triggered a reload")
	}
	if m.currentPath != "" {
		t.Errorf("currentPath = %q, want it unchanged at the root", m.currentPath)
	}
}

// Each level pushes onto the stack, so the breadcrumb and the cursor history
// stay aligned however deep the user goes.
func TestNestedNavigationTracksTheStack(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(2)
	m = feed(t, m, testutil.Key("right")) // into clients
	m = feed(t, m, EntriesLoadedMsg{Path: "/tmp/workspaces/clients", Entries: []Entry{
		{Name: "a", Path: "/tmp/workspaces/clients/a", IsDir: true, IsGitRepo: true},
	}})

	m = feed(t, m, testutil.Key("right")) // into a
	if m.currentPath != "/tmp/workspaces/clients/a" {
		t.Fatalf("currentPath = %q, want the nested directory", m.currentPath)
	}
	if len(m.navigationStack) != 1 || m.navigationStack[0] != "/tmp/workspaces/clients" {
		t.Errorf("navigationStack = %v, want the parent pushed", m.navigationStack)
	}
	if got := m.tabCount(); got != 3 {
		t.Errorf("tabCount() = %d at two levels deep, want 3 (home + parent + current)", got)
	}

	m = feed(t, m, testutil.Key("left"))
	if m.currentPath != "/tmp/workspaces/clients" {
		t.Errorf("currentPath = %q after one level up, want the parent", m.currentPath)
	}
	if len(m.navigationStack) != 0 {
		t.Errorf("navigationStack = %v, want it popped", m.navigationStack)
	}
}

// Holding `→` used to drill twice from one listing. The load is a Cmd, so the
// table still held the parent's rows when the second press arrived: it read the
// same row again and pushed the new currentPath onto the stack, so the
// breadcrumb grew `devsecops devsecops`. Reported from a real session.
func TestASecondDrillDownBeforeTheListingLandsDoesNothing(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(2) // clients

	m = feed(t, m, testutil.Key("right"))
	m = feed(t, m, testutil.Key("right")) // the listing has not landed yet

	if m.currentPath != "/tmp/workspaces/clients" {
		t.Errorf("currentPath = %q, want the directory entered once", m.currentPath)
	}
	if len(m.navigationStack) != 0 {
		t.Errorf("navigationStack = %v, want nothing pushed — the root is not a tab", m.navigationStack)
	}
	if got := m.tabCount(); got != 2 {
		t.Errorf("tabCount() = %d, want 2 (home + clients); a duplicate breadcrumb is the reported symptom", got)
	}
}

// Worse than a duplicate: with the cursor moved in between, the second press
// pushed a *sibling* of the directory just entered as if it were nested in it.
func TestMovingTheCursorMidLoadCannotFabricateANestedPath(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(2) // clients

	m = feed(t, m, testutil.Key("right"))
	m = feed(t, m, testutil.Key("down")) // still the parent's rows
	m = feed(t, m, testutil.Key("right"))

	if m.currentPath != "/tmp/workspaces/clients" {
		t.Errorf("currentPath = %q, want the directory actually entered", m.currentPath)
	}
	if len(m.navigationStack) != 0 {
		t.Errorf("navigationStack = %v, want nothing pushed", m.navigationStack)
	}
}

// Once the listing lands, `→` works again — the guard is about the rows in
// hand, not a lock that has to be released by something.
func TestTheDrillDownWorksAgainOnceTheListingLands(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(2)
	m = feed(t, m, testutil.Key("right"))
	m = feed(t, m, EntriesLoadedMsg{Path: "/tmp/workspaces/clients", Entries: []Entry{
		{Name: "a", Path: "/tmp/workspaces/clients/a", IsDir: true},
	}})

	m = feed(t, m, testutil.Key("right"))

	if m.currentPath != "/tmp/workspaces/clients/a" {
		t.Errorf("currentPath = %q, want the nested directory", m.currentPath)
	}
	if len(m.navigationStack) != 1 || m.navigationStack[0] != "/tmp/workspaces/clients" {
		t.Errorf("navigationStack = %v, want the parent pushed once", m.navigationStack)
	}
}

// `←` is not guarded: it reads the stack, not the table. What must not happen
// is the listing it overtook landing on top of the directory it went back to.
func TestAListingThatArrivesAfterNavigatingAwayIsDropped(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(2)
	m = feed(t, m, testutil.Key("right")) // into clients
	m = feed(t, m, testutil.Key("left"))  // straight back out

	// The load issued for clients lands now.
	m = feed(t, m, EntriesLoadedMsg{Path: "/tmp/workspaces/clients", Entries: []Entry{
		{Name: "a", Path: "/tmp/workspaces/clients/a", IsDir: true},
	}})

	if m.currentPath != "" {
		t.Fatalf("currentPath = %q, want the root", m.currentPath)
	}
	if got := len(m.table.Items()); got != len(entryFixtures()) {
		t.Errorf("the table holds %d rows, want the root's %d — a stale listing was displayed", got, len(entryFixtures()))
	}
}

// The same for an error: one about a directory nobody is looking at any more
// must not be reported over the one on screen.
func TestAnErrorAboutAnAbandonedDirectoryIsDropped(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(2)
	m = feed(t, m, testutil.Key("right"))
	m = feed(t, m, testutil.Key("left"))

	m = feed(t, m, LoadErrorMsg{Path: "/tmp/workspaces/clients", Error: errors.New("permission denied")})

	if m.error != "" {
		t.Errorf("error = %q, want none — it is about a directory that was left", m.error)
	}
}

// bubbles does not clamp the cursor when the row count shrinks. Without the
// clamp datatable does on SetItems, drilling into a smaller directory leaves
// the cursor past the end: nothing is highlighted and every action that
// resolves the selection silently does nothing.
func TestCursorIsClampedWhenTheListShrinks(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(4) // last of five

	m = feed(t, m, EntriesLoadedMsg{Entries: entryFixtures()[:2]})

	if got := m.table.Cursor(); got != 1 {
		t.Errorf("cursor = %d after the list shrank to two rows, want the last valid row", got)
	}
	// And the selection still resolves, which is the point.
	if got := m.resolveTargetPath(); got != "/tmp/workspaces/clean-repo" {
		t.Errorf("target = %q, want the row the clamped cursor points at", got)
	}
}

func TestCursorIsClampedWhenAFilterNarrows(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(4)

	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("clean")...)

	if got := m.table.Cursor(); got != 0 {
		t.Errorf("cursor = %d after filtering to one row, want 0", got)
	}
}

// An empty list leaves the cursor at bubbles' own -1, which every selection
// guard already treats as "nothing selected". What must not happen is a cursor
// left pointing at a row that is no longer there.
func TestCursorNeverPointsPastTheEnd(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(4)

	m = feed(t, m, EntriesLoadedMsg{Entries: nil})

	if got := m.table.Cursor(); got >= 0 {
		t.Errorf("cursor = %d on an empty list, want it to report nothing selected", got)
	}
	if got := m.resolveTargetPath(); got != "/tmp/workspaces" {
		t.Errorf("target = %q with nothing selected, want the browsed root", got)
	}
}

func TestEscNavigatesUp(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(2)
	m = feed(t, m, testutil.Key("right"))

	m = feed(t, m, testutil.Key("esc"))

	if m.currentPath != "" {
		t.Errorf("currentPath = %q after esc, want the root", m.currentPath)
	}
}

func TestVerticalNavigation(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, testutil.Key("end"))
	if got := m.table.Cursor(); got != 4 {
		t.Errorf("cursor = %d after end, want the last row", got)
	}
	m = feed(t, m, testutil.Key("home"))
	if got := m.table.Cursor(); got != 0 {
		t.Errorf("cursor = %d after home, want the top", got)
	}
	m = feed(t, m, testutil.Key("down"))
	if got := m.table.Cursor(); got != 1 {
		t.Errorf("cursor = %d after down, want 1", got)
	}
	m = feed(t, m, testutil.Key("up"))
	if got := m.table.Cursor(); got != 0 {
		t.Errorf("cursor = %d after up, want 0", got)
	}
}

// ── Selection mode ───────────────────────────────────────────────────────────

// Enter confirms the directory being *browsed*, not the highlighted row — the
// user drills in with → first. Getting this backwards would scan the wrong tree.
func TestSelectionConfirmsTheBrowsedDirectory(t *testing.T) {
	m := feed(t, NewForSelection(testConfig(), "pick one"), tea.WindowSizeMsg{Width: 160, Height: 30})
	m = feed(t, m, EntriesLoadedMsg{Entries: entryFixtures()})
	m.table.SetCursor(0) // devdesk highlighted, but not entered

	_, cmd := step(t, m, testutil.Key("enter"))

	msg, ok := testutil.MsgOf[DirectorySelectedMsg](cmd)
	if !ok {
		t.Fatalf("enter did not emit DirectorySelectedMsg, got %T", testutil.Msg(cmd))
	}
	if msg.Path != "/tmp/workspaces" {
		t.Errorf("selected %q, want the browsed root rather than the highlighted entry", msg.Path)
	}
}

func TestSelectionConfirmsAfterDrillingIn(t *testing.T) {
	m := feed(t, NewForSelection(testConfig(), "pick one"), tea.WindowSizeMsg{Width: 160, Height: 30})
	m = feed(t, m, EntriesLoadedMsg{Entries: entryFixtures()})
	m.table.SetCursor(0)
	m = feed(t, m, testutil.Key("right"))

	_, cmd := step(t, m, testutil.Key("enter"))

	msg, _ := testutil.MsgOf[DirectorySelectedMsg](cmd)
	if msg.Path != "/tmp/workspaces/devdesk" {
		t.Errorf("selected %q, want the directory drilled into", msg.Path)
	}
}

// Esc backs out one level, and only cancels once there is nowhere left to go.
func TestSelectionEscBacksOutBeforeCancelling(t *testing.T) {
	m := feed(t, NewForSelection(testConfig(), "pick one"), tea.WindowSizeMsg{Width: 160, Height: 30})
	m = feed(t, m, EntriesLoadedMsg{Entries: entryFixtures()})
	m.table.SetCursor(0)
	m = feed(t, m, testutil.Key("right"))

	m, cmd := step(t, m, testutil.Key("esc"))
	if _, cancelled := testutil.MsgOf[SelectionCancelledMsg](cmd); cancelled {
		t.Fatal("esc cancelled the selection instead of backing out one level")
	}
	if m.currentPath != "" {
		t.Errorf("currentPath = %q after esc, want the root", m.currentPath)
	}

	_, cmd = step(t, m, testutil.Key("esc"))
	if _, cancelled := testutil.MsgOf[SelectionCancelledMsg](cmd); !cancelled {
		t.Errorf("esc at the root did not cancel, got %T", testutil.Msg(cmd))
	}
}

// The destructive keys have no business in a picker.
func TestSelectionModeIgnoresTheEditingKeys(t *testing.T) {
	m := feed(t, NewForSelection(testConfig(), "pick one"), tea.WindowSizeMsg{Width: 160, Height: 30})
	m = feed(t, m, EntriesLoadedMsg{Entries: entryFixtures()})

	for _, key := range []string{"ctrl+n", "ctrl+d", "r"} {
		next := feed(t, m, testutil.Key(key))
		if next.mode != ModeSelecting {
			t.Errorf("%q left selection mode", key)
		}
		if next.input != nil || next.confirmModal != nil {
			t.Errorf("%q opened an editing overlay in selection mode", key)
		}
	}
}

// ── Create, rename, delete ───────────────────────────────────────────────────

func TestCreateFlow(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.New))
	if m.mode != ModeAdding || m.input == nil {
		t.Fatal("ctrl+n did not open the create input")
	}

	m, cmd := step(t, m, WorkspaceInputSubmitMsg{Name: "new-project"})
	if m.mode != ModeNormal || m.input != nil {
		t.Error("the input stayed open after submission")
	}
	if cmd == nil {
		t.Error("submission issued no command, so nothing is created")
	}
}

func TestCreateCancel(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.New))

	m = feed(t, m, WorkspaceInputCancelMsg{})

	if m.mode != ModeNormal || m.input != nil {
		t.Error("cancelling left the input open")
	}
}

func TestRenameFlowUsesTheSelectedEntry(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(1) // clean-repo

	m = feed(t, m, testutil.Key(keymap.Rename))
	if m.mode != ModeRenaming || m.input == nil {
		t.Fatal("r did not open the rename input")
	}
	if m.pendingEntry == nil || m.pendingEntry.Name != "clean-repo" {
		t.Errorf("pendingEntry = %v, want the row under the cursor", m.pendingEntry)
	}

	m, cmd := step(t, m, RenameInputSubmitMsg{Name: "renamed"})
	if m.mode != ModeNormal {
		t.Error("the rename input stayed open after submission")
	}
	if cmd == nil {
		t.Error("the rename issued no command")
	}
}

// A rename submitted with nothing pending must do nothing rather than rename
// whatever happens to be under the cursor by then. The form used to carry a row
// index, which named a different entry the moment the list changed under it.
func TestRenameWithNothingPendingIsInert(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.Rename))
	m.pendingEntry = nil

	_, cmd := step(t, m, RenameInputSubmitMsg{Name: "renamed"})

	if cmd != nil {
		t.Error("a rename was issued for an out-of-range index")
	}
}

func TestDeleteFlowNamesTheEntry(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(3) // empty-dir

	m = feed(t, m, testutil.Key(keymap.Delete))
	if m.mode != ModeConfirmingDelete || m.confirmModal == nil {
		t.Fatal("ctrl+d did not open the confirmation")
	}
	if !strings.Contains(m.confirmModal.View(), "empty-dir") {
		t.Error("the confirmation does not name the entry")
	}

	m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})
	if m.mode != ModeNormal || m.confirmModal != nil {
		t.Error("the confirmation stayed open after answering yes")
	}
	if cmd == nil {
		t.Error("confirming issued no delete")
	}
}

// The title distinguishes a file from a directory, since deleting a directory
// takes its contents with it.
func TestDeleteConfirmationDistinguishesFilesFromDirectories(t *testing.T) {
	m := loadedModel(t)

	m.table.SetCursor(4) // notes.md
	file := feed(t, m, testutil.Key(keymap.Delete))
	if !strings.Contains(file.confirmModal.View(), "Delete File") {
		t.Error("deleting a file does not say so")
	}

	m.table.SetCursor(3) // empty-dir
	dir := feed(t, m, testutil.Key(keymap.Delete))
	if !strings.Contains(dir.confirmModal.View(), "Delete Directory") {
		t.Error("deleting a directory does not say so")
	}
}

func TestDeleteCancel(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.Delete))

	m, cmd := step(t, m, sharedcomponents.ConfirmModalNoMsg{})

	if m.mode != ModeNormal || m.confirmModal != nil {
		t.Error("cancelling left the confirmation open")
	}
	if cmd != nil {
		t.Error("cancelling still issued a delete")
	}
}

func TestEditingKeysAreInertWithoutEntries(t *testing.T) {
	m := newTestModel(t)

	for _, key := range []string{"r", "ctrl+d"} {
		next := feed(t, m, testutil.Key(key))
		if next.mode != ModeNormal {
			t.Errorf("%q changed mode with no entries", key)
		}
	}
}

func TestActionResultsReloadOrSurface(t *testing.T) {
	tests := []struct {
		name string
		ok   tea.Msg
		bad  tea.Msg
	}{
		{"create", WorkspaceCreatedMsg{}, WorkspaceCreatedMsg{Error: errors.New("exists")}},
		{"delete", EntryDeletedMsg{}, EntryDeletedMsg{Error: errors.New("busy")}},
		{"rename", EntryRenamedMsg{}, EntryRenamedMsg{Error: errors.New("exists")}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, cmd := step(t, loadedModel(t), tc.ok)
			if cmd == nil {
				t.Error("a successful action did not reload the entries")
			}
			if m.error != "" {
				t.Errorf("error = %q after a successful action", m.error)
			}

			m, cmd = step(t, loadedModel(t), tc.bad)
			if cmd != nil {
				t.Error("a failed action still reloaded the entries")
			}
			if m.error == "" {
				t.Error("a failed action left the error empty")
			}
		})
	}
}

// ── Scanning (Rule 126) ──────────────────────────────────────────────────────

// ctrl+s on a git repo purges that repo's cache entry before rescanning, so the
// result on screen cannot be a stale one.
func TestScanOneRepoPurgesItsCacheEntry(t *testing.T) {
	m := scannedModel(t)
	m.table.SetCursor(0) // devdesk, which has a cached result

	m, cmd := step(t, m, testutil.Key(keymap.Scan))

	if _, still := m.scanCache["/tmp/workspaces/devdesk"]; still {
		t.Error("the cached result survived a rescan request")
	}
	if cmd == nil {
		t.Error("no scan was issued")
	}
}

// A scan already running is reported, not queued twice.
func TestScanRefusesToStartTwice(t *testing.T) {
	m := scannedModel(t)
	m.table.SetCursor(0)
	m.scanningPaths["/tmp/workspaces/devdesk"] = true

	m, cmd := step(t, m, testutil.Key(keymap.Scan))

	if m.footer.Text() != busyMessage {
		t.Errorf("footerInfo = %q, want the already-running notice", m.footer.Text())
	}
	// Rule 128: the message must come with the timer that clears it.
	if cmd == nil {
		t.Error("no command returned, so the footer message would never clear")
	}
}

// A plain directory scans every repo nested under it.
func TestScanOnADirectoryScansItsSubRepos(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(2) // clients, with two sub-repos

	_, cmd := step(t, m, testutil.Key(keymap.Scan))

	if cmd == nil {
		t.Error("scanning a directory with nested repos issued no command")
	}
}

func TestScanIsInertOnEntriesWithNothingToScan(t *testing.T) {
	m := loadedModel(t)

	for _, tc := range []struct {
		name   string
		cursor int
		reason string
	}{
		{"a file", 4, reasonNotARepo},
		{"a directory with no repos", 3, reasonNoScanTarget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.table.SetCursor(tc.cursor)

			next := refused(t, m, keymap.Scan, tc.reason)

			if len(next.scanningPaths) != 0 {
				t.Errorf("a scan was started for %s: %v", tc.name, next.scanningPaths)
			}
		})
	}
}

// Rule 126: A with the purge unchecked scans only what has never been scanned;
// checked, it purges and rescans everything. They were A and ctrl+a, two keys
// separated by a modifier with nothing saying which one destroyed data (§3.26).
func TestScanAllUnscannedSkipsCachedRepos(t *testing.T) {
	m := scannedModel(t) // devdesk is cached, clean-repo and the two sub-repos are not

	m, cmd := scanAll(t, m, false)

	if cmd == nil {
		t.Fatal("A issued no scan even though repos were unscanned")
	}
	if _, gone := m.scanCache["/tmp/workspaces/devdesk"]; !gone {
		t.Error("A purged the cached result with the checkbox unticked")
	}
}

func TestScanAllUnscannedIsInertWhenEverythingIsCached(t *testing.T) {
	m := loadedModel(t)
	m = feed(t, m, ScanCacheLoadedMsg{Cache: map[string]cache.WorkspaceScanEntry{
		"/tmp/workspaces/devdesk":    {RepoPath: "/tmp/workspaces/devdesk"},
		"/tmp/workspaces/clean-repo": {RepoPath: "/tmp/workspaces/clean-repo"},
		"/tmp/workspaces/clients/a":  {RepoPath: "/tmp/workspaces/clients/a"},
		"/tmp/workspaces/clients/b":  {RepoPath: "/tmp/workspaces/clients/b"},
	}})

	_, cmd := scanAll(t, m, false)

	if cmd != nil {
		t.Error("A issued a scan with nothing left unscanned")
	}
}

func TestScanAllPurgesTheWholeCache(t *testing.T) {
	m := scannedModel(t)

	m, cmd := scanAll(t, m, true)

	if len(m.scanCache) != 0 {
		t.Errorf("scanCache still holds %d entries after the purging scan", len(m.scanCache))
	}
	if cmd == nil {
		t.Error("the purging scan issued no scan")
	}
}

func TestScanAllIsInertWithNoRepos(t *testing.T) {
	m := feed(t, newTestModel(t), EntriesLoadedMsg{Entries: []Entry{
		{Name: "notes.md", Path: "/tmp/workspaces/notes.md"},
	}})

	_, cmd := step(t, m, testutil.Key(keymap.ScanAll))

	if cmd != nil {
		t.Error("ctrl+a issued a scan with no repos in view")
	}
}

func TestScanLifecycle(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, WorkspaceScanStartingMsg{RepoPath: "/tmp/workspaces/devdesk"})
	if !m.scanningPaths["/tmp/workspaces/devdesk"] {
		t.Error("the repo was not marked as scanning")
	}

	m = feed(t, m, WorkspaceScanCompleteMsg{
		RepoPath: "/tmp/workspaces/devdesk",
		Critical: 1, High: 2, Medium: 3, Low: 4, Sensitive: secretsFound(),
	})
	if m.scanningPaths["/tmp/workspaces/devdesk"] {
		t.Error("the repo is still marked as scanning after the result arrived")
	}
	entry, ok := m.scanCache["/tmp/workspaces/devdesk"]
	if !ok {
		t.Fatal("the result was not cached")
	}
	if entry.Critical != 1 || entry.High != 2 || entry.Medium != 3 || entry.Low != 4 || entry.Sensitive == nil || !*entry.Sensitive {
		t.Errorf("cached entry = %+v, want the reported counts", entry)
	}
}

func TestFailedScanSurfacesAShortMessage(t *testing.T) {
	m := feed(t, loadedModel(t), WorkspaceScanStartingMsg{RepoPath: "/tmp/workspaces/devdesk"})

	m, cmd := step(t, m, WorkspaceScanCompleteMsg{
		RepoPath: "/tmp/workspaces/devdesk",
		Error:    errors.New("trivy: exit status 1"),
	})

	if !m.footer.IsSet() {
		t.Error("a failed scan left the footer empty")
	}
	if strings.Contains(m.footer.Text(), "exit status") {
		t.Errorf("footer = %q leaks the raw error; Rule 128 wants a short message plus a log", m.footer.Text())
	}
	if m.scanningPaths["/tmp/workspaces/devdesk"] {
		t.Error("a failed scan left the repo marked as scanning forever")
	}
	if _, cached := m.scanCache["/tmp/workspaces/devdesk"]; cached {
		t.Error("a failed scan cached a result")
	}
	if cmd == nil {
		t.Error("no command returned, so the footer message would never clear")
	}
}

func TestTheFooterClearsOnItsOwnExpiry(t *testing.T) {
	m := loadedModel(t)
	m.footer.Error("something")

	m = feed(t, m, sharedcomponents.ClearFooterMsg{ID: m.footer.ID()})

	if m.footer.IsSet() {
		t.Errorf("footer = %q after its expiry", m.footer.Text())
	}
}

// The spinner animates the Scanned column while scans are in flight, and stops
// once they are done.
func TestSpinnerTicksOnlyWhileScanning(t *testing.T) {
	m := loadedModel(t)

	_, cmd := step(t, m, spinner.TickMsg{})
	if cmd != nil {
		t.Error("the spinner ran with no scan in flight")
	}

	m = feed(t, m, WorkspaceScanStartingMsg{RepoPath: "/tmp/workspaces/devdesk"})
	_, cmd = step(t, m, spinner.TickMsg{})
	if cmd == nil {
		t.Error("the spinner stopped while a scan was running")
	}
}

// ── External tools ───────────────────────────────────────────────────────────

// The path these actions open follows the cursor when it is on a directory, and
// falls back to what is being browsed otherwise.
func TestTargetPathFollowsTheSelection(t *testing.T) {
	m := loadedModel(t)

	m.table.SetCursor(0)
	if got := m.resolveTargetPath(); got != "/tmp/workspaces/devdesk" {
		t.Errorf("target = %q with a directory selected, want that directory", got)
	}

	m.table.SetCursor(4) // a file: fall back to the browsed directory
	if got := m.resolveTargetPath(); got != "/tmp/workspaces" {
		t.Errorf("target = %q with a file selected, want the browsed root", got)
	}

	empty := newTestModel(t)
	if got := empty.resolveTargetPath(); got != "/tmp/workspaces" {
		t.Errorf("target = %q with no entries, want the workspaces root", got)
	}
}

func TestExternalToolFailuresSurface(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{"ide", IDEOpenedMsg{Error: errors.New("code not found")}},
		{"browser", BrowserOpenedMsg{Error: errors.New("no handler")}},
		{"terminal", TerminalOpenedMsg{Error: errors.New("no terminal")}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := feed(t, loadedModel(t), tc.msg)

			if m.error == "" && !m.footer.IsSet() {
				t.Error("a failed launch said nothing")
			}
		})
	}
}

// Returning from a suspended terminal reloads, since the user may have changed
// the tree from the shell.
func TestReturningFromATerminalReloads(t *testing.T) {
	_, cmd := step(t, loadedModel(t), TerminalExitMsg{})

	if cmd == nil {
		t.Error("returning from the terminal did not reload the entries")
	}
}

// The browser action needs a remote, so a repository without one gets a greyed
// W and, if the user presses it anyway, the reason. IsGitRepo alone used to
// advertise it, and openInBrowser then returned in silence.
func TestBrowserRequiresARemote(t *testing.T) {
	m := feed(t, newTestModel(t), EntriesLoadedMsg{Entries: []Entry{
		{Name: "local-only", Path: "/tmp/workspaces/local-only", IsDir: true, IsGitRepo: true},
	}})

	if !shortcutDisabled(m.GetShortcuts(), keymap.Web) {
		t.Error("W is offered on a repo with no remote")
	}
	refused(t, m, keymap.Web, reasonNoRemote)
}

// ── Details ──────────────────────────────────────────────────────────────────

// Enter opens the scan details, but only for a repo that actually has results.
func TestEnterOpensDetailsOnlyForScannedRepos(t *testing.T) {
	m := scannedModel(t)

	m.table.SetCursor(1) // clean-repo, never scanned
	refused(t, m, "enter", reasonNothingToSee)

	m.table.SetCursor(2) // a plain directory
	refused(t, m, "enter", reasonNothingToSee)

	m.table.SetCursor(0) // devdesk, scanned
	_, cmd := step(t, m, testutil.Key("enter"))
	if cmd == nil {
		t.Error("enter did not open details for a scanned repo")
	}
}

// ── Opening a file in the viewer ─────────────────────────────────────────────

// enter carries two actions, and they cannot collide: a file is never a git
// repository.
func TestEnterOnAFileAsksForTheViewer(t *testing.T) {
	m := scannedModel(t)
	m.table.SetCursor(fileRow(t, m, "notes.md"))

	_, cmd := step(t, m, testutil.Key("enter"))
	if cmd == nil {
		t.Fatal("enter on a file issued no command")
	}
	msg, ok := cmd().(uiviewer.OpenRequestMsg)
	if !ok {
		t.Fatalf("enter on a file produced %T, want a viewer.OpenRequestMsg", cmd())
	}
	source, ok := msg.Source.(viewerpkg.FileSource)
	if !ok {
		t.Fatalf("the request carried a %T, want a FileSource", msg.Source)
	}
	if source.Path != "/tmp/workspaces/notes.md" {
		t.Errorf("Path = %q, want the selected file", source.Path)
	}
	// KindAuto: a file browser does not know what it is opening, so the
	// extension and then the content decide.
	if source.Kind() != viewerpkg.KindAuto {
		t.Errorf("Kind = %q, want auto", source.Kind())
	}
}

// Rule 130: the description changes with what is selected, because the action
// does.
func TestTheEnterShortcutNamesWhicheverActionApplies(t *testing.T) {
	m := scannedModel(t)

	m.table.SetCursor(fileRow(t, m, "notes.md"))
	if got := shortcutFor(m, "enter"); got != "View file" {
		t.Errorf("enter on a file reads %q, want \"View file\"", got)
	}

	m.table.SetCursor(0) // devdesk, scanned
	if got := shortcutFor(m, "enter"); got != "Scan details" {
		t.Errorf("enter on a scanned repo reads %q, want \"Scan details\"", got)
	}

	m.table.SetCursor(1) // clean-repo, never scanned
	if !shortcutDisabled(m.GetShortcuts(), "enter") {
		t.Error("enter is offered on a repo with nothing to open")
	}
	// It keeps the wording of the action it would perform once there is a
	// result: a greyed entry naming nothing would be a blank line in a column
	// whose other rows all read.
	if got := shortcutFor(m, "enter"); got != "Scan details" {
		t.Errorf("the greyed enter reads %q, want \"Scan details\"", got)
	}
}

func fileRow(t *testing.T, m Model, name string) int {
	t.Helper()
	for i, row := range m.table.Visible() {
		if row.Entry.Name == name {
			return i
		}
	}
	t.Fatalf("no row named %q", name)
	return -1
}

func shortcutFor(m Model, key string) string {
	for _, s := range m.GetShortcuts() {
		if s.Key == key {
			return s.Description
		}
	}
	return ""
}

// ── Filtering (Rule 136) ─────────────────────────────────────────────────────

func TestSearchFiltersByNameAndRemote(t *testing.T) {
	tests := []struct {
		query string
		want  []string
	}{
		{"clean", []string{"clean-repo"}},
		{"anthnel", []string{"devdesk", "clean-repo"}}, // remote match
		{"notes", []string{"notes.md"}},
		{"nomatch", nil},
	}

	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			m := feed(t, loadedModel(t), testutil.Key("/"))
			m = feed(t, m, testutil.Type(tc.query)...)

			got := rowNames(m.table.Table().Rows())
			if len(got) != len(tc.want) {
				t.Fatalf("rows = %v, want %v", got, tc.want)
			}
			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("row %d = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

func TestSearchModeCapturesKeys(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("/"))

	if !m.InEditMode() {
		t.Fatal("InEditMode() is false while the search input has focus")
	}

	m = feed(t, m, testutil.Key(keymap.New))
	if m.mode != ModeNormal {
		t.Error("ctrl+n opened the create input while the search had focus")
	}
}

// ── Overlays capture the keyboard ────────────────────────────────────────────

func TestOverlaysCaptureKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(t *testing.T) Model
	}{
		{"create input", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key(keymap.New)) }},
		{"rename input", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key(keymap.Rename)) }},
		{"confirmation", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key(keymap.Delete)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.open(t)
			cursor := m.table.Cursor()

			m = feed(t, m, testutil.Key("down"))

			if m.table.Cursor() != cursor {
				t.Error("a keystroke reached the table under the overlay")
			}
			if !m.InEditMode() {
				t.Error("InEditMode() is false with an overlay open")
			}
		})
	}
}

// ── Pure helpers ─────────────────────────────────────────────────────────────

func TestExtractRemotePath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"https", "https://gitlab.com/group/project.git", "group/project"},
		{"https without suffix", "https://gitlab.com/group/project", "group/project"},
		{"nested groups", "https://gitlab.com/group/sub/project.git", "group/sub/project"},
		{"scp syntax", "git@gitlab.com:group/project.git", "group/project"},
		{"scp with nested groups", "git@gitlab.com:group/sub/project.git", "group/sub/project"},
		{"ssh url", "ssh://git@gitlab.com/group/project.git", "group/project"},
		{"host only", "https://gitlab.com", "https://gitlab.com"},
		{"garbage passes through", "not a url", "not a url"},
		{"empty", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractRemotePath(tc.in); got != tc.want {
				t.Errorf("extractRemotePath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The result is handed to a browser, so embedded credentials must not survive.
func TestNormalizeRemoteURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"scp becomes https", "git@gitlab.com:group/project.git", "https://gitlab.com/group/project"},
		{"https keeps its scheme", "https://gitlab.com/group/project.git", "https://gitlab.com/group/project"},
		{"token is stripped", "https://token@gitlab.com/group/project.git", "https://gitlab.com/group/project"},
		{"user and password are stripped", "https://user:pass@gitlab.com/group/project.git", "https://gitlab.com/group/project"},
		{"ssh url", "ssh://git@gitlab.com/group/project.git", "ssh://gitlab.com/group/project"},
		{"garbage passes through", "not a url", "not a url"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeRemoteURL(tc.in)
			if got != tc.want {
				t.Errorf("normalizeRemoteURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "@") {
				t.Errorf("normalizeRemoteURL(%q) = %q still carries credentials", tc.in, got)
			}
		})
	}
}

func TestDetectSubRepoPaths(t *testing.T) {
	root := t.TempDir()
	mkRepo := func(parts ...string) string {
		t.Helper()
		dir := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatalf("creating the repo: %v", err)
		}
		return dir
	}

	shallow := mkRepo("a")
	nested := mkRepo("b", "c")
	// A repo inside a repo must not be reported: the walk stops at the outer one.
	if err := os.MkdirAll(filepath.Join(shallow, "vendor", "inner", ".git"), 0o755); err != nil {
		t.Fatalf("creating the nested repo: %v", err)
	}
	// A hidden directory is walked, or not, according to the setting: the walk
	// feeds S, F and A, so it answers the same question the listing does.
	hidden := mkRepo(".hidden")

	for _, tc := range []struct {
		name       string
		showHidden bool
		wantHidden bool
	}{
		{"hidden files off", false, false},
		{"hidden files on", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := detectSubRepoPaths(root, tc.showHidden)

			found := map[string]bool{}
			for _, p := range got {
				found[p] = true
			}
			if !found[shallow] || !found[nested] {
				t.Errorf("detectSubRepoPaths = %v, want both %q and %q", got, shallow, nested)
			}
			if found[filepath.Join(shallow, "vendor", "inner")] {
				t.Error("a repo nested inside another repo was reported")
			}
			if found[hidden] != tc.wantHidden {
				t.Errorf("the hidden repo was reported = %v, want %v", found[hidden], tc.wantHidden)
			}
		})
	}
}

// D59: the walk stopped at three levels, so a repository at
// `monorepos/client/2026/api` was invisible to S, F and A while the directory
// holding it browsed normally — and nothing said a limit had been applied.
func TestARepositoryIsFoundHoweverDeepItSits(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c", "d", "e", "f", "g", "deep-repo")
	if err := os.MkdirAll(filepath.Join(deep, ".git"), 0o755); err != nil {
		t.Fatalf("creating the deep repo: %v", err)
	}

	got := detectSubRepoPaths(root, false)
	if len(got) != 1 || got[0] != deep {
		t.Errorf("detectSubRepoPaths = %v, want the repository eight levels down (%q)", got, deep)
	}
}

// The prune that does the work, and the one the depth limit was covering for: a
// repository's own vendored tree is never entered, so removing the limit does
// not turn a scan of a monorepo into a walk of every node_modules under it.
func TestTheWalkStopsAtEveryRepositoryItFinds(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "outer")
	buried := filepath.Join(repo, "node_modules", "pkg", "vendored")
	if err := os.MkdirAll(filepath.Join(buried, ".git"), 0o755); err != nil {
		t.Fatalf("creating the buried repo: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("creating the outer repo: %v", err)
	}

	got := detectSubRepoPaths(root, false)
	if len(got) != 1 || got[0] != repo {
		t.Errorf("detectSubRepoPaths = %v, want only the outer repository %q", got, repo)
	}
}

func TestDetectSubRepoPathsOnAMissingDirectory(t *testing.T) {
	if got := detectSubRepoPaths(filepath.Join(t.TempDir(), "nope"), false); len(got) != 0 {
		t.Errorf("detectSubRepoPaths = %v on a missing directory, want nothing", got)
	}
}

// ── Layout ───────────────────────────────────────────────────────────────────

// Rule 116, and the narrow widths are the point. Eleven columns ask for 130
// characters; calculateColumns handed Modified the remainder and then clamped
// it back up to 15, so the columns overflowed the viewport by up to 34 and the
// selected row wrapped. The solver shares the shortfall out instead.
func TestColumnsFitEveryWidth(t *testing.T) {
	for _, width := range []int{80, 120, 160, 220} {
		m := feed(t, loadedModel(t), tea.WindowSizeMsg{Width: width, Height: 30})

		for _, col := range m.table.Table().Columns() {
			if col.Width < 0 {
				t.Errorf("at width %d, column %q is %d wide", width, col.Title, col.Width)
			}
		}
		// RenderedWidth rather than the declared columns plus two cells each: a
		// column dropped for want of room renders nothing and hands its padding
		// back, so that arithmetic asks for less than the line spans (D61).
		if got, want := m.table.RenderedWidth(), width-2; got != want {
			t.Errorf("at width %d the line spans %d, want %d", width, got, want)
		}
	}
}

func TestResizeKeepsTheTableUsable(t *testing.T) {
	m := feed(t, loadedModel(t), tea.WindowSizeMsg{Width: 200, Height: 50})
	if m.table.Table().Height() < 1 {
		t.Errorf("table height = %d, want it usable", m.table.Table().Height())
	}

	m = feed(t, m, tea.WindowSizeMsg{Width: 20, Height: 1})
	if m.table.Table().Height() < 0 {
		t.Errorf("table height = %d on a tiny terminal, want it non-negative", m.table.Table().Height())
	}
	if m.View() == "" {
		t.Error("View() returned nothing on a tiny terminal")
	}
}

// ── D24: the cursor and the rows disagree under a filter ─────────────────────

// D24. The rows were filtered and m.entries was not, and every action resolved
// the cursor against m.entries — so under a filter they acted on whatever sat
// at that index in the *unfiltered* list. ctrl+d deletes a directory, which
// makes this the wrong-object defect at its worst.
func TestAFilteredSelectionActsOnTheRowTheUserSees(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("/"))
	m = feed(t, m, testutil.Type("empty")...)
	m = feed(t, m, testutil.Key("enter")) // confirm the filter, hand the keys back

	if got := rowNames(m.table.Table().Rows()); !equalNames(got, []string{"empty-dir"}) {
		t.Fatalf("rows under the filter = %v, want just empty-dir", got)
	}

	m, _ = step(t, m, testutil.Key(keymap.Delete))
	if m.mode != ModeConfirmingDelete {
		t.Fatalf("mode = %d after ctrl+d, want the delete confirmation", m.mode)
	}
	if !strings.Contains(m.View(), "empty-dir") {
		t.Errorf("the confirmation does not name empty-dir — it resolved the cursor against the unfiltered list")
	}
}

func equalNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── A scan asked for from outside ────────────────────────────────────────────

// The router sends this when a cached scan's stored result has gone missing:
// the row is still in the list, and the scan that replaces it belongs here, next
// to the cache it writes. The security form used to be opened instead, and it no
// longer exists.
func TestAScanRequestRescansTheNamedRepository(t *testing.T) {
	m := scannedModel(t)

	next, cmd := step(t, m, ScanRequestMsg{TargetPath: "/tmp/workspaces/devdesk"})

	if _, still := next.scanCache["/tmp/workspaces/devdesk"]; still {
		t.Error("the stale cached result survived the rescan request")
	}
	if cmd == nil {
		t.Fatal("the request started no scan")
	}
}

// A request naming nothing is a mistake upstream, not a reason to scan the
// current directory.
func TestAnEmptyScanRequestDoesNothing(t *testing.T) {
	if _, cmd := step(t, scannedModel(t), ScanRequestMsg{}); cmd != nil {
		t.Error("an empty request started a scan")
	}
}

// Rule 128: asking while one is already running says so rather than queueing.
func TestAScanRequestForARunningScanIsRefused(t *testing.T) {
	m := scannedModel(t)
	m.scanningPaths["/tmp/workspaces/devdesk"] = true

	next, cmd := step(t, m, ScanRequestMsg{TargetPath: "/tmp/workspaces/devdesk"})

	if next.footer.Text() != busyMessage {
		t.Errorf("footerInfo = %q, want the already-running notice", next.footer.Text())
	}
	if cmd == nil {
		t.Error("the message was set without a timer to clear it")
	}
}

// ── The delete is guarded like every other action ────────────────────────────
//
// §3.23: the delete was the one action this view's own busy machinery did not
// know about. os.RemoveAll on a multi-gigabyte tree takes seconds with nothing
// on screen saying so, and a second D fired a second RemoveAll whose failure
// was reported to the user for a deletion that had in fact succeeded.

// confirmDeleteOf runs D on the row at idx and answers its confirmation.
func confirmDeleteOf(t *testing.T, m Model, idx int) (Model, tea.Cmd) {
	t.Helper()
	m.table.SetCursor(idx)
	m, _ = step(t, m, testutil.Key(keymap.Delete))
	if m.mode != ModeConfirmingDelete {
		t.Fatal("D did not open the delete confirmation")
	}
	return step(t, m, sharedcomponents.ConfirmModalYesMsg{})
}

func TestAConfirmedDeleteMarksThePathBusy(t *testing.T) {
	m, cmd := confirmDeleteOf(t, loadedModel(t), 3) // empty-dir

	if !m.deletingPaths["/tmp/workspaces/empty-dir"] {
		t.Error("the confirmed delete left the path unmarked")
	}
	if cmd == nil {
		t.Error("confirming issued no delete")
	}
}

func TestADeletingRowSpinsInTheGitStatusColumn(t *testing.T) {
	m, _ := confirmDeleteOf(t, loadedModel(t), 0) // devdesk, a git repo

	row := m.table.Items()[0]
	if !strings.Contains(row.GitStatus, "deleting") {
		t.Errorf("Git Status = %q, want the delete marker", row.GitStatus)
	}
	// Rule 122: a table cell carries no escape sequences.
	if strings.Contains(row.GitStatus, "\x1b") {
		t.Errorf("Git Status carries ANSI: %q", row.GitStatus)
	}
}

// The second D is the defect: it used to fire a second os.RemoveAll, which
// fails on a path the first one has already taken away.
func TestASecondDeleteIsRefusedWhileTheFirstRuns(t *testing.T) {
	m, _ := confirmDeleteOf(t, loadedModel(t), 3)

	next, cmd := step(t, m, testutil.Key(keymap.Delete))

	if next.mode != ModeNormal || next.confirmModal != nil {
		t.Error("a second D opened the confirmation on a row already being deleted")
	}
	if next.footer.Text() != busyMessage || cmd == nil {
		t.Errorf("footer = %q, want the busy warning", next.footer.Text())
	}
}

// The guard runs again at the confirmation, because a batch sync marks its
// repositories from a Cmd — one can take the path while the modal is open.
func TestADeleteConfirmedOnANowSyncingPathIsRefused(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(0)
	m, _ = step(t, m, testutil.Key(keymap.Delete))

	m = feed(t, m, WorkspaceSyncStartingMsg{RepoPath: devdeskPath})
	m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})

	if m.deletingPaths[devdeskPath] {
		t.Error("a repository being synced was marked for deletion")
	}
	if m.footer.Text() != busyMessage || cmd == nil {
		t.Errorf("footer = %q, want the busy warning", m.footer.Text())
	}
}

// A marker left behind holds the path against every other action for the life
// of the view, so it is cleared on the failure too — which is the outcome that
// used to leave nothing to clear it.
func TestADeleteClearsItsMarkerOnEitherOutcome(t *testing.T) {
	tests := []struct {
		name string
		msg  EntryDeletedMsg
	}{
		{"success", EntryDeletedMsg{Path: devdeskPath}},
		{"failure", EntryDeletedMsg{Path: devdeskPath, Error: errors.New("permission denied")}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := confirmDeleteOf(t, loadedModel(t), 0)

			m = feed(t, m, tc.msg)

			if m.deletingPaths[devdeskPath] {
				t.Error("the marker survived the completion")
			}
		})
	}
}

// busy() is one predicate, so the other two actions inherit the guard.
func TestADeletingPathIsRefusedByScanAndSync(t *testing.T) {
	for _, key := range []string{keymap.Scan, keymap.Fetch} {
		t.Run(key, func(t *testing.T) {
			m := loadedModel(t)
			m.deletingPaths[devdeskPath] = true
			m.table.SetCursor(0)

			next, cmd := step(t, m, testutil.Key(key))

			if next.footer.Text() != busyMessage || cmd == nil {
				t.Errorf("footer = %q, want the busy warning", next.footer.Text())
			}
		})
	}
}

// The spinner has to keep being scheduled, or the frame freezes and reads as a
// hang — which is what a delete looks like anyway.
func TestADeleteKeepsTheSpinnerTurning(t *testing.T) {
	m, _ := confirmDeleteOf(t, loadedModel(t), 3)

	before := m.spinnerFrameIdx
	next, cmd := step(t, m, spinner.TickMsg{})

	if next.spinnerFrameIdx == before {
		t.Error("the spinner frame did not advance during a delete")
	}
	if cmd == nil {
		t.Error("the tick chain stopped during a delete")
	}
}
