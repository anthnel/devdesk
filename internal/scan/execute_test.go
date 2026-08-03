package scan

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
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
		ToolSourceBinary, "", "", false, false, nil)

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
		ToolSourceBinary, "", "", false, false, nil)

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
		ToolSourceBinary, "", "", false, false, nil); err == nil {
		t.Fatal("a tool that never started was reported as succeeding")
	}
}

func TestACleanScanWithNoFindingsIsNotAnError(t *testing.T) {
	answering(t, `{"Results":[]}`, nil)

	findings, err := RunTrivy(context.Background(), "/repos", TargetDirectory, false,
		ToolSourceBinary, "", "", false, false, nil)

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
		ToolSourceBinary, "", "", false, false, nil); err == nil {
		t.Error("an unsupported target type was accepted")
	}
	if _, err := RunTrivyMisconfig(context.Background(), "x", TargetType("registry"),
		ToolSourceBinary, "", "", false, nil); err == nil {
		t.Error("an unsupported target type was accepted for misconfig")
	}
	if _, err := GenerateSBOM(context.Background(), "x", TargetType("registry"),
		ToolSourceBinary, "", "", ""); err == nil {
		t.Error("an unsupported target type was accepted for the SBOM")
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
		ToolSourceBinary, "", "", false, false, func(line string) {
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
		ToolSourceBinary, "", "https://trivy:4954", true, true, nil); err != nil {
		t.Fatalf("RunTrivy: %v", err)
	}

	shown := GetTrivyCommand("/repos", TargetDirectory, false, ToolSourceBinary, "", "https://trivy:4954", true, true)
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
		ToolSourceBinary, "", "", false, nil)

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

// ── SBOM ─────────────────────────────────────────────────────────────────────

// Unlike a scan, a non-zero exit here means no file was written, so there is
// nothing to hand back.
func TestAFailedSBOMYieldsNoPath(t *testing.T) {
	answering(t, "", &exitError{Code: 1, Stderr: "permission denied"})

	path, err := GenerateSBOM(context.Background(), "/repos", TargetDirectory, ToolSourceBinary, "", "", "")

	if err == nil {
		t.Fatal("a failed generation returned success")
	}
	if path != "" {
		t.Errorf("path = %q, want empty when nothing was written", path)
	}
}

func TestTheSBOMPathComesBackOnSuccess(t *testing.T) {
	r := answering(t, "", nil)

	path, err := GenerateSBOM(context.Background(), "/repos", TargetDirectory, ToolSourceBinary, "", "", "/out")

	if err != nil {
		t.Fatalf("GenerateSBOM: %v", err)
	}
	if !strings.HasSuffix(strings.ReplaceAll(path, "\\", "/"), "/out/sbom-report.json") {
		t.Errorf("path = %q", path)
	}
	if cmds := r.commands(); len(cmds) != 1 || !strings.Contains(cmds[0], "cyclonedx") {
		t.Errorf("ran %v, want a CycloneDX generation", cmds)
	}
}

// ── Gitleaks ─────────────────────────────────────────────────────────────────

func TestSecretsAreReturnedMasked(t *testing.T) {
	answering(t, gitleaksReport(t, "aws-access-token"), &exitError{Code: gitleaksSecretsFound})

	findings, err := RunGitleaks(context.Background(), "/repos", ToolSourceBinary, "", false, "", nil)

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

// Exit 1 with no report is how gitleaks says it found nothing worth writing —
// a clean repository, not a failure.
func TestACleanRepositoryIsNotAFailure(t *testing.T) {
	answering(t, "", &exitError{Code: gitleaksSecretsFound})

	findings, err := RunGitleaks(context.Background(), "/repos", ToolSourceBinary, "", false, "", nil)

	if err != nil {
		t.Fatalf("a clean scan was reported as failing: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %+v, want none", findings)
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

			if _, err := RunGitleaks(context.Background(), "/repos", ToolSourceBinary, "", false, "", nil); err == nil {
				t.Error("the failure was reported as a clean scan")
			}
		})
	}
}
