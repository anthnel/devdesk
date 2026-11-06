package indicator

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// wantAlias maps every state to the theme alias it must read at call time.
func wantAlias(s State) lipgloss.Color {
	switch s {
	case Running:
		return theme.ColorActivityRunning
	case Failed:
		return theme.ColorActivityFailed
	case Suspended, Restarting:
		return theme.ColorActivitySuspended
	case Queued, Done, Cancelled, Skipped, Unknown:
		return theme.ColorActivityNeutral
	default: // Idle
		return theme.ColorText
	}
}

func allStates() []State { return append([]State{Idle}, States()...) }

func TestColorMapsEachStateToItsAlias(t *testing.T) {
	for _, s := range allStates() {
		t.Run(s.String(), func(t *testing.T) {
			if got, want := Color(s), wantAlias(s); got == "" || got != want {
				t.Errorf("Color(%s) = %q, want %q", s, got, want)
			}
		})
	}
}

func TestColorNeverUsesOKOrDim(t *testing.T) {
	for _, s := range allStates() {
		got := Color(s)
		if got == theme.ColorOK {
			t.Errorf("Color(%s) = ColorOK %q: done must not read green", s, got)
		}
		if got == theme.ColorDim {
			t.Errorf("Color(%s) = ColorDim %q: dim reads as nothing here", s, got)
		}
	}
}

func TestStyleIsForegroundOnly(t *testing.T) {
	for _, s := range allStates() {
		st := Style(s)
		if _, ok := st.GetBackground().(lipgloss.NoColor); !ok {
			t.Errorf("Style(%s) has a background %v; datatable paints it", s, st.GetBackground())
		}
		if got, want := st.GetForeground(), lipgloss.TerminalColor(Color(s)); got != want {
			t.Errorf("Style(%s) foreground = %v, want %v", s, got, want)
		}
		if st.GetHorizontalPadding() != 0 || st.GetVerticalPadding() != 0 {
			t.Errorf("Style(%s) has padding", s)
		}
	}
}

// Color must read the theme globals at call time: ApplyTheme reassigns them.
func TestColorFollowsAThemeChange(t *testing.T) {
	t.Cleanup(func() { theme.ApplyTheme(theme.DefaultTheme()) })
	theme.ApplyTheme(theme.DefaultTheme())
	before := Color(Running)

	alt := theme.DefaultTheme()
	alt.ColorSecondary = "#123456"
	alt.ColorText = "#abcdef"
	theme.ApplyTheme(alt)

	if got := Color(Running); got != theme.ColorActivityRunning || got == before {
		t.Errorf("Color(Running) = %q after theme change (before %q), want %q", got, before, theme.ColorActivityRunning)
	}
	if got, want := Color(Idle), theme.ColorText; got != want {
		t.Errorf("Color(Idle) = %q after theme change, want %q", got, want)
	}
}
