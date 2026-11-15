package theme

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
)

// DEVDESK-470 step 1: a "disabled" button variant, for a button that is
// advertised but cannot act (Rule 130). Dim text on the inactive background,
// the focused background (still dim text) when it holds focus.

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func TestButtonDisabledColors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		focused bool
		bg      color.Color
	}{
		{"unfocused", false, ColorButtonInactiveBg},
		{"focused", true, ColorButtonBg},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ButtonStyle(tc.focused, "disabled")
			if got := s.GetForeground(); !sameColor(got, ColorDim) {
				t.Errorf("foreground = %v, want ColorDim", got)
			}
			if got := s.GetBackground(); !sameColor(got, tc.bg) {
				t.Errorf("background = %v, want %v", got, tc.bg)
			}
		})
	}
}

func TestButtonDisabledDiffersFromPrimary(t *testing.T) {
	for _, focused := range []bool{false, true} {
		dis := RenderButton("Trace Path", focused, "disabled")
		pri := RenderButton("Trace Path", focused, "primary")
		if dis == pri {
			t.Errorf("focused=%v: the disabled button renders like primary: %q", focused, dis)
		}
	}
}

func TestButtonDisabledFocusIsVisible(t *testing.T) {
	on := RenderButton("Trace Path", true, "disabled")
	off := RenderButton("Trace Path", false, "disabled")
	if on == off {
		t.Errorf("a focused disabled button renders like an unfocused one: %q", on)
	}
}

func TestButtonDisabledKeepsPrimaryWidth(t *testing.T) {
	for _, focused := range []bool{false, true} {
		dis := lipgloss.Width(RenderButton("Trace Path", focused, "disabled"))
		pri := lipgloss.Width(RenderButton("Trace Path", focused, "primary"))
		if dis != pri {
			t.Errorf("focused=%v: width %d, want primary's %d", focused, dis, pri)
		}
	}
}
