package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forward"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// routerWithStore is a router whose forwards file lives in the test's own
// directory. newWithSize sets no store, so nothing else in this package can
// reach the developer's ~/.devdesk.
func routerWithStore(t *testing.T) (*App, *forward.Store) {
	t.Helper()
	testutil.FastTimers(t, &components.FooterMsgDuration)
	a := newWithSize(testConfig(), 120, 40)
	t.Cleanup(a.sharedState.Forwards.CloseAll)
	store := forward.NewStore(filepath.Join(t.TempDir(), forward.FileName))
	a.forwardStore = store
	return a, store
}

// footersPosted feeds a message to the router and returns what it asked the
// footer to say, running the batch it answers with.
func footersPosted(t *testing.T, a *App, msg tea.Msg) []components.PostFooterMsg {
	t.Helper()
	_, cmd := a.Update(msg)
	var out []components.PostFooterMsg
	for _, m := range testutil.Msgs(cmd) {
		if p, ok := m.(components.PostFooterMsg); ok {
			out = append(out, p)
		}
	}
	return out
}

func TestAnOpenedForwardIsSaved(t *testing.T) {
	a, store := routerWithStore(t)
	target := echoServer(t)
	port := freePort(t)

	opened := runCmd(t, mustCmd(a.Update(forward.OpenMsg{LocalPort: port, Target: target}))).(forward.OpenedMsg)
	footersPosted(t, a, opened)

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := forward.Entry{LocalPort: port, Target: target}
	if len(got) != 1 || got[0] != want {
		t.Errorf("the file holds %+v, want [%+v]", got, want)
	}
}

func TestARefusedOpenSavesNothing(t *testing.T) {
	a, store := routerWithStore(t)

	opened := runCmd(t, mustCmd(a.Update(forward.OpenMsg{LocalPort: 80, Target: "127.0.0.1:1"}))).(forward.OpenedMsg)
	footersPosted(t, a, opened)

	if _, err := os.Stat(store.Path()); !os.IsNotExist(err) {
		t.Errorf("a refused open wrote %s (stat err: %v)", store.Path(), err)
	}
}

func TestAClosedForwardIsRemovedFromTheFile(t *testing.T) {
	a, store := routerWithStore(t)
	target := echoServer(t)
	f, err := a.sharedState.Forwards.Open(freePort(t), target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.Save(a.sharedState.Forwards.Entries()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	footersPosted(t, a, forward.CloseMsg{ID: f.ID})

	if got, _ := store.Load(); len(got) != 0 {
		t.Errorf("the file still holds %+v after a delete", got)
	}
}

func TestAPauseIsSavedAsPaused(t *testing.T) {
	a, store := routerWithStore(t)
	target := echoServer(t)
	f, err := a.sharedState.Forwards.Open(freePort(t), target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	toggled := runCmd(t, mustCmd(a.Update(forward.ToggleMsg{ID: f.ID}))).(forward.ToggledMsg)
	if toggled.Err != nil {
		t.Fatalf("Toggle: %v", toggled.Err)
	}
	footersPosted(t, a, toggled)

	got, _ := store.Load()
	if len(got) != 1 || !got[0].Paused {
		t.Errorf("the file holds %+v, want the one entry paused", got)
	}
}

func TestAResumeThatCannotBindWarnsAndKeepsTheEntry(t *testing.T) {
	a, store := routerWithStore(t)
	target := echoServer(t)
	f, err := a.sharedState.Forwards.Open(freePort(t), target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := a.sharedState.Forwards.Toggle(f.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	squatter := squat(t, f.Addr())
	defer func() { _ = squatter.Close() }()

	toggled := runCmd(t, mustCmd(a.Update(forward.ToggleMsg{ID: f.ID}))).(forward.ToggledMsg)
	posted := footersPosted(t, a, toggled)

	if len(posted) != 1 || posted[0].Level != components.LevelWarning ||
		!strings.Contains(posted[0].Text, "already taken") {
		t.Errorf("footer = %+v, want one warning naming the taken port", posted)
	}
	if got, _ := store.Load(); len(got) != 1 {
		t.Errorf("the file holds %+v, want the entry kept", got)
	}
}

func TestStartupReopensWhatWasSavedAndSaysSo(t *testing.T) {
	a, store := routerWithStore(t)
	target := echoServer(t)
	port := freePort(t)
	if err := store.Save([]forward.Entry{{LocalPort: port, Target: target}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	restored := runCmd(t, a.restoreForwardsCmd()).(forward.RestoredMsg)
	if restored.Err != nil || restored.Summary.Live != 1 {
		t.Fatalf("restore = %+v, want one live", restored)
	}
	posted := footersPosted(t, a, restored)

	if len(posted) != 1 || posted[0].Level != components.LevelInfo ||
		!strings.Contains(posted[0].Text, "Restored 1 forward") {
		t.Errorf("footer = %+v, want an info saying one forward was restored", posted)
	}
	if got := a.sharedState.Forwards.List(); len(got) != 1 || got[0].LocalPort != port {
		t.Errorf("the registry holds %+v, want the saved forward", got)
	}
}

func TestStartupWarnsWhenSomethingCouldNotBeBound(t *testing.T) {
	a, store := routerWithStore(t)
	silent := "127.0.0.1:" + itoa(freePort(t))
	if err := store.Save([]forward.Entry{{LocalPort: freePort(t), Target: silent}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	restored := runCmd(t, a.restoreForwardsCmd()).(forward.RestoredMsg)
	posted := footersPosted(t, a, restored)

	if len(posted) != 1 || posted[0].Level != components.LevelWarning ||
		!strings.Contains(posted[0].Text, "1 of 1 forwards could not be bound") {
		t.Errorf("footer = %+v, want a warning counting the unbound one", posted)
	}
	// Restoring must not rewrite the file: nothing changed.
	if got, _ := store.Load(); len(got) != 1 {
		t.Errorf("the file holds %+v, want the entry kept", got)
	}
}

func TestAnUnreadableFileIsReportedAndNotOverwrittenByStartup(t *testing.T) {
	a, store := routerWithStore(t)
	const garbage = "forwards: [not: yaml\n"
	if err := os.WriteFile(store.Path(), []byte(garbage), 0o600); err != nil {
		t.Fatal(err)
	}

	restored := runCmd(t, a.restoreForwardsCmd()).(forward.RestoredMsg)
	if restored.Err == nil {
		t.Fatal("an unreadable file was reported as fine")
	}
	posted := footersPosted(t, a, restored)
	if len(posted) != 1 || posted[0].Level != components.LevelError {
		t.Errorf("footer = %+v, want one error", posted)
	}
	if got, _ := os.ReadFile(store.Path()); string(got) != garbage {
		t.Errorf("startup touched the unreadable file: %q", got)
	}
}

func TestARouterBuiltByATestNeverTouchesTheHomeDirectory(t *testing.T) {
	a := newWithSize(testConfig(), 120, 40)
	if a.forwardStore != nil {
		t.Fatal("newWithSize set a forwards store; only New may, or a test would write ~/.devdesk")
	}
	if a.restoreForwardsCmd() != nil || a.saveForwardsCmd() != nil {
		t.Error("without a store the router still produced I/O")
	}
}

func TestAFailedSaveReachesTheFooter(t *testing.T) {
	a, _ := routerWithStore(t)
	posted := footersPosted(t, a, forward.SavedMsg{Err: os.ErrPermission})
	if len(posted) != 1 || posted[0].Level != components.LevelError {
		t.Errorf("footer = %+v, want one error", posted)
	}
}
