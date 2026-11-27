package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09a2 (DEVDESK-943): the helpers of the tests of the recording runner, the argv
// rule, the labelled resources and the targeted cleanup. None of them starts an
// engine: a runner here is a strict stub that answers from a script, or a fakecli
// tool whose PATH holds nothing else.
//
// The helpers the coder writes in contract_support_test.go (the support file of G5),
// all in package docker, in a _test.go file, with no exported function added to the
// package and no destructive or pull argv outside it (G2, G3, G8):
//
//	// contract gets two fields besides t and s, and t becomes a testing.TB so that
//	// a test can hold its Fatal and its Errorf.
//	type contract struct {
//		t    testing.TB
//		s    *enginecontract.Session
//		rec  *recordingRunner  // nil until startContract installs it, or sibling makes it
//		ours map[string]string // id or name of a resource of this run -> role
//	}
//
//	func newContract(t testing.TB, s *enginecontract.Session) *contract // no engine, no runner swap, nothing registered
//	func startContract(t testing.TB, s *enginecontract.Session) *contract
//	    // what requireEngine does once Require accepted the run: pins s.Engine.Tool, builds
//	    // the contract, installs its recorder as `runner`, and registers on t, in this
//	    // order, "put the runner and the engine back" and then c.cleanup (t.Cleanup is
//	    // last in, first out, so the cleanup runs first and still has the recorder).
//	    // requireEngine is `return startContract(t, enginecontract.Require(t, probeEngine))`.
//	func requireEngine(t *testing.T) *contract // as before
//
//	func (c *contract) requireLab()          // c.s.RequireLab(c.t)
//	func (c *contract) container(role string, cmd ...string) string // id
//	func (c *contract) volume(role string) string                   // name, as the engine printed it
//	func (c *contract) network(role string) string                  // id, or name, as the engine printed it
//	func (c *contract) cleanup()             // registered by startContract: nothing unless a creation was attempted
//	func (c *contract) cleanupNow() []string // the references it removed: containers, then networks, then volumes
//	func (c *contract) sibling(suffix string) *contract
//	    // a second contract for the run id "<run>-<suffix>", with its own recorder, its own
//	    // rule and its own `ours`, whose methods swap their recorder into `runner` for
//	    // their length. Only the self-check of the cleanup uses it, to own a resource that
//	    // carries the label of another run. It registers its own cleanup on c.t.
//	func (c *contract) exchange(campaign, scenario, text string, call func()) enginefixture.Exchange
//	    // a2: exactly one engine exchange must happen in call, else c.t.Fatalf; returns it
//	    // (argv, exit code, stdout, stderr) and logs argv and code; no Record, no Verify yet.
//
//	type exchangeLog struct { Argv []string; Combined bool; Exit int; Stdout, Stderr []byte }
//	type recordingRunner struct { /* t testing.TB; allow func([]string) error; mu; log []exchangeLog */ }
//	func newRecordingRunner(t testing.TB, allow func(argv []string) error) *recordingRunner
//	var errNotContracted = errors.New(...)
//	func contractArgvRule(runID string, ours func(ref string) bool) func(argv []string) error
//
// The names of these tests do not start with "TestContract": a function of that name
// outside a *_contract_test.go file breaks G5 (R3), and contract.sh selects
// '^TestContract'.

const (
	// labRunID is the run id of the tests, and foreignRunID the one of another run.
	labRunID     = "20261127-a1b2c3d4"
	foreignRunID = "20261127-ffffffff"
	// contractImage stands for the pinned image of the contracts; nothing pulls it.
	contractImage = "registry.invalid/ddc/sleeper@sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

// Valid container ids of 64 hex characters (G3: never "abc").
var (
	idA = strings.Repeat("a1", 32)
	idB = strings.Repeat("b2", 32)
	idC = strings.Repeat("c3", 32)
)

func labelOf(run string) string { return enginecontract.Label(run) }

// contractT stands for the *testing.T a contract holds. Errorf and Fatalf are
// recorded instead of failing the real test; a Fatal ends the function it runs in
// with a panic that run() recovers, as runtime.Goexit would end a real test.
// Cleanups are held, not run, until runCleanups: the test decides when "the test is
// over". Everything else is the real test's.
type contractT struct {
	testing.TB
	errs     []string
	fatals   []string
	cleanups []func()
}

type endOfContractTest struct{}

func (c *contractT) Helper() {}
func (c *contractT) Errorf(format string, args ...any) {
	c.errs = append(c.errs, sprintf(format, args...))
}
func (c *contractT) Error(args ...any) { c.errs = append(c.errs, sprint(args...)) }
func (c *contractT) Fatalf(format string, args ...any) {
	c.fatals = append(c.fatals, sprintf(format, args...))
	panic(endOfContractTest{})
}
func (c *contractT) Fatal(args ...any) {
	c.fatals = append(c.fatals, sprint(args...))
	panic(endOfContractTest{})
}
func (c *contractT) FailNow() { c.fatals = append(c.fatals, ""); panic(endOfContractTest{}) }
func (c *contractT) Fail()    { c.errs = append(c.errs, "") }
func (c *contractT) Failed() bool {
	return len(c.errs) > 0 || len(c.fatals) > 0
}
func (c *contractT) Cleanup(f func()) { c.cleanups = append(c.cleanups, f) }

// run calls f and reports whether it ended on a Fatal.
func (c *contractT) run(f func()) (ended bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(endOfContractTest); !ok {
				panic(r)
			}
			ended = true
		}
	}()
	f()
	return false
}

// runCleanups runs the held cleanups last in, first out, once each.
func (c *contractT) runCleanups() {
	for len(c.cleanups) > 0 {
		f := c.cleanups[len(c.cleanups)-1]
		c.cleanups = c.cleanups[:len(c.cleanups)-1]
		f()
	}
}

// queuedStep is one invocation a queuedRunner accepts, and what it answers.
type queuedStep struct {
	argv []string
	out  string
	err  error
}

// queuedRunner is a strict stub like stubStrict, with one difference that the
// cleanup needs: the same argv may be declared more than once, with different
// answers, and the declarations are consumed in order. A cleanup reads the same
// list before and after it removes; a stub that always gives the same answer
// cannot tell a clean store from a leftover.
//
// Anything not declared, or declared fewer times than it is run, fails the test
// naming the argv. A declaration nothing ran fails it at the end of the test.
type queuedRunner struct {
	t     testing.TB
	steps []queuedStep
	done  []bool
	calls []dockerCmd
}

func (q *queuedRunner) LookPath() error { return nil }

func (q *queuedRunner) Build(args ...string) *exec.Cmd {
	q.t.Errorf("Build(%q): a contract never builds an interactive command", args)
	return exec.Command("true")
}

func (q *queuedRunner) Run(dc dockerCmd) ([]byte, error) {
	q.calls = append(q.calls, dc)
	for i, s := range q.steps {
		if !q.done[i] && slices.Equal(s.argv, dc.Args) {
			q.done[i] = true
			return []byte(s.out), s.err
		}
	}
	q.t.Errorf("%v (or run more often than declared): %q", errUnexpectedArgv, dc.Args)
	return nil, errUnexpectedArgv
}

// queued installs a queuedRunner for the length of the test, puts the runner and
// the engine back, and fails the test for a declared step nothing ran.
func queued(t testing.TB, steps ...queuedStep) *queuedRunner {
	t.Helper()
	q := &queuedRunner{t: t, steps: steps, done: make([]bool, len(steps))}
	previous, previousEngine := runner, engine.Current()
	runner = q
	t.Cleanup(func() {
		runner = previous
		engine.SetCurrent(previousEngine)
		for i, s := range q.steps {
			if !q.done[i] {
				t.Errorf("declared argv was never run: %q", s.argv)
			}
		}
	})
	return q
}

// openLab sets the two marks of a lab VM for the test: the run id variable, and a
// marker file that says "n".
func openLab(t *testing.T) {
	t.Helper()
	setLab(t, true, true)
}

// setLab sets the marks the lab gate asks for, each one on its own.
func setLab(t *testing.T, marker, runVar bool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "current")
	if marker {
		if err := os.WriteFile(path, []byte("n\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	enginecontract.SetLabMarkerForTest(t, path)
	run := ""
	if runVar {
		run = labRunID
	}
	t.Setenv(enginecontract.RunEnv, run)
}

// labSession is the session of a run of the given engine, as Require would build it.
func labSession(tool enginefixture.Tool, run string) *enginecontract.Session {
	return &enginecontract.Session{
		Engine: enginecontract.Engine{Tool: tool, Version: "6.2.1", Mode: enginefixture.Rootless},
		RunID:  run,
		Mode:   enginecontract.Verify,
		// The tests of the helpers run scenarios nobody recorded: with RequireFixtures true (OC-09b2)
		// they would fail on "no recorded fixture". Require never sets this.
		MissingFixturesOK: true,
	}
}

func sprintf(format string, args ...any) string {
	return strings.TrimSpace(fmt.Sprintf(format, args...))
}
func sprint(args ...any) string { return strings.TrimSpace(fmt.Sprint(args...)) }
