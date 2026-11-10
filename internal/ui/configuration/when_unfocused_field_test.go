package configuration

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-210 step 6: the "When unfocused" cycle field in the Appearance group.

func whenUnfocusedModel(t *testing.T, v string) Model {
	t.Helper()
	isolateMotionHome(t)
	m := newModel(t)
	m.config.App.WhenUnfocused = v
	return focusOn(t, m, whenUnfocusedLabel)
}

func TestWhenUnfocusedIsACycleFieldInTheAppearanceGroup(t *testing.T) {
	f := fieldNamed(t, whenUnfocusedLabel)

	if whenUnfocusedLabel != "When unfocused" {
		t.Errorf("whenUnfocusedLabel = %q, want When unfocused", whenUnfocusedLabel)
	}
	if f.Kind != kindCycle {
		t.Errorf("kind = %v, want kindCycle (Rule 132)", f.Kind)
	}
	if f.Group != fieldNamed(t, "Gauge glyph").Group {
		t.Errorf("group = %q, want the group of Gauge glyph (Appearance)", f.Group)
	}
	want := config.WhenUnfocusedModes()
	if strings.Join(f.Options, ",") != strings.Join(want, ",") {
		t.Errorf("options = %v, want %v", f.Options, want)
	}
}

func TestArrowsWriteWhenUnfocusedAndEmitConfigSavedMsg(t *testing.T) {
	for _, tt := range []struct {
		name, start, key, want string
	}{
		{"pause right", config.WhenUnfocusedPause, "right", config.WhenUnfocusedRun},
		{"run right wraps", config.WhenUnfocusedRun, "right", config.WhenUnfocusedPause},
		{"pause left wraps", config.WhenUnfocusedPause, "left", config.WhenUnfocusedRun},
		{"run left", config.WhenUnfocusedRun, "left", config.WhenUnfocusedPause},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := whenUnfocusedModel(t, tt.start)
			updated, cmd := m.Update(testutil.Key(tt.key))
			next := updated.(Model)

			if got := next.config.App.WhenUnfocused; got != tt.want {
				t.Fatalf("WhenUnfocused = %q, want %q", got, tt.want)
			}
			msg, ok := testutil.MsgOf[ConfigSavedMsg](cmd)
			if !ok {
				t.Fatal("no ConfigSavedMsg; the router never applies the new setting")
			}
			if msg.Config.App.WhenUnfocused != tt.want {
				t.Errorf("ConfigSavedMsg carries %q, want %q", msg.Config.App.WhenUnfocused, tt.want)
			}
		})
	}
}

func TestSpaceAndEnterDoNothingOnWhenUnfocused(t *testing.T) {
	for _, key := range []string{"space", "enter"} {
		t.Run(key, func(t *testing.T) {
			m := whenUnfocusedModel(t, config.WhenUnfocusedPause)
			updated, cmd := m.Update(testutil.Key(key))
			next := updated.(Model)

			if next.config.App.WhenUnfocused != config.WhenUnfocusedPause {
				t.Errorf("%s changed the setting to %q (Rule 135)", key, next.config.App.WhenUnfocused)
			}
			if _, ok := testutil.MsgOf[ConfigSavedMsg](cmd); ok {
				t.Errorf("%s saved the configuration", key)
			}
		})
	}
}

func TestAbsentWhenUnfocusedShowsTheDefaultAndStaysAbsent(t *testing.T) {
	m := whenUnfocusedModel(t, "")

	if got := fieldNamed(t, whenUnfocusedLabel).Value(m.config); got != "pause (default)" {
		t.Errorf("Value = %q, want %q", got, "pause (default)")
	}
	if !strings.Contains(plainRows(m), "pause (default)") {
		t.Error("the row does not read 'pause (default)'")
	}
	if m.config.App.WhenUnfocused != "" {
		t.Error("displaying the default wrote it")
	}

	next := feed(t, m, testutil.Key("right"))
	if next.config.App.WhenUnfocused != config.WhenUnfocusedRun {
		t.Fatalf("first → gave %q, want run", next.config.App.WhenUnfocused)
	}
	next = feed(t, next, testutil.Key("right"))
	if next.config.App.WhenUnfocused != config.WhenUnfocusedPause {
		t.Fatalf("second → gave %q, want an explicit pause", next.config.App.WhenUnfocused)
	}
	if got := fieldNamed(t, whenUnfocusedLabel).Value(next.config); got != "pause" {
		t.Errorf("explicit pause reads %q, want plain pause", got)
	}
}
