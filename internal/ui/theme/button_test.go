package theme

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
)

// DEVDESK-470 step 1: a "disabled" button variant, for a button that is
// advertised but cannot act (Rule 130). Dim italic text on the raised surface,
// the two colours swapped when it holds focus (DEVDESK-764).

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func TestButtonDisabledColors(t *testing.T) {
	// DEVDESK-764: dim italic on the raised surface; under focus the two swap.
	for _, tc := range []struct {
		name    string
		focused bool
		fg, bg  func() color.Color
	}{
		{"unfocused", false, func() color.Color { return ColorDim }, func() color.Color { return ColorCmdLineBg }},
		{"focused", true, func() color.Color { return ColorCmdLineBg }, func() color.Color { return ColorDim }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ButtonStyle(tc.focused, "disabled")
			if got := s.GetForeground(); !sameColor(got, tc.fg()) {
				t.Errorf("foreground = %v, want %v", got, tc.fg())
			}
			if got := s.GetBackground(); !sameColor(got, tc.bg()) {
				t.Errorf("background = %v, want %v", got, tc.bg())
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
