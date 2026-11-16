package nexusbench

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
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
