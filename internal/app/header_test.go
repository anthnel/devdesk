package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

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

// ── The command line ─────────────────────────────────────────────────────────

// Closed, the header shows the ":" prompt that says the command line is there.
func TestTheClosedCommandLineShowsThePrompt(t *testing.T) {
	a := router(t, &fakeView{})

	rendered := a.renderHeader()

	if !strings.Contains(rendered, ":") {
		t.Error("the inactive command line does not show its prompt")
	}
	if lipgloss.Height(rendered) != headerMinHeight+2 {
		t.Errorf("the header block is %d rows, want %d (header + blank + command line)",
			lipgloss.Height(rendered), headerMinHeight+2)
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

// The completion preview shows only the part still to be typed, so the line
// reads as one command rather than the prefix twice.
func TestTheCompletionPreviewShowsOnlyTheRemainder(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "stat")

	rendered := a.renderCommandLineWithCompletion()

	if strings.Contains(rendered, "statstatus") {
		t.Errorf("the preview repeats the typed prefix:\n%s", rendered)
	}
	if !strings.Contains(rendered, "us") {
		t.Errorf("the preview does not complete stat to status:\n%s", rendered)
	}
}

// The counter says how many suggestions there are and which one enter would
// run, which is what makes repeated tab navigable.
func TestTheCompletionCounterNamesThePosition(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "s")
	total := len(a.completionSuggestions)
	if total == 0 {
		t.Skip("no suggestion for \"s\"")
	}

	rendered := a.renderCommandLineWithCompletion()

	if !strings.Contains(rendered, "[1/") {
		t.Errorf("the command line does not show the position:\n%s", rendered)
	}

	feedKey(t, a, testutil.Key("tab"))
	if total > 1 && !strings.Contains(a.renderCommandLineWithCompletion(), "[2/") {
		t.Error("the position did not advance with tab")
	}
}

func TestTheCommandLineIsPlainWithoutSuggestions(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "zzz")

	if got, want := a.renderCommandLineWithCompletion(), a.commandInput.View(); got != want {
		t.Errorf("with nothing to suggest the line renders %q, want the bare input %q", got, want)
	}
}
