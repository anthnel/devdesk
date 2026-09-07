package metrics

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Rate computation is pure: no Docker, no network, no terminal.
// It is also the place where a wrong answer shows the least on screen — a
// plausible but wrong rate looks just like a rate.

func at(seconds int) time.Time {
	return time.Date(2026, 8, 14, 12, 0, seconds, 0, time.UTC)
}

// TestTheFirstSampleHasNoRate — net.IOCounters is cumulative, so the first
// reading has nothing to subtract from. The view must display `-`, not `0`.
func TestTheFirstSampleHasNoRate(t *testing.T) {
	cur := Counters{RX: 1000, TX: 500, At: at(1), Valid: true}

	if _, _, ok := rate(Counters{}, cur); ok {
		t.Error("a counter read once produced a rate — there is nothing to subtract from")
	}
}

func TestARateIsBytesPerSecond(t *testing.T) {
	prev := Counters{RX: 1000, TX: 500, At: at(0), Valid: true}
	cur := Counters{RX: 3000, TX: 1500, At: at(2), Valid: true}

	rx, tx, ok := rate(prev, cur)
	if !ok {
		t.Fatal("two valid readings two seconds apart produced no rate")
	}
	if rx != 1000 || tx != 500 {
		t.Errorf("rate = %.0f RX / %.0f TX, want 1000 / 500", rx, tx)
	}
}

// TestARateSurvivesACounterReset — a reset interface makes the counter go
// backward. Doing the subtraction as-is would give a negative rate; the
// sample is dropped instead.
func TestARateSurvivesACounterReset(t *testing.T) {
	prev := Counters{RX: 9000, TX: 9000, At: at(0), Valid: true}
	reset := Counters{RX: 12, TX: 8, At: at(1), Valid: true}

	rx, tx, ok := rate(prev, reset)
	if ok {
		t.Errorf("a counter that went backwards produced a rate of %.0f / %.0f", rx, tx)
	}
}

func TestARateNeedsTimeToHavePassed(t *testing.T) {
	prev := Counters{RX: 1000, At: at(1), Valid: true}

	if _, _, ok := rate(prev, Counters{RX: 2000, At: at(1), Valid: true}); ok {
		t.Error("two readings at the same instant produced a rate — that is a division by zero")
	}
	if _, _, ok := rate(prev, Counters{RX: 2000, At: at(0), Valid: true}); ok {
		t.Error("a clock that went backwards produced a rate")
	}
}

// TestAnUnreadableCounterYieldsNoRate — readNetCounters returns an invalid
// Counters when the host cannot be read, and an invalid reading is not a zero
// reading.
func TestAnUnreadableCounterYieldsNoRate(t *testing.T) {
	prev := Counters{RX: 1000, At: at(0), Valid: true}

	if _, _, ok := rate(prev, Counters{At: at(1)}); ok {
		t.Error("an unreadable counter produced a rate")
	}
}

// TestLoadAverageIsDisplayedNowhere pins the Windows trap: load.Avg() returns
// {0, 0, 0} with a nil error there, so it does not fail — it produces a number
// that reads as "idle". This test exists so nobody adds it back on the
// grounds that it "works on Linux".
// It checks the *imports*, not the text: the comments that explain why load
// average is excluded need to be able to name it.
// A grep would forbid its own justification.
func TestLoadAverageIsDisplayedNowhere(t *testing.T) {
	const banned = "gopsutil/v4/load"

	fset := token.NewFileSet()
	for _, root := range []string{".", filepath.Join("..", "ui", "dashboard")} {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("reading %s: %v", root, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			path := filepath.Join(root, e.Name())
			file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parsing %s: %v", path, err)
			}
			for _, imp := range file.Imports {
				if strings.Contains(imp.Path.Value, banned) {
					t.Errorf("%s imports %s — load average is displayed on no platform, "+
						"because on Windows it returns {0,0,0} with a nil error and reads as idle",
						path, banned)
				}
			}
		}
	}
}

// SampleHost talks to the host, so this asserts only what holds on any machine:
// it returns without panicking, and the counters it hands back are what the
// next call must be given.
func TestSampleHostReturnsTheCountersForTheNextCall(t *testing.T) {
	first, counters := SampleHost(Counters{})

	if first.HasRate {
		t.Error("the first sample reported a rate")
	}
	if first.Taken.IsZero() {
		t.Error("the sample is not stamped, so its age cannot be shown")
	}
	if !counters.Valid {
		t.Skip("this host exposes no network counters; the rate path cannot be exercised here")
	}

	second, _ := SampleHost(counters)
	if !second.HasRate {
		t.Error("a second sample against valid counters produced no rate")
	}
	if second.NetRXPerSec < 0 || second.NetTXPerSec < 0 {
		t.Errorf("a negative throughput reached the caller: %.0f / %.0f", second.NetRXPerSec, second.NetTXPerSec)
	}
}

func TestDiskReportsFreeSpaceOrSaysItCouldNot(t *testing.T) {
	usage := Disk(t.TempDir())
	if !usage.OK {
		t.Skip("the temp directory's filesystem could not be read")
	}
	if usage.Total == 0 {
		t.Error("a readable filesystem reported a total of zero")
	}
	if usage.Free > usage.Total {
		t.Errorf("free %d exceeds total %d", usage.Free, usage.Total)
	}
}

// A path that does not exist must report OK=false rather than a zeroed usage
// that reads as a full disk.
func TestAnUnreadablePathIsNotAFullDisk(t *testing.T) {
	usage := Disk(filepath.Join(t.TempDir(), "no", "such", "place"))

	if usage.OK {
		t.Error("a missing path reported a usable reading")
	}
	if usage.UsedPercent != 0 || usage.Total != 0 {
		t.Errorf("a failed reading carries numbers: %+v", usage)
	}
}
