package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

func manyShortcuts(n int) shortcut.Shortcuts {
	out := make(shortcut.Shortcuts, 0, n)
	for i := range n {
		out = append(out, shortcut.Shortcut{Key: string(rune('a' + i%26)), Description: "Do the thing"})
	}
	return out
}

// ── Fixed height ─────────────────────────────────────────────────────────────

// The header is a fixed seven rows. resize() budgets the viewport against it,
// so a header that grew with the shortcut count would push the last table row
// off the bottom of the window.
func TestTheHeaderIsAlwaysSevenRows(t *testing.T) {
	for _, n := range []int{0, 1, 7, 8, 20, 40} {
		got := lipgloss.Height(renderHeaderContent(
			[]shortcut.HeaderInfo{{Key: "Context", Value: "default"}},
			manyShortcuts(n),
			180,
		))
		if got != headerMinHeight {
			t.Errorf("with %d shortcuts the header is %d rows, want %d", n, got, headerMinHeight)
		}
	}
}

func TestBuildShortcutLinesAlwaysFillsTheColumn(t *testing.T) {
	for _, n := range []int{0, 3, 11, 30} {
		if got := len(buildShortcutLines(manyShortcuts(n), 60)); got != headerMinHeight {
			t.Errorf("with %d shortcuts buildShortcutLines returned %d lines, want %d", n, got, headerMinHeight)
		}
	}
}

// Shortcuts overflow into a second column rather than being dropped: the header
// height is fixed, so the only alternative to wrapping is losing them.
func TestShortcutsOverflowIntoASecondColumn(t *testing.T) {
	oneColumn := buildShortcutLines(manyShortcuts(headerMinHeight), 200)
	twoColumns := buildShortcutLines(manyShortcuts(headerMinHeight+1), 200)

	if lipgloss.Width(twoColumns[0]) <= lipgloss.Width(oneColumn[0]) {
		t.Errorf("an eighth shortcut did not widen the block: %d then %d",
			lipgloss.Width(oneColumn[0]), lipgloss.Width(twoColumns[0]))
	}
}

// ── Width ────────────────────────────────────────────────────────────────────

// Every row is exactly the window width — no shorter, which would show the
// terminal's own background through the header (Rule 115), and no longer, which
// would wrap and cost the viewport a row. 80 is the case that used to overflow:
// the shortcut block is clipped to its column, not merely padded (D18).
func TestEveryHeaderRowFillsTheWidth(t *testing.T) {
	for _, width := range []int{80, 120, 180, 240} {
		rendered := renderHeaderContent(
			[]shortcut.HeaderInfo{{Key: "Context", Value: "default"}},
			manyShortcuts(12),
			width,
		)
		for i, line := range strings.Split(rendered, "\n") {
			if got := lipgloss.Width(line); got != width {
				t.Errorf("at width %d, row %d is %d wide", width, i, got)
			}
		}
	}
}

// Clipping must not cut an escape sequence in half: the truncated row would
// leak its colour onto everything rendered after it, which is the corruption
// Rule 122 is about.
func TestClippingTheShortcutBlockLeavesTheStylingIntact(t *testing.T) {
	testutil.TrueColor(t)

	rendered := renderHeaderContent(
		[]shortcut.HeaderInfo{{Key: "Context", Value: "default"}},
		manyShortcuts(12),
		80,
	)

	for i, line := range strings.Split(rendered, "\n") {
		if truncated(line) {
			t.Errorf("row %d ends inside an escape sequence: %q", i, line)
		}
	}
}

// truncated reports whether the line holds an escape sequence that was never
// terminated — the shape a naive slice leaves behind.
func truncated(line string) bool {
	for _, after := range strings.Split(line, "\x1b[")[1:] {
		end := strings.IndexByte(after, 'm')
		if end < 0 || strings.Contains(after[:end], "\x1b") {
			return true
		}
	}
	return false
}

// A window too narrow for the logo alone must not produce a negative column
// width — lipgloss would panic on the repeat.
func TestTheHeaderSurvivesAWindowNarrowerThanTheLogo(t *testing.T) {
	rendered := renderHeaderContent(
		[]shortcut.HeaderInfo{{Key: "Context", Value: "default"}},
		manyShortcuts(4),
		30,
	)

	if lipgloss.Height(rendered) != headerMinHeight {
		t.Errorf("the header is %d rows at width 30, want %d", lipgloss.Height(rendered), headerMinHeight)
	}
}

// ── Content ──────────────────────────────────────────────────────────────────

// Values align on the longest key, which is what makes the info block readable
// as a column rather than a ragged list.
func TestInfoValuesAlignOnTheLongestKey(t *testing.T) {
	lines := buildInfoLines([]shortcut.HeaderInfo{
		{Key: "URL", Value: "gitlab.com"},
		{Key: "Context", Value: "default"},
	}, 40)

	first := strings.Index(lines[0], "gitlab.com")
	second := strings.Index(lines[1], "default")
	if first != second {
		t.Errorf("values start at columns %d and %d; they should align", first, second)
	}
}

func TestInfoLinesPadOutToTheHeaderHeight(t *testing.T) {
	lines := buildInfoLines([]shortcut.HeaderInfo{{Key: "Context", Value: "default"}}, 40)

	if len(lines) != headerMinHeight {
		t.Fatalf("buildInfoLines returned %d lines, want %d", len(lines), headerMinHeight)
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" {
			t.Errorf("line %d is %q, want it empty", i, lines[i])
		}
	}
}

// The logo is centred in the seven rows: one blank above, one below.
func TestTheLogoIsVerticallyCentred(t *testing.T) {
	testutil.TrueColor(t)
	lines := buildLogoLines(logoWidth)

	if strings.Contains(lines[0], "_") {
		t.Error("the logo starts on the first row; it should have a blank above it")
	}
	if !strings.Contains(lines[1], "_") {
		t.Error("the logo does not start on the second row")
	}
	if strings.Contains(lines[headerMinHeight-1], "_") {
		t.Error("the logo reaches the last row; it should have a blank below it")
	}
}

// ── The header block (DEVDESK-109) ───────────────────────────────────────────

// Closed, the header block is the 7 rows and one blank line: the permanent
// command line, its inactive prompt and the blank line under it are gone.
func TestTheClosedHeaderBlockHasNoCommandLine(t *testing.T) {
	a := router(t, &fakeView{})

	rendered := a.renderHeader()

	if strings.Contains(rendered, "❯") {
		t.Error("the closed header still shows a command line prompt")
	}
	if lipgloss.Height(rendered) != headerMinHeight+1 {
		t.Errorf("the header block is %d rows, want %d (header + blank)",
			lipgloss.Height(rendered), headerMinHeight+1)
	}
}

// A view that provides no header renders none, and resize() has to survive it.
func TestAViewWithoutAHeaderRendersNothing(t *testing.T) {
	a := router(t, &bareView{})

	if got := a.renderHeader(); got != "" {
		t.Errorf("renderHeader() = %q for a view that provides no header data", got)
	}
	a.resize(120, 40) // must not panic
}

// The input is a row of the palette's frame, not of the header: the header
// shows neither the typed text nor a prompt, and the frame shows the bare input
// (no ghost completion, no [i/n] position, DEVDESK-89).
func TestTheTypedTextIsInTheFrameNotInTheHeader(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "stat")

	header := ansi.Strip(a.renderHeader())
	if strings.Contains(header, "❯") || strings.Contains(header, "stat") {
		t.Errorf("the header carries the command line:\n%s", header)
	}
	input := mustFrame(t, a).input()
	if strings.Contains(input, "statstatus") || strings.Contains(input, "stat"+"us") {
		t.Errorf("the input shows a completion preview: %q", input)
	}
	if !strings.Contains(input, "❯ stat") {
		t.Errorf("the input row does not show the typed text: %q", input)
	}
}

// ── Narrow terminals (DEVDESK-89) ────────────────────────────────────────────

// logoMarker is a run of the logo's last row that appears nowhere else in the
// header, so finding it means the logo column was rendered.
func logoMarker() string { return strings.TrimSpace(logoLines[len(logoLines)-1]) }

// The logo column is fixed at logoWidth cells; when the window cannot hold it
// next to the info column the column goes away, rather than pushing every row
// past the window and wrapping (Rule 108). Nothing changes where it fits.
func TestTheHeaderNeverOverflowsANarrowTerminal(t *testing.T) {
	info := []shortcut.HeaderInfo{{Key: "Context", Value: "default"}}
	cases := []struct {
		width    int
		wantLogo bool
	}{
		{40, false},
		{50, false},
		{58, true},
		{60, true},
		{80, true},
		{120, true},
	}
	for _, tc := range cases {
		rendered := renderHeaderContent(info, manyShortcuts(12), tc.width)
		lines := strings.Split(rendered, "\n")

		if len(lines) != headerMinHeight {
			t.Errorf("width %d: the header is %d rows, want %d", tc.width, len(lines), headerMinHeight)
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got != tc.width {
				t.Errorf("width %d: row %d is %d cells wide, want exactly %d", tc.width, i, got, tc.width)
			}
		}
		if got := strings.Contains(rendered, logoMarker()); got != tc.wantLogo {
			t.Errorf("width %d: logo present = %v, want %v", tc.width, got, tc.wantLogo)
		}
	}
}

// The whole block the router renders (header, blank) keeps its eight rows and
// wraps nowhere at 40 columns.
func TestTheHeaderBlockKeepsItsHeightAtFortyColumns(t *testing.T) {
	a := routerAt(t, &fakeView{shortcuts: manyShortcuts(12)}, 40, 20)

	lines := strings.Split(a.renderHeader(), "\n")

	if len(lines) != headerMinHeight+1 {
		t.Errorf("the header block is %d rows, want %d", len(lines), headerMinHeight+1)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got > 40 {
			t.Errorf("row %d is %d cells wide in a 40-column window", i, got)
		}
	}
}

// Every width from 0 up must give seven rows no wider than the window, and
// exactly as wide from 2 columns (the two padding cells) on. The sampled widths
// around 55 are where the logo fits but leaves the shortcut column no room.
func TestTheHeaderNeverExceedsTheWindowAtAnyWidth(t *testing.T) {
	infos := map[string][]shortcut.HeaderInfo{
		"short": {{Key: "Context", Value: "default"}},
		"long": {
			{Key: "Context", Value: strings.Repeat("very-long-value-", 12)},
			{Key: "Workspace", Value: strings.Repeat("x", 200)},
		},
		"wide": {{Key: "コンテキスト", Value: "日本語の長い値日本語の長い値日本語の長い値日本語の長い値"}},
	}
	var widths []int
	for w := 0; w <= 8; w++ {
		widths = append(widths, w)
	}
	widths = append(widths, 20, 40, 54, 55, 56, 57, 58, 59, 60, 62, 64, 66, 80)

	for name, info := range infos {
		for _, w := range widths {
			rendered := renderHeaderContent(info, manyShortcuts(12), w)
			lines := strings.Split(rendered, "\n")
			if len(lines) != headerMinHeight {
				t.Errorf("%s width %d: %d rows, want %d", name, w, len(lines), headerMinHeight)
			}
			for i, line := range lines {
				got := lipgloss.Width(line)
				if got > w {
					t.Errorf("%s width %d: row %d is %d cells wide", name, w, i, got)
				}
				if w >= 2 && got != w {
					t.Errorf("%s width %d: row %d is %d cells wide, want exactly %d", name, w, i, got, w)
				}
			}
		}
	}
}

// At 55 the logo is kept and the shortcut column has no width left: it must be
// empty rather than spill past the window.
func TestAnEmptyShortcutColumnRendersNoShortcut(t *testing.T) {
	for _, line := range buildShortcutLines(manyShortcuts(12), 0) {
		if lipgloss.Width(line) != 0 {
			t.Errorf("a zero-width shortcut column rendered %q", line)
		}
	}
	for _, line := range buildInfoLines([]shortcut.HeaderInfo{{Key: "Context", Value: "default"}}, 0) {
		if lipgloss.Width(line) != 0 {
			t.Errorf("a zero-width info column rendered %q", line)
		}
	}
}
