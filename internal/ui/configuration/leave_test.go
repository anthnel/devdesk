package configuration

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The defect itself: a value typed and then abandoned by leaving the view was
// never written, and the cached view went on showing it (§1.3 D62).
func TestLeavingTheViewWritesTheFocusedField(t *testing.T) {
	m := focusOn(t, newModel(t), "Plumber config")
	m.input.SetValue("/etc/.plumber.yaml")

	left, _, ok := m.Leave()
	if !ok {
		t.Fatal("Leave() refused an acceptable value")
	}
	if got := left.(Model).config.Scan.PlumberConfig; got != "/etc/.plumber.yaml" {
		t.Errorf("PlumberConfig = %q, want the typed value written on the way out", got)
	}
}

// The forge URL is the worst case: leaving without committing it left a session
// open against an address the user believed they had changed, and without the
// message that says to sign in again.
func TestLeavingTheViewCarriesTheForgeURLConsequences(t *testing.T) {
	m := focusOn(t, newModel(t), "URL")
	m.input.SetValue("https://gitlab.example.com")

	left, cmd, ok := m.Leave()
	if !ok {
		t.Fatal("Leave() refused an acceptable URL")
	}
	if got := left.(Model).config.Forge.URL; got != "https://gitlab.example.com" {
		t.Errorf("Forge.URL = %q, want the typed value", got)
	}
	saved, found := testutil.MsgOf[ConfigSavedMsg](cmd)
	if !found {
		t.Fatal("no ConfigSavedMsg; the router is never told")
	}
	if !saved.ForgeChanged {
		t.Error("ForgeChanged is false, so the router keeps a session pointed at the old host")
	}
}

// A refusal keeps the screen. It is the only answer that can be said out loud:
// the footer belongs to this view, so a message posted on the way out would
// leave with the view that posted it.
func TestARefusedValueRefusesToBeLeft(t *testing.T) {
	m := focusOn(t, newModel(t), "Parallel jobs")
	m.config.Forge.Pull.ParallelJobs = 4
	m.input.SetValue("not-a-number")

	left, _, ok := m.Leave()
	if ok {
		t.Fatal("Leave() allowed the switch despite a refused value")
	}
	kept := left.(Model)
	if got := kept.config.Forge.Pull.ParallelJobs; got != 4 {
		t.Errorf("ParallelJobs = %d, want the old value kept", got)
	}
	if !kept.footer.IsSet() {
		t.Error("nothing was reported to the user")
	}
}

// esc fell through to the input, which ignores it — so the one gesture tried to
// settle a field was the one that settled nothing.
func TestEscapeWritesTheFocusedFieldWithoutMoving(t *testing.T) {
	m := focusOn(t, newModel(t), "Plumber config")
	was := m.focusedField
	m.input.SetValue("/etc/.plumber.yaml")

	m = feed(t, m, testutil.Key("esc"))

	if m.config.Scan.PlumberConfig != "/etc/.plumber.yaml" {
		t.Errorf("PlumberConfig = %q, want the typed value written by esc", m.config.Scan.PlumberConfig)
	}
	if m.focusedField != was {
		t.Errorf("focus moved to %d; esc saves without moving", m.focusedField)
	}
}

// An integer field normalises what it is given, and the input has to show what
// was stored rather than what was typed — telling a written value from a merely
// typed one is the other half of what made D62 durable.
func TestEscapeShowsWhatWasStoredRatherThanWhatWasTyped(t *testing.T) {
	m := focusOn(t, newModel(t), "Parallel jobs")
	m.input.SetValue("007")

	m = feed(t, m, testutil.Key("esc"))

	if m.config.Forge.Pull.ParallelJobs != 7 {
		t.Fatalf("ParallelJobs = %d, want 7", m.config.Forge.Pull.ParallelJobs)
	}
	if got := m.input.Value(); got != "7" {
		t.Errorf("input shows %q, want the stored %q", got, "7")
	}
}

// Rule 130: greyed means it does nothing, and it does nothing exactly where it
// is greyed. A checkbox and an ordinary cycle field have already written.
func TestEscapeIsOfferedOnlyWhereItSettlesSomething(t *testing.T) {
	cases := []struct {
		label string
		want  bool
	}{
		{"Plumber config", true},
		{"Parallel jobs", true},
		{"Forge", true},
		{"Secret backend", true},
		{"Theme", false},
		{"Scan git history", false},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			m := focusOn(t, newModel(t), tc.label)
			if got := testutil.ShortcutEnabled(m.GetShortcuts(), "esc"); got != tc.want {
				t.Errorf("esc enabled = %v on %q, want %v", got, tc.label, tc.want)
			}
			if got := m.settlesOnBlur(m.current()); got != tc.want {
				t.Errorf("settlesOnBlur = %v on %q, want %v", got, tc.label, tc.want)
			}
		})
	}
}
