package scan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// GitleaksFinding represents a single finding from Gitleaks
type GitleaksFinding struct {
	RuleID      string `json:"RuleID"`
	Description string `json:"Description"`
	StartLine   int    `json:"StartLine"`
	EndLine     int    `json:"EndLine"`
	StartColumn int    `json:"StartColumn"`
	EndColumn   int    `json:"EndColumn"`
	Match       string `json:"Match"`
	Secret      string `json:"Secret"`
	File        string `json:"File"`
	Fingerprint string `json:"Fingerprint,omitempty"`
	Commit      string `json:"Commit,omitempty"`
	Author      string `json:"Author,omitempty"`
	Email       string `json:"Email,omitempty"`
	Date        string `json:"Date,omitempty"`
	Message     string `json:"Message,omitempty"`
}

// gitleaksSecretsFound is the exit code Gitleaks uses to say it found
// something. It is a result, not a failure.
const gitleaksSecretsFound = 1

// gitleaksArgs builds the invocation. Gitleaks writes its report to a path
// rather than to stdout, so the report path is redirected at the process's own
// stdout — which is a different pseudo-file inside a container.
func gitleaksArgs(target string, tool ToolSpec, history bool, configPath string) toolCmd {
	appendOptions := func(args []string) []string {
		// History is the expensive mode, so it is opted into by dropping
		// --no-git rather than by adding a flag.
		if !history {
			args = append(args, "--no-git")
		}
		if configPath != "" {
			args = append(args, "--config", configPath)
		}
		return args
	}

	if tool.Source == ToolSourceDocker {
		image := gitleaksImage(tool.Image)
		args := []string{
			"run", "--rm",
			"-v", target + ":" + containerScanPath + ":ro",
			image,
			"detect",
			"--source", containerScanPath,
			"--gitleaks-ignore-path", containerScanPath,
			"--report-format", "json",
			"--report-path", "/dev/fd/1",
		}
		return toolCmd{Name: "docker", Args: appendOptions(args)}
	}

	args := []string{
		"detect",
		"--source", target,
		"--gitleaks-ignore-path", target,
		"--report-format", "json",
		"--report-path", "/dev/stdout",
	}
	return toolCmd{Name: gitleaksBinary(tool), Args: appendOptions(args)}
}

// RunGitleaks executes Gitleaks and returns findings.
// progressFn is an optional callback called with each stderr line.
func RunGitleaks(ctx context.Context, target string, tool ToolSpec, history bool, configPath string, progressFn func(string)) ([]Finding, error) {
	stdout, err := runner.Run(ctx, gitleaksArgs(target, tool, history, configPath), progressFn)
	if err != nil && len(stdout) == 0 {
		// Exit 1 with no report means it ran and found nothing; any other
		// non-zero exit, or a process that never ran, is a genuine failure.
		var exit *exitError
		if !errors.As(err, &exit) || exit.Code != gitleaksSecretsFound {
			return nil, fmt.Errorf("gitleaks failed: %w", err)
		}
	}

	if len(stdout) == 0 {
		return []Finding{}, nil
	}
	return parseGitleaksOutput(stdout)
}

// parseGitleaksOutput parses Gitleaks JSON output into findings
func parseGitleaksOutput(data []byte) ([]Finding, error) {
	var gitleaksFindings []GitleaksFinding
	if err := json.Unmarshal(data, &gitleaksFindings); err != nil {
		return nil, fmt.Errorf("failed to parse gitleaks output: %w", err)
	}

	findings := make([]Finding, 0, len(gitleaksFindings))

	for _, gf := range gitleaksFindings {
		findings = append(findings, Finding{
			ID:          gf.RuleID,
			Title:       fmt.Sprintf("Secret detected: %s", gf.RuleID),
			Description: gf.Description,
			Severity:    SeverityHigh, // Secrets are always high severity
			Source:      "gitleaks",
			File:        gf.File,
			Line:        gf.StartLine,
			Match:       maskSecret(gf.Secret),
			Fingerprint: gf.Fingerprint,
		})
	}

	return findings, nil
}

// GetGitleaksCommand returns the command that would be executed, for display
// and logging. It is built by the same builder as the executed command, so the
// two cannot drift apart.
func GetGitleaksCommand(target string, tool ToolSpec, history bool, configPath string) string {
	return gitleaksArgs(target, tool, history, configPath).String()
}

// AddToGitleaksIgnore adds a finding to the .gitleaksignore file in the target directory
// Returns nil if already present (silently skips duplicates)
func AddToGitleaksIgnore(targetDir string, finding Finding) (err error) {
	ignorePath := targetDir + "/.gitleaksignore"

	// Use the fingerprint as the ignore entry (gitleaks native format)
	entry := finding.Fingerprint
	if entry == "" {
		// Fallback: build fingerprint manually
		entry = fmt.Sprintf("%s:%s:%d", finding.File, finding.ID, finding.Line)
	}

	// Check if entry already exists
	if existingContent, readErr := os.ReadFile(ignorePath); readErr == nil {
		if strings.Contains(string(existingContent), entry) {
			// Already present, silently skip
			return nil
		}
	}

	// Open file in append mode, create if doesn't exist
	f, err := os.OpenFile(ignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open .gitleaksignore: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close .gitleaksignore: %w", cerr)
		}
	}()

	if _, err := f.WriteString(entry + "\n"); err != nil {
		return fmt.Errorf("failed to write to .gitleaksignore: %w", err)
	}

	return nil
}
