package enginecontract_test

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09b1 (DEVDESK-807): the parameterised Comparer behind SameUpToDurations and
// SameUpToDurationsAndHealth, and the timestamps it can leave out. `ps` prints the
// CreatedAt of a container and `network ls` the one of a network as Go prints a
// time.Time ("2026-11-27 12:00:00 +0000 UTC"), podman may print a fraction and a
// monotonic reading, and an inspect prints RFC 3339.
//
//	type Tolerance uint8 // Durations, Timestamps, Health
//	func Tolerating(kinds ...Tolerance) Comparer // no kind: an exact comparison
//
// SameUpToDurations is Tolerating(Durations), SameUpToDurationsAndHealth is
// Tolerating(Durations, Health); compare_test.go holds what they accept and refuse.

func ts(row string) string { return "a1b2c3d4e5f6\tweb\talpine\trunning\t" + row + "\n" }

func TestTolerantComparerLeavesOutTimestampsAndOnlyWhenAsked(t *testing.T) {
	const (
		goTime  = "2026-11-27 12:00:00 +0000 UTC"
		goOther = "2026-12-01 03:04:05 +0000 UTC"
	)
	tests := []struct {
		name          string
		recorded, got string
	}{
		{"the CreatedAt of ps", ts(goTime), ts(goOther)},
		{"a fraction", ts("2026-11-27 12:00:00.123456789 +0000 UTC"), ts(goOther)},
		{"a monotonic reading", ts("2026-11-27 12:00:00.5 +0000 UTC m=+0.012345678"), ts(goOther)},
		{"another zone", ts("2026-11-27 13:00:00 +0100 CET"), ts(goOther)},
		{"a zone with no name", ts("2026-11-27 13:00:00 +0300 +03"), ts(goOther)},
		{"no zone name", ts("2026-11-27 12:00:00 +0000"), ts(goOther)},
		{"RFC 3339", ts("2026-11-27T12:00:00Z"), ts("2026-12-01T03:04:05.987654321+01:00")},
		{"RFC 3339 against the Go form", ts("2026-11-27T12:00:00Z"), ts(goOther)},
		{"in a line of the default table", "web  " + goTime + "  Up\n", "web  " + goOther + "  Up\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, got := streams(tt.recorded, ""), streams(tt.got, "")
			cmp := enginecontract.Tolerating(enginecontract.Timestamps)

			if err := cmp(rec, got); err != nil {
				t.Errorf("Tolerating(Timestamps)(%q, %q) = %v, want nil", tt.recorded, tt.got, err)
			}
			if err := cmp(got, rec); err != nil {
				t.Errorf("with the exchanges swapped = %v, want nil", err)
			}
			if tt.recorded != tt.got {
				if err := enginecontract.Tolerating(enginecontract.Durations, enginecontract.Health)(rec, got); err == nil {
					t.Errorf("Tolerating(Durations, Health)(%q, %q) = nil, want a difference: a timestamp is not tolerated unless asked", tt.recorded, tt.got)
				}
				if err := enginecontract.Tolerating()(rec, got); err == nil {
					t.Errorf("Tolerating()(%q, %q) = nil, want an exact comparison", tt.recorded, tt.got)
				}
			}
		})
	}
}

func TestTolerantComparerStillSeesWhatIsNotATimestamp(t *testing.T) {
	tests := []struct {
		name          string
		recorded, got string
	}{
		{"a date with no time", ts("2026-11-27"), ts("2026-12-01")},
		{"a time of day with no date", ts("12:00:00"), ts("12:00:01")},
		{"a version", ts("v2026.11.27"), ts("v2026.12.01")},
		{"a timestamp and none", ts("2026-11-27 12:00:00 +0000 UTC"), ts("")},
		{"a name next to a timestamp", "web\t2026-11-27 12:00:00 +0000 UTC\n", "db\t2026-11-27 12:00:00 +0000 UTC\n"},
		{"a word after a timestamp with no zone name", "2026-11-27 12:00:00 +0000 Up\n", "2026-11-27 12:00:00 +0000 Down\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enginecontract.Tolerating(enginecontract.Durations, enginecontract.Timestamps, enginecontract.Health)(streams(tt.recorded, ""), streams(tt.got, ""))

			if err == nil {
				t.Errorf("Tolerating(all)(%q, %q) = nil, want a difference", tt.recorded, tt.got)
			}
		})
	}
}

// The kinds are independent, and the order they are given in does not matter.
func TestTolerantComparerCombinesItsKinds(t *testing.T) {
	psRow := func(created, status string) string { return "web\talpine\trunning\t" + status + "\t" + created + "\n" }
	rec := streams(psRow("2026-11-27 12:00:00 +0000 UTC", "Up 3 seconds (healthy)"), "")
	got := streams(psRow("2026-11-27 12:05:00 +0000 UTC", "Up 5 minutes (health: starting)"), "")
	tests := []struct {
		name  string
		kinds []enginecontract.Tolerance
		ok    bool
	}{
		{"all three", []enginecontract.Tolerance{enginecontract.Durations, enginecontract.Timestamps, enginecontract.Health}, true},
		{"all three, another order", []enginecontract.Tolerance{enginecontract.Health, enginecontract.Timestamps, enginecontract.Durations}, true},
		{"the same kind twice", []enginecontract.Tolerance{enginecontract.Durations, enginecontract.Durations, enginecontract.Timestamps, enginecontract.Health}, true},
		{"no health", []enginecontract.Tolerance{enginecontract.Durations, enginecontract.Timestamps}, false},
		{"no timestamps", []enginecontract.Tolerance{enginecontract.Durations, enginecontract.Health}, false},
		{"no durations", []enginecontract.Tolerance{enginecontract.Timestamps, enginecontract.Health}, false},
		{"nothing", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enginecontract.Tolerating(tt.kinds...)(rec, got)

			if tt.ok && err != nil {
				t.Errorf("Tolerating(%v) = %v, want nil", tt.kinds, err)
			}
			if !tt.ok && err == nil {
				t.Errorf("Tolerating(%v) = nil, want a difference", tt.kinds)
			}
		})
	}
}

func TestTolerantComparerDoesNotChangeItsInputs(t *testing.T) {
	rec, got := streams(ts("2026-11-27 12:00:00 +0000 UTC"), "x 3 seconds ago\n"), streams(ts("2026-11-28 12:00:00 +0000 UTC"), "x 4 seconds ago\n")

	if err := enginecontract.Tolerating(enginecontract.Durations, enginecontract.Timestamps)(rec, got); err != nil {
		t.Fatalf("comparer = %v, want nil", err)
	}

	if string(rec.Stdout) != ts("2026-11-27 12:00:00 +0000 UTC") || string(got.Stderr) != "x 4 seconds ago\n" {
		t.Errorf("the exchanges changed: %q %q, %q %q", rec.Stdout, rec.Stderr, got.Stdout, got.Stderr)
	}
}

// A kind the package does not know is a mistake of the contract: it panics when the
// Comparer is made, not when a run differs.
func TestTolerantComparerPanicsOnAKindItDoesNotKnow(t *testing.T) {
	for _, kind := range []enginecontract.Tolerance{0, 1 << 5, 255} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("Tolerating(%d) did not panic", kind)
				}
			}()
			enginecontract.Tolerating(kind)
		}()
	}
}

// The error names the stream that differs, with the tolerated values replaced.
func TestTolerantComparerReportsBothStreamsWhenBothDiffer(t *testing.T) {
	rec := enginefixture.Exchange{Stdout: []byte("a 3 seconds ago\n"), Stderr: []byte("b\n")}
	got := enginefixture.Exchange{Stdout: []byte("c 4 seconds ago\n"), Stderr: []byte("d\n")}

	err := enginecontract.Tolerating(enginecontract.Durations)(rec, got)

	if err == nil || !strings.Contains(err.Error(), "stdout") || !strings.Contains(err.Error(), "stderr") {
		t.Errorf("error = %v, want it to name both streams", err)
	}
}
