package docker

import (
	"strconv"
	"strings"
)

// safeSlice returns s[start:end] clamped to valid bounds
func safeSlice(s string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if start >= len(s) {
		return ""
	}
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}

// parseNetIO parses Docker NetIO string "1.2kB / 3.4kB" into received and transmitted bytes
func parseNetIO(netIO string) (rx, tx int64) {
	return parsePair(netIO)
}

// parsePair splits one of Docker's "used / total" cells into its two sides.
//
// It is named apart from parseNetIO because it serves MemUsage too, where the
// halves are "used" and "limit" rather than received and sent — the parsing is
// identical, the words are not, and a memory reading going through a function
// called parseNetIO would read as a mistake at every call site.
func parsePair(cell string) (left, right int64) {
	parts := strings.SplitN(cell, "/", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	return parseSize(strings.TrimSpace(parts[0])), parseSize(strings.TrimSpace(parts[1]))
}

// formatSize is parseSize's inverse, in Docker's own decimal units rather than
// binary ones: le chiffre est destiné à être comparé avec la sortie de
// `docker system df`, où 1 GB vaut 10⁹ octets.
func formatSize(bytes int64) string {
	switch {
	case bytes >= 1e9:
		return strconv.FormatFloat(float64(bytes)/1e9, 'f', 1, 64) + "GB"
	case bytes >= 1e6:
		return strconv.FormatFloat(float64(bytes)/1e6, 'f', 1, 64) + "MB"
	case bytes >= 1e3:
		return strconv.FormatFloat(float64(bytes)/1e3, 'f', 1, 64) + "kB"
	default:
		return strconv.FormatInt(bytes, 10) + "B"
	}
}

// parseSize parses Docker human-readable sizes like "1.5GB", "256MB", "10.2kB"
// and "5.324MiB" into bytes.
//
// Docker prints **both** unit families and does not mix them arbitrarily: net
// and block IO come out decimal (kB, MB, GB), memory binary (KiB, MiB, GiB).
// The binary suffixes are not decoration — without them "15.18GiB" fell through
// every decimal case to the bare "B", failed to parse "15.18Gi" as a number,
// and returned **0**. A zero here is a denominator, so it was worth more than a
// rounding difference.
func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	// Order matters: the binary suffixes end in "B" too, so a decimal-first
	// table would match "GB" against "GiB"'s tail and never reach them. The
	// bare "B" stays last for the same reason.
	multipliers := []struct {
		suffix string
		mult   float64
	}{
		{"TiB", 1 << 40},
		{"GiB", 1 << 30},
		{"MiB", 1 << 20},
		{"KiB", 1 << 10},
		{"kiB", 1 << 10},
		{"TB", 1e12},
		{"GB", 1e9},
		{"MB", 1e6},
		{"kB", 1e3},
		{"B", 1},
	}

	for _, m := range multipliers {
		if numStr, ok := strings.CutSuffix(s, m.suffix); ok {
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0
			}
			return int64(num * m.mult)
		}
	}
	return 0
}
