package scan

import (
	"context"
	"fmt"
	"log"
	"os/exec"
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
	Stage  string      // unique identifier: "vuln", "secret", "license", "misconfig", "sbom"
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
	GenerateSBOM    bool   // SBOM generation (Trivy, CycloneDX format)
	SBOMOutputDir   string // Output directory for SBOM files (optional)
	TrivyImage      string // Custom Docker image for Trivy (optional)
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
	Target         string         `json:"target"`
	TargetType     TargetType     `json:"target_type"`
	StartTime      time.Time      `json:"start_time"`
	EndTime        time.Time      `json:"end_time"`
	Duration       time.Duration  `json:"duration"`
	Counts         SeverityCounts `json:"counts"`          // CVE counts by severity
	SecretCount    int            `json:"secret_count"`    // Total secrets found (gitleaks)
	LicenseCount   int            `json:"license_count"`   // Total license issues found
	MisconfigCount int            `json:"misconfig_count"` // Total misconfigurations found
	Findings       []Finding      `json:"findings"`
	Errors         []string       `json:"errors,omitempty"`
	SBOMPath       string         `json:"sbom_path,omitempty"` // Path to generated SBOM file
}

// CountFindings aggregates findings into separate counters by source and severity.
// CVEs (source "trivy") are counted by severity into Counts.
// Secrets (source "gitleaks"), licenses ("trivy-license"), and misconfigs ("trivy-misconfig")
// are counted into their respective dedicated fields.
func (r *Result) CountFindings() {
	r.Counts = SeverityCounts{}
	r.SecretCount = 0
	r.LicenseCount = 0
	r.MisconfigCount = 0
	for _, f := range r.Findings {
		switch f.Source {
		case "gitleaks":
			r.SecretCount++
		case "trivy-license":
			r.LicenseCount++
		case "trivy-misconfig":
			r.MisconfigCount++
		default: // "trivy" and any unknown source → CVE counts by severity
			switch f.Severity {
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
	}
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
	TrivyImage        string // Docker image used for Trivy
	GitleaksAvailable bool
	GitleaksSource    ToolSource
	GitleaksVersion   string
	GitleaksImage     string // Docker image used for Gitleaks
	DockerAvailable   bool
}

// Default Docker images
const (
	DefaultTrivyImage    = "aquasec/trivy"
	DefaultGitleaksImage = "zricethezav/gitleaks"
)

// CheckDependenciesWithImages verifies tools with custom Docker images
func CheckDependenciesWithImages(trivyImage, gitleaksImage string) DependencyStatus {
	// Use defaults if not specified
	if trivyImage == "" {
		trivyImage = DefaultTrivyImage
	}
	if gitleaksImage == "" {
		gitleaksImage = DefaultGitleaksImage
	}

	status := DependencyStatus{
		TrivySource:    ToolSourceNone,
		TrivyImage:     trivyImage,
		GitleaksSource: ToolSourceNone,
		GitleaksImage:  gitleaksImage,
	}

	// Check if Docker is available
	if path, err := exec.LookPath("docker"); err == nil && path != "" {
		status.DockerAvailable = true
	}

	// Check Trivy binary first
	if path, err := exec.LookPath("trivy"); err == nil && path != "" {
		status.TrivyAvailable = true
		status.TrivySource = ToolSourceBinary
		if out, err := exec.Command("trivy", "--version").Output(); err == nil {
			status.TrivyVersion = string(out)
		}
	} else if status.DockerAvailable {
		// Check for Trivy Docker image
		if checkDockerImage(trivyImage) {
			status.TrivyAvailable = true
			status.TrivySource = ToolSourceDocker
			// Get version from Docker image
			if out, err := exec.Command("docker", "run", "--rm", trivyImage, "--version").Output(); err == nil {
				status.TrivyVersion = "docker:" + string(out)
			} else {
				status.TrivyVersion = "docker"
			}
		}
	}

	// Check Gitleaks binary first
	if path, err := exec.LookPath("gitleaks"); err == nil && path != "" {
		status.GitleaksAvailable = true
		status.GitleaksSource = ToolSourceBinary
		if out, err := exec.Command("gitleaks", "version").Output(); err == nil {
			status.GitleaksVersion = string(out)
		}
	} else if status.DockerAvailable {
		// Check for Gitleaks Docker image
		if checkDockerImage(gitleaksImage) {
			status.GitleaksAvailable = true
			status.GitleaksSource = ToolSourceDocker
			// Get version from Docker image
			if out, err := exec.Command("docker", "run", "--rm", gitleaksImage, "version").Output(); err == nil {
				status.GitleaksVersion = "docker:" + string(out)
			} else {
				status.GitleaksVersion = "docker"
			}
		}
	}

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
	return newScannerWithDeps(opts, CheckDependenciesWithImages(opts.TrivyImage, opts.GitleaksImage))
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
	wantsTrivy := s.options.EnableVuln || s.options.EnableMisconfig || s.options.GenerateSBOM ||
		(s.options.EnableLicense && targetType == TargetDirectory)
	wantsGitleaks := s.options.EnableSecret && targetType == TargetDirectory

	var errs []string
	if wantsTrivy && !s.deps.TrivyAvailable {
		errs = append(errs, fmt.Sprintf(
			"trivy: not available — nothing was scanned. Install trivy or pull %s", trivyImage(s.deps.TrivyImage)))
	}
	if wantsGitleaks && !s.deps.GitleaksAvailable {
		errs = append(errs, fmt.Sprintf(
			"gitleaks: not available — no secret scan was run. Install gitleaks or pull %s",
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

	// mu guards writes to result (Findings, Errors, SBOMPath) from concurrent goroutines
	var mu sync.Mutex

	eg, egCtx := errgroup.WithContext(ctx)

	// Vuln scan (Trivy)
	if s.options.EnableVuln && s.deps.TrivyAvailable {
		eg.Go(func() error {
			notify(ProgressUpdate{Stage: "vuln", Label: "Vulnerabilities", Status: StageRunning})
			progressFn := func(detail string) {
				notify(ProgressUpdate{Stage: "vuln", Label: "Vulnerabilities", Status: StageRunning, Detail: detail})
			}
			cmd := GetTrivyCommand(target, targetType, false, s.deps.TrivySource, s.deps.TrivyImage, s.options.TrivyServer, s.options.IgnoreUnfixed, s.options.IgnoreEOL)
			log.Printf("Running: %s", cmd)
			findings, err := RunTrivy(egCtx, target, targetType, false, s.deps.TrivySource, s.deps.TrivyImage, s.options.TrivyServer, s.options.IgnoreUnfixed, s.options.IgnoreEOL, progressFn)
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
			cmd := GetTrivyCommand(target, targetType, true, s.deps.TrivySource, s.deps.TrivyImage, s.options.TrivyServer, s.options.IgnoreUnfixed, s.options.IgnoreEOL)
			log.Printf("Running: %s", cmd)
			findings, err := RunTrivy(egCtx, target, targetType, true, s.deps.TrivySource, s.deps.TrivyImage, s.options.TrivyServer, s.options.IgnoreUnfixed, s.options.IgnoreEOL, progressFn)
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
			findings, err := RunTrivyMisconfig(egCtx, target, targetType, s.deps.TrivySource, s.deps.TrivyImage, s.options.TrivyServer, s.options.IgnoreEOL, progressFn)
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
			notify(ProgressUpdate{Stage: "secret", Label: "Secrets", Status: StageRunning})
			progressFn := func(detail string) {
				notify(ProgressUpdate{Stage: "secret", Label: "Secrets", Status: StageRunning, Detail: detail})
			}
			cmd := GetGitleaksCommand(target, s.deps.GitleaksSource, s.deps.GitleaksImage, s.options.GitleaksHistory, s.options.GitleaksConfig)
			log.Printf("Running: %s", cmd)
			findings, err := RunGitleaks(egCtx, target, s.deps.GitleaksSource, s.deps.GitleaksImage, s.options.GitleaksHistory, s.options.GitleaksConfig, progressFn)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("gitleaks: %v", err))
				notify(ProgressUpdate{Stage: "secret", Label: "Secrets", Status: StageError})
			} else {
				result.Findings = append(result.Findings, findings...)
				notify(ProgressUpdate{Stage: "secret", Label: "Secrets", Status: StageDone})
			}
			return nil
		})
	}

	// SBOM generation (Trivy)
	if s.options.GenerateSBOM && s.deps.TrivyAvailable {
		eg.Go(func() error {
			notify(ProgressUpdate{Stage: "sbom", Label: "SBOM", Status: StageRunning})
			sbomPath, err := GenerateSBOM(egCtx, target, targetType, s.deps.TrivySource, s.deps.TrivyImage, s.options.TrivyServer, s.options.SBOMOutputDir)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("sbom: %v", err))
				notify(ProgressUpdate{Stage: "sbom", Label: "SBOM", Status: StageError})
			} else {
				result.SBOMPath = sbomPath
				notify(ProgressUpdate{Stage: "sbom", Label: "SBOM", Status: StageDone})
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
