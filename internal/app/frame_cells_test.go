package app

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The frame, compared by cells (DEVDESK-281, task 281.2).
//
// The router assembles the frame in a few passes (renderBody's Style.Width,
// fitHeight, the whole-frame lipgloss.Place, the palette overlay). Changing how
// one of them pads changes bytes without necessarily changing what a terminal
// shows, so the safety net is not a byte comparison: testutil.Cells decodes the
// frame into (glyph, foreground, background) per cell, and
//
//   - TestFrameHasNoBareCell holds Rule 115: no cell of the frame is left on the
//     terminal's default background, and the frame is exactly width x height;
//   - TestFrameCellsGolden compares the whole decoded frame, case by case,
//     against internal/app/testdata/frames/<case>.cells.
//
// Both run at 120x40 with the TrueColor profile, on the default theme.
//
// Regenerate the goldens after an intended rendering change, then read the diff
// as cells:
//
//	go test ./internal/app -run TestFrameCellsGolden -update-frames -count=1
//
// The flag is the package's own: pass it with the package path, not ./...

var updateFrames = flag.Bool("update-frames", false, "rewrite internal/app/testdata/frames/*.cells from the current frames")

const (
	framesWidth  = 120
	framesHeight = 40
	framesDir    = "testdata/frames"
)

// cellsView is a footedView whose footer carries its own background: footedView's
// writes "FOOTER" with none, and this net would rightly call those cells bare.
type cellsView struct{ footedView }

func (v *cellsView) RenderFooter(width int) string {
	return theme.EmptyLineBg(width) + "\n" + theme.BgLine("FOOTER", width)
}

// Update keeps the wrapper: the promoted one would hand the router the inner view.
func (v *cellsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := v.footedView.Update(msg)
	return v, cmd
}

func newCellsView(title, body string) *cellsView {
	return &cellsView{footedView{fakeView{title: title, body: body}}}
}

// shortFooterView is a cellsView whose footer is one line shorter than the
// GetFooterHeight it declares, so the stacked frame is a row short of the
// screen. The router does not fill that row: the whole-frame lipgloss.Place
// does, and it is the one thing Place still pads in practice (it never adds
// columns once a line, the header's, is as wide as the screen). This is the
// case that guards what a different padding pass must keep doing.
type shortFooterView struct{ cellsView }

func (v *shortFooterView) RenderFooter(width int) string {
	return theme.BgLine("a footer one line short of its declared height", width)
}

func (v *shortFooterView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := v.cellsView.Update(msg)
	return v, cmd
}

// framelessCells is a cellsView that draws its own frames (FramelessView).
type framelessCells struct{ cellsView }

func (v *framelessCells) Frameless() bool { return true }

func (v *framelessCells) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := v.cellsView.Update(msg)
	return v, cmd
}

// dimOnBackground is a dim run that keeps the background (Rule 115): unlike
// theme.DimStyle, whose reset would leave the cells after it on the terminal's.
func dimOnBackground(s string) string {
	return lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorDim).Render(s)
}

// cellsBody is a body of ordinary rows, every styled run on the background
// (Rule 115): plain text, a row padded with the
// background, coloured runs, wide and emoji glyphs, a Nerd Font icon, blank rows.
func cellsBody() string {
	inner := framesWidth - 2
	return strings.Join([]string{
		"",
		"  plain row, no style at all",
		theme.PadWithBg("  row padded with the background", inner),
		theme.Bg("  ") + theme.KeyStyle.Render("key") + theme.Bg(" ") + dimOnBackground("dim text") + theme.Bg(" then plain"),
		theme.Bg("  ") + theme.StatusErrorStyle.Render("error") + theme.Bg(" on the background"),
		"  wide: 世界  emoji: ❤️ ✔️  icon: " + theme.IconOK + " ok",
		"",
		"  last row",
	}, "\n")
}

// overflowBody has rows wider than the inner width (118 cells here): plain,
// styled, and one whose wide glyphs straddle the right edge.
func overflowBody() string {
	return strings.Join([]string{
		"short row",
		strings.Repeat("0123456789", 20),
		theme.KeyStyle.Render(strings.Repeat("k", 150)),
		strings.Repeat("a", 115) + "世世世" + "tail",
		"short row again",
	}, "\n")
}

// resetBody has rows that end on a reset (\x1b[m or \x1b[0m): the reset drops the
// background the viewport style set, so the padding after it is covered only if
// something repaints it.
func resetBody() string {
	return strings.Join([]string{
		"plain, then \x1b[1mbold to the end of the row\x1b[m",
		"\x1b[38;2;255;0;0mred to the end\x1b[0m",
		"\x1b[48;2;1;2;3mown background to the end\x1b[m",
		"ordinary row",
	}, "\n")
}

// resetThenTextBody has rows where text follows the reset. The text is not
// padding: whoever wrote the row left it without a background, and the router
// does not repair it.
func resetThenTextBody() string {
	return strings.Join([]string{
		"plain, then \x1b[1mbold\x1b[m and the rest after a reset",
		"\x1b[38;2;255;0;0mred\x1b[0m   spaces after a 0m reset",
		"\x1b[48;2;1;2;3mown background\x1b[m then plain text",
		"ordinary row",
	}, "\n")
}

// frameCase is one frame the net covers.
type frameCase struct {
	name  string
	build func(t *testing.T) *App
	// knownBare, when set, says why this case is known to leave cells on the
	// terminal's default background on the current code. The case then logs
	// them instead of failing, and fails once there are none, so the marker
	// cannot outlive the gap.
	knownBare string
}

func framedView() *cellsView { return newCellsView("Framed view", cellsBody()) }

func frameCases() []frameCase {
	atSize := func(t *testing.T, v Screen) *App { return trueColorRouter(t, v, framesWidth, framesHeight) }
	return []frameCase{
		{name: "framed", build: func(t *testing.T) *App { return atSize(t, framedView()) }},
		{name: "frameless", build: func(t *testing.T) *App {
			a := atSize(t, &framelessCells{*newCellsView("Frameless view", cellsBody())})
			if !a.frameless() {
				t.Fatal("the view is not frameless")
			}
			return a
		}},
		{name: "overflow", build: func(t *testing.T) *App {
			return atSize(t, newCellsView("Rows wider than the viewport", overflowBody()))
		}},
		{name: "short-frame", build: func(t *testing.T) *App {
			a := atSize(t, &shortFooterView{*newCellsView("A frame a row short", cellsBody())})
			inner := fitHeight(a.renderHeader()+"\n"+a.renderTitleLine(), a.renderBody(false), a.renderViewFooter(), a.height)
			if got := strings.Count(inner, "\n") + 1; got != framesHeight-1 {
				t.Fatalf("the stacked frame has %d rows, want %d: the case no longer needs Place", got, framesHeight-1)
			}
			return a
		}},
		{name: "reset", build: func(t *testing.T) *App {
			return atSize(t, newCellsView("Rows ending on a reset", resetBody()))
		}},
		{
			name: "reset-then-text",
			build: func(t *testing.T) *App {
				return atSize(t, newCellsView("Text after a reset", resetThenTextBody()))
			},
			knownBare: "text written after a reset in a view's row keeps no background: " +
				"renderBody's Style.Background is dropped by the reset and nothing repaints it",
		},
		{name: "help", build: func(t *testing.T) *App {
			v := framedView()
			v.helpContent = help.Content{
				Title:       "Frame test help",
				Description: "A view whose help is open: the overlay replaces the whole frame.",
				KeyBindings: []help.KeyBinding{{Key: "enter", Description: "Open"}, {Key: "esc", Description: "Go back"}},
				Sections:    []help.Section{{Title: "Notes", Body: "Some body text."}},
			}
			a := atSize(t, v)
			a.handleKeyMsg(testutil.Key("?"))
			if !a.showHelp {
				t.Fatal("help did not open")
			}
			return a
		}},
		{name: "palette", build: func(t *testing.T) *App {
			a := atSize(t, framedView())
			press(t, a, "ctrl+p")
			a.suggestions = fixedPaletteSuggestions(a)
			a.sugCursor, a.sugOffset = -1, 0
			if len(a.paletteLines()) == 0 {
				t.Fatal("the palette did not open")
			}
			return a
		}},
	}
}

// fixedPaletteSuggestions is the list the palette case draws: a fixed set of
// entries instead of command.Entries(), so adding, renaming or re-describing a
// real command does not change this golden. Twelve rows against the eight the
// palette shows at this size keep the "more" indicator in the frame; one row is
// the open view (the "(current)" mark is the router's, from a.currentView), one
// takes an argument, and one description is long enough to be cut.
func fixedPaletteSuggestions(a *App) command.Suggestions {
	entries := []command.Entry{
		{Name: "alpha", Aliases: []string{"a"}, Description: "First fixed view"},
		{Name: "bravo", Aliases: []string{"b", "brv"}, Description: "Second fixed view, two aliases"},
		{Name: string(a.currentView), Aliases: []string{"cur"}, Description: "Long description, cut by the current mark"},
		{Name: "delta", Description: "A view with no alias"},
		{Name: "echo", Aliases: []string{"e", "ech", "ecc"}, Description: "Three aliases, joined by a dot"},
		{Name: "foxtrot", Aliases: []string{"f"}, Description: "One more fixed view"},
		{Name: "golf", Aliases: []string{"g"}, Description: "Seventh fixed view"},
		{Name: "hotel", Aliases: []string{"h"}, Description: "Eighth fixed view"},
		{Name: "india", Aliases: []string{"i"}, Description: "Ninth fixed view, below the fold"},
		{Name: "juliet", Aliases: []string{"j"}, Description: "Tenth fixed view, below the fold"},
		{Name: "kilo", Arg: "<name>", Kind: command.KindAction, Description: "An action that takes an argument"},
		{Name: "lima", Kind: command.KindAction, Description: "An action with no argument"},
	}
	return command.Suggestions{Stage: command.StageCommand, Entries: entries, Total: len(entries)}
}

// useDefaultTheme makes the frame independent of the theme an earlier test left.
func useDefaultTheme(t *testing.T) {
	t.Helper()
	theme.ApplyTheme(theme.DefaultTheme())
}

func TestFrameHasNoBareCell(t *testing.T) {
	useDefaultTheme(t)
	for _, c := range frameCases() {
		t.Run(c.name, func(t *testing.T) {
			frame := c.build(t).Render()

			// A missing cell is as bare as one on the default background.
			rows := testutil.Cells(frame)
			if len(rows) != framesHeight {
				t.Errorf("frame has %d rows, want %d", len(rows), framesHeight)
			}
			for r, row := range rows {
				if len(row) != framesWidth {
					t.Errorf("row %d has %d cells, want %d", r, len(row), framesWidth)
					break
				}
			}

			// BareCells sees a default background only. A reversed cell whose
			// foreground was the terminal's default has "default-fg" as its
			// background, and one reversed over a default background has
			// "default-bg": neither is DefaultColor, and both leave the cell's
			// fill to the terminal's palette. No case has one today (the
			// palette's input cursor is reversed too, but over ColorText: its
			// background is #rrggbb and only its foreground is "default-bg"), so
			// none is allowed; a case that needs one lists it here with a reason.
			if got := reversedDefaultBackgrounds(rows); len(got) > 0 {
				t.Errorf("%d cells whose background is a reversed terminal default (default-fg or default-bg)\n%s", len(got), describeBare(got))
			}

			bare := testutil.BareCells(frame)
			switch {
			case len(bare) == 0 && c.knownBare != "":
				t.Errorf("no bare cell any more: remove knownBare (%q) from the case", c.knownBare)
			case len(bare) > 0 && c.knownBare != "":
				t.Logf("KNOWN GAP, not asserted: %d bare cells (%s)\n%s", len(bare), c.knownBare, describeBare(bare))
			case len(bare) > 0:
				t.Errorf("%d cells on the terminal's default background (Rule 115)\n%s", len(bare), describeBare(bare))
			}
		})
	}
}

// The decoder reads the sequence Lip Gloss really writes for an invalid field
// (Rule 140: curly underline plus an underline colour, SGR 58): the cells keep
// the background and the foreground the style gave them.
func TestCellsDecodeTheInvalidFieldUnderline(t *testing.T) {
	useDefaultTheme(t)
	base := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText)
	out := theme.StyleInvalidField(base).Render("bad")
	if !strings.Contains(out, "58;") {
		t.Fatalf("premise changed: no SGR 58 in %q", out)
	}
	wantBG, wantFG := theme.Hex(theme.ColorBackground), theme.Hex(theme.ColorText)
	rows := testutil.Cells(out)
	if len(rows) != 1 || len(rows[0]) != 3 {
		t.Fatalf("Cells(%q) = %#v, want 3 cells", out, rows)
	}
	for i, c := range rows[0] {
		if c.BG != wantBG || c.FG != wantFG {
			t.Errorf("cell %d = %+v, want fg %s bg %s", i, c, wantFG, wantBG)
		}
	}
}

// reversedDefaultBackgrounds returns the position of every cell whose
// background is testutil.ReversedDefaultFG or testutil.ReversedDefaultBG, in
// reading order.
func reversedDefaultBackgrounds(rows [][]testutil.Cell) []testutil.Pos {
	var out []testutil.Pos
	for r, row := range rows {
		for c, cell := range row {
			if cell.BG == testutil.ReversedDefaultFG || cell.BG == testutil.ReversedDefaultBG {
				out = append(out, testutil.Pos{Row: r, Col: c})
			}
		}
	}
	return out
}

// describeBare summarises bare cells by row: the row, how many, and the columns
// of the first and last.
func describeBare(bare []testutil.Pos) string {
	var b strings.Builder
	shown := 0
	for i := 0; i < len(bare); {
		j := i
		for j < len(bare) && bare[j].Row == bare[i].Row {
			j++
		}
		if shown++; shown > 12 {
			fmt.Fprintf(&b, "  ... %d more rows\n", countRows(bare[i:]))
			break
		}
		fmt.Fprintf(&b, "  row %d: %d cells, columns %d to %d\n", bare[i].Row, j-i, bare[i].Col, bare[j-1].Col)
		i = j
	}
	return b.String()
}

func countRows(bare []testutil.Pos) int {
	n := 0
	for i, p := range bare {
		if i == 0 || p.Row != bare[i-1].Row {
			n++
		}
	}
	return n
}

func TestFrameCellsGolden(t *testing.T) {
	useDefaultTheme(t)
	for _, c := range frameCases() {
		t.Run(c.name, func(t *testing.T) {
			got := testutil.FormatCells(testutil.Cells(c.build(t).Render()))
			path := filepath.Join(framesDir, c.name+".cells")
			if *updateFrames {
				if err := os.MkdirAll(framesDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v\nrecord it with: go test ./internal/app -run TestFrameCellsGolden -update-frames -count=1", err)
			}
			if got != string(want) {
				t.Errorf("the frame's cells changed (%s); first differences:\n%s\nif intended: go test ./internal/app -run TestFrameCellsGolden -update-frames -count=1",
					path, firstDifferences(string(want), got, 10))
			}
		})
	}
}

// firstDifferences lists up to limit lines where want and got differ.
func firstDifferences(want, got string, limit int) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	n := 0
	for i := 0; i < max(len(w), len(g)); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl == gl {
			continue
		}
		fmt.Fprintf(&b, "  line %d\n    want %q\n    got  %q\n", i+1, wl, gl)
		if n++; n >= limit {
			b.WriteString("  ...\n")
			break
		}
	}
	return b.String()
}

// The check above is only worth having if it can fail: a reversed default
// foreground (ESC[7m over a plain cell) is not a bare cell for BareCells.
func TestReversedDefaultBackgroundsSeesWhatBareCellsMisses(t *testing.T) {
	frame := "\x1b[48;2;1;2;3mab\x1b[7;49m \x1b[m"
	if got := testutil.BareCells(frame); len(got) != 0 {
		t.Fatalf("premise changed: BareCells = %v, want none for a reversed cell", got)
	}
	want := []testutil.Pos{{Row: 0, Col: 2}}
	if got := reversedDefaultBackgrounds(testutil.Cells(frame)); !reflect.DeepEqual(got, want) {
		t.Fatalf("reversedDefaultBackgrounds = %v, want %v", got, want)
	}
}
