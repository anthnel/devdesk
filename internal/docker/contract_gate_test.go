package docker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/engineguard"
)

// OC-09a1 (DEVDESK-806), criteria 1 and 5: outside a contract run, no contract
// test of this package runs. The proof is a child process: this test binary is run
// again, with the contract tests selected the way contract.sh selects them
// (-test.run '^TestContract'), in an environment cleaned of every variable that
// steers the gate.
//
//	no DEVDESK_ENGINE_CONTRACT       every contract is SKIP, with the message, exit 0,
//	                                 no PASS and no FAIL (criteria 1 and 5)
//	CI=true and the variable         every contract is FAIL, "refused", exit non-zero,
//	                                 and no line "engineguard:" nor "panic:": the
//	                                 refusal comes before any call to the engine
//
// The name of this test does not start with TestContract: the child would select
// it, and a TestContract* function outside a *_contract_test.go file breaks G5 (R3).
//
// Safety: the PATH of the child is an empty directory, so that even a gate that
// opened by mistake would find no engine; nothing here reaches the real docker or
// podman of the machine.

// childEnvironment is the environment of this process without the variables that
// steer the gate, the fake CLI, the recording mode and the PATH, then the given
// pairs, and a PATH that holds nothing.
func childEnvironment(t *testing.T, kv ...string) []string {
	t.Helper()
	var out []string
	for _, e := range os.Environ() {
		name, _, _ := strings.Cut(e, "=")
		switch {
		case slices.Contains(engineguard.CIMarkers(), name), name == "PATH", strings.HasPrefix(name, "DEVDESK_ENGINE_"),
			strings.HasPrefix(name, "DEVDESK_FIXTURE_"), strings.HasPrefix(name, "FAKECLI_"):
			continue
		}
		out = append(out, e)
	}
	out = append(out, "PATH="+t.TempDir())
	return append(out, kv...)
}

// runChild runs this test binary with args and returns its combined output and
// exit code.
func runChild(t *testing.T, env []string, args ...string) (string, int) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, self, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exit):
		return string(out), exit.ExitCode()
	default:
		t.Fatalf("running the test binary again: %v", err)
		return "", -1
	}
}

// listedContracts is what `-test.list ^TestContract` names: the contracts of this
// package, at least the pilot.
func listedContracts(t *testing.T) []string {
	t.Helper()
	out, code := runChild(t, childEnvironment(t), "-test.list=^TestContract")
	if code != 0 {
		t.Fatalf("-test.list exited %d:\n%s", code, out)
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "TestContract") {
			names = append(names, strings.TrimSpace(line))
		}
	}
	if len(names) == 0 {
		t.Fatalf("no TestContract* function in this package: the check below would pass on nothing:\n%s", out)
	}
	return names
}

// verdicts reads the `--- SKIP: Name (0.00s)` lines of a -test.v run.
func verdicts(out, kind string) map[string]bool {
	found := map[string]bool{}
	re := regexp.MustCompile(`(?m)^--- ` + kind + `: (\S+) \(`)
	for _, m := range re.FindAllStringSubmatch(out, -1) {
		found[m[1]] = true
	}
	return found
}

func TestEveryContractIsSkippedOrRefusedOutsideAContractRun(t *testing.T) {
	contracts := listedContracts(t)
	run := []string{"-test.run=^TestContract", "-test.v", "-test.count=1"}

	t.Run("without the variable, skipped with a message", func(t *testing.T) {
		out, code := runChild(t, childEnvironment(t), run...)

		if code != 0 {
			t.Errorf("exit code = %d, want 0: a skipped contract is not a failure\n%s", code, out)
		}
		skipped := verdicts(out, "SKIP")
		for _, name := range contracts {
			if !skipped[name] {
				t.Errorf("%s was not reported as skipped:\n%s", name, out)
			}
		}
		for _, kind := range []string{"PASS", "FAIL"} {
			if got := verdicts(out, kind); len(got) != 0 {
				t.Errorf("contracts reported as %s with the gate closed: %v\n%s", kind, got, out)
			}
		}
		if !strings.Contains(out, "DEVDESK_ENGINE_CONTRACT=1") {
			t.Errorf("the skip does not say how to run the contracts:\n%s", out)
		}
	})

	t.Run("in CI, refused even with the variable, before any call to the engine", func(t *testing.T) {
		env := childEnvironment(t, "CI=true", "DEVDESK_ENGINE_CONTRACT=1", "DEVDESK_ENGINE_CONTRACT_TOOL=docker")

		out, code := runChild(t, env, run...)

		if code == 0 {
			t.Errorf("exit code = 0, want non-zero: a refused contract fails\n%s", out)
		}
		failed := verdicts(out, "FAIL")
		for _, name := range contracts {
			if !failed[name] {
				t.Errorf("%s was not reported as failed:\n%s", name, out)
			}
		}
		for _, kind := range []string{"PASS", "SKIP"} {
			if got := verdicts(out, kind); len(got) != 0 {
				t.Errorf("contracts reported as %s in CI: %v\n%s", kind, got, out)
			}
		}
		for _, want := range []string{"refused", "CI"} {
			if !strings.Contains(out, want) {
				t.Errorf("the output does not contain %q:\n%s", want, out)
			}
		}
		for _, banned := range []string{"engineguard:", "panic:"} {
			if strings.Contains(out, banned) {
				t.Errorf("the output contains %q: the refusal must come before the engine is reached:\n%s", banned, out)
			}
		}
	})

	t.Run("outside CI with the variable but no engine named, refused", func(t *testing.T) {
		env := childEnvironment(t, "DEVDESK_ENGINE_CONTRACT=1")

		out, code := runChild(t, env, run...)

		if code == 0 {
			t.Errorf("exit code = 0, want non-zero\n%s", out)
		}
		failed := verdicts(out, "FAIL")
		for _, name := range contracts {
			if !failed[name] {
				t.Errorf("%s was not reported as failed:\n%s", name, out)
			}
		}
		if !strings.Contains(out, "DEVDESK_ENGINE_CONTRACT_TOOL") {
			t.Errorf("the refusal does not name DEVDESK_ENGINE_CONTRACT_TOOL:\n%s", out)
		}
		if strings.Contains(out, "panic:") {
			t.Errorf("the contract panicked instead of being refused:\n%s", out)
		}
	})
}
