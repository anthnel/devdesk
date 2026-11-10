package theme

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// DEVDESK-201 step A2: the palette is color.Color, Hex replaces string(c), and
// the contrast code reads components through RGBA() after a type check.
//
// These tests use the v2 API (Hex, validateTheme, color.Color-typed
// applyColor, textinput.SetStyles), so they are [R-compile] until the
// production side of A2 lands.

// [R-compile] Hex undefined.
func TestHexFormatsAColor(t *testing.T) {
	tests := []struct {
		name string
		c    color.Color
		want string
	}{
		{"six digits", lipgloss.Color("#a1b2c3"), "#a1b2c3"},
		{"three digits are expanded", lipgloss.Color("#fff"), "#ffffff"},
		{"upper case is lowered", lipgloss.Color("#ABCDEF"), "#abcdef"},
		{"empty is no colour", lipgloss.Color(""), "none"},
		{"NoColor", lipgloss.NoColor{}, "none"},
		{"an ANSI index is not an RGBA", lipgloss.Color("12"), "none"},
		{"nil", nil, "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Hex(tt.c); got != tt.want {
				t.Errorf("Hex = %q, want %q", got, tt.want)
			}
		})
	}
}

// [R-compile] validateTheme undefined. The message format is the contract the
// callers log.
func TestValidateThemeNamesTheFirstBadKey(t *testing.T) {
	th := DefaultTheme()
	th.ColorOK = "#ggg"
	err := validateTheme(th)
	if err == nil {
		t.Fatal("validateTheme accepted color_ok = #ggg")
	}
	if want := `color_ok: "#ggg" is not #rgb or #rrggbb`; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err, want)
	}
}

func TestTheDefaultThemeIsValid(t *testing.T) {
	if err := validateTheme(DefaultTheme()); err != nil {
		t.Fatalf("DefaultTheme is refused by its own validation: %v", err)
	}
}

// [R-compile] applyColor takes and returns color.Color. Empty is the fallback.
func TestApplyColorFallsBackOnEmptyOnly(t *testing.T) {
	fallback := lipgloss.Color("#123456")
	if got := applyColor("", fallback); got != fallback {
		t.Errorf("applyColor(\"\") = %s, want the fallback %s", Hex(got), Hex(fallback))
	}
	if got := applyColor("#abcdef", fallback); Hex(got) != "#abcdef" {
		t.Errorf("applyColor(#abcdef) = %s, want #abcdef", Hex(got))
	}
}

// [C] on the semantics (spike criterion 2): an empty colour renders no SGR and
// an unknown icon role reads as plain text.
func TestAnEmptyColorRendersNoSGR(t *testing.T) {
	if got := lipgloss.NewStyle().Foreground(lipgloss.Color("")).Render("x"); got != "x" {
		t.Errorf("empty foreground rendered %q, want %q", got, "x")
	}
	if got := IconColor(IconRole("nope")); got != ColorText {
		t.Errorf("IconColor(unknown) = %s, want ColorText %s", Hex(got), Hex(ColorText))
	}
}

// [R-compile] The contrast code type-checks before reading components:
// NoColor.RGBA() is opaque black and would otherwise read as a real colour.
func TestContrastRejectsNonRGBAColors(t *testing.T) {
	white := lipgloss.Color("#ffffff")
	for name, c := range map[string]color.Color{
		"NoColor":    lipgloss.NoColor{},
		"empty":      lipgloss.Color(""),
		"ANSI index": lipgloss.Color("5"),
		"nil":        nil,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ContrastRatio(c, white); err == nil {
				t.Errorf("ContrastRatio accepted %s as a foreground", name)
			}
			if _, err := ContrastRatio(white, c); err == nil {
				t.Errorf("ContrastRatio accepted %s as a background", name)
			}
			if _, err := ColorDistance(c, white); err == nil {
				t.Errorf("ColorDistance accepted %s", name)
			}
		})
	}
}

// [R-compile] StyleTextInput goes through SetStyles in v2 (the v1 fields
// TextStyle, PromptStyle, PlaceholderStyle and Cursor.Style are gone). The
// virtual cursor loses its background (Rule 115): that part is a manual check.
func TestStyleTextInputAppliesThemeColours(t *testing.T) {
	withTheme(t, separatedTheme())
	ti := textinput.New()
	ti.Prompt = "> "

	StyleTextInput(&ti)

	if ti.Prompt != "" {
		t.Errorf("Prompt = %q, want empty (Rule 120: the label is rendered outside)", ti.Prompt)
	}
	styles := ti.Styles()
	for name, state := range map[string]textinput.StyleState{"focused": styles.Focused, "blurred": styles.Blurred} {
		if got := state.Text.GetForeground(); got != ColorText {
			t.Errorf("%s text foreground = %s, want ColorText %s", name, Hex(got), Hex(ColorText))
		}
		if got := state.Text.GetBackground(); got != ColorBackground {
			t.Errorf("%s text background = %s, want the app background %s (Rule 115)", name, Hex(got), Hex(ColorBackground))
		}
		if got := state.Placeholder.GetForeground(); got != ColorDim {
			t.Errorf("%s placeholder foreground = %s, want ColorDim %s", name, Hex(got), Hex(ColorDim))
		}
		if got := state.Placeholder.GetBackground(); got != ColorBackground {
			t.Errorf("%s placeholder background = %s, want the app background", name, Hex(got))
		}
		if got := state.Prompt.GetForeground(); got != ColorHighlight {
			t.Errorf("%s prompt foreground = %s, want ColorHighlight %s", name, Hex(got), Hex(ColorHighlight))
		}
	}
	if got := styles.Cursor.Color; got != ColorHighlight {
		t.Errorf("cursor colour = %s, want ColorHighlight %s", Hex(got), Hex(ColorHighlight))
	}
}
