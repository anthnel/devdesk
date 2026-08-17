package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The default theme gives ColorError and SeverityCritical the same hex, and
// ColorWarn and SeverityMedium likewise. Asserting on the default would
// therefore pass whichever of the two pairs the aliases were wired to, so these
// tests apply a theme that separates them.
func separatedTheme() *Theme {
	t := DefaultTheme()
	t.Name = "separated"
	t.ColorError = "#ff0000"
	t.ColorWarn = "#ff8800"
	t.ColorText = "#0000ff"
	t.SeverityCritical = "#aa0011"
	t.SeverityMedium = "#aa8811"
	return t
}

func withTheme(t *testing.T, th *Theme) {
	t.Helper()
	ApplyTheme(th)
	t.Cleanup(func() { ApplyTheme(DefaultTheme()) })
}

func TestFooterErrorTakesTheCriticalSeverityColor(t *testing.T) {
	withTheme(t, separatedTheme())

	if got, want := ColorFooterError, ColorSeverityCritical; got != want {
		t.Errorf("ColorFooterError = %q, want the CRITICAL severity colour %q", got, want)
	}
	if ColorFooterError == ColorError {
		t.Error("ColorFooterError aliases ColorError; it must alias ColorSeverityCritical")
	}
}

func TestFooterWarnTakesTheMediumSeverityColor(t *testing.T) {
	withTheme(t, separatedTheme())

	if got, want := ColorFooterWarn, ColorSeverityMedium; got != want {
		t.Errorf("ColorFooterWarn = %q, want the MEDIUM severity colour %q", got, want)
	}
	if ColorFooterWarn == ColorWarn {
		t.Error("ColorFooterWarn aliases ColorWarn; it must alias ColorSeverityMedium")
	}
}

// Info must be neutral. ColorHighlight carried it before and is a yellow one
// notch from the warning's orange — the two levels were indistinguishable.
func TestFooterInfoIsNeutralAndNotTheHighlight(t *testing.T) {
	withTheme(t, separatedTheme())

	if got, want := ColorFooterInfo, ColorText; got != want {
		t.Errorf("ColorFooterInfo = %q, want the ordinary text colour %q", got, want)
	}
	if ColorFooterInfo == ColorHighlight {
		t.Error("ColorFooterInfo aliases ColorHighlight, the colour this change moved away from")
	}
}

// The styles are rebuilt by RefreshStyles, which ApplyTheme calls last. A style
// captured at declaration would keep the zero value forever — the trap the
// syntax colours' comment already names.
func TestFooterStylesFollowAThemeChange(t *testing.T) {
	withTheme(t, separatedTheme())

	cases := []struct {
		name  string
		style lipgloss.Style
		want  lipgloss.Color
		bold  bool
	}{
		{"FooterInfoStyle", FooterInfoStyle, ColorFooterInfo, false},
		{"FooterWarnStyle", FooterWarnStyle, ColorFooterWarn, true},
		{"FooterErrorStyle", FooterErrorStyle, ColorFooterError, true},
	}

	for _, c := range cases {
		if got := c.style.GetForeground(); got != lipgloss.TerminalColor(c.want) {
			t.Errorf("%s foreground = %v, want %v", c.name, got, c.want)
		}
		if got := c.style.GetBackground(); got != lipgloss.TerminalColor(ColorBackground) {
			t.Errorf("%s has no app background (Rule 115): %v", c.name, got)
		}
		if got := c.style.GetBold(); got != c.bold {
			t.Errorf("%s bold = %v, want %v", c.name, got, c.bold)
		}
	}
}
