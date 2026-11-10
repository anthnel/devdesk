package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/filebrowser"
	"github.com/anthnel/devdesk/internal/ui/jobsview"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-227: a delete started from ws or :files is an immediate run. Once the
// router has registered it, :jobs greys K and a press posts no cancellation.
// The run comes from the views' own deleteRun, so a regression back to a
// cancellable run is seen here.

// assertDeleteCannotBeStopped opens :jobs and checks K for the one delete
// running in the registry.
func assertDeleteCannotBeStopped(t *testing.T, a *App, origin command.ViewType, label string) {
	t.Helper()
	testutil.FastTimers(t, &components.FooterMsgDuration)
	snap := a.jobs.Snapshot()
	if len(snap) != 1 || snap[0].Kind != jobs.KindDelete || snap[0].Origin != origin ||
		snap[0].Items[0].State != jobs.ItemRunning {
		t.Fatalf("snapshot = %+v, want one running delete from %s", snap, origin)
	}
	if snap[0].Stoppable() {
		t.Error("a delete in flight is Stoppable()")
	}

	testutil.Msgs(a.switchView(command.ViewJobs))
	a.resize(a.width, a.height)
	view, ok := a.views[command.ViewJobs].(jobsview.Model)
	if !ok {
		t.Fatalf("the jobs view is a %T", a.views[command.ViewJobs])
	}
	if row := rowOf(t, ansi.Strip(view.Render()), label); !strings.Contains(row, string(jobs.KindDelete)) {
		t.Errorf("the selected :jobs row %q does not name the delete", row)
	}
	sc := view.GetShortcuts()
	if !testutil.HasShortcut(sc, "K") {
		t.Fatal("K was dropped rather than greyed (Rule 130)")
	}
	if !testutil.ShortcutDisabled(sc, "K") {
		t.Error("K is offered for a delete in flight")
	}

	_, cmd := a.Update(testutil.Key("K"))
	for _, msg := range testutil.Msgs(cmd) {
		switch msg.(type) {
		case jobs.CancelMsg, jobs.CancelItemMsg:
			t.Errorf("K posted %T for a delete in flight", msg)
		}
	}
	if got := a.jobs.Snapshot()[0].Items[0].State; got != jobs.ItemRunning {
		t.Errorf("item state after K = %q, want running", got)
	}
}

func TestAWsDeleteCannotBeStoppedFromJobs(t *testing.T) {
	a, repo := wsRouter(t)
	openWorkspaces(t, a)

	a.Update(testutil.Key(keymap.Delete))
	_, cmd := a.Update(components.ConfirmModalYesMsg{})
	start := startFrom(t, a, cmd)
	if start.Run.Kind != jobs.KindDelete || start.Run.Items[0].Target != repo {
		t.Fatalf("ws started %+v, want a delete of %s", start.Run, repo)
	}
	assertDeleteCannotBeStopped(t, a, command.ViewWorkspaces, "api")
}

func TestAFilesDeleteCannotBeStoppedFromJobs(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	// isolateHome made HOME a temp directory; :files opens there.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(home, "victim.txt")
	if err := os.WriteFile(victim, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	testutil.Msgs(a.switchView(command.ViewFiles))
	for _, msg := range testutil.Msgs(a.views[command.ViewFiles].Init()) {
		a.Update(msg)
	}
	a.resize(a.width, a.height)
	_, cmd := a.Update(testutil.Key(keymap.Delete))
	var prepared tea.Msg
	for _, msg := range testutil.Msgs(cmd) {
		prepared = msg
		a.Update(msg)
	}
	if _, ok := prepared.(filebrowser.DeletePreparedMsg); !ok {
		t.Fatalf("D produced %T, want DeletePreparedMsg", prepared)
	}
	_, cmd = a.Update(components.ConfirmModalYesMsg{})
	start := startFrom(t, a, cmd)
	if start.Run.Kind != jobs.KindDelete || start.Run.Items[0].Target != victim {
		t.Fatalf(":files started %+v, want a delete of %s", start.Run, victim)
	}
	// The registered run's work is not run: the file is still there.
	assertDeleteCannotBeStopped(t, a, command.ViewFiles, "victim.txt")
}
