package config

import "testing"

// The setting is written on load so the configuration view, which cycles a
// closed set, has a value to start from.
func TestTheBaseImageTrackDefaultsToSameLine(t *testing.T) {
	cfg := &Config{}
	if err := applyDefaults(cfg); err != nil {
		t.Fatalf("applyDefaults: %v", err)
	}
	if cfg.Scan.BaseImageTrack != BaseImageTrackSameLine {
		t.Errorf("BaseImageTrack = %q, want %q", cfg.Scan.BaseImageTrack, BaseImageTrackSameLine)
	}
	if Default().Scan.BaseImageTrack != BaseImageTrackSameLine {
		t.Errorf("Default() BaseImageTrack = %q, want %q", Default().Scan.BaseImageTrack, BaseImageTrackSameLine)
	}
}

// A chosen value is kept, and a value outside the set is not rewritten behind
// the user's back — remediation reads it as same-line.
func TestAConfiguredBaseImageTrackIsKept(t *testing.T) {
	for _, value := range []string{BaseImageTrackNextMajor, "typo"} {
		cfg := &Config{Scan: ScanConfig{BaseImageTrack: value}}
		if err := applyDefaults(cfg); err != nil {
			t.Fatalf("applyDefaults: %v", err)
		}
		if cfg.Scan.BaseImageTrack != value {
			t.Errorf("BaseImageTrack = %q, want %q kept", cfg.Scan.BaseImageTrack, value)
		}
	}
}
