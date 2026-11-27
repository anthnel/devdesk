package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-08a (DEVDESK-805), step 4: the in-memory replay of a recorded fixture.
//
// fixtureRunner is a dockerRunner (the interface is not exported, so it lives
// here, not in internal/enginefixture). It answers every Run from a selection of
// fixtures instead of starting a process: the same bytes, the same exit code and
// the same stderr a recording holds, without a daemon and without a process.
// It is a test helper, so it is a _test.go file: guard G2 reads production
// files only, and nothing here starts the engine.
//
// What differs from a real process, and is measured by the equivalence tests
// (fixture_equivalence_test.go) rather than assumed:
//   - the error of a non-zero exit is a *fixtureExit, not an *exec.ExitError (the
//     latter cannot carry a code outside a real process); errors.As to
//     **exec.ExitError works and yields an ExitError that holds Stderr, but its
//     ProcessState is nil, so its ExitCode() is -1 and its Error() is "<nil>".
//     Read the code through interface{ ExitCode() int }, which both satisfy;
//   - Combined returns stdout then stderr. A real engine may interleave them;
//   - stdin is never recorded and is ignored;
//   - a real ExitError.Stderr is truncated by Go above 64 KiB, this one is not.

// fixtureRunner replays fixtures. It reports through t, like stubStrict. The code
// under test calls the engine from several goroutines (Bubble Tea Cmds), so the
// journal of calls is guarded; the replay itself is safe for concurrent use.
type fixtureRunner struct {
	t      testing.TB
	replay *enginefixture.Replay
	mu     sync.Mutex
	calls  []dockerCmd
}

// record appends one call to the journal.
func (f *fixtureRunner) record(dc dockerCmd) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, dc)
}

// fixtureExit is the error of a replayed non-zero exit.
type fixtureExit struct {
	code   int
	stderr []byte
}

func (e *fixtureExit) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// ExitCode is what *exec.ExitError offers too: callers that classify a failure
// by its code go through interface{ ExitCode() int }.
func (e *fixtureExit) ExitCode() int { return e.code }

// As makes errors.As(err, &exitErr) work for an *exec.ExitError target, which
// the production code uses to read the engine's own diagnostic (InspectContainer).
func (e *fixtureExit) As(target any) bool {
	if p, ok := target.(**exec.ExitError); ok {
		*p = &exec.ExitError{Stderr: e.stderr}
		return true
	}
	return false
}

// useFixtures installs a fixtureRunner for the test. It fails the test at once
// when the selection cannot be replayed (none, a pending file, a duplicate key),
// and when the test ends it fails for every fixture nothing asked for, with the
// same rigour as stubStrict. It restores the runner and the engine.
func useFixtures(t testing.TB, fixtures ...enginefixture.Fixture) *fixtureRunner {
	t.Helper()
	replay, err := enginefixture.NewReplay(fixtures...)
	if err != nil {
		t.Fatalf("useFixtures: %v", err)
	}
	r := &fixtureRunner{t: t, replay: replay}
	stub(t, &stubRunner{}) // only for its restore of the runner and the engine; replaced just below
	runner = r
	t.Cleanup(func() {
		for _, name := range replay.Unused() {
			t.Errorf("fixture was never asked for: %s", name)
		}
	})
	return r
}

func (f *fixtureRunner) LookPath() error { return nil }

// answer finds the fixture of the engine in use for argv, or reports the argv.
func (f *fixtureRunner) answer(argv []string) (enginefixture.Fixture, bool) {
	f.t.Helper()
	name := engine.Current().Name
	fx, ok := f.replay.Answer(enginefixture.Tool(name), argv)
	if !ok {
		f.t.Errorf("no fixture for %s %q: %v", name, argv, errUnexpectedArgv)
	}
	return fx, ok
}

func (f *fixtureRunner) Run(dc dockerCmd) ([]byte, error) {
	f.t.Helper()
	f.record(dc)
	if dc.Helper != "" {
		f.t.Errorf("a credential helper (%q) is not replayed: its identification calls have no fixture", dc.Helper)
		return nil, errUnexpectedArgv
	}
	// Like exec.Cmd.Start: a context that is already done never starts anything.
	if dc.Ctx != nil && dc.Ctx.Err() != nil {
		return nil, dc.Ctx.Err()
	}
	fx, ok := f.answer(dc.Args)
	if !ok {
		return nil, errUnexpectedArgv
	}
	x := fx.Exchange
	out := append([]byte(nil), x.Stdout...)
	if dc.Combined {
		out = append(out, x.Stderr...)
	}
	if x.Exit == 0 {
		return out, nil
	}
	if dc.Combined {
		// CombinedOutput leaves ExitError.Stderr empty.
		return out, &fixtureExit{code: x.Exit}
	}
	return out, &fixtureExit{code: x.Exit, stderr: append([]byte(nil), x.Stderr...)}
}

// Build checks the argv with the same rigour as Run and returns a command that
// cannot reach a real engine: it names a path that does not exist.
func (f *fixtureRunner) Build(args ...string) *exec.Cmd {
	f.t.Helper()
	f.record(dockerCmd{Args: args})
	f.answer(args)
	return exec.Command(filepath.Join(f.t.TempDir(), "no-such-engine"), args...) //nolint:gosec // a path that does not exist, on purpose
}

// usingEngine pins the engine for one test and puts the previous one back when
// the test ends, which useEngine alone does not.
func usingEngine(t *testing.T, name string) {
	t.Helper()
	previous := engine.Current()
	t.Cleanup(func() { engine.SetCurrent(previous) })
	useEngine(t, name)
}

// syntheticFixtures loads the synthetic campaign of internal/enginefixture.
func syntheticFixtures(t testing.TB) []enginefixture.Fixture {
	t.Helper()
	fixtures, err := enginefixture.LoadFS(os.DirFS(filepath.Join("..", "enginefixture", "testdata")), "synthetic")
	if err != nil {
		t.Fatalf("loading the synthetic fixtures: %v", err)
	}
	return fixtures
}

// pickFixture returns the synthetic fixture of a scenario for a tool.
func pickFixture(t testing.TB, scenario string, tool enginefixture.Tool) enginefixture.Fixture {
	t.Helper()
	for _, f := range syntheticFixtures(t) {
		if f.Scenario == scenario && f.Meta.Tool == tool {
			return f
		}
	}
	t.Fatalf("no synthetic fixture %q for %s", scenario, tool)
	return enginefixture.Fixture{}
}
