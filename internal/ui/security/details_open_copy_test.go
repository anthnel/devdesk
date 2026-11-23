package security

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/clipboard"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
	"github.com/anthnel/devdesk/internal/viewer"
)

// DEVDESK-751 (step 6): enter opens the finding's file in the viewer, and Y puts
// the command under Run on the clipboard. Neither reaches the machine: the
// viewer is a request to the router, and the clipboard is the writeClipboard
// seam, which TestMain replaces and these tests take over.

// stubClipboard takes the clipboard write for the test, and reports what was
// written. err is what the write answers.
func stubClipboard(t *testing.T, err error) *[]string {
	t.Helper()
	orig := writeClipboard
	written := new([]string)
	writeClipboard = func(text string) error {
		*written = append(*written, text)
		return err
	}
	t.Cleanup(func() { writeClipboard = orig })
	return written
}

func TestTheClipboardSeamIsNotTheRealClipboardInTests(t *testing.T) {
	if reflect.ValueOf(writeClipboard).Pointer() == reflect.ValueOf(clipboard.WriteAll).Pointer() {
		t.Fatal("writeClipboard is clipboard.WriteAll: TestMain must replace it, or a test writes to the machine's clipboard")
	}
}

// ── enter opens the file ─────────────────────────────────────────────────────

func TestEnterOpensTheFindingFileInTheViewer(t *testing.T) {
	tests := []struct {
		name string
		file string
		want func(dir string) string
	}{
		{"a file at the root", "Dockerfile", func(d string) string { return filepath.Join(d, "Dockerfile") }},
		{"a file in a directory", "config/prod.env", func(d string) string { return filepath.Join(d, "config", "prod.env") }},
		{"a path under the container mount", "/scan/k8s/deploy.yaml", func(d string) string { return filepath.Join(d, "k8s", "deploy.yaml") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			m := openDetailOn(t, gitleaksSecret(tc.file, 12), dir, scan.TargetDirectory, 160, 30)

			next, cmd := step(t, m, testutil.Key("enter"))
			req, ok := testutil.MsgOf[uiviewer.OpenRequestMsg](cmd)
			if !ok {
				t.Fatal("enter produced no OpenRequestMsg")
			}
			src, ok := req.Source.(viewer.FileSource)
			if !ok {
				t.Fatalf("the source is a %T, want a viewer.FileSource", req.Source)
			}
			if got, want := src.Path(), tc.want(dir); got != want {
				t.Errorf("path = %q, want %q", got, want)
			}
			if next.state != StateDetails || next.footer.Text() != "" {
				t.Errorf("state = %v, footer = %q: opening the file changes neither", next.state, next.footer.Text())
			}
		})
	}
}

// Rule 110: the Cmd takes a copy of the path, not a view of the model.
func TestTheViewerRequestKeepsThePathItWasBuiltWith(t *testing.T) {
	dir := t.TempDir()
	m := openDetailOn(t, gitleaksSecret("config/prod.env", 12), dir, scan.TargetDirectory, 160, 30)
	_, cmd := step(t, m, testutil.Key("enter"))
	*m.selectedFinding = scan.Finding{File: "elsewhere/other.env"}

	req, ok := testutil.MsgOf[uiviewer.OpenRequestMsg](cmd)
	if !ok {
		t.Fatal("enter produced no OpenRequestMsg")
	}
	if got, want := req.Source.(viewer.FileSource).Path(), filepath.Join(dir, "config", "prod.env"); got != want {
		t.Errorf("path = %q, want %q: the Cmd read the model after Update", got, want)
	}
}

// enter does not run anything and does not read the file: a file that is not
// there is the viewer's to report.
func TestEnterOnAFileThatIsNotThereStillAsksTheViewer(t *testing.T) {
	m := openDetailOn(t, gitleaksSecret("no/such/file.env", 1), t.TempDir(), scan.TargetDirectory, 160, 30)
	if _, cmd := step(t, m, testutil.Key("enter")); cmd == nil {
		t.Error("enter on a path that may not exist produced nothing: the header and the key must not look at the disk")
	}
}

// ── Y copies the command ─────────────────────────────────────────────────────

func commandFinding(command string) scan.Finding {
	f := cveWith("gomod", command)
	f.PkgName = "golang.org/x/net"
	return f
}

func TestYCopiesExactlyTheCommandUnderRun(t *testing.T) {
	long := "go get example.com/" + strings.Repeat("deeply/nested/", 20) + "module@v1.2.3"
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{"a plain command", "go get golang.org/x/net@v0.7.0", "go get golang.org/x/net@v0.7.0"},
		{"a command with spaces and a newline around it", "  go get golang.org/x/net@v0.7.0 \n", "go get golang.org/x/net@v0.7.0"},
		{"a command wider than any screen", long, long},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.FastTimers(t, &components.FooterMsgDuration)
			written := stubClipboard(t, nil)
			m := openDetail(t, commandFinding(tc.command), 80, 24)
			if cmd, ok := runCommand(*m.selectedFinding); !ok || cmd != tc.want {
				t.Fatalf("runCommand = (%q, %v), want %q", cmd, ok, tc.want)
			}

			m, cmd := step(t, m, testutil.Key(keymap.Copy))
			if len(*written) != 0 {
				t.Fatalf("Update wrote %q to the clipboard: the write belongs in the Cmd (Rule 110)", *written)
			}
			msg, ok := testutil.MsgOf[FixCommandCopiedMsg](cmd)
			if !ok {
				t.Fatal("Y produced no FixCommandCopiedMsg")
			}
			if len(*written) != 1 || (*written)[0] != tc.want {
				t.Errorf("clipboard = %q, want exactly %q: no \"$ \", no newline, never cut", *written, tc.want)
			}
			if msg.Command != tc.want || msg.Err != nil {
				t.Errorf("FixCommandCopiedMsg = %+v, want the command and no error", msg)
			}

			m = feed(t, m, msg)
			if m.footer.Text() != "Command copied to the clipboard" || m.footer.Level() != components.LevelInfo {
				t.Errorf("footer = %q (level %v), want the Info confirmation", m.footer.Text(), m.footer.Level())
			}
		})
	}
}

// The screen cuts a command that does not fit; the clipboard never does.
func TestARunLineThatIsCutOnScreenIsCopiedWhole(t *testing.T) {
	long := "go get example.com/" + strings.Repeat("deeply/nested/", 20) + "module@v1.2.3"
	written := stubClipboard(t, nil)
	m := openDetail(t, commandFinding(long), 80, 24)

	var runLine string
	for _, line := range plainLines(m) {
		if strings.HasPrefix(strings.TrimSpace(line), "Run:") {
			runLine = line
		}
	}
	if runLine == "" {
		t.Fatal("the detail shows no Run line")
	}
	if !strings.Contains(runLine, "...") || strings.Contains(runLine, long) {
		t.Errorf("Run line = %q, want it cut with \"...\"", runLine)
	}
	// The content is padded by one cell on each side: the viewport is 78 wide.
	if w := ansi.StringWidth(runLine); w > textWidth(80)+2 {
		t.Errorf("Run line is %d cells wide, want at most %d", w, textWidth(80)+2)
	}

	_, cmd := step(t, m, testutil.Key(keymap.Copy))
	testutil.Msgs(cmd)
	if len(*written) != 1 || (*written)[0] != long {
		t.Errorf("clipboard = %q, want the whole command", *written)
	}
}

// Rule 110: the Cmd takes a copy of the command.
func TestTheCopyKeepsTheCommandItWasBuiltWith(t *testing.T) {
	written := stubClipboard(t, nil)
	m := openDetail(t, commandFinding("go get golang.org/x/net@v0.7.0"), 160, 30)
	_, cmd := step(t, m, testutil.Key(keymap.Copy))
	m.selectedFinding.FixCommand = "rm -rf /"

	testutil.Msgs(cmd)
	if len(*written) != 1 || (*written)[0] != "go get golang.org/x/net@v0.7.0" {
		t.Errorf("clipboard = %q, want the command as it was when Y was pressed", *written)
	}
}

// Copying is the whole of it: the only message is the report of the write.
func TestYProducesNothingButTheReportOfTheWrite(t *testing.T) {
	stubClipboard(t, nil)
	m := openDetail(t, commandFinding("go get golang.org/x/net@v0.7.0"), 160, 30)
	_, cmd := step(t, m, testutil.Key(keymap.Copy))
	msgs := testutil.Msgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("Y produced %d messages (%v), want exactly one", len(msgs), msgs)
	}
	if _, ok := msgs[0].(FixCommandCopiedMsg); !ok {
		t.Errorf("Y produced a %T", msgs[0])
	}
}

func TestYOnAnInstructionIsRefusedWithItsReason(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	written := stubClipboard(t, nil)
	m := openDetail(t, cveWith("", "Update golang.org/x/net to 0.7.0"), 160, 30)

	m, cmd := step(t, m, testutil.Key(keymap.Copy))
	if m.footer.Text() != reasonNoCommand || m.footer.Level() != components.LevelWarning {
		t.Errorf("footer = %q (level %v), want %q as a warning", m.footer.Text(), m.footer.Level(), reasonNoCommand)
	}
	for _, msg := range testutil.Msgs(cmd) {
		if _, ok := msg.(FixCommandCopiedMsg); ok {
			t.Error("an instruction was offered to the clipboard")
		}
	}
	if len(*written) != 0 {
		t.Errorf("clipboard = %q, want nothing written", *written)
	}
}

// ── The report of the write (Rule 128) ───────────────────────────────────────

func TestAFailedCopyIsAnErrorThatDoesNotLeakTheCause(t *testing.T) {
	stubClipboard(t, errors.New("no clipboard on this system"))
	m := openDetail(t, commandFinding("go get golang.org/x/net@v0.7.0"), 160, 30)
	m = feed(t, m, FixCommandCopiedMsg{Command: "go get golang.org/x/net@v0.7.0", Err: errors.New("no clipboard on this system")})

	if m.footer.Level() != components.LevelError {
		t.Errorf("footer level = %v, want an error", m.footer.Level())
	}
	if got, want := m.footer.Text(), "Failed to copy the command — check logs"; got != want {
		t.Errorf("footer = %q, want %q", got, want)
	}
	if strings.Contains(m.footer.Text(), "no clipboard") {
		t.Error("the footer shows err.Error() (Rule 128)")
	}
}

func TestACopyThatWorkedIsAnInfoNotAnActivity(t *testing.T) {
	m := openDetail(t, commandFinding("go get golang.org/x/net@v0.7.0"), 160, 30)
	m = feed(t, m, FixCommandCopiedMsg{Command: "go get golang.org/x/net@v0.7.0"})
	if m.footer.Text() != "Command copied to the clipboard" || m.footer.Level() != components.LevelInfo || m.status().Activity {
		t.Errorf("footer = %q (level %v), activity = %v: a copy is a notice, not a state",
			m.footer.Text(), m.footer.Level(), m.status().Activity)
	}
}
