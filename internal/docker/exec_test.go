package docker

import (
	"errors"
	"os/exec"
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// errExit stands in for the failure of a docker invocation.
var errExit = errors.New("exit status 1")

// errUnexpectedArgv is what a strict stub returns for an argv no reply names.
var errUnexpectedArgv = errors.New("unexpected argv")

// reply is one invocation a strict stub accepts, matched on the whole argv.
type reply struct {
	argv []string
	out  string
	err  error
}

// reporter is the part of testing.TB a strict stub fails through; a test of the
// stub itself passes a recorder instead of the real *testing.T.
type reporter interface {
	Helper()
	Errorf(format string, args ...any)
}

// stubRunner records the invocations it receives and replays canned output.
//
// It works in one of two modes. Without a report it is keyed by the first
// argument (the docker subcommand) so a test can stub several calls made by one
// exported function — ListImages, for instance, issues "image" and "system"
// calls — whatever flags or template the call carries. That is the mode of the
// tests not yet moved to the strict one (OC-07 made strictness opt-in).
//
// With a report it is strict: only the argv listed in replies are accepted,
// compared whole, and any other fails the test naming it.
type stubRunner struct {
	// replies are the invocations a strict stub accepts. Read only when report
	// is set.
	replies []reply
	// report makes the stub strict: Run and Build fail through it for an argv
	// no reply names.
	report reporter
	// used marks the replies a call has matched, for the check stubStrict runs
	// at the end of the test.
	used []bool
	// output maps a subcommand to the bytes it should return.
	output map[string][]byte
	// err maps a subcommand to the error it should return.
	err map[string]error
	// missing makes LookPath fail, simulating a host without Docker.
	missing bool
	// calls records every invocation in order.
	calls []dockerCmd
}

func (s *stubRunner) LookPath() error {
	if s.missing {
		return errors.New("executable file not found in $PATH")
	}
	return nil
}

func (s *stubRunner) Build(args ...string) *exec.Cmd {
	s.calls = append(s.calls, dockerCmd{Args: args})
	if s.report != nil {
		s.match(args)
	}
	return exec.Command("docker", args...)
}

// match finds the reply declared for exactly argv. It reports the argv through
// s.report when there is none, quoted with %q so the words the code under test
// passed, and their exact template, are readable.
func (s *stubRunner) match(argv []string) (reply, bool) {
	s.report.Helper()
	if len(s.used) != len(s.replies) {
		s.used = make([]bool, len(s.replies))
	}
	for i, r := range s.replies {
		if slices.Equal(r.argv, argv) {
			s.used[i] = true
			return r, true
		}
	}
	s.report.Errorf("%v: %q", errUnexpectedArgv, argv)
	return reply{}, false
}

func (s *stubRunner) Run(dc dockerCmd) ([]byte, error) {
	s.calls = append(s.calls, dc)
	if s.report != nil {
		r, ok := s.match(dc.Args)
		if !ok {
			return nil, errUnexpectedArgv
		}
		return []byte(r.out), r.err
	}
	key := ""
	if len(dc.Args) > 0 {
		key = dc.Args[0]
	}
	if err, ok := s.err[key]; ok {
		return s.output[key], err
	}
	return s.output[key], nil
}

// lastArgs returns the arguments of the most recent invocation.
func (s *stubRunner) lastArgs() []string {
	if len(s.calls) == 0 {
		return nil
	}
	return s.calls[len(s.calls)-1].Args
}

// stub installs a stubRunner for the duration of the test. Tests using it must
// not call t.Parallel: the runner is package-level state.
//
// It restores the resolved engine too. A test that pins podman would otherwise
// leak it into every test that ran after it, and the symptom — a label reading
// "podman stop" in a test about something else — would point nowhere near the
// test that caused it.
func stub(t testing.TB, s *stubRunner) *stubRunner {
	t.Helper()
	previous := runner
	previousEngine := engine.Current()
	runner = s
	t.Cleanup(func() {
		runner = previous
		engine.SetCurrent(previousEngine)
	})
	return s
}

// stubStrict is stub for a strict stubRunner reporting through t: any argv the
// replies do not name fails the test, and so does a reply the code under test
// never asked for (checked at cleanup), so a declared argv cannot silently go
// stale.
func stubStrict(t testing.TB, replies ...reply) *stubRunner {
	t.Helper()
	s := stub(t, &stubRunner{replies: replies, report: t})
	t.Cleanup(func() {
		for i, r := range s.replies {
			if i >= len(s.used) || !s.used[i] {
				t.Errorf("declared argv was never run: %q", r.argv)
			}
		}
	})
	return s
}

// useEngine pins the engine for the duration of the test. Always paired with
// stub, which is what restores it.
func useEngine(t *testing.T, name string) {
	t.Helper()
	engine.SetCurrent(engine.ShapeFor(name))
}

// forEachEngine runs fn once per declared engine (engine.Names()), in a subtest
// named after it, with useEngine already applied, and puts the original engine
// back when the subtests are over. A test whose result depends on the engine
// (an argv, a template, a message naming the binary) goes through it, with
// stubStrict, instead of pinning one engine.
//
// OC-07 criterion 4, an engine added to the configuration must fail a test
// until it is run here, is held by TestForEachEngineCoversEveryConfiguredEngine
// (foreach_test.go), which compares the engines this helper visits with
// config.ContainerEngines(). TestEveryConfiguredEngineHasAShape
// (internal/engine) cannot stand in for it: it does not see this helper.
//
// Like stub, it is for tests that do not call t.Parallel.
func forEachEngine(t *testing.T, fn func(t *testing.T, shape engine.Shape)) {
	t.Helper()
	// Registered before any fn runs, so a subtest that fails or calls t.Fatal
	// cannot skip the restore.
	previous := engine.Current()
	t.Cleanup(func() { engine.SetCurrent(previous) })
	for _, name := range engine.Names() {
		t.Run(name, func(t *testing.T) {
			useEngine(t, name)
			fn(t, engine.ShapeFor(name))
		})
	}
}

// stubOutput is the common case: one subcommand returning one canned payload.
func stubOutput(t *testing.T, subcommand, output string) *stubRunner {
	t.Helper()
	return stub(t, &stubRunner{output: map[string][]byte{subcommand: []byte(output)}})
}

func TestRequireDockerReportsMissingBinary(t *testing.T) {
	stub(t, &stubRunner{missing: true})

	err := engineAvailable()

	if err == nil {
		t.Fatal("engineAvailable() returned nil when docker is absent")
	}
	// The message reaches the footer under Rule 128, so it must name the cause.
	if got := err.Error(); got != "docker not found: executable file not found in $PATH" {
		t.Errorf("engineAvailable() = %q, want it to name the missing binary", got)
	}
}

func TestMutateSurfacesCombinedOutputOnFailure(t *testing.T) {
	// Docker writes its diagnostic to stderr, so a failing mutation must report
	// the combined output rather than the bare exit status.
	s := stub(t, &stubRunner{
		output: map[string][]byte{"stop": []byte("Error response from daemon: no such container\n")},
		err:    map[string]error{"stop": errors.New("exit status 1")},
	})

	err := StopContainer("0123456789ab")

	if err == nil {
		t.Fatal("StopContainer() returned nil on a failing invocation")
	}
	want := "docker stop failed: Error response from daemon: no such container"
	if err.Error() != want {
		t.Errorf("StopContainer() = %q, want %q", err.Error(), want)
	}
	if !s.calls[0].Combined {
		t.Error("mutating call did not request combined output, so stderr would be lost")
	}
}

func TestSplitLinesDropsBlanks(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int
	}{
		{"empty output yields no lines", "", 0},
		{"whitespace only yields no lines", "  \n\t\n", 0},
		{"blank interior lines are dropped", "a\n\nb\n", 2},
		{"trailing newline does not add a line", "a\nb\n", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitLines([]byte(tt.output)); len(got) != tt.want {
				t.Errorf("splitLines(%q) = %v (%d lines), want %d", tt.output, got, len(got), tt.want)
			}
		})
	}
}
