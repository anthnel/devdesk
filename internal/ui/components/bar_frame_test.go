package components

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ── OpensWithBarFrame (DEVDESK-457) ──────────────────────────────────────────

// The router detects the bar from the footer's first two lines instead of
// asking the view, so the answer cannot diverge from what is drawn.
func TestOpensWithBarFrame(t *testing.T) {
	visible := NewFilterBar()
	visible.ActivateSearch()

	tests := []struct {
		name   string
		footer string
		want   bool
	}{
		{"BarFrame then the info line", BarFrame(80, "x") + "\n" + "info", true},
		{"a visible filter bar", visible.View(), true},
		{"a blank line then the info line", theme.EmptyLineBg(80) + "\n" + "info", false},
		{"an empty footer", "", false},
		{"a single bar line, no floor", "│x│", false},
		{"a bar line over a line that is not a floor", "│x│\nplain", false},
		{"a bar line over a line with a floor's left corner only", "│x│\n└───", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OpensWithBarFrame(tt.footer); got != tt.want {
				t.Errorf("OpensWithBarFrame(%q) = %v, want %v", ansi.Strip(tt.footer), got, tt.want)
			}
		})
	}
}
