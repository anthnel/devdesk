package agentusageview

import (
	"fmt"
	"math/bits"
)

const (
	// shareScale is a share's precision: tenths of a percent.
	shareScale = 1000
	// noShare marks a share that cannot be computed: the session has no tokens.
	noShare = -1
)

// shares returns each total's part of the session total in tenths of a
// percent, floored. The entry at mainIdx takes the rounding residue so the
// parts add up to shareScale; mainIdx < 0 leaves every part floored. A zero
// total yields noShare everywhere.
func shares(totals []int64, total int64, mainIdx int) []int {
	out := make([]int, len(totals))
	if total <= 0 {
		for i := range out {
			out[i] = noShare
		}
		return out
	}
	rest := shareScale
	for i, t := range totals {
		out[i] = floorTenths(t, total)
		if i != mainIdx {
			rest -= out[i]
		}
	}
	if mainIdx >= 0 && mainIdx < len(out) {
		out[mainIdx] = max(rest, 0) // clamped: saturated totals can overshoot
	}
	return out
}

// floorTenths is floor(part*shareScale/total) on 128 bits, so no product
// overflows. part is clamped to [0, total]; total > 0.
func floorTenths(part, total int64) int {
	part = min(max(part, 0), total)
	hi, lo := bits.Mul64(uint64(part), shareScale)
	q, _ := bits.Div64(hi, lo, uint64(total)) // hi < total because part <= total
	return int(q)
}

// formatShare renders tenths of a percent: 123 is "12.3%"; noShare is "-".
func formatShare(tenths int) string {
	if tenths < 0 {
		return noValue
	}
	return fmt.Sprintf("%d.%d%%", tenths/10, tenths%10)
}
