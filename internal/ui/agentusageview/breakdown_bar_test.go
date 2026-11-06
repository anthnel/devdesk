package agentusageview

import (
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/agentusage"
)

func cellSum(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}

func TestApportion(t *testing.T) {
	const m = math.MaxInt64
	tests := []struct {
		name  string
		parts []int64
		width int
		want  []int
	}{
		{"single non-zero part takes the bar", []int64{1, 0, 0, 0}, 10, []int{10, 0, 0, 0}},
		{"single non-zero part, other slot", []int64{0, 0, 0, 7}, 10, []int{0, 0, 0, 10}},
		{"equal parts: ties go to the lower index", []int64{25, 25, 25, 25}, 10, []int{3, 3, 2, 2}},
		{"under half a cell stays at 0", []int64{1, 1, 1, 97}, 10, []int{0, 0, 0, 10}},
		{"exactly half a cell is eligible", []int64{5, 5, 0, 90}, 10, []int{1, 0, 0, 9}},
		{"no eligible part left: remainder to the largest", []int64{4, 4, 4, 88}, 10, []int{0, 0, 0, 10}},
		{"exact shares", []int64{30, 0, 0, 70}, 10, []int{3, 0, 0, 7}},
		{"largest remainder rounds up", []int64{2, 1, 0, 0}, 10, []int{7, 3, 0, 0}},
		{"largest remainder, not nearest", []int64{1, 1, 1, 0}, 10, []int{4, 3, 3, 0}},
		{"equal remainders: the larger part first, then the lower index", []int64{1, 4, 10}, 10, []int{0, 3, 7}},
		{"equal remainders, equal parts: the lower index", []int64{1, 1, 4}, 9, []int{2, 1, 6}},
		{"all zero", []int64{0, 0, 0, 0}, 10, []int{0, 0, 0, 0}},
		{"nil parts", nil, 10, []int{}},
		{"width zero", []int64{1, 2, 3, 4}, 0, []int{0, 0, 0, 0}},
		{"negative width", []int64{1, 2, 3, 4}, -1, []int{0, 0, 0, 0}},
		{"negative part counts as 0", []int64{-5, 0, 0, 10}, 10, []int{0, 0, 0, 10}},
		{"only negative parts", []int64{-5, -1}, 10, []int{0, 0}},
		{"huge equal counters, no overflow", []int64{m, m, m, m}, 10, []int{3, 3, 2, 2}},
		{"huge and tiny", []int64{m, 1, 0, 0}, 10, []int{10, 0, 0, 0}},
		{"huge uneven", []int64{m, m / 3, 0, 0}, 10, []int{8, 2, 0, 0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := apportion(tc.parts, tc.width)
			if !slices.Equal(got, tc.want) {
				t.Errorf("apportion(%v, %d) = %v, want %v", tc.parts, tc.width, got, tc.want)
			}
		})
	}
}

func TestApportionDeterministic(t *testing.T) {
	parts := []int64{25, 25, 25, 25}
	first := apportion(parts, 10)
	for i := 0; i < 20; i++ {
		if got := apportion(parts, 10); !slices.Equal(got, first) {
			t.Fatalf("run %d: %v differs from %v", i, got, first)
		}
	}
	// Equal remainders: the larger part takes the spare cell before the lower index.
	if got, want := apportion([]int64{1, 4, 10}, 10), []int{0, 3, 7}; !slices.Equal(got, want) {
		t.Errorf("remainder tie = %v, want %v", got, want)
	}
	// A tie in remainder and size goes to the lower index, whatever the slot.
	if got, want := apportion([]int64{0, 1, 1, 0}, 3), []int{0, 2, 1, 0}; !slices.Equal(got, want) {
		t.Errorf("tie = %v, want %v", got, want)
	}
}

func TestApportionDoesNotMutateInput(t *testing.T) {
	parts := []int64{-3, 5, 0, 90}
	apportion(parts, 10)
	if !slices.Equal(parts, []int64{-3, 5, 0, 90}) {
		t.Errorf("input mutated: %v", parts)
	}
}

func TestApportionProperties(t *testing.T) {
	rng := rand.New(rand.NewSource(98))
	for i := 0; i < 200; i++ {
		n := 1 + rng.Intn(6)
		width := 1 + rng.Intn(30)
		parts := make([]int64, n)
		var total int64
		var anyPositive bool
		for j := range parts {
			switch rng.Intn(4) {
			case 0: // zero
			case 1:
				parts[j] = rng.Int63n(100)
			case 2:
				parts[j] = rng.Int63n(1_000_000)
			default:
				parts[j] = rng.Int63() / 8
			}
			total += parts[j]
			anyPositive = anyPositive || parts[j] > 0
		}
		got := apportion(parts, width)
		if len(got) != n {
			t.Fatalf("case %d: len %d, want %d", i, len(got), n)
		}
		want := 0
		if anyPositive {
			want = width
		}
		if s := cellSum(got); s != want {
			t.Fatalf("case %d: parts %v width %d: cells %v sum %d, want %d", i, parts, width, got, s, want)
		}
		for j := range parts {
			// 2*parts*width < total  <=>  share under half a cell. Divide to stay in range.
			if parts[j] > 0 && float64(parts[j])*2*float64(width) < float64(total)*(1-1e-9) && got[j] != 0 {
				// the only legal exception: nothing eligible, all cells to the largest part
				if got[j] != width {
					t.Errorf("case %d: part %d (%d of %d) under half a cell got %d", i, j, parts[j], total, got[j])
				}
			}
			for k := range parts {
				if parts[j] > parts[k] && got[j] < got[k] {
					t.Errorf("case %d: %v -> %v: larger part %d got fewer cells than %d", i, parts, got, j, k)
				}
			}
		}
	}
}

func TestSegmentCells(t *testing.T) {
	tests := []struct {
		name   string
		tokens agentusage.Tokens
		want   [segCount]int
		wantOK bool
	}{
		{"no token", agentusage.Tokens{}, [segCount]int{}, false},
		{"plan example, bar order", agentusage.Tokens{Input: 20, Output: 10, CacheWrite: 20, CacheRead: 50}, [segCount]int{2, 1, 2, 5}, true},
		{"only input", agentusage.Tokens{Input: 3}, [segCount]int{10, 0, 0, 0}, true},
		{"only output", agentusage.Tokens{Output: 3}, [segCount]int{0, 10, 0, 0}, true},
		{"only cache write", agentusage.Tokens{CacheWrite: 3}, [segCount]int{0, 0, 10, 0}, true},
		{"only cache read", agentusage.Tokens{CacheRead: 3}, [segCount]int{0, 0, 0, 10}, true},
		{"huge counters", agentusage.Tokens{Input: math.MaxInt64, Output: math.MaxInt64, CacheWrite: math.MaxInt64, CacheRead: math.MaxInt64}, [segCount]int{3, 3, 2, 2}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := segmentCells(tc.tokens)
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("segmentCells(%+v) = %v, %v; want %v, %v", tc.tokens, got, ok, tc.want, tc.wantOK)
			}
			if ok && cellSum(got[:]) != breakdownBarWidth {
				t.Errorf("cells %v sum to %d, want %d", got, cellSum(got[:]), breakdownBarWidth)
			}
		})
	}
}
