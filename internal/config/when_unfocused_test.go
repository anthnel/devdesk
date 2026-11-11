package config

import (
	"os"
	"strings"
	"testing"
)

// DEVDESK-210 step 6: app.when_unfocused, "pause" (default) or "run".

func TestWhenUnfocusedTable(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		effective string
		runs      bool
		valid     bool
		warn      bool
	}{
		{"absent", "", WhenUnfocusedPause, false, true, false},
		{"pause", "pause", WhenUnfocusedPause, false, true, false},
		{"run", "run", WhenUnfocusedRun, true, true, false},
		{"unknown", "sleep", WhenUnfocusedPause, false, false, true},
		{"case matters", "Run", WhenUnfocusedPause, false, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateMotionHome(t)
			buf := captureLog(t)
			cfg := Default()
			cfg.App.WhenUnfocused = tc.in
			if err := applyDefaults(cfg); err != nil {
				t.Fatalf("applyDefaults: %v", err)
			}
			if cfg.App.WhenUnfocused != tc.in {
				t.Errorf("WhenUnfocused = %q, want %q (kept as read)", cfg.App.WhenUnfocused, tc.in)
			}
			if got := cfg.App.EffectiveWhenUnfocused(); got != tc.effective {
				t.Errorf("EffectiveWhenUnfocused = %q, want %q", got, tc.effective)
			}
			if got := cfg.App.RunsWhenUnfocused(); got != tc.runs {
				t.Errorf("RunsWhenUnfocused = %v, want %v", got, tc.runs)
			}
			if got := cfg.App.WhenUnfocusedValid(); got != tc.valid {
				t.Errorf("WhenUnfocusedValid = %v, want %v", got, tc.valid)
			}
			want := `WARN [config] app.when_unfocused "` + tc.in + `" is not pause or run, pausing`
			if got := strings.Contains(buf.String(), want); got != tc.warn {
				t.Errorf("WARN logged = %v, want %v; log: %q", got, tc.warn, buf.String())
			}
		})
	}
}

func TestWhenUnfocusedConstantsAndSet(t *testing.T) {
	if WhenUnfocusedPause != "pause" || WhenUnfocusedRun != "run" || DefaultWhenUnfocused != WhenUnfocusedPause {
		t.Fatalf("constants: %q %q %q", WhenUnfocusedPause, WhenUnfocusedRun, DefaultWhenUnfocused)
	}
	got := WhenUnfocusedModes()
	if len(got) != 2 || got[0] != WhenUnfocusedPause || got[1] != WhenUnfocusedRun {
		t.Errorf("WhenUnfocusedModes() = %v, want [pause run]", got)
	}
}

func TestWhenUnfocusedDefaultDoesNotSetItself(t *testing.T) {
	isolateMotionHome(t)
	cfg := Default()
	if cfg.App.RunsWhenUnfocused() {
		t.Error("Default() must pause when unfocused")
	}
	if cfg.App.WhenUnfocused != "" {
		t.Errorf("Default().App.WhenUnfocused = %q, want empty (absent stays absent)", cfg.App.WhenUnfocused)
	}
}

func TestWhenUnfocusedValidValueRoundTrips(t *testing.T) {
	loaded, reloaded, saved := roundTrip(t, "app:\n  when_unfocused: run\n")
	if !loaded.App.RunsWhenUnfocused() || !reloaded.App.RunsWhenUnfocused() {
		t.Error("run must survive load, save and reload")
	}
	if !strings.Contains(saved, "when_unfocused: run") {
		t.Errorf("saved file lost when_unfocused: run:\n%s", saved)
	}
}

func TestWhenUnfocusedAbsentKeyStaysAbsentOnSave(t *testing.T) {
	loaded, reloaded, saved := roundTrip(t, "app:\n  gauge_glyph: block\n")
	if loaded.App.WhenUnfocused != "" || reloaded.App.WhenUnfocused != "" {
		t.Errorf("WhenUnfocused = %q / %q, want empty", loaded.App.WhenUnfocused, reloaded.App.WhenUnfocused)
	}
	if strings.Contains(saved, "when_unfocused") {
		t.Errorf("saved file gained a when_unfocused key:\n%s", saved)
	}
}

func TestWhenUnfocusedInvalidValueSurvivesLoadAndSave(t *testing.T) {
	buf := captureLog(t)
	loaded, reloaded, _ := roundTrip(t, "app:\n  when_unfocused: sleep\n")
	if loaded.App.WhenUnfocused != "sleep" || reloaded.App.WhenUnfocused != "sleep" {
		t.Errorf("loaded=%q reloaded=%q, want sleep kept", loaded.App.WhenUnfocused, reloaded.App.WhenUnfocused)
	}
	if loaded.App.WhenUnfocusedValid() || loaded.App.RunsWhenUnfocused() {
		t.Error("an invalid value must be invalid and pause")
	}
	if !strings.Contains(buf.String(), `WARN [config] app.when_unfocused "sleep" is not pause or run, pausing`) {
		t.Errorf("missing WARN, log: %q", buf.String())
	}
}

func TestWhenUnfocusedListFailsTheLoad(t *testing.T) {
	isolateMotionHome(t)
	if err := EnsureConfigDir(); err != nil {
		t.Fatal(err)
	}
	path, err := GetContextPath("wutest")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, "app:\n  when_unfocused: [run]\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadContext("wutest"); err == nil {
		t.Error("a list for app.when_unfocused must make the load fail, like the other app.* keys")
	}
}

func writeFile(path, body string) error { return os.WriteFile(path, []byte(body), 0o600) }
