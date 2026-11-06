package theme

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// activityColors returns the four activity aliases by role name.
func activityColors() map[string]lipgloss.Color {
	return map[string]lipgloss.Color{
		"running":   ColorActivityRunning,
		"neutral":   ColorActivityNeutral,
		"suspended": ColorActivitySuspended,
		"failed":    ColorActivityFailed,
	}
}

// The aliases must target the semantic colours, not the raw pairs the default
// theme makes coincide, hence the separating theme.
func TestActivityColorsAreClosedAliases(t *testing.T) {
	withTheme(t, separatedTheme())

	if ColorActivitySuspended != ColorSeverityMedium {
		t.Errorf("ColorActivitySuspended = %q, want the MEDIUM severity colour %q", ColorActivitySuspended, ColorSeverityMedium)
	}
	if ColorActivitySuspended == ColorWarn {
		t.Error("ColorActivitySuspended aliases ColorWarn; it must alias ColorSeverityMedium")
	}
	if ColorActivityFailed != ColorSeverityCritical {
		t.Errorf("ColorActivityFailed = %q, want the CRITICAL severity colour %q", ColorActivityFailed, ColorSeverityCritical)
	}
	if ColorActivityFailed == ColorError {
		t.Error("ColorActivityFailed aliases ColorError; it must alias ColorSeverityCritical")
	}
	if ColorActivityRunning != ColorSecondary {
		t.Errorf("ColorActivityRunning = %q, want ColorSecondary %q", ColorActivityRunning, ColorSecondary)
	}
	if ColorActivityNeutral != ColorText {
		t.Errorf("ColorActivityNeutral = %q, want ColorText %q", ColorActivityNeutral, ColorText)
	}
}

func TestActivityColorsFollowAThemeChange(t *testing.T) {
	other := separatedTheme()
	other.Name = "other"
	other.ColorSecondary = "#123456"
	other.ColorText = "#abcdef"
	other.SeverityCritical = "#bb0022"
	other.SeverityMedium = "#bb9922"
	withTheme(t, other)

	want := map[string]lipgloss.Color{
		"running":   "#123456",
		"neutral":   "#abcdef",
		"suspended": "#bb9922",
		"failed":    "#bb0022",
	}
	for role, c := range activityColors() {
		if c != want[role] {
			t.Errorf("activity %s = %q after a theme change, want %q", role, c, want[role])
		}
	}
}

// No theme key may name an activity colour (D4): the aliases stay closed.
func TestNoThemeKeyNamesAnActivityColor(t *testing.T) {
	typ := reflect.TypeOf(Theme{})
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if strings.HasPrefix(tag, "activity") || strings.Contains(tag, "_activity") {
			t.Errorf("Theme field %s has json key %q: activity colours must not be theme keys", typ.Field(i).Name, tag)
		}
	}
}

func TestActivityColorsContrastWithBackgroundAndSelection(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		for role, c := range activityColors() {
			for _, bg := range []struct {
				what string
				c    lipgloss.Color
			}{{"background", ColorBackground}, {"selected row", ColorTableLineSelected}} {
				r, err := ContrastRatio(c, bg.c)
				if err != nil {
					t.Fatalf("theme %s: %s %q on %s %q: %v", name, role, c, bg.what, bg.c, err)
				}
				if r < tokenMinContrast {
					t.Errorf("theme %s: %s %s on %s %s is %.2f:1, want >= %.1f:1",
						name, role, c, bg.what, bg.c, r, tokenMinContrast)
				}
			}
		}
	})
}

// Neutral carries queued/done/cancelled; dim would read as "nothing here".
func TestNeutralIsNotDim(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		if ColorActivityNeutral == ColorDim {
			t.Errorf("theme %s: ColorActivityNeutral equals ColorDim %q", name, ColorDim)
		}
	})
}
