package filebrowser

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
)

var testHome string

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	sharedcomponents.FooterMsgDuration = time.Millisecond
	home, err := os.MkdirTemp("", "devdesk-filebrowser-test")
	if err != nil {
		panic(err)
	}
	testHome = home
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("USERPROFILE", home)
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// tree builds dir/{a/, b/, notes.md, z.txt} and returns dir.
func tree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"a", "b"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"notes.md", "z.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// step runs one message through Update, then every Cmd it returns that is a
// listing — the one kind of I/O these tests want performed for real.
func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

// settle feeds back whatever listing or creation the Cmd produced.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range testutil.Msgs(cmd) {
		switch msg.(type) {
		case EntriesLoadedMsg, LoadErrorMsg, EntryCreatedMsg, DeletePreparedMsg:
			var next tea.Cmd
			m, next = step(t, m, msg)
			m = settle(t, m, next)
		}
	}
	return m
}

func openAt(t *testing.T, dir string) Model {
	t.Helper()
	m := New(&config.Config{})
	m, _ = step(t, m, testutil.Resize(100, 30))
	next, cmd := m.navigate(dir, "")
	return settle(t, next.(Model), cmd)
}

func names(m Model) []string {
	var out []string
	for _, r := range m.table.Visible() {
		out = append(out, r.Name)
	}
	return out
}

func press(t *testing.T, m Model, key string) (Model, tea.Cmd) {
	t.Helper()
	return step(t, m, testutil.Key(key))
}

func TestItListsDirectoriesFirstAndHidesDotFiles(t *testing.T) {
	dir := tree(t)
	_ = os.WriteFile(filepath.Join(dir, ".secret"), nil, 0o644)
	m := openAt(t, dir)
	if got, want := names(m), []string{"a", "b", "notes.md", "z.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRightEntersAndLeftComesBackOnTheDirectoryLeft(t *testing.T) {
	dir := tree(t)
	m := openAt(t, dir)
	m, _ = press(t, m, "down") // b
	m, cmd := press(t, m, "right")
	m = settle(t, m, cmd)
	if m.CurrentPath() != filepath.Join(dir, "b") {
		t.Fatalf("→ must enter b, at %s", m.CurrentPath())
	}
	m, cmd = press(t, m, "left")
	m = settle(t, m, cmd)
	if m.CurrentPath() != dir {
		t.Fatalf("← must come back, at %s", m.CurrentPath())
	}
	if r, _ := m.selected(); r.Name != "b" {
		t.Errorf("the cursor must land on the directory just left, got %q", r.Name)
	}
}

func TestAStaleListingIsIgnored(t *testing.T) {
	dir := tree(t)
	m := openAt(t, dir)
	m, _ = step(t, m, EntriesLoadedMsg{Dir: "/somewhere/else"})
	if m.CurrentPath() != dir || len(names(m)) != 4 {
		t.Fatalf("a listing nobody asked for replaced the screen: %s %v", m.CurrentPath(), names(m))
	}
}

func TestAnUnreadableDirectoryKeepsTheListingOnScreen(t *testing.T) {
	dir := tree(t)
	m := openAt(t, dir)
	next, cmd := m.navigate(filepath.Join(dir, "missing"), "")
	m = settle(t, next.(Model), cmd)
	if m.CurrentPath() != dir {
		t.Fatalf("the path moved to a directory that could not be read: %s", m.CurrentPath())
	}
	if m.footer.Level() != sharedcomponents.LevelError {
		t.Error("the failure must be said in the footer")
	}
}

func TestEnterOpensAFileInTheViewerAndIsGreyedOnADirectory(t *testing.T) {
	m := openAt(t, tree(t))
	if !testutil.ShortcutDisabled(m.GetShortcuts(), "enter") {
		t.Error("enter must be greyed on a directory")
	}
	m, _ = press(t, m, "down")
	m, _ = press(t, m, "down") // notes.md
	if !testutil.ShortcutEnabled(m.GetShortcuts(), "enter") {
		t.Error("enter must be offered on a file")
	}
	_, cmd := press(t, m, "enter")
	if _, ok := testutil.MsgOf[uiviewer.OpenRequestMsg](cmd); !ok {
		t.Error("enter on a file must ask the router for the viewer")
	}
}

func TestTheSetOfKeysDoesNotChangeFromRowToRow(t *testing.T) {
	m := openAt(t, tree(t))
	want := testutil.ShortcutKeys(m.GetShortcuts())
	for range 3 {
		m, _ = press(t, m, "down")
		if got := testutil.ShortcutKeys(m.GetShortcuts()); !reflect.DeepEqual(got, want) {
			t.Fatalf("keys changed with the row: %v vs %v", got, want)
		}
	}
}

func TestNCreatesADirectoryAndSelectsIt(t *testing.T) {
	dir := tree(t)
	m := openAt(t, dir)
	m, _ = press(t, m, keymap.New)
	if m.mode != modeCreating {
		t.Fatal("N must open the form")
	}
	for _, k := range testutil.Type("new-dir") {
		m, _ = step(t, m, k)
	}
	m, cmd := press(t, m, "enter")
	for _, msg := range testutil.Msgs(cmd) {
		var next tea.Cmd
		m, next = step(t, m, msg)
		m = settle(t, m, next)
	}
	if info, err := os.Stat(filepath.Join(dir, "new-dir")); err != nil || !info.IsDir() {
		t.Fatalf("the directory was not created: %v", err)
	}
	if r, _ := m.selected(); r.Name != "new-dir" {
		t.Errorf("the cursor must land on what was created, got %q", r.Name)
	}
}

func TestNCreatesAFileWhenTheTypeIsCycled(t *testing.T) {
	dir := tree(t)
	m := openAt(t, dir)
	m, _ = press(t, m, keymap.New)
	m, _ = press(t, m, "up")    // Type
	m, _ = press(t, m, "right") // file
	m, _ = press(t, m, "down")  // Name
	for _, k := range testutil.Type("todo.txt") {
		m, _ = step(t, m, k)
	}
	m, cmd := press(t, m, "enter")
	for _, msg := range testutil.Msgs(cmd) {
		var next tea.Cmd
		m, next = step(t, m, msg)
		m = settle(t, m, next)
	}
	if info, err := os.Stat(filepath.Join(dir, "todo.txt")); err != nil || info.IsDir() {
		t.Fatalf("want a regular file: %v", err)
	}
}

func TestAnInvalidNameKeepsTheFormOpen(t *testing.T) {
	m := openAt(t, tree(t))
	m, _ = press(t, m, keymap.New)
	for _, k := range testutil.Type("../escape") {
		m, _ = step(t, m, k)
	}
	m, cmd := press(t, m, "enter")
	for _, msg := range testutil.Msgs(cmd) {
		m, _ = step(t, m, msg)
	}
	if m.mode != modeCreating {
		t.Error("a refused name must keep the form, and what was typed")
	}
	if m.footer.Level() != sharedcomponents.LevelWarning {
		t.Error("the refusal is a Warn")
	}
}

func TestAnExistingNameIsAWarning(t *testing.T) {
	m := openAt(t, tree(t))
	m, _ = step(t, m, EntryCreatedMsg{Path: filepath.Join(m.CurrentPath(), "a"), Err: os.ErrExist})
	if m.footer.Level() != sharedcomponents.LevelWarning {
		t.Error("an existing name is a Warn, not an Error")
	}
}

func TestDConfirmsThenStartsADeleteRun(t *testing.T) {
	dir := tree(t)
	m := openAt(t, dir) // cursor on a
	m, cmd := press(t, m, keymap.Delete)
	m = settle(t, m, cmd)
	if m.mode != modeConfirmingDelete {
		t.Fatal("D must ask first")
	}
	m, cmd = step(t, m, sharedcomponents.ConfirmModalYesMsg{})
	start, ok := testutil.MsgOf[jobs.StartMsg](cmd)
	if !ok {
		t.Fatal("a confirmed delete must start a run")
	}
	if start.Run.Kind != jobs.KindDelete || start.Run.Items[0].Target != filepath.Join(dir, "a") {
		t.Errorf("unexpected run: %+v", start.Run)
	}
	if m.mode != modeNormal {
		t.Error("the modal must close")
	}
}

func TestDIsGreyedOnHome(t *testing.T) {
	m := openAt(t, filepath.Dir(testHome))
	idx, ok := m.indexOf(testHome)
	if !ok {
		t.Skip("home is not listed from its parent")
	}
	m.table.SetCursor(idx)
	if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Delete) {
		t.Error("D must be greyed on the home directory")
	}
	_, cmd := press(t, m, keymap.Delete)
	if _, ok := testutil.MsgOf[DeletePreparedMsg](cmd); ok {
		t.Error("a greyed D must not act")
	}
}

// ── Picker ──────────────────────────────────────────────────────────────────

func openPicker(t *testing.T, req PickRequestMsg) Model {
	t.Helper()
	m := NewPicker(&config.Config{}, req)
	m, _ = step(t, m, testutil.Resize(100, 30))
	return settle(t, m, m.Init())
}

func TestADirectoryPickerOffersTheBrowsedDirectoryFirst(t *testing.T) {
	dir := tree(t)
	m := openPicker(t, PickRequestMsg{Kind: PickDir, Start: dir, Tag: "ws"})
	if got := names(m); got[0] != "." {
		t.Fatalf("the first row must be the directory itself, got %v", got)
	}
	_, cmd := press(t, m, "enter")
	picked, ok := testutil.MsgOf[PathPickedMsg](cmd)
	if !ok || picked.Path != dir || picked.Tag != "ws" {
		t.Fatalf("enter on . must pick %s, got %+v", dir, picked)
	}
}

func TestADirectoryPickerGreysAFile(t *testing.T) {
	dir := tree(t)
	m := openPicker(t, PickRequestMsg{Kind: PickDir, Start: dir})
	idx, _ := m.indexOf(filepath.Join(dir, "notes.md"))
	m.table.SetCursor(idx)
	if !testutil.ShortcutDisabled(m.GetShortcuts(), "enter") {
		t.Error("a file cannot answer a directory picker")
	}
	_, cmd := press(t, m, "enter")
	if _, ok := testutil.MsgOf[PathPickedMsg](cmd); ok {
		t.Error("a greyed enter must not pick")
	}
}

func TestAFilePickerOpensOnTheFileItWasGiven(t *testing.T) {
	dir := tree(t)
	file := filepath.Join(dir, "z.txt")
	m := openPicker(t, PickRequestMsg{Kind: PickFile, Start: file})
	if m.CurrentPath() != dir {
		t.Fatalf("a file's picker opens in its directory, at %s", m.CurrentPath())
	}
	if r, _ := m.selected(); r.Path != file {
		t.Errorf("the cursor must be on the file, got %s", r.Path)
	}
	_, cmd := press(t, m, "enter")
	if picked, ok := testutil.MsgOf[PathPickedMsg](cmd); !ok || picked.Path != file {
		t.Errorf("enter must pick the file, got %+v", picked)
	}
}

func TestAPickerOnAMissingPathOpensAtItsNearestAncestor(t *testing.T) {
	dir := tree(t)
	m := openPicker(t, PickRequestMsg{Kind: PickDir, Start: filepath.Join(dir, "a", "not", "yet")})
	if m.CurrentPath() != filepath.Join(dir, "a") {
		t.Errorf("want the nearest existing ancestor, at %s", m.CurrentPath())
	}
}

func TestEscCancelsAPicker(t *testing.T) {
	m := openPicker(t, PickRequestMsg{Kind: PickDir, Start: tree(t), Tag: "log"})
	_, cmd := press(t, m, "esc")
	if cancelled, ok := testutil.MsgOf[PickCancelledMsg](cmd); !ok || cancelled.Tag != "log" {
		t.Errorf("esc must cancel with the tag, got %+v", cancelled)
	}
	if !m.InEditMode() {
		t.Error("a picker holds the keyboard: its esc answers the borrower")
	}
}
