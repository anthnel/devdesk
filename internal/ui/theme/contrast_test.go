package theme

import (
	"math"
	"testing"

	"charm.land/lipgloss/v2"
)

// Accepted forms: "#rgb" and "#rrggbb", case-insensitive. Anything else
// (names, ANSI indexes, "rgb(...)", missing "#", wrong length) is an error.
func TestContrastRatio_ReferenceValues(t *testing.T) {
	tests := []struct {
		name   string
		fg, bg string
		want   float64
	}{
		{"white on black", "#ffffff", "#000000", 21},
		{"black on white", "#000000", "#ffffff", 21},
		{"identical", "#1e1e2e", "#1e1e2e", 1},
		{"short form", "#fff", "#000", 21},
		{"uppercase", "#FFFFFF", "#000000", 21},
		{"catppuccin text on base", "#cdd6f4", "#1e1e2e", 11.3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ContrastRatio(lipgloss.Color(tt.fg), lipgloss.Color(tt.bg))
			if err != nil {
				t.Fatalf("ContrastRatio(%q, %q) error: %v", tt.fg, tt.bg, err)
			}
			if math.Abs(got-tt.want) > 0.05 {
				t.Errorf("ContrastRatio(%q, %q) = %.3f, want %.1f", tt.fg, tt.bg, got, tt.want)
			}
		})
	}
}

func TestContrastRatio_Symmetric(t *testing.T) {
	a, errA := ContrastRatio(lipgloss.Color("#cdd6f4"), lipgloss.Color("#1e1e2e"))
	b, errB := ContrastRatio(lipgloss.Color("#1e1e2e"), lipgloss.Color("#cdd6f4"))
	if errA != nil || errB != nil {
		t.Fatalf("errors: %v, %v", errA, errB)
	}
	if a == 0 || a != b {
		t.Errorf("ratio must be symmetric and non-zero, got %v and %v", a, b)
	}
}

func TestContrastRatio_RejectsNonHex(t *testing.T) {
	for _, c := range []string{"", "red", "12", "ffffff", "#ggg", "#12345", "#1234567", "rgb(1,2,3)"} {
		t.Run(c, func(t *testing.T) {
			if _, err := ContrastRatio(lipgloss.Color(c), lipgloss.Color("#000000")); err == nil {
				t.Errorf("foreground %q: want an error", c)
			}
			if _, err := ContrastRatio(lipgloss.Color("#ffffff"), lipgloss.Color(c)); err == nil {
				t.Errorf("background %q: want an error", c)
			}
		})
	}
}
