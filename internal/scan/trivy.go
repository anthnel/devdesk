package scan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
func RunTrivy(ctx context.Context, target string, targetType TargetType, licenseMode bool, tool ToolSpec, server string, ignoreUnfixed bool, ignoreEOL bool, progressFn func(string)) ([]Finding, error) {
	tc, err := trivyArgs(target, targetType, licenseMode, tool, server, ignoreUnfixed, ignoreEOL)
	if err != nil {
		return nil, err
	}
	return runTrivy(ctx, tc, progressFn)
}

// trivySem serializes every Trivy process this application starts.
//
// Trivy's local cache — the vulnerability database and the fs cache, both
// BoltDB — allows only one writer at a time. Scanner.Scan starts several
// Trivy stages concurrently for a single target (vuln, secret, misconfig),
// and a batch scan runs several targets concurrently on top of that: two
// Trivy processes racing for the cache lock do not queue behind each other,
// the loser fails outright with "unable to acquire cache or database lock".
// A size-1 semaphore around every invocation turns that race into a queue.
var trivySem = make(chan struct{}, 1)

// runTrivy executes a built invocation and parses what it wrote to stdout.
//
// A non-zero exit is not on its own a failure: Trivy exits 1 when it finds
// vulnerabilities, and the report on stdout is exactly what the caller asked
// for. Only an exit that produced no report is reported as an error.
func runTrivy(ctx context.Context, tc toolCmd, progressFn func(string)) ([]Finding, error) {
	select {
	case trivySem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-trivySem }()

	stdout, err := runner.Run(ctx, tc, progressFn)
	if err != nil {
		var exit *exitError
		if !errors.As(err, &exit) || len(stdout) == 0 {
			return nil, fmt.Errorf("trivy failed: %w", err)
		}
	}
	return parseTrivyOutput(stdout)
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

		// Process secrets found by Trivy.
		//
		// The report has carried these since TrivySecret was declared; nothing
		// read them, so a secret Trivy found was parsed and dropped. Only the
		// secret stage asks for them (--scanners secret), so there is no risk of
		// the vulnerability stage returning the same finding twice.
		for _, secret := range result.Secrets {
			findings = append(findings, Finding{
				ID:          secret.RuleID,
				Title:       secret.Title,
				Description: secret.Category,
				Severity:    parseSeverity(secret.Severity),
				Source:      SourceTrivySecret,
				File:        result.Target,
				Line:        secret.StartLine,
				Match:       maskSecret(secret.Match),
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

// GetTrivyCommand returns the command that would be executed, for display and
// logging. It is built by the same builder as the executed command, so the two
// cannot drift apart.
func GetTrivyCommand(target string, targetType TargetType, licenseMode bool, tool ToolSpec, server string, ignoreUnfixed bool, ignoreEOL bool) string {
	tc, err := trivyArgs(target, targetType, licenseMode, tool, server, ignoreUnfixed, ignoreEOL)
	if err != nil {
		return ""
	}
	return tc.String()
}

// GetTrivySecretCommand returns the secret scan command for display.
func GetTrivySecretCommand(target string, targetType TargetType, tool ToolSpec, server string) string {
	tc, err := trivySecretArgs(target, targetType, tool, server)
	if err != nil {
		return ""
	}
	return tc.String()
}

// RunTrivySecret executes Trivy with --scanners secret and returns findings.
// progressFn is an optional callback called with each stderr line.
func RunTrivySecret(ctx context.Context, target string, targetType TargetType, tool ToolSpec, server string, progressFn func(string)) ([]Finding, error) {
	tc, err := trivySecretArgs(target, targetType, tool, server)
	if err != nil {
		return nil, err
	}
	return runTrivy(ctx, tc, progressFn)
}

// GetTrivyMisconfigCommand returns the misconfiguration scan command for display.
func GetTrivyMisconfigCommand(target string, targetType TargetType, tool ToolSpec, server string, ignoreEOL bool) string {
	tc, err := trivyMisconfigArgs(target, targetType, tool, server, ignoreEOL)
	if err != nil {
		return ""
	}
	return tc.String()
}

// RunTrivyMisconfig executes Trivy with --scanners misconfig and returns findings.
// progressFn is an optional callback called with each stderr line.
func RunTrivyMisconfig(ctx context.Context, target string, targetType TargetType, tool ToolSpec, server string, ignoreEOL bool, progressFn func(string)) ([]Finding, error) {
	tc, err := trivyMisconfigArgs(target, targetType, tool, server, ignoreEOL)
	if err != nil {
		return nil, err
	}
	return runTrivy(ctx, tc, progressFn)
}
