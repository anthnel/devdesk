package configuration

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-141, step 3: the Motion cycle field in the Appearance group, its
// footer warning for a value outside the set, and its help section.

func isolateMotionHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

// motionModel is the config view focused on Motion with app.motion set to v.
func motionModel(t *testing.T, v string) Model {
	t.Helper()
	isolateMotionHome(t)
	m := newModel(t)
	m.config.App.Motion = v
	return focusOn(t, m, motionLabel)
}

func plainFooter(m Model) string { return ansi.Strip(m.RenderFooter(120)) }

func plainRows(m Model) string { return ansi.Strip(m.Render()) }

func TestMotionIsACycleFieldInTheAppearanceGroup(t *testing.T) {
	f := fieldNamed(t, motionLabel)

	if motionLabel != "Motion" {
		t.Errorf("motionLabel = %q, want Motion", motionLabel)
	}
	if f.Kind != kindCycle {
		t.Errorf("kind = %v, want kindCycle (Rule 132)", f.Kind)
	}
	if f.Group != fieldNamed(t, "Gauge glyph").Group {
		t.Errorf("group = %q, want the group of Gauge glyph", f.Group)
	}
	want := config.Motions()
	if strings.Join(f.Options, ",") != strings.Join(want, ",") {
		t.Errorf("options = %v, want %v", f.Options, want)
	}

	// Right after Gauge glyph.
	m := newModel(t)
	var labels []string
	for _, fl := range m.sections[0].Fields {
		labels = append(labels, fl.Label)
	}
	for i, l := range labels {
		if l == "Gauge glyph" {
			if i+1 >= len(labels) || labels[i+1] != motionLabel {
				t.Errorf("field after Gauge glyph is not Motion; labels %v", labels)
			}
		}
	}
}

func TestArrowsWriteMotionAndEmitConfigSavedMsg(t *testing.T) {
	for _, tt := range []struct {
		name, start, key, want string
	}{
		{"blink right", config.MotionBlink, "right", config.MotionStatic},
		{"static right wraps", config.MotionStatic, "right", config.MotionBlink},
		{"blink left wraps", config.MotionBlink, "left", config.MotionStatic},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := motionModel(t, tt.start)
			updated, cmd := m.Update(testutil.Key(tt.key))
			next := updated.(Model)

			if got := next.config.App.Motion; got != tt.want {
				t.Fatalf("Motion = %q, want %q", got, tt.want)
			}
			msg, ok := testutil.MsgOf[ConfigSavedMsg](cmd)
			if !ok {
				t.Fatal("no ConfigSavedMsg; the router never applies the new motion")
			}
			if msg.Config.App.Motion != tt.want {
				t.Errorf("ConfigSavedMsg carries Motion %q, want %q", msg.Config.App.Motion, tt.want)
			}
		})
	}
}

func TestSpaceAndEnterDoNothingOnMotion(t *testing.T) {
	for _, key := range []string{"space", "enter"} {
		t.Run(key, func(t *testing.T) {
			m := motionModel(t, config.MotionBlink)
			updated, cmd := m.Update(testutil.Key(key))
			next := updated.(Model)

			if next.config.App.Motion != config.MotionBlink {
				t.Errorf("%s changed Motion to %q (Rule 135)", key, next.config.App.Motion)
			}
			if _, ok := testutil.MsgOf[ConfigSavedMsg](cmd); ok {
				t.Errorf("%s saved the configuration", key)
			}
		})
	}
}

func TestAbsentMotionShowsTheDefaultAndStaysAbsent(t *testing.T) {
	m := motionModel(t, "")

	if got := fieldNamed(t, motionLabel).Value(m.config); got != "blink (default)" {
		t.Errorf("Value = %q, want %q", got, "blink (default)")
	}
	if !strings.Contains(plainRows(m), "blink (default)") {
		t.Error("the Motion row does not read 'blink (default)'")
	}
	if m.config.App.Motion != "" {
		t.Error("displaying the default wrote it")
	}

	// First arrow: static, written explicitly. Second: blink, explicit, no
	// longer shown as the default.
	next := feed(t, m, testutil.Key("right"))
	if next.config.App.Motion != config.MotionStatic {
		t.Fatalf("first → gave %q, want static", next.config.App.Motion)
	}
	next = feed(t, next, testutil.Key("right"))
	if next.config.App.Motion != config.MotionBlink {
		t.Fatalf("second → gave %q, want an explicit blink", next.config.App.Motion)
	}
	if got := fieldNamed(t, motionLabel).Value(next.config); got != "blink" {
		t.Errorf("explicit blink reads %q, want plain blink", got)
	}
}

func TestOtherCycleFieldsKeepTheirPlainValue(t *testing.T) {
	m := newModel(t)
	m.config.App.Theme = ""
	m.config.App.GaugeGlyph = ""
	for _, label := range []string{"Theme", "Gauge glyph"} {
		f := fieldNamed(t, label)
		if got := f.Value(m.config); got != "" {
			t.Errorf("%s reads %q, want an empty value, not a default", label, got)
		}
	}
}

func TestInvalidMotionWarnsOnTheAppTabOnly(t *testing.T) {
	const warning = `Motion "flash" is not blink or static`

	m := motionModel(t, "flash")
	if !strings.Contains(plainRows(m), "flash") {
		t.Error("the Motion row hides the value the file holds")
	}
	if !strings.Contains(plainFooter(m), warning) {
		t.Errorf("footer lacks the warning %q:\n%s", warning, plainFooter(m))
	}

	// Whichever field is focused on the app tab.
	other := focusOn(t, m, "Theme")
	if !strings.Contains(plainFooter(other), warning) {
		t.Error("the warning disappears when another app field has focus")
	}

	// Another tab shows its own hint.
	moved := feed(t, m, testutil.Key("tab"))
	if moved.activeTab == m.activeTab {
		t.Fatal("tab did not change the section")
	}
	if strings.Contains(plainFooter(moved), warning) {
		t.Error("the warning shows on a tab other than app")
	}
}

func TestTheMotionWarningIsAWarningLevelStatus(t *testing.T) {
	m := motionModel(t, "flash")
	st := m.status()

	if st.Level != components.LevelWarning {
		t.Errorf("Level = %v, want LevelWarning", st.Level)
	}
	if !strings.Contains(st.Text, `Motion "flash"`) {
		t.Errorf("Text = %q", st.Text)
	}
	if v := motionModel(t, config.MotionStatic).status(); v.Level == components.LevelWarning {
		t.Error("a valid value still warns")
	}
}

func TestAnArrowClearsTheMotionWarning(t *testing.T) {
	for _, key := range []string{"right", "left"} {
		t.Run(key, func(t *testing.T) {
			m := feed(t, motionModel(t, "flash"), testutil.Key(key))

			valid := false
			for _, v := range config.Motions() {
				valid = valid || m.config.App.Motion == v
			}
			if !valid {
				t.Fatalf("Motion = %q, not in %v", m.config.App.Motion, config.Motions())
			}
			if strings.Contains(plainFooter(m), "is not blink or static") {
				t.Error("the warning outlived the arrow")
			}
		})
	}
}

// Rule 128: error -> warning -> info -> status.
func TestAnErrorMessageBeatsTheMotionWarning(t *testing.T) {
	m := motionModel(t, "flash")
	_ = m.footer.Error("Failed to save — check logs")

	got := plainFooter(m)
	if !strings.Contains(got, "Failed to save") {
		t.Errorf("the error is not shown:\n%s", got)
	}
	if strings.Contains(got, "is not blink or static") {
		t.Error("the standing warning displaced the error")
	}
}

func TestHelpHasAMotionSection(t *testing.T) {
	m := newModel(t)

	for _, s := range m.GetHelpContent().Sections {
		if s.Title != "Motion" {
			continue
		}
		glyph := indicator.Glyph(indicator.Running, true)
		for _, want := range []string{"blink", "static", glyph} {
			if !strings.Contains(s.Body, want) {
				t.Errorf("Motion help lacks %q:\n%s", want, s.Body)
			}
		}
		return
	}
	t.Fatal("no help section titled Motion (Rule 114)")
}
