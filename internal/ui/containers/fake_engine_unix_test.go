//go:build unix

package containers

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/engine"
)

// fakeEngine is a stand-in for the engine binary: a small shell script in
// t.TempDir() that the engine seam (engine.Current().Binary) points at. No real
// docker or podman is ever reached.
//
// It is a script rather than a Go stub because the code under test launches the
// engine through os/exec today and may launch it through the docker runner
// tomorrow (OC-04, OC-05): both end at engine.Current().Binary, so a test written
// against the binary holds for either design.
//
// What it does, driven by environment variables the test sets with t.Setenv:
//   - every invocation appends its argv (space-joined) to the log;
//   - `exec <id> which bash` exits 1 when FAKE_ENGINE_NO_BASH is set, else 0;
//   - `exec ...` first waits for one line on the FIFO named by FAKE_ENGINE_BLOCK,
//     which is how a test simulates a daemon that answers slowly without sleeping;
//   - `inspect <id>` writes a warning on stderr and a JSON document on stdout.
type fakeEngine struct {
	bin string
	log string
}

const fakeEngineScript = `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_ENGINE_LOG"
case "$1" in
inspect)
	echo "WARNING: this engine version is deprecated" >&2
	echo '[{"Id":"aaaa111122223333","Name":"/web"}]'
	exit 0
	;;
exec)
	if [ -n "$FAKE_ENGINE_BLOCK" ]; then
		read -r _ < "$FAKE_ENGINE_BLOCK"
	fi
	if [ "$4" = "bash" ] && [ -n "$FAKE_ENGINE_NO_BASH" ]; then
		exit 1
	fi
	exit 0
	;;
esac
exit 0
`

// installFakeEngine points the engine seam at the script and restores it on
// cleanup. Tests that use it must not run in parallel (the seam is global).
func installFakeEngine(t *testing.T) *fakeEngine {
	t.Helper()
	dir := t.TempDir()
	f := &fakeEngine{bin: filepath.Join(dir, "engine"), log: filepath.Join(dir, "engine.log")}
	if err := os.WriteFile(f.bin, []byte(fakeEngineScript), 0o755); err != nil { //nolint:gosec // a test executable
		t.Fatal(err)
	}
	t.Setenv("FAKE_ENGINE_LOG", f.log)
	t.Setenv("FAKE_ENGINE_BLOCK", "")
	t.Setenv("FAKE_ENGINE_NO_BASH", "")

	previous := engine.Current()
	shape := engine.ShapeFor("docker")
	shape.Binary = f.bin
	engine.SetCurrent(shape)
	t.Cleanup(func() { engine.SetCurrent(previous) })
	return f
}

// calls returns the argv of every invocation so far, one space-joined line each.
func (f *fakeEngine) calls(t *testing.T) []string {
	t.Helper()
	return readLines(t, f.log)
}

// readLines returns the non-empty lines of a file, nil when it does not exist.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// slowDaemon makes every `exec` invocation of the fake engine block until the
// returned release function is called. The release is also registered on
// cleanup, so a failing test never leaves a blocked process behind.
//
// The test keeps the FIFO open read-write for its whole life: the script's
// open() never waits for a writer and its read() returns as soon as the line
// is written, whichever side gets there first.
func slowDaemon(t *testing.T) (release func()) {
	t.Helper()
	fifo := filepath.Join(t.TempDir(), "gate")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	gate, err := os.OpenFile(fifo, os.O_RDWR, 0) //nolint:gosec // a test fifo
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_ENGINE_BLOCK", fifo)
	released := false
	release = func() {
		if released {
			return
		}
		released = true
		_, _ = gate.WriteString("go\n")
	}
	t.Cleanup(func() {
		release()
		// Give a script that is still reading a moment to drain before the
		// descriptor closes; it then sees EOF at worst, which also ends read.
		time.Sleep(20 * time.Millisecond)
		_ = gate.Close()
	})
	return release
}
