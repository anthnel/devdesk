package scan

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

// RunGitleaks executes Gitleaks and returns findings.
// progressFn is an optional callback called with each stderr line.
func RunGitleaks(ctx context.Context, target string, source ToolSource, image string, history bool, configPath string, progressFn func(string)) ([]Finding, error) {
	if image == "" {
		image = DefaultGitleaksImage
	}

	var cmd *exec.Cmd

	if source == ToolSourceDocker {
		// Use Docker to run Gitleaks
		// Note: Use /dev/fd/1 instead of /dev/stdout for proper output in Docker
		dockerArgs := []string{
			"run", "--rm",
			"-v", target + ":/scan:ro",
			image,
			"detect",
			"--source", "/scan",
			"--gitleaks-ignore-path", "/scan",
			"--report-format", "json",
			"--report-path", "/dev/fd/1",
		}
		if !history {
			dockerArgs = append(dockerArgs, "--no-git")
		}
		if configPath != "" {
			dockerArgs = append(dockerArgs, "--config", configPath)
		}
		cmd = exec.CommandContext(ctx, "docker", dockerArgs...)
	} else {
		// Use binary directly
		args := []string{
			"detect",
			"--source", target,
			"--gitleaks-ignore-path", target,
			"--report-format", "json",
			"--report-path", "/dev/stdout",
		}
		if !history {
			args = append(args, "--no-git")
		}
		if configPath != "" {
			args = append(args, "--config", configPath)
		}
		cmd = exec.CommandContext(ctx, "gitleaks", args...)
	}

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
		return nil, fmt.Errorf("gitleaks failed to start: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		if stdoutBuf.Len() == 0 {
			// No output means actual error or no secrets found
			if exitErr, ok := err.(*exec.ExitError); ok {
				// Exit code 1 = secrets found (normal), other = error
				if exitErr.ExitCode() != 1 {
					return nil, fmt.Errorf("gitleaks failed: %s", string(exitErr.Stderr))
				}
			} else {
				return nil, fmt.Errorf("gitleaks failed: %w", err)
			}
		}
	}

	// Empty output means no secrets found
	if stdoutBuf.Len() == 0 {
		return []Finding{}, nil
	}

	return parseGitleaksOutput(stdoutBuf.Bytes())
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

// GetGitleaksCommand returns the command that would be executed (for display/logging purposes)
func GetGitleaksCommand(target string, source ToolSource, image string, history bool, configPath string) string {
	// Use default image if not specified
	if image == "" {
		image = DefaultGitleaksImage
	}

	if source == ToolSourceDocker {
		args := []string{
			"docker", "run", "--rm",
			"-v", target + ":/scan:ro",
			image,
			"detect",
			"--source", "/scan",
			"--gitleaks-ignore-path", "/scan",
			"--report-format", "json",
			"--report-path", "/dev/fd/1",
		}
		if !history {
			args = append(args, "--no-git")
		}
		if configPath != "" {
			args = append(args, "--config", configPath)
		}
		return strings.Join(args, " ")
	}

	args := []string{
		"gitleaks",
		"detect",
		"--source", target,
		"--gitleaks-ignore-path", target,
		"--report-format", "json",
		"--report-path", "/dev/stdout",
	}
	if !history {
		args = append(args, "--no-git")
	}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	return strings.Join(args, " ")
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
