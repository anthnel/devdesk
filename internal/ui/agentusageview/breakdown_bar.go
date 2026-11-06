package agentusageview

import (
	"cmp"
	"math/big"
	"slices"

	"github.com/anthnel/devdesk/internal/agentusage"
)

// breakdownBarWidth is the width of the token-mix bar, in cells.
const breakdownBarWidth = 10

// Segment order of the bar.
const (
	segInput = iota
	segOutput
	segCacheWrite
	segCacheRead
	segCount
)

// share is one part's exact claim on the bar: floor cells plus a remainder
// (in units of 1/total cell), and whether it is worth at least half a cell.
type share struct {
	index    int
	part     int64
	floor    int
	rem      *big.Int
	eligible bool
}

// apportion splits width cells between parts in proportion to them, using the
// largest-remainder method. Negative parts count as 0. A part worth less than
// half a cell gets no cell of its own; cells left over once every eligible
// part has had its turn go to the largest part. Equal remainders go to the larger part, then to
// the lowest index.
// The result has len(parts) entries and sums to width when any part is
// positive, 0 otherwise. Arithmetic is done in big integers so counters near
// math.MaxInt64 neither overflow nor lose precision.
func apportion(parts []int64, width int) []int {
	cells := make([]int, len(parts))
	total := new(big.Int)
	for _, p := range parts {
		total.Add(total, big.NewInt(max(p, 0)))
	}
	if width <= 0 || total.Sign() == 0 {
		return cells
	}
	shares := make([]share, len(parts))
	remaining := width
	for i, p := range parts {
		shares[i] = newShare(i, max(p, 0), width, total)
		cells[i] = shares[i].floor
		remaining -= shares[i].floor
	}
	remaining = giveByRemainder(cells, shares, remaining)
	if remaining > 0 {
		cells[largestPart(parts)] += remaining
	}
	return cells
}

// newShare computes part p's exact claim on width cells out of total.
func newShare(index int, p int64, width int, total *big.Int) share {
	num := new(big.Int).Mul(big.NewInt(p), big.NewInt(int64(width)))
	q, r := new(big.Int).QuoRem(num, total, new(big.Int))
	s := share{index: index, part: p, rem: r, eligible: num.Lsh(num, 1).Cmp(total) >= 0}
	if s.eligible {
		s.floor = int(q.Int64())
	}
	return s
}

// giveByRemainder hands one extra cell to each eligible share, largest
// remainder first, while cells remain; it returns the cells still unplaced.
func giveByRemainder(cells []int, shares []share, remaining int) int {
	order := slices.DeleteFunc(slices.Clone(shares), func(s share) bool { return !s.eligible })
	slices.SortStableFunc(order, func(a, b share) int {
		if c := b.rem.Cmp(a.rem); c != 0 {
			return c
		}
		return cmp.Compare(b.part, a.part)
	})
	for _, s := range order {
		if remaining == 0 {
			break
		}
		cells[s.index]++
		remaining--
	}
	return remaining
}

// largestPart returns the index of the largest part, the lowest on a tie.
func largestPart(parts []int64) int {
	best := 0
	for i, p := range parts {
		if p > parts[best] {
			best = i
		}
	}
	return best
}

// segmentCells is apportion over t's four categories in segment order, at
// breakdownBarWidth; ok is false when t has no token. Note that
// agentusage.Tokens declares CacheRead before CacheWrite, unlike the bar.
func segmentCells(t agentusage.Tokens) (cells [segCount]int, ok bool) {
	parts := make([]int64, segCount)
	parts[segInput] = t.Input
	parts[segOutput] = t.Output
	parts[segCacheWrite] = t.CacheWrite
	parts[segCacheRead] = t.CacheRead
	copy(cells[:], apportion(parts, breakdownBarWidth))
	return cells, slices.ContainsFunc(cells[:], func(c int) bool { return c > 0 })
}
