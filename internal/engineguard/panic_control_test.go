package engineguard_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engineguard"
)

// The control lives in a file of its own: it runs this test binary again
// (os.Executable), and fakecli's own guard (F2) refuses a file that does that
// and also sets PATH. It sets no PATH and writes no script: it is not a fake
// engine, it proves the guard against a real start.

// The negative control: a test that goes on to start a real binary panics, and
// the binary is never started. The offender runs in a child process (this test
// binary, one test selected), because the panic of a test that nobody recovers
// ends the whole binary, which is the point of the guard.
const childEnv = "DEVDESK_ENGINE_GUARD_CHILD"

// TestOffendingTestStartsARealBinary is the offender. It does nothing in a
// normal run: only the control below starts it, with childEnv set to a directory
// that holds an executable named docker (an empty file: it could not run anyway,
// and the proof that nothing was started is the marker the test leaves after the
// guard, which is never written).
func TestOffendingTestStartsARealBinary(t *testing.T) {
	dir := os.Getenv(childEnv)
	if dir == "" {
		t.Skip("run by TestATestThatStartsARealBinaryPanics only")
	}
	engineguard.Check("Run", filepath.Join(dir, "docker")) // the first statement of the runner, before the start
	// Not reached: this is where the runner would start the binary.
	if err := os.WriteFile(filepath.Join(dir, "started"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestATestThatStartsARealBinaryPanics(t *testing.T) {
	dir := t.TempDir()
	engine := engineBinary(t, "docker") // skips on Windows
	if err := os.Rename(engine, filepath.Join(dir, "docker")); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(self, "-test.run=^TestOffendingTestStartsARealBinary$", "-test.count=1")
	cmd.Env = append(cleanEnv(), childEnv+"="+dir)
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("the offending test passed, want its binary to die on a panic:\n%s", out)
	}
	for _, want := range []string{"panic:", "DEVDESK-799", "network.md"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
		t.Error("the test went on past the guard: it panics before the start, never after")
	}
}

// cleanEnv is the environment of this process without the variables that change
// the mode of the guard, so the child is in the default (refusing) mode.
func cleanEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, engineguard.SurveyEnv+"=") || strings.HasPrefix(kv, engineguard.ContractEnv+"=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
