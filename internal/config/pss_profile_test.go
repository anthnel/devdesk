package config

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// DEVDESK-686 (S-01), criteria 1 and 6: scan.pss_profile is absent until the
// user picks a value, reads as "all" while absent, and a value outside the set
// falls back on "all" with a WARN, never silently.

func TestPSSProfileConstantsAndSet(t *testing.T) {
	if PSSProfileAll != "all" || PSSProfileBaseline != "baseline" || PSSProfileRestricted != "restricted" {
		t.Fatalf("constants: %q %q %q", PSSProfileAll, PSSProfileBaseline, PSSProfileRestricted)
	}
	want := []string{"all", "baseline", "restricted"}
	if got := PSSProfiles(); !slices.Equal(got, want) {
		t.Errorf("PSSProfiles() = %v, want %v", got, want)
	}
}

func TestPSSProfileTable(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		effective string
		valid     bool
		warn      bool
	}{
		{"absent", "", PSSProfileAll, true, false},
		{"all", "all", PSSProfileAll, true, false},
		{"baseline", "baseline", PSSProfileBaseline, true, false},
		{"restricted", "restricted", PSSProfileRestricted, true, false},
		{"strict", "strict", PSSProfileAll, false, true},
		{"Baseline is not baseline", "Baseline", PSSProfileAll, false, true},
		{"privileged is out of scope", "privileged", PSSProfileAll, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateMotionHome(t)
			buf := captureLog(t)
			cfg := Default()
			cfg.Scan.PSSProfile = tc.in
			if err := applyDefaults(cfg); err != nil {
				t.Fatalf("applyDefaults: %v", err)
			}
			if cfg.Scan.PSSProfile != tc.in {
				t.Errorf("PSSProfile = %q, want %q kept as read (never defaulted in memory)", cfg.Scan.PSSProfile, tc.in)
			}
			if got := cfg.Scan.EffectivePSSProfile(); got != tc.effective {
				t.Errorf("EffectivePSSProfile = %q, want %q", got, tc.effective)
			}
			if got := cfg.Scan.PSSProfileValid(); got != tc.valid {
				t.Errorf("PSSProfileValid = %v, want %v", got, tc.valid)
			}
			want := `WARN [config] scan.pss_profile "` + tc.in + `" is not all, baseline or restricted, using all`
			if has := strings.Contains(buf.String(), want); has != tc.warn {
				t.Errorf("log contains WARN = %v, want %v; log: %q", has, tc.warn, buf.String())
			}
			if !tc.warn && strings.Contains(buf.String(), "pss_profile") {
				t.Errorf("unexpected pss_profile log: %q", buf.String())
			}
		})
	}
}

// Criterion 1: an existing context without the key reads as "all", its file is
// not rewritten by loading, and saving it again does not add the key.
func TestAnExistingContextWithoutPSSProfileStaysAsIs(t *testing.T) {
	const body = "scan:\n  base_image_track: same-line\n"
	isolateMotionHome(t)
	captureLog(t)
	if err := EnsureConfigDir(); err != nil {
		t.Fatal(err)
	}
	path, err := GetContextPath("psstest")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadContext("psstest")
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}
	if loaded.Scan.PSSProfile != "" {
		t.Errorf("PSSProfile = %q, want empty (absent stays absent in memory)", loaded.Scan.PSSProfile)
	}
	if got := loaded.Scan.EffectivePSSProfile(); got != PSSProfileAll {
		t.Errorf("EffectivePSSProfile = %q, want all", got)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != body {
		t.Errorf("loading rewrote the file:\n%s", after)
	}

	if err := SaveContext(loaded, "psstest"); err != nil {
		t.Fatalf("SaveContext: %v", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "pss_profile") {
		t.Errorf("saving gained a pss_profile key:\n%s", saved)
	}
	if got := Default().Scan.PSSProfile; got != "" {
		t.Errorf("Default().Scan.PSSProfile = %q, want empty", got)
	}
}

// Criterion 6: a chosen value is written to the context file and read back.
func TestAChosenPSSProfileRoundTrips(t *testing.T) {
	for _, value := range []string{PSSProfileBaseline, PSSProfileRestricted} {
		t.Run(value, func(t *testing.T) {
			captureLog(t)
			loaded, reloaded, saved := roundTrip(t, "scan:\n  pss_profile: "+value+"\n")
			if loaded.Scan.PSSProfile != value || reloaded.Scan.PSSProfile != value {
				t.Errorf("PSSProfile loaded=%q reloaded=%q, want %q", loaded.Scan.PSSProfile, reloaded.Scan.PSSProfile, value)
			}
			if got := reloaded.Scan.EffectivePSSProfile(); got != value {
				t.Errorf("EffectivePSSProfile = %q, want %q", got, value)
			}
			if !strings.Contains(saved, "pss_profile: "+value) {
				t.Errorf("saved file lost pss_profile: %s:\n%s", value, saved)
			}
		})
	}
}

// Criterion 6: a hand-edited value outside the set falls back on "all" with a
// WARN, and is not rewritten behind the user's back.
func TestAnInvalidPSSProfileFallsBackOnAllWithAWarning(t *testing.T) {
	buf := captureLog(t)
	loaded, reloaded, saved := roundTrip(t, "scan:\n  pss_profile: strict\n")
	if loaded.Scan.PSSProfileValid() || reloaded.Scan.PSSProfileValid() {
		t.Error("strict must be invalid")
	}
	if got := loaded.Scan.EffectivePSSProfile(); got != PSSProfileAll {
		t.Errorf("EffectivePSSProfile = %q, want all", got)
	}
	if !strings.Contains(buf.String(), `WARN [config] scan.pss_profile "strict" is not all, baseline or restricted, using all`) {
		t.Errorf("missing WARN, log: %q", buf.String())
	}
	if !strings.Contains(saved, "pss_profile: strict") {
		t.Errorf("the invalid value was rewritten:\n%s", saved)
	}
}
