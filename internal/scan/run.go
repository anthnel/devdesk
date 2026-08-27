package scan

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// toolCmd is one subprocess invocation: the binary and the arguments handed to
// it. Building it is pure, which is what makes the argument surface — the part
// that decides what a scanner is actually asked to do — testable without a
// scanner installed.
//
// It is also what the UI displays. Deriving the shown command and the executed
// one from the same value is the point: the two used to be built separately and
// had drifted (§1.3 D19).
type toolCmd struct {
	Name string
	Args []string
	// Env is the child's whole environment when it is not nil, os.Environ()
	// plus what the caller added. It exists so a credential can reach a tool
	// without going through argv, which is readable from the process list —
	// the arrangement the clone already uses for http.extraHeader (§3.16).
	//
	// String() never renders it: the invocation is logged and shown in the UI.
	Env []string

	// Dir is the working directory the tool runs in, when it has one.
	//
	// It exists for plumber, whose `analyze` takes **no path argument**: it
	// works on the current directory. Without this the tool ran in whatever
	// directory DevDesk was launched from and reported
	// `--project is required (could not auto-detect from git remote)` — a
	// failure about a repository nobody had asked it to look at. Trivy and
	// Gitleaks take their target in argv and set nothing here.
	Dir string
}

// String renders the invocation as a shell command line, for logs and for the
// command the UI shows next to a scan.
func (c toolCmd) String() string {
	if c.Name == "" {
		return ""
	}
	line := strings.Join(append([]string{c.Name}, c.Args...), " ")
	// The directory is part of what the command means when the tool takes no
	// path: a logged invocation that cannot be re-run says less than it looks
	// like it does.
	if c.Dir != "" {
		return "cd " + c.Dir + " && " + line
	}
	return line
}

// exitError reports a tool that ran and exited non-zero. The code matters:
// both scanners use a non-zero exit to mean "I found something" rather than
// "I failed" — trivy for vulnerabilities, gitleaks for secrets — so callers
// have to tell the two apart.
type exitError struct {
	Code   int
	Stderr string
}

func (e *exitError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("exit status %d: %s", e.Code, strings.TrimSpace(e.Stderr))
	}
	return fmt.Sprintf("exit status %d", e.Code)
}

// commandRunner is the seam every subprocess in this package passes through.
//
// The package drives trivy and gitleaks as external processes, which makes it
// untestable without both installed and a repository to point them at. Routing
// through this interface lets tests exercise the argument building, the
// exit-code handling and the output parsing — where the defects live — against
// canned output.
type commandRunner interface {
	// Run executes tc and returns what it wrote to stdout. Lines written to
	// stderr are forwarded to progressFn as they arrive, which is how Trivy
	// reports its database download. A tool that runs and exits non-zero
	// returns its stdout along with an *exitError.
	Run(ctx context.Context, tc toolCmd, progressFn func(string)) ([]byte, error)
}

// cliRunner is the production commandRunner: it shells out to the real tools.
type cliRunner struct{}

func (cliRunner) Run(ctx context.Context, tc toolCmd, progressFn func(string)) ([]byte, error) {
	cmd := exec.CommandContext(ctx, tc.Name, tc.Args...) //nolint:gosec // name is one of trivy/gitleaks/plumber/docker
	cmd.Env = tc.Env                                     // nil inherits this process's environment, which is the default
	cmd.Dir = tc.Dir                                     // empty runs in DevDesk's own working directory

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	// Stderr is consumed two ways: streamed to progressFn while the tool runs,
	// and kept so a failure can be reported with the tool's own diagnostic.
	var stderr bytes.Buffer
	if progressFn != nil {
		pipe, err := cmd.StderrPipe()
		if err == nil {
			done := make(chan struct{})
			go func() {
				defer close(done)
				sc := bufio.NewScanner(pipe)
				for sc.Scan() {
					line := strings.TrimSpace(sc.Text())
					if line == "" {
						continue
					}
					stderr.WriteString(line)
					stderr.WriteByte('\n')
					progressFn(line)
				}
				// A read failure on the progress pipe is not a scan failure:
				// the report comes back on stdout, and the tool's exit code
				// says whether it succeeded. Dropping the rest of the progress
				// is the whole consequence.
				_ = sc.Err()
			}()
			if err := cmd.Start(); err != nil {
				return nil, fmt.Errorf("%s failed to start: %w", tc.Name, err)
			}
			<-done // drain stderr before Wait closes the pipe
			return finish(tc.Name, cmd, &stdout, &stderr)
		}
	}

	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s failed to start: %w", tc.Name, err)
	}
	return finish(tc.Name, cmd, &stdout, &stderr)
}

// finish waits for the tool and only then reads what it wrote.
//
// The order is the point, and it is not a style preference: os/exec fills the
// output buffers from goroutines it owns, and only Wait guarantees they are
// done. Reading the buffer in the same expression as Wait — which is what
// `return stdout.Bytes(), waitErr(..., cmd.Wait(), ...)` does, since operands
// are evaluated before the call — snapshots it while it is still empty, and
// every scan comes back with no findings.
func finish(name string, cmd *exec.Cmd, stdout, stderr *bytes.Buffer) ([]byte, error) {
	err := cmd.Wait()
	return stdout.Bytes(), waitErr(name, err, stderr.String())
}

// waitErr turns a non-zero exit into an *exitError carrying the code, and
// leaves anything else (a killed process, a broken pipe) as it is.
func waitErr(name string, err error, stderr string) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if stderr == "" {
			stderr = string(exitErr.Stderr)
		}
		return &exitError{Code: exitErr.ExitCode(), Stderr: stderr}
	}
	return fmt.Errorf("%s failed: %w", name, err)
}

// runner is swapped by tests. Production code never reassigns it, so the
// package stays safe to call concurrently; tests that swap it must not run in
// parallel.
var runner commandRunner = cliRunner{}
