package agentusageview

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/testutil/colorprofile"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// viewWidths are the widths Rules 108 and 116 are checked at, from a wide
// terminal down to a single cell.
var viewWidths = []int{198, 118, 78, 58, 38, 22, 10, 1}

const longModel = "claude-some-extremely-long-model-name-with-a-very-long-suffix-20991231-preview-build"

// viewBreakdown is a three-block panel built by hand, so these tests do not
// depend on the builders of breakdown.go. Every block adds up to 1000 tokens.
func viewBreakdown() breakdown {
	return breakdown{blocks: []breakdownBlock{
		{title: "By category", total: 1000, lines: []breakdownLine{
			{label: "Input", role: theme.TokenRoleInput, tokens: 100, tenths: 100},
			{label: "Output", role: theme.TokenRoleOutput, tokens: 200, tenths: 200},
			{label: "Cache write 5 min", role: theme.TokenRoleCacheWrite, tokens: 50, tenths: 50},
			{label: "Cache write 1 h", role: theme.TokenRoleCacheWrite, tokens: 50, tenths: 50},
			{label: "Cache read", role: theme.TokenRoleCacheRead, tokens: 600, tenths: 600},
		}},
		{title: "By model", total: 1000, lines: []breakdownLine{
			{label: longModel, tokens: 900, tenths: 900},
			{label: "unknown", tokens: 100, tenths: 100},
		}},
		{title: "By agent", total: 1000, lines: []breakdownLine{
			{label: "orchestrator", tokens: 700, tenths: 700},
			{label: "general-purpose a1b2c3", tokens: 300, tenths: 300},
		}},
	}}
}

// emptyBreakdown is a session with no token: every share is noShare.
func emptyBreakdown() breakdown {
	cat := breakdownBlock{title: "By category"}
	for _, l := range viewBreakdown().blocks[0].lines {
		cat.lines = append(cat.lines, breakdownLine{label: l.label, role: l.role, tenths: noShare})
	}
	return breakdown{blocks: []breakdownBlock{
		cat,
		{title: "By model"},
		{title: "By agent", lines: []breakdownLine{{label: "orchestrator", tenths: noShare}}},
	}}
}

// plain strips every escape sequence from the rendered lines.
func plain(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

func TestRenderBreakdownEveryLineIsExactlyWidthCells(t *testing.T) {
	colorprofile.TrueColor(t)
	for name, b := range map[string]breakdown{"filled": viewBreakdown(), "empty": emptyBreakdown()} {
		for _, w := range viewWidths {
			lines := renderBreakdown(b, w)
			if len(lines) == 0 {
				t.Fatalf("%s at width %d: no line rendered", name, w)
			}
			for i, l := range lines {
				if got := lipgloss.Width(l); got != w {
					t.Errorf("%s at width %d: line %d is %d cells wide, want %d: %q",
						name, w, i, got, w, ansi.Strip(l))
				}
				if got := ansi.StringWidth(l); got != w {
					t.Errorf("%s at width %d: line %d is %d cells by ansi, want %d", name, w, i, got, w)
				}
			}
		}
	}
}

func TestRenderBreakdownTitlesAppearInOrder(t *testing.T) {
	text := strings.Join(plain(renderBreakdown(viewBreakdown(), 78)), "\n")
	last := -1
	for _, title := range []string{"By category", "By model", "By agent"} {
		if n := strings.Count(text, title); n != 1 {
			t.Fatalf("title %q appears %d times, want 1", title, n)
		}
		at := strings.Index(text, title)
		if at < last {
			t.Errorf("title %q comes before the previous one", title)
		}
		last = at
	}
}

func TestRenderBreakdownEachBlockEndsWithATotalAtOneHundredPercent(t *testing.T) {
	var totals []string
	for _, l := range plain(renderBreakdown(viewBreakdown(), 78)) {
		if strings.Contains(l, "Total") {
			totals = append(totals, l)
		}
	}
	if len(totals) != 3 {
		t.Fatalf("got %d Total lines, want 3: %q", len(totals), totals)
	}
	for _, l := range totals {
		if !strings.Contains(l, "100.0%") {
			t.Errorf("Total line lacks 100.0%%: %q", l)
		}
		if !strings.Contains(l, "1.0k") {
			t.Errorf("Total line lacks the block total (1.0k): %q", l)
		}
	}
}

// fgCode is the "38;2;r;g;b" parameter lipgloss emits for a foreground colour.
func fgCode(t *testing.T, c lipgloss.Color) string {
	t.Helper()
	out := lipgloss.NewStyle().Foreground(c).Render("x")
	at := strings.Index(out, "38;")
	end := strings.Index(out, "m")
	if at < 0 || end < at {
		t.Fatalf("no foreground sequence in %q", out)
	}
	return out[at:end]
}

func TestRenderBreakdownSwatchCarriesTheRoleColour(t *testing.T) {
	colorprofile.TrueColor(t)
	lines := renderBreakdown(viewBreakdown(), 118)
	cases := []struct {
		label string
		role  theme.TokenRole
	}{
		{"Input", theme.TokenRoleInput},
		{"Output", theme.TokenRoleOutput},
		{"Cache write 5 min", theme.TokenRoleCacheWrite},
		{"Cache write 1 h", theme.TokenRoleCacheWrite},
		{"Cache read", theme.TokenRoleCacheRead},
	}
	for _, tc := range cases {
		var line string
		for _, l := range lines {
			if strings.Contains(ansi.Strip(l), tc.label) && strings.Contains(ansi.Strip(l), "%") {
				line = l
				break
			}
		}
		if line == "" {
			t.Fatalf("no line for %q", tc.label)
		}
		before := line[:strings.Index(line, tc.label)]
		if !strings.Contains(ansi.Strip(before), theme.GaugeFill(1)) {
			t.Errorf("%q: no swatch glyph before the label: %q", tc.label, ansi.Strip(before))
		}
		if want := fgCode(t, theme.TokenColor(tc.role)); !strings.Contains(before, want) {
			t.Errorf("%q: swatch is not in the colour of role %q (%s)", tc.label, tc.role, want)
		}
		for _, other := range theme.TokenRoles() {
			if other == tc.role {
				continue
			}
			if code := fgCode(t, theme.TokenColor(other)); strings.Contains(before, code) {
				t.Errorf("%q: swatch also carries the colour of role %q", tc.label, other)
			}
		}
	}
}

func TestRenderBreakdownOnlyTheCategoryBlockHasSwatches(t *testing.T) {
	text := plain(renderBreakdown(viewBreakdown(), 118))
	for _, l := range text {
		if (strings.Contains(l, "orchestrator") || strings.Contains(l, "unknown")) && strings.Contains(l, theme.GaugeFill(1)) {
			t.Errorf("a non-category line carries a swatch: %q", l)
		}
	}
}

func TestRenderBreakdownColumnsDisappearAtNarrowWidths(t *testing.T) {
	cases := []struct {
		width         int
		share, tokens bool
	}{
		{118, true, true},
		{58, true, true},
		{38, true, true},
		{22, true, false},  // Tokens goes first
		{10, false, false}, // then Share
	}
	for _, tc := range cases {
		text := strings.Join(plain(renderBreakdown(viewBreakdown(), tc.width)), "\n")
		if got := strings.Contains(text, "Share"); got != tc.share {
			t.Errorf("width %d: Share header present = %v, want %v", tc.width, got, tc.share)
		}
		if got := strings.Contains(text, "Tokens"); got != tc.tokens {
			t.Errorf("width %d: Tokens header present = %v, want %v", tc.width, got, tc.tokens)
		}
		if got := strings.Contains(text, "%"); got != tc.share {
			t.Errorf("width %d: a percentage is shown = %v, want %v", tc.width, got, tc.share)
		}
	}
}

func TestRenderBreakdownSessionWithoutTokenShowsDashes(t *testing.T) {
	text := strings.Join(plain(renderBreakdown(emptyBreakdown(), 78)), "\n")
	if !strings.Contains(text, "-") {
		t.Errorf("a session with no token shows no \"-\":\n%s", text)
	}
	for _, bad := range []string{"NaN", "Inf", "-1"} {
		if strings.Contains(text, bad) {
			t.Errorf("a session with no token shows %q:\n%s", bad, text)
		}
	}
	for _, l := range plain(renderBreakdown(emptyBreakdown(), 78)) {
		if strings.Contains(l, "Input") && !strings.Contains(l, "-") {
			t.Errorf("the Input line shows no \"-\" share: %q", l)
		}
	}
}

func TestRenderBreakdownLongModelNameIsTruncatedWithAnEllipsis(t *testing.T) {
	for _, w := range []int{78, 38} {
		text := strings.Join(plain(renderBreakdown(viewBreakdown(), w)), "\n")
		if strings.Contains(text, longModel) {
			t.Errorf("width %d: the long model name is not truncated", w)
		}
		if !strings.Contains(text, "claude-some") || !strings.Contains(text, "…") {
			t.Errorf("width %d: expected a prefix of the model name ending in an ellipsis:\n%s", w, text)
		}
	}
	// Room enough: the whole name is kept.
	text := strings.Join(plain(renderBreakdown(viewBreakdown(), 198)), "\n")
	if !strings.Contains(text, longModel) {
		t.Errorf("width 198: the model name should fit whole")
	}
}

func TestClampTop(t *testing.T) {
	tests := []struct {
		name               string
		top, lines, height int
		want               int
	}{
		{"within range", 5, 10, 20, 0},
		{"past the end", 15, 30, 20, 10},
		{"negative", -3, 30, 20, 0},
		{"empty", 0, 0, 0, 0},
	}
	for _, tc := range tests {
		if got := clampTop(tc.top, tc.lines, tc.height); got != tc.want {
			t.Errorf("%s: clampTop(%d, %d, %d) = %d, want %d", tc.name, tc.top, tc.lines, tc.height, got, tc.want)
		}
	}
}
