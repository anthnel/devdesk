package app

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// padFrame against lipgloss.Place (DEVDESK-281, task 281.3).
//
// padFrame is the whole-frame padding of App.Render's non-overlay path. It must
// leave the same cells on screen as the lipgloss.Place it replaces, which is
// what these tests hold: the unit tests state the behaviour, and
// TestPadFrameMatchesPlaceOnTheFrames is the net, over the frames of the cell
// goldens and over ragged blocks that really need padding.

// placeFrame is the call padFrame replaces, as App.Render made it.
func placeFrame(inner string, width, height int) string {
	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, inner,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(theme.ColorBackground)))
}

// innerFrame is the stacked frame App.Render hands to the padding pass.
func innerFrame(a *App) string {
	header := a.renderHeader() + "\n" + a.renderTitleLine()
	footer := a.renderViewFooter()
	barBelow := !a.frameless() && components.OpensWithBarFrame(footer)
	return fitHeight(header, a.renderBody(barBelow), footer, a.height)
}

// requireSameCells fails unless a and b decode to the same cells, row by row.
func requireSameCells(t *testing.T, what, want, got string) {
	t.Helper()
	w, g := testutil.FormatCells(testutil.Cells(want)), testutil.FormatCells(testutil.Cells(got))
	if w != g {
		t.Errorf("%s: cells differ from lipgloss.Place; first differences:\n%s", what, firstDifferences(w, g, 8))
	}
}

func rowWidths(s string) []int {
	var out []int
	for _, l := range strings.Split(s, "\n") {
		out = append(out, ansi.StringWidth(l))
	}
	return out
}

func TestPadFramePadsAShortLineWithTheBackground(t *testing.T) {
	useDefaultTheme(t)
	got := padFrame("ab\nabcd", 6, 2)

	rows := testutil.Cells(got)
	if len(rows) != 2 || len(rows[0]) != 6 || len(rows[1]) != 6 {
		t.Fatalf("got %d rows of %v cells, want 2 rows of 6: %q", len(rows), rowWidths(got), got)
	}
	bg := theme.Hex(theme.ColorBackground)
	for c := 2; c < 6; c++ {
		if rows[0][c].BG != bg || rows[0][c].Glyph != " " {
			t.Errorf("padding cell %d = %+v, want a space on %s (Rule 115)", c, rows[0][c], bg)
		}
	}
	if rows[0][0].Glyph != "a" || rows[0][1].Glyph != "b" {
		t.Errorf("the text moved: %+v", rows[0][:2])
	}
	if !strings.HasPrefix(got, "ab") {
		t.Errorf("the line is not left-aligned: %q", got)
	}
}

func TestPadFrameLeavesAFullWidthFrameByteIdentical(t *testing.T) {
	useDefaultTheme(t)
	inner := "abcdef\n" + theme.BgLine("styled", 6) + "\n\x1b[1mbold\x1b[m  "
	if got := padFrame(inner, 6, 3); got != inner {
		t.Errorf("a frame already width x height changed:\nwant %q\ngot  %q", inner, got)
	}
}

func TestPadFrameDoesNotCutALongLine(t *testing.T) {
	useDefaultTheme(t)
	// Like Place: once a line is wider than the screen nothing is padded
	// horizontally, the short lines included, and nothing is cut.
	inner := "abcdefghij\nab"
	got := padFrame(inner, 6, 2)
	if got != inner {
		t.Errorf("got %q, want the frame untouched (%q)", got, inner)
	}
	requireSameCells(t, "long line", placeFrame(inner, 6, 2), got)

	// The rows it adds are as wide as the widest line, as Place's are.
	got = padFrame(inner, 6, 4)
	if widths := rowWidths(got); len(widths) != 4 || widths[2] != 10 || widths[3] != 10 {
		t.Errorf("row widths = %v, want the two added rows 10 wide", widths)
	}
	requireSameCells(t, "long line, taller screen", placeFrame(inner, 6, 4), got)
}

func TestPadFrameCompletesAShortFrameWithBackgroundRows(t *testing.T) {
	useDefaultTheme(t)
	got := padFrame("abcdef\nabcdef", 6, 5)

	rows := testutil.Cells(got)
	if len(rows) != 5 {
		t.Fatalf("got %d rows, want 5: %q", len(rows), got)
	}
	bg := theme.Hex(theme.ColorBackground)
	for r := 2; r < 5; r++ {
		if len(rows[r]) != 6 {
			t.Errorf("row %d has %d cells, want 6", r, len(rows[r]))
		}
		for c, cell := range rows[r] {
			if cell.BG != bg || cell.Glyph != " " {
				t.Errorf("row %d cell %d = %+v, want a space on %s (Rule 115)", r, c, cell, bg)
			}
		}
	}
	if strings.HasSuffix(got, "\n") {
		t.Errorf("a trailing newline was added: %q", got)
	}
}

func TestPadFrameNeverAddsAnythingForANonPositiveSize(t *testing.T) {
	useDefaultTheme(t)
	for _, inner := range []string{"", "ab", "ab\nabcd", "a\n\nabc"} {
		for _, size := range [][2]int{{0, 0}, {-3, -3}, {0, 5}, {5, 0}, {-1, 4}, {4, -1}} {
			w, h := size[0], size[1]
			got := padFrame(inner, w, h)
			requireSameCells(t, "inner "+strings.ReplaceAll(inner, "\n", `\n`)+" size "+sizeLabel(w, h), placeFrame(inner, w, h), got)
			if w <= 0 && h <= 0 && got != inner {
				t.Errorf("padFrame(%q, %d, %d) = %q, want it unchanged", inner, w, h, got)
			}
		}
	}
}

func sizeLabel(w, h int) string { return fmt.Sprintf("%dx%d", w, h) }

// Width is measured by x/ansi (grapheme clusters), not by runewidth: a VS16
// emoji is two cells there (Rule 116), and a sequence has no width at all.
func TestPadFrameMeasuresWithAnsi(t *testing.T) {
	useDefaultTheme(t)
	lines := []string{
		"\x1b[31mred\x1b[m",
		"hearts ❤️ and ✔️ ok",
		"wide 世界",
		"flag 🇫🇷 family 👨‍👩‍👧",
		"\x1b[48;2;1;2;3mown background\x1b[m",
		"combining é",
	}
	inner := strings.Join(lines, "\n")
	for _, width := range []int{20, 24, 40} {
		got := padFrame(inner, width, len(lines))
		for i, w := range rowWidths(got) {
			if want := max(width, ansi.StringWidth(lines[i])); w != want {
				t.Errorf("width %d, line %d (%q): %d cells, want %d", width, i, lines[i], w, want)
			}
		}
		requireSameCells(t, fmt.Sprintf("ansi lines at width %d", width), placeFrame(inner, width, len(lines)), got)
	}
}

// A tab or a CRLF changes what Place measures (it rewrites them first); the
// frame is then handed to Place itself, so the answer stays the same.
func TestPadFrameKeepsPlacesAnswerForTabsAndCRLF(t *testing.T) {
	useDefaultTheme(t)
	for _, inner := range []string{"a\tb\nc", "ab\r\ncd", "tab\there\r\nand\tmore"} {
		got := padFrame(inner, 20, 4)
		if want := placeFrame(inner, 20, 4); got != want {
			t.Errorf("padFrame(%q) = %q, want Place's %q", inner, got, want)
		}
	}
}

// The common case, a frame with nothing to fill, allocates nothing.
func TestPadFrameAllocatesNothingWhenThereIsNothingToFill(t *testing.T) {
	useDefaultTheme(t)
	line := theme.BgLine("row", 80)
	inner := strings.Repeat(line+"\n", 23) + line
	var sink string
	if n := testing.AllocsPerRun(20, func() { sink = padFrame(inner, 80, 24) }); n != 0 {
		t.Errorf("%v allocations on a full frame, want 0", n)
	}
	if sink != inner {
		t.Error("the frame changed")
	}

	// Nothing to fill whichever line is the full one, the last included: Place
	// pads nothing once one line fills the width.
	late := "a\nab\n" + line + "\nabc"
	if n := testing.AllocsPerRun(20, func() { sink = padFrame(late, 80, 4) }); n != 0 {
		t.Errorf("%v allocations when only the third line is full width, want 0", n)
	}
	if sink != late {
		t.Error("the frame changed")
	}
}

// ragged is a block whose lines have different widths, so that padding is real.
func raggedFrames() map[string]string {
	return map[string]string{
		"ragged":         "a\nabc\n\nabcdefgh\nab",
		"styled":         theme.BgLine("styled", 10) + "\n" + theme.KeyStyle.Render("key") + "\n\x1b[1mbold\x1b[m",
		"trailing-reset": "plain\n\x1b[38;2;255;0;0mred to the end\x1b[0m\nok",
		"wide":           "世界\n❤️ ✔️\nflag 🇫🇷",
		"one-line":       "only one line",
		"empty":          "",
		"blank-lines":    "\n\n\n",
		"wide-last":      "a\nab\nabcdefghijklmnop",
		"wide-first":     "abcdefghijklmnop\nab\na",
	}
}

func TestPadFrameMatchesPlaceOnRaggedBlocks(t *testing.T) {
	useDefaultTheme(t)
	for name, inner := range raggedFrames() {
		for _, size := range [][2]int{{20, 3}, {20, 8}, {12, 12}, {120, 40}, {5, 2}, {1, 1}} {
			w, h := size[0], size[1]
			requireSameCells(t, name+" at "+sizeLabel(w, h), placeFrame(inner, w, h), padFrame(inner, w, h))
		}
	}
}

// The net of the track, on real frames. Most frames of the cell goldens and of
// the other sizes are already width x height with a full-width header line, so
// for them padFrame returns inner and the comparison with Place is inner against
// itself: it guards that nothing is added, not that padding is right. Two things
// make it bite: the short-frame golden (a stacked frame a row short, which only
// the padding pass completes), and the frames of the framed view stacked at 120x40
// then padded for a bigger screen, where both columns and rows are missing. The
// test counts the frames that really need filling, so the corpus cannot drift
// into the first kind only.
//
// A case whose frame is an overlay (help) never reaches the padding pass and is
// skipped; the palette case is the same frame with the palette drawn after it.
func TestPadFrameMatchesPlaceOnTheFrames(t *testing.T) {
	useDefaultTheme(t)
	type corpusFrame struct {
		name string
		app  *App
	}
	var corpus []corpusFrame
	for _, c := range frameCases() {
		corpus = append(corpus, corpusFrame{c.name, c.build(t)})
	}
	for _, size := range [][2]int{{80, 24}, {60, 12}, {200, 50}, {40, 10}} {
		a := trueColorRouter(t, framedView(), size[0], size[1])
		corpus = append(corpus, corpusFrame{"framed at " + sizeLabel(size[0], size[1]), a})
	}

	checked, filled := 0, 0
	for _, c := range corpus {
		if c.app.activeOverlay() != "" {
			continue
		}
		inner := innerFrame(c.app)
		want := placeFrame(inner, c.app.width, c.app.height)
		got := padFrame(inner, c.app.width, c.app.height)
		requireSameCells(t, c.name, want, got)
		if got != inner {
			filled++
		}

		// The helper must be what Render stacks: with no palette the frame
		// Render returns is the padded one.
		if len(c.app.paletteLines()) == 0 {
			requireSameCells(t, c.name+" (Render)", want, c.app.Render())
		}
		checked++
	}
	if checked < 8 {
		t.Errorf("only %d frames checked: the corpus shrank", checked)
	}

	// A real frame shorter than the screen: the framed view stacked for 120x40,
	// then padded for a screen with more rows, more columns, or both.
	inner := innerFrame(trueColorRouter(t, framedView(), framesWidth, framesHeight))
	for _, size := range [][2]int{{framesWidth, framesHeight + 4}, {framesWidth + 10, framesHeight}, {framesWidth + 10, framesHeight + 4}} {
		w, h := size[0], size[1]
		got := padFrame(inner, w, h)
		requireSameCells(t, "framed frame padded to "+sizeLabel(w, h), placeFrame(inner, w, h), got)
		if got == inner {
			t.Errorf("padFrame left a %dx%d frame as it was for a %s screen", framesWidth, framesHeight, sizeLabel(w, h))
		}
		filled++
	}
	if filled < 4 {
		t.Errorf("only %d frames needed filling: the test no longer exercises the padding", filled)
	}
}
