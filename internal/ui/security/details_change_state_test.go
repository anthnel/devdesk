package security

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
)

// DEVDESK-753, lot 2, steps 3 and 4 (and the state side of step 5): the preview
// of the change is computed by a Cmd the detail starts when it opens, stored by
// Update, and read from the model by the header, ctrl+o and the Fix line. No test
// here reads the clock, the keyring, Docker or a real process: files live under
// t.TempDir() with times set by os.Chtimes, and a Cmd is run by hand with run
// (feed drops Cmds).

// ── Scenes ───────────────────────────────────────────────────────────────────

// previewScene is a detail opened on a real directory.
type previewScene struct {
	m   Model
	dir string
	fx  changeFixture
	// cmd is what enter returned: the preview computation.
	cmd tea.Cmd
	// msg is the answer, once answered has run.
	msg ChangePreviewMsg
}

// openPreview writes the fixture with its files stamped stamp after the end of
// the scan, and opens the detail of its finding. Nothing is answered yet.
func openPreview(t *testing.T, fx changeFixture, stamp time.Duration) previewScene {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, fx.files, scannedAtFixed.Add(stamp))
	m, cmd := step(t, changeModel(t, dir, fx.finding, scannedAtFixed), testutil.Key("enter"))
	if m.state != StateDetails || m.selectedFinding == nil {
		t.Fatalf("enter did not open the detail of %s", fx.id)
	}
	return previewScene{m: m, dir: dir, fx: fx, cmd: cmd}
}

// answered runs the preview Cmd and feeds its answer to the model.
func (s previewScene) answered(t *testing.T) previewScene {
	t.Helper()
	msg, ok := run(t, s.cmd).(ChangePreviewMsg)
	if !ok {
		t.Fatalf("the Cmd enter returned did not answer with a ChangePreviewMsg")
	}
	s.msg = msg
	s.m = feed(t, s.m, msg)
	return s
}

func ds0002(t *testing.T) changeFixture { return fixtureFor(t, "AVD-DS-0002") }

// alreadyFixed is a Dockerfile the rule declines: it is already fixed.
func alreadyFixed() changeFixture {
	return dockerfileFixture("AVD-DS-0029", "FROM debian:12\nRUN apt-get install --no-install-recommends -y curl\n", 2, 2)
}

func readyScene(t *testing.T) previewScene {
	return openPreview(t, ds0002(t), -time.Hour).answered(t)
}

// expiredScene is a file written an hour after the scan ended.
func expiredScene(t *testing.T) previewScene {
	return openPreview(t, ds0002(t), time.Hour).answered(t)
}

func declinedScene(t *testing.T) previewScene {
	return openPreview(t, alreadyFixed(), -time.Hour).answered(t)
}

// failedScene is a file that vanished between the open and the computation.
func failedScene(t *testing.T) previewScene {
	t.Helper()
	s := openPreview(t, ds0002(t), -time.Hour)
	if err := os.Remove(filepath.Join(s.dir, "Dockerfile")); err != nil {
		t.Fatal(err)
	}
	return s.answered(t)
}

// requireReadable skips a test that needs a file the process cannot read.
func requireUnreadableFiles(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file permissions are not enforced for this user")
	}
}

// previewScenes are the five states a stored preview can be in.
func previewScenes(t *testing.T) map[string]previewScene {
	t.Helper()
	return map[string]previewScene{
		"pending":  openPreview(t, ds0002(t), -time.Hour),
		"ready":    readyScene(t),
		"expired":  expiredScene(t),
		"declined": declinedScene(t),
		"failed":   failedScene(t),
	}
}

// ── Step 3: the Cmd, and who starts it ───────────────────────────────────────

func TestOpeningTheDetailOfACatalogMisconfigurationStartsThePreview(t *testing.T) {
	fx := ds0002(t)
	s := openPreview(t, fx, -time.Hour)
	if s.cmd == nil {
		t.Fatal("enter returned no Cmd for a rule the catalog fixes, on a directory")
	}
	if s.m.changePreview.State != previewPending {
		t.Errorf("state = %v before the answer, want pending", s.m.changePreview.State)
	}

	s = s.answered(t)
	if s.msg.Target != s.dir || s.msg.Prepared.Err != nil || len(s.msg.Prepared.Lines) == 0 {
		t.Fatalf("answer = %+v, want the change of %s on %s", s.msg, fx.id, s.dir)
	}
	if s.msg.Seq != s.m.changePreview.Seq {
		t.Errorf("the answer is numbered %d, the stored request %d", s.msg.Seq, s.m.changePreview.Seq)
	}
	want := computeMisconfigFix(s.dir, fx.finding, scannedAtFixed)
	if !slices.Equal(s.msg.Prepared.Lines, want.Lines) {
		t.Errorf("the preview is not the shared computation:\n got: %q\nwant: %q", s.msg.Prepared.Lines, want.Lines)
	}
	if s.m.changePreview.State != previewReady {
		t.Errorf("state = %v after the answer, want ready", s.m.changePreview.State)
	}
}

// What starts nothing: a vulnerability, an image, a refusal the finding alone
// settles, a rule the catalog does not fix.
func TestTheDetailStartsNoPreviewWhereNoChangeCanBeWritten(t *testing.T) {
	tests := []struct {
		name   string
		f      scan.Finding
		target string
		kind   scan.TargetType
	}{
		{"a CVE", findingFixtures()[0], detailsTarget, scan.TargetDirectory},
		{"a secret", findingFixtures()[2], detailsTarget, scan.TargetDirectory},
		{"a licence", findingFixtures()[3], detailsTarget, scan.TargetDirectory},
		{"a catalog rule on an image", misconfigOf("AVD-DS-0002"), "app:1.0", scan.TargetImage},
		{"a finding the rule declines before reading", unconfinedSeccomp(), detailsTarget, scan.TargetDirectory},
		{"a rule outside the catalog", misconfigOf("KSV-0003"), detailsTarget, scan.TargetDirectory},
		{"a render failure", renderFailureFixture(), detailsTarget, scan.TargetDirectory},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cmd := openOn(t, tt.f, tt.target, tt.kind, 120, 28)
			if cmd != nil {
				t.Errorf("enter started a preview for %s", tt.f.ID)
			}
		})
	}
}

// openOn is openDetailOn that keeps the Cmd enter returns.
func openOn(t *testing.T, f scan.Finding, target string, kind scan.TargetType, width, height int) (Model, tea.Cmd) {
	t.Helper()
	result := &scan.Result{Target: target, TargetType: kind, Findings: []scan.Finding{f}}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), testutil.Resize(width, height))
	m.switchTab(tabOf(t, f))
	m, cmd := step(t, m, testutil.Key("enter"))
	if m.state != StateDetails || m.selectedFinding == nil {
		t.Fatalf("enter on %s did not open its details", f.ID)
	}
	return m, cmd
}

func TestAnOldAnswerIsIgnored(t *testing.T) {
	fx := ds0002(t)
	s := openPreview(t, fx, -time.Hour)
	old, ok := run(t, s.cmd).(ChangePreviewMsg)
	if !ok {
		t.Fatal("no ChangePreviewMsg")
	}

	// Leave the detail and open it again: a new request, a new number.
	m := feed(t, s.m, testutil.Key("esc"))
	m, cmd := step(t, m, testutil.Key("enter"))
	if m.state != StateDetails || cmd == nil {
		t.Fatalf("reopening the detail: state %v, Cmd %v", m.state, cmd != nil)
	}
	current, ok := run(t, cmd).(ChangePreviewMsg)
	if !ok {
		t.Fatal("no ChangePreviewMsg on the second open")
	}
	if current.Seq == old.Seq {
		t.Fatalf("two requests carry the number %d", old.Seq)
	}

	m = feed(t, m, old)
	if m.changePreview.State != previewPending {
		t.Errorf("the old answer was taken: state %v", m.changePreview.State)
	}
	if block := changeBlock(m); len(block) != 1 || block[0] != "Change: preparing the preview..." {
		t.Errorf("the section reads %q after an old answer, want it still preparing", block)
	}

	m = feed(t, m, current)
	if m.changePreview.State != previewReady {
		t.Errorf("the current answer was not taken: state %v", m.changePreview.State)
	}
}

func TestAnAnswerAfterEscapeChangesNothing(t *testing.T) {
	s := openPreview(t, ds0002(t), -time.Hour)
	msg, ok := run(t, s.cmd).(ChangePreviewMsg)
	if !ok {
		t.Fatal("no ChangePreviewMsg")
	}
	m := feed(t, s.m, testutil.Key("esc"))
	if m.state != StateResults {
		t.Fatalf("esc left the state at %v", m.state)
	}
	m, cmd := step(t, m, msg)
	if m.state != StateResults {
		t.Errorf("a late answer moved the view to state %v", m.state)
	}
	if m.changePreview.State == previewReady {
		t.Error("a late answer was stored for a detail that is closed")
	}
	if m.footer.Text() != "" {
		t.Errorf("a late answer set the footer: %q", m.footer.Text())
	}
	if cmd != nil {
		t.Error("a late answer returned a Cmd")
	}
}

func TestAnAnswerForAnotherTargetOrAnotherFindingIsIgnored(t *testing.T) {
	t.Run("another target", func(t *testing.T) {
		s := openPreview(t, ds0002(t), -time.Hour)
		msg, _ := run(t, s.cmd).(ChangePreviewMsg)
		msg.Target = "/somewhere/else"
		m := feed(t, s.m, msg)
		if m.changePreview.State != previewPending {
			t.Errorf("an answer about another target was taken: state %v", m.changePreview.State)
		}
	})
	t.Run("another finding", func(t *testing.T) {
		s := openPreview(t, ds0002(t), -time.Hour)
		msg, _ := run(t, s.cmd).(ChangePreviewMsg)
		// The number and the target are the current ones; the finding on screen is
		// no longer the one the request was about.
		other := fixtureFor(t, "KSV-0017").finding
		s.m.selectedFinding = &other
		m := feed(t, s.m, msg)
		if m.changePreview.State == previewReady {
			t.Error("an answer about another finding was taken")
		}
	})
}

func TestAWindowResizeStartsNoPreview(t *testing.T) {
	s := openPreview(t, ds0002(t), -time.Hour).answered(t)
	m, cmd := step(t, s.m, testutil.Resize(100, 24))
	if cmd != nil {
		t.Error("a resize returned a Cmd")
	}
	if m.changePreview.State != previewReady {
		t.Errorf("a resize reset the preview to state %v", m.changePreview.State)
	}
}

// The file just changed, so the verdict puts the preview back to "preparing" and
// asks again: no old diff, no greyed key survives the re-scan.
func TestAVerdictWithAResultPutsThePreviewBackToPendingAndAsksAgain(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	for name, s := range map[string]previewScene{"ready": readyScene(t), "expired": expiredScene(t)} {
		t.Run(name, func(t *testing.T) {
			f := *s.m.selectedFinding
			rescanned := &scan.Result{
				Target: s.dir, TargetType: scan.TargetDirectory, Findings: []scan.Finding{f},
				StartTime: scannedAtFixed.Add(2*time.Hour - time.Minute), EndTime: scannedAtFixed.Add(2 * time.Hour),
			}
			m, cmd := step(t, s.m, MisconfigVerifiedMsg{
				Target: s.dir, Rule: f.ID, File: f.File, Cleared: false, Still: []string{f.ID}, Result: rescanned,
			})
			if m.state != StateDetails {
				t.Fatalf("the detail closed (state %v)", m.state)
			}
			if m.changePreview.State != previewPending {
				t.Errorf("state = %v after the verdict, want pending", m.changePreview.State)
			}
			if block := changeBlock(m); len(block) != 1 || block[0] != "Change: preparing the preview..." {
				t.Errorf("the section reads %q after the verdict, want it preparing again", block)
			}
			if !testutil.ShortcutEnabled(m.GetShortcuts(), writeRemediationKey) {
				t.Error("ctrl+o stayed greyed after the re-scan")
			}
			if strings.Contains(ansiPlain(m), reasonPreviewStale) {
				t.Error("the stale reason survived the re-scan")
			}
			next, ok := testutil.MsgOf[ChangePreviewMsg](cmd)
			if !ok {
				t.Fatal("the verdict started no new preview")
			}
			// The file is older than the end of the new scan: this time it is ready.
			m = feed(t, m, next)
			if m.changePreview.State != previewReady {
				t.Errorf("state = %v after the new answer, want ready (Err %v)", m.changePreview.State, next.Prepared.Err)
			}
		})
	}
}

// ── Failures (step 5) ────────────────────────────────────────────────────────

func TestAFailedPreviewSaysSoInTheFooterAndTheSectionAndLeavesTheDetailUsable(t *testing.T) {
	s := failedScene(t)
	m := s.m

	if m.changePreview.State != previewFailed {
		t.Fatalf("state = %v, want failed", m.changePreview.State)
	}
	if m.footer.Text() != "Could not preview the fix — check logs" || m.footer.Level() != components.LevelError {
		t.Errorf("footer = %q (level %v), want the error that points at the logs", m.footer.Text(), m.footer.Level())
	}
	want := []string{"Change: preview failed: Dockerfile no longer exists"}
	if block := changeBlock(m); !slices.Equal(block, want) {
		t.Errorf("the section reads %q, want %q", block, want)
	}
	if strings.Contains(m.footer.Text(), s.dir) {
		t.Error("the footer shows a path")
	}

	// The detail is still a detail.
	if m.state != StateDetails {
		t.Fatalf("state = %v", m.state)
	}
	m2, cmd := step(t, m, testutil.Key("enter"))
	if open, ok := runMsgOf[uiviewer.OpenRequestMsg](cmd); !ok || m2.state != StateDetails {
		t.Errorf("enter did not ask for the viewer (%v, %+v)", ok, open)
	}
	m2 = feed(t, m, testutil.Key(keymap.Web))
	if m2.footer.Text() != "No references available" {
		t.Errorf("W did not reach its handler: footer %q", m2.footer.Text())
	}
	before := m.detailsViewport.YOffset()
	m2 = feed(t, m, testutil.Key("down"))
	if m2.state != StateDetails || m2.detailsViewport.YOffset() < before {
		t.Error("scrolling stopped working")
	}
}

func runMsgOf[T tea.Msg](cmd tea.Cmd) (T, bool) {
	var zero T
	if cmd == nil {
		return zero, false
	}
	v, ok := cmd().(T)
	return v, ok
}

func TestAnUnreadableFileIsWordedAndNamed(t *testing.T) {
	requireUnreadableFiles(t)
	s := openPreview(t, ds0002(t), -time.Hour)
	if err := os.Chmod(filepath.Join(s.dir, "Dockerfile"), 0); err != nil {
		t.Fatal(err)
	}
	s = s.answered(t)
	want := []string{"Change: preview failed: Dockerfile cannot be read"}
	if block := changeBlock(s.m); !slices.Equal(block, want) {
		t.Errorf("the section reads %q, want %q", block, want)
	}
	if s.m.footer.Text() != "Could not preview the fix — check logs" {
		t.Errorf("footer = %q", s.m.footer.Text())
	}
}

// H7: a failed preview leaves ctrl+o lit, because not knowing is not a no; the
// press recomputes and reports its own error, and writes nothing.
func TestCtrlOAfterAFailedPreviewIsLitAndReportsItsOwnError(t *testing.T) {
	s := failedScene(t)
	m := s.m
	if !testutil.ShortcutEnabled(m.GetShortcuts(), writeRemediationKey) {
		t.Fatal("ctrl+o is greyed after a failed preview")
	}
	m, cmd := step(t, m, testutil.Key(writeRemediationKey))
	if cmd == nil {
		t.Fatalf("ctrl+o started nothing; footer %q", m.footer.Text())
	}
	prepared, ok := run(t, cmd).(MisconfigFixPreparedMsg)
	if !ok || prepared.Err == nil {
		t.Fatalf("answer = %+v, want its own error", prepared)
	}
	m = feed(t, m, prepared)
	if m.confirmModal != nil {
		t.Error("a confirmation opened on a file that is gone")
	}
	if m.footer.Level() != components.LevelError || !strings.Contains(m.footer.Text(), "Could not prepare the fix") {
		t.Errorf("footer = %q (level %v), want the preparation's own error", m.footer.Text(), m.footer.Level())
	}
}

// A link that leaves the target is refused with its reason; nothing outside is
// read or written.
func TestALinkThatLeavesTheTargetIsRefusedByThePreviewWithItsReason(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0021")
	target, outside := outsideTree(t)
	symlink(t, filepath.Join(outside, "Dockerfile"), filepath.Join(target, "Dockerfile"))
	before := readTree(t, outside, "Dockerfile")

	m, cmd := step(t, changeModel(t, target, fx.finding, scannedAtFixed), testutil.Key("enter"))
	msg, ok := run(t, cmd).(ChangePreviewMsg)
	if !ok {
		t.Fatal("no preview was computed")
	}
	if len(msg.Prepared.File.Original) != 0 {
		t.Error("the file behind the link was read")
	}
	m = feed(t, m, msg)

	if m.changePreview.State != previewDeclined {
		t.Errorf("state = %v, want declined", m.changePreview.State)
	}
	if a := m.canFixMisconfig(); a.Enabled() || a.Reason != reasonFileLinkOutsideTarget {
		t.Errorf("canFixMisconfig = %+v, want %q", a, reasonFileLinkOutsideTarget)
	}
	if got := m.fixStatus(fx.finding).Label(); got != labelBuiltInUnavailable+": "+reasonFileLinkOutsideTarget {
		t.Errorf("the Fix line reads %q", got)
	}
	if block := changeBlock(m); !slices.Equal(block, []string{"Change: none: DevDesk cannot write this fix"}) {
		t.Errorf("the section reads %q", block)
	}
	if got := readTree(t, outside, "Dockerfile"); got != before {
		t.Errorf("the file outside the target changed:\n%s", got)
	}
}

// ── Step 4: the stored verdict ───────────────────────────────────────────────

// Rule 130: the column of keys is the same in every state of the preview; what
// changes is Disabled, and only ctrl+o moves.
func TestTheShortcutColumnIsTheSameInEveryPreviewState(t *testing.T) {
	scenes := previewScenes(t)
	wantKeys := testutil.ShortcutKeys(scenes["pending"].m.GetShortcuts())
	enabled := map[string]bool{"pending": true, "ready": true, "failed": true, "expired": false, "declined": false}
	for name, s := range scenes {
		t.Run(name, func(t *testing.T) {
			got := s.m.GetShortcuts()
			if keys := testutil.ShortcutKeys(got); !slices.Equal(keys, wantKeys) {
				t.Errorf("keys = %v, want the pending state's %v", keys, wantKeys)
			}
			if !testutil.HasShortcut(got, writeRemediationKey) {
				t.Fatal("ctrl+o is not advertised")
			}
			if testutil.ShortcutEnabled(got, writeRemediationKey) != enabled[name] {
				t.Errorf("ctrl+o enabled = %v, want %v", !enabled[name], enabled[name])
			}
			// The others do not depend on the preview.
			for _, key := range []string{"esc", "enter", keymap.Web, keymap.Copy, keymap.Exclude} {
				pending := testutil.ShortcutDisabled(scenes["pending"].m.GetShortcuts(), key)
				if testutil.ShortcutDisabled(got, key) != pending {
					t.Errorf("%s changed with the preview state", key)
				}
			}
		})
	}
}

// Rule 130, "one computation": a stale preview greys ctrl+o with the stale
// reason, and the press says it in the footer, as a warning, computing nothing.
func TestAStalePreviewGreysCtrlOWithItsReason(t *testing.T) {
	s := expiredScene(t)
	if s.m.changePreview.State != previewExpired {
		t.Fatalf("state = %v, want expired (Err %v)", s.m.changePreview.State, s.msg.Prepared.Err)
	}
	if a := s.m.canFixMisconfig(); a.Enabled() || a.Reason != reasonPreviewStale {
		t.Errorf("canFixMisconfig = %+v, want %q", a, reasonPreviewStale)
	}
	if a := s.m.previewVerdict(*s.m.selectedFinding); a.Enabled() || a.Reason != reasonPreviewStale {
		t.Errorf("previewVerdict = %+v, want %q", a, reasonPreviewStale)
	}
	m, cmd := step(t, s.m, testutil.Key(writeRemediationKey))
	if m.footer.Text() != reasonPreviewStale || m.footer.Level() != components.LevelWarning {
		t.Errorf("footer = %q (level %v), want %q as a warning", m.footer.Text(), m.footer.Level(), reasonPreviewStale)
	}
	if m.confirmModal != nil {
		t.Error("a confirmation opened")
	}
	if _, computed := testutil.MsgOf[MisconfigFixPreparedMsg](cmd); computed {
		t.Error("the press computed the fix again instead of reading the stored verdict")
	}
}

// The verdict is the stored one: it reads no disk. Remove the directory and the
// answers do not change.
func TestTheShortcutsAndTheFixLineReadNoDisk(t *testing.T) {
	for _, name := range []string{"pending", "ready", "expired", "declined", "failed"} {
		t.Run(name, func(t *testing.T) {
			s := previewScenes(t)[name]
			f := *s.m.selectedFinding
			keys, availability, status := s.m.GetShortcuts(), s.m.canFixMisconfig(), s.m.fixStatus(f)
			content := ansiPlain(s.m)

			if err := os.RemoveAll(s.dir); err != nil {
				t.Fatal(err)
			}

			if got := s.m.GetShortcuts(); !slices.Equal(got, keys) {
				t.Errorf("GetShortcuts changed after the directory was removed:\n before: %+v\n after:  %+v", keys, got)
			}
			if got := s.m.canFixMisconfig(); got != availability {
				t.Errorf("canFixMisconfig = %+v after the removal, was %+v", got, availability)
			}
			if got := s.m.fixStatus(f); got != status {
				t.Errorf("fixStatus = %+v after the removal, was %+v", got, status)
			}
			if got := ansiPlain(s.m); got != content {
				t.Errorf("the detail changed after the directory was removed")
			}
		})
	}
}

// The Fix line says "Built-in fix unavailable: R" exactly when ctrl+o is greyed
// with R, on a stored preview: the extension of
// TestFixStatusAndTheShortcutAgree and
// TestTheFooterAfterCtrlOSaysTheReasonOfTheFixLine.
func TestFixStatusAndTheShortcutAgreeOnAStoredPreview(t *testing.T) {
	want := map[string]string{
		"expired":  reasonPreviewStale,
		"declined": remediation.ReasonAlreadyFixed,
	}
	for name, s := range previewScenes(t) {
		t.Run(name, func(t *testing.T) {
			f := *s.m.selectedFinding
			a, status := s.m.canFixMisconfig(), s.m.fixStatus(f)
			if a.Enabled() != (status.State == fixBuiltIn) {
				t.Fatalf("ctrl+o enabled = %v, Fix line = %q", a.Enabled(), status.Label())
			}
			if a.Enabled() {
				if status.Label() != labelBuiltInAvailable {
					t.Errorf("the Fix line reads %q while ctrl+o is lit", status.Label())
				}
				return
			}
			reason := want[name]
			if a.Reason != reason {
				t.Errorf("ctrl+o is greyed with %q, want %q", a.Reason, reason)
			}
			if status.State != fixBuiltInDeclined || status.Reason != a.Reason {
				t.Errorf("the status is %+v, want Built-in fix unavailable with %q", status, a.Reason)
			}
			if got, want := status.Label(), "Built-in fix unavailable: "+a.Reason; got != want {
				t.Errorf("the Fix line reads %q, want %q", got, want)
			}
			if content := ansiPlain(s.m); !strings.Contains(content, "Built-in fix unavailable") {
				t.Errorf("the detail does not show the Fix line:\n%s", content)
			}
			m := feed(t, s.m, testutil.Key(writeRemediationKey))
			if got := footerText(m, 400); !strings.Contains(got, a.Reason) {
				t.Errorf("the footer after ctrl+o does not say %q:\n%s", a.Reason, got)
			}
		})
	}
}

// The stored preview belongs to the finding on screen: another finding is read
// from the catalog and the finding alone, never from a verdict that is not its.
func TestAStoredVerdictIsOnlyForTheFindingItWasComputedFor(t *testing.T) {
	s := expiredScene(t)
	other := fixtureFor(t, "KSV-0017").finding
	if a := s.m.previewVerdict(other); !a.Enabled() {
		t.Errorf("the verdict of another finding is %+v, want it enabled", a)
	}
	if got := s.m.fixStatus(other).State; got == fixBuiltInDeclined && s.m.fixStatus(other).Reason == reasonPreviewStale {
		t.Error("the stale reason was applied to another finding")
	}
}

// ── The footer says "preparing" while it waits ───────────────────────────────

func TestTheFooterSaysItIsPreparingThePreviewUntilItAnswers(t *testing.T) {
	s := openPreview(t, ds0002(t), -time.Hour)
	st := s.m.status()
	if !strings.Contains(strings.ToLower(st.Text), "preparing the change preview") || !st.Activity {
		t.Errorf("status = %+v while pending, want a \"Preparing the change preview...\" text with Activity", st)
	}
	if got := footerText(s.m, 160); !strings.Contains(got, "Preparing the change preview") {
		t.Errorf("the footer does not say it is preparing:\n%s", got)
	}
	if !s.m.WantsBlink() {
		t.Error("WantsBlink() is false while the footer draws the activity dot: it would stay fixed")
	}

	s = s.answered(t)
	if strings.Contains(strings.ToLower(s.m.status().Text), "preparing") || s.m.status().Activity || s.m.WantsBlink() {
		t.Errorf("status = %+v after the answer, want the announcement gone", s.m.status())
	}
}
