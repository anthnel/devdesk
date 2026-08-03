package security

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// renderWarningsPanel renders scan errors in the table area so they are always fully visible.
// Only FATAL/ERROR/WARN log lines are shown — INFO lines and progress bars are stripped.
func (m Model) renderWarningsPanel() string {
	var b strings.Builder

	wrapWidth := max(m.width-6, 40)
	wrapStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Width(wrapWidth)

	b.WriteString(theme.StatusWarningStyle.Render("⚠ Scan Warnings") + "\n\n")
	for _, errMsg := range m.result.Errors {
		source, detail := parseScanError(errMsg)
		if source != "" {
			b.WriteString(theme.SubTitleStyle.Render("  "+source) + "\n")
		}
		b.WriteString(wrapStyle.Render("  "+detail) + "\n\n")
	}

	return b.String()
}

// parseScanError splits a scan error string into a source label (e.g. "trivy vuln") and
// a cleaned detail message with only FATAL/ERROR/WARN lines from tool stderr.
func parseScanError(errMsg string) (source, detail string) {
	// Format: "source: tool failed: <stderr>"
	idx := strings.Index(errMsg, ": ")
	if idx == -1 {
		return "", extractMeaningfulLines(errMsg)
	}
	source = errMsg[:idx]
	rest := errMsg[idx+2:]

	// Strip the intermediate "tool failed: " prefix
	for _, prefix := range []string{
		"trivy failed: ",
		"trivy misconfig failed: ",
		"sbom generation failed: ",
		"gitleaks failed: ",
	} {
		if strings.HasPrefix(rest, prefix) {
			rest = rest[len(prefix):]
			break
		}
	}

	return source, extractMeaningfulLines(rest)
}

// extractMeaningfulLines filters tool stderr output, keeping only FATAL/ERROR/WARN lines
// and discarding INFO log lines and progress bar output.
// Handles both tab (\t) and multi-space separators (zerolog vs logrus formats).
func extractMeaningfulLines(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Skip progress bar lines (download indicators from trivy-db)
		if strings.Contains(line, " MiB /") || strings.Contains(line, " p/s ") || strings.Contains(line, "[---") {
			continue
		}
		// Detect timestamped log line: starts with YYYY-MM-DD
		if len(line) > 10 && line[4] == '-' && line[7] == '-' {
			level, message := splitLogLine(line)
			switch strings.ToUpper(level) {
			case "INFO":
				continue // discard
			case "FATAL", "ERROR", "WARN", "WARNING":
				kept = append(kept, formatScanErrorLine(level, message))
				continue
			}
		}
		// Non-structured line (plain error text) — keep as-is
		kept = append(kept, line)
	}
	if len(kept) == 0 {
		return strings.TrimSpace(text) // fallback: original message
	}
	return strings.Join(kept, "\n")
}

// splitLogLine splits a timestamped log line (YYYY-MM-DDThh:mm:ssZ<sep>LEVEL<sep>message)
// into level and message. Handles tab and multi-space separators.
func splitLogLine(line string) (level, message string) {
	// Skip past the timestamp — find first whitespace after opening chars
	tEnd := strings.IndexAny(line, " \t")
	if tEnd < 0 {
		return "", line
	}
	rest := strings.TrimLeft(line[tEnd:], " \t")
	lEnd := strings.IndexAny(rest, " \t")
	if lEnd < 0 {
		return rest, ""
	}
	return rest[:lEnd], strings.TrimLeft(rest[lEnd:], " \t")
}

// formatScanErrorLine formats a FATAL/ERROR/WARN log message as "Error : <message>".
// For FATAL lines, Trivy prepends "Fatal error <sep> run error: " which is stripped.
func formatScanErrorLine(level, message string) string {
	if strings.ToUpper(level) == "FATAL" {
		if idx := strings.Index(message, "run error: "); idx >= 0 {
			message = message[idx+len("run error: "):]
		} else {
			message = strings.TrimLeft(strings.TrimPrefix(message, "Fatal error"), " \t")
		}
	}
	return "Error : " + message
}
