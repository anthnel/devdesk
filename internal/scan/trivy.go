package scan

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// TrivyReport represents the JSON output from Trivy
type TrivyReport struct {
	Results []TrivyResult `json:"Results"`
}

// TrivyResult represents a single result from Trivy
type TrivyResult struct {
	Target            string                  `json:"Target"`
	Class             string                  `json:"Class"`
	Type              string                  `json:"Type"`
	Vulnerabilities   []TrivyVulnerability    `json:"Vulnerabilities"`
	Secrets           []TrivySecret           `json:"Secrets,omitempty"`
	Licenses          []TrivyLicense          `json:"Licenses,omitempty"`
	Misconfigurations []TrivyMisconfiguration `json:"Misconfigurations,omitempty"`
}

// TrivyVulnerability represents a vulnerability found by Trivy
type TrivyVulnerability struct {
	VulnerabilityID  string   `json:"VulnerabilityID"`
	PkgName          string   `json:"PkgName"`
	InstalledVersion string   `json:"InstalledVersion"`
	FixedVersion     string   `json:"FixedVersion"`
	Severity         string   `json:"Severity"`
	Title            string   `json:"Title"`
	Description      string   `json:"Description"`
	PrimaryURL       string   `json:"PrimaryURL,omitempty"`
	References       []string `json:"References,omitempty"`
}

// TrivySecret represents a secret found by Trivy
type TrivySecret struct {
	RuleID    string `json:"RuleID"`
	Category  string `json:"Category"`
	Severity  string `json:"Severity"`
	Title     string `json:"Title"`
	StartLine int    `json:"StartLine"`
	EndLine   int    `json:"EndLine"`
	Match     string `json:"Match"`
}

// TrivyLicense represents a license found by Trivy
type TrivyLicense struct {
	Severity   string  `json:"Severity"`
	Category   string  `json:"Category"` // forbidden, restricted, reciprocal, notice, permissive, unencumbered, unknown
	PkgName    string  `json:"PkgName"`
	FilePath   string  `json:"FilePath"`
	Name       string  `json:"Name"` // License identifier (SPDX): MIT, Apache-2.0, GPL-3.0, etc.
	Confidence float64 `json:"Confidence"`
	Link       string  `json:"Link"`
}

// TrivyMisconfiguration represents a misconfiguration found by Trivy
type TrivyMisconfiguration struct {
	Type          string `json:"Type"`
	ID            string `json:"ID"`
	AVDID         string `json:"AVDID"`
	Title         string `json:"Title"`
	Desc          string `json:"Description"`
	Message       string `json:"Message"`
	Severity      string `json:"Severity"`
	Resolution    string `json:"Resolution"`
	PrimaryURL    string `json:"PrimaryURL"`
	Status        string `json:"Status"`
	CauseMetadata struct {
		StartLine int `json:"StartLine"`
		EndLine   int `json:"EndLine"`
	} `json:"CauseMetadata"`
}

// RunTrivy executes Trivy and returns findings.
// progressFn is an optional callback called with each stderr line (DB download progress etc.).
func RunTrivy(ctx context.Context, target string, targetType TargetType, licenseMode bool, source ToolSource, image string, server string, ignoreUnfixed bool, ignoreEOL bool, progressFn func(string)) ([]Finding, error) {
	// Use default image if not specified
	if image == "" {
		image = DefaultTrivyImage
	}

	var args []string

	// Build trivy arguments based on target type
	switch targetType {
	case TargetDirectory:
		args = []string{"fs", "--format", "json"}
		if licenseMode {
			args = append(args, "--scanners", "license")
		} else {
			args = append(args, "--scanners", "vuln")
		}

	case TargetImage:
		args = []string{"image", "--format", "json"}

	default:
		return nil, fmt.Errorf("unsupported target type: %s", targetType)
	}

	// Add server flag for client-server mode
	if server != "" {
		args = append(args, "--server", server)
	}

	// Add ignore-unfixed flag
	if ignoreUnfixed {
		args = append(args, "--ignore-unfixed")
	}

	// Add ignore-status end_of_life flag
	if ignoreEOL {
		args = append(args, "--ignore-status", "end_of_life")
	}

	var cmd *exec.Cmd

	if source == ToolSourceDocker {
		// Use Docker to run Trivy
		dockerArgs := []string{
			"run", "--rm",
		}

		// For directory scans, mount the target directory
		if targetType == TargetDirectory {
			dockerArgs = append(dockerArgs,
				"-v", target+":/scan:ro",
			)
			args = append(args, "/scan")
		} else {
			// For image scans with server mode, no need for docker socket
			if server == "" {
				dockerArgs = append(dockerArgs,
					"-v", "/var/run/docker.sock:/var/run/docker.sock:ro",
				)
			}
			args = append(args, target)
		}

		dockerArgs = append(dockerArgs, image)
		dockerArgs = append(dockerArgs, args...)
		cmd = exec.CommandContext(ctx, "docker", dockerArgs...)
	} else {
		// Use binary directly
		args = append(args, target)
		cmd = exec.CommandContext(ctx, "trivy", args...)
	}

	return runTrivyCmd(cmd, progressFn)
}

// runTrivyCmd executes a pre-built Trivy command, optionally forwarding stderr lines to progressFn.
func runTrivyCmd(cmd *exec.Cmd, progressFn func(string)) ([]Finding, error) {
	var stdoutBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf

	if progressFn != nil {
		stderrPipe, err := cmd.StderrPipe()
		if err == nil {
			go func() {
				sc := bufio.NewScanner(stderrPipe)
				for sc.Scan() {
					if line := strings.TrimSpace(sc.Text()); line != "" {
						progressFn(line)
					}
				}
			}()
		}
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("trivy failed to start: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		if stdoutBuf.Len() == 0 {
			return nil, fmt.Errorf("trivy failed: %w", err)
		}
		// Non-zero exit with JSON output: Trivy returns exit code 1 when vulnerabilities are found
	}

	return parseTrivyOutput(stdoutBuf.Bytes())
}

// parseTrivyOutput parses Trivy JSON output into findings
func parseTrivyOutput(data []byte) ([]Finding, error) {
	var report TrivyReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("failed to parse trivy output: %w", err)
	}

	var findings []Finding

	for _, result := range report.Results {
		// Process vulnerabilities
		for _, vuln := range result.Vulnerabilities {
			// Build references from PrimaryURL and advisory links
			refs := make([]string, 0, len(vuln.References)+1)
			if vuln.PrimaryURL != "" {
				refs = append(refs, vuln.PrimaryURL)
			}
			for _, r := range vuln.References {
				if r != vuln.PrimaryURL {
					refs = append(refs, r)
				}
			}

			// Build a fix command suggestion when a fixed version is known
			fixCmd := ""
			if vuln.FixedVersion != "" {
				fixCmd = fmt.Sprintf("Update %s to %s", vuln.PkgName, vuln.FixedVersion)
			}

			findings = append(findings, Finding{
				ID:          vuln.VulnerabilityID,
				Title:       vuln.Title,
				Description: vuln.Description,
				Severity:    parseSeverity(vuln.Severity),
				Source:      "trivy",
				File:        result.Target,
				PkgName:     vuln.PkgName,
				Version:     vuln.InstalledVersion,
				FixedIn:     vuln.FixedVersion,
				References:  refs,
				FixCommand:  fixCmd,
			})
		}

		// Process licenses found by Trivy
		for _, license := range result.Licenses {
			findings = append(findings, Finding{
				ID:          license.Name,
				Title:       fmt.Sprintf("%s (%s)", license.PkgName, license.Name),
				Description: fmt.Sprintf("License category: %s", license.Category),
				Severity:    parseSeverity(license.Severity),
				Source:      "trivy-license",
				File:        license.FilePath,
				PkgName:     license.PkgName,
			})
		}

		// Process misconfigurations found by Trivy
		for _, misconf := range result.Misconfigurations {
			var miscRefs []string
			if misconf.PrimaryURL != "" {
				miscRefs = append(miscRefs, misconf.PrimaryURL)
			}
			findings = append(findings, Finding{
				ID:          misconf.AVDID,
				Title:       misconf.Title,
				Description: misconf.Desc,
				Severity:    parseSeverity(misconf.Severity),
				Source:      "trivy-misconfig",
				File:        result.Target,
				Line:        misconf.CauseMetadata.StartLine,
				Resolution:  misconf.Resolution,
				References:  miscRefs,
			})
		}
	}

	return findings, nil
}

// parseSeverity converts string severity to SeverityLevel
func parseSeverity(s string) SeverityLevel {
	switch strings.ToUpper(s) {
	case "CRITICAL":
		return SeverityCritical
	case "HIGH":
		return SeverityHigh
	case "MEDIUM":
		return SeverityMedium
	case "LOW":
		return SeverityLow
	default:
		return SeverityUnknown
	}
}

// maskSecret partially masks secret values for display
func maskSecret(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}

// GetTrivyCommand returns the command that would be executed (for display/logging purposes)
func GetTrivyCommand(target string, targetType TargetType, licenseMode bool, source ToolSource, image string, server string, ignoreUnfixed bool) string {
	// Use default image if not specified
	if image == "" {
		image = DefaultTrivyImage
	}

	var args []string

	// Build trivy arguments based on target type
	switch targetType {
	case TargetDirectory:
		args = []string{"fs", "--format", "json"}
		if licenseMode {
			args = append(args, "--scanners", "license")
		} else {
			args = append(args, "--scanners", "vuln")
		}
	case TargetImage:
		args = []string{"image", "--format", "json"}
	default:
		return ""
	}

	if server != "" {
		args = append(args, "--server", server)
	}
	if ignoreUnfixed {
		args = append(args, "--ignore-unfixed")
	}

	if source == ToolSourceDocker {
		dockerArgs := []string{"docker", "run", "--rm"}

		if targetType == TargetDirectory {
			dockerArgs = append(dockerArgs, "-v", target+":/scan:ro")
			args = append(args, "/scan")
		} else {
			if server == "" {
				dockerArgs = append(dockerArgs, "-v", "/var/run/docker.sock:/var/run/docker.sock:ro")
			}
			args = append(args, target)
		}

		dockerArgs = append(dockerArgs, image)
		dockerArgs = append(dockerArgs, args...)
		return strings.Join(dockerArgs, " ")
	}

	// Binary mode
	fullArgs := []string{"trivy"}
	fullArgs = append(fullArgs, args...)
	fullArgs = append(fullArgs, target)
	return strings.Join(fullArgs, " ")
}

// RunTrivyMisconfig executes Trivy with --scanners misconfig and returns findings.
// progressFn is an optional callback called with each stderr line.
func RunTrivyMisconfig(ctx context.Context, target string, targetType TargetType, source ToolSource, image string, server string, ignoreEOL bool, progressFn func(string)) ([]Finding, error) {
	if image == "" {
		image = DefaultTrivyImage
	}

	var args []string

	switch targetType {
	case TargetDirectory:
		args = []string{"fs", "--format", "json", "--scanners", "misconfig"}
	case TargetImage:
		args = []string{"image", "--format", "json", "--scanners", "misconfig"}
	default:
		return nil, fmt.Errorf("unsupported target type: %s", targetType)
	}

	if server != "" {
		args = append(args, "--server", server)
	}

	if ignoreEOL {
		args = append(args, "--ignore-status", "end_of_life")
	}

	var cmd *exec.Cmd

	if source == ToolSourceDocker {
		dockerArgs := []string{"run", "--rm"}
		if targetType == TargetDirectory {
			dockerArgs = append(dockerArgs, "-v", target+":/scan:ro")
			args = append(args, "/scan")
		} else {
			if server == "" {
				dockerArgs = append(dockerArgs, "-v", "/var/run/docker.sock:/var/run/docker.sock:ro")
			}
			args = append(args, target)
		}
		dockerArgs = append(dockerArgs, image)
		dockerArgs = append(dockerArgs, args...)
		cmd = exec.CommandContext(ctx, "docker", dockerArgs...)
	} else {
		args = append(args, target)
		cmd = exec.CommandContext(ctx, "trivy", args...)
	}

	return runTrivyCmd(cmd, progressFn)
}

// GenerateSBOM generates a CycloneDX SBOM using Trivy and returns the output file path.
// If outputDir is non-empty, the SBOM file is written there instead of next to the target.
func GenerateSBOM(ctx context.Context, target string, targetType TargetType, source ToolSource, image string, server string, outputDir string) (string, error) {
	if image == "" {
		image = DefaultTrivyImage
	}

	// Determine output path
	var outputPath string
	sanitizedName := "sbom-report.json"
	if targetType == TargetImage {
		sanitizedName = fmt.Sprintf("sbom-%s.json", strings.NewReplacer("/", "_", ":", "_").Replace(target))
	}

	switch {
	case outputDir != "":
		outputPath = filepath.Join(outputDir, sanitizedName)
	case targetType == TargetDirectory:
		outputPath = filepath.Join(target, sanitizedName)
	case targetType == TargetImage:
		outputPath = sanitizedName
	default:
		return "", fmt.Errorf("unsupported target type: %s", targetType)
	}

	var args []string

	switch targetType {
	case TargetDirectory:
		args = []string{"fs", "--format", "cyclonedx", "--output", outputPath}
	case TargetImage:
		args = []string{"image", "--format", "cyclonedx", "--output", outputPath}
	}

	if server != "" {
		args = append(args, "--server", server)
	}

	var cmd *exec.Cmd

	if source == ToolSourceDocker {
		dockerArgs := []string{"run", "--rm"}
		if targetType == TargetDirectory {
			if outputDir != "" {
				dockerArgs = append(dockerArgs, "-v", target+":/scan:ro", "-v", outputDir+":/output")
				args = []string{"fs", "--format", "cyclonedx", "--output", "/output/" + sanitizedName}
			} else {
				dockerArgs = append(dockerArgs, "-v", target+":/scan")
				args = []string{"fs", "--format", "cyclonedx", "--output", "/scan/" + sanitizedName}
			}
			if server != "" {
				args = append(args, "--server", server)
			}
			args = append(args, "/scan")
		} else {
			if server == "" {
				dockerArgs = append(dockerArgs, "-v", "/var/run/docker.sock:/var/run/docker.sock:ro")
			}
			// Mount output directory
			mountDir := outputDir
			containerOutput := "/output/" + sanitizedName
			if mountDir == "" {
				mountDir = "."
			}
			dockerArgs = append(dockerArgs, "-v", mountDir+":/output")
			args = []string{"image", "--format", "cyclonedx", "--output", containerOutput}
			if server != "" {
				args = append(args, "--server", server)
			}
			args = append(args, target)
		}
		dockerArgs = append(dockerArgs, image)
		dockerArgs = append(dockerArgs, args...)
		cmd = exec.CommandContext(ctx, "docker", dockerArgs...)
	} else {
		args = append(args, target)
		cmd = exec.CommandContext(ctx, "trivy", args...)
	}

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("sbom generation failed: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("sbom generation failed: %w", err)
	}

	return outputPath, nil
}
