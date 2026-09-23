package templates

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/template"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Fixtures ─────────────────────────────────────────────────────────────────

// inTempHome points the home directory at a temporary one: the scan writes to
// ~/.devdesk, and a test must never touch the real one.
func inTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// localTemplate commits the files in a fresh repository and returns an entry
// pointing at it.
func localTemplate(t *testing.T, slug string, files map[string]string) template.Entry {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet", "--initial-branch=main")
	files[".gitattributes"] = "* -text\n"
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "init")
	return template.Entry{Slug: slug, Name: "Fixture", Source: template.Source{Kind: template.KindLocal, Path: dir}}
}

// scanned builds a view on a catalog holding e, with the scanner answered by fn.
func scanned(t *testing.T, e template.Entry, fn scanFunc) Model {
	t.Helper()
	m := opened(t, e)
	m.scanner = fn
	return m
}

func withScanners(m Model, trivy, gitleaks bool) Model {
	m.deps = &scan.Report{Tools: map[scan.ToolID]scan.ToolStatus{
		scan.ToolTrivy:    {Available: trivy},
		scan.ToolGitleaks: {Available: gitleaks},
	}}
	return m
}

// launched presses S and returns the run the view asked the router to register
// and the builder the router would call with the context it stamped.
func launched(t *testing.T, m Model) (jobs.StartMsg, Model) {
	t.Helper()
	updated, cmd := m.Update(testutil.Key("S"))
	next := updated.(Model)
	start, ok := testutil.MsgOf[jobs.StartMsg](cmd)
	if !ok {
		t.Fatalf("S produced %T, want a jobs.StartMsg (footer: %q)", testutil.Msg(cmd), next.footer.Text())
	}
	return start, next
}

// ── Launching ────────────────────────────────────────────────────────────────

func TestSRegistersAScanOfTheTemplatesDirectory(t *testing.T) {
	home := inTempHome(t)
	m := scanned(t, entry("spring-api", "Spring API"), nil)

	start, _ := launched(t, m)

	want := filepath.Join(home, ".devdesk", "cache", "template-scan", "spring-api")
	run := start.Run
	if run.Kind != jobs.KindScan || run.Origin != command.ViewTemplates || run.Label != "Spring API" {
		t.Errorf("run = %+v, want a scan launched from the templates view labelled by the template", run)
	}
	if len(run.Items) != 1 || run.Items[0].Target != want {
		t.Errorf("items = %+v, want the one directory %s", run.Items, want)
	}
}

// The whole path, with the scanners answered: fetch, write, scan, cache.
func TestAScanFetchesMaterializesScansAndCaches(t *testing.T) {
	inTempHome(t)
	e := localTemplate(t, "fixture", map[string]string{"app.env": "TOKEN=abc\n"})
	var scannedDir string
	var sawFile bool
	m := scanned(t, e, func(_ context.Context, dir string, _ scan.ScanOptions) (*scan.Result, error) {
		scannedDir = dir
		_, err := os.Stat(filepath.Join(dir, "app.env"))
		sawFile = err == nil
		return &scan.Result{Counts: scan.SeverityCounts{Critical: 1, High: 2}, EndTime: time.Now()}, nil
	})
	start, _ := launched(t, m)

	msgs := testutil.Msgs(start.Work("work"))

	if len(msgs) != 2 {
		t.Fatalf("the scan produced %d messages, want starting then complete: %v", len(msgs), msgs)
	}
	if _, ok := msgs[0].(ScanStartingMsg); !ok {
		t.Errorf("first message = %T, want ScanStartingMsg", msgs[0])
	}
	done, ok := msgs[1].(ScanCompleteMsg)
	if !ok || done.Err != nil {
		t.Fatalf("second message = %#v, want a successful ScanCompleteMsg", msgs[1])
	}
	if !sawFile || scannedDir != done.Path {
		t.Errorf("the scanner saw dir %q (file present: %v), want the materialized template", scannedDir, sawFile)
	}
	if done.Counts.Critical != 1 || done.Counts.High != 2 {
		t.Errorf("counts = %+v", done.Counts)
	}

	// Cached by path in the context the run was launched in — which is what
	// puts it in :sec.
	entry := workspaceCache(t, "work").Get(done.Path)
	if entry == nil || entry.Critical != 1 {
		t.Errorf("the launching context's cache holds %+v, want the counts", entry)
	}
	if other := workspaceCache(t, "elsewhere").Get(done.Path); other != nil {
		t.Errorf("another context's cache holds %+v", other)
	}
}

func workspaceCache(t *testing.T, contextName string) *cache.WorkspaceScanCache {
	t.Helper()
	c, err := cache.NewWorkspaceScanCache(contextName)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// A template that cannot be fetched is a failed scan that never reached the
// scanners.
func TestAScanOfATemplateThatCannotBeFetchedFails(t *testing.T) {
	inTempHome(t)
	e := template.Entry{Slug: "gone", Name: "Gone", Source: template.Source{Kind: template.KindLocal, Path: filepath.Join(t.TempDir(), "missing")}}
	called := false
	m := scanned(t, e, func(context.Context, string, scan.ScanOptions) (*scan.Result, error) {
		called = true
		return &scan.Result{}, nil
	})
	start, _ := launched(t, m)

	msgs := testutil.Msgs(start.Work("work"))

	done := msgs[len(msgs)-1].(ScanCompleteMsg)
	if done.Err == nil {
		t.Fatal("a template that cannot be fetched scanned clean")
	}
	if called {
		t.Error("the scanners ran on a template that was never fetched")
	}
	if done.Transition().State != jobs.ItemFailed {
		t.Errorf("transition = %+v, want failed", done.Transition())
	}
}

func TestAScannerThatFailsFailsTheScan(t *testing.T) {
	inTempHome(t)
	e := localTemplate(t, "fixture", map[string]string{"a.txt": "x"})
	m := scanned(t, e, func(context.Context, string, scan.ScanOptions) (*scan.Result, error) {
		return nil, errors.New("trivy exploded")
	})
	start, _ := launched(t, m)

	msgs := testutil.Msgs(start.Work("work"))

	done := msgs[len(msgs)-1].(ScanCompleteMsg)
	if done.Err == nil || !strings.Contains(done.Err.Error(), "trivy exploded") {
		t.Errorf("Err = %v, want the scanner's", done.Err)
	}
	m = send(t, m, done)
	if m.footer.Level() != sharedcomponents.LevelError {
		t.Errorf("footer level = %v, want an error", m.footer.Level())
	}
}

// A template has no pipeline to grade: plumber would look for a remote it does
// not have.
func TestTheCIScoreIsOffForATemplate(t *testing.T) {
	inTempHome(t)
	m := scanned(t, entry("api", "API"), nil)
	m.config.Scan.Categories.CI.Enabled = true

	if m.scanOptions().Categories.CI.Enabled {
		t.Error("a template scan asks for a CI score")
	}
}

// ── Reporting ────────────────────────────────────────────────────────────────

func TestTheScanReportsToTheRegistry(t *testing.T) {
	cancelled := false
	starting := ScanStartingMsg{Path: "/dir", Cancel: func() { cancelled = true }}
	tr := starting.Transition()
	if tr.State != jobs.ItemRunning || tr.Target != "/dir" || tr.Kind != jobs.KindScan {
		t.Errorf("starting = %+v", tr)
	}
	tr.Cancel()
	if !cancelled {
		t.Error("the cancel function does not travel with the running state, so K could not stop it")
	}

	if got := (ScanCompleteMsg{Path: "/dir"}).Transition(); got.State != jobs.ItemDone {
		t.Errorf("complete = %+v, want done", got)
	}
}

func TestTheFooterSaysHowTheScanEnded(t *testing.T) {
	m := opened(t, entry("api", "API"))

	clean := send(t, m, ScanCompleteMsg{Name: "API"})
	if !strings.Contains(clean.footer.Text(), "no findings") {
		t.Errorf("clean footer = %q", clean.footer.Text())
	}

	dirty := send(t, m, ScanCompleteMsg{Name: "API", Counts: scan.SeverityCounts{Critical: 2, Low: 1}})
	if text := dirty.footer.Text(); !strings.Contains(text, "2 critical") || !strings.Contains(text, ":sec") {
		t.Errorf("dirty footer = %q, want the counts and where the report is", text)
	}

	// "No findings" from a scan whose stage failed would be a claim about
	// something it did not look at.
	partial := send(t, m, ScanCompleteMsg{Name: "API", Partial: true})
	if partial.footer.Level() != sharedcomponents.LevelWarning || strings.Contains(partial.footer.Text(), "no findings") {
		t.Errorf("partial footer = %q (level %v), want a warning that does not say no findings", partial.footer.Text(), partial.footer.Level())
	}
}

func TestALiveScanShowsInTheFooterStatus(t *testing.T) {
	m := opened(t, entry("api", "API"))
	run := jobs.NewRun(jobs.KindScan, command.ViewTemplates, "", "API", "/dir")
	run.ID = 1

	m = send(t, m, jobs.ChangedMsg{Runs: []jobs.Run{run}})

	if status := m.jobsStatus(); !status.Spinner || !strings.Contains(status.Text, "Scanning") {
		t.Errorf("status = %+v, want a spinner saying it is scanning", status)
	}
}

// ── Availability (Rule 130) ──────────────────────────────────────────────────

func TestNoScannerGreysSAndSaysWhy(t *testing.T) {
	m := withScanners(opened(t, entry("api", "API")), false, false)

	if !testutil.ShortcutDisabled(m.GetShortcuts(), "S") {
		t.Error("S is not greyed with neither scanner available")
	}
	m = send(t, m, testutil.Key("S"))
	if m.footer.Text() != reasonNoScanner {
		t.Errorf("footer = %q, want %q", m.footer.Text(), reasonNoScanner)
	}
}

// Not knowing is not knowing that not: greying S for the length of the check
// would read as a glitch.
func TestSStaysLitUntilTheScannersAreKnown(t *testing.T) {
	m := opened(t, entry("api", "API"))
	if m.deps != nil {
		t.Fatal("deps are set before the check came back")
	}
	if !testutil.ShortcutEnabled(m.GetShortcuts(), "S") {
		t.Error("S is greyed before anything is known")
	}
	if !testutil.ShortcutEnabled(withScanners(m, false, true).GetShortcuts(), "S") {
		t.Error("S is greyed although Gitleaks is available — one scanner is enough")
	}
}

func TestSIsRefusedWhileThatTemplateIsBeingScanned(t *testing.T) {
	home := inTempHome(t)
	m := opened(t, entry("api", "API"))
	dir := filepath.Join(home, ".devdesk", "cache", "template-scan", "api")
	run := jobs.NewRun(jobs.KindScan, command.ViewTemplates, "", "API", dir)
	run.ID = 1
	m = send(t, m, jobs.ChangedMsg{Runs: []jobs.Run{run}})

	if !testutil.ShortcutDisabled(m.GetShortcuts(), "S") {
		t.Error("S is not greyed while the template is being scanned")
	}
	m = send(t, m, testutil.Key("S"))
	if m.footer.Text() != reasonScanRunning {
		t.Errorf("footer = %q, want %q", m.footer.Text(), reasonScanRunning)
	}
}

func TestNothingSelectedGreysS(t *testing.T) {
	m := opened(t)
	if !testutil.ShortcutDisabled(m.GetShortcuts(), "S") {
		t.Error("S is not greyed with no template selected")
	}
}

// The picker changes nothing and scans nothing.
func TestThePickerHasNoScan(t *testing.T) {
	m := pickerOn(t, entry("api", "API"))
	if testutil.HasShortcut(m.GetShortcuts(), "S") {
		t.Error("S is advertised in the picker")
	}
	_, cmd := m.Update(testutil.Key("S"))
	if _, ok := testutil.MsgOf[jobs.StartMsg](cmd); ok {
		t.Error("S started a scan from the picker")
	}
}
