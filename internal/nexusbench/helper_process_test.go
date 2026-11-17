package nexusbench

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The "helper process" pattern of os/exec's own tests, for the two tools the
// bench starts (kubectl and skopeo): the tests put a one-line shell shim named
// like the tool first on PATH, and the shim re-runs this test binary on
// TestHelperProcess, which records what it was given, answers as the test
// asked and exits (or hangs, to be killed). PATH holds nothing else, so the
// real kubectl and skopeo of the host can never be reached. The shim needs a
// POSIX shell.

// Environment of the helper, set by installHelperTool.
const (
	helperWantEnv   = "GO_WANT_HELPER_PROCESS" // set by the shim, never by a test
	helperBinaryEnv = "HELPER_TEST_BINARY"
	helperToolEnv   = "HELPER_TOOL"
	helperRecordEnv = "HELPER_RECORD"  // JSON lines, one per run
	helperPIDEnv    = "HELPER_PIDFILE" // the pid of the last run
	helperStdoutEnv = "HELPER_STDOUT"
	// helperStdoutBytesEnv: that many 'x' bytes are printed after HELPER_STDOUT
	// (an environment value is too small to hold a manifest of several MiB).
	helperStdoutBytesEnv = "HELPER_STDOUT_BYTES"
	helperStderrEnv      = "HELPER_STDERR"
	helperExitEnv        = "HELPER_EXIT"
	helperHangEnv        = "HELPER_HANG" // "1": print, then wait to be killed
)

// helperShim is the stand-in binary. The variable is exported by the shim
// itself so that only a process started through it ever sees it. The race
// detector sleeps one second before a process exits (atexit_sleep_ms), which
// would add a second to every run of a stand-in under `mise run test-race`.
const helperShim = `#!/bin/sh
export GO_WANT_HELPER_PROCESS=1
export GORACE="${GORACE:+$GORACE }atexit_sleep_ms=0"
exec "$HELPER_TEST_BINARY" -test.run='^TestHelperProcess$' -- "$@"
`

// helperAuthfile is an authentication file the tool was pointed at, read while
// it still existed.
type helperAuthfile struct {
	Flag    string `json:"flag"`
	Path    string `json:"path"`
	Mode    uint32 `json:"mode"`
	DirMode uint32 `json:"dirMode"`
	Content string `json:"content"`
}

// helperRecord is what one run of the helper saw.
type helperRecord struct {
	Tool      string           `json:"tool"`
	Args      []string         `json:"args"`
	Env       []string         `json:"env"`
	PID       int              `json:"pid"`
	Authfiles []helperAuthfile `json:"authfiles"`
	// PolicyPath and Policy: the --policy file and what it held.
	PolicyPath string `json:"policyPath"`
	Policy     string `json:"policy"`
	// RegistriesConf: CONTAINERS_REGISTRIES_CONF, whether the file existed and
	// its size.
	RegistriesConf       string `json:"registriesConf"`
	RegistriesConfExists bool   `json:"registriesConfExists"`
	RegistriesConfSize   int64  `json:"registriesConfSize"`
}

// helperTool is one installed stand-in.
type helperTool struct {
	name      string
	record    string
	pidFile   string
	scratch   string // TMPDIR of the test: where a tool's temporary files go
	testBytes string
}

// installHelperTool puts a stand-in for the tool name first, and alone, on PATH
// and points TMPDIR at a folder of its own, so a test can tell whether the code
// under test left a file behind. env sets the answer of the helper (the
// helper*Env names) and anything else the test wants the tool to see.
func installHelperTool(t *testing.T, name string, env map[string]string) helperTool {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in tools are POSIX shell scripts")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	bin, out, scratch := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, name), []byte(helperShim), 0o755); err != nil { //nolint:gosec // an executable shim is the point
		t.Fatalf("writing the stand-in %s: %v", name, err)
	}
	h := helperTool{
		name:      name,
		record:    filepath.Join(out, "record.jsonl"),
		pidFile:   filepath.Join(out, "pid"),
		scratch:   scratch,
		testBytes: exe,
	}
	t.Setenv("PATH", bin) // nothing else: the real tool cannot be found
	t.Setenv("TMPDIR", scratch)
	t.Setenv(helperBinaryEnv, exe)
	t.Setenv(helperToolEnv, name)
	t.Setenv(helperRecordEnv, h.record)
	t.Setenv(helperPIDEnv, h.pidFile)
	for _, k := range []string{helperStdoutEnv, helperStdoutBytesEnv, helperStderrEnv, helperExitEnv, helperHangEnv} {
		t.Setenv(k, "")
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	return h
}

// records returns every run of the stand-in, oldest first.
func (h helperTool) records(t *testing.T) []helperRecord {
	t.Helper()
	raw, err := os.ReadFile(h.record)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("reading the record of the stand-in %s: %v", h.name, err)
	}
	var out []helperRecord
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var r helperRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("decoding a record of the stand-in %s: %v", h.name, err)
		}
		out = append(out, r)
	}
	return out
}

// last returns the last run, and fails when the stand-in never ran.
func (h helperTool) last(t *testing.T) helperRecord {
	t.Helper()
	rs := h.records(t)
	if len(rs) == 0 {
		t.Fatalf("the stand-in %s was never run", h.name)
	}
	return rs[len(rs)-1]
}

// neverRan fails when the stand-in was run: the code under test had to refuse
// before it started a process.
func (h helperTool) neverRan(t *testing.T) {
	t.Helper()
	if rs := h.records(t); len(rs) != 0 {
		t.Errorf("the stand-in %s was run %d time(s) (first: %q), want a refusal before any process", h.name, len(rs), rs[0].Args)
	}
}

// pid returns the pid of the last run of a stand-in that is still running.
func (h helperTool) pid(t *testing.T) int {
	t.Helper()
	raw, err := os.ReadFile(h.pidFile)
	if err != nil {
		t.Fatalf("the stand-in %s wrote no pid: %v", h.name, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("the pid file of %s holds %q", h.name, raw)
	}
	return pid
}

// processGone tells whether pid is neither running nor a zombie: signal 0 to a
// process that was killed but not waited for still succeeds.
func processGone(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	return p.Signal(syscall.Signal(0)) != nil
}

// waitProcessGone polls for processGone: the kill of a cancelled context is
// asynchronous.
func waitProcessGone(t *testing.T, pid int, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if processGone(pid) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return processGone(pid)
}

// ran tells whether the stand-in has been run at least once. It never fails
// the test, so a goroutine may poll it.
func (h helperTool) ran() bool {
	info, err := os.Stat(h.record)
	return err == nil && info.Size() > 0
}

// waitRan polls ran; false means the stand-in did not start in time.
func (h helperTool) waitRan(within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if h.ran() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return h.ran()
}

// flagValue returns the value of a "--flag value" or "--flag=value" argument.
func flagValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
		if v, ok := strings.CutPrefix(a, flag+"="); ok {
			return v, true
		}
	}
	return "", false
}

// TestHelperProcess is not a test: it is the stand-in tool the shim starts.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperWantEnv) != "1" {
		return
	}
	args := os.Args
	if i := slices.Index(args, "--"); i >= 0 {
		args = args[i+1:]
	} else {
		args = nil
	}
	// The pid first, and atomically: a test that waits for the record may read
	// the pid at once, and a process killed early must still have left one.
	writePIDFile(os.Getenv(helperPIDEnv))
	rec := helperRecord{Tool: os.Getenv(helperToolEnv), Args: args, Env: os.Environ(), PID: os.Getpid()}
	rec.readFiles()
	line, _ := json.Marshal(rec)
	f, err := os.OpenFile(os.Getenv(helperRecordEnv), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(97)
	}
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()

	_, _ = os.Stdout.WriteString(os.Getenv(helperStdoutEnv))
	if n, _ := strconv.Atoi(os.Getenv(helperStdoutBytesEnv)); n > 0 {
		writeFiller(os.Stdout, n)
	}
	_, _ = os.Stderr.WriteString(os.Getenv(helperStderrEnv))
	if os.Getenv(helperHangEnv) == "1" {
		time.Sleep(time.Hour)
	}
	code, _ := strconv.Atoi(os.Getenv(helperExitEnv))
	os.Exit(code)
}

// writePIDFile writes the pid of this process to path through a rename, so a
// reader sees no file or the whole pid.
func writePIDFile(path string) {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(os.Getpid())), 0o600); err == nil {
		_ = os.Rename(tmp, path)
	}
}

// writeFiller writes n bytes 'x' to w.
func writeFiller(w *os.File, n int) {
	chunk := bytes.Repeat([]byte("x"), 64<<10)
	for n > 0 {
		k := min(n, len(chunk))
		if _, err := w.Write(chunk[:k]); err != nil {
			return
		}
		n -= k
	}
}

// readFiles reads, while they still exist, the files the arguments and the
// environment point at.
func (r *helperRecord) readFiles() {
	for _, flag := range []string{"--authfile", "--src-authfile", "--dest-authfile"} {
		path, ok := flagValue(r.Args, flag)
		if !ok {
			continue
		}
		af := helperAuthfile{Flag: flag, Path: path}
		if info, err := os.Stat(path); err == nil {
			af.Mode = uint32(info.Mode().Perm())
			if raw, err := os.ReadFile(path); err == nil { //nolint:gosec // a file the code under test named in its own arguments
				af.Content = string(raw)
			}
		}
		if info, err := os.Stat(filepath.Dir(path)); err == nil {
			af.DirMode = uint32(info.Mode().Perm())
		}
		r.Authfiles = append(r.Authfiles, af)
	}
	if path, ok := flagValue(r.Args, "--policy"); ok {
		r.PolicyPath = path
		if raw, err := os.ReadFile(path); err == nil { //nolint:gosec // as above
			r.Policy = string(raw)
		}
	}
	for _, kv := range r.Env {
		if v, ok := strings.CutPrefix(kv, "CONTAINERS_REGISTRIES_CONF="); ok {
			r.RegistriesConf = v
			if info, err := os.Stat(v); err == nil {
				r.RegistriesConfExists, r.RegistriesConfSize = true, info.Size()
			}
		}
	}
}

// envOf returns the value of name in an environment list.
func envOf(env []string, name string) (string, bool) {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, name+"="); ok {
			return v, true
		}
	}
	return "", false
}
