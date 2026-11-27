package fakecli_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-02 (DEVDESK-797): one fake binary for docker, podman, kind and kubectl.
//
// The API these tests pin down:
//
//	fakecli.Main(m *testing.M) int                       // called from TestMain: the test binary
//	                                                     // behaves as the tool when started under its name
//	fakecli.Install(t, src Source, tools ...string) *Fake // tools alone on PATH, answering src
//	(*Fake).Dir() string                                 // the directory holding the tools
//	(*Fake).Invocations() []Invocation                   // the journal, oldest first
//	fakecli.Scenario                                     // []Exchange, the usual Source
//	fakecli.Source                                       // Exchanges() []Exchange: the extension point
//	                                                     // a fixture replay (OC-08) plugs into
//
// Install changes PATH (and the process environment) through t.Setenv, so it
// does not combine with t.Parallel.

func TestMain(m *testing.M) {
	os.Exit(fakecli.Main(m))
}

// result is what one run of a fake tool produced.
type result struct {
	stdout, stderr string
	exit           int
}

// run starts the tool by name, so it is resolved through PATH the way the code
// under test does it.
func run(t *testing.T, name string, args ...string) result {
	t.Helper()
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := result{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		res.exit = exitErr.ExitCode()
	case err != nil:
		t.Fatalf("starting %s %q: %v", name, args, err)
	}
	return res
}

// spyT records what Install reports through Errorf and runs the cleanups on
// demand, so a test can observe a failure it expects without failing itself.
type spyT struct {
	*testing.T
	errs     []string
	cleanups []func()
}

func (s *spyT) Errorf(format string, args ...any) {
	s.errs = append(s.errs, fmt.Sprintf(format, args...))
}
func (s *spyT) Error(args ...any) { s.errs = append(s.errs, fmt.Sprint(args...)) }
func (s *spyT) Cleanup(f func())  { s.cleanups = append(s.cleanups, f) }

// finish runs the registered cleanups, last registered first, as testing does.
func (s *spyT) finish() {
	for _, f := range slices.Backward(s.cleanups) {
		f()
	}
	s.cleanups = nil
}

// Criterion 1: stdout, stderr and the exit code come from the declared scenario.
func TestRepliesComeFromTheScenario(t *testing.T) {
	cases := []struct {
		name  string
		sc    fakecli.Scenario
		tool  string
		argv  []string
		want  result
		tools []string
	}{
		{
			name: "stdout only, exit 0",
			sc: fakecli.Scenario{
				{Tool: "docker", Args: []string{"image", "ls"}, Reply: fakecli.Reply{Stdout: "nginx\n"}},
			},
			tool: "docker", argv: []string{"image", "ls"},
			want: result{stdout: "nginx\n"},
		},
		{
			name: "stderr and a non-zero exit code",
			sc: fakecli.Scenario{
				{Tool: "docker", Args: []string{"ps"}, Reply: fakecli.Reply{Stderr: "Cannot connect to the Docker daemon\n", Exit: 1}},
			},
			tool: "docker", argv: []string{"ps"},
			want: result{stderr: "Cannot connect to the Docker daemon\n", exit: 1},
		},
		{
			name: "all three at once",
			sc: fakecli.Scenario{
				{Tool: "podman", Args: []string{"info"}, Reply: fakecli.Reply{Stdout: "out", Stderr: "warn", Exit: 125}},
			},
			tool: "podman", argv: []string{"info"},
			want: result{stdout: "out", stderr: "warn", exit: 125},
		},
		{
			name: "kind is served like the others",
			sc: fakecli.Scenario{
				{Tool: "kind", Args: []string{"get", "clusters"}, Reply: fakecli.Reply{Stdout: "devdesk\n"}},
			},
			tool: "kind", argv: []string{"get", "clusters"},
			want: result{stdout: "devdesk\n"},
		},
		{
			name: "kubectl is served like the others",
			sc: fakecli.Scenario{
				{Tool: "kubectl", Args: []string{"get", "nodes"}, Reply: fakecli.Reply{Stdout: "node-1\n", Exit: 0}},
			},
			tool: "kubectl", argv: []string{"get", "nodes"},
			want: result{stdout: "node-1\n"},
		},
		{
			name: "an empty Tool answers any tool",
			sc: fakecli.Scenario{
				{Args: []string{"version"}, Reply: fakecli.Reply{Stdout: "v1\n"}},
			},
			tool: "kubectl", argv: []string{"version"},
			want: result{stdout: "v1\n"},
		},
		{
			name: "the same argv answers per tool",
			sc: fakecli.Scenario{
				{Tool: "docker", Args: []string{"version"}, Reply: fakecli.Reply{Stdout: "docker\n"}},
				{Tool: "podman", Args: []string{"version"}, Reply: fakecli.Reply{Stdout: "podman\n"}},
			},
			tool: "podman", argv: []string{"version"},
			want: result{stdout: "podman\n"},
		},
		{
			name: "a prefix exchange answers any longer argv",
			sc: fakecli.Scenario{
				{Tool: "docker", Args: []string{"image"}, Prefix: true, Reply: fakecli.Reply{Stdout: "any image command\n"}},
			},
			tool: "docker", argv: []string{"image", "inspect", "--format", "{{.Id}}", "nginx"},
			want: result{stdout: "any image command\n"},
		},
		{
			name: "the longest matching prefix wins",
			sc: fakecli.Scenario{
				{Tool: "docker", Args: []string{"image"}, Prefix: true, Reply: fakecli.Reply{Stdout: "short"}},
				{Tool: "docker", Args: []string{"image", "ls"}, Prefix: true, Reply: fakecli.Reply{Stdout: "long"}},
			},
			tool: "docker", argv: []string{"image", "ls", "-a"},
			want: result{stdout: "long"},
		},
		{
			name: "an exact exchange wins over a prefix of the same argv",
			sc: fakecli.Scenario{
				{Tool: "docker", Args: []string{"image", "ls"}, Prefix: true, Reply: fakecli.Reply{Stdout: "prefix"}},
				{Tool: "docker", Args: []string{"image", "ls"}, Reply: fakecli.Reply{Stdout: "exact"}},
			},
			tool: "docker", argv: []string{"image", "ls"},
			want: result{stdout: "exact"},
		},
		{
			name: "an empty prefix is a catch-all that succeeds silently",
			sc: fakecli.Scenario{
				{Tool: "docker", Prefix: true},
			},
			tool: "docker", argv: []string{"anything", "at", "all"},
			want: result{},
		},
		{
			name: "arguments are compared one by one, not as a joined string",
			sc: fakecli.Scenario{
				{Tool: "docker", Args: []string{"run", "a b"}, Reply: fakecli.Reply{Stdout: "one arg"}},
				{Tool: "docker", Args: []string{"run", "a", "b"}, Reply: fakecli.Reply{Stdout: "two args"}},
			},
			tool: "docker", argv: []string{"run", "a b"},
			want: result{stdout: "one arg"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools := tc.tools
			if tools == nil {
				tools = []string{"docker", "podman", "kind", "kubectl"}
			}
			fakecli.Install(t, tc.sc, tools...)

			got := run(t, tc.tool, tc.argv...)

			if got != tc.want {
				t.Errorf("%s %q = %+v, want %+v", tc.tool, tc.argv, got, tc.want)
			}
		})
	}
}

// Criterion 1: the argv is journaled exactly and a test can read it.
func TestTheArgvIsJournaledExactly(t *testing.T) {
	t.Setenv("FAKECLI_PROBE", "from-the-test")
	f := fakecli.Install(t, fakecli.Scenario{{Prefix: true}}, "docker", "kubectl")

	if got := f.Invocations(); len(got) != 0 {
		t.Fatalf("journal before any run = %+v, want empty", got)
	}

	argvs := []struct {
		tool string
		args []string
	}{
		{"docker", []string{"run", "--rm", "-e", "A=b c", "", "img:1", "sh", "-c", "echo 'quoted' \"double\" $HOME"}},
		{"kubectl", []string{"get", "pods", "-n", "kube-system"}},
		{"docker", nil},
	}
	for _, a := range argvs {
		run(t, a.tool, a.args...)
	}

	got := f.Invocations()
	if len(got) != len(argvs) {
		t.Fatalf("journal has %d entries, want %d: %+v", len(got), len(argvs), got)
	}
	for i, a := range argvs {
		if got[i].Tool != a.tool {
			t.Errorf("entry %d tool = %q, want %q", i, got[i].Tool, a.tool)
		}
		// nil and empty are the same argv.
		if !slices.Equal(got[i].Args, a.args) {
			t.Errorf("entry %d argv = %q, want %q", i, got[i].Args, a.args)
		}
		if !got[i].Matched {
			t.Errorf("entry %d reported as unmatched, the catch-all covers it", i)
		}
		if !slices.Contains(got[i].Env, "FAKECLI_PROBE=from-the-test") {
			t.Errorf("entry %d environment does not hold the variable the test set", i)
		}
	}
}

// The journal is read by the test while the tool may still be running calls
// from several goroutines of the code under test: no entry is lost or torn.
func TestTheJournalKeepsEveryRunOfConcurrentCalls(t *testing.T) {
	const runs = 24
	f := fakecli.Install(t, fakecli.Scenario{{Prefix: true}}, "docker")

	done := make(chan struct{}, runs)
	for i := range runs {
		go func() {
			cmd := exec.Command("docker", "call", fmt.Sprint(i))
			_ = cmd.Run()
			done <- struct{}{}
		}()
	}
	for range runs {
		<-done
	}

	got := f.Invocations()
	if len(got) != runs {
		t.Fatalf("journal has %d entries, want %d", len(got), runs)
	}
	seen := map[string]bool{}
	for _, inv := range got {
		if len(inv.Args) != 2 || inv.Args[0] != "call" {
			t.Fatalf("a torn entry: %+v", inv)
		}
		seen[inv.Args[1]] = true
	}
	if len(seen) != runs {
		t.Errorf("%d distinct calls journaled, want %d", len(seen), runs)
	}
}

// Criterion 2: an argv the scenario does not declare fails the test, and the
// failure shows the argv that was received. The fake runs in its own process,
// so the process says so on stderr and exits non-zero at once, and the test is
// failed when it ends.
func TestAnUndeclaredArgvFailsTheTestWithTheReceivedArgv(t *testing.T) {
	cases := []struct {
		name string
		sc   fakecli.Scenario
		tool string
		argv []string
	}{
		{
			name: "nothing declared at all",
			sc:   nil,
			tool: "docker", argv: []string{"volume", "prune", "-f"},
		},
		{
			name: "an exact exchange does not cover a longer argv",
			sc: fakecli.Scenario{
				{Tool: "docker", Args: []string{"volume", "prune"}},
			},
			tool: "docker", argv: []string{"volume", "prune", "-f"},
		},
		{
			name: "an exchange for another tool does not cover this one",
			sc: fakecli.Scenario{
				{Tool: "podman", Args: []string{"volume", "prune", "-f"}},
			},
			tool: "docker", argv: []string{"volume", "prune", "-f"},
		},
		{
			name: "a prefix does not cover a different leading argument",
			sc: fakecli.Scenario{
				{Tool: "docker", Args: []string{"image"}, Prefix: true},
			},
			tool: "docker", argv: []string{"volume", "prune", "-f"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := &spyT{T: t}
			f := fakecli.Install(spy, tc.sc, "docker", "podman")

			got := run(t, tc.tool, tc.argv...)

			if got.exit == 0 {
				t.Errorf("an undeclared argv exited 0, want a failure the code under test sees")
			}
			wantArgv := fmt.Sprintf("%q", tc.argv)
			if !strings.Contains(got.stderr, wantArgv) {
				t.Errorf("stderr = %q, want it to show the received argv %s", got.stderr, wantArgv)
			}

			spy.finish()
			if len(spy.errs) == 0 {
				t.Fatal("the test was not failed for an undeclared argv")
			}
			report := strings.Join(spy.errs, "\n")
			if !strings.Contains(report, tc.tool) || !strings.Contains(report, wantArgv) {
				t.Errorf("failure report = %q, want the tool %q and the argv %s", report, tc.tool, wantArgv)
			}

			inv := f.Invocations()
			if len(inv) != 1 || inv[0].Matched {
				t.Errorf("journal = %+v, want one unmatched entry", inv)
			}
		})
	}
}

// A run the scenario declares never fails the test.
func TestADeclaredArgvLeavesTheTestGreen(t *testing.T) {
	spy := &spyT{T: t}
	fakecli.Install(spy, fakecli.Scenario{
		{Tool: "docker", Args: []string{"ps"}, Reply: fakecli.Reply{Stdout: "x"}},
	}, "docker")

	run(t, "docker", "ps")
	spy.finish()

	if len(spy.errs) != 0 {
		t.Errorf("the test was failed although every argv was declared: %q", spy.errs)
	}
}

// A scenario can be a strict list of exchanges for a tool that is never run:
// declaring an exchange nobody triggers is not an error (a table of cases
// shares one scenario).
func TestAnUnusedExchangeIsNotAFailure(t *testing.T) {
	spy := &spyT{T: t}
	fakecli.Install(spy, fakecli.Scenario{
		{Tool: "docker", Args: []string{"ps"}},
		{Tool: "docker", Args: []string{"info"}},
	}, "docker")

	run(t, "docker", "ps")
	spy.finish()

	if len(spy.errs) != 0 {
		t.Errorf("an exchange that was not used failed the test: %q", spy.errs)
	}
}

// PATH holds the declared tools and nothing else, so the real tool of the
// machine cannot be reached; Dir names the directory (OC-03a declares it to
// the engine guard).
func TestOnlyTheDeclaredToolsAreOnThePath(t *testing.T) {
	f := fakecli.Install(t, fakecli.Scenario{{Prefix: true}}, "docker", "kubectl")

	if got := os.Getenv("PATH"); got != f.Dir() {
		t.Errorf("PATH = %q, want exactly the fake directory %q", got, f.Dir())
	}
	for _, name := range []string{"docker", "kubectl"} {
		p, err := exec.LookPath(name)
		if err != nil {
			t.Errorf("%s is not found on PATH: %v", name, err)
			continue
		}
		if filepath.Dir(p) != f.Dir() {
			t.Errorf("%s resolves to %q, want a file in %q", name, p, f.Dir())
		}
	}
	for _, name := range []string{"podman", "kind"} {
		if p, err := exec.LookPath(name); err == nil {
			t.Errorf("%s was not declared but resolves to %q", name, p)
		}
	}
}

// Tools other than the four engine tools can be installed: the scanners and
// skopeo of the migrated fakes (trivy, gitleaks, skopeo) are named the same way.
func TestAnyToolNameCanBeInstalled(t *testing.T) {
	fakecli.Install(t, fakecli.Scenario{
		{Tool: "trivy", Args: []string{"--version"}, Reply: fakecli.Reply{Stdout: "Version: 0.50.1\n"}},
	}, "trivy", "gitleaks")

	got := run(t, "trivy", "--version")

	if got.stdout != "Version: 0.50.1\n" || got.exit != 0 {
		t.Errorf("trivy --version = %+v", got)
	}
}

// Each test gets its own journal and its own directory: two installs in a row
// share nothing.
func TestInstallsDoNotShareAJournal(t *testing.T) {
	first := fakecli.Install(t, fakecli.Scenario{{Prefix: true}}, "docker")
	run(t, "docker", "one")

	second := fakecli.Install(t, fakecli.Scenario{{Prefix: true}}, "docker")
	run(t, "docker", "two")

	if got := second.Invocations(); len(got) != 1 || !reflect.DeepEqual(got[0].Args, []string{"two"}) {
		t.Errorf("second journal = %+v, want only the run of the second install", got)
	}
	if first.Dir() == second.Dir() {
		t.Errorf("both installs use %q", first.Dir())
	}
}

// replay stands for what OC-08 will provide: a Source built from recorded
// exchanges. It lives here only to prove that anything exposing Exchanges can
// drive the fake, with no change to fakecli. Reading real fixtures is OC-08.
type replay struct {
	recorded []recordedExchange
}

type recordedExchange struct {
	tool   string
	argv   []string
	stdout string
	stderr string
	exit   int
}

func (r replay) Exchanges() []fakecli.Exchange {
	out := make([]fakecli.Exchange, 0, len(r.recorded))
	for _, e := range r.recorded {
		out = append(out, fakecli.Exchange{
			Tool:  e.tool,
			Args:  e.argv,
			Reply: fakecli.Reply{Stdout: e.stdout, Stderr: e.stderr, Exit: e.exit},
		})
	}
	return out
}

// Criterion 5, extension point only: a Source other than Scenario is served
// the same way, and Scenario is itself a Source.
func TestASourceOtherThanAScenarioIsServed(t *testing.T) {
	var _ fakecli.Source = fakecli.Scenario(nil)

	fakecli.Install(t, replay{recorded: []recordedExchange{
		{tool: "docker", argv: []string{"volume", "prune", "-f"}, stdout: "Total reclaimed space: 0B\n"},
		{tool: "podman", argv: []string{"volume", "prune", "-f"}, stderr: "Error: refused\n", exit: 125},
	}}, "docker", "podman")

	if got := run(t, "docker", "volume", "prune", "-f"); got != (result{stdout: "Total reclaimed space: 0B\n"}) {
		t.Errorf("docker replay = %+v", got)
	}
	if got := run(t, "podman", "volume", "prune", "-f"); got != (result{stderr: "Error: refused\n", exit: 125}) {
		t.Errorf("podman replay = %+v", got)
	}
}
