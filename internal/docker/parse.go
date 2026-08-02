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
	parts := strings.SplitN(netIO, "/", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	return parseSize(strings.TrimSpace(parts[0])), parseSize(strings.TrimSpace(parts[1]))
}

// parseSize parses Docker human-readable sizes like "1.5GB", "256MB", "10.2kB" into bytes
func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	multipliers := []struct {
		suffix string
		mult   float64
	}{
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
