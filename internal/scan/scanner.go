package scan

import (
	"context"
	"fmt"
	"log"
	"os/exec"

	"github.com/anthnel/devdesk/internal/config"
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
	Stage  string      // unique identifier: "vuln", "secret", "trivy-secret", "license", "misconfig"
	Label  string      // human-readable label
	Status StageStatus // current status of the stage
	Detail string      // optional detail (e.g., DB download progress from Trivy stderr)
}

// ScanOptions configures which scanners to run
type ScanOptions struct {
	EnableVuln      bool   // Vulnerability scanning (Trivy)
	EnableSecret    bool   // Secret scanning (Gitleaks)
	EnableLicense   bool   // License scanning (Trivy)
	EnableMisconfig bool   // Misconfiguration scanning (Trivy)
	TrivySource     string // Where Trivy runs from: auto | binary | image
	TrivyPath       string // Custom Trivy executable (optional)
	TrivyImage      string // Custom Docker image for Trivy (optional)
	GitleaksSource  string // Where Gitleaks runs from: auto | binary | image
	GitleaksPath    string // Custom Gitleaks executable (optional)
	GitleaksImage   string // Custom Docker image for Gitleaks (optional)
	TrivyServer     string // Trivy server URL for client-server mode (optional)
	IgnoreUnfixed   bool   // Trivy: only show vulnerabilities with fixes
	IgnoreEOL       bool   // Trivy: ignore end-of-life package vulnerabilities (--ignore-status end_of_life)
	GitleaksHistory bool   // Gitleaks: scan git history (omit --no-git)
	GitleaksConfig  string // Gitleaks: custom config file path
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
	// Une étape secrets peut ne pas avoir tourné pour trois raisons — option
	// coupée, outil absent, étape en erreur — et dans les trois cas un compteur
	// à zéro dirait « cette cible est propre » d'un scan qui n'a rien regardé.
	// C'est le même principe que missingToolErrors ci-dessous (§1.3 D20), porté
	// jusqu'à ce que les vues affichent.
	SecretsScanned bool      `json:"secrets_scanned"`
	LicenseCount   int       `json:"license_count"`   // Total license issues found
	MisconfigCount int       `json:"misconfig_count"` // Total misconfigurations found
	Findings       []Finding `json:"findings"`
	Errors         []string  `json:"errors,omitempty"`
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
	for _, f := range r.Findings {
		switch Categorize(f) {
		case CategorySecret:
			r.SecretCount++
		case CategoryLicense:
			r.LicenseCount++
		case CategoryMisconfiguration:
			r.MisconfigCount++
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
// Elle est le seul calcul du verdict de l'application. Il y en avait deux, tous
// deux écrits « une finding dont Source vaut gitleaks », et tous deux devenus
// faux le jour où Trivy s'est mis à trouver des secrets lui aussi : un dépôt
// dont c'étaient les seuls se lisait propre. SecretCount vient de Categorize,
// qui est le seul classeur (§3.11).
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
		r.SecretCount + r.LicenseCount + r.MisconfigCount
}

// ToolSource indicates how a tool is available
type ToolSource string

const (
	ToolSourceNone   ToolSource = "none"
	ToolSourceBinary ToolSource = "binary"
	ToolSourceDocker ToolSource = "docker"
)

// DependencyStatus holds the availability status of external tools
type DependencyStatus struct {
	TrivyAvailable    bool
	TrivySource       ToolSource
	TrivyVersion      string
	TrivyBinary       string // Executable to run when TrivySource is binary
	TrivyImage        string // Docker image used for Trivy
	GitleaksAvailable bool
	GitleaksSource    ToolSource
	GitleaksVersion   string
	GitleaksBinary    string // Executable to run when GitleaksSource is binary
	GitleaksImage     string // Docker image used for Gitleaks
	PlumberAvailable  bool
	PlumberSource     ToolSource
	PlumberVersion    string
	PlumberBinary     string // Executable to run when PlumberSource is binary
	PlumberImage      string // Docker image used for plumber
	DockerAvailable   bool
}

// Default Docker images
const (
	DefaultTrivyImage    = "aquasec/trivy"
	DefaultGitleaksImage = "zricethezav/gitleaks"
	DefaultPlumberImage  = "getplumber/plumber"
)

// CheckDependencies works out where each scanner runs from, for one context's
// scan configuration.
//
// It replaces CheckDependenciesWithImages, which took only the two image names.
// That signature is why `trivy_path` and `gitleaks_path` were never read: there
// was nowhere to pass them (D27). Taking the whole ScanConfig also lets the
// per-tool source preference be honoured, which binary-first resolution made
// impossible to express.
func CheckDependencies(c config.ScanConfig) DependencyStatus {
	trivyImage := c.TrivyImage
	if trivyImage == "" {
		trivyImage = DefaultTrivyImage
	}
	gitleaksImage := c.GitleaksImage
	if gitleaksImage == "" {
		gitleaksImage = DefaultGitleaksImage
	}
	plumberImage := c.PlumberImage
	if plumberImage == "" {
		plumberImage = DefaultPlumberImage
	}

	status := DependencyStatus{
		TrivySource:    ToolSourceNone,
		TrivyImage:     trivyImage,
		GitleaksSource: ToolSourceNone,
		GitleaksImage:  gitleaksImage,
		PlumberSource:  ToolSourceNone,
		PlumberImage:   plumberImage,
	}

	if path, err := exec.LookPath("docker"); err == nil && path != "" {
		status.DockerAvailable = true
	}

	trivy := resolveTool(c.TrivySource, c.TrivyPath, "trivy", trivyImage, status.DockerAvailable, "--version")
	status.TrivyAvailable = trivy.Available
	status.TrivySource = trivy.Source
	status.TrivyBinary = trivy.Binary
	status.TrivyVersion = trivy.Version

	gitleaks := resolveTool(c.GitleaksSource, c.GitleaksPath, "gitleaks", gitleaksImage, status.DockerAvailable, "version")
	status.GitleaksAvailable = gitleaks.Available
	status.GitleaksSource = gitleaks.Source
	status.GitleaksBinary = gitleaks.Binary
	status.GitleaksVersion = gitleaks.Version

	// `plumber version` writes the installed version to stdout and an upgrade
	// notice — "plumber v0.4.44 is available (you have 0.4.40)" — to stderr.
	// toolVersion reads stdout only, so the two cannot be confused; measured
	// rather than assumed, because reporting the available version as the
	// installed one is the kind of thing nobody notices for months.
	plumber := resolveTool(c.PlumberSource, c.PlumberPath, "plumber", plumberImage, status.DockerAvailable, "version")
	status.PlumberAvailable = plumber.Available
	status.PlumberSource = plumber.Source
	status.PlumberBinary = plumber.Binary
	status.PlumberVersion = plumber.Version

	return status
}

// checkDockerImage verifies if a Docker image exists locally
func checkDockerImage(image string) bool {
	cmd := exec.Command("docker", "images", "-q", image)
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
	deps    DependencyStatus
}

// NewScanner creates a new scanner with the given options, detecting which
// tools are available on this machine.
func NewScanner(opts ScanOptions) *Scanner {
	// Spelled out rather than passed as a config: ScanOptions is what a scan was
	// asked to do, and these six fields are the part of it detection needs.
	return newScannerWithDeps(opts, CheckDependencies(config.ScanConfig{
		TrivySource:    opts.TrivySource,
		TrivyPath:      opts.TrivyPath,
		TrivyImage:     opts.TrivyImage,
		GitleaksSource: opts.GitleaksSource,
		GitleaksPath:   opts.GitleaksPath,
		GitleaksImage:  opts.GitleaksImage,
	}))
}

// newScannerWithDeps builds a scanner against a known set of tools. Detection
// probes the machine it runs on, so tests state the availability they mean
// instead of inheriting the developer's installation.
func newScannerWithDeps(opts ScanOptions, deps DependencyStatus) *Scanner {
	return &Scanner{options: opts, deps: deps}
}

// missingToolErrors reports the stages the caller asked for that cannot run
// because their tool is neither installed nor available as an image.
//
// A stage that does not apply to the target type is not missing anything:
// gitleaks scans a working tree, so a secret scan of an image is skipped for a
// reason that has nothing to do with what the machine has installed. Only the
// stages that would otherwise have run are reported.
func (s *Scanner) missingToolErrors(targetType TargetType) []string {
	wantsTrivy := s.options.EnableVuln || s.options.EnableMisconfig ||
		s.options.EnableSecret || (s.options.EnableLicense && targetType == TargetDirectory)
	wantsGitleaks := s.options.EnableSecret && targetType == TargetDirectory

	var errs []string
	if wantsTrivy && !s.deps.TrivyAvailable {
		errs = append(errs, fmt.Sprintf(
			"trivy: not available — nothing was scanned. Install trivy or pull %s", trivyImage(s.deps.TrivyImage)))
	}
	if wantsGitleaks && !s.deps.GitleaksAvailable {
		// Not "no secret scan was run": Trivy runs one too now, so naming what
		// Gitleaks alone contributes is what keeps this accurate when only one
		// of the two is missing.
		errs = append(errs, fmt.Sprintf(
			"gitleaks: not available — git history was not scanned for secrets. Install gitleaks or pull %s",
			gitleaksImage(s.deps.GitleaksImage)))
	}
	return errs
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
	if s.options.EnableVuln && s.deps.TrivyAvailable {
		eg.Go(func() error {
			notify(ProgressUpdate{Stage: "vuln", Label: "Vulnerabilities", Status: StageRunning})
			progressFn := func(detail string) {
				notify(ProgressUpdate{Stage: "vuln", Label: "Vulnerabilities", Status: StageRunning, Detail: detail})
			}
			cmd := GetTrivyCommand(target, targetType, false, s.deps.TrivySpec(), s.options.TrivyServer, s.options.IgnoreUnfixed, s.options.IgnoreEOL)
			log.Printf("Running: %s", cmd)
			findings, err := RunTrivy(egCtx, target, targetType, false, s.deps.TrivySpec(), s.options.TrivyServer, s.options.IgnoreUnfixed, s.options.IgnoreEOL, progressFn)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("trivy vuln: %v", err))
				notify(ProgressUpdate{Stage: "vuln", Label: "Vulnerabilities", Status: StageError, Detail: err.Error()})
			} else {
				result.Findings = append(result.Findings, findings...)
				notify(ProgressUpdate{Stage: "vuln", Label: "Vulnerabilities", Status: StageDone})
			}
			return nil
		})
	}

	// License scan (Trivy, directory only)
	if s.options.EnableLicense && s.deps.TrivyAvailable && targetType == TargetDirectory {
		eg.Go(func() error {
			notify(ProgressUpdate{Stage: "license", Label: "Licenses", Status: StageRunning})
			progressFn := func(detail string) {
				notify(ProgressUpdate{Stage: "license", Label: "Licenses", Status: StageRunning, Detail: detail})
			}
			cmd := GetTrivyCommand(target, targetType, true, s.deps.TrivySpec(), s.options.TrivyServer, s.options.IgnoreUnfixed, s.options.IgnoreEOL)
			log.Printf("Running: %s", cmd)
			findings, err := RunTrivy(egCtx, target, targetType, true, s.deps.TrivySpec(), s.options.TrivyServer, s.options.IgnoreUnfixed, s.options.IgnoreEOL, progressFn)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("trivy license: %v", err))
				notify(ProgressUpdate{Stage: "license", Label: "Licenses", Status: StageError})
			} else {
				result.Findings = append(result.Findings, findings...)
				notify(ProgressUpdate{Stage: "license", Label: "Licenses", Status: StageDone})
			}
			return nil
		})
	}

	// Misconfig scan (Trivy)
	if s.options.EnableMisconfig && s.deps.TrivyAvailable {
		eg.Go(func() error {
			notify(ProgressUpdate{Stage: "misconfig", Label: "Misconfigurations", Status: StageRunning})
			progressFn := func(detail string) {
				notify(ProgressUpdate{Stage: "misconfig", Label: "Misconfigurations", Status: StageRunning, Detail: detail})
			}
			findings, err := RunTrivyMisconfig(egCtx, target, targetType, s.deps.TrivySpec(), s.options.TrivyServer, s.options.IgnoreEOL, progressFn)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("trivy misconfig: %v", err))
				notify(ProgressUpdate{Stage: "misconfig", Label: "Misconfigurations", Status: StageError})
			} else {
				result.Findings = append(result.Findings, findings...)
				notify(ProgressUpdate{Stage: "misconfig", Label: "Misconfigurations", Status: StageDone})
			}
			return nil
		})
	}

	// Secret scan (Gitleaks, directory only)
	if s.options.EnableSecret && s.deps.GitleaksAvailable && targetType == TargetDirectory {
		eg.Go(func() error {
			notify(ProgressUpdate{Stage: "secret", Label: "Secrets (Gitleaks)", Status: StageRunning})
			progressFn := func(detail string) {
				notify(ProgressUpdate{Stage: "secret", Label: "Secrets (Gitleaks)", Status: StageRunning, Detail: detail})
			}
			cmd := GetGitleaksCommand(target, s.deps.GitleaksSpec(), s.options.GitleaksHistory, s.options.GitleaksConfig)
			log.Printf("Running: %s", cmd)
			findings, err := RunGitleaks(egCtx, target, s.deps.GitleaksSpec(), s.options.GitleaksHistory, s.options.GitleaksConfig, progressFn)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("gitleaks: %v", err))
				notify(ProgressUpdate{Stage: "secret", Label: "Secrets (Gitleaks)", Status: StageError})
			} else {
				result.Findings = append(result.Findings, findings...)
				// Une étape qui aboutit est ce qui rend le verdict connu ;
				// celle qui échoue ne dit rien, et surtout pas « propre ».
				result.SecretsScanned = true
				notify(ProgressUpdate{Stage: "secret", Label: "Secrets (Gitleaks)", Status: StageDone})
			}
			return nil
		})
	}

	// Secret scan (Trivy). Both target types, unlike Gitleaks: this is what
	// gives an image scan a secret stage at all. The two are complementary
	// rather than redundant — Gitleaks reads git history, Trivy reads image
	// layers — so both run when the option is on and both tools are there.
	if s.options.EnableSecret && s.deps.TrivyAvailable {
		eg.Go(func() error {
			notify(ProgressUpdate{Stage: "trivy-secret", Label: "Secrets (Trivy)", Status: StageRunning})
			progressFn := func(detail string) {
				notify(ProgressUpdate{Stage: "trivy-secret", Label: "Secrets (Trivy)", Status: StageRunning, Detail: detail})
			}
			cmd := GetTrivySecretCommand(target, targetType, s.deps.TrivySpec(), s.options.TrivyServer)
			log.Printf("Running: %s", cmd)
			findings, err := RunTrivySecret(egCtx, target, targetType, s.deps.TrivySpec(), s.options.TrivyServer, progressFn)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("trivy secret: %v", err))
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
