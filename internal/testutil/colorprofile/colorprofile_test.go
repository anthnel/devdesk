package colorprofile_test

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/testutil/colorprofile"
)

// Lip Gloss v2 has no global colour profile: Style.Render always emits the
// full-colour SGR sequence and the downsampling happens at the output writer.
// The helper therefore keeps its signatures (about 50 callers) and does
// nothing; these tests pin that contract (DEVDESK-201, H2). The helper's own
// removal and the termenv dependency are step F.

// fakeT records what the helper asks of the test.
type fakeT struct {
	helpers  int
	cleanups []func()
}

func (f *fakeT) Helper()           { f.helpers++ }
func (f *fakeT) Cleanup(fn func()) { f.cleanups = append(f.cleanups, fn) }

// A style emits an escape sequence with no helper call at all: this is the
// premise that makes the helper a no-op.
func TestV2RendersWithoutAnyForcedProfile(t *testing.T) {
	got := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Render("x")
	if got == "x" {
		t.Fatal("a styled render emitted no escape sequence; the helper cannot be a no-op")
	}
}

func TestTrueColorKeepsItsSignatureAndChangesNothing(t *testing.T) {
	ft := &fakeT{}
	before := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Render("x")

	colorprofile.TrueColor(ft)

	for _, fn := range ft.cleanups {
		fn()
	}
	if after := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Render("x"); after != before {
		t.Errorf("render changed across TrueColor: %q then %q", before, after)
	}
}

func TestTrueColorWorksWithRealT(t *testing.T) {
	t.Run("inner", func(t *testing.T) {
		colorprofile.TrueColor(t)
		if got := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Render("x"); got == "x" {
			t.Error("style rendered without an escape sequence")
		}
	})
}

// Force is called from TestMain, where there is no *testing.T: it must still
// hand back a callable restore function.
func TestForceReturnsACallableRestore(t *testing.T) {
	restore := colorprofile.Force()
	if restore == nil {
		t.Fatal("Force returned a nil restore func")
	}
	restore()
}
