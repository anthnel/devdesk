package scan

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
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

	// Stdin is fed to the tool when it is not nil. It exists for kubeconform
	// validating what helm or kustomize rendered (§3.80): the rendered YAML
	// exists only in memory, and writing it to a temporary file would need a
	// second mount for a containerised kubeconform. A container reading it
	// must be run with -i.
	Stdin []byte

	// Verbatim lists the arguments DevDesk wrote itself that have the shape
	// key=value (`--print=false`, `-e GIT_CONFIG_COUNT=1`), exact strings.
	// Loggable leaves them readable and masks every other key=value, since
	// the rest of argv may come from scan.tools.*.args (DEVDESK-768).
	Verbatim []string
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

// Loggable renders the invocation for the log (DEVDESK-768). It is String()
// with three differences, and the same line when none applies:
//
//   - the value of a key=value argument is masked (`password=***`): the
//     arguments come from scan.tools.*.args, which the user writes and which
//     may hold a secret. An argument listed in Verbatim is DevDesk's own and
//     stays readable;
//   - the credentials of a URL are removed (`https://user:token@host/x`);
//   - a word that holds a control character, such as a newline in a folder
//     name from the scanned repository, is quoted with %q so the entry stays
//     on one log line.
//
// Limit: a secret passed as a bare positional argument has no shape to
// recognise and is not masked.
func (c toolCmd) Loggable() string {
	if c.Name == "" {
		return ""
	}
	words := []string{logWord(c.Name)}
	for _, arg := range c.Args {
		if !slices.Contains(c.Verbatim, arg) {
			arg = maskArg(arg)
		}
		words = append(words, logWord(arg))
	}
	line := strings.Join(words, " ")
	if c.Dir != "" {
		return "cd " + logWord(c.Dir) + " && " + line
	}
	return line
}

var (
	// urlUserinfo is everything up to the last "@" before the first "/", "?"
	// or "#" of a URL: greedy, so a password holding an "@" leaves no rest.
	urlUserinfo = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/?#\s]*@`)
	// urlQueryValue is the value of a query parameter, masked in any word that
	// holds "://".
	urlQueryValue = regexp.MustCompile(`([?&][^=&#]+=)[^&#]*`)
	// keyValueArg is an argument of the form key=value or --key=value. The key
	// is any run of characters but a space, "=", "/" and ":" (helm keys carry
	// brackets, braces, dots, commas and backslashes: secrets[0].password,
	// a\.b), and does not start with "." or "/" so a path is not one. A key
	// with a "/" (auth/token=x) is left readable: it cannot be told from a
	// relative path.
	keyValueArg = regexp.MustCompile(`^([^\s=/:.][^\s=/:]*)=`)
)

// maskArg removes the credentials and the query values of a URL and hides the
// value of a key=value argument.
func maskArg(arg string) string {
	if strings.Contains(arg, "://") {
		arg = urlUserinfo.ReplaceAllString(arg, "$1")
		arg = urlQueryValue.ReplaceAllString(arg, "${1}***")
	}
	if m := keyValueArg.FindStringSubmatch(arg); m != nil {
		return m[1] + "=***"
	}
	return arg
}

// logWord is word as one token of a log line: bare, unless it is not valid
// UTF-8 or holds a rune that does not print (a newline would split the entry
// in two; U+2028, U+202E or a C1 byte would fake or reorder it). Printable
// unicode names stay bare.
func logWord(word string) string {
	if !utf8.ValidString(word) || strings.ContainsFunc(word, func(r rune) bool { return !unicode.IsPrint(r) }) {
		return strconv.Quote(word)
	}
	return word
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
	if tc.Stdin != nil {
		cmd.Stdin = bytes.NewReader(tc.Stdin)
	}

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	// Stderr is consumed two ways: streamed to progressFn while the tool runs,
	// and kept so a failure can be reported with the tool's own diagnostic.
	// What is kept stays raw: waitErr redacts it, on both paths. What is
	// streamed is redacted line by line, since it leaves before the tool exits.
	var stderr bytes.Buffer
	redact := proxyForward().Redactor()
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
					progressFn(redact(line))
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
// leaves anything else (a killed process, a broken pipe) as it is. The tool's
// diagnostic, from the buffer or from os/exec's ExitError.Stderr, is stripped of
// the proxy credentials first (DEVDESK-583).
func waitErr(name string, err error, stderr string) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if stderr == "" {
			stderr = string(exitErr.Stderr)
		}
		return &exitError{Code: exitErr.ExitCode(), Stderr: proxyForward().Redact(stderr)}
	}
	return fmt.Errorf("%s failed: %w", name, err)
}

// runner is swapped by tests. Production code never reassigns it, so the
// package stays safe to call concurrently; tests that swap it must not run in
// parallel.
var runner commandRunner = cliRunner{}
