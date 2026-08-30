package docker

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// dockerCmd describes one CLI invocation.
type dockerCmd struct {
	// Args are passed to the binary verbatim, e.g. {"ps", "--format", "..."}.
	Args []string
	// Stdin is written to the process when non-empty. Used to keep secrets out
	// of the process list (`docker login --password-stdin`).
	Stdin string
	// Combined merges stderr into the returned bytes. Docker writes failure
	// detail to stderr, so mutating calls set this to produce a usable message;
	// calls that parse stdout leave it false.
	Combined bool
	// Helper runs `docker-credential-<Helper>` instead of `docker`.
	Helper string
	// Ctx kills the process when it is cancelled. Nil for the invocations
	// nobody can stop — which is all of them but the pull, the one long call
	// this package makes that `K` is offered on (jobs.KindPull.Cancellable).
	// A nil Ctx builds exactly the command it built before.
	Ctx context.Context
}

// dockerRunner is the seam every CLI invocation in this package passes through.
//
// The package talks to the Docker CLI rather than the SDK, which keeps the
// dependency light but makes the whole package untestable without a daemon.
// Routing through this interface lets tests exercise the argument building and
// output parsing — the parts that actually carry defects — against canned
// output.
type dockerRunner interface {
	// LookPath reports whether the docker binary is reachable on PATH.
	LookPath() error
	// Run executes one invocation and returns its captured output.
	Run(cmd dockerCmd) ([]byte, error)
	// Build returns a ready-to-run command without executing it, for callers
	// that hand the process to a terminal (tea.ExecProcess).
	Build(args ...string) *exec.Cmd
}

// cliRunner is the production dockerRunner: it shells out to the real binary.
type cliRunner struct{}

func (cliRunner) LookPath() error {
	_, err := exec.LookPath("docker")
	return err
}

func (cliRunner) Build(args ...string) *exec.Cmd {
	return exec.Command("docker", args...)
}

func (cliRunner) Run(dc dockerCmd) ([]byte, error) {
	name := "docker"
	if dc.Helper != "" {
		name = "docker-credential-" + dc.Helper
	}
	cmd := exec.Command(name, dc.Args...) //nolint:gosec // helper name comes from ~/.docker/config.json
	if dc.Ctx != nil {
		cmd = exec.CommandContext(dc.Ctx, name, dc.Args...) //nolint:gosec // same argument as above
	}
	if dc.Stdin != "" {
		cmd.Stdin = strings.NewReader(dc.Stdin)
	}
	if dc.Combined {
		return cmd.CombinedOutput()
	}
	return cmd.Output()
}

// runner is swapped by tests via stubRunner. Production code never reassigns
// it, so the package stays safe to call concurrently; tests that swap it must
// not run in parallel.
var runner dockerRunner = cliRunner{}

// requireDocker reports the absence of the docker binary with the error message
// callers have always produced.
func requireDocker() error {
	if err := runner.LookPath(); err != nil {
		return fmt.Errorf("docker not found: %w", err)
	}
	return nil
}

// dockerOutput runs `docker args...` and returns stdout only.
func dockerOutput(args ...string) ([]byte, error) {
	return runner.Run(dockerCmd{Args: args})
}

// dockerCombined runs `docker args...` and returns stdout and stderr merged.
func dockerCombined(args ...string) ([]byte, error) {
	return runner.Run(dockerCmd{Args: args, Combined: true})
}

// wrapErr reports a failed invocation whose stderr was not captured, keeping the
// underlying error for errors.Is/As.
func wrapErr(label string, err error) error {
	return fmt.Errorf("%s failed: %w", label, err)
}

// errWithOutput reports a failed invocation using the captured output, which
// carries Docker's own diagnostic.
func errWithOutput(label string, output []byte) error {
	return fmt.Errorf("%s failed: %s", label, strings.TrimSpace(string(output)))
}

// mutate runs a docker subcommand whose output only matters on failure, and
// wraps that output in an error prefixed with label (e.g. "docker stop failed").
//
// It leaves Ctx at its zero value: nothing stops these, which is D7's answer
// for every mutating call but the pull.
func mutate(label string, args ...string) error {
	output, err := runner.Run(dockerCmd{Args: args, Combined: true})
	return mutationError(label, output, err)
}

// mutateContext is mutate for the one call that can be stopped.
func mutateContext(ctx context.Context, label string, args ...string) error {
	output, err := runner.Run(dockerCmd{Args: args, Combined: true, Ctx: ctx})
	return mutationError(label, output, err)
}

// mutationError turns a captured invocation into the error both mutators
// report, using Docker's own diagnostic.
func mutationError(label string, output []byte, err error) error {
	if err != nil {
		return fmt.Errorf("%s failed: %s", label, strings.TrimSpace(string(output)))
	}
	return nil
}

// prune runs a docker prune subcommand and returns its trimmed report.
func prune(label string, args ...string) (string, error) {
	output, err := dockerCombined(args...)
	if err != nil {
		return "", fmt.Errorf("%s failed: %s", label, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// splitLines splits trimmed command output into non-empty lines.
func splitLines(output []byte) []string {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil
	}
	var lines []string
	for line := range strings.SplitSeq(trimmed, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
