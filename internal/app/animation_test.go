package app

import (
	"testing"
	"time"
)

func TestThePhaseIsReadFromTheTickTime(t *testing.T) {
	tests := []struct {
		name string
		ms   int64
		want bool
	}{
		{"epoch is on", 0, true},
		{"last ms of the first half is on", 499, true},
		{"first ms of the second half is off", 500, false},
		{"last ms of the second half is off", 999, false},
		{"next period starts on", 1000, true},
		{"large timestamp, first half", 1_700_000_000_250, true},
		{"large timestamp, second half", 1_700_000_000_750, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := blinkPhase(time.UnixMilli(tt.ms)); got != tt.want {
				t.Errorf("blinkPhase(%d ms) = %v, want %v", tt.ms, got, tt.want)
			}
		})
	}
}

func TestTheCadence(t *testing.T) {
	tests := []struct {
		name                        string
		running, wantsBlink, static bool
		want                        time.Duration
		wantOK                      bool
	}{
		{"running alone is fast", true, false, false, animationFastInterval, true},
		{"running and blinking is fast", true, true, false, animationFastInterval, true},
		{"running with static motion is fast", true, false, true, animationFastInterval, true},
		{"running, blinking, static is fast", true, true, true, animationFastInterval, true},
		{"only blinking is slow", false, true, false, animationBlinkInterval, true},
		{"blinking under static motion is none", false, true, true, 0, false},
		{"nothing wanted is none", false, false, false, 0, false},
		{"nothing wanted, static is none", false, false, true, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := cadence(tt.running, tt.wantsBlink, tt.static)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("cadence(%v, %v, %v) = (%v, %v), want (%v, %v)",
					tt.running, tt.wantsBlink, tt.static, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestTheIntervalsMatchTheSpec(t *testing.T) {
	if animationFastInterval != 100*time.Millisecond {
		t.Errorf("fast interval = %v, want 100ms", animationFastInterval)
	}
	if animationBlinkInterval != 500*time.Millisecond {
		t.Errorf("blink interval = %v, want 500ms", animationBlinkInterval)
	}
}
