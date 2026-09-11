package scan

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// scriptedRunner stands in for the scanners. The reply is chosen per invocation
// so one scan can be given a different answer for each stage it runs, and the
// calls are recorded so a test can say which tool was asked what.
//
// It is locked because Scanner.Scan runs its stages concurrently.
type scriptedRunner struct {
	mu       sync.Mutex
	calls    []toolCmd
	progress []string
	reply    func(toolCmd) ([]byte, error)
}

func (r *scriptedRunner) Run(_ context.Context, tc toolCmd, progressFn func(string)) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, tc)
	lines := r.progress
	r.mu.Unlock()

	if progressFn != nil {
		for _, line := range lines {
			progressFn(line)
		}
	}
	return r.reply(tc)
}

func (r *scriptedRunner) commands() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, c.String())
	}
	return out
}

// commandForStage returns the invocation of one stage, or "" if it never ran.
//
// Scan runs its stages concurrently, so calls are recorded in completion order:
// a test that wants one scanner's command has to name the stage. Taking the
// last command that merely looked like Trivy is how this went: two stages
// invoke Trivy, and whichever finished second won.
func (r *scriptedRunner) commandForStage(stage string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if stageOf(c) == stage {
			return c.String()
		}
	}
	return ""
}

func (r *scriptedRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// useRunner swaps the package runner for the duration of the test. Production
// never reassigns it, so tests that do must not run in parallel.
func useRunner(t *testing.T, r commandRunner) {
	t.Helper()
	previous := runner
	runner = r
	t.Cleanup(func() { runner = previous })
}

// answering builds a runner that gives every invocation the same reply.
func answering(t *testing.T, stdout string, err error) *scriptedRunner {
	t.Helper()
	r := &scriptedRunner{reply: func(toolCmd) ([]byte, error) { return []byte(stdout), err }}
	useRunner(t, r)
	return r
}

// ── Fixtures ─────────────────────────────────────────────────────────────────

func trivyReport(t *testing.T, result TrivyResult) string {
	t.Helper()
	data, err := json.Marshal(TrivyReport{Results: []TrivyResult{result}})
	if err != nil {
		t.Fatalf("building the fixture: %v", err)
	}
	return string(data)
}

func vulnReport(t *testing.T, id, severity string) string {
	t.Helper()
	return trivyReport(t, TrivyResult{
		Target:          "go.sum",
		Vulnerabilities: []TrivyVulnerability{{VulnerabilityID: id, Severity: severity, PkgName: "golang.org/x/net"}},
	})
}

func gitleaksReport(t *testing.T, rule string) string {
	t.Helper()
	data, err := json.Marshal([]GitleaksFinding{{
		RuleID: rule, File: "config.yaml", StartLine: 7,
		Secret: "AKIAIOSFODNN7EXAMPLE", Fingerprint: "config.yaml:" + rule + ":7",
	}})
	if err != nil {
		t.Fatalf("building the fixture: %v", err)
	}
	return string(data)
}

// ── Trivy ────────────────────────────────────────────────────────────────────

// Trivy exits 1 when it finds vulnerabilities. Treating that as a failure would
// discard the report in exactly the case the user cares about.
func TestFindingsSurviveTheExitCodeThatAnnouncesThem(t *testing.T) {
	answering(t, vulnReport(t, "CVE-2024-1", "HIGH"), &exitError{Code: 1, Stderr: "1 vulnerability"})

	findings, err := RunTrivy(context.Background(), "/repos", TargetDirectory, false,
		ToolSpec{Source: ToolSourceBinary}, "", false, false, nil)

	if err != nil {
		t.Fatalf("a scan that found something was reported as failing: %v", err)
	}
	if len(findings) != 1 || findings[0].ID != "CVE-2024-1" {
		t.Fatalf("findings = %+v, want the one CVE", findings)
	}
}

// An exit code with nothing on stdout is the shape of a real failure — there is
// no report to salvage.
func TestAnExitWithNoReportIsAFailure(t *testing.T) {
	answering(t, "", &exitError{Code: 2, Stderr: "FATAL invalid flag"})

	_, err := RunTrivy(context.Background(), "/repos", TargetDirectory, false,
		ToolSpec{Source: ToolSourceBinary}, "", false, false, nil)

	if err == nil {
		t.Fatal("a scan that produced nothing was reported as succeeding")
	}
	if !strings.Contains(err.Error(), "invalid flag") {
		t.Errorf("err = %v, want trivy's own diagnostic carried through", err)
	}
}

// A process that never ran has no exit code, so whatever is on stdout cannot be
// a report — it must not be parsed as one.
func TestAToolThatNeverRanIsAFailureWhateverIsOnStdout(t *testing.T) {
	answering(t, vulnReport(t, "CVE-2024-1", "HIGH"), errors.New("trivy failed to start"))

	if _, err := RunTrivy(context.Background(), "/repos", TargetDirectory, false,
		ToolSpec{Source: ToolSourceBinary}, "", false, false, nil); err == nil {
		t.Fatal("a tool that never started was reported as succeeding")
	}
}

func TestACleanScanWithNoFindingsIsNotAnError(t *testing.T) {
	answering(t, `{"Results":[]}`, nil)

	findings, err := RunTrivy(context.Background(), "/repos", TargetDirectory, false,
		ToolSpec{Source: ToolSourceBinary}, "", false, false, nil)

	if err != nil {
		t.Fatalf("RunTrivy: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %+v, want none", findings)
	}
}

// The target type is validated before anything is spawned, so an unsupported
// one costs no process.
func TestAnUnsupportedTargetTypeSpawnsNothing(t *testing.T) {
	r := answering(t, "", nil)

	if _, err := RunTrivy(context.Background(), "x", TargetType("registry"), false,
		ToolSpec{Source: ToolSourceBinary}, "", false, false, nil); err == nil {
		t.Error("an unsupported target type was accepted")
	}
	if _, err := RunTrivyMisconfig(context.Background(), "x", TargetType("registry"),
		ToolSpec{Source: ToolSourceBinary}, "", false, nil); err == nil {
		t.Error("an unsupported target type was accepted for misconfig")
	}

	if r.count() != 0 {
		t.Errorf("%d process(es) were spawned for a target that cannot be scanned", r.count())
	}
}

func TestProgressFromTheToolReachesTheCaller(t *testing.T) {
	r := answering(t, `{"Results":[]}`, nil)
	r.progress = []string{"downloading db", "done"}

	var seen []string
	if _, err := RunTrivy(context.Background(), "/repos", TargetDirectory, false,
		ToolSpec{Source: ToolSourceBinary}, "", false, false, func(line string) {
			seen = append(seen, line)
		}); err != nil {
		t.Fatalf("RunTrivy: %v", err)
	}

	if strings.Join(seen, "|") != "downloading db|done" {
		t.Errorf("progress = %v", seen)
	}
}

// The invocation that is run is the one the builder produced, which is the same
// one the UI shows (D19).
func TestTheScanRunsTheCommandTheUserWasShown(t *testing.T) {
	r := answering(t, `{"Results":[]}`, nil)

	if _, err := RunTrivy(context.Background(), "/repos", TargetDirectory, false,
		ToolSpec{Source: ToolSourceBinary}, "https://trivy:4954", true, true, nil); err != nil {
		t.Fatalf("RunTrivy: %v", err)
	}

	shown := GetTrivyCommand("/repos", TargetDirectory, false, ToolSpec{Source: ToolSourceBinary}, "https://trivy:4954", true, true)
	if got := r.commands(); len(got) != 1 || got[0] != shown {
		t.Errorf("ran %v, shown %q", got, shown)
	}
}

func TestTheMisconfigScanAsksForTheMisconfigScanner(t *testing.T) {
	r := answering(t, trivyReport(t, TrivyResult{
		Target: "Dockerfile",
		Misconfigurations: []TrivyMisconfiguration{
			{AVDID: "AVD-DS-0002", Title: "root user", Severity: "HIGH"},
		},
	}), nil)

	findings, err := RunTrivyMisconfig(context.Background(), "/repos", TargetDirectory,
		ToolSpec{Source: ToolSourceBinary}, "", false, nil)

	if err != nil {
		t.Fatalf("RunTrivyMisconfig: %v", err)
	}
	if len(findings) != 1 || findings[0].Source != "trivy-misconfig" {
		t.Fatalf("findings = %+v, want one misconfiguration", findings)
	}
	if cmds := r.commands(); len(cmds) != 1 || !strings.Contains(cmds[0], "--scanners misconfig") {
		t.Errorf("ran %v, want the misconfig scanner", cmds)
	}
}

// Scanner.Scan starts several Trivy stages at once for one target (vuln,
// secret, misconfig), and a batch scan runs several targets at once on top of
// that. Trivy's local cache accepts only one writer, so two of these
// processes actually running at the same instant is exactly the race that
// produced "unable to acquire cache or database lock" in the field.
func TestConcurrentTrivyStagesAreSerialized(t *testing.T) {
	var inFlight, overlapped int32
	r := &scriptedRunner{reply: func(toolCmd) ([]byte, error) {
		if atomic.AddInt32(&inFlight, 1) > 1 {
			atomic.StoreInt32(&overlapped, 1)
		}
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		return []byte(`{"Results":[]}`), nil
	}}
	useRunner(t, r)

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			_, _ = RunTrivy(context.Background(), "/repos", TargetDirectory, false,
				ToolSpec{Source: ToolSourceBinary}, "", false, false, nil)
		})
	}
	wg.Wait()

	if atomic.LoadInt32(&overlapped) != 0 {
		t.Error("two Trivy invocations ran at the same time — the cache-lock race is back")
	}
	if r.count() != 5 {
		t.Errorf("count = %d, want 5", r.count())
	}
}

// A run queued behind the Trivy semaphore must not wait past its own
// cancellation — K in :jobs cancels the scan's context, and that has to reach
// a stage that has not even started its process yet.
func TestAQueuedTrivyRunCanStillBeCancelled(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	r := &scriptedRunner{reply: func(toolCmd) ([]byte, error) {
		close(started)
		<-release
		return []byte(`{"Results":[]}`), nil
	}}
	useRunner(t, r)

	go func() {
		_, _ = RunTrivy(context.Background(), "/repos", TargetDirectory, false,
			ToolSpec{Source: ToolSourceBinary}, "", false, false, nil)
	}()
	<-started // the first run now holds the semaphore

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RunTrivy(ctx, "/repos", TargetDirectory, false,
		ToolSpec{Source: ToolSourceBinary}, "", false, false, nil)
	close(release)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled — a queued run must not wait past its own cancellation", err)
	}
}

// ── Gitleaks ─────────────────────────────────────────────────────────────────

func TestSecretsAreReturnedMasked(t *testing.T) {
	answering(t, gitleaksReport(t, "aws-access-token"), &exitError{Code: gitleaksSecretsFound})

	findings, err := RunGitleaks(context.Background(), "/repos", ToolSpec{Source: ToolSourceBinary}, false, "", nil)

	if err != nil {
		t.Fatalf("a scan that found a secret was reported as failing: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want one secret", findings)
	}
	if strings.Contains(findings[0].Match, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("the secret itself was carried through: %q", findings[0].Match)
	}
}

// A clean repository exits 0 and writes an empty report — measured on gitleaks
// v8.30.1, and it is what makes the next test true.
func TestACleanRepositoryIsNotAFailure(t *testing.T) {
	answering(t, "[]", nil)

	findings, err := RunGitleaks(context.Background(), "/repos", ToolSpec{Source: ToolSourceBinary}, false, "", nil)

	if err != nil {
		t.Fatalf("a clean scan was reported as failing: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %+v, want none", findings)
	}
}

// The one that was D56. Gitleaks exits 1 for two unrelated things — it found
// secrets, and it died before scanning anything — and the report is what
// separates them. This case was read as a clean repository, so a --config
// gitleaks could not load came back as a green icon in ws for a directory
// nobody had looked at.
func TestAnExitWithNoReportIsAFailureRatherThanACleanRepository(t *testing.T) {
	answering(t, "", &exitError{
		Code:   gitleaksSecretsFound,
		Stderr: "FTL unable to load gitleaks config, err: open /gitleaks.toml: no such file or directory",
	})

	findings, err := RunGitleaks(context.Background(), "/repos", ToolSpec{Source: ToolSourceBinary}, false, "", nil)

	if err == nil {
		t.Fatalf("a scan that read nothing was reported as clean: findings = %+v", findings)
	}
	// The tool's own diagnostic is the only thing that can say why: nothing on
	// DevDesk's side knows whether the config was missing or would not parse.
	if !strings.Contains(err.Error(), "unable to load gitleaks config") {
		t.Errorf("the failure did not carry gitleaks' reason: %v", err)
	}
}

// The guard is not there to catch a bad configuration — RunGitleaks reports
// that with gitleaks' own words — but to stop docker creating a directory where
// the file should have been (measured on Docker Desktop 29.7.2).
func TestAnUnreadableConfigIsRefusedBeforeAnythingStarts(t *testing.T) {
	r := answering(t, "[]", nil)

	missing := filepath.Join(t.TempDir(), "gitleaks.toml")
	if _, err := RunGitleaks(context.Background(), "/repos", ToolSpec{Source: ToolSourceDocker}, false, missing, nil); err == nil {
		t.Fatal("a config that is not there was accepted")
	}
	if cmds := r.commands(); len(cmds) != 0 {
		t.Errorf("the tool was started anyway: %v", cmds)
	}
}

func TestAnyOtherGitleaksExitIsAFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"a configuration error", &exitError{Code: 2, Stderr: "failed to load config"}},
		{"a process that never ran", errors.New("gitleaks failed to start")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			answering(t, "", tt.err)

			if _, err := RunGitleaks(context.Background(), "/repos", ToolSpec{Source: ToolSourceBinary}, false, "", nil); err == nil {
				t.Error("the failure was reported as a clean scan")
			}
		})
	}
}
