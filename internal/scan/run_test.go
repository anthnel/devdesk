package scan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The package drives trivy and gitleaks as subprocesses, so the production
// runner cannot be exercised without both installed. Instead the test binary
// re-executes itself: TestMain notices the helper variables and behaves the way
// a scanner does — a report on stdout, progress on stderr, and an exit code that
// may mean "I found something" rather than "I failed".
const (
	helperMode   = "DEVDESK_SCAN_HELPER"
	helperStdout = "DEVDESK_SCAN_HELPER_STDOUT"
	helperStderr = "DEVDESK_SCAN_HELPER_STDERR"
	helperExit   = "DEVDESK_SCAN_HELPER_EXIT"
	// helperFailOn names an argument the helper refuses. One binary is asked
	// several different things — `docker images` and `docker run` both go to
	// the same fake — so failing has to be selectable per subcommand.
	helperFailOn = "DEVDESK_SCAN_HELPER_FAIL_ON"
)

func TestMain(m *testing.M) {
	if os.Getenv(helperMode) == "1" {
		os.Exit(runAsHelper())
	}
	// Scan logs the command of every stage it starts, which is useful in the
	// application and pure noise across a suite that runs hundreds of them.
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

func runAsHelper() int {
	if refused := os.Getenv(helperFailOn); refused != "" && slices.Contains(os.Args[1:], refused) {
		_, _ = fmt.Fprintln(os.Stderr, "cannot connect to the daemon")
		return 1
	}
	if out := os.Getenv(helperStdout); out != "" {
		_, _ = fmt.Fprint(os.Stdout, out)
	}
	if diag := os.Getenv(helperStderr); diag != "" {
		for line := range strings.SplitSeq(diag, "\n") {
			_, _ = fmt.Fprintln(os.Stderr, line)
		}
	}
	code, _ := strconv.Atoi(os.Getenv(helperExit))
	return code
}

// asScanner points a toolCmd at the test binary and tells it what to produce.
func asScanner(t *testing.T, stdout, stderr string, exit int) toolCmd {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	t.Setenv(helperMode, "1")
	t.Setenv(helperStdout, stdout)
	t.Setenv(helperStderr, stderr)
	t.Setenv(helperExit, strconv.Itoa(exit))
	t.Setenv(helperFailOn, "")
	return toolCmd{Name: self}
}

// ── The production runner ────────────────────────────────────────────────────

// production is the real runner, named so the tests below can call it in an if
// statement — a composite literal cannot appear there unparenthesised.
var production = cliRunner{}

func TestTheReportOnStdoutIsWhatComesBack(t *testing.T) {
	tc := asScanner(t, `{"Results":[]}`, "", 0)

	stdout, err := production.Run(context.Background(), tc, nil)
	if err != nil {
		t.Fatalf("a tool that exited cleanly was reported as failing: %v", err)
	}
	if string(stdout) != `{"Results":[]}` {
		t.Errorf("stdout = %q, want the report", stdout)
	}
}

// Trivy reports its database download on stderr while it runs, which is the
// only thing the user has to look at during a long scan.
func TestStderrIsHandedOverLineByLine(t *testing.T) {
	tc := asScanner(t, "", "downloading db\n25%\ndone", 0)

	var progress []string
	if _, err := production.Run(context.Background(), tc, func(line string) {
		progress = append(progress, line)
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{"downloading db", "25%", "done"}
	if strings.Join(progress, "|") != strings.Join(want, "|") {
		t.Errorf("progress = %v, want %v", progress, want)
	}
}

// A blank line is not progress; forwarding it would blank the detail already on
// screen for no reason.
func TestABlankStderrLineIsNotProgress(t *testing.T) {
	tc := asScanner(t, "", "first\n\nsecond", 0)

	var progress []string
	if _, err := production.Run(context.Background(), tc, func(line string) {
		progress = append(progress, line)
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(progress) != 2 {
		t.Errorf("progress = %v, want the blank line dropped", progress)
	}
}

// Both scanners exit non-zero to say they found something, so the code has to
// survive alongside the report rather than replace it.
func TestANonZeroExitKeepsBothTheCodeAndTheReport(t *testing.T) {
	tc := asScanner(t, `{"Results":[]}`, "1 vulnerability found", 1)

	stdout, err := production.Run(context.Background(), tc, func(string) {})

	var exit *exitError
	if !errors.As(err, &exit) {
		t.Fatalf("err = %v, want an *exitError", err)
	}
	if exit.Code != 1 {
		t.Errorf("exit code = %d, want 1", exit.Code)
	}
	if !strings.Contains(exit.Stderr, "1 vulnerability found") {
		t.Errorf("the tool's own diagnostic was dropped: %q", exit.Stderr)
	}
	if string(stdout) != `{"Results":[]}` {
		t.Errorf("the report was discarded along with the exit code: %q", stdout)
	}
}

// Nobody watching progress must not mean nobody keeping the diagnostic: it is
// what explains the failure once the scan is over.
func TestTheDiagnosticIsKeptWithNoProgressCallback(t *testing.T) {
	tc := asScanner(t, "", "FATAL unable to initialize scanner", 2)

	_, err := production.Run(context.Background(), tc, nil)

	var exit *exitError
	if !errors.As(err, &exit) {
		t.Fatalf("err = %v, want an *exitError", err)
	}
	if !strings.Contains(exit.Stderr, "unable to initialize scanner") {
		t.Errorf("the diagnostic was lost: %q", exit.Stderr)
	}
}

func TestAToolThatIsNotInstalledIsReportedAsSuch(t *testing.T) {
	_, err := production.Run(context.Background(), toolCmd{Name: "devdesk-no-such-scanner"}, nil)

	if err == nil {
		t.Fatal("a missing binary was reported as a clean run")
	}
	if !strings.Contains(err.Error(), "failed to start") {
		t.Errorf("err = %v, want it to say the tool never started", err)
	}
	// A tool that never ran has no exit code, so it must not look like a
	// scanner reporting findings.
	var exit *exitError
	if errors.As(err, &exit) {
		t.Errorf("a missing binary was reported as an exit code: %v", exit)
	}
}

func TestACancelledScanStops(t *testing.T) {
	tc := asScanner(t, "", "", 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := production.Run(ctx, tc, nil); err == nil {
		t.Error("a cancelled scan reported success")
	}
}

// ── Exit handling, on its own ────────────────────────────────────────────────

func TestWaitErrLeavesAFailureThatIsNotAnExitAlone(t *testing.T) {
	err := waitErr("trivy", errors.New("broken pipe"), "")

	if err == nil {
		t.Fatal("a failure was swallowed")
	}
	var exit *exitError
	if errors.As(err, &exit) {
		t.Fatalf("a non-exit failure was turned into an exit code: %v", exit)
	}
	if !strings.Contains(err.Error(), "trivy failed") {
		t.Errorf("err = %v, want the tool named", err)
	}
}

// A caller that let os/exec capture the output — rather than streaming it —
// gets the diagnostic hung on the exit error instead, and that is where it has
// to be read from. This is the path CheckDependencies takes with Output().
func TestTheDiagnosticIsTakenFromTheExitErrorWhenNothingWasCaptured(t *testing.T) {
	tc := asScanner(t, "", "FATAL could not read config", 2)

	_, err := exec.Command(tc.Name).Output() //nolint:gosec // the test binary, acting as a scanner

	got := waitErr("trivy", err, "")
	var exit *exitError
	if !errors.As(got, &exit) {
		t.Fatalf("err = %v, want an *exitError", got)
	}
	if !strings.Contains(exit.Stderr, "could not read config") {
		t.Errorf("the diagnostic was lost: %q", exit.Stderr)
	}
}

func TestWaitErrReportsNothingWhenTheToolSucceeded(t *testing.T) {
	if err := waitErr("trivy", nil, "some noise on stderr"); err != nil {
		t.Errorf("a clean exit was reported as %v", err)
	}
}

func TestAnExitErrorQuotesTheDiagnosticWhenThereIsOne(t *testing.T) {
	withDiag := (&exitError{Code: 2, Stderr: "  config not found\n"}).Error()
	if !strings.Contains(withDiag, "exit status 2") || !strings.Contains(withDiag, "config not found") {
		t.Errorf("Error() = %q, want the code and the diagnostic", withDiag)
	}
	if strings.HasSuffix(withDiag, "\n") {
		t.Errorf("Error() = %q, want the diagnostic trimmed", withDiag)
	}

	if bare := (&exitError{Code: 1}).Error(); bare != "exit status 1" {
		t.Errorf("Error() = %q, want just the code", bare)
	}
}

// ── The invocation as the user sees it ───────────────────────────────────────

func TestAnUnbuiltCommandRendersAsNothing(t *testing.T) {
	if got := (toolCmd{}).String(); got != "" {
		t.Errorf("String() = %q, want empty for a command that was never built", got)
	}
	if got := (toolCmd{Args: []string{"fs"}}).String(); got != "" {
		t.Errorf("String() = %q, want empty when there is no binary to run", got)
	}
}

func TestACommandRendersAsAShellLine(t *testing.T) {
	got := toolCmd{Name: "trivy", Args: []string{"fs", "--format", "json", "/repos"}}.String()
	if got != "trivy fs --format json /repos" {
		t.Errorf("String() = %q", got)
	}
}
