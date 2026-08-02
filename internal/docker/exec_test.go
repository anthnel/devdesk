package docker

import (
	"errors"
	"os/exec"
	"testing"
)

// errExit stands in for the failure of a docker invocation.
var errExit = errors.New("exit status 1")

// stubRunner records the invocations it receives and replays canned output.
//
// Keyed by the first argument (the docker subcommand) so a test can stub
// several calls made by one exported function — ListImages, for instance,
// issues "image" and "system" calls.
type stubRunner struct {
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
	return exec.Command("docker", args...)
}

func (s *stubRunner) Run(dc dockerCmd) ([]byte, error) {
	s.calls = append(s.calls, dc)
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
func stub(t *testing.T, s *stubRunner) *stubRunner {
	t.Helper()
	previous := runner
	runner = s
	t.Cleanup(func() { runner = previous })
	return s
}

// stubOutput is the common case: one subcommand returning one canned payload.
func stubOutput(t *testing.T, subcommand, output string) *stubRunner {
	t.Helper()
	return stub(t, &stubRunner{output: map[string][]byte{subcommand: []byte(output)}})
}

func TestRequireDockerReportsMissingBinary(t *testing.T) {
	stub(t, &stubRunner{missing: true})

	err := requireDocker()

	if err == nil {
		t.Fatal("requireDocker() returned nil when docker is absent")
	}
	// The message reaches the footer under Rule 128, so it must name the cause.
	if got := err.Error(); got != "docker not found: executable file not found in $PATH" {
		t.Errorf("requireDocker() = %q, want it to name the missing binary", got)
	}
}

func TestMutateSurfacesCombinedOutputOnFailure(t *testing.T) {
	// Docker writes its diagnostic to stderr, so a failing mutation must report
	// the combined output rather than the bare exit status.
	s := stub(t, &stubRunner{
		output: map[string][]byte{"stop": []byte("Error response from daemon: no such container\n")},
		err:    map[string]error{"stop": errors.New("exit status 1")},
	})

	err := StopContainer("nope")

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
