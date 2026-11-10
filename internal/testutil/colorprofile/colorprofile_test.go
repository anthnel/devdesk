package colorprofile_test

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/muesli/termenv"

	"github.com/anthnel/devdesk/internal/testutil/colorprofile"
)

// fakeT records Cleanup functions so a test can run them on demand and observe
// the profile before and after.
type fakeT struct {
	helpers  int
	cleanups []func()
}

func (f *fakeT) Helper()           { f.helpers++ }
func (f *fakeT) Cleanup(fn func()) { f.cleanups = append(f.cleanups, fn) }

func (f *fakeT) runCleanups() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
	f.cleanups = nil
}

// setBaseline puts the global profile on a value that is not TrueColor and
// restores whatever was there when the test ends.
func setBaseline(t *testing.T, p termenv.Profile) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(p)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

func TestTrueColorForcesProfileThenRestores(t *testing.T) {
	for name, base := range map[string]termenv.Profile{"Ascii": termenv.Ascii, "ANSI256": termenv.ANSI256} {
		t.Run(name, func(t *testing.T) {
			setBaseline(t, base)
			ft := &fakeT{}

			colorprofile.TrueColor(ft)

			if got := lipgloss.ColorProfile(); got != termenv.TrueColor {
				t.Fatalf("profile after TrueColor = %v, want TrueColor", got)
			}
			if ft.helpers == 0 {
				t.Error("TrueColor did not call t.Helper()")
			}
			if len(ft.cleanups) != 1 {
				t.Fatalf("TrueColor registered %d cleanups, want 1", len(ft.cleanups))
			}

			ft.runCleanups()

			if got := lipgloss.ColorProfile(); got != base {
				t.Errorf("profile after Cleanup = %v, want %v restored", got, base)
			}
		})
	}
}

func TestTrueColorWorksWithRealT(t *testing.T) {
	setBaseline(t, termenv.Ascii)

	t.Run("inner", func(t *testing.T) {
		colorprofile.TrueColor(t)
		if got := lipgloss.ColorProfile(); got != termenv.TrueColor {
			t.Fatalf("profile inside subtest = %v, want TrueColor", got)
		}
	})

	if got := lipgloss.ColorProfile(); got != termenv.Ascii {
		t.Errorf("profile after subtest = %v, want Ascii restored", got)
	}
}

func TestTrueColorMakesStylesEmitEscapes(t *testing.T) {
	setBaseline(t, termenv.Ascii)
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
	if style.Render("x") != "x" {
		t.Skip("baseline Ascii profile unexpectedly emits escapes")
	}

	colorprofile.TrueColor(t)

	if got := style.Render("x"); got == "x" {
		t.Error("style rendered without an escape under a forced TrueColor profile")
	}
}

func TestForceReturnsRestore(t *testing.T) {
	setBaseline(t, termenv.Ascii)

	restore := colorprofile.Force()

	if got := lipgloss.ColorProfile(); got != termenv.TrueColor {
		t.Fatalf("profile after Force = %v, want TrueColor", got)
	}
	if restore == nil {
		t.Fatal("Force returned a nil restore func")
	}

	restore()

	if got := lipgloss.ColorProfile(); got != termenv.Ascii {
		t.Errorf("profile after restore = %v, want Ascii", got)
	}
}
