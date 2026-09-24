package scan

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/engine"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// TargetType represents the type of scan target
type TargetType string

const (
	TargetDirectory TargetType = "directory"
	TargetImage     TargetType = "image"
)

// SeverityLevel represents vulnerability severity
type SeverityLevel string

const (
	SeverityCritical SeverityLevel = "CRITICAL"
	SeverityHigh     SeverityLevel = "HIGH"
	SeverityMedium   SeverityLevel = "MEDIUM"
	SeverityLow      SeverityLevel = "LOW"
	SeverityUnknown  SeverityLevel = "UNKNOWN"
)

// StageStatus represents the execution status of a scan stage
type StageStatus string

const (
	StageRunning StageStatus = "running"
	StageDone    StageStatus = "done"
	StageError   StageStatus = "error"
	StageSkipped StageStatus = "skipped"
)

// ProgressUpdate carries structured progress notifications from the scanner to the UI.
// It is emitted by OnProgress at the start, during, and on completion of each stage.
type ProgressUpdate struct {
	Stage  string      // unique identifier: "vuln", "secret", "trivy-secret", "license", "misconfig", "build-context"...
	Label  string      // human-readable label
	Status StageStatus // current status of the stage
	Detail string      // optional detail (e.g., DB download progress from Trivy stderr)
}

// ScanOptions configures which scanners to run
type ScanOptions struct {
	// Categories is what the scan looks for and, per category, which tools
	// run it (§3.86). Uses reads it.
	Categories config.ScanCategories
	// Tools is every tool's settings: where it runs from, and its own options.
	Tools config.ScanTools
	// TrivyServer is the address a scan sends Trivy to, and empty unless the
	// client-server mode is on: the address is kept in the config while the
	// mode is off, and a scan must not see it then. OptionsFromConfig resolves
	// it once, so no stage re-reads the switch.
	TrivyServer string
	// Detected is a detection already made — the router's, shared by every
	// view — so a scan does not probe the machine again. Nil detects, which is
	// what a caller with no report yet gets. A tool that disappeared between
	// that detection and the scan fails at its stage, with the stage's message.
	Detected *Report

	// Forge is the platform this context targets, and it is what decides
	// whether a repository is graded at all: a GitHub context grades its GitHub
	// repositories and nothing else (§3.42). The scan resolves that per target
	// rather than per batch, because one batch holds repositories with
	// different remotes.
	Forge config.ForgeConfig
	// LoadForgeToken is called only for a repository whose remote is the
	// configured forge, so one of another host never reaches the secret store.
	// It runs on a scan goroutine, like the clone's tokenLoader.
	LoadForgeToken func() string
	// OnProgress is an optional callback invoked at each stage transition.
	// It is called from scan goroutines; implementations must be thread-safe.
	OnProgress func(ProgressUpdate)
}

// Finding represents a single security finding
type Finding struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Severity    SeverityLevel `json:"severity"`
	Source      string        `json:"source"` // "trivy" or "gitleaks"
	File        string        `json:"file"`
	Line        int           `json:"line"`
	Match       string        `json:"match,omitempty"`       // For secrets
	Fingerprint string        `json:"fingerprint,omitempty"` // Gitleaks fingerprint for .gitleaksignore
	PkgName     string        `json:"pkg_name,omitempty"`    // For vulnerabilities
	Version     string        `json:"version,omitempty"`
	FixedIn     string        `json:"fixed_in,omitempty"`
	Resolution  string        `json:"resolution,omitempty"`  // Recommended fix steps
	References  []string      `json:"references,omitempty"`  // Links to advisories or documentation
	FixCommand  string        `json:"fix_command,omitempty"` // Suggested command to run
	// Class and Ecosystem say what kind of package a vulnerability sits in:
	// Trivy's Result.Class (os-pkgs or lang-pkgs) and Result.Type (alpine,
	// debian, gomod, npm...). The class decides the fix — a base image bump
	// clears an os-pkgs CVE and does nothing for a lang-pkgs one. Both are
	// empty on results cached before they were recorded, which reads as
	// "unknown" and is never counted as either class.
	Class     string `json:"class,omitempty"`
	Ecosystem string `json:"ecosystem,omitempty"`
	// EndLine, Message and Status belong to a misconfiguration (§3.78). Trivy
	// has always reported them and they were dropped when the Finding was
	// built, which left a caller knowing where a faulty block starts and not
	// where it ends — enough to read, not enough to replace.
	//
	// EndLine is the last line of that block, Line being its first. Message is
	// this instance's own wording ("Specify at least 1 USER command") where
	// Description carries the rule's generic text, and Status is what Trivy
	// concluded for the rule on this target.
	//
	// All three are empty on a result cached before they were recorded, which
	// reads as "unknown" — an EndLine of zero is never a line number — and they
	// reappear at the next scan. Same convention as Class and Ecosystem above,
	// and no cache migration for the same reason.
	EndLine int    `json:"end_line,omitempty"`
	Message string `json:"message,omitempty"`
	Status  string `json:"status,omitempty"`
	// Job and ScriptLine are where a CI finding sits inside the pipeline
	// (§3.42). plumber grades the configuration GitLab derives from the
	// repository — includes and components resolved server-side — so a finding
	// is about a job the repository very often does not contain, and its own
	// file link points at the `include:` entry that brought it in rather than
	// at the offending line.
	//
	// These two are the anchors that survive that: the job the control was
	// looking at, and, on the controls that read a script, the offending line
	// verbatim. Both are empty on the controls that are about the project
	// rather than the pipeline — branch protection, approval rules — and on
	// those about an include, which name nothing at all.
	//
	// ScriptLine is not a secret and must not become one: it is the pipeline's
	// own source, already visible to anyone who can read the repository. The
	// MCP `finding` type declares its fields one by one, so nothing here
	// reaches it without being added there deliberately.
	Job        string `json:"job,omitempty"`
	ScriptLine string `json:"script_line,omitempty"`
	// IaCType is the dialect a misconfiguration was found in: Trivy's
	// Result.Type — "dockerfile", "kubernetes", "helm", "terraform",
	// "cloudformation"… — and "kubernetes" for a schema finding (§3.80). It is
	// what tells a Deployment's KSV rule from a Dockerfile's DS rule without
	// reading the file name, and what the fix catalog checks before touching a
	// file: a finding in a Helm template points at a template, not at YAML it
	// can edit.
	//
	// Empty on anything that is not a misconfiguration, and on a result cached
	// before it was recorded — "unknown", like Class and Ecosystem.
	IaCType string `json:"iac_type,omitempty"`
}

// SeverityCounts holds counts by severity level
type SeverityCounts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Unknown  int `json:"unknown"`
}

// Result represents the aggregated scan results
type Result struct {
	Target      string         `json:"target"`
	TargetType  TargetType     `json:"target_type"`
	StartTime   time.Time      `json:"start_time"`
	EndTime     time.Time      `json:"end_time"`
	Duration    time.Duration  `json:"duration"`
	Counts      SeverityCounts `json:"counts"`       // CVE counts by severity
	SecretCount int            `json:"secret_count"` // Total secrets found (gitleaks + trivy)
	// SecretsScanned tells "no secret was found" apart from "no one looked".
	// A secrets stage can fail to run for three reasons — the option is off,
	// the tool is missing, the stage errored — and in all three cases a
	// counter stuck at zero would say "this target is clean" about a scan
	// that never looked at it. It is the same principle as missingToolErrors
	// below (§1.3 D20), carried through to what the views display.
	SecretsScanned bool `json:"secrets_scanned"`
	LicenseCount   int  `json:"license_count"`   // Total license issues found
	MisconfigCount int  `json:"misconfig_count"` // Total misconfigurations found
	// MisconfigScanned tells "no misconfiguration was found" apart from "no one
	// looked", exactly as SecretsScanned does two fields up. The category has
	// two stages — Trivy's security rules and kubeconform's schema validation —
	// and either can be cut by the option, by a missing tool, or fail; in all of
	// those a counter stuck at zero would call a target clean that nothing ever
	// read.
	MisconfigScanned bool `json:"misconfig_scanned"`
	// MisconfigWorst is the highest severity among the misconfigurations, and it
	// is what colours the column.
	//
	// One column rather than four: the C/H/M/L counters beside it count
	// vulnerabilities only, and splitting misconfigurations the same way would
	// spend sixteen cells in the narrowest table of the application answering a
	// question nobody asks of them — a misconfiguration backlog is read whole,
	// not severity by severity, and what one wants from the list is whether
	// there is a CRITICAL in it. Empty when there are none.
	MisconfigWorst SeverityLevel `json:"misconfig_worst,omitempty"`
	CIIssueCount   int           `json:"ci_issue_count"` // Total CI-configuration issues found
	Findings       []Finding     `json:"findings"`
	Errors         []string      `json:"errors,omitempty"`

	// CIScanned tells "this pipeline was graded" apart from "no one looked",
	// exactly as SecretsScanned does one field up: the stage can fail to run
	// four ways — the option is off, plumber is absent, the repository is not
	// this context's forge, or the run errored — and in all four a letter would
	// be a claim nobody made.
	CIScanned bool `json:"ci_scanned"`
	// CIScore is the letter, and it is **empty on a withheld run**. plumber
	// writes one anyway, and it flatters: on the reference fixture the degraded
	// run reads B/79 where the complete run reads E/30, because a control that
	// did not run found nothing.
	CIScore string `json:"ci_score,omitempty"`
	// CIPoints is finalPoints out of 100, a float — see PlumberReport.Points.
	CIPoints float64 `json:"ci_points,omitempty"`
	// CIWithheld is a run that could not conclude, and CIReasons is what it
	// could not collect. Together they are the `?` of the CI column.
	CIWithheld bool     `json:"ci_withheld,omitempty"`
	CIReasons  []string `json:"ci_reasons,omitempty"`
	// CIMissing is a repository with no pipeline at all, which is not a bad
	// score — it is the absence of the thing being scored.
	CIMissing bool `json:"ci_missing,omitempty"`

	// K8sUnrendered are the Helm charts and Kustomize overlays the schema
	// stage could not validate because nothing rendered them (§3.80). Not an
	// error — the scan did all it could — and not "clean" either: it is what
	// keeps "nothing was found there" apart from "nobody looked there".
	K8sUnrendered []string `json:"k8s_unrendered,omitempty"`
}

// CIVerdict is what this scan can say about the pipeline's grade, for the cache
// and the column: nil when nothing graded it, the letter otherwise.
//
// It is the only calculation of the verdict, on SecretVerdict's model and for
// its reason — there were two of those, written the same way, and both became
// wrong on the same day (§3.12). A withheld run answers nil rather than its
// letter: nobody concluded, so nobody may be quoted.
func (r *Result) CIVerdict() *string {
	if !r.CIScanned || r.CIWithheld || r.CIScore == "" {
		return nil
	}
	score := r.CIScore
	return &score
}

// MisconfigSummary is what a scan concluded about misconfigurations: how many
// there are, how bad the worst one is, and how much of the target no stage
// could read at all.
//
// Unrendered is len(K8sUnrendered), carried here because the caches keep this
// summary and not the whole result. A Helm chart nobody rendered is not a clean
// chart: a row printing "0" for a repository of charts would be exactly the
// confusion SecretsScanned exists to prevent, one category over.
type MisconfigSummary struct {
	Count      int           `json:"count"`
	Worst      SeverityLevel `json:"worst,omitempty"`
	Unrendered int           `json:"unrendered,omitempty"`
}

// Total, WorstSeverity and UnrenderedCount read a summary that may be nil,
// which is the state "nobody looked". They exist so a caller can hand the three
// numbers to internal/ui/theme without unwrapping the pointer first: that
// package takes plain values on purpose — it does not import this one, which is
// also why SeverityTextStyle takes a string.
func (m *MisconfigSummary) Total() int {
	if m == nil {
		return 0
	}
	return m.Count
}

// WorstSeverity is the highest severity present, as the string theme wants.
func (m *MisconfigSummary) WorstSeverity() string {
	if m == nil {
		return ""
	}
	return string(m.Worst)
}

// UnrenderedCount is how many charts or overlays nothing rendered.
func (m *MisconfigSummary) UnrenderedCount() int {
	if m == nil {
		return 0
	}
	return m.Unrendered
}

// MisconfigVerdict is what this scan can say about misconfigurations, for the
// caches and the column: nil when no stage looked, a summary otherwise.
//
// Third of its kind after SecretVerdict and CIVerdict, and the only calculation
// — the workspaces list, the security inventory and the images list all read
// this one rather than counting findings again for themselves.
func (r *Result) MisconfigVerdict() *MisconfigSummary {
	if !r.MisconfigScanned {
		return nil
	}
	return &MisconfigSummary{
		Count:      r.MisconfigCount,
		Worst:      r.MisconfigWorst,
		Unrendered: len(r.K8sUnrendered),
	}
}

// severityRank orders the severities so two of them can be compared.
//
// Unknown ranks above nothing and below LOW: a rule whose severity the tool did
// not state must not outrank one that did, and must not be mistaken for the
// empty value that means "there are none".
func severityRank(s SeverityLevel) int {
	switch s {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	case SeverityUnknown:
		return 1
	default:
		return 0
	}
}

// WorseSeverity returns whichever of the two is higher. Exported because the
// workspaces list folds the summaries of the repositories under a directory,
// and that fold must order severities the same way this package does.
func WorseSeverity(a, b SeverityLevel) SeverityLevel {
	if severityRank(b) > severityRank(a) {
		return b
	}
	return a
}

// recordStageError puts a stage's failure where it can be found.
//
// The result carries it for the caches and the callers; the **log** carries the
// detail, because the views do not print a tool's stderr any more — Rule 128's
// arrangement, which this package was not part of: a stage that failed appended
// to result.Errors and logged nothing, so the only copy of the reason was on
// screen, in a panel that replaced the findings. The footer now says to look in
// the log, and this is what makes that true.
func recordStageError(result *Result, stage string, err error) {
	log.Printf("ERROR [scan/%s] stage failed: %v", stage, err)
	result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", stage, err))
}

// CountFindings aggregates findings into separate counters by category and
// severity. Vulnerabilities are counted by severity into Counts; secrets,
// licenses and misconfigurations into their dedicated fields.
//
// The category comes from Categorize, which the results tabs also use — these
// counters and the tab labels are the same numbers, and used to be computed by
// two rules that disagreed (see category.go).
func (r *Result) CountFindings() {
	r.Counts = SeverityCounts{}
	r.SecretCount = 0
	r.LicenseCount = 0
	r.MisconfigCount = 0
	r.MisconfigWorst = ""
	r.CIIssueCount = 0
	for _, f := range r.Findings {
		switch Categorize(f) {
		case CategorySecret:
			r.SecretCount++
		case CategoryLicense:
			r.LicenseCount++
		case CategoryMisconfiguration:
			r.MisconfigCount++
			r.MisconfigWorst = WorseSeverity(r.MisconfigWorst, f.Severity)
		case CategoryCIScore:
			r.CIIssueCount++
		case CategoryVulnerability:
			r.countBySeverity(f.Severity)
		}
	}
}

// countBySeverity adds one vulnerability to the severity histogram.
func (r *Result) countBySeverity(severity SeverityLevel) {
	switch severity {
	case SeverityCritical:
		r.Counts.Critical++
	case SeverityHigh:
		r.Counts.High++
	case SeverityMedium:
		r.Counts.Medium++
	case SeverityLow:
		r.Counts.Low++
	default:
		r.Counts.Unknown++
	}
}

// SecretVerdict is what this scan can say about secrets, for the caches and the
// columns that show a verdict rather than a count: nil when no stage looked,
// false when one looked and found nothing, true when one found something.
//
// It is the only verdict calculation in the application. There used to be
// two, both written as "a finding whose Source is gitleaks", and both became
// wrong the day Trivy started finding secrets too: a repo whose only secrets
// were Trivy's read as clean. SecretCount comes from Categorize, which is
// the only classifier (§3.11).
func (r *Result) SecretVerdict() *bool {
	if !r.SecretsScanned {
		return nil
	}
	found := r.SecretCount > 0
	return &found
}

// TotalFindings returns the total number of findings across all types
func (r *Result) TotalFindings() int {
	return r.Counts.Critical + r.Counts.High + r.Counts.Medium + r.Counts.Low + r.Counts.Unknown +
		r.SecretCount + r.LicenseCount + r.MisconfigCount + r.CIIssueCount
}

// ToolSource indicates how a tool is available
type ToolSource string

const (
	ToolSourceNone   ToolSource = "none"
	ToolSourceBinary ToolSource = "binary"
	// ToolSourceContainer runs the tool from an image, through whichever engine
	// is configured. It used to be ToolSourceDocker = "docker", which baked the
	// engine's name into a type the UI reads (§3.67). The rename is internal
	// only: this value is never serialised, and the configuration already spells
	// the same choice engine-neutrally (config.ToolSourceImage = "image").
	ToolSourceContainer ToolSource = "container"
)

// Default images for the container-sourced tools
const (
	DefaultTrivyImage    = "aquasec/trivy"
	DefaultGitleaksImage = "zricethezav/gitleaks"
	DefaultPlumberImage  = "getplumber/plumber"
)

// imageIsLocal verifies that the engine already holds an image.
func imageIsLocal(image string) bool {
	cmd := exec.Command(engine.Current().Binary, "images", "-q", image) //nolint:gosec // the binary is a declared engine name or a configured path
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	// If output is not empty, image exists
	return len(output) > 0
}

// Scanner performs security scans using multiple tools
type Scanner struct {
	options ScanOptions
	deps    Report
}

// NewScanner creates a new scanner with the given options, on the detection
// they carry, or on a fresh one when they carry none.
func NewScanner(opts ScanOptions) *Scanner {
	if opts.Detected != nil {
		return newScannerWithDeps(opts, *opts.Detected)
	}
	return newScannerWithDeps(opts, Detect(opts.Tools))
}

// newScannerWithDeps builds a scanner against a known set of tools. Detection
// probes the machine it runs on, so tests state the availability they mean
// instead of inheriting the developer's installation.
func newScannerWithDeps(opts ScanOptions, deps Report) *Scanner {
	return &Scanner{options: opts, deps: deps}
}

// spec is how one tool is handed to its builder: where detection found it,
// plus what the context adds — its extra arguments, and Trivy's rules file.
func (s *Scanner) spec(id ToolID) ToolSpec {
	spec := s.deps.Spec(id)
	set := s.options.Tools.Tool(string(id))
	spec.Args = set.Args
	if id == ToolTrivy {
		spec.Config = set.Config
	}
	return spec
}

// runs reports whether one category's tool runs on this target: the settings
// ask for it, it applies to the target, and it is there.
func (s *Scanner) runs(cat CategoryID, tool ToolID, targetType TargetType) bool {
	return s.wants(cat, tool, targetType) && s.deps.Available(tool)
}

// wants is runs without the availability: what the settings ask of this target.
func (s *Scanner) wants(cat CategoryID, tool ToolID, targetType TargetType) bool {
	if !Uses(s.options.Categories, cat, tool) {
		return false
	}
	for _, c := range categoryTable {
		if c.ID != cat {
			continue
		}
		for _, ct := range c.Tools {
			if ct.Tool == tool {
				// What a Trivy server cannot do is not done, rather than
				// forced off in the configuration: the tick survives the
				// server being switched off again (§3.86).
				if tool == ToolTrivy && !ct.Server && s.options.TrivyServer != "" {
					return false
				}
				return ct.Applies(targetType)
			}
		}
	}
	return false
}

// missingToolErrors reports the tools the caller asked for that cannot run
// because they are neither installed nor available as an image — one error per
// tool, read off the tables, so every tool is reported the same way (plumber
// was not, until §3.86).
//
// A tool that does not apply to the target type is not missing anything:
// gitleaks scans a working tree, so a secret scan of an image skips it for a
// reason that has nothing to do with what the machine has installed.
//
// A renderer (helm, kustomize) is not reported here: without it the stage still
// runs, and names each chart or overlay it could not validate
// (Result.K8sUnrendered) — which says more than one line per scan would.
func (s *Scanner) missingToolErrors(targetType TargetType) []string {
	var errs []string
	for _, tool := range toolTable {
		if s.deps.Available(tool.ID) || !s.wantsAnywhere(tool.ID, targetType) {
			continue
		}
		errs = append(errs, fmt.Sprintf("%s: not available — %s. Install %s or pull %s",
			tool.Binary, tool.Lost, tool.Binary, s.deps.Status(tool.ID).Image))
	}
	return errs
}

// wantsAnywhere reports whether some category asks this target for the tool,
// leaving out the renderers, whose absence the stage reports itself.
func (s *Scanner) wantsAnywhere(tool ToolID, targetType TargetType) bool {
	for _, cat := range categoryTable {
		for _, ct := range cat.Tools {
			if ct.Tool == tool && ct.DependsOn == "" && s.wants(cat.ID, tool, targetType) {
				return true
			}
		}
	}
	return false
}

// ErrTrivyUnavailable is returned by ScanRemoteImage when Trivy cannot be run.
var ErrTrivyUnavailable = errors.New("trivy is not available")

// ScanRemoteImage scans an image read from its registry, without pulling it
// into the local engine: the vulnerability stage only, with the options this
// scanner was built with (server, ignore-unfixed, ignore-EOL).
//
// It exists to measure a base image bump. A candidate is not something the user
// owns, so it must not land in the engine's storage, and only the CVE counts of
// it matter — secrets and misconfigurations describe the user's own layers, not
// the base's.
func (s *Scanner) ScanRemoteImage(ctx context.Context, image string) (*Result, error) {
	if !s.deps.Available(ToolTrivy) {
		return nil, ErrTrivyUnavailable
	}
	if err := checkRulesFile("trivy", s.spec(ToolTrivy).Config); err != nil {
		return nil, err
	}
	result := &Result{
		Target:     image,
		TargetType: TargetImage,
		StartTime:  time.Now(),
		Findings:   []Finding{},
		Errors:     []string{},
	}
	tc, err := trivyRemoteImageArgs(image, s.spec(ToolTrivy), s.options.TrivyServer, s.options.Tools.Trivy.IgnoreUnfixed, s.options.Tools.Trivy.IgnoreEOL)
	if err != nil {
		return nil, err
	}
	log.Printf("Running: %s", tc.String())
	findings, err := runTrivy(ctx, tc, nil)
	if err != nil {
		return nil, err
	}
	result.Findings = findings
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.CountFindings()
	return result, nil
}

// Scan performs a security scan on the target, running all enabled stages in parallel.
func (s *Scanner) Scan(ctx context.Context, target string, targetType TargetType) (*Result, error) {
	result := &Result{
		Target:     target,
		TargetType: targetType,
		StartTime:  time.Now(),
		Findings:   []Finding{},
		Errors:     []string{},
	}

	notify := func(update ProgressUpdate) {
		if s.options.OnProgress != nil {
			s.options.OnProgress(update)
		}
	}

	// A stage whose tool is missing is not started at all, so it leaves neither a
	// finding nor an error behind — and a result with neither is indistinguishable
	// from a clean scan. Recording the reason up front is what stops "nothing
	// looked at this image" being reported as "this image is fine" (§1.3 D20).
	result.Errors = append(result.Errors, s.missingToolErrors(targetType)...)

	// mu guards writes to result (Findings, Errors) from concurrent goroutines
	var mu sync.Mutex

	eg, egCtx := errgroup.WithContext(ctx)

	// Vuln scan (Trivy)
	if s.runs(CategoryIDVuln, ToolTrivy, targetType) {
		eg.Go(func() error {
			notify(ProgressUpdate{Stage: "vuln", Label: "Vulnerabilities", Status: StageRunning})
			progressFn := func(detail string) {
				notify(ProgressUpdate{Stage: "vuln", Label: "Vulnerabilities", Status: StageRunning, Detail: detail})
			}
			cmd := GetTrivyCommand(target, targetType, false, s.spec(ToolTrivy), s.options.TrivyServer, s.options.Tools.Trivy.IgnoreUnfixed, s.options.Tools.Trivy.IgnoreEOL)
			log.Printf("Running: %s", cmd)
			findings, err := RunTrivy(egCtx, target, targetType, false, s.spec(ToolTrivy), s.options.TrivyServer, s.options.Tools.Trivy.IgnoreUnfixed, s.options.Tools.Trivy.IgnoreEOL, progressFn)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				recordStageError(result, "trivy vuln", err)
				notify(ProgressUpdate{Stage: "vuln", Label: "Vulnerabilities", Status: StageError, Detail: err.Error()})
			} else {
				result.Findings = append(result.Findings, findings...)
				notify(ProgressUpdate{Stage: "vuln", Label: "Vulnerabilities", Status: StageDone})
			}
			return nil
		})
	}

	// License scan (Trivy, directory only)
	if s.runs(CategoryIDLicense, ToolTrivy, targetType) {
		eg.Go(func() error {
			notify(ProgressUpdate{Stage: "license", Label: "Licenses", Status: StageRunning})
			progressFn := func(detail string) {
				notify(ProgressUpdate{Stage: "license", Label: "Licenses", Status: StageRunning, Detail: detail})
			}
			cmd := GetTrivyCommand(target, targetType, true, s.spec(ToolTrivy), s.options.TrivyServer, s.options.Tools.Trivy.IgnoreUnfixed, s.options.Tools.Trivy.IgnoreEOL)
			log.Printf("Running: %s", cmd)
			findings, err := RunTrivy(egCtx, target, targetType, true, s.spec(ToolTrivy), s.options.TrivyServer, s.options.Tools.Trivy.IgnoreUnfixed, s.options.Tools.Trivy.IgnoreEOL, progressFn)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				recordStageError(result, "trivy license", err)
				notify(ProgressUpdate{Stage: "license", Label: "Licenses", Status: StageError})
			} else {
				result.Findings = append(result.Findings, findings...)
				notify(ProgressUpdate{Stage: "license", Label: "Licenses", Status: StageDone})
			}
			return nil
		})
	}

	// CI configuration score (plumber, directories only). An image has no
	// pipeline, so there is nothing to grade and the tab is empty on one.
	//
	// The target decides, not the batch: ciOptions reads this repository's own
	// remote and refuses one that is not this context's forge.
	if ciOpts, ok := s.ciOptions(target, targetType); ok {
		opts := ciOpts
		eg.Go(func() error {
			notify(ProgressUpdate{Stage: "ci", Label: "CI score", Status: StageRunning})
			progressFn := func(detail string) {
				notify(ProgressUpdate{Stage: "ci", Label: "CI score", Status: StageRunning, Detail: detail})
			}
			log.Printf("Running: %s", GetPlumberCommand(target, s.spec(ToolPlumber), opts))
			report, err := RunPlumber(egCtx, target, s.spec(ToolPlumber), opts, progressFn)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				recordStageError(result, "plumber", err)
				notify(ProgressUpdate{Stage: "ci", Label: "CI score", Status: StageError, Detail: err.Error()})
				return nil
			}
			// CIScanned is written by a stage that succeeds, exactly as
			// SecretsScanned is — a withheld run counts as having looked, which
			// is why it is set here and Withheld is carried beside it rather
			// than instead of it.
			result.CIScanned = true
			result.CIScore = report.Score
			result.CIPoints = report.Points
			result.CIWithheld = report.Withheld
			result.CIReasons = report.Reasons
			result.CIMissing = report.CIMissing
			result.Findings = append(result.Findings, report.Findings...)
			notify(ProgressUpdate{Stage: "ci", Label: "CI score", Status: StageDone})
			return nil
		})
	}

	// Kubernetes schema validation (kubeconform, directories only). An image
	// holds no manifests anyone applies. It belongs to Misconfiguration, beside
	// Trivy's security rules (§3.86).
	if s.runs(CategoryIDMisconfig, ToolKubeconform, targetType) {
		eg.Go(func() error {
			s.runKubeconformStage(egCtx, target, result, &mu, notify)
			return nil
		})
	}

	// Misconfig scan (Trivy)
	if s.runs(CategoryIDMisconfig, ToolTrivy, targetType) {
		eg.Go(func() error {
			notify(ProgressUpdate{Stage: "misconfig", Label: "Misconfigurations", Status: StageRunning})
			progressFn := func(detail string) {
				notify(ProgressUpdate{Stage: "misconfig", Label: "Misconfigurations", Status: StageRunning, Detail: detail})
			}
			findings, err := RunTrivyMisconfig(egCtx, target, targetType, s.spec(ToolTrivy), s.options.TrivyServer, s.options.Tools.Trivy.IgnoreEOL, progressFn)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				recordStageError(result, "trivy misconfig", err)
				notify(ProgressUpdate{Stage: "misconfig", Label: "Misconfigurations", Status: StageError})
			} else {
				result.Findings = append(result.Findings, findings...)
				// A stage that succeeds is what makes the verdict known, as in
				// the two secret stages below: one that fails says nothing, and
				// certainly not "clean".
				result.MisconfigScanned = true
				notify(ProgressUpdate{Stage: "misconfig", Label: "Misconfigurations", Status: StageDone})
			}
			return nil
		})
	}

	// Build context (DevDesk itself, directories only): what a COPY of the
	// whole context takes into the image (§3.81). No tool to detect, so it
	// runs whenever the category is on.
	if s.checksBuildContext(targetType) {
		eg.Go(func() error {
			runBuildContextStage(target, result, &mu, notify)
			return nil
		})
	}

	// Secret scan (Gitleaks, directory only)
	if s.runs(CategoryIDSecret, ToolGitleaks, targetType) {
		eg.Go(func() error {
			notify(ProgressUpdate{Stage: "secret", Label: "Secrets (Gitleaks)", Status: StageRunning})
			progressFn := func(detail string) {
				notify(ProgressUpdate{Stage: "secret", Label: "Secrets (Gitleaks)", Status: StageRunning, Detail: detail})
			}
			cmd := GetGitleaksCommand(target, s.spec(ToolGitleaks), s.options.Tools.Gitleaks.History, s.options.Tools.Gitleaks.Config)
			log.Printf("Running: %s", cmd)
			findings, err := RunGitleaks(egCtx, target, s.spec(ToolGitleaks), s.options.Tools.Gitleaks.History, s.options.Tools.Gitleaks.Config, progressFn)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				recordStageError(result, "gitleaks", err)
				notify(ProgressUpdate{Stage: "secret", Label: "Secrets (Gitleaks)", Status: StageError})
			} else {
				result.Findings = append(result.Findings, findings...)
				// A stage that succeeds is what makes the verdict known;
				// one that fails says nothing, and certainly not "clean".
				result.SecretsScanned = true
				notify(ProgressUpdate{Stage: "secret", Label: "Secrets (Gitleaks)", Status: StageDone})
			}
			return nil
		})
	}

	// Secret scan (Trivy). Both target types, unlike Gitleaks: this is what
	// gives an image scan a secret stage at all (see the category table).
	if s.runs(CategoryIDSecret, ToolTrivy, targetType) {
		eg.Go(func() error {
			notify(ProgressUpdate{Stage: "trivy-secret", Label: "Secrets (Trivy)", Status: StageRunning})
			progressFn := func(detail string) {
				notify(ProgressUpdate{Stage: "trivy-secret", Label: "Secrets (Trivy)", Status: StageRunning, Detail: detail})
			}
			cmd := GetTrivySecretCommand(target, targetType, s.spec(ToolTrivy), s.options.TrivyServer)
			log.Printf("Running: %s", cmd)
			findings, err := RunTrivySecret(egCtx, target, targetType, s.spec(ToolTrivy), s.options.TrivyServer, progressFn)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				recordStageError(result, "trivy secret", err)
				notify(ProgressUpdate{Stage: "trivy-secret", Label: "Secrets (Trivy)", Status: StageError})
			} else {
				result.Findings = append(result.Findings, findings...)
				result.SecretsScanned = true
				notify(ProgressUpdate{Stage: "trivy-secret", Label: "Secrets (Trivy)", Status: StageDone})
			}
			return nil
		})
	}

	// All goroutines return nil, so eg.Wait() never returns an error
	_ = eg.Wait()

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.CountFindings()

	return result, nil
}
