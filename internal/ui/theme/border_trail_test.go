package theme

import (
	"github.com/anthnel/devdesk/internal/testutil/colorprofile"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ── BorderTrailBudget / RenderBorderTitleTrail (DEVDESK-134) ──────────────────

// trueColor forces colour output so background coverage can be inspected.
func trueColor(t *testing.T) {
	t.Helper()
	colorprofile.TrueColor(t)
}

func TestBorderTrailChromeIsNine(t *testing.T) {
	// "┌─ " + " (" + ")" + " " + one "─" + "┐": the parentheses belong to the
	// theme, so the budget already pays for them.
	if BorderTrailChrome != 9 {
		t.Errorf("BorderTrailChrome = %d, want 9", BorderTrailChrome)
	}
}

func TestBorderTrailBudget(t *testing.T) {
	tests := []struct {
		name  string
		title string
		width int
		want  int
	}{
		{"fits", "My Title", 40, 40 - 8 - BorderTrailChrome},
		{"wide", "My Title", 200, 200 - 8 - BorderTrailChrome},
		{"empty title", "", 40, 40 - BorderTrailChrome},
		{"exactly no room", "My Title", 8 + BorderTrailChrome, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BorderTrailBudget(tt.title, tt.width); got != tt.want {
				t.Errorf("BorderTrailBudget(%q, %d) = %d, want %d", tt.title, tt.width, got, tt.want)
			}
		})
	}
}

func TestBorderTrailBudgetIsNotPositiveWhenThereIsNoRoom(t *testing.T) {
	for _, w := range []int{-5, 0, 1, 10, 16, 17} {
		if got := BorderTrailBudget("My Title", w); got > 0 {
			t.Errorf("BorderTrailBudget(\"My Title\", %d) = %d, want <= 0", w, got)
		}
	}
}

func TestBorderTrailBudgetMeasuresTheRenderedTitle(t *testing.T) {
	trueColor(t)
	// Nerd Font glyph plus separators: the budget follows the cells the
	// title takes, never its byte length.
	title := "\U000f06a9 Agent usage › app"
	want := 120 - lipgloss.Width(title) - BorderTrailChrome
	if got := BorderTrailBudget(title, 120); got != want {
		t.Errorf("BorderTrailBudget = %d, want %d", got, want)
	}
}

func TestRenderBorderTitleTrailExactWidth(t *testing.T) {
	for _, w := range []int{22, 40, 80, 200} {
		const trail = "trail"
		got := RenderBorderTitleTrail("My Title", trail, w)
		if lipgloss.Width(got) != w {
			t.Errorf("width %d: line is %d cells", w, lipgloss.Width(got))
		}
		plain := ansi.Strip(got)
		// The trail hugs the title, in parentheses added by the theme.
		const head = "┌─ My Title (trail) "
		if !strings.HasPrefix(plain, head) {
			t.Errorf("width %d: starts %q, want prefix %q", w, plain, head)
		}
		if !strings.HasSuffix(plain, "─┐") {
			t.Errorf("width %d: ends %q", w, plain)
		}
	}
}

func TestRenderBorderTitleTrailFillRunsFromTheTrailToTheCorner(t *testing.T) {
	const w = 60
	plain := ansi.Strip(RenderBorderTitleTrail("My Title", "trail", w))
	mid := strings.TrimSuffix(strings.TrimPrefix(plain, "┌─ My Title (trail) "), "┐")
	if mid == "" || strings.Trim(mid, "─") != "" {
		t.Fatalf("filler between the trail and the corner = %q, want only '─'", mid)
	}
	// head is 20 cells, the corner 1.
	if want := w - 20 - 1; len([]rune(mid)) != want {
		t.Errorf("filler is %d dashes, want %d", len([]rune(mid)), want)
	}
}

func TestRenderBorderTitleTrailWithTheWholeBudgetLeavesOneDash(t *testing.T) {
	trueColor(t)
	const w = 60
	budget := BorderTrailBudget("My Title", w)
	trail := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBackground).
		Render(strings.Repeat("x", budget))
	got := RenderBorderTitleTrail("My Title", trail, w)
	if lipgloss.Width(got) != w {
		t.Fatalf("line is %d cells, want %d", lipgloss.Width(got), w)
	}
	plain := ansi.Strip(got)
	if want := "┌─ My Title (" + strings.Repeat("x", budget) + ") ─┐"; plain != want {
		t.Errorf("exactly one dash expected before the corner:\n got %q\nwant %q", plain, want)
	}
}

func TestRenderBorderTitleTrailDegradesToThePlainTitle(t *testing.T) {
	trueColor(t)
	const w = 60
	budget := BorderTrailBudget("My Title", w)
	tests := []struct {
		name  string
		trail string
	}{
		{"empty", ""},
		{"one over budget", strings.Repeat("x", budget+1)},
		{"far over budget", strings.Repeat("x", w*2)},
		{"styled over budget", lipgloss.NewStyle().Bold(true).Foreground(ColorDim).
			Render(strings.Repeat("x", budget+1))},
	}
	want := RenderBorderTitle("My Title", w)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderBorderTitleTrail("My Title", tt.trail, w); got != want {
				t.Errorf("not byte-identical to RenderBorderTitle:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestRenderBorderTitleTrailWithNoRoomAtAll(t *testing.T) {
	for _, w := range []int{-3, 0, 5, 17} {
		want := RenderBorderTitle("My Title", w)
		if got := RenderBorderTitleTrail("My Title", "x", w); got != want {
			t.Errorf("width %d: got %q, want %q", w, got, want)
		}
	}
}

var styledRun = regexp.MustCompile(`\x1b\[[0-9;]*m[^\x1b]*\x1b\[0m`)

func TestRenderBorderTitleTrailEveryCellCarriesTheBackground(t *testing.T) {
	trueColor(t)
	trail := lipgloss.NewStyle().Foreground(ColorDim).Background(ColorBackground).Render("Sessions › app")
	got := RenderBorderTitleTrail("My Title", trail, 70)

	// Rule 115: no visible cell may sit outside a styled run, otherwise the
	// terminal's own background shows through.
	if rest := styledRun.ReplaceAllString(got, ""); rest != "" {
		t.Errorf("text outside any styled run (bare background): %q", rest)
	}
	// Every run opens with a background colour.
	for _, run := range styledRun.FindAllString(got, -1) {
		open := run[:strings.Index(run, "m")+1]
		if !strings.Contains(open, "48;2;") {
			t.Errorf("run %q has no background in %q", run, open)
		}
	}
}

func TestRenderBorderTitleTrailKeepsTheTrailStyledAsGiven(t *testing.T) {
	trueColor(t)
	trail := lipgloss.NewStyle().Foreground(ColorSecondary).Background(ColorBackground).Bold(true).Render("1a2b3c4d")
	got := RenderBorderTitleTrail("My Title", trail, 70)
	if !strings.Contains(got, trail) {
		t.Errorf("the styled trail was altered or cut: %q", got)
	}
	if plain := ansi.Strip(got); !strings.Contains(plain, "My Title (1a2b3c4d) ") {
		t.Errorf("parentheses and spacing are the theme's job: %q", plain)
	}
}
