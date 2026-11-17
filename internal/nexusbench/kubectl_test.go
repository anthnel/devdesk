package nexusbench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// The tests here never reach a real kubectl: PATH is replaced by a directory
// that holds either nothing or a shell script standing in for kubectl, which
// records what it was given. They need no cluster, no network and no
// requireBench guard.

func TestNewExecRunnerKubectlMissing(t *testing.T) {
	// An empty directory as the whole PATH: no kubectl can be found, whatever
	// the host has installed.
	t.Setenv("PATH", t.TempDir())

	r, err := NewExecRunner("datomir")
	if err == nil {
		t.Fatal("NewExecRunner succeeded without kubectl on PATH")
	}
	if r != nil {
		t.Errorf("runner = %v, want nil on error", r)
	}
	var be *BenchError
	if !errors.As(err, &be) {
		t.Fatalf("error %T is not a *BenchError: %v", err, err)
	}
	if be.Kind != KubectlMissing {
		t.Errorf("Kind = %d, want KubectlMissing (%d)", be.Kind, KubectlMissing)
	}
	msg := err.Error()
	for _, want := range []string{"kubectl", "mise install"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %q", msg, want)
		}
	}
}

// fakeKubectlScript is a stand-in kubectl: it writes its arguments, one per
// line, and its standard input to the files named by the environment, answers
// on both streams and exits with the requested code.
const fakeKubectlScript = `#!/bin/sh
printf '%s\n' "$@" > "$KUBECTL_FAKE_ARGS"
cat > "$KUBECTL_FAKE_STDIN"
echo "stdout-line"
echo "stderr-line" >&2
exit "${KUBECTL_FAKE_EXIT:-0}"
`

type fakeKubectlBinary struct {
	argsFile, stdinFile string
}

// installFakeKubectl puts the script first on PATH and points it at two files
// of the test's temporary directory. The script needs a POSIX shell.
func installFakeKubectl(t *testing.T) fakeKubectlBinary {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in kubectl is a POSIX shell script")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fakeKubectlScript), 0o755); err != nil { //nolint:gosec // an executable script is the point
		t.Fatalf("writing the stand-in kubectl: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	out := t.TempDir()
	b := fakeKubectlBinary{argsFile: filepath.Join(out, "args"), stdinFile: filepath.Join(out, "stdin")}
	t.Setenv("KUBECTL_FAKE_ARGS", b.argsFile)
	t.Setenv("KUBECTL_FAKE_STDIN", b.stdinFile)
	t.Setenv("KUBECTL_FAKE_EXIT", "0")
	return b
}

func (b fakeKubectlBinary) args(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(b.argsFile)
	if err != nil {
		t.Fatalf("the stand-in kubectl was not run: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

func (b fakeKubectlBinary) stdin(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(b.stdinFile)
	if err != nil {
		t.Fatalf("the stand-in kubectl was not run: %v", err)
	}
	return string(raw)
}

func TestExecRunnerInjectsTheContext(t *testing.T) {
	tests := []struct {
		name    string
		context string
		args    []string
		stdin   []byte
	}{
		{"read", "datomir", []string{"get", "pods", "-n", "nexus-bench"}, nil},
		{"apply with a manifest on stdin", "datomir", []string{"apply", "--server-side", "-f", "-"}, []byte("kind: Namespace\n")},
		{"another context", "k0s-lab", []string{"get", "nodes", "-o", "json"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := installFakeKubectl(t)
			r, err := NewExecRunner(tt.context)
			if err != nil {
				t.Fatalf("NewExecRunner: %v", err)
			}

			stdout, stderr, err := r.Run(context.Background(), tt.stdin, tt.args...)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			want := append([]string{"--context", tt.context}, tt.args...)
			if got := bin.args(t); !slices.Equal(got, want) {
				t.Errorf("kubectl got %q, want %q", got, want)
			}
			if got := bin.stdin(t); got != string(tt.stdin) {
				t.Errorf("kubectl stdin = %q, want %q", got, tt.stdin)
			}
			if string(stdout) != "stdout-line\n" {
				t.Errorf("stdout = %q, want %q", stdout, "stdout-line\n")
			}
			if string(stderr) != "stderr-line\n" {
				t.Errorf("stderr = %q, want %q", stderr, "stderr-line\n")
			}
		})
	}
}

func TestExecRunnerKeepsBothStreamsOnFailure(t *testing.T) {
	installFakeKubectl(t)
	t.Setenv("KUBECTL_FAKE_EXIT", "3")
	r, err := NewExecRunner("datomir")
	if err != nil {
		t.Fatalf("NewExecRunner: %v", err)
	}

	stdout, stderr, err := r.Run(context.Background(), nil, "get", "pods")
	if err == nil {
		t.Fatal("Run succeeded although kubectl exited 3")
	}
	if string(stdout) != "stdout-line\n" || string(stderr) != "stderr-line\n" {
		t.Errorf("streams = %q / %q, want both kept for the classifier", stdout, stderr)
	}
}

func TestExecRunnerHonorsACancelledContext(t *testing.T) {
	installFakeKubectl(t)
	r, err := NewExecRunner("datomir")
	if err != nil {
		t.Fatalf("NewExecRunner: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := r.Run(ctx, nil, "get", "pods"); err == nil {
		t.Error("Run succeeded on a cancelled context")
	}
}

// ---- port-forward (DEVDESK-539) -----------------------------------------------

// The port-forward tests run a stand-in kubectl started through the helper
// process (helper_process_test.go): PATH holds it alone.

const forwardLine = "Forwarding from 127.0.0.1:41234 -> 5000\n"

func TestParseForwardingLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
		ok   bool
	}{
		{"IPv4", "Forwarding from 127.0.0.1:41234 -> 5000", "127.0.0.1:41234", true},
		{"IPv4 with the line end kubectl prints", "Forwarding from 127.0.0.1:41234 -> 5000\n", "127.0.0.1:41234", true},
		{"IPv4 with a Windows line end", "Forwarding from 127.0.0.1:41234 -> 5000\r\n", "127.0.0.1:41234", true},
		{"IPv4 with leading blanks", "  Forwarding from 127.0.0.1:8080 -> 5000", "127.0.0.1:8080", true},
		{"IPv6 loopback is ignored", "Forwarding from [::1]:41234 -> 5000", "", false},
		{"another address is ignored", "Forwarding from 10.0.0.5:41234 -> 5000", "", false},
		{"every interface is ignored", "Forwarding from 0.0.0.0:41234 -> 5000", "", false},
		{"a host that only starts like the loopback", "Forwarding from 127.0.0.1.evil.com:41234 -> 5000", "", false},
		{"connection log line", "Handling connection for 41234", "", false},
		{"error line", "error: unable to forward port because pod is not running. Current status=Pending", "", false},
		{"empty", "", "", false},
		{"only the prefix", "Forwarding from ", "", false},
		{"no port", "Forwarding from 127.0.0.1 -> 5000", "", false},
		{"port zero", "Forwarding from 127.0.0.1:0 -> 5000", "", false},
		{"port out of range", "Forwarding from 127.0.0.1:70000 -> 5000", "", false},
		{"port not a number", "Forwarding from 127.0.0.1:http -> 5000", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseForwardingLine(tt.line)
			if got != tt.want || ok != tt.ok {
				t.Errorf("parseForwardingLine(%q) = %q, %v; want %q, %v", tt.line, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// newHelperForwarder installs the stand-in kubectl (alone on PATH) and returns
// the forwarder that finds it.
func newHelperForwarder(t *testing.T, env map[string]string) (PortForwarder, helperTool) {
	t.Helper()
	tool := installHelperTool(t, "kubectl", env)
	f, err := NewExecPortForwarder("datomir")
	if err != nil {
		t.Fatalf("NewExecPortForwarder: %v", err)
	}
	return f, tool
}

func TestNewExecPortForwarderKubectlMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	f, err := NewExecPortForwarder("datomir")
	if err == nil {
		t.Fatal("NewExecPortForwarder succeeded without kubectl on PATH")
	}
	if f != nil {
		t.Errorf("forwarder = %v, want nil on error", f)
	}
	var be *BenchError
	if !errors.As(err, &be) {
		t.Fatalf("error %T is not a *BenchError: %v", err, err)
	}
	if be.Kind != KubectlMissing {
		t.Errorf("Kind = %d, want KubectlMissing (%d)", be.Kind, KubectlMissing)
	}
}

func TestForwardTimeoutDefaultIsFifteenSeconds(t *testing.T) {
	if portForwardTimeout != 15*time.Second {
		t.Errorf("portForwardTimeout = %s, want 15s", portForwardTimeout)
	}
}

func TestForwardRunsKubectlPortForward(t *testing.T) {
	f, tool := newHelperForwarder(t, map[string]string{helperStdoutEnv: forwardLine, helperHangEnv: "1"})

	local, stop, err := f.Forward(context.Background(), "nexus-bench", "bench-upstream", 5000)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if stop == nil {
		t.Fatal("Forward returned a nil stop function")
	}
	defer func() { _ = stop() }()

	if local != "127.0.0.1:41234" {
		t.Errorf("local = %q, want %q", local, "127.0.0.1:41234")
	}
	want := []string{"--context", "datomir", "-n", "nexus-bench", "port-forward", "svc/bench-upstream", ":5000", "--address", "127.0.0.1"}
	if got := tool.last(t).Args; !slices.Equal(got, want) {
		t.Errorf("kubectl got %q, want %q", got, want)
	}
}

func TestForwardIgnoresIPv6AndForeignLines(t *testing.T) {
	out := "Forwarding from [::1]:11111 -> 5000\nsome other line\nForwarding from 127.0.0.1:43210 -> 5000\nHandling connection for 43210\n"
	f, _ := newHelperForwarder(t, map[string]string{helperStdoutEnv: out, helperHangEnv: "1"})

	local, stop, err := f.Forward(context.Background(), "nexus-bench", "bench-upstream", 5000)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	defer func() { _ = stop() }()
	if local != "127.0.0.1:43210" {
		t.Errorf("local = %q, want the IPv4 address %q", local, "127.0.0.1:43210")
	}
}

func TestForwardOnlyIPv6LineTimesOut(t *testing.T) {
	old := portForwardTimeout
	portForwardTimeout = 2 * time.Second // the stand-in must have time to start, even under -race
	t.Cleanup(func() { portForwardTimeout = old })
	f, tool := newHelperForwarder(t, map[string]string{helperStdoutEnv: "Forwarding from [::1]:11111 -> 5000\n", helperHangEnv: "1"})

	_, stop, err := f.Forward(context.Background(), "nexus-bench", "bench-upstream", 5000)
	if err == nil {
		_ = stop()
		t.Fatal("Forward accepted an IPv6 address")
	}
	if !tool.waitRan(5 * time.Second) {
		t.Fatal("the stand-in kubectl never started")
	}
	if !waitProcessGone(t, tool.pid(t), 5*time.Second) {
		t.Error("kubectl is still running after Forward gave up")
	}
}

func TestForwardStopKillsAndWaitsForTheProcess(t *testing.T) {
	f, tool := newHelperForwarder(t, map[string]string{helperStdoutEnv: forwardLine, helperHangEnv: "1"})
	_, stop, err := f.Forward(context.Background(), "nexus-bench", "bench-upstream", 5000)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	pid := tool.pid(t)
	if processGone(pid) {
		t.Fatal("kubectl is not running after Forward returned")
	}

	if err := stop(); err != nil {
		t.Errorf("stop: %v", err)
	}
	// No polling: stop has waited, so the process is gone and not a zombie.
	if !processGone(pid) {
		t.Error("kubectl is still there right after stop returned: it was killed but not waited for")
	}
}

func TestForwardStopIsIdempotent(t *testing.T) {
	f, _ := newHelperForwarder(t, map[string]string{helperStdoutEnv: forwardLine, helperHangEnv: "1"})
	_, stop, err := f.Forward(context.Background(), "nexus-bench", "bench-upstream", 5000)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	for i := 1; i <= 3; i++ {
		if err := stop(); err != nil {
			t.Errorf("stop call %d: %v", i, err)
		}
	}
}

func TestForwardContextCancellationKillsTheProcess(t *testing.T) {
	f, tool := newHelperForwarder(t, map[string]string{helperStdoutEnv: forwardLine, helperHangEnv: "1"})
	ctx, cancel := context.WithCancel(context.Background())
	_, stop, err := f.Forward(ctx, "nexus-bench", "bench-upstream", 5000)
	if err != nil {
		cancel()
		t.Fatalf("Forward: %v", err)
	}
	pid := tool.pid(t)

	cancel()

	if !waitProcessGone(t, pid, 5*time.Second) {
		t.Error("kubectl survived the cancellation of its context (or was left as a zombie)")
	}
	done := make(chan error, 1)
	go func() { done <- stop() }()
	select {
	case err := <-done:
		// Only the return matters here: the process was killed by the context,
		// so what stop reports about that is not part of the contract.
		if err != nil {
			t.Logf("stop after the cancellation returned: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("stop hangs after the context was cancelled")
	}
}

func TestForwardGivesUpAfterTheTimeout(t *testing.T) {
	old := portForwardTimeout
	portForwardTimeout = 2 * time.Second // the stand-in must have time to start, even under -race
	t.Cleanup(func() { portForwardTimeout = old })
	// kubectl starts, says nothing, and stays.
	f, tool := newHelperForwarder(t, map[string]string{helperHangEnv: "1"})

	start := time.Now()
	local, stop, err := f.Forward(context.Background(), "nexus-bench", "bench-upstream", 5000)
	if err == nil {
		_ = stop()
		t.Fatalf("Forward succeeded with %q although kubectl printed nothing", local)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("Forward took %s to give up on a 2s timeout", elapsed)
	}
	var be *BenchError
	if !errors.As(err, &be) {
		t.Fatalf("error %T is not a *BenchError: %v", err, err)
	}
	if be.Kind != Deadline {
		t.Errorf("Kind = %d, want Deadline (%d); message: %v", be.Kind, Deadline, err)
	}
	if !processGone(tool.pid(t)) {
		t.Error("kubectl is still there when Forward returned its error: it must be killed and waited for")
	}
}

func TestForwardContextCancelledWhileWaitingForTheLine(t *testing.T) {
	f, tool := newHelperForwarder(t, map[string]string{helperHangEnv: "1"})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		tool.waitRan(5 * time.Second)
		cancel()
	}()

	start := time.Now()
	_, stop, err := f.Forward(ctx, "nexus-bench", "bench-upstream", 5000)
	if err == nil {
		_ = stop()
		t.Fatal("Forward succeeded on a cancelled context")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("Forward took %s after the cancellation, want it to stop waiting at once", elapsed)
	}
	if !waitProcessGone(t, tool.pid(t), 5*time.Second) {
		t.Error("kubectl survived the cancellation")
	}
}

func TestForwardAlreadyCancelledContext(t *testing.T) {
	f, _ := newHelperForwarder(t, map[string]string{helperStdoutEnv: forwardLine, helperHangEnv: "1"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, stop, err := f.Forward(ctx, "nexus-bench", "bench-upstream", 5000); err == nil {
		_ = stop()
		t.Error("Forward succeeded on a cancelled context")
	}
}

func TestForwardKubectlExitsBeforeTheLine(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		exit   string
		want   ErrKind
	}{
		{"forbidden", "Error from server (Forbidden): pods \"x\" is forbidden: User \"u\" cannot create resource \"pods/portforward\"; token=sekrit-forward\n", "1", Forbidden},
		{"cluster unreachable", "Unable to connect to the server: dial tcp 192.168.1.31:6443: connect: connection refused; token=sekrit-forward\n", "1", ClusterUnreachable},
		{"unknown text", "something nobody classified; token=sekrit-forward\n", "1", Unclassified},
		{"exit 0 without the line", "token=sekrit-forward\n", "0", Unclassified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := newHelperForwarder(t, map[string]string{helperStderrEnv: tt.stderr, helperExitEnv: tt.exit})

			_, stop, err := f.Forward(context.Background(), "nexus-bench", "bench-upstream", 5000)
			if err == nil {
				_ = stop()
				t.Fatal("Forward succeeded although kubectl exited without naming a port")
			}
			var be *BenchError
			if !errors.As(err, &be) {
				t.Fatalf("error %T is not a *BenchError: %v", err, err)
			}
			if be.Kind != tt.want {
				t.Errorf("Kind = %d, want %d; message: %v", be.Kind, tt.want, err)
			}
			if strings.Contains(err.Error(), "sekrit-forward") || strings.Contains(err.Error(), "dial tcp") {
				t.Errorf("the message carries the stderr of kubectl: %q", err.Error())
			}
			if be.Err != nil && strings.Contains(be.Err.Error(), "sekrit-forward") {
				t.Errorf("the wrapped error carries the stderr of kubectl: %q", be.Err.Error())
			}
		})
	}
}

// ---- the target of a forward, and the reader of kubectl's output ----------------

func TestForwardRefusesAnInvalidTarget(t *testing.T) {
	tests := []struct {
		name               string
		namespace, service string
		port               int
	}{
		{"namespace starts with a dash", "-nexus-bench", "bench-upstream", 5000},
		{"namespace is an option", "--all-namespaces", "bench-upstream", 5000},
		{"service starts with a dash", "nexus-bench", "-bench-upstream", 5000},
		{"service is an option", "nexus-bench", "--address=0.0.0.0", 5000},
		{"namespace ends with a dash", "nexus-bench-", "bench-upstream", 5000},
		{"service ends with a dash", "nexus-bench", "bench-upstream-", 5000},
		{"empty namespace", "", "bench-upstream", 5000},
		{"empty service", "nexus-bench", "", 5000},
		{"upper case namespace", "Nexus-Bench", "bench-upstream", 5000},
		{"upper case service", "nexus-bench", "Bench-Upstream", 5000},
		{"underscore", "nexus_bench", "bench-upstream", 5000},
		{"space in the service", "nexus-bench", "bench upstream", 5000},
		{"slash in the service", "nexus-bench", "bench/upstream", 5000},
		{"service with a resource prefix", "nexus-bench", "svc/bench-upstream", 5000},
		{"service with a port suffix", "nexus-bench", "bench-upstream:5000", 5000},
		{"newline in the namespace", "nexus-bench\n", "bench-upstream", 5000},
		{"shell metacharacters", "nexus-bench;id", "bench-upstream", 5000},
		{"port zero", "nexus-bench", "bench-upstream", 0},
		{"negative port", "nexus-bench", "bench-upstream", -1},
		{"port above the range", "nexus-bench", "bench-upstream", 65536},
		{"large port", "nexus-bench", "bench-upstream", 1 << 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, tool := newHelperForwarder(t, map[string]string{helperStdoutEnv: forwardLine, helperHangEnv: "1"})

			local, stop, err := f.Forward(context.Background(), tt.namespace, tt.service, tt.port)
			if err == nil {
				_ = stop()
				t.Fatalf("Forward accepted the target and returned %q", local)
			}
			var be *BenchError
			if !errors.As(err, &be) {
				t.Fatalf("error %T is not a *BenchError: %v", err, err)
			}
			if be.Cause == "" || be.Check == "" {
				t.Errorf("the BenchError has no cause or no check: %+v", be)
			}
			tool.neverRan(t) // refused before any process
		})
	}
}

func TestForwardAcceptsTheEdgesOfAValidTarget(t *testing.T) {
	tests := []struct {
		name               string
		namespace, service string
		port               int
	}{
		{"one letter names, port 1", "a", "b", 1},
		{"digits first, port 65535", "1ns", "2svc", 65535},
		{"dashes inside", "nexus-bench", "bench-upstream", 5000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, tool := newHelperForwarder(t, map[string]string{helperStdoutEnv: forwardLine, helperHangEnv: "1"})

			_, stop, err := f.Forward(context.Background(), tt.namespace, tt.service, tt.port)
			if err != nil {
				t.Fatalf("Forward: %v", err)
			}
			defer func() { _ = stop() }()
			args := tool.last(t).Args
			if !slices.Contains(args, "svc/"+tt.service) || !slices.Contains(args, fmt.Sprintf(":%d", tt.port)) {
				t.Errorf("kubectl got %q, want svc/%s and :%d", args, tt.service, tt.port)
			}
		})
	}
}

func newLineWriter() *forwardLineWriter { return &forwardLineWriter{found: make(chan string, 1)} }

// foundOf returns the address the writer published, if it has.
func foundOf(w *forwardLineWriter) (string, bool) {
	select {
	case v := <-w.found:
		return v, true
	default:
		return "", false
	}
}

func TestForwardLineWriter(t *testing.T) {
	tests := []struct {
		name   string
		writes []string
		want   string // "" = nothing published
	}{
		{"one whole line", []string{forwardLine}, "127.0.0.1:41234"},
		{"a line cut in two writes", []string{"Forwarding from 127.0.", "0.1:41234 -> 5000\n"}, "127.0.0.1:41234"},
		{"a line cut before its line end", []string{"Forwarding from 127.0.0.1:41234 -> 5000", "\n"}, "127.0.0.1:41234"},
		{"a line cut one byte at a time", strings.Split(forwardLine, ""), "127.0.0.1:41234"},
		{"no line end yet", []string{"Forwarding from 127.0.0.1:41234 -> 5000"}, ""},
		{"foreign lines first", []string{"some noise\nanother\n" + forwardLine}, "127.0.0.1:41234"},
		{"IPv6 first, IPv4 in the next write", []string{"Forwarding from [::1]:11111 -> 5000\n", "Forwarding from 127.0.0.1:43210 -> 5000\n"}, "127.0.0.1:43210"},
		{"IPv6 only", []string{"Forwarding from [::1]:11111 -> 5000\n"}, ""},
		{"a line end alone", []string{"\n\n\n"}, ""},
		{"the first address wins", []string{"Forwarding from 127.0.0.1:1111 -> 5000\nForwarding from 127.0.0.1:2222 -> 5000\n"}, "127.0.0.1:1111"},
		{"a long line then the right one", []string{strings.Repeat("x", forwardLineMax+1), "\n" + forwardLine}, "127.0.0.1:41234"},
		{"a very long line without a line end, then the right line", []string{strings.Repeat("z", 10*forwardLineMax), forwardLine}, "127.0.0.1:41234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newLineWriter()
			for _, s := range tt.writes {
				if n, err := w.Write([]byte(s)); n != len(s) || err != nil {
					t.Fatalf("Write(%d bytes) = %d, %v; want all of them and no error", len(s), n, err)
				}
			}
			got, ok := foundOf(w)
			if tt.want == "" {
				if ok {
					t.Errorf("published %q, want nothing", got)
				}
				return
			}
			if !ok || got != tt.want {
				t.Errorf("published %q (found: %v), want %q", got, ok, tt.want)
			}
		})
	}
}

// What kubectl prints without a line end is not kept without bound.
func TestForwardLineWriterBoundsWhatItKeeps(t *testing.T) {
	w := newLineWriter()
	chunk := []byte(strings.Repeat("q", 1000))
	for range 200 { // 200 kB without a single line end
		if n, err := w.Write(chunk); n != len(chunk) || err != nil {
			t.Fatalf("Write = %d, %v", n, err)
		}
		if len(w.pending) > forwardLineMax {
			t.Fatalf("the writer keeps %d bytes of an unfinished line, more than forwardLineMax (%d)", len(w.pending), forwardLineMax)
		}
	}
	if _, ok := foundOf(w); ok {
		t.Error("an address was published from a stream of q")
	}
}

// "Handling connection for ..." goes on as long as the forward lives: after the
// address, the writer takes everything, keeps nothing and publishes nothing more.
func TestForwardLineWriterIgnoresWhatFollowsTheAddress(t *testing.T) {
	w := newLineWriter()
	if _, err := w.Write([]byte(forwardLine)); err != nil {
		t.Fatal(err)
	}
	if got, ok := foundOf(w); !ok || got != "127.0.0.1:41234" {
		t.Fatalf("published %q (found: %v), want the address", got, ok)
	}
	for range 50 {
		line := []byte("Handling connection for 41234\nForwarding from 127.0.0.1:55555 -> 5000\n" + strings.Repeat("h", 100))
		if n, err := w.Write(line); n != len(line) || err != nil {
			t.Fatalf("Write = %d, %v; want all of it and no error: a failing writer stops kubectl", n, err)
		}
	}
	if got, ok := foundOf(w); ok {
		t.Errorf("published a second address %q", got)
	}
	if len(w.pending) != 0 {
		t.Errorf("the writer keeps %d bytes after the address", len(w.pending))
	}
}
