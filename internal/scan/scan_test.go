package scan

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Scan runs its stages concurrently against one runner, so a test says what
// each stage answers rather than what the next call answers.

type stageReply struct {
	stdout string
	err    error
}

// stageOf names the stage an invocation belongs to, from the invocation alone —
// which is the only thing the runner sees.
//
// "secret" and "trivy-secret" are separate: secret scanning is the one option
// served by both tools, Gitleaks over git history and Trivy over the target's
// content, and only Trivy's half applies to an image.
func stageOf(tc toolCmd) string {
	s := tc.String()
	switch {
	case strings.Contains(s, "--scanners license"):
		return "license"
	case strings.Contains(s, "--scanners misconfig"):
		return "misconfig"
	case strings.Contains(s, "--scanners secret"):
		return "trivy-secret"
	case strings.HasPrefix(s, "gitleaks"):
		return "secret"
	default:
		return "vuln"
	}
}

func byStage(t *testing.T, replies map[string]stageReply) *scriptedRunner {
	t.Helper()
	r := &scriptedRunner{}
	r.reply = func(tc toolCmd) ([]byte, error) {
		stage := stageOf(tc)
		reply, scripted := replies[stage]
		if !scripted {
			t.Errorf("the %s stage ran although nothing was scripted for it: %s", stage, tc)
		}
		return []byte(reply.stdout), reply.err
	}
	useRunner(t, r)
	return r
}

// stagesRun reports which stages the runner was actually asked to execute.
func stagesRun(r *scriptedRunner) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		seen = append(seen, stageOf(c))
	}
	sort.Strings(seen)
	return seen
}

func everyTool() DependencyStatus {
	return DependencyStatus{
		TrivyAvailable:    true,
		TrivySource:       ToolSourceBinary,
		TrivyImage:        DefaultTrivyImage,
		GitleaksAvailable: true,
		GitleaksSource:    ToolSourceBinary,
		GitleaksImage:     DefaultGitleaksImage,
		DockerAvailable:   true,
	}
}

func everyStage() ScanOptions {
	return ScanOptions{
		EnableVuln:      true,
		EnableSecret:    true,
		EnableLicense:   true,
		EnableMisconfig: true,
	}
}

// recorder collects progress from the scan goroutines.
type recorder struct {
	mu      sync.Mutex
	updates []ProgressUpdate
}

func (rec *recorder) fn() func(ProgressUpdate) {
	return func(u ProgressUpdate) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.updates = append(rec.updates, u)
	}
}

// statusOf returns the transitions reported for one stage, in order. A running
// update carrying a detail is the tool talking, not a transition, so it is left
// out — otherwise every line of a database download would read as a state
// change.
func (rec *recorder) statusOf(stage string) []StageStatus {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var out []StageStatus
	for _, u := range rec.updates {
		if u.Stage != stage || (u.Status == StageRunning && u.Detail != "") {
			continue
		}
		out = append(out, u.Status)
	}
	return out
}

func licenseReport(t *testing.T) string {
	t.Helper()
	return trivyReport(t, TrivyResult{Licenses: []TrivyLicense{
		{Name: "GPL-3.0", Category: "restricted", PkgName: "somelib", Severity: "MEDIUM", FilePath: "go.mod"},
	}})
}

func trivySecretReport(t *testing.T, ruleID string) string {
	t.Helper()
	return trivyReport(t, TrivyResult{Target: "app/.env", Secrets: []TrivySecret{
		{RuleID: ruleID, Category: "AWS", Severity: "CRITICAL", Title: "AWS Secret Access Key",
			StartLine: 3, Match: "AKIAIOSFODNN7EXAMPLE"},
	}})
}

func misconfigReport(t *testing.T) string {
	t.Helper()
	return trivyReport(t, TrivyResult{Target: "Dockerfile", Misconfigurations: []TrivyMisconfiguration{
		{AVDID: "AVD-DS-0002", Title: "root user", Severity: "HIGH"},
	}})
}

// ── What a full scan produces ────────────────────────────────────────────────

func TestEveryEnabledStageContributesItsFindings(t *testing.T) {
	byStage(t, map[string]stageReply{
		"vuln":         {stdout: vulnReport(t, "CVE-2024-1", "CRITICAL"), err: &exitError{Code: 1}},
		"license":      {stdout: licenseReport(t)},
		"misconfig":    {stdout: misconfigReport(t)},
		"secret":       {stdout: gitleaksReport(t, "aws-access-token"), err: &exitError{Code: gitleaksSecretsFound}},
		"trivy-secret": {stdout: trivySecretReport(t, "aws-secret-access-key")},
	})

	result, err := newScannerWithDeps(everyStage(), everyTool()).
		Scan(context.Background(), "/repos", TargetDirectory)

	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("a scan where every stage succeeded recorded errors: %v", result.Errors)
	}

	// Each kind lands in its own counter, which is what the four result tabs
	// are built from.
	if result.Counts.Critical != 1 {
		t.Errorf("Counts.Critical = %d, want 1", result.Counts.Critical)
	}
	// Both secret scanners feed the one counter, and the one tab: Gitleaks reads
	// git history, Trivy reads the target's content, and neither sees what the
	// other does.
	if result.SecretCount != 2 {
		t.Errorf("SecretCount = %d, want the gitleaks and the trivy secret", result.SecretCount)
	}
	if result.LicenseCount != 1 {
		t.Errorf("LicenseCount = %d, want 1", result.LicenseCount)
	}
	if result.MisconfigCount != 1 {
		t.Errorf("MisconfigCount = %d, want 1", result.MisconfigCount)
	}
	if result.Target != "/repos" || result.TargetType != TargetDirectory {
		t.Errorf("the result does not describe what was scanned: %+v", result.Target)
	}
	if result.EndTime.Before(result.StartTime) {
		t.Error("the scan ended before it started")
	}
}

// ── What decides a stage runs at all ─────────────────────────────────────────

// A stage whose tool is missing is still not run — but it is reported, which is
// the change D20 made. It used to be skipped silently on the grounds that the
// dashboard already says the tool is absent; that reasoning does not survive
// contact with the result panel, where "no secrets found" and "nothing looked
// for secrets" are the same screen.
func TestAStageWithoutItsToolIsReportedRatherThanSkippedSilently(t *testing.T) {
	deps := everyTool()
	deps.GitleaksAvailable = false

	r := byStage(t, map[string]stageReply{
		"vuln":         {stdout: `{"Results":[]}`},
		"trivy-secret": {stdout: `{"Results":[]}`},
	})

	result, err := newScannerWithDeps(
		ScanOptions{EnableVuln: true, EnableSecret: true}, deps).
		Scan(context.Background(), "/repos", TargetDirectory)

	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// Trivy's half of the secret scan still runs — it is Gitleaks that is
	// missing, and saying so is the point of D20.
	if got := stagesRun(r); strings.Join(got, ",") != "trivy-secret,vuln" {
		t.Errorf("stages run = %v, want the gitleaks stage skipped and trivy's kept", got)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "gitleaks") {
		t.Fatalf("Errors = %v, want one naming the tool that is missing", result.Errors)
	}
	// The message has to be actionable: the user needs to know what to install.
	if !strings.Contains(result.Errors[0], DefaultGitleaksImage) {
		t.Errorf("Errors[0] = %q, want it to name the image that would do instead", result.Errors[0])
	}
}

// D20. With no scanner at all, every stage is skipped, so the result carried
// no findings and no errors — indistinguishable from a clean scan, and the OCI
// images view cached it as one.
func TestAScanThatCouldRunNoScannerIsNotACleanScan(t *testing.T) {
	deps := everyTool()
	deps.TrivyAvailable = false

	r := byStage(t, map[string]stageReply{})

	result, err := newScannerWithDeps(
		ScanOptions{EnableVuln: true}, deps).
		Scan(context.Background(), "api:v1", TargetImage)

	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if r.count() != 0 {
		t.Errorf("%d process(es) ran with no scanner installed", r.count())
	}
	if result.TotalFindings() != 0 {
		t.Errorf("TotalFindings = %d, want none", result.TotalFindings())
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "trivy") {
		t.Fatalf("Errors = %v, want one naming trivy", result.Errors)
	}
	// This pairing is what callers dispatch on: scanOneImageCmd reports a
	// failure when there are errors and no findings, which is exactly this.
	if result.TotalFindings() != 0 && len(result.Errors) > 0 {
		t.Error("the result is ambiguous between a failure and a partial scan")
	}
}

// A stage that does not apply to the target type is not missing anything.
// Gitleaks scans a working tree, so its half of the secret scan is skipped on an
// image for a reason that has nothing to do with what is installed — reporting
// it would train the user to ignore the warnings panel.
func TestAStageThatDoesNotApplyToTheTargetIsNotAMissingTool(t *testing.T) {
	deps := everyTool()
	deps.GitleaksAvailable = false

	byStage(t, map[string]stageReply{
		"vuln":         {stdout: `{"Results":[]}`},
		"trivy-secret": {stdout: `{"Results":[]}`},
	})

	result, err := newScannerWithDeps(
		ScanOptions{EnableVuln: true, EnableSecret: true}, deps).
		Scan(context.Background(), "api:v1", TargetImage)

	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want none — a secret scan of an image was never going to run", result.Errors)
	}
}

// Nothing enabled is not a missing tool either: the user asked for no scan.
func TestNothingEnabledReportsNoMissingTool(t *testing.T) {
	byStage(t, map[string]stageReply{})

	result, err := newScannerWithDeps(ScanOptions{}, DependencyStatus{}).
		Scan(context.Background(), "/repos", TargetDirectory)

	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want none when no stage was asked for", result.Errors)
	}
}

// Licences come from a package manifest and Gitleaks reads a git history, so
// neither has anything to read in an image. Trivy's secret scan does: it reads
// the image's layers, which is what gives an image scan a secret stage at all.
func TestAnImageIsScannedForSecretsByTrivyOnly(t *testing.T) {
	r := byStage(t, map[string]stageReply{
		"vuln":         {stdout: `{"Results":[]}`},
		"misconfig":    {stdout: `{"Results":[]}`},
		"trivy-secret": {stdout: `{"Results":[]}`},
	})

	if _, err := newScannerWithDeps(everyStage(), everyTool()).
		Scan(context.Background(), "api:v1", TargetImage); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := strings.Join(stagesRun(r), ",")
	if got != "misconfig,trivy-secret,vuln" {
		t.Errorf("stages run = %s, want licence and gitleaks left out and trivy's secret scan kept", got)
	}
}

// An image's secrets were parsed and dropped: Trivy's default scanners for an
// image are "vuln,secret", so the vulnerability stage was paying for a secret
// scan whose output nothing read. Asking for one scanner per stage is what
// stops the same secret being reported twice now that it is read.
func TestTheVulnerabilityStageDoesNotAlsoScanForSecrets(t *testing.T) {
	r := byStage(t, map[string]stageReply{"vuln": {stdout: `{"Results":[]}`}})

	if _, err := newScannerWithDeps(ScanOptions{EnableVuln: true}, everyTool()).
		Scan(context.Background(), "api:v1", TargetImage); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if !strings.Contains(c.String(), "--scanners vuln") {
			t.Errorf("the vulnerability stage ran %q, want it limited to --scanners vuln", c)
		}
	}
}

func TestAScanWithNothingEnabledRunsNothing(t *testing.T) {
	r := byStage(t, map[string]stageReply{})

	result, err := newScannerWithDeps(ScanOptions{}, everyTool()).
		Scan(context.Background(), "/repos", TargetDirectory)

	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if r.count() != 0 {
		t.Errorf("%d process(es) ran for a scan with no stage enabled", r.count())
	}
	if result.TotalFindings() != 0 {
		t.Errorf("TotalFindings = %d, want 0", result.TotalFindings())
	}
}

// ── When a stage fails ───────────────────────────────────────────────────────

// The stages are independent, so one failing must not cost the user the results
// of the others — that is the whole reason they run in parallel rather than in
// sequence with an early return.
func TestOneStageFailingLeavesTheOthersIntact(t *testing.T) {
	byStage(t, map[string]stageReply{
		"vuln":         {stdout: vulnReport(t, "CVE-2024-9", "HIGH")},
		"secret":       {err: errors.New("gitleaks failed to start")},
		"trivy-secret": {stdout: `{"Results":[]}`},
	})

	result, err := newScannerWithDeps(
		ScanOptions{EnableVuln: true, EnableSecret: true}, everyTool()).
		Scan(context.Background(), "/repos", TargetDirectory)

	if err != nil {
		t.Fatalf("Scan returned an error although a stage failure is a result: %v", err)
	}
	if result.Counts.High != 1 {
		t.Errorf("Counts.High = %d, want the vulnerability that was found anyway", result.Counts.High)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "gitleaks") {
		t.Errorf("Errors = %v, want one naming gitleaks", result.Errors)
	}
}

func TestEveryStageReportsItsOwnFailure(t *testing.T) {
	failing := &exitError{Code: 2, Stderr: "FATAL"}
	byStage(t, map[string]stageReply{
		"vuln":         {err: failing},
		"license":      {err: failing},
		"misconfig":    {err: failing},
		"secret":       {err: failing},
		"trivy-secret": {err: failing},
	})

	result, err := newScannerWithDeps(everyStage(), everyTool()).
		Scan(context.Background(), "/repos", TargetDirectory)

	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Errors) != 5 {
		t.Fatalf("Errors = %v, want one per stage", result.Errors)
	}
	// Each entry has to say which stage it came from, or the footer message is
	// unactionable. The two secret scanners are named apart for that reason:
	// "gitleaks" and "trivy secret" fail for different causes and are fixed by
	// different things.
	joined := strings.Join(result.Errors, "\n")
	for _, want := range []string{"trivy vuln", "trivy license", "trivy misconfig", "gitleaks", "trivy secret"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no error naming %q:\n%s", want, joined)
		}
	}
}

// ── Progress ─────────────────────────────────────────────────────────────────

func TestAStageIsAnnouncedBeforeItRunsAndAgainWhenItIsOver(t *testing.T) {
	r := byStage(t, map[string]stageReply{"vuln": {stdout: `{"Results":[]}`}})
	r.progress = []string{"downloading db"}

	rec := &recorder{}
	opts := ScanOptions{EnableVuln: true, OnProgress: rec.fn()}

	if _, err := newScannerWithDeps(opts, everyTool()).
		Scan(context.Background(), "/repos", TargetDirectory); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := rec.statusOf("vuln")
	if len(got) != 2 || got[0] != StageRunning || got[1] != StageDone {
		t.Errorf("vuln statuses = %v, want running then done", got)
	}

	// The tool's own output is forwarded as detail on the running stage, which
	// is what fills the line under the spinner.
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var sawDetail bool
	for _, u := range rec.updates {
		if u.Stage == "vuln" && u.Detail == "downloading db" && u.Status == StageRunning {
			sawDetail = true
		}
	}
	if !sawDetail {
		t.Errorf("the tool's progress was not forwarded: %+v", rec.updates)
	}
}

// Every stage forwards the tool's own output under its own label. They run at
// the same time against the same tools, so a line arriving unlabelled — or
// labelled with another stage — would land under the wrong spinner.
func TestEachStageLabelsItsOwnProgress(t *testing.T) {
	r := byStage(t, map[string]stageReply{
		"vuln":         {stdout: `{"Results":[]}`},
		"license":      {stdout: `{"Results":[]}`},
		"misconfig":    {stdout: `{"Results":[]}`},
		"secret":       {stdout: "[]"},
		"trivy-secret": {stdout: `{"Results":[]}`},
	})
	r.progress = []string{"downloading db"}

	rec := &recorder{}
	opts := everyStage()
	opts.OnProgress = rec.fn()

	if _, err := newScannerWithDeps(opts, everyTool()).
		Scan(context.Background(), "/repos", TargetDirectory); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	labelled := map[string]string{}
	for _, u := range rec.updates {
		if u.Detail == "downloading db" {
			labelled[u.Stage] = u.Label
		}
	}

	for _, stage := range []string{"vuln", "license", "misconfig", "secret", "trivy-secret"} {
		if labelled[stage] == "" {
			t.Errorf("the %s stage did not forward the tool's progress under a label", stage)
		}
	}
	// The two secret stages share a spinner row only if they share a stage id,
	// and they must not: one can finish while the other is still running.
	if labelled["secret"] == labelled["trivy-secret"] {
		t.Errorf("both secret stages report the label %q, so they collapse onto one row", labelled["secret"])
	}
}

func TestAFailedStageIsAnnouncedAsFailed(t *testing.T) {
	byStage(t, map[string]stageReply{"vuln": {err: &exitError{Code: 2, Stderr: "FATAL bad flag"}}})

	rec := &recorder{}
	opts := ScanOptions{EnableVuln: true, OnProgress: rec.fn()}

	if _, err := newScannerWithDeps(opts, everyTool()).
		Scan(context.Background(), "/repos", TargetDirectory); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := rec.statusOf("vuln")
	if len(got) == 0 || got[len(got)-1] != StageError {
		t.Errorf("vuln statuses = %v, want it to end in error", got)
	}
}

// OnProgress is optional; a caller that does not want progress must not have to
// supply an empty function to avoid a panic.
func TestAScanWithoutAProgressCallbackIsFine(t *testing.T) {
	byStage(t, map[string]stageReply{"vuln": {stdout: `{"Results":[]}`}})

	if _, err := newScannerWithDeps(ScanOptions{EnableVuln: true}, everyTool()).
		Scan(context.Background(), "/repos", TargetDirectory); err != nil {
		t.Fatalf("Scan: %v", err)
	}
}

// ── Options that reach the tools ─────────────────────────────────────────────

// The options the user set in the form have to arrive at the invocation; there
// is no other way to tell they were honoured.
func TestScanOptionsReachTheInvocation(t *testing.T) {
	r := byStage(t, map[string]stageReply{
		"vuln":         {stdout: `{"Results":[]}`},
		"secret":       {stdout: "[]"},
		"trivy-secret": {stdout: `{"Results":[]}`},
	})

	opts := ScanOptions{
		EnableVuln:      true,
		EnableSecret:    true,
		TrivyServer:     "https://trivy:4954",
		IgnoreUnfixed:   true,
		IgnoreEOL:       true,
		GitleaksHistory: true,
		GitleaksConfig:  "/etc/gitleaks.toml",
	}

	if _, err := newScannerWithDeps(opts, everyTool()).
		Scan(context.Background(), "/repos", TargetDirectory); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	vuln := r.commandForStage("vuln")
	trivySecret := r.commandForStage("trivy-secret")
	gitleaks := r.commandForStage("secret")

	for _, want := range []string{"--server https://trivy:4954", "--ignore-unfixed", "--ignore-status end_of_life"} {
		if !strings.Contains(vuln, want) {
			t.Errorf("the trivy vuln invocation is missing %q:\n%s", want, vuln)
		}
	}
	// The secret stage takes a server too, and it is the one option it shares
	// with the vuln stage — the other two do not apply to it.
	if !strings.Contains(trivySecret, "--server https://trivy:4954") {
		t.Errorf("the trivy secret invocation is missing the server:\n%s", trivySecret)
	}
	if !strings.Contains(gitleaks, "--config /etc/gitleaks.toml") {
		t.Errorf("the gitleaks config was not passed:\n%s", gitleaks)
	}
	if strings.Contains(gitleaks, "--no-git") {
		t.Errorf("history was asked for but git was still skipped:\n%s", gitleaks)
	}
}
