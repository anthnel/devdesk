package workspaces

import (
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// secretsFound writes the "a secret was found" verdict. Le verdict est un
// pointeur parce qu'il en a trois : nil dit que personne n'a cherché.
func secretsFound() *bool { v := true; return &v }

// Loading entries walks the filesystem and shells out to git; scanning runs
// Trivy and Gitleaks in containers. No test executes a command Update returns:
// entries and scan results are fed in as messages instead.
//
// These tests drive Update() and View() only — model.go is 1299 lines and due
// to be split, and assertions on its internals would pin the current layout.

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	// Reading a Cmd runs it, and most of this view's carry a footer expiry —
	// three real seconds each (Rule 128). Nothing here asserts on that
	// duration; the test that does is TestAMessageGetsThreeSeconds, in the
	// package that owns it.
	sharedcomponents.FooterMsgDuration = time.Millisecond

	// The context tests write to the real scan cache and set the current
	// context, both of which live under the home directory. Redirecting it for
	// the package is what keeps a test from rewriting the developer's
	// ~/.devdesk — the same reason internal/ui/security does it.
	home, err := os.MkdirTemp("", "devdesk-workspaces-test")
	if err != nil {
		log.SetOutput(os.Stderr)
		panic(err)
	}
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("USERPROFILE", home)

	code := m.Run()

	_ = os.RemoveAll(home)
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.App.WorkspacesDir = "/tmp/workspaces"
	cfg.App.IDECommand = "code"
	return cfg
}

// entryFixtures cover the shapes the view branches on: a git repo with a dirty
// tree, a clean git repo, a plain directory holding nested repos, a directory
// holding none, and a file.
func entryFixtures() []Entry {
	modTime := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	return []Entry{
		{
			Name: "devdesk", Path: "/tmp/workspaces/devdesk", IsDir: true, ModTime: modTime,
			IsGitRepo: true, GitBranch: "main", GitRemote: "anthnel/devdesk",
			GitRemoteURL: "https://github.com/anthnel/devdesk",
			GitModified:  2, GitUntracked: 1, GitUnpushed: 3, GitUnpulled: 0,
		},
		{
			Name: "clean-repo", Path: "/tmp/workspaces/clean-repo", IsDir: true, ModTime: modTime,
			IsGitRepo: true, GitBranch: "main", GitRemote: "anthnel/clean",
			GitRemoteURL: "https://github.com/anthnel/clean",
		},
		{
			Name: "clients", Path: "/tmp/workspaces/clients", IsDir: true, ModTime: modTime,
			SubRepoPaths: []string{"/tmp/workspaces/clients/a", "/tmp/workspaces/clients/b"},
		},
		{
			Name: "empty-dir", Path: "/tmp/workspaces/empty-dir", IsDir: true, ModTime: modTime,
		},
		{
			Name: "notes.md", Path: "/tmp/workspaces/notes.md", IsDir: false, ModTime: modTime,
		},
	}
}

// newTestModel returns a laid-out model with no entries yet.
func newTestModel(t *testing.T) Model {
	t.Helper()
	return feed(t, New(testConfig(), nil), tea.WindowSizeMsg{Width: 160, Height: 30})
}

// loadedModel returns a model that has absorbed the fixture entries.
func loadedModel(t *testing.T) Model {
	t.Helper()
	return feed(t, newTestModel(t), EntriesLoadedMsg{Entries: entryFixtures()})
}

// scannedModel returns a loaded model with a cached scan result for the first
// repo, which is what unlocks the details view.
func scannedModel(t *testing.T) Model {
	t.Helper()
	m := loadedModel(t)
	return feed(t, m, ScanCacheLoadedMsg{Cache: map[string]cache.WorkspaceScanEntry{
		"/tmp/workspaces/devdesk": {
			RepoPath: "/tmp/workspaces/devdesk",
			Critical: 1, High: 2, Medium: 3, Low: 4, Sensitive: secretsFound(),
			ScannedAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		},
	}})
}

func feed(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = step(t, m, msg)
	}
	return m
}

// scanAll runs A and answers its modal, which is what one of two keys used to
// do. purge is the checkbox: ctrl+a's half of the old pair (§3.26).
func scanAll(t *testing.T, m Model, purge bool) (Model, tea.Cmd) {
	t.Helper()
	m, _ = step(t, m, testutil.Key(keymap.ScanAll))
	if m.scanAllModal == nil {
		t.Fatal("A did not open the scan-all confirmation")
	}
	return step(t, m, sharedcomponents.OptionConfirmModalYesMsg{Option: purge})
}

// refused presses a key the header greys out and checks the view declined it,
// naming why in the footer.
//
// It is the other half of shortcutDisabled. A greyed key that still acted would
// pass one and fail the other, which is exactly the divergence the single
// actionSet exists to make impossible.
//
// The Cmd Update returns is deliberately dropped: it is the footer's expiry
// timer, and running it sleeps three real seconds (Rule 128).
func refused(t *testing.T, m Model, key, wantReason string) Model {
	t.Helper()
	next, cmd := step(t, m, testutil.Key(key))
	if got := next.RenderFooter(200); !strings.Contains(got, wantReason) {
		t.Errorf("%s was declined without saying why — footer:\n%s\nwant it to carry %q", key, got, wantReason)
	}
	if run, started := startedRun(cmd); started {
		t.Errorf("%s was declined and registered a %s run anyway: %+v", key, run.Kind, run.Items)
	}
	return next
}

// startedRun returns the run a command asks the router to register, if it asks
// for one. It is how a test says "no work was launched" now that launching is a
// message rather than a write into the model.
func startedRun(cmd tea.Cmd) (jobs.Run, bool) {
	msg, ok := testutil.MsgOf[jobs.StartMsg](cmd)
	if !ok {
		return jobs.Run{}, false
	}
	return msg.Run, true
}

// wantRun fails unless the command registers a run of the given kind over
// exactly these targets, every one of them queued.
func wantRun(t *testing.T, cmd tea.Cmd, kind jobs.Kind, targets ...string) jobs.Run {
	t.Helper()
	run, started := startedRun(cmd)
	if !started {
		t.Fatalf("no run was registered, want a %s over %v", kind, targets)
	}
	if run.Kind != kind {
		t.Errorf("run kind = %q, want %q", run.Kind, kind)
	}
	got := make([]string, 0, len(run.Items))
	for _, item := range run.Items {
		got = append(got, item.Target)
	}
	if strings.Join(got, "|") != strings.Join(targets, "|") {
		t.Errorf("run targets = %v, want %v", got, targets)
	}
	for _, item := range run.Items {
		if item.State != jobs.ItemQueued {
			t.Errorf("%s starts as %q, want every target queued (D6)", item.Target, item.State)
		}
	}
	return run
}

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want workspaces.Model", next)
	}
	return updated, cmd
}

// colName is the Name cell's index. Column 0 is the glyph naming what the row
// is, so a test reading names has to skip it — and naming the index here means
// a future column added to the left moves one constant rather than every test.
// Column indices the tests read by name. Column 0 is the glyph naming what the
// row is, so naming these means a column added to the left moves three
// constants rather than every assertion.
const (
	colIcon      = 0
	colName      = 1
	colGitStatus = 3
)

// rowNames returns the Name cell of every table row.
func rowNames(rows []table.Row) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row[colName])
	}
	return names
}

// ── Putting work in flight, the way the router does ──────────────────────────
//
// The view holds no bookkeeping of its own any more: what is running arrives in
// a jobs.ChangedMsg. These helpers build one, so a test says "a scan is running
// on this repository" in the same words the application uses.

// withJobs feeds the model a snapshot, as the router's broadcast would.
func withJobs(t *testing.T, m Model, runs ...jobs.Run) Model {
	t.Helper()
	return feed(t, m, jobs.ChangedMsg{Runs: runs, Frame: "*", RenderedFrame: "*"})
}

// runningRun builds a run of one kind with every target already running.
func runningRun(kind jobs.Kind, targets ...string) jobs.Run {
	run := jobs.NewRun(kind, command.ViewWorkspaces, "default", "~/work", targets...)
	for i := range run.Items {
		run.Items[i].State = jobs.ItemRunning
	}
	return run
}

// running puts the given targets in flight under one kind.
func running(t *testing.T, m Model, kind jobs.Kind, targets ...string) Model {
	t.Helper()
	return withJobs(t, m, runningRun(kind, targets...))
}

// settledRun builds a run whose items all carry the given state and detail.
func settledRun(kind jobs.Kind, state jobs.ItemState, detail string, targets ...string) jobs.Run {
	run := jobs.NewRun(kind, command.ViewWorkspaces, "default", "~/work", targets...)
	for i := range run.Items {
		run.Items[i].State = state
		run.Items[i].Detail = detail
	}
	return run
}
