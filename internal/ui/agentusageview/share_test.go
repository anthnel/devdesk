package agentusageview

import (
	"math"
	"testing"
)

func TestFloorTenths(t *testing.T) {
	tests := []struct {
		name        string
		part, total int64
		want        int
	}{
		{"one third", 1, 3, 333},
		{"two thirds", 2, 3, 666},
		{"everything", 3, 3, 1000},
		{"nothing", 0, 5, 0},
		{"saturated whole", math.MaxInt64, math.MaxInt64, 1000},
		{"saturated half", math.MaxInt64 / 2, math.MaxInt64, 499},
		{"part above total is clamped", 7, 3, 1000},
		{"negative part is clamped", -4, 3, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := floorTenths(tt.part, tt.total); got != tt.want {
				t.Errorf("floorTenths(%d, %d) = %d, want %d", tt.part, tt.total, got, tt.want)
			}
		})
	}
}

func sum(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}

func TestShares(t *testing.T) {
	tests := []struct {
		name    string
		totals  []int64
		total   int64
		mainIdx int
		want    []int
	}{
		{"exact parts", []int64{600, 300, 100}, 1000, 0, []int{600, 300, 100}},
		{"residue goes to the orchestrator", []int64{1, 1, 1}, 3, 0, []int{334, 333, 333}},
		{"a tiny sub-agent floors to zero", []int64{999999, 1}, 1_000_000, 0, []int{1000, 0}},
		{"the orchestrator absorbs even with no tokens", []int64{0, 2, 1}, 3, 0, []int{1, 666, 333}},
		{"zero total means no share", []int64{0, 0}, 0, 0, []int{noShare, noShare}},
		{"no orchestrator: every part floored", []int64{1, 1, 1}, 3, -1, []int{333, 333, 333}},
		{"saturated totals never go negative", []int64{math.MaxInt64, math.MaxInt64, math.MaxInt64}, math.MaxInt64, 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shares(tt.totals, tt.total, tt.mainIdx)
			if len(got) != len(tt.totals) {
				t.Fatalf("got %d shares, want %d", len(got), len(tt.totals))
			}
			if tt.want == nil { // saturated: only the invariant on the orchestrator is asserted
				if got[0] != 0 {
					t.Errorf("orchestrator share = %d, want 0 (clamped residue)", got[0])
				}
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("shares[%d] = %d, want %d (all: %v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

// Property: without saturation and with an orchestrator, the parts add up to
// exactly shareScale.
func TestSharesAlwaysAddUpToTheScale(t *testing.T) {
	cases := [][]int64{
		{1, 1, 1}, {7, 3, 5, 11}, {0, 0, 9}, {1000, 1}, {5, 5, 5, 5, 5, 5, 5}, {13, 0, 0, 0},
		{123456789, 987654321, 5, 77},
	}
	for _, totals := range cases {
		var total int64
		for _, x := range totals {
			total += x
		}
		if got := sum(shares(totals, total, 0)); got != shareScale {
			t.Errorf("shares(%v) add up to %d, want %d", totals, got, shareScale)
		}
	}
}

func TestFormatShare(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0.0%"},
		{5, "0.5%"},
		{123, "12.3%"},
		{1000, "100.0%"},
		{noShare, "-"},
	}
	for _, tt := range tests {
		if got := formatShare(tt.in); got != tt.want {
			t.Errorf("formatShare(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
