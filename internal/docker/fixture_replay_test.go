package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-08a (DEVDESK-805) criteria 1 and 5, on the in-memory replay.
//
//	1  a fixtureRunner given the argv of a fixture returns exactly its stdout,
//	   its stderr and its exit code;
//	5  an argv with no fixture fails the replay with the argv (the rigour of
//	   stubStrict, OC-07), and a fixture nothing asked for fails at Cleanup.
//
// The runner is in fixture_runner_test.go. The seams (the runner, the current
// engine) are global: none of these tests calls t.Parallel.

// runCleanups ends a test by hand, last registered first, so the failures a
// capture holds can be read; it also puts the runner and the engine back.
func (c *capture) runCleanups() {
	for i := len(c.cleanups) - 1; i >= 0; i-- {
		c.cleanups[i]()
	}
	c.cleanups = nil
}

// exitCodeOf reads the code the way production code must: through the
// interface both *exec.ExitError and *fixtureExit satisfy.
func exitCodeOf(err error) (int, bool) {
	var coder interface{ ExitCode() int }
	if errors.As(err, &coder) {
		return coder.ExitCode(), true
	}
	return 0, false
}

// exitStderrOf reads ExitError.Stderr, which InspectContainer uses.
func exitStderrOf(err error) ([]byte, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Stderr, true
	}
	return nil, false
}

// Criterion 1: the same argv gives exactly the recorded stdout, the recorded
// exit code and the recorded stderr, under each engine.
func TestFixtureRunnerReplaysTheRecordedExchange(t *testing.T) {
	wantExit := map[string]int{engine.Docker: 1, engine.Podman: 2}

	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		f := pickFixture(t, "rm-running", enginefixture.Tool(shape.Name))
		useFixtures(t, f)

		out, err := runner.Run(dockerCmd{Args: f.Exchange.Argv})

		if !bytes.Equal(out, f.Exchange.Stdout) || len(out) != 0 {
			t.Errorf("stdout = %q, want the recorded %q (empty)", out, f.Exchange.Stdout)
		}
		if err == nil {
			t.Fatalf("a recorded exit %d returned no error", f.Exchange.Exit)
		}
		if want := fmt.Sprintf("exit status %d", wantExit[shape.Name]); err.Error() != want {
			t.Errorf("error = %q, want %q", err, want)
		}
		code, ok := exitCodeOf(err)
		if !ok || code != f.Exchange.Exit || code != wantExit[shape.Name] {
			t.Errorf("ExitCode() = %d, %v, want the recorded %d", code, ok, f.Exchange.Exit)
		}
		stderr, ok := exitStderrOf(err)
		if !ok || !bytes.Equal(stderr, f.Exchange.Stderr) || len(stderr) == 0 {
			t.Errorf("ExitError.Stderr = %q, %v, want the recorded %q", stderr, ok, f.Exchange.Stderr)
		}
	})
}

// With Combined, stdout then stderr come back in one buffer, and ExitError.Stderr
// is empty, as with CombinedOutput. The recorded exchange writes on both.
func TestFixtureRunnerCombinedReturnsStdoutThenStderr(t *testing.T) {
	usingEngine(t, engine.Docker)
	f := pickFixture(t, "port-taken", enginefixture.Docker)
	if len(f.Exchange.Stdout) == 0 || len(f.Exchange.Stderr) == 0 {
		t.Fatal("test bug: port-taken must write on both streams")
	}
	useFixtures(t, f)

	out, err := runner.Run(dockerCmd{Args: f.Exchange.Argv, Combined: true})

	want := append(slices.Clone(f.Exchange.Stdout), f.Exchange.Stderr...)
	if !bytes.Equal(out, want) {
		t.Errorf("combined output = %q, want stdout then stderr %q", out, want)
	}
	if err == nil || err.Error() != "exit status 125" {
		t.Errorf("error = %v, want exit status 125", err)
	}
	if stderr, ok := exitStderrOf(err); !ok || len(stderr) != 0 {
		t.Errorf("ExitError.Stderr = %q, %v, want it empty under Combined (as CombinedOutput leaves it)", stderr, ok)
	}
}

// Without Combined only stdout comes back, and stderr is on the error.
func TestFixtureRunnerSeparatesTheStreamsWithoutCombined(t *testing.T) {
	usingEngine(t, engine.Docker)
	f := pickFixture(t, "port-taken", enginefixture.Docker)
	useFixtures(t, f)

	out, err := runner.Run(dockerCmd{Args: f.Exchange.Argv})

	if !bytes.Equal(out, f.Exchange.Stdout) {
		t.Errorf("stdout = %q, want %q", out, f.Exchange.Stdout)
	}
	if stderr, ok := exitStderrOf(err); !ok || !bytes.Equal(stderr, f.Exchange.Stderr) {
		t.Errorf("ExitError.Stderr = %q, %v, want %q", stderr, ok, f.Exchange.Stderr)
	}
}

// Exit 0: no error, and stderr is not in the way unless Combined asks for it.
func TestFixtureRunnerExitZeroReturnsNoError(t *testing.T) {
	usingEngine(t, engine.Docker)
	f := pickFixture(t, "binary-stdout", enginefixture.Docker)
	if f.Exchange.Exit != 0 || len(f.Exchange.Stderr) == 0 {
		t.Fatal("test bug: binary-stdout is an exit 0 that also wrote on stderr")
	}

	tests := []struct {
		name     string
		combined bool
		want     []byte
	}{
		{"stdout alone, the exact bytes", false, []byte{0xff, 0x00, '\r'}},
		{"stdout then stderr", true, append([]byte{0xff, 0x00, '\r'}, f.Exchange.Stderr...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFixtures(t, f)

			out, err := runner.Run(dockerCmd{Args: f.Exchange.Argv, Combined: tt.combined})

			if err != nil {
				t.Errorf("error = %v, want nil for exit 0", err)
			}
			if !bytes.Equal(out, tt.want) {
				t.Errorf("output = %q, want %q", out, tt.want)
			}
		})
	}
}

// stdin is never recorded (`login --password-stdin` would carry a password), so
// the replay ignores it: the answer does not depend on it.
func TestFixtureRunnerIgnoresStdin(t *testing.T) {
	usingEngine(t, engine.Docker)
	f := pickFixture(t, "rm-running", enginefixture.Docker)
	useFixtures(t, f)

	out, err := runner.Run(dockerCmd{Args: f.Exchange.Argv, Stdin: "not-a-real-secret"})

	if len(out) != 0 || err == nil || err.Error() != "exit status 1" {
		t.Errorf("Run with stdin = %q, %v, want the recorded exchange all the same", out, err)
	}
}

// The replay needs no engine binary on PATH.
func TestFixtureRunnerLookPathSucceeds(t *testing.T) {
	usingEngine(t, engine.Docker)
	f := pickFixture(t, "rm-running", enginefixture.Docker)
	useFixtures(t, f)
	if err := runner.LookPath(); err != nil {
		t.Errorf("LookPath() = %v, want nil: the replay needs no binary", err)
	}
	// Ask the fixture too, so the cleanup check has nothing to report.
	_, _ = runner.Run(dockerCmd{Args: f.Exchange.Argv})
}

// ---- Criterion 5. ----

// An argv no fixture holds fails the test with the argv and the engine, like
// stubStrict does for a stub, and returns errUnexpectedArgv.
func TestAnArgvWithoutAFixtureFailsWithTheArgvAndTheEngine(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		f := pickFixture(t, "rm-running", enginefixture.Tool(shape.Name))
		c := &capture{TB: t}
		useFixtures(c, f)

		// The declared argv first, so the only report is the undeclared one.
		_, _ = runner.Run(dockerCmd{Args: f.Exchange.Argv})
		out, err := runner.Run(dockerCmd{Args: []string{"rm", "ddm-other"}})
		c.runCleanups()

		if !errors.Is(err, errUnexpectedArgv) || len(out) != 0 {
			t.Errorf("Run(undeclared) = %q, %v, want no output and errUnexpectedArgv", out, err)
		}
		if len(c.msgs) != 1 {
			t.Fatalf("reports = %q, want exactly one", c.msgs)
		}
		for _, want := range []string{`["rm" "ddm-other"]`, shape.Name} {
			if !strings.Contains(c.msgs[0], want) {
				t.Errorf("report = %q, want it to contain %s", c.msgs[0], want)
			}
		}
	})
}

// The right verb with another word is another argv: the replay does not fall
// back on the nearest fixture.
func TestARecordedVerbWithOtherWordsHasNoFixture(t *testing.T) {
	usingEngine(t, engine.Docker)
	f := pickFixture(t, "rm-running", enginefixture.Docker) // rm ddm-run
	tests := [][]string{
		{"rm"},
		{"rm", "ddm-run", "extra"},
		{"rm", "-f", "ddm-run"},
		{"ddm-run", "rm"},
	}
	for _, argv := range tests {
		t.Run(strings.Join(argv, "_"), func(t *testing.T) {
			c := &capture{TB: t}
			useFixtures(c, f)

			_, err := runner.Run(dockerCmd{Args: argv})
			c.runCleanups()

			if !errors.Is(err, errUnexpectedArgv) {
				t.Errorf("Run(%q) error = %v, want errUnexpectedArgv", argv, err)
			}
			if want := fmt.Sprintf("%q", argv); len(c.msgs) == 0 || !strings.Contains(c.msgs[0], want) {
				t.Errorf("reports = %q, want the first to quote %s", c.msgs, want)
			}
		})
	}
}

// A fixture the code under test never asked for fails when the test ends, naming
// it: the test would otherwise keep passing on a recording nothing reads.
func TestAFixtureNeverAskedForFailsAtCleanup(t *testing.T) {
	usingEngine(t, engine.Docker)
	asked := pickFixture(t, "rm-running", enginefixture.Docker)
	never := pickFixture(t, "binary-stdout", enginefixture.Docker)
	c := &capture{TB: t}
	useFixtures(c, asked, never)

	_, _ = runner.Run(dockerCmd{Args: asked.Exchange.Argv})
	if len(c.msgs) != 0 {
		t.Fatalf("a declared argv was reported: %q", c.msgs)
	}
	c.runCleanups()

	if len(c.msgs) != 1 || !strings.Contains(c.msgs[0], never.Name) {
		t.Errorf("reports = %q, want one naming %s", c.msgs, never.Name)
	}
	if strings.Contains(strings.Join(c.msgs, "\n"), asked.Name) {
		t.Errorf("reports = %q name the fixture that was asked for", c.msgs)
	}
}

// useFixtures fails at once on a selection the replay cannot serve (a pending
// file, two fixtures with the same key), instead of replaying half of it.
func TestUseFixturesFailsAtOnceOnAnUnusableSelection(t *testing.T) {
	a := pickFixture(t, "rm-running", enginefixture.Docker)
	pending := a
	pending.Name = "rm-pending--docker-29.10.1-rootful"
	pending.Meta.Record = enginefixture.RecordPending
	dup := a
	dup.Name = "rm-again--docker-29.10.1-rootful"

	for name, in := range map[string][]enginefixture.Fixture{
		"none":      nil,
		"a pending": {a, pending},
		"a twin":    {a, dup},
	} {
		t.Run(name, func(t *testing.T) {
			fatal := &fatalRecorder{TB: t}
			func() {
				defer func() {
					if r := recover(); r != nil && r != errFatal {
						panic(r)
					}
				}()
				useFixtures(fatal, in...)
			}()
			for i := len(fatal.cleanups) - 1; i >= 0; i-- {
				fatal.cleanups[i]() // put the runner back if the helper went on
			}
			if !fatal.failed {
				t.Error("useFixtures went on with a selection NewReplay refuses")
			}
		})
	}
}

// errFatal stands for the end of the goroutine Fatalf causes.
var errFatal = errors.New("fatal")

// fatalRecorder observes Fatalf without failing the run: Fatalf panics with
// errFatal, which the caller recovers, like runtime.Goexit would unwind.
type fatalRecorder struct {
	testing.TB
	failed   bool
	cleanups []func()
}

func (f *fatalRecorder) Fatalf(string, ...any) { f.failed = true; panic(errFatal) }
func (f *fatalRecorder) Cleanup(fn func())     { f.cleanups = append(f.cleanups, fn) }

// The identification calls of a credential helper have no fixture: they are
// refused with a report, never answered from another recording.
func TestFixtureRunnerRefusesACredentialHelper(t *testing.T) {
	usingEngine(t, engine.Docker)
	f := pickFixture(t, "rm-running", enginefixture.Docker)
	c := &capture{TB: t}
	useFixtures(c, f)

	_, err := runner.Run(dockerCmd{Args: f.Exchange.Argv, Helper: "ecr-login"})
	c.runCleanups()

	if err == nil {
		t.Error("a helper call was answered")
	}
	if len(c.msgs) == 0 || !strings.Contains(c.msgs[0], "ecr-login") {
		t.Errorf("reports = %q, want the first to name the helper", c.msgs)
	}
}

// Like exec.Cmd.Start: a context already cancelled starts nothing.
func TestFixtureRunnerStartsNothingOnACancelledContext(t *testing.T) {
	usingEngine(t, engine.Docker)
	f := pickFixture(t, "rm-running", enginefixture.Docker)
	c := &capture{TB: t}
	useFixtures(c, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out, err := runner.Run(dockerCmd{Args: f.Exchange.Argv, Ctx: ctx})
	c.runCleanups()

	if len(out) != 0 || !errors.Is(err, context.Canceled) {
		t.Errorf("Run(cancelled) = %q, %v, want no output and context.Canceled", out, err)
	}
	for _, m := range c.msgs {
		if strings.Contains(m, "no fixture") {
			t.Errorf("a cancelled call was reported as an unknown argv: %q", m)
		}
	}
}

// Build is the other way in (launch and exec hand the command to the terminal):
// it is checked with the same rigour, and the command it returns cannot reach a
// real engine.
func TestFixtureRunnerChecksBuildToo(t *testing.T) {
	usingEngine(t, engine.Docker)
	f := pickFixture(t, "rm-running", enginefixture.Docker)
	c := &capture{TB: t}
	useFixtures(c, f)

	declared := runner.Build(f.Exchange.Argv...)
	if len(c.msgs) != 0 {
		t.Errorf("a declared Build was reported: %q", c.msgs)
	}
	if declared == nil || declared.Run() == nil {
		t.Errorf("Build returned %v: running it must fail rather than start an engine", declared)
	}

	runner.Build("rm", "ddm-other")
	c.runCleanups()

	if len(c.msgs) != 1 || !strings.Contains(c.msgs[0], `["rm" "ddm-other"]`) {
		t.Errorf("reports = %q, want one naming the undeclared Build argv", c.msgs)
	}
}

// ---- A real use: the function that reads ExitError.Stderr. ----

// InspectContainer reads the engine's diagnostic from *exec.ExitError.Stderr
// (OC-04). Through the replay it gets it from the recorded stderr and says
// nothing about "exit status".
func TestInspectContainerReadsTheRecordedDiagnostic(t *testing.T) {
	usingEngine(t, engine.Podman)
	useFixtures(t, pickFixture(t, "inspect-absent", enginefixture.Podman))

	out, err := InspectContainer("3f9d2c7a41be")

	if out != nil {
		t.Errorf("InspectContainer returned %q with an error", out)
	}
	const want = `podman inspect failed: Error: no such object: "3f9d2c7a41be"`
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
	if strings.Contains(err.Error(), "exit status") {
		t.Errorf("error = %q: the engine's own text must replace the bare exit status", err)
	}
}

func TestInspectContainerReturnsTheRecordedStdout(t *testing.T) {
	usingEngine(t, engine.Docker)
	f := pickFixture(t, "inspect-present", enginefixture.Docker)
	useFixtures(t, f)

	out, err := InspectContainer("3f9d2c7a41be")

	if err != nil || !bytes.Equal(out, f.Exchange.Stdout) {
		t.Errorf("InspectContainer = %q, %v, want the recorded stdout %q", out, err, f.Exchange.Stdout)
	}
}
