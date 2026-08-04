package ociresources

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The commands in commands.go shell out to docker and, for a scan, to trivy.
// Rather than stub the seam inside internal/docker — which is unexported, and
// stubbing it would leave the wrapper's own error handling untested — the tools
// are installed on a PATH of the test's own making. Each one is a copy of the
// test binary, which TestMain teaches to behave as the tool: what it prints and
// what it exits with come from the environment.
//
// This is the technique internal/scan uses (dependencies_test.go), with one
// addition: a single fake answers several different commands here — `docker
// image ls` and `docker network inspect` reach the same file — so the response
// is selected by the invocation rather than fixed for the whole process.

const (
	helperMode   = "DEVDESK_OCI_HELPER"
	helperScript = "DEVDESK_OCI_HELPER_SCRIPT"
	// helperLog names a file each invocation is appended to, so a test can
	// assert what the tool was actually asked to do. The fake runs in its own
	// process, so a file is the only channel that does not collide with the
	// output the caller is parsing.
	helperLog = "DEVDESK_OCI_HELPER_LOG"
)

// fakeReply is what a fake tool does for one invocation.
type fakeReply struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Exit   int    `json:"exit"`
}

// fakeScript maps an invocation prefix — the tool name followed by the leading
// arguments, e.g. "docker image ls" — to the reply it produces. The longest
// matching prefix wins, so "docker image ls" and "docker image inspect" can be
// scripted separately. An invocation nothing matches succeeds silently, which
// is how "docker accepted the command and printed nothing" is expressed.
type fakeScript map[string]fakeReply

// runAsHelper is the whole of the fake tool. It runs in a subprocess, so it
// reports through its exit code and streams rather than through testing.T.
func runAsHelper() int {
	var script fakeScript
	if raw := os.Getenv(helperScript); raw != "" {
		if err := json.Unmarshal([]byte(raw), &script); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "unreadable helper script: %v\n", err)
			return 127
		}
	}

	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	invocation := strings.Join(append([]string{name}, os.Args[1:]...), " ")

	if path := os.Getenv(helperLog); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "cannot log the invocation: %v\n", err)
			return 127
		}
		_, _ = fmt.Fprintln(f, invocation)
		_ = f.Close()
	}

	var match string
	for prefix := range script {
		if strings.HasPrefix(invocation, prefix) && len(prefix) > len(match) {
			match = prefix
		}
	}
	reply := script[match]

	if reply.Stdout != "" {
		_, _ = fmt.Fprint(os.Stdout, reply.Stdout)
	}
	if reply.Stderr != "" {
		_, _ = fmt.Fprint(os.Stderr, reply.Stderr)
	}
	return reply.Exit
}

// fakeToolDir holds one copy of the test binary per tool name. Copying is the
// expensive part — the binary is tens of megabytes — and what a fake *does*
// comes from the environment, so the copies are made once and shared. Nothing
// in this package runs in parallel, but the mutex keeps that from being a
// silent requirement.
var (
	fakeToolMu  sync.Mutex
	fakeToolDir string
)

// installFakeTools puts the named tools on an otherwise empty PATH and scripts
// how they answer.
func installFakeTools(t *testing.T, script fakeScript, names ...string) {
	t.Helper()

	dir := ensureFakeTools(t, names...)

	encoded, err := json.Marshal(script)
	if err != nil {
		t.Fatalf("encoding the helper script: %v", err)
	}
	t.Setenv("PATH", dir)
	t.Setenv(helperMode, "1")
	t.Setenv(helperScript, string(encoded))
}

// installFakeDocker is the common case: docker alone, answering one script.
func installFakeDocker(t *testing.T, script fakeScript) {
	t.Helper()
	installFakeTools(t, script, "docker")
}

// recordInvocations makes the installed fakes append what they were asked to a
// file, and returns a reader for it.
func recordInvocations(t *testing.T) func() []string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "invocations")
	t.Setenv(helperLog, path)

	return func() []string {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var lines []string
		for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
			if line != "" {
				lines = append(lines, line)
			}
		}
		return lines
	}
}

// noDocker points PATH at a directory holding nothing, which is what an
// uninstalled docker looks like to exec.LookPath.
func noDocker(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func ensureFakeTools(t *testing.T, names ...string) string {
	t.Helper()

	fakeToolMu.Lock()
	defer fakeToolMu.Unlock()

	if fakeToolDir == "" {
		dir, err := os.MkdirTemp("", "devdesk-oci-tools")
		if err != nil {
			t.Fatalf("creating the tool directory: %v", err)
		}
		fakeToolDir = dir
	}

	for _, name := range names {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		path := filepath.Join(fakeToolDir, name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		self, err := os.Executable()
		if err != nil {
			t.Fatalf("locating the test binary: %v", err)
		}
		body, err := os.ReadFile(self)
		if err != nil {
			t.Fatalf("reading the test binary: %v", err)
		}
		if err := os.WriteFile(path, body, 0o755); err != nil {
			t.Fatalf("installing %s: %v", name, err)
		}
	}
	return fakeToolDir
}

// removeFakeTools is called by TestMain once the suite is over.
func removeFakeTools() {
	fakeToolMu.Lock()
	defer fakeToolMu.Unlock()
	if fakeToolDir != "" {
		_ = os.RemoveAll(fakeToolDir)
		fakeToolDir = ""
	}
}

// run executes a command and returns the single message it produced. tea.Cmd is
// a function returning a message, so this is what the Bubble Tea runtime does
// with it.
func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command to run")
	}
	return cmd()
}
