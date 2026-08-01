package netdiag

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/theme"
)

// trProbe holds the data for one traceroute probe (one of the three sent per hop).
type trProbe struct {
	host    string    // display hostname; equals IP when unresolved; empty = timeout
	ip      string    // raw IP address; empty = timeout
	times   []float64 // RTT in ms (may be >1 when multiple consecutive probes hit the same host)
	timeout bool
}

var (
	reHopLine = regexp.MustCompile(`^\s*(\d+)\s+(.*)$`)
	reHostIP  = regexp.MustCompile(`^(\S+)\s+\((\d[\d.:a-fA-F]+)\)`)
	reBareIP  = regexp.MustCompile(`^(\d[\d.:]+)$`)
	reLatency = regexp.MustCompile(`^(\d+\.\d+)\s+ms`)
)

// formatTracerouteOutput formats a raw traceroute output for display in the details viewport.
// Simple hops are shown on one line; ECMP hops are expanded to indented sub-lines.
// Returns one padded string per output line, ready to join with "\n".
func formatTracerouteOutput(output string, width int) []string {
	var result []string
	for rawLine := range strings.SplitSeq(output, "\n") {
		result = append(result, formatTracerouteLine(rawLine, width)...)
	}
	return result
}

// formatTracerouteLine formats a single raw traceroute line into one or more padded lines.
func formatTracerouteLine(rawLine string, width int) []string {
	trimmed := strings.TrimSpace(rawLine)

	// Header line (e.g. "traceroute to google.com …") — render dimmed.
	if strings.HasPrefix(trimmed, "traceroute to") || strings.HasPrefix(trimmed, "Selected device") {
		return []string{theme.PadWithBg(dimStyle().Render(rawLine), width)}
	}

	m := reHopLine.FindStringSubmatch(rawLine)
	if m == nil {
		// Not a hop line (empty line, error message, etc.)
		return []string{theme.PadWithBg(dimStyle().Render(rawLine), width)}
	}

	hopNum := m[1]
	probes := parseProbes(m[2])

	icon := hopStatusIcon(worstLatency(probes))
	return renderHop(icon, hopNum, probes, width)
}

// parseProbes tokenises the post-hop-number part of a traceroute line into probes.
func parseProbes(rest string) []trProbe {
	var probes []trProbe
	tokens := strings.Fields(rest)

	i := 0
	for i < len(tokens) {
		tok := tokens[i]

		// Timeout probe
		if tok == "*" {
			probes = append(probes, trProbe{timeout: true})
			i++
			continue
		}

		// Latency value belonging to the most recent probe
		if lm := reLatency.FindString(tok + " " + nextOrEmpty(tokens, i+1) + " ms"); lm != "" {
			// tok is the numeric part, tokens[i+1] should be "ms"
			if i+1 < len(tokens) && tokens[i+1] == "ms" {
				ms, err := strconv.ParseFloat(tok, 64)
				if err == nil {
					if len(probes) == 0 {
						// Shouldn't happen in well-formed output, but handle gracefully.
						probes = append(probes, trProbe{})
					}
					probes[len(probes)-1].times = append(probes[len(probes)-1].times, ms)
					i += 2
					continue
				}
			}
		}

		// "hostname (IP)" pair
		combined := tok
		if i+1 < len(tokens) && strings.HasPrefix(tokens[i+1], "(") {
			combined = tok + " " + tokens[i+1]
		}
		if hm := reHostIP.FindStringSubmatch(combined); hm != nil {
			probes = append(probes, trProbe{host: hm[1], ip: hm[2]})
			i += 2
			continue
		}

		// Bare IP (no hostname resolution)
		if reBareIP.MatchString(tok) {
			probes = append(probes, trProbe{host: tok, ip: tok})
			i++
			continue
		}

		// Unknown token — skip
		i++
	}
	return probes
}

func nextOrEmpty(tokens []string, idx int) string {
	if idx < len(tokens) {
		return tokens[idx]
	}
	return ""
}

// worstLatency returns the maximum latency across all probes and whether any probe timed out.
func worstLatency(probes []trProbe) (worst float64, hasTimeout bool) {
	for _, p := range probes {
		if p.timeout {
			hasTimeout = true
			continue
		}
		for _, t := range p.times {
			if t > worst {
				worst = t
			}
		}
	}
	return worst, hasTimeout
}

// isECMP returns true when probes contain 2 or more distinct IP addresses.
func isECMP(probes []trProbe) bool {
	seen := map[string]bool{}
	for _, p := range probes {
		if !p.timeout {
			seen[p.ip] = true
		}
	}
	return len(seen) > 1
}

// renderHop builds the display lines for one hop.
func renderHop(icon, hopNum string, probes []trProbe, width int) []string {
	// Indent width = icon (2 runes) + space + padded hop number (3 chars) + 2 spaces
	// e.g. "󰗠  1  " = icon(2) + " "(1) + "  1"(3) + "  "(2) = 8 chars
	hopField := fmt.Sprintf("%3s", hopNum)
	indent := strings.Repeat(" ", lipgloss.Width(icon)+1+len(hopField)+2)

	if !isECMP(probes) {
		return []string{renderSimpleHop(icon, hopField, probes, indent, width)}
	}
	return renderECMPHop(icon, hopField, probes, indent, width)
}

// renderSimpleHop produces a single line: icon hopNum  host (ip)  t1  t2  t3
func renderSimpleHop(icon, hopField string, probes []trProbe, _ string, width int) string {
	txt := dimStyle().Render(icon) + spaceBg() +
		dimStyle().Render(hopField) + spaceBg() + spaceBg()

	if len(probes) == 0 {
		return theme.PadWithBg(txt, width)
	}

	// Collect all times from all probes (they all have the same host in a simple hop)
	var allTimes []float64
	for _, p := range probes {
		if p.timeout {
			txt += dimStyle().Render("*") + spaceBg()
		} else {
			allTimes = append(allTimes, p.times...)
		}
	}

	// Show host once (from first non-timeout probe)
	for _, p := range probes {
		if !p.timeout {
			txt += renderHost(p)
			break
		}
	}

	for _, ms := range allTimes {
		txt += spaceBg() + spaceBg() + latencyStyle(ms).Render(fmt.Sprintf("%.3f ms", ms))
	}

	return theme.PadWithBg(txt, width)
}

// renderECMPHop produces one line per probe, indented after the first.
func renderECMPHop(icon, hopField string, probes []trProbe, indent string, width int) []string {
	var lines []string

	for i, p := range probes {
		var txt string
		if i == 0 {
			txt = dimStyle().Render(icon) + spaceBg() +
				dimStyle().Render(hopField) + spaceBg() + spaceBg()
		} else {
			txt = theme.Bg(indent)
		}

		if p.timeout {
			txt += dimStyle().Render("*")
		} else {
			txt += renderHost(p)
			for _, ms := range p.times {
				txt += spaceBg() + spaceBg() + latencyStyle(ms).Render(fmt.Sprintf("%.3f ms", ms))
			}
		}

		lines = append(lines, theme.PadWithBg(txt, width))
	}

	return lines
}

// renderHost renders "hostname (IP)" with correct per-segment background.
func renderHost(p trProbe) string {
	if p.host == p.ip {
		// Unresolved: show plain IP
		return textStyle().Render(p.ip)
	}
	return textStyle().Render(p.host) + spaceBg() +
		dimStyle().Render("(") + textStyle().Render(p.ip) + dimStyle().Render(")")
}

// hopStatusIcon returns a colored icon based on the worst-case latency for a hop.
func hopStatusIcon(worstMs float64, hasTimeout bool) string {
	base := lipgloss.NewStyle().Background(theme.ColorBackground)
	switch {
	case hasTimeout || worstMs > 50:
		return base.Foreground(theme.ColorError).Render(theme.IconError)
	case worstMs >= 10:
		return base.Foreground(theme.ColorWarn).Render(theme.IconWarning)
	default:
		return base.Foreground(theme.ColorOK).Render(theme.IconOK)
	}
}

// latencyStyle returns a lipgloss style for a given latency value.
func latencyStyle(ms float64) lipgloss.Style {
	return lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(latencyColor(ms))
}

// latencyColor returns the appropriate color for a given latency in milliseconds.
func latencyColor(ms float64) lipgloss.Color {
	switch {
	case ms > 50:
		return theme.ColorError
	case ms >= 10:
		return theme.ColorWarn
	default:
		return theme.ColorOK
	}
}

func dimStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorDim)
}

func textStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText)
}

func spaceBg() string {
	return theme.Bg(" ")
}
