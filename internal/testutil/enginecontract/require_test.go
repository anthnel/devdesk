package enginecontract_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/engineguard"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09a1, criteria 1 and 5: Require is what the requireEngine(t) of every
// package calls first. In order: Decide (Skip or Fatal), engineguard.AllowRealEngine,
// the run id, the probe (Fatal when the engine does not answer), the supported
// versions (record: a version outside N and N-1 is Fatal; verify: Skip, no fixture
// can exist for it), then the banner, in t.Logf and once per process on stderr.
//
//	func Require(t testing.TB, probe Probe) *Session
//	type Session struct {
//		Engine Engine
//		RunID  string
//		Mode   Mode
//		OutDir string
//	}
//
// Nothing here starts an engine: the probe is a function of the test, and the
// gate decides before it is called.

// endOfTest is what a captured Skip or Fatal panics with, as runtime.Goexit would
// end a real test: the code after it does not run.
type endOfTest struct{}

// captureT stands for the *testing.T Require receives. Skip and Fatal are
// recorded instead of ending the real test; every other method is the real
// test's (TempDir, Setenv, Name). The cleanups go to the real test, so an
// allowance AllowRealEngine gives ends with the subtest that called Require.
type captureT struct {
	testing.TB
	skipped, failed bool
	msg             string
	logs            []string
}

func (c *captureT) Helper()          {}
func (c *captureT) Cleanup(f func()) { c.TB.Cleanup(f) }
func (c *captureT) Log(args ...any)  { c.logs = append(c.logs, fmt.Sprint(args...)) }
func (c *captureT) Logf(format string, args ...any) {
	c.logs = append(c.logs, fmt.Sprintf(format, args...))
}
func (c *captureT) Skip(args ...any) { c.skip(fmt.Sprint(args...)) }
func (c *captureT) Skipf(format string, args ...any) {
	c.skip(fmt.Sprintf(format, args...))
}
func (c *captureT) SkipNow() { c.skip("") }
func (c *captureT) skip(msg string) {
	c.skipped, c.msg = true, msg
	panic(endOfTest{})
}
func (c *captureT) Fatal(args ...any) { c.fatal(fmt.Sprint(args...)) }
func (c *captureT) Fatalf(format string, args ...any) {
	c.fatal(fmt.Sprintf(format, args...))
}
func (c *captureT) FailNow() { c.fatal("") }
func (c *captureT) fatal(msg string) {
	c.failed, c.msg = true, msg
	panic(endOfTest{})
}
func (c *captureT) Error(args ...any) { c.failed, c.msg = true, fmt.Sprint(args...) }
func (c *captureT) Errorf(format string, args ...any) {
	c.failed, c.msg = true, fmt.Sprintf(format, args...)
}
func (c *captureT) Fail() { c.failed = true }

// requireOutcome is what a call to Require did.
type requireOutcome struct {
	session *enginecontract.Session
	c       *captureT
	probed  []enginefixture.Tool
}

// callRequire runs Require under a captureT with a probe that records its calls
// and answers with engine (or err).
func callRequire(t testing.TB, engine enginecontract.Engine, err error) requireOutcome {
	t.Helper()
	out := requireOutcome{c: &captureT{TB: t}}
	probe := func(tool enginefixture.Tool) (enginecontract.Engine, error) {
		out.probed = append(out.probed, tool)
		return engine, err
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(endOfTest); !ok {
					panic(r)
				}
			}
		}()
		out.session = enginecontract.Require(out.c, probe)
	}()
	return out
}

// openGate sets the variables a contract run has: the gate open, no CI, an
// engine, a run id and a versions file with the synthetic lines above.
func openGate(t *testing.T, tool enginefixture.Tool) {
	t.Helper()
	versions := filepath.Join(t.TempDir(), "versions.env")
	if err := os.WriteFile(versions, []byte(versionsText), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(enginecontract.RunEnv, "20261127-a1b2c3d4")
	t.Setenv(enginecontract.VersionsEnv, versions)
	t.Setenv(enginecontract.ToolEnv, string(tool))
	t.Setenv(enginecontract.RecordingEnv, "")
	t.Setenv(enginecontract.OutEnv, "")
	t.Setenv(engineguard.ContractEnv, "1")
	clearCIMarkers(t)
	clearEngineEnv(t) // the engine of the developer's shell must not decide what Require does
}

var podmanN = enginecontract.Engine{Tool: enginefixture.Podman, Version: "6.2.1", Mode: enginefixture.Rootless}

func TestRequireSkipsWhenTheGateIsClosedAndNeverProbes(t *testing.T) {
	for _, value := range []string{"", "0", "true"} {
		t.Run("variable "+value, func(t *testing.T) {
			openGate(t, enginefixture.Podman)
			t.Setenv(engineguard.ContractEnv, value)

			got := callRequire(t, podmanN, nil)

			if !got.c.skipped || got.c.failed {
				t.Fatalf("Require skipped = %v, failed = %v (%q), want a skip", got.c.skipped, got.c.failed, got.c.msg)
			}
			if !strings.Contains(got.c.msg, "DEVDESK_ENGINE_CONTRACT=1") {
				t.Errorf("skip message = %q, want it to say how to run the contracts", got.c.msg)
			}
			if len(got.probed) != 0 {
				t.Errorf("the probe was called %d time(s) with the gate closed: nothing reaches the engine before the gate", len(got.probed))
			}
			if got.session != nil {
				t.Error("Require returned a session after a skip")
			}
		})
	}
}

func TestRequireIsFatalInCIAndNeverProbes(t *testing.T) {
	for _, ci := range []string{"true", "1"} {
		t.Run("CI="+ci, func(t *testing.T) {
			openGate(t, enginefixture.Podman)
			t.Setenv("CI", ci)

			got := callRequire(t, podmanN, nil)

			if !got.c.failed || got.c.skipped {
				t.Fatalf("Require failed = %v, skipped = %v, want a refusal that fails the test", got.c.failed, got.c.skipped)
			}
			for _, w := range []string{"refused", "CI"} {
				if !strings.Contains(got.c.msg, w) {
					t.Errorf("fatal message = %q, want it to contain %q", got.c.msg, w)
				}
			}
			if len(got.probed) != 0 {
				t.Errorf("the probe was called %d time(s) in CI: the refusal comes before any engine call", len(got.probed))
			}
		})
	}
}

func TestRequireIsFatalWithoutAnEngineAndNeverProbes(t *testing.T) {
	for _, tool := range []string{"", "kind"} {
		t.Run("tool "+tool, func(t *testing.T) {
			openGate(t, enginefixture.Tool(tool))

			got := callRequire(t, podmanN, nil)

			if !got.c.failed || !strings.Contains(got.c.msg, "DEVDESK_ENGINE_CONTRACT_TOOL") {
				t.Errorf("failed = %v, message %q, want a refusal that names the tool variable", got.c.failed, got.c.msg)
			}
			if len(got.probed) != 0 {
				t.Errorf("the probe was called with no valid engine named")
			}
		})
	}
}

func TestRequireIsFatalWhenTheProbeFails(t *testing.T) {
	openGate(t, enginefixture.Podman)

	got := callRequire(t, enginecontract.Engine{}, errors.New("cannot reach the socket"))

	if !got.c.failed || got.c.skipped {
		t.Fatalf("failed = %v, skipped = %v, want a failure: the engine does not answer", got.c.failed, got.c.skipped)
	}
	if !strings.Contains(got.c.msg, "cannot reach the socket") {
		t.Errorf("fatal message = %q, want it to cite the probe's error", got.c.msg)
	}
}

func TestRequireProbesTheEngineTheGateNamed(t *testing.T) {
	tests := []enginecontract.Engine{
		{Tool: enginefixture.Docker, Version: "29.10.1", Mode: enginefixture.Rootful},
		{Tool: enginefixture.Podman, Version: "6.2.1", Mode: enginefixture.Rootful},
	}
	for _, engine := range tests {
		t.Run(string(engine.Tool), func(t *testing.T) {
			openGate(t, engine.Tool)

			got := callRequire(t, engine, nil)

			if got.c.failed || got.c.skipped {
				t.Fatalf("Require failed = %v, skipped = %v (%q)", got.c.failed, got.c.skipped, got.c.msg)
			}
			if len(got.probed) != 1 || got.probed[0] != engine.Tool {
				t.Errorf("the probe was called with %v, want once with %q", got.probed, engine.Tool)
			}
		})
	}
}

func TestRequireChecksTheVersionAgainstNAndN1(t *testing.T) {
	outside := enginecontract.Engine{Tool: enginefixture.Podman, Version: "6.3.0", Mode: enginefixture.Rootless}

	t.Run("record mode refuses a version outside the two lines", func(t *testing.T) {
		openGate(t, enginefixture.Podman)
		t.Setenv(enginecontract.RecordingEnv, "1")
		t.Setenv(enginecontract.OutEnv, filepath.Join(t.TempDir(), "out"))

		got := callRequire(t, outside, nil)

		if !got.c.failed || got.c.skipped {
			t.Fatalf("failed = %v, skipped = %v, want a failure: a fixture of an unsupported version must not be recorded", got.c.failed, got.c.skipped)
		}
		for _, w := range []string{"6.3.0", "6.2.1, 5.8.11"} {
			if !strings.Contains(got.c.msg, w) {
				t.Errorf("fatal message = %q, want it to contain %q", got.c.msg, w)
			}
		}
	})

	t.Run("verify mode skips it: no fixture can exist for that version", func(t *testing.T) {
		openGate(t, enginefixture.Podman)

		got := callRequire(t, outside, nil)

		if !got.c.skipped || got.c.failed {
			t.Fatalf("skipped = %v, failed = %v (%q), want a skip", got.c.skipped, got.c.failed, got.c.msg)
		}
		if !strings.Contains(got.c.msg, "6.3.0") {
			t.Errorf("skip message = %q, want it to name the version", got.c.msg)
		}
	})
}

func TestRequireReturnsTheSession(t *testing.T) {
	t.Run("verify", func(t *testing.T) {
		openGate(t, enginefixture.Podman)

		got := callRequire(t, podmanN, nil)

		if got.c.failed || got.c.skipped || got.session == nil {
			t.Fatalf("failed = %v, skipped = %v (%q), session = %v", got.c.failed, got.c.skipped, got.c.msg, got.session)
		}
		if *got.session != (enginecontract.Session{Engine: podmanN, RunID: "20261127-a1b2c3d4", Mode: enginecontract.Verify}) {
			t.Errorf("Session = %+v, want the engine probed, the run id of the environment and the verify mode", *got.session)
		}
		want := enginecontract.Banner(podmanN, "20261127-a1b2c3d4", enginecontract.Verify)
		if !contains(got.c.logs, want) {
			t.Errorf("logs = %q, want the banner %q", got.c.logs, want)
		}
	})

	t.Run("record", func(t *testing.T) {
		openGate(t, enginefixture.Podman)
		out := filepath.Join(t.TempDir(), "out")
		t.Setenv(enginecontract.RecordingEnv, "1")
		t.Setenv(enginecontract.OutEnv, out)

		got := callRequire(t, podmanN, nil)

		if got.c.failed || got.c.skipped || got.session == nil {
			t.Fatalf("failed = %v, skipped = %v (%q), session = %v", got.c.failed, got.c.skipped, got.c.msg, got.session)
		}
		if got.session.Mode != enginecontract.Record || got.session.OutDir != out {
			t.Errorf("Session = %+v, want the record mode and the output directory %q", *got.session, out)
		}
		if want := enginecontract.Banner(podmanN, "20261127-a1b2c3d4", enginecontract.Record); !contains(got.c.logs, want) {
			t.Errorf("logs = %q, want the banner %q", got.c.logs, want)
		}
	})
}

func contains(logs []string, want string) bool {
	for _, l := range logs {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

// reach is a call of the runner's first statement on an engine binary outside every
// fakecli directory; it returns what Check panicked with, nil when it let it pass.
func reach(binary string) (recovered any) {
	defer func() { recovered = recover() }()
	engineguard.Check("Run", binary)
	return nil
}

// An accepted Require is engineguard.AllowRealEngine too (a second, independent
// check of the same gate): the reaches of the test are no longer refused, and they
// are again when the test is over. The binary is an empty file, never run.
func TestRequireAllowsTheRealEngineForTheLengthOfTheTestOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("an executable is recognised by its extension on Windows")
	}
	engineguard.Isolate(t) // a reach panics, whatever the mode of the suite
	binary := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(binary, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	if reach(binary) == nil {
		t.Fatal("the guard does not refuse an engine outside a fakecli directory before Require: the check below proves nothing")
	}

	t.Run("a contract test", func(t *testing.T) {
		openGate(t, enginefixture.Docker)
		got := callRequire(t, enginecontract.Engine{Tool: enginefixture.Docker, Version: "29.10.1", Mode: enginefixture.Rootful}, nil)
		if got.session == nil {
			t.Fatalf("Require did not accept: %q", got.c.msg)
		}
		if r := reach(binary); r != nil {
			t.Errorf("a reach panicked inside an accepted contract test: %v", r)
		}
	})

	if reach(binary) == nil {
		t.Error("the allowance outlived the contract test: AllowRealEngine is for the length of one test")
	}
}

// A refusal must not leave the guard open either.
func TestARefusedRequireDoesNotAllowTheRealEngine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("an executable is recognised by its extension on Windows")
	}
	engineguard.Isolate(t)
	binary := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(binary, nil, 0o700); err != nil {
		t.Fatal(err)
	}

	t.Run("CI", func(t *testing.T) {
		openGate(t, enginefixture.Docker)
		t.Setenv("CI", "true")
		_ = callRequire(t, podmanN, nil)
		if reach(binary) == nil {
			t.Error("a reach did not panic after a refused Require")
		}
	})
}
