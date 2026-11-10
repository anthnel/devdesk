package status

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/testutil/colorprofile"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Rule 115 and DEVDESK-201 (lipgloss v2 renamed WithWhitespaceBackground to
// WithWhitespaceStyle): a modal centered by lipgloss.Place paints every cell
// around its box with ColorBackground, otherwise the terminal's own background
// shows through. The sites are in the view packages, so the guard runs through
// the status view's delete confirmation (the same call as the 15 others).
func TestPlacedModalsPaintTheirWhitespace(t *testing.T) {
	colorprofile.TrueColor(t)

	// The SGR prefix lipgloss emits for ColorBackground, read from lipgloss
	// itself so the test does not hard-code the theme's RGB.
	probe := lipgloss.NewStyle().Background(theme.ColorBackground).Render("x")
	bg := probe[:strings.Index(probe, "x")]
	if !strings.Contains(bg, "48;2;") {
		t.Fatalf("precondition: the background SGR %q is not true colour", bg)
	}

	m := feed(t, loadedModel(t), testutil.Key(keymap.Delete))
	m = feed(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	out := m.Render()

	lines := strings.Split(out, "\n")
	if len(lines) != 24 {
		t.Fatalf("the placed modal is %d lines, want 24", len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != 80 {
			t.Errorf("line %d is %d cells wide, want 80", i, w)
		}
		// The first and last lines are entirely whitespace: every cell must
		// sit inside a run that carries the background.
		if i == 0 || i == len(lines)-1 {
			if !strings.HasPrefix(line, bg) {
				t.Errorf("line %d does not start with the background SGR: %q", i, line)
			}
		}
	}
}
