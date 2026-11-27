package enginecontract_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/engineguard"
)

// OC-09a1, criteria 1 and 5, on the side of the task: tools/engine-vms/contract.sh
// applies the gate on the developer's machine, before any ssh, then passes the
// variable on to the test VM. Run here, it can only stop at the gate or at its
// argument check: the Incus host is fixture-host.invalid, which does not resolve,
// and each case ends before a connection is attempted (the output must not name
// the host, or say ssh).
//
// The script, for a1: `contract.sh verify <docker|podman> <n|n1> [--mode
// rootful|rootless] [--no-reset]`.
//
//	DEVDESK_ENGINE_CONTRACT is not 1 -> prints "skipped", exits 0
//	CI is not empty, gate open        -> prints "refused", exits 1
//	invalid arguments                 -> exits 2 (unknown engine or line, an unknown
//	                                    action, --mode rootless with docker, --mode other than rootful or rootless)
//
// Not pinned, because the plan states both orders: the case of CI set and the
// variable unset (the Go gate skips; the plan's script text lists the refusal first),
// and invalid arguments with the gate closed.

const hostThatDoesNotResolve = "fixture-host.invalid"

// scriptEnv is the environment of this process without what steers the gate and
// the VM scripts, plus the given pairs.
func scriptEnv(kv ...string) []string {
	var out []string
	for _, e := range os.Environ() {
		name, _, _ := strings.Cut(e, "=")
		if slices.Contains(engineguard.CIMarkers(), name) || strings.HasPrefix(name, "DEVDESK_ENGINE_") || strings.HasPrefix(name, "DEVDESK_FIXTURE_") ||
			strings.HasPrefix(name, "DD_ENGINE_VMS_") {
			continue
		}
		out = append(out, e)
	}
	out = append(out, "DD_ENGINE_VMS_HOST="+hostThatDoesNotResolve)
	return append(out, kv...)
}

// runScript runs contract.sh from the repository root and returns its output and
// exit code.
func runScript(t *testing.T, env []string, args ...string) (string, int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the VM scripts are bash")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash on this machine")
	}
	root := repoRoot(t)
	script := filepath.Join(root, "tools", "engine-vms", "contract.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("tools/engine-vms/contract.sh does not exist: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, append([]string{script}, args...)...)
	cmd.Dir = root
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exit):
		return string(out), exit.ExitCode()
	default:
		t.Fatalf("running contract.sh: %v", err)
		return "", -1
	}
}

func noRemoteTrace(t *testing.T, out string) {
	t.Helper()
	for _, w := range []string{hostThatDoesNotResolve, "ssh", "Could not resolve"} {
		if strings.Contains(out, w) {
			t.Errorf("the output mentions %q: the script went to the VM host before it was allowed to:\n%s", w, out)
		}
	}
}

func TestTheScriptIsSkippedWithoutTheVariable(t *testing.T) {
	out, code := runScript(t, scriptEnv(), "verify", "docker", "n")

	if code != 0 {
		t.Errorf("exit code = %d, want 0: a skipped contract run is not a failure\n%s", code, out)
	}
	if !strings.Contains(out, "skipped") {
		t.Errorf("output does not say the run was skipped:\n%s", out)
	}
	noRemoteTrace(t, out)
}

func TestTheScriptRefusesWhenCIIsSetEvenWithTheVariable(t *testing.T) {
	for _, ci := range []string{"1", "true"} {
		t.Run("CI="+ci, func(t *testing.T) {
			out, code := runScript(t, scriptEnv("DEVDESK_ENGINE_CONTRACT=1", "CI="+ci), "verify", "docker", "n")

			if code != 1 {
				t.Errorf("exit code = %d, want 1\n%s", code, out)
			}
			if !strings.Contains(out, "refused") || !strings.Contains(out, "CI") {
				t.Errorf("output does not say the run was refused because of CI:\n%s", out)
			}
			noRemoteTrace(t, out)
		})
	}
}

func TestTheScriptRefusesInvalidArgumentsBeforeTheVM(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"an engine that is not docker or podman", []string{"verify", "kind", "n"}},
		{"a line that is not n or n1", []string{"verify", "docker", "n2"}},
		{"rootless docker: the docker VM is rootful", []string{"verify", "docker", "n", "--mode", "rootless"}},
		{"a mode that is not rootful or rootless", []string{"verify", "podman", "n", "--mode", "sideways"}},
		{"an unknown action", []string{"frobnicate", "docker", "n"}},
		{"no engine", []string{"verify"}},
		{"no line", []string{"verify", "docker"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, code := runScript(t, scriptEnv("DEVDESK_ENGINE_CONTRACT=1"), tt.args...)

			if code != 2 {
				t.Errorf("exit code = %d, want 2 (usage)\n%s", code, out)
			}
			noRemoteTrace(t, out)
		})
	}
}

func TestTheScriptParses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the VM scripts are bash")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash on this machine")
	}
	script := filepath.Join(repoRoot(t), "tools", "engine-vms", "contract.sh")
	if out, err := exec.Command(bash, "-n", script).CombinedOutput(); err != nil { //nolint:gosec // a fixed script of the repository
		t.Errorf("bash -n contract.sh: %v\n%s", err, out)
	}
}

// remoteUse is what reaches the VM host or builds for it: the gate must come
// before the first of them.
var remoteUse = regexp.MustCompile(`\b(host_run|vm_root|vm_user|ssh|incus)\b|\bgo test\b|\breset\.sh\b`)

// The gate is textually before any use of the VM host, so a later edit cannot
// move a connection above it without this test saying so. Comment lines are not read.
func TestTheScriptAppliesTheGateBeforeAnyRemoteCall(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "engine-vms", "contract.sh"))
	if err != nil {
		t.Fatalf("tools/engine-vms/contract.sh: %v", err)
	}
	gate, remote := 0, 0
	for i, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if gate == 0 && strings.Contains(line, "DEVDESK_ENGINE_CONTRACT") {
			gate = i + 1
		}
		if remote == 0 && remoteUse.MatchString(line) {
			remote = i + 1
		}
	}
	switch {
	case gate == 0:
		t.Error("contract.sh never reads DEVDESK_ENGINE_CONTRACT: it applies no gate")
	case remote != 0 && remote < gate:
		t.Errorf("contract.sh:%d reaches the VM host (or compiles) before the gate at line %d", remote, gate)
	}
}
