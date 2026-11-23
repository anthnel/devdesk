package security

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/patch"
	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-753, security F1: the detail reads the scanned file when it opens, so
// what it reads is bounded and typed before a byte is taken. A hostile checkout
// can name a FIFO, a device or a file of any size.

// padded is src grown to exactly n bytes with comment lines after its content.
func padded(src string, n int) string {
	var b strings.Builder
	b.WriteString(src)
	for b.Len() < n {
		line := "# padding padding padding padding padding padding padding\n"
		if rest := n - b.Len(); rest < len(line) {
			line = strings.Repeat("#", rest-1) + "\n"
			if rest == 1 {
				line = "\n"
			}
		}
		b.WriteString(line)
	}
	return b.String()
}

func TestAFileLargerThanTheCapIsDeclinedWithoutBeingRead(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0021")
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"Dockerfile": padded(fx.files["Dockerfile"], maxFixFileBytes+1)}, scannedAtFixed.Add(-time.Hour))

	msg := computeMisconfigFix(dir, fx.finding, scannedAtFixed)
	declinedWith(t, msg, reasonFileTooLarge)
	if len(msg.File.Original) != 0 {
		t.Errorf("a file over the cap was read: %d bytes", len(msg.File.Original))
	}
}

func TestAFileExactlyAtTheCapIsStillFixed(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0021")
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"Dockerfile": padded(fx.files["Dockerfile"], maxFixFileBytes)}, scannedAtFixed.Add(-time.Hour))

	msg := computeMisconfigFix(dir, fx.finding, scannedAtFixed)
	if msg.Err != nil {
		t.Fatalf("a file at the cap was refused: %v", msg.Err)
	}
	if len(msg.File.Original) != maxFixFileBytes {
		t.Errorf("read %d bytes, want %d", len(msg.File.Original), maxFixFileBytes)
	}
}

// The build context rule edits .dockerignore and reads the finding's Dockerfile
// to find it: both are measured.
func TestTheFindingsFileAndTheEditedFileAreBothBounded(t *testing.T) {
	fx := fixtureFor(t, scan.BuildContextGitID)
	if fx.edited == fx.finding.File {
		t.Fatalf("the fixture edits the finding's own file; this test needs two")
	}
	for _, big := range []string{fx.finding.File, fx.edited} {
		t.Run(big, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, fx.files, scannedAtFixed.Add(-time.Hour))
			writeTree(t, dir, map[string]string{big: padded(fx.files[big], maxFixFileBytes+1)}, scannedAtFixed.Add(-time.Hour))

			declinedWith(t, computeMisconfigFix(dir, fx.finding, scannedAtFixed), reasonFileTooLarge)
		})
	}
}

func TestADirectoryWhereTheFileShouldBeIsDeclined(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0021")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Dockerfile"), 0o755); err != nil {
		t.Fatal(err)
	}
	declinedWith(t, computeMisconfigFix(dir, fx.finding, scannedAtFixed), reasonFileNotRegular)
}

// A file that grows after the stat is cut by the reader itself, not trusted to
// the size read earlier.
func TestTheBoundedReadStopsAtTheCapWhateverTheFileDoes(t *testing.T) {
	r := bytes.NewReader(bytes.Repeat([]byte("a"), maxFixFileBytes+1))
	if _, err := readBounded(r); !errors.Is(err, errFileTooLarge) {
		t.Errorf("readBounded of cap+1 bytes: err = %v, want errFileTooLarge", err)
	}
	got, err := readBounded(bytes.NewReader(bytes.Repeat([]byte("a"), maxFixFileBytes)))
	if err != nil || len(got) != maxFixFileBytes {
		t.Errorf("readBounded of cap bytes = %d bytes, %v", len(got), err)
	}
}

// ── Paths that cannot be resolved (F3) ───────────────────────────────────────

// A path that does not resolve is not proven to stay inside: only "does not
// exist" is treated as a path to be created.
func TestAPathThatCannotBeResolvedIsRefused(t *testing.T) {
	dir := t.TempDir()
	symlink(t, filepath.Join(dir, "b"), filepath.Join(dir, "a"))
	symlink(t, filepath.Join(dir, "a"), filepath.Join(dir, "b"))

	if staysInside(dir, filepath.Join(dir, "a", "Dockerfile")) {
		t.Error("a loop of links (ELOOP) was accepted")
	}
	if staysInside(filepath.Join(dir, "a"), filepath.Join(dir, "a", "Dockerfile")) {
		t.Error("a root that cannot be resolved was accepted")
	}
	if !staysInside(dir, filepath.Join(dir, "missing", "Dockerfile")) {
		t.Error("a path that does not exist yet was refused: ErrNotExist is not a link")
	}
}

func TestAPathBehindAnUnreadableDirectoryIsRefused(t *testing.T) {
	requireUnreadableFiles(t)
	dir := t.TempDir()
	sealed := filepath.Join(dir, "sealed")
	if err := os.MkdirAll(filepath.Join(sealed, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sealed, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sealed, 0o755) })

	if staysInside(dir, filepath.Join(sealed, "sub", "Dockerfile")) {
		t.Error("a path behind a directory that cannot be searched (EACCES) was accepted")
	}
}

// ── The write rechecks the path (F2) ─────────────────────────────────────────

// A link put in place between the confirmation and the write, to a file holding
// the very bytes the fix was computed from: the content check would pass, so the
// write itself must look at the path again.
func TestALinkPlacedAfterTheConfirmationIsNotWrittenThrough(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0021")
	target, outside := outsideTree(t)
	writeTree(t, target, map[string]string{"Dockerfile": fx.files["Dockerfile"]}, scannedAtFixed.Add(-time.Hour))
	before := readTree(t, outside, "Dockerfile")
	if before != fx.files["Dockerfile"] {
		t.Fatal("the outside file must hold the same bytes as the inside one")
	}

	m, cmd := step(t, changeModel(t, target, fx.finding, scannedAtFixed), testutil.Key(writeRemediationKey))
	m = feed(t, m, run(t, cmd))
	if m.confirmModal == nil {
		t.Fatal("no confirmation opened")
	}
	if err := os.Remove(filepath.Join(target, "Dockerfile")); err != nil {
		t.Fatal(err)
	}
	symlink(t, filepath.Join(outside, "Dockerfile"), filepath.Join(target, "Dockerfile"))

	m, cmd = step(t, m, components.ConfirmModalYesMsg{})
	written, ok := run(t, cmd).(MisconfigFixWrittenMsg)
	if !ok {
		t.Fatal("the confirmation did not answer with a MisconfigFixWrittenMsg")
	}
	if !errors.Is(written.Err, errLeavesTarget) {
		t.Errorf("Err = %v, want errLeavesTarget", written.Err)
	}
	if got := readTree(t, outside, "Dockerfile"); got != before {
		t.Errorf("the file behind the link was written:\n%s", got)
	}
	m = feed(t, m, written)
	if !strings.Contains(m.footer.Text(), "link that leaves the scanned directory") {
		t.Errorf("footer = %q, want the link refusal", m.footer.Text())
	}
}

// ── A file name is the repository's, not ours (F4) ───────────────────────────

const hostileName = "a\x1b]0;pwned\x07b\x1b[2Jc\nd.yaml"

func TestAHostileFileNameNeverReachesTheModalOrTheFooterRaw(t *testing.T) {
	text := misconfigConfirmationText(preparedWrite{File: hostileName}, "body\n")
	if strings.ContainsAny(text, "\x1b\x07") || strings.Contains(text, "\n"+"d.yaml") {
		t.Errorf("the confirmation carries raw control characters: %q", text)
	}
	if !strings.Contains(text, "abc d.yaml") {
		t.Errorf("the confirmation lost the readable part of the name: %q", text)
	}

	verdicts := []MisconfigVerifiedMsg{
		{Rule: "AVD-DS-0002", File: hostileName, Cleared: true},
		{Rule: "AVD-DS-0002", File: hostileName, Cleared: false},
		{Rule: "AVD-DS-0002", File: hostileName, Cleared: true, Scope: remediation.Scope{Rules: []string{"KSV-0030"}}},
	}
	for _, msg := range verdicts {
		m := feed(t, NewWithPreloadedResult(testConfig(), nil, &scan.Result{Target: t.TempDir()}), tea.WindowSizeMsg{Width: 160, Height: 30})
		_ = m.verdictFooter(msg)
		if got := m.footer.Text(); strings.ContainsAny(got, "\x1b\x07\n") || !strings.Contains(got, "abc d.yaml") {
			t.Errorf("verdict footer = %q", got)
		}
	}

	m := feed(t, NewWithPreloadedResult(testConfig(), nil, &scan.Result{Target: t.TempDir()}), tea.WindowSizeMsg{Width: 160, Height: 30})
	for _, err := range []error{errLeavesTarget, patch.ErrChanged, os.ErrPermission} {
		_ = m.reportFailedFix(MisconfigFixWrittenMsg{File: hostileName, Err: err})
		if got := m.footer.Text(); strings.ContainsAny(got, "\x1b\x07\n") {
			t.Errorf("failed-write footer for %v = %q", err, got)
		}
	}
}

// ── Logs name the relative path and the cause, never the absolute path (F5) ──

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func TestTheLogOfAFailedPreviewHasNoAbsolutePath(t *testing.T) {
	buf := captureLog(t)
	s := failedScene(t)
	got := buf.String()
	if got == "" {
		t.Fatal("a failed preview logged nothing")
	}
	if strings.Contains(got, s.dir) {
		t.Errorf("the log holds the absolute path:\n%s", got)
	}
	if !strings.Contains(got, "Dockerfile") || !strings.Contains(got, "no such file") {
		t.Errorf("the log lost the relative path or the cause:\n%s", got)
	}
}

func TestTheLogOfAFailedWriteHasNoAbsolutePath(t *testing.T) {
	buf := captureLog(t)
	dir := t.TempDir()
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, &scan.Result{Target: dir}), tea.WindowSizeMsg{Width: 160, Height: 30})
	cause := &fs.PathError{Op: "open", Path: filepath.Join(dir, "Dockerfile"), Err: fs.ErrPermission}
	_ = m.reportFailedFix(MisconfigFixWrittenMsg{File: "Dockerfile", Err: cause})
	_, _ = m.handleMisconfigFixPrepared(MisconfigFixPreparedMsg{Target: dir, Err: fmt.Errorf("Dockerfile: %w", cause)})

	got := buf.String()
	if strings.Contains(got, dir) {
		t.Errorf("the log holds the absolute path:\n%s", got)
	}
	if strings.Count(got, "permission denied") != 2 {
		t.Errorf("the log lost the cause:\n%s", got)
	}
}

// ── A verdict with no result still replaces the old diff (DEVDESK-753) ───────

// The file was rewritten whatever the re-scan managed to say: a detail that
// keeps the diff of the file as it was would show a change that is already in.
func TestAVerdictWithNoResultAsksForTheChangeAgain(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	verdicts := map[string]func(f scan.Finding, dir string) MisconfigVerifiedMsg{
		"no result": func(f scan.Finding, dir string) MisconfigVerifiedMsg {
			return MisconfigVerifiedMsg{Target: dir, Rule: f.ID, File: f.File}
		},
		"unreadable result": func(f scan.Finding, dir string) MisconfigVerifiedMsg {
			return MisconfigVerifiedMsg{Target: dir, Rule: f.ID, File: f.File, Err: errors.New("cache unreadable")}
		},
	}
	for name, verdict := range verdicts {
		t.Run(name, func(t *testing.T) {
			s := readyScene(t)
			m, cmd := step(t, s.m, verdict(*s.m.selectedFinding, s.dir))

			if m.state != StateDetails {
				t.Fatalf("the detail closed (state %v)", m.state)
			}
			if m.changePreview.State != previewPending {
				t.Errorf("state = %v, want pending: the old diff is still shown", m.changePreview.State)
			}
			next, ok := testutil.MsgOf[ChangePreviewMsg](cmd)
			if !ok {
				t.Fatal("the verdict started no new preview")
			}
			m = feed(t, m, next)
			if m.changePreview.State == previewPending || m.changePreview.Seq != next.Seq {
				t.Errorf("the new answer was not taken: state %v", m.changePreview.State)
			}
		})
	}
}
