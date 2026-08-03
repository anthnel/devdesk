package security

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func (m Model) GetShortcuts() shortcut.Shortcuts {
	switch m.state {
	case StateInput:
		shortcuts := []shortcut.Shortcut{
			{Key: "space", Description: "Toggle"},
			{Key: "←→", Description: "Cycle value"},
			{Key: "enter / ctrl+s", Description: "Scan"},
		}
		// Show 'b' shortcut for directory and image modes on target field
		if m.focusedField == 1 {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "b", Description: "Browse"})
		}
		shortcuts = append(shortcuts,
			shortcut.Shortcut{Key: "alt+:", Description: "Command"},
			shortcut.Shortcut{Key: "?", Description: "Help"},
		)
		return shortcuts
	case StateScanning:
		return []shortcut.Shortcut{
			// {Key: "scanning...", Description: ""},
		}
	case StateResults:
		shortcuts := []shortcut.Shortcut{
			{Key: "tab", Description: "Switch tab"},
			{Key: "enter", Description: "Details"},
		}
		if m.activeTab == TabCVE || m.activeTab == TabLicense || m.activeTab == TabMisconfig {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: ".", Description: "Filter"})
		}
		if m.activeTab == TabSecrets {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "i", Description: "Ignore"})
		}
		shortcuts = append(shortcuts,
			shortcut.Shortcut{Key: "ctrl+r", Description: "New scan"},
			shortcut.Shortcut{Key: "alt+:", Description: "Command"},
			shortcut.Shortcut{Key: "?", Description: "Help"},
		)
		return shortcuts
	case StateDetails:
		shortcuts := []shortcut.Shortcut{
			{Key: "esc/⌫", Description: "Back"},
		}
		if m.selectedIdx < len(m.filteredFindings) {
			f := m.filteredFindings[m.selectedIdx]
			if len(f.References) > 0 {
				shortcuts = append(shortcuts, shortcut.Shortcut{Key: "o", Description: "Open ref"})
			}
		}
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "alt+:", Description: "Command"})
		return shortcuts
	}
	return nil
}

func (m Model) GetTitle() string {
	base := theme.IconSecurity + " Security Scanner"
	if m.state == StateInput {
		return base + " " + theme.IconChevronRight + " Scan Configuration"
	}
	if m.targetPath != "" && (m.state == StateScanning || m.state == StateResults || m.state == StateDetails) {
		annotation := lipgloss.NewStyle().
			Foreground(theme.ColorSecondary).
			Background(theme.ColorBackground).
			Render("(" + m.targetPath + ")")
		return base + " " + annotation
	}
	return base
}

func (m Model) GetIcon() string {
	return ""
}

// GetHeaderInfo returns the key-value info for the header
func (m Model) GetHeaderInfo(_ string) []shortcut.HeaderInfo {
	trivyVal := "not found"
	if m.deps.TrivyAvailable {
		trivyVal = m.parseVersion(m.deps.TrivyVersion)
	}
	gitleaksVal := "not found"
	if m.deps.GitleaksAvailable {
		gitleaksVal = m.parseVersion(m.deps.GitleaksVersion)
	}

	info := []shortcut.HeaderInfo{
		{Key: "Trivy", Value: trivyVal, Style: theme.HeaderValueStyle},
		{Key: "Gitleaks", Value: gitleaksVal, Style: theme.HeaderValueStyle},
	}

	// If we have results, add scan info to header
	if m.result != nil {
		info = append(info, shortcut.HeaderInfo{
			Key:   "Duration",
			Value: m.result.Duration.Round(1e8).String(),
			Style: theme.HeaderValueStyle,
		})

		info = append(info, shortcut.HeaderInfo{
			Key:   "Filter",
			Value: strings.ToUpper(m.severityFilter),
			Style: theme.HeaderValueStyle,
		})

		// Secrets count
		info = append(info, shortcut.HeaderInfo{
			Key:   "Secrets",
			Value: fmt.Sprintf("%d", m.result.SecretCount),
			Style: theme.HeaderValueStyle,
		})

		// Licenses count
		info = append(info, shortcut.HeaderInfo{
			Key:   "Licenses",
			Value: fmt.Sprintf("%d", m.result.LicenseCount),
			Style: theme.HeaderValueStyle,
		})

		// CVE breakdown as a pill bar (vulnerabilities only) — pre-rendered, no style override
		info = append(info, shortcut.HeaderInfo{
			Key:   "CVEs",
			Value: renderSeverityBar(m.result.Counts),
		})

	}

	return info
}

// renderSeverityBar renders the color-coded CVE pill bar
func renderSeverityBar(c scan.SeverityCounts) string {
	renderBlock := func(count int, bg, fg lipgloss.Color) string {
		s := lipgloss.NewStyle().
			Background(bg).
			Foreground(fg).
			Width(4).
			Align(lipgloss.Center)
		if count == 0 {
			s = s.Foreground(theme.ColorDim)
		}
		return s.Render(fmt.Sprintf("%d", count))
	}

	return renderBlock(c.Critical, theme.ColorSeverityCritical, theme.ColorSeverityCriticalFg) +
		renderBlock(c.High, theme.ColorSeverityHigh, theme.ColorSeverityHighFg) +
		renderBlock(c.Medium, theme.ColorSeverityMedium, theme.ColorSeverityMediumFg) +
		renderBlock(c.Low, theme.ColorSeverityLow, theme.ColorSeverityLowFg) +
		renderBlock(c.Unknown, theme.ColorSeverityInfo, theme.ColorSeverityInfoFg)
}

// parseVersion extracts version number from tool output
func (m Model) parseVersion(versionOutput string) string {
	if versionOutput == "" {
		return ""
	}

	isDocker := false
	output := versionOutput

	// Handle docker versions (format: "docker:Version X.Y.Z...")
	if strings.HasPrefix(versionOutput, "docker:") {
		isDocker = true
		output = strings.TrimPrefix(versionOutput, "docker:")
		if output == "" {
			return "(docker)"
		}
	}

	// Extract first line and try to find version number
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		if isDocker {
			return "(docker)"
		}
		return ""
	}

	firstLine := strings.TrimSpace(lines[0])
	// Try to extract version pattern (e.g., "v0.50.1" or "0.50.1")
	for _, part := range strings.Fields(firstLine) {
		if strings.HasPrefix(part, "v") || (len(part) > 0 && part[0] >= '0' && part[0] <= '9') {
			if isDocker {
				return part + " (docker)"
			}
			return part
		}
	}

	// Return first line if no version pattern found (truncated)
	result := firstLine
	if len(result) > 15 {
		result = result[:15] + "..."
	}
	if isDocker {
		return result + " (docker)"
	}
	return result
}

// GetHelpContent retourne le contenu d'aide de la vue Security
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "Security Scanner",
		Description: "This view scans directories or Docker images for vulnerabilities, exposed secrets, license issues, and IaC misconfigurations. Optionally generates SBOM in CycloneDX format. Scans use Trivy and Gitleaks via Docker or local binary.",
		KeyBindings: []help.KeyBinding{
			{Key: "↑/k", Description: "Move selection up"},
			{Key: "↓/j", Description: "Move selection down"},
			{Key: "g/Home", Description: "Go to top of list"},
			{Key: "G/End", Description: "Go to bottom of list"},
			{Key: "enter / ctrl+s", Description: "Start scan (from input form) / view details (in results)"},
			{Key: "space", Description: "Toggle a scan option (only key that toggles checkboxes)"},
			{Key: "ctrl+s", Description: "Start scan (from input form)"},
			{Key: "b", Description: "Browse directories (workspaces view) or Docker images (on Target field)"},
			{Key: "i", Description: "Ignore a secret (add to .gitleaksignore, in Secrets results tab)"},
			{Key: "o", Description: "Open first reference URL in the default browser (detail view)"},
			{Key: "tab / shift+tab", Description: "Switch tabs in results (CVE, Secrets, Licenses, Misconfig)"},
			{Key: "1 / 2 / 3 / 4", Description: "Jump directly to a tab"},
			{Key: ".", Description: "Cycle severity filter (in CVE/Licenses/Misconfig results)"},
			{Key: "ctrl+r", Description: "New scan (from results)"},
			{Key: "esc", Description: "Back / cancel"},
			{Key: "alt+:", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Scan Types",
				Body:  "Vulnerability Scan: detects CVEs in dependencies and packages (Trivy).\nSecret Scan: detects exposed secrets and API keys in code (Gitleaks + Trivy).\nMisconfig Scan: detects IaC misconfigurations in Dockerfiles, Terraform, K8s manifests (Trivy).\nLicense Scan: analyzes dependency licenses (Trivy).\nSBOM Generation: generates a CycloneDX SBOM report (Trivy).",
			},
			{
				Title: "Target Types",
				Body:  "Directory: scans a local directory. Use 'b' to browse via the Workspaces view.\nImage: scans a Docker image. Use 'b' to browse via the OCI Images view, or type the name manually (e.g., nginx:latest).",
			},
			{
				Title: "Advanced Options",
				Body:  "Trivy Server: use a remote Trivy server (client-server mode). Persisted to config.\nIgnore Unfixed: only show vulnerabilities that have available fixes.\nScan Git History: scan the full git history for secrets (slower but more thorough).\nGitleaks Config: specify a custom .gitleaks.toml configuration file.",
			},
			{
				Title: "Results",
				Body:  "Results are displayed by tab (CVE, Secrets, Licenses, Misconfig). Use '.' to cycle the severity filter. Press Enter to view finding details. For secrets, 'i' adds a finding to .gitleaksignore. If SBOM was generated, its path is shown above the tabs.",
			},
			{
				Title: "Command Logging",
				Body:  "Executed scanner commands are logged to the application log file (~/.devdesk/devdesk.log).",
			},
			{
				Title: "Prerequisites",
				Body:  "Trivy and Gitleaks must be installed (local binary or Docker image). Tool status is displayed in the header status bar.",
			},
		},
	}
}
