package docker

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/anthnel/devdesk/internal/engine"
)

// dockerCmd describes one CLI invocation.
type dockerCmd struct {
	// Args are passed to the binary verbatim, e.g. {"ps", "--format", "..."}.
	Args []string
	// Stdin is written to the process when non-empty. Used to keep secrets out
	// of the process list (`<engine> login --password-stdin`).
	Stdin string
	// Combined merges stderr into the returned bytes. Both engines write failure
	// detail to stderr, so mutating calls set this to produce a usable message;
	// calls that parse stdout leave it false.
	Combined bool
	// Helper runs `<engine>-credential-<Helper>` instead of the engine itself.
	Helper string
	// Ctx kills the process when it is cancelled. Nil for the invocations
	// nobody can stop — which is all of them but the pull, the one long call
	// this package makes that `K` is offered on (jobs.KindPull.Cancellable).
	// A nil Ctx builds exactly the command it built before.
	Ctx context.Context
}

// dockerRunner is the seam every CLI invocation in this package passes through.
//
// The package talks to the engine's CLI rather than to an SDK, which keeps the
// dependency light but makes the whole package untestable without a daemon.
// Routing through this interface lets tests exercise the argument building and
// output parsing — the parts that actually carry defects — against canned
// output.
//
// It is also what made podman cheap (§3.67): the binary name was written in
// exactly one implementation of this interface, so the engine became a value
// without any call site moving.
type dockerRunner interface {
	// LookPath reports whether the engine's binary is reachable on PATH.
	LookPath() error
	// Run executes one invocation and returns its captured output.
	Run(cmd dockerCmd) ([]byte, error)
	// Build returns a ready-to-run command without executing it, for callers
	// that hand the process to a terminal (tea.ExecProcess).
	Build(args ...string) *exec.Cmd
}

// cliRunner is the production dockerRunner: it shells out to the real binary.
//
// It holds no engine of its own and reads engine.Current() on every call. A
// field would be a second place the resolved engine lives, and the two would
// be free to disagree after a context switch — which is exactly the kind of
// drift the one-runner seam exists to prevent.
type cliRunner struct{}

func (cliRunner) LookPath() error {
	_, err := exec.LookPath(engine.Current().Binary)
	return err
}

func (cliRunner) Build(args ...string) *exec.Cmd {
	return exec.Command(engine.Current().Binary, args...) //nolint:gosec // the binary is a declared engine name or a path the user configured
}

func (cliRunner) Run(dc dockerCmd) ([]byte, error) {
	shape := engine.Current()
	name := shape.Binary
	if dc.Helper != "" {
		name = shape.HelperPrefix + dc.Helper
	}
	cmd := exec.Command(name, dc.Args...) //nolint:gosec // helper name comes from the engine's own auth file
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
// it — switching engines changes what cliRunner reads, not which runner is
// installed — so the package stays safe to call concurrently; tests that swap
// it must not run in parallel.
var runner dockerRunner = cliRunner{}

// requireEngine reports the absence of the container engine, naming the one
// that was actually looked for rather than "docker" — someone who pinned podman
// is not helped by being told docker is missing.
func requireEngine() error {
	if err := runner.LookPath(); err != nil {
		return fmt.Errorf("%s not found: %w", engine.Current().Name, err)
	}
	return nil
}

// cmdLabel builds the prefix a failed invocation is reported under — "docker
// stop", "podman network create". It is what the footer shows (Rule 128), so it
// names the engine in use rather than the one this package was first written
// against.
//
// Not called `label`: every reporting helper below already takes a `label`
// parameter, and a function of that name would be shadowed inside each of them.
func cmdLabel(subcommand string) string {
	return engine.Current().Name + " " + subcommand
}

// templates is the set of --format strings for the engine in use.
func templates() engine.Templates { return engine.Current().Templates }

// dockerOutput runs `<engine> args...` and returns stdout only.
func dockerOutput(args ...string) ([]byte, error) {
	return runner.Run(dockerCmd{Args: args})
}

// dockerCombined runs `<engine> args...` and returns stdout and stderr merged.
func dockerCombined(args ...string) ([]byte, error) {
	return runner.Run(dockerCmd{Args: args, Combined: true})
}

// wrapErr reports a failed invocation whose stderr was not captured, keeping the
// underlying error for errors.Is/As.
func wrapErr(label string, err error) error {
	return fmt.Errorf("%s failed: %w", label, err)
}

// errWithOutput reports a failed invocation using the captured output, which
// carries the engine's own diagnostic.
func errWithOutput(label string, output []byte) error {
	return fmt.Errorf("%s failed: %s", label, strings.TrimSpace(string(output)))
}

// mutate runs a subcommand whose output only matters on failure, and wraps that
// output in an error prefixed with label (e.g. "docker stop failed"). Callers
// build the label with cmdLabel, so it names the engine actually in use.
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
// report, using the engine's own diagnostic.
func mutationError(label string, output []byte, err error) error {
	if err != nil {
		return fmt.Errorf("%s failed: %s", label, strings.TrimSpace(string(output)))
	}
	return nil
}

// prune runs a prune subcommand and returns its trimmed report.
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
