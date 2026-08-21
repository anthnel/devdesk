package docker

import "testing"

// The defect this file exists for: a dashboard reading 1400% CPU.
//
// `docker stats` reports CPU relative to **one core** — Docker computes
// (cpuDelta/systemDelta) × onlineCPUs × 100, measured at 199.62% for a container
// busy on two cores. Summed over the containers the raw total runs to
// NCPU × 100, so 1400% on a sixteen-CPU daemon is a true statement about
// fourteen cores and not a percentage of anything. The dashboard drew it beside
// the host's 0–100 figure, under the same word, on a chart whose maximum is 100.
func TestTheCPUShareIsTakenAgainstTheDaemonsCores(t *testing.T) {
	metrics := map[string]Container{
		"a": {CPUPercent: 800},
		"b": {CPUPercent: 600},
	}

	agg := aggregate(metrics, Capacity{Cores: 16, MemTotal: 16 << 30})

	if agg.CPUPercent != 87.5 {
		t.Errorf("CPUPercent = %v, want 87.5 — fourteen cores out of sixteen", agg.CPUPercent)
	}
	if agg.CPUPercent > 100 {
		t.Error("the share exceeds 100, so it is not a share")
	}
	if agg.Cores != 16 {
		t.Errorf("Cores = %d, want the denominator carried alongside the figure", agg.Cores)
	}
}

// Every core busy is 100%, and not one percent more. It is the boundary the
// chart's fixed maximum of 100 depends on.
func TestASaturatedDaemonReadsAsOneHundred(t *testing.T) {
	metrics := map[string]Container{"a": {CPUPercent: 400}, "b": {CPUPercent: 400}}

	if got := aggregate(metrics, Capacity{Cores: 8, MemTotal: 1 << 30}).CPUPercent; got != 100 {
		t.Errorf("CPUPercent = %v, want 100", got)
	}
}

// The memory half was the worse of the two, and the only one that was arithmetic
// rather than presentation: `.MemPerc` is a share of *that container's* limit,
// so two of them are fractions of different wholes. Measured on a live daemon, a
// container capped at 256MiB read 0.13% and one against the daemon's 15.18GiB
// read 0.03% — and 0.16% is a percentage of nothing.
//
// The bytes are summed instead, against what the daemon has.
func TestTheMemoryShareIgnoresThePerContainerPercentages(t *testing.T) {
	metrics := map[string]Container{
		// 0.13% of its own 256MiB limit, and 25% of the daemon's gigabyte.
		"capped": {MemBytes: 256 << 20, MemPercent: 0.13},
		// 0.03% of the daemon's total, and 25% of it here.
		"free": {MemBytes: 256 << 20, MemPercent: 0.03},
	}

	agg := aggregate(metrics, Capacity{Cores: 4, MemTotal: 1 << 30})

	if agg.MemPercent != 50 {
		t.Errorf("MemPercent = %v, want 50 — half a gigabyte of one", agg.MemPercent)
	}
	if agg.MemPercent == 0.16 {
		t.Error("the per-container percentages were summed, which adds fractions of different wholes")
	}
}

// A daemon with nothing running is a daemon that is available and idle. It is
// not the same answer as one that could not be read, and the dashboard says the
// two differently.
func TestAnIdleDaemonIsAvailable(t *testing.T) {
	agg := aggregate(map[string]Container{}, Capacity{Cores: 8, MemTotal: 1 << 30})

	if !agg.Available || agg.Running != 0 {
		t.Errorf("aggregate = %+v, want available with nothing running", agg)
	}
	if agg.CPUPercent != 0 || agg.MemPercent != 0 {
		t.Errorf("an idle daemon reports %v%% CPU and %v%% RAM", agg.CPUPercent, agg.MemPercent)
	}
}

// A capacity is a denominator, so a zero in it is not a small number — it is the
// absence of one. Reporting the raw sums under a percent sign would be the
// original defect with an extra step.
func TestACapacityWithAZeroIsNotADenominator(t *testing.T) {
	cases := []struct {
		name string
		cap  Capacity
	}{
		{"nothing read at all", Capacity{}},
		{"no cores", Capacity{Cores: 0, MemTotal: 1 << 30}},
		{"no memory", Capacity{Cores: 8, MemTotal: 0}},
		{"a negative count", Capacity{Cores: -1, MemTotal: 1 << 30}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cap.OK() {
				t.Errorf("%+v passes as a denominator", tc.cap)
			}
		})
	}

	if !(Capacity{Cores: 1, MemTotal: 1}).OK() {
		t.Error("a capacity with both halves read does not pass")
	}
}

// The parsing that feeds the memory share. MemUsage is Docker's one binary-unit
// column, and parseSize returned 0 for it until this change — which put a zero
// in the numerator of every container and, had the total come from the same
// place, in the denominator too.
func TestMemUsageIsParsedIntoBytes(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"5.324MiB / 15.18GiB", 5_582_618},
		{"344KiB / 256MiB", 352_256},
		{"0B / 0B", 0},
		{"", 0},
	}

	for _, tc := range cases {
		if got, _ := parsePair(tc.in); got != tc.want {
			t.Errorf("parsePair(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
