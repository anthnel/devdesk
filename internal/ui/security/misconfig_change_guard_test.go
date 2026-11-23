package security

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-753, step 2 (AC3 half, security): the shared computation refuses a file
// written after the scan and a path that leaves the scanned directory, whichever
// key started it (the Misconfigurations tab or the detail).
//
// "After the scan" means after Result.EndTime, as the story's AC3 says: a file
// written while the scan was running (between StartTime and EndTime) is not
// stale. A result with no EndTime cannot be judged and is never stale.
//
// Modification times are set with os.Chtimes on files under t.TempDir(); no test
// reads the clock.

func fixtureFor(t *testing.T, id string) changeFixture {
	t.Helper()
	for _, fx := range changeFixtures(t) {
		if fx.id == id {
			return fx
		}
	}
	t.Fatalf("no change fixture for %s", id)
	return changeFixture{}
}

// declinedWith asserts the message is a decline with that reason and nothing else.
func declinedWith(t *testing.T, msg MisconfigFixPreparedMsg, reason string) {
	t.Helper()
	var d errDeclined
	if !errors.As(msg.Err, &d) || d.reason != reason {
		t.Fatalf("Err = %v, want a decline: %q", msg.Err, reason)
	}
	if len(msg.Lines) != 0 || len(msg.File.Updated) != 0 || msg.Body != "" {
		t.Errorf("a decline carries %d lines, %d bytes and a body %q", len(msg.Lines), len(msg.File.Updated), msg.Body)
	}
}

// ── previewStale ─────────────────────────────────────────────────────────────

func TestPreviewStale(t *testing.T) {
	tests := []struct {
		name             string
		modTime, scanEnd time.Time
		want             bool
	}{
		{"no scan time: it cannot be judged", scannedAtFixed, time.Time{}, false},
		{"no modification time", time.Time{}, scannedAtFixed, false},
		{"neither is known", time.Time{}, time.Time{}, false},
		{"written before the scan ended", scannedAtFixed.Add(-time.Second), scannedAtFixed, false},
		{"written the very instant the scan ended", scannedAtFixed, scannedAtFixed, false},
		{"written a nanosecond after", scannedAtFixed.Add(time.Nanosecond), scannedAtFixed, true},
		{"written a second after", scannedAtFixed.Add(time.Second), scannedAtFixed, true},
		{"written a day after", scannedAtFixed.Add(24 * time.Hour), scannedAtFixed, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := previewStale(tt.modTime, tt.scanEnd); got != tt.want {
				t.Errorf("previewStale(%v, %v) = %v, want %v", tt.modTime, tt.scanEnd, got, tt.want)
			}
		})
	}
}

// ── A file written after the scan ────────────────────────────────────────────

func TestAFileWrittenAfterTheScanDeclinesTheFix(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0021")
	tests := []struct {
		name  string
		mtime time.Time
		stale bool
	}{
		{"a second after the end of the scan", scannedAtFixed.Add(time.Second), true},
		{"an hour after the end of the scan", scannedAtFixed.Add(time.Hour), true},
		{"the instant the scan ended", scannedAtFixed, false},
		{"before the scan ended", scannedAtFixed.Add(-time.Second), false},
		// The orchestrator's decision, against the plan's first hypothesis: the
		// threshold is EndTime, not StartTime. A write during the scan is not
		// "after" it.
		{"during the scan, after its start", scannedAtFixed.Add(-30 * time.Second), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, fx.files, tt.mtime)
			msg := computeMisconfigFix(dir, fx.finding, scannedAtFixed)
			if tt.stale {
				declinedWith(t, msg, reasonPreviewStale)
				return
			}
			if msg.Err != nil || len(msg.Lines) == 0 {
				t.Fatalf("Err %v, %d lines: a file not written after the scan was refused", msg.Err, len(msg.Lines))
			}
		})
	}
}

// A result with no end time cannot say what "after" is: the file is previewed, as
// it was before this check existed.
func TestAResultWithNoEndTimeNeverMakesAFileStale(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0021")
	dir := t.TempDir()
	writeTree(t, dir, fx.files, scannedAtFixed.Add(100*24*time.Hour))
	msg := computeMisconfigFix(dir, fx.finding, time.Time{})
	if msg.Err != nil || len(msg.Lines) == 0 {
		t.Errorf("Err %v, %d lines", msg.Err, len(msg.Lines))
	}
}

// The build context fix edits the .dockerignore for a finding about the
// Dockerfile: either file written after the scan makes the preview stale.
func TestBothTheEditedFileAndTheFindingsFileAreCheckedForStaleness(t *testing.T) {
	fx := fixtureFor(t, "DEVDESK-CTX-001")
	before, after := scannedAtFixed.Add(-time.Hour), scannedAtFixed.Add(time.Hour)
	tests := []struct {
		name               string
		dockerfile, ignore time.Time
		stale              bool
	}{
		{"both before the scan", before, before, false},
		{"the Dockerfile after", after, before, true},
		{"the .dockerignore after", before, after, true},
		{"both after", after, after, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, fx.files, before)
			touch(t, filepath.Join(dir, "Dockerfile"), tt.dockerfile)
			touch(t, filepath.Join(dir, ".dockerignore"), tt.ignore)
			msg := computeMisconfigFix(dir, fx.finding, scannedAtFixed)
			if tt.stale {
				declinedWith(t, msg, reasonPreviewStale)
			} else if msg.Err != nil {
				t.Errorf("Err = %v", msg.Err)
			}
		})
	}
}

// H3: the refusal is in the shared computation, so the key refuses from the
// Misconfigurations tab and from the detail alike: a warning with the reason, no
// confirmation, and the file is not written.
func TestCtrlOOnAFileWrittenAfterTheScanWarnsAndWritesNothing(t *testing.T) {
	for _, from := range []string{"the Misconfigurations tab", "the detail"} {
		t.Run(from, func(t *testing.T) {
			fx := fixtureFor(t, "AVD-DS-0002")
			dir := t.TempDir()
			writeTree(t, dir, fx.files, scannedAtFixed.Add(time.Minute))
			m := changeModel(t, dir, fx.finding, scannedAtFixed)
			if from == "the detail" {
				m = feed(t, m, testutil.Key("enter"))
			}

			m, msg := previewed(t, m)

			declinedWith(t, msg, reasonPreviewStale)
			if m.confirmModal != nil {
				t.Fatal("a confirmation opened on a file the preview was not computed from")
			}
			if m.footer.Text() != reasonPreviewStale || m.footer.Level() != sharedcomponents.LevelWarning {
				t.Errorf("footer = %q (level %v), want %q as a warning", m.footer.Text(), m.footer.Level(), reasonPreviewStale)
			}
			if got := readTree(t, dir, "Dockerfile"); got != fx.files["Dockerfile"] {
				t.Errorf("the file was written:\n%s", got)
			}
		})
	}
}

// The same key on a file the scan did measure still opens the confirmation: the
// check refuses a stale file, not every file of a result that has an EndTime.
func TestCtrlOOnAFileWrittenBeforeTheScanEndedStillOpensTheConfirmation(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0002")
	dir := t.TempDir()
	writeTree(t, dir, fx.files, scannedAtFixed.Add(-time.Minute))
	m, msg := previewed(t, changeModel(t, dir, fx.finding, scannedAtFixed))
	if msg.Err != nil || m.confirmModal == nil {
		t.Fatalf("Err %v, modal %v; footer %q", msg.Err, m.confirmModal != nil, m.footer.Text())
	}
}

// A file that is gone is a failure, not a stale preview and not a decline: the
// error keeps fs.ErrNotExist for the wording of the preview.
func TestAMissingFileIsAnErrorNotADecline(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0002")
	msg := computeMisconfigFix(t.TempDir(), fx.finding, scannedAtFixed)
	var d errDeclined
	if errors.As(msg.Err, &d) {
		t.Fatalf("declined with %q, want an error", d.reason)
	}
	if !errors.Is(msg.Err, fs.ErrNotExist) {
		t.Errorf("Err = %v, want it to wrap fs.ErrNotExist", msg.Err)
	}
}

// ── Paths that leave the target ──────────────────────────────────────────────

// outsideTree is a scanned directory and, next to it, a directory holding a
// Dockerfile the fix would happily rewrite.
func outsideTree(t *testing.T) (target, outside string) {
	t.Helper()
	base := t.TempDir()
	target, outside = filepath.Join(base, "target"), filepath.Join(base, "outside")
	for _, d := range []string{target, outside} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTree(t, outside, map[string]string{"Dockerfile": fixtureFor(t, "AVD-DS-0021").files["Dockerfile"]}, scannedAtFixed.Add(-time.Hour))
	return target, outside
}

func symlink(t *testing.T, old, name string) {
	t.Helper()
	if err := os.Symlink(old, name); err != nil {
		t.Skipf("symbolic links are not available: %v", err)
	}
}

func TestAPathOutsideTheTargetIsRefusedBeforeAnythingIsRead(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0021")
	target, outside := outsideTree(t)
	before := readTree(t, outside, "Dockerfile")
	for name, file := range map[string]string{
		"a parent segment": "../outside/Dockerfile",
		"an absolute path": filepath.Join(outside, "Dockerfile"),
	} {
		t.Run(name, func(t *testing.T) {
			f := fx.finding
			f.File = file
			msg := computeMisconfigFix(target, f, scannedAtFixed)
			declinedWith(t, msg, reasonFileOutsideTarget)
			if len(msg.File.Original) != 0 {
				t.Error("the file outside the target was read")
			}
		})
	}
	if got := readTree(t, outside, "Dockerfile"); got != before {
		t.Errorf("the file outside the target changed:\n%s", got)
	}
}

// Trivy run in a container reports files under its mount point; the shared path
// resolution (findingFilePath) maps it back onto the target.
func TestAFileUnderTheScanMountIsFoundInTheTarget(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0021")
	dir := fixtureOnDisk(t, fx)
	f := fx.finding
	f.File = "/scan/Dockerfile"
	msg := computeMisconfigFix(dir, f, scannedAtFixed)
	if msg.Err != nil {
		t.Fatalf("Err = %v", msg.Err)
	}
	if msg.File.Path != filepath.Join(dir, "Dockerfile") || len(msg.File.Updated) == 0 {
		t.Errorf("Path = %q, %d bytes; want the Dockerfile of the target", msg.File.Path, len(msg.File.Updated))
	}
}

// A link that leaves the target is refused: nothing outside is read, and a write
// through it is never prepared. The lexical check passes (the name is inside);
// it is the link check that stops it.
func TestALinkThatLeavesTheTargetIsRefusedAndNothingOutsideIsTouched(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0021")
	tests := []struct {
		name string
		link func(t *testing.T, target, outside string)
		file string
	}{
		{"the file is a link", func(t *testing.T, target, outside string) {
			symlink(t, filepath.Join(outside, "Dockerfile"), filepath.Join(target, "Dockerfile"))
		}, "Dockerfile"},
		{"a directory on the way is a link", func(t *testing.T, target, outside string) {
			symlink(t, outside, filepath.Join(target, "linked"))
		}, "linked/Dockerfile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, outside := outsideTree(t)
			tt.link(t, target, outside)
			before := readTree(t, outside, "Dockerfile")
			f := fx.finding
			f.File = tt.file

			msg := computeMisconfigFix(target, f, scannedAtFixed)
			declinedWith(t, msg, reasonFileLinkOutsideTarget)
			if len(msg.File.Original) != 0 {
				t.Error("the file behind the link was read")
			}

			// And through the key: a warning, no confirmation, no write.
			m, _ := previewed(t, changeModel(t, target, f, scannedAtFixed))
			if m.confirmModal != nil {
				t.Error("a confirmation opened for a file outside the target")
			}
			if m.footer.Text() != reasonFileLinkOutsideTarget || m.footer.Level() != sharedcomponents.LevelWarning {
				t.Errorf("footer = %q (level %v), want %q as a warning", m.footer.Text(), m.footer.Level(), reasonFileLinkOutsideTarget)
			}
			if got := readTree(t, outside, "Dockerfile"); got != before {
				t.Errorf("the file outside the target changed:\n%s", got)
			}
		})
	}
}

// The link check comes before anything is measured: a stale file reached through
// a link that leaves the target answers with the link, not with its age.
func TestTheLinkIsRefusedBeforeTheFileIsMeasured(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0021")
	target, outside := outsideTree(t)
	touch(t, filepath.Join(outside, "Dockerfile"), scannedAtFixed.Add(time.Hour))
	symlink(t, filepath.Join(outside, "Dockerfile"), filepath.Join(target, "Dockerfile"))
	declinedWith(t, computeMisconfigFix(target, fx.finding, scannedAtFixed), reasonFileLinkOutsideTarget)
}

// A link that stays inside the target is an ordinary file: the preview is
// computed from what it points at, and the write replaces what it points at,
// leaving the link a link.
func TestALinkThatStaysInsideTheTargetStillWorks(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0021")
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"real/Dockerfile.prod": fx.files["Dockerfile"]}, scannedAtFixed.Add(-time.Hour))
	symlink(t, filepath.Join("real", "Dockerfile.prod"), filepath.Join(dir, "Dockerfile"))

	m := changeModel(t, dir, fx.finding, scannedAtFixed)
	m, msg := previewed(t, m)
	if msg.Err != nil || m.confirmModal == nil {
		t.Fatalf("Err %v, modal %v; footer %q", msg.Err, m.confirmModal != nil, m.footer.Text())
	}
	m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})
	written, ok := run(t, cmd).(MisconfigFixWrittenMsg)
	if !ok || written.Err != nil {
		t.Fatalf("the write failed: %+v", written)
	}
	_ = m
	if got := readTree(t, dir, "real/Dockerfile.prod"); !bytes.Equal([]byte(got), msg.File.Updated) {
		t.Errorf("the target of the link is not Updated:\n%s", got)
	}
	if info, err := os.Lstat(filepath.Join(dir, "Dockerfile")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced by a regular file (%v, %v)", info, err)
	}
}
