package scan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/anthnel/devdesk/internal/engine"
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
// something. It is a result, not a failure — but it is not only that: gitleaks
// exits 1 for a fatal error too, and what separates them is the report. See
// RunGitleaks.
const gitleaksSecretsFound = 1

// gitleaksConfigMount is where a configured rules file is mounted inside the
// container, so the argument passed to gitleaks is never the host path.
//
// The container root is where it goes because that is the one place nothing
// else can be: the scanned target is mounted at containerScanPath, so no file
// of the repository can land beside it, and the image itself holds no
// /gitleaks.toml — its root is a plain Alpine tree (checked on
// zricethezav/gitleaks v8.30.1). Writing the name down here is the point: a
// mount path chosen at the call site is one nobody can check against the
// target's.
const gitleaksConfigMount = "/gitleaks.toml"

// gitleaksArgs builds the invocation. Gitleaks writes its report to a path
// rather than to stdout, so the report path is redirected at the process's own
// stdout — which is a different pseudo-file inside a container.
func gitleaksArgs(target string, tool ToolSpec, history bool, configPath string) toolCmd {
	docker := tool.Source == ToolSourceContainer

	// In Docker mode the flag has to name the mount rather than the host path.
	// The file is not in the container otherwise, and gitleaks then dies before
	// scanning anything — which used to be reported as a clean repository (D56).
	flagPath := configPath
	if docker && configPath != "" {
		flagPath = gitleaksConfigMount
	}

	appendOptions := func(args []string) []string {
		// History is the expensive mode, so it is opted into by dropping
		// --no-git rather than by adding a flag.
		if !history {
			args = append(args, "--no-git")
		}
		if configPath != "" {
			args = append(args, "--config", flagPath)
		}
		return args
	}

	if docker {
		image := gitleaksImage(tool.Image)
		args := []string{
			"run", "--rm",
			"-v", target + ":" + containerScanPath + ":ro",
		}
		// Before the image name: everything after it is gitleaks' own argv.
		if configPath != "" {
			args = append(args, "-v", configPath+":"+gitleaksConfigMount+":ro")
		}
		args = append(args,
			image,
			"detect",
			"--source", containerScanPath,
			"--gitleaks-ignore-path", containerScanPath,
			"--report-format", "json",
			"--report-path", "/dev/fd/1",
		)
		return toolCmd{Name: engine.Current().Binary, Args: appendOptions(args)}
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

// checkGitleaksConfig refuses a rules file that cannot be read, before
// anything is started.
//
// It is not the guard against a bad configuration — RunGitleaks is, and it
// reports gitleaks' own diagnostic, which also covers a file that exists and
// does not parse. This one exists for the side effect: `docker run -v` on a
// host path that is not there does not fail, it **creates a directory** at that
// path and mounts it (measured on Docker Desktop 29.7.2, which made a
// `nope.toml/` and its parent). A typo in gitleaks_config would leave
// directories on the user's disk, once per scan.
func checkGitleaksConfig(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.Open(path) //nolint:gosec // the path is the user's own setting
	if err != nil {
		return fmt.Errorf("gitleaks config: %w", err)
	}
	return f.Close()
}

// RunGitleaks executes Gitleaks and returns findings.
// progressFn is an optional callback called with each stderr line.
//
// Gitleaks exits 1 for two unrelated things, and telling them apart is the
// whole of D56. Measured on v8.30.1:
//
//	clean          exit 0, stdout "[]"
//	secrets found  exit 1, stdout the report
//	fatal error    exit 1, stdout empty
//
// So the report is what separates a result from a failure, and an exit 1 with
// nothing on stdout is **never** a clean repository. It used to be read as one:
// a --config gitleaks could not load died before scanning a single byte, and
// the scan came back with SecretsScanned true and no findings — a green icon in
// ws for a repository nobody had looked at.
func RunGitleaks(ctx context.Context, target string, tool ToolSpec, history bool, configPath string, progressFn func(string)) ([]Finding, error) {
	if err := checkGitleaksConfig(configPath); err != nil {
		return nil, err
	}

	stdout, err := runner.Run(ctx, gitleaksArgs(target, tool, history, configPath), progressFn)
	if err != nil {
		// A report alongside exit 1 is gitleaks saying it found something.
		// Anything else — another exit code, a process that never ran, or an
		// exit 1 with no report at all — is a failure, and carries gitleaks'
		// own stderr with it.
		var exit *exitError
		if len(stdout) == 0 || !errors.As(err, &exit) || exit.Code != gitleaksSecretsFound {
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
