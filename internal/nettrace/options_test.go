package nettrace

import (
	"testing"
	"time"
)

func TestLimitsAreFixed(t *testing.T) {
	if MaxHopsLimit != 30 {
		t.Errorf("MaxHopsLimit = %d, want 30", MaxHopsLimit)
	}
	if DefaultProbeTimeout != 3*time.Second {
		t.Errorf("DefaultProbeTimeout = %v, want 3s", DefaultProbeTimeout)
	}
	if DeadlineLimit != 30*time.Second {
		t.Errorf("DeadlineLimit = %v, want 30s", DeadlineLimit)
	}
}

func TestOptionsNormalized(t *testing.T) {
	defaults := Options{MaxHops: 30, ProbeTimeout: 3 * time.Second, Deadline: 30 * time.Second}
	tests := []struct {
		name string
		in   Options
		want Options
	}{
		{"zero value gets every default", Options{}, defaults},
		{"negative max hops", Options{MaxHops: -1}, defaults},
		{"max hops above the limit", Options{MaxHops: 31}, defaults},
		{"max hops at the limit", Options{MaxHops: 30}, defaults},
		{"max hops of one is kept", Options{MaxHops: 1}, Options{MaxHops: 1, ProbeTimeout: 3 * time.Second, Deadline: 30 * time.Second}},
		{"negative probe timeout", Options{ProbeTimeout: -time.Second}, defaults},
		{"short probe timeout is kept", Options{ProbeTimeout: 500 * time.Millisecond}, Options{MaxHops: 30, ProbeTimeout: 500 * time.Millisecond, Deadline: 30 * time.Second}},
		{"probe timeout capped at the deadline", Options{ProbeTimeout: 10 * time.Second, Deadline: 5 * time.Second}, Options{MaxHops: 30, ProbeTimeout: 5 * time.Second, Deadline: 5 * time.Second}},
		{"default probe timeout capped at a short deadline", Options{Deadline: time.Second}, Options{MaxHops: 30, ProbeTimeout: time.Second, Deadline: time.Second}},
		{"negative deadline", Options{Deadline: -time.Second}, defaults},
		{"deadline above the limit", Options{Deadline: 31 * time.Second}, defaults},
		{"probe timeout below a clamped deadline is kept", Options{ProbeTimeout: 20 * time.Second, Deadline: time.Minute}, Options{MaxHops: 30, ProbeTimeout: 20 * time.Second, Deadline: 30 * time.Second}},
		{"probe timeout above a clamped deadline is capped", Options{ProbeTimeout: 45 * time.Second, Deadline: time.Minute}, Options{MaxHops: 30, ProbeTimeout: 30 * time.Second, Deadline: 30 * time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.Normalized()
			if got != tt.want {
				t.Errorf("Normalized() = %+v, want %+v", got, tt.want)
			}
			if again := got.Normalized(); again != got {
				t.Errorf("Normalized is not idempotent: %+v then %+v", got, again)
			}
		})
	}
}
