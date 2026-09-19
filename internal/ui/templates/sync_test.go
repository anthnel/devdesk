package templates

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/template"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// synced is a view on a catalog holding e, with a cache of its own so no test
// writes to the real ~/.devdesk.
func synced(t *testing.T, e template.Entry) Model {
	t.Helper()
	m := opened(t, e)
	m.cache = template.NewCacheAt(t.TempDir())
	return m
}

func pressF(t *testing.T, m Model) (jobs.StartMsg, Model) {
	t.Helper()
	updated, cmd := m.Update(testutil.Key("F"))
	next := updated.(Model)
	start, ok := testutil.MsgOf[jobs.StartMsg](cmd)
	if !ok {
		t.Fatalf("F produced %T, want a jobs.StartMsg (footer: %q)", testutil.Msg(cmd), next.footer.Text())
	}
	return start, next
}

func TestFRegistersASyncOfTheTemplate(t *testing.T) {
	m := synced(t, entry("spring-api", "Spring API"))

	start, _ := pressF(t, m)

	run := start.Run
	if run.Kind != jobs.KindSync || run.Origin != command.ViewTemplates || run.Label != "Spring API" {
		t.Errorf("run = %+v, want a sync launched from the templates view labelled by the template", run)
	}
	if len(run.Items) != 1 || run.Items[0].Target != "spring-api" {
		t.Errorf("items = %+v, want the one template", run.Items)
	}
}

// The whole path: the source is read, the copy is kept, and a second sync picks
// up what the source gained since.
func TestASyncReadsTheSourceAgainAndKeepsTheCopy(t *testing.T) {
	e := localTemplate(t, "fixture", map[string]string{"one.txt": "1\n"})
	m := synced(t, e)

	start, _ := pressF(t, m)
	msgs := testutil.Msgs(start.Work("work"))
	if len(msgs) != 2 {
		t.Fatalf("the sync produced %d messages, want starting then complete: %v", len(msgs), msgs)
	}
	if _, ok := msgs[0].(SyncStartingMsg); !ok {
		t.Errorf("first message = %T, want SyncStartingMsg", msgs[0])
	}
	done, ok := msgs[1].(SyncCompleteMsg)
	if !ok || done.Err != nil {
		t.Fatalf("second message = %#v, want a successful SyncCompleteMsg", msgs[1])
	}
	first := done.Files
	if _, ok := m.cache.FetchedAt("fixture", e.Source); !ok {
		t.Error("the sync did not keep a copy")
	}

	commit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = e.Source.Path
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(e.Source.Path, "two.txt"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit("add", ".")
	commit("commit", "--quiet", "-m", "two")

	msgs = testutil.Msgs(syncTemplateCmd(syncJob{entry: e, cache: m.cache}))
	done = msgs[len(msgs)-1].(SyncCompleteMsg)
	if done.Err != nil || done.Files != first+1 {
		t.Errorf("second sync = %+v, want %d files", done, first+1)
	}
}

func TestASyncThatFailsSaysSoAndKeepsNothing(t *testing.T) {
	e := entry("api", "API")
	e.Source = template.Source{Kind: template.KindLocal, Path: filepath.Join(t.TempDir(), "missing")}
	m := synced(t, e)

	msgs := testutil.Msgs(syncTemplateCmd(syncJob{entry: e, cache: m.cache}))
	done := msgs[len(msgs)-1].(SyncCompleteMsg)
	if done.Err == nil {
		t.Fatal("syncing a missing directory succeeded")
	}
	m = send(t, m, done)
	if got := m.footer.Text(); got != "Sync of API failed — check logs" {
		t.Errorf("footer = %q", got)
	}
	if _, ok := m.cache.FetchedAt("api", e.Source); ok {
		t.Error("a failed sync left a copy")
	}
}

func TestTheFooterSaysWhatWasSynced(t *testing.T) {
	m := synced(t, entry("api", "API"))
	m = send(t, m, SyncCompleteMsg{Slug: "api", Name: "API", Files: 3})
	if got := m.footer.Text(); got != "Synced API — 3 files" {
		t.Errorf("footer = %q", got)
	}
}

func TestTheSyncReportsToTheRegistry(t *testing.T) {
	if got := (SyncStartingMsg{Slug: "api"}).Transition(); got.State != jobs.ItemRunning || got.Target != "api" || got.Kind != jobs.KindSync {
		t.Errorf("starting = %+v", got)
	}
	if got := (SyncCompleteMsg{Slug: "api"}).Transition(); got.State != jobs.ItemDone {
		t.Errorf("complete = %+v, want done", got)
	}
	if got := (SyncCompleteMsg{Slug: "api", Err: errors.New("boom")}).Transition(); got.State != jobs.ItemFailed {
		t.Errorf("failed = %+v, want failed", got)
	}
}

func TestFIsRefusedWhileThatTemplateIsBeingSynced(t *testing.T) {
	m := synced(t, entry("api", "API"))
	run := jobs.NewRun(jobs.KindSync, command.ViewTemplates, "", "API", "api")
	run.ID = 1
	m = send(t, m, jobs.ChangedMsg{Runs: []jobs.Run{run}})

	if !testutil.ShortcutDisabled(m.GetShortcuts(), "F") {
		t.Error("F is not greyed while the template is being synced")
	}
	m = send(t, m, testutil.Key("F"))
	if m.footer.Text() != reasonSyncRunning {
		t.Errorf("footer = %q, want %q", m.footer.Text(), reasonSyncRunning)
	}
}

func TestNothingSelectedGreysF(t *testing.T) {
	m := opened(t)
	if !testutil.ShortcutDisabled(m.GetShortcuts(), "F") {
		t.Error("F is not greyed with no template selected")
	}
}

func TestThePickerHasNoSync(t *testing.T) {
	m := pickerOn(t, entry("api", "API"))
	if testutil.HasShortcut(m.GetShortcuts(), "F") {
		t.Error("F is advertised in the picker")
	}
	_, cmd := m.Update(testutil.Key("F"))
	if _, ok := testutil.MsgOf[jobs.StartMsg](cmd); ok {
		t.Error("F started a sync from the picker")
	}
}

// Deleting a template takes its cached copy with it.
func TestDeletingATemplateForgetsItsCopy(t *testing.T) {
	e := localTemplate(t, "fixture", map[string]string{"one.txt": "1\n"})
	path := catalogWith(t, e)
	store, err := template.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	cache := template.NewCacheAt(t.TempDir())
	if _, err := cache.Sync(t.Context(), e.Slug, e.Source, template.Credentials{}); err != nil {
		t.Fatal(err)
	}

	msg := testutil.Msgs(deleteCmd(store, cache, e.Slug))[0].(DeletedMsg)

	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	if _, ok := cache.FetchedAt(e.Slug, e.Source); ok {
		t.Error("the cached copy survived the delete")
	}
}
