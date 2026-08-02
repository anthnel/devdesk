package netdiag

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// dnsRecord holds one parsed answer record from dig output.
type dnsRecord struct {
	name  string
	ttl   string
	rtype string
	value string
}

// digResult holds the structured fields extracted from a dig output.
type digResult struct {
	status    string // NOERROR, NXDOMAIN, SERVFAIL, …
	question  string // e.g. "google.com. A"
	answers   []dnsRecord
	queryTime string // e.g. "2 ms"
	server    string // e.g. "8.8.8.8#53"
}

var (
	reDigStatus    = regexp.MustCompile(`status:\s*(\w+)`)
	reDigQueryTime = regexp.MustCompile(`;;\s*Query time:\s*(\d+)\s*msec`)
	reDigServer    = regexp.MustCompile(`;;\s*SERVER:\s*(\S+)`)
	reDigAnswer    = regexp.MustCompile(`^(\S+)\s+(\d+)\s+IN\s+(\w+)\s+(.+)$`)
	reDigQuestion  = regexp.MustCompile(`^;\s*(\S+)\s+IN\s+(\w+)`)
)

// parseDNSOutput extracts structured data from raw dig output.
func parseDNSOutput(output string) digResult {
	var res digResult
	section := ""

	for line := range strings.SplitSeq(output, "\n") {
		trimmed := strings.TrimSpace(line)

		// Section detection
		switch {
		case strings.Contains(trimmed, ";; QUESTION SECTION:"):
			section = "question"
			continue
		case strings.Contains(trimmed, ";; ANSWER SECTION:"):
			section = "answer"
			continue
		case strings.HasPrefix(trimmed, ";;") && strings.HasSuffix(trimmed, "SECTION:"):
			section = "other"
			continue
		}

		// Global metadata
		if m := reDigStatus.FindStringSubmatch(trimmed); m != nil && res.status == "" {
			res.status = m[1]
		}
		if m := reDigQueryTime.FindStringSubmatch(trimmed); m != nil {
			res.queryTime = m[1] + " ms"
		}
		if m := reDigServer.FindStringSubmatch(trimmed); m != nil {
			srv := m[1]
			if idx := strings.Index(srv, "("); idx > 0 {
				srv = srv[:idx]
			}
			res.server = srv
		}

		switch section {
		case "question":
			if strings.HasPrefix(trimmed, ";") && !strings.HasPrefix(trimmed, ";;") {
				if m := reDigQuestion.FindStringSubmatch(trimmed); m != nil {
					res.question = strings.TrimSuffix(m[1], ".") + "  " + m[2]
				}
			}
		case "answer":
			if trimmed == "" || strings.HasPrefix(trimmed, ";") {
				continue
			}
			if m := reDigAnswer.FindStringSubmatch(trimmed); m != nil {
				value := strings.TrimSpace(m[4])
				value = strings.TrimSuffix(value, ".")
				res.answers = append(res.answers, dnsRecord{
					name:  strings.TrimSuffix(m[1], "."),
					ttl:   m[2],
					rtype: m[3],
					value: value,
				})
			}
		}
	}

	return res
}

// formatDNSOutput renders a parsed dig output into styled, padded lines for the details viewport.
// Returns one padded string per display line, ready to join with "\n".
func formatDNSOutput(output string, width int) []string {
	res := parseDNSOutput(output)
	var lines []string

	// --- Status ---
	statusIcon := dnsStatusIcon(res.status, len(res.answers))
	statusText := res.status
	if statusText == "" {
		statusText = "unknown"
	}
	statusColor := theme.ColorOK
	if res.status != "NOERROR" {
		statusColor = theme.ColorError
	}
	statusStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(statusColor).Bold(true)
	lines = append(lines,
		theme.PadWithBg(statusIcon+spaceBg()+dimStyle().Render("Status")+spaceBg()+spaceBg()+statusStyle.Render(statusText), width),
		theme.EmptyLineBg(width),
	)

	// --- Query ---
	if res.question != "" {
		lines = append(lines,
			theme.PadWithBg(dnsSectionBullet()+spaceBg()+dimStyle().Render("Query")+spaceBg()+spaceBg()+textStyle().Render(res.question), width),
			theme.EmptyLineBg(width),
		)
	}

	// --- Answers ---
	typeStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorHighlight)
	switch {
	case len(res.answers) > 0:
		lines = append(lines, theme.PadWithBg(dnsSectionBullet()+spaceBg()+dimStyle().Render("Answers"), width))
		for _, rec := range res.answers {
			txt := theme.Bg("  ") +
				textStyle().Render(rec.value) +
				spaceBg() + spaceBg() +
				typeStyle.Render(rec.rtype) +
				spaceBg() + spaceBg() +
				dimStyle().Render("TTL "+rec.ttl+"s")
			lines = append(lines, theme.PadWithBg(txt, width))
		}
		lines = append(lines, theme.EmptyLineBg(width))
	case res.status == "NOERROR":
		lines = append(lines,
			theme.PadWithBg(dnsSectionBullet()+spaceBg()+dimStyle().Render("Answers")+spaceBg()+spaceBg()+dimStyle().Render("(none)"), width),
			theme.EmptyLineBg(width),
		)
	}

	// --- Stats ---
	if res.queryTime != "" {
		lines = append(lines,
			theme.PadWithBg(dnsSectionBullet()+spaceBg()+dimStyle().Render("Query time")+spaceBg()+spaceBg()+textStyle().Render(res.queryTime), width),
		)
	}
	if res.server != "" {
		lines = append(lines,
			theme.PadWithBg(dnsSectionBullet()+spaceBg()+dimStyle().Render("Server")+spaceBg()+spaceBg()+textStyle().Render(res.server), width),
		)
	}

	return lines
}

// dnsStatusIcon returns a colored status icon based on the DNS result.
func dnsStatusIcon(status string, answerCount int) string {
	base := lipgloss.NewStyle().Background(theme.ColorBackground)
	switch {
	case status == "NOERROR" && answerCount > 0:
		return base.Foreground(theme.ColorOK).Render(theme.IconOK)
	case status == "NOERROR" && answerCount == 0:
		return base.Foreground(theme.ColorWarn).Render(theme.IconWarning)
	default:
		return base.Foreground(theme.ColorError).Render(theme.IconError)
	}
}

// dnsSectionBullet returns a dimmed bullet used for section labels.
func dnsSectionBullet() string {
	return lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorDim).Render("◆")
}
