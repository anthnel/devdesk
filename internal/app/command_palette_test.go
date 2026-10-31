package app

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-109: the command palette is an overlay on the rendered screen. The
// permanent command line (and its two blank lines) is gone from the header, the
// viewport gains the two lines, and opening or closing changes no height.
// The tests below read the screen back (see findPaletteFrame) rather than the
// router's fields, so they hold for any implementation of the overlay.

// ── Closed: the layout without a command line ────────────────────────────────

func TestClosedLayoutGivesTheViewportTwoMoreLines(t *testing.T) {
	for _, sz := range paletteSizes {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			view := footedFake(fullWidthBody())
			a := routerAt(t, view, sz[0], sz[1])
			lines := screen(a)

			if len(lines) != sz[1] {
				t.Fatalf("View is %d lines, want %d", len(lines), sz[1])
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != sz[0] {
					t.Errorf("line %d is %d cells wide, want %d", i, w, sz[0])
				}
				if strings.Contains(l, "❯") {
					t.Errorf("line %d shows a command line prompt on the closed screen: %q", i, l)
				}
			}
			if h := lipgloss.Height(a.renderHeader()); h != paletteHeaderLines {
				t.Errorf("header block is %d lines, want %d", h, paletteHeaderLines)
			}
			if strings.TrimSpace(lines[7]) != "" {
				t.Errorf("line 7 should be the blank line under the header, got %q", lines[7])
			}
			if !strings.HasPrefix(lines[8], "┌") {
				t.Errorf("line 8 should be the viewport's title border, got %q", lines[8])
			}
			// 7 header + 1 blank + title line + viewport + 2-line footer.
			if want := sz[1] - paletteHeaderLines - 2 - 1; a.viewport.Height != want {
				t.Errorf("viewport height = %d, want %d", a.viewport.Height, want)
			}
			size, ok := lastWindowSize(&view.fakeView)
			if !ok {
				t.Fatal("the view was never told its size")
			}
			if want := sz[1] - paletteHeaderLines - 2 - 2; size.Height != want {
				t.Errorf("the view was told height %d, want %d (two more than with the command line)", size.Height, want)
			}
			if !strings.Contains(lines[sz[1]-1], "FOOTER") {
				t.Errorf("the footer is not the last line: %q", lines[sz[1]-1])
			}
		})
	}
}

// ── Opening ──────────────────────────────────────────────────────────────────

func TestOpeningKeysDrawThePaletteAndColonInAFieldDoesNot(t *testing.T) {
	t.Run("ctrl+p from a focused field", func(t *testing.T) {
		a := router(t, &editingView{editing: true})
		a.handleKeyMsg(testutil.Key(commandModeKey))
		mustFrame(t, a)
	})
	t.Run("colon with nothing focused", func(t *testing.T) {
		a := router(t, &editingView{})
		a.handleKeyMsg(testutil.Key(":"))
		mustFrame(t, a)
	})
	t.Run("colon in a focused field is a character", func(t *testing.T) {
		view := &editingView{editing: true}
		a := router(t, view)
		a.handleKeyMsg(testutil.Key(":"))
		if _, drawn := findPaletteFrame(screen(a)); drawn || a.commandMode {
			t.Error("the palette opened on a colon typed in a field")
		}
		if got := view.keysSeen(); len(got) != 1 || got[0] != ":" {
			t.Errorf("the field received %v, want the colon", got)
		}
	})
}

// ── Position: under the header, centered, sized to the width ─────────────────

func TestThePaletteNeverCoversTheHeader(t *testing.T) {
	for _, sz := range paletteSizes {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			view := footedFake(fullWidthBody())
			a := routerAt(t, view, sz[0], sz[1])
			press(t, a, "ctrl+p")

			lines := screen(a)
			want := strings.Split(ansi.Strip(renderHeaderContent(view.GetHeaderInfo(a.currentContext), a.commandLineShortcuts(), a.width)), "\n")
			for i := 0; i < 7; i++ {
				if lines[i] != want[i] {
					t.Errorf("header row %d changed under the open palette:\n got %q\nwant %q", i, lines[i], want[i])
				}
			}
			if strings.TrimSpace(lines[7]) != "" {
				t.Errorf("the blank line under the header was used: %q", lines[7])
			}
			if f := mustFrame(t, a); f.Y != paletteHeaderLines {
				t.Errorf("the frame starts on row %d, want %d (right under the blank line)", f.Y, paletteHeaderLines)
			}
		})
	}
}

func TestThePaletteIsCenteredAndSized(t *testing.T) {
	cases := []struct{ width, wantX, wantW int }{
		{80, 2, 76},
		{53, 0, 53}, // under 54 columns: the full width
		{54, 2, 50},
		{120, 20, 80}, // capped at 80
		{200, 60, 80},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d", tc.width), func(t *testing.T) {
			a := routerAt(t, footedFake(fullWidthBody()), tc.width, 30)
			closed := screen(a)
			press(t, a, "ctrl+p")
			open := screen(a)

			f := mustFrame(t, a)
			if f.X != tc.wantX || f.Width != tc.wantW || f.Y != paletteHeaderLines {
				t.Fatalf("frame at x=%d y=%d width=%d, want x=%d y=%d width=%d", f.X, f.Y, f.Width, tc.wantX, paletteHeaderLines, tc.wantW)
			}
			if !strings.HasPrefix(f.Lines[0], "┌─ Go to ") || !strings.HasSuffix(f.Lines[0], "┐") {
				t.Errorf("top border %q is not ┌─ Go to …┐", f.Lines[0])
			}
			for i, l := range f.Lines {
				if w := ansi.StringWidth(l); w != tc.wantW {
					t.Errorf("frame line %d is %d cells wide, want %d: %q", i, w, tc.wantW, l)
				}
				// Outside the frame, on each side, the view is as it was.
				row := f.Y + i
				if left := ansi.Cut(open[row], 0, f.X); left != ansi.Cut(closed[row], 0, f.X) {
					t.Errorf("row %d, left of the frame: %q, was %q", row, left, ansi.Cut(closed[row], 0, f.X))
				}
				if right := ansi.Cut(open[row], f.X+f.Width, tc.width); right != ansi.Cut(closed[row], f.X+f.Width, tc.width) {
					t.Errorf("row %d, right of the frame: %q, was %q", row, right, ansi.Cut(closed[row], f.X+f.Width, tc.width))
				}
			}
			if tc.wantX > 0 && strings.TrimSpace(ansi.Cut(closed[f.Y+3], 0, tc.wantX)) == "" {
				t.Error("test setup: nothing of the view is visible left of the frame to compare")
			}
		})
	}
}

func TestThePaletteKeepsTheViewVisibleBelow(t *testing.T) {
	a := routerAt(t, footedFake(fullWidthBody()), 80, 40)
	closed := screen(a)
	press(t, a, "ctrl+p")
	f := mustFrame(t, a)
	open := screen(a)

	below := f.Y + len(f.Lines)
	if below >= len(open)-3 {
		t.Fatalf("test setup: the frame ends at row %d of %d, nothing below to compare", below, len(open))
	}
	for row := below; row < len(open); row++ {
		if open[row] != closed[row] {
			t.Errorf("row %d under the frame changed:\n got %q\nwant %q", row, open[row], closed[row])
		}
	}
	if !strings.Contains(open[len(open)-1], "FOOTER") {
		t.Errorf("the footer is not the last row: %q", open[len(open)-1])
	}
}

// ── Content and size ─────────────────────────────────────────────────────────

// n = min(8, V-4) with V = H - 8 - footer; the frame is n+4 rows, and 3 rows
// when n is 0. 40x16 with the usual 2-line footer gives n = 2.
func TestThePaletteRowsFollowTheFreeHeight(t *testing.T) {
	cases := []struct {
		w, h, footer int
		wantRows     int
	}{
		{80, 24, 2, 8},
		{120, 40, 2, 8},
		{60, 30, 2, 8},
		{40, 16, 2, 2},
		{40, 16, 0, 4},
		{80, 14, 2, 0}, // V = 4
		{80, 15, 2, 1}, // V = 5
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%dx%d/footer%d", tc.w, tc.h, tc.footer), func(t *testing.T) {
			var view tea.Model = &fakeView{body: fullWidthBody()}
			if tc.footer == 2 {
				view = footedFake(fullWidthBody())
			}
			a := routerAt(t, view, tc.w, tc.h)
			press(t, a, "ctrl+p")

			f := mustFrame(t, a)
			if got := len(f.rows()); got != tc.wantRows {
				t.Errorf("%d rows, want %d", got, tc.wantRows)
			}
			wantHeight := tc.wantRows + 4
			if tc.wantRows == 0 {
				wantHeight = 3
			}
			if len(f.Lines) != wantHeight {
				t.Errorf("frame is %d lines, want %d:\n%s", len(f.Lines), wantHeight, strings.Join(f.Lines, "\n"))
			}
		})
	}
}

func TestPaletteHeightIsConstantWhileTyping(t *testing.T) {
	for _, sz := range paletteSizes {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			a := routerAt(t, footedFake(fullWidthBody()), sz[0], sz[1])
			press(t, a, "ctrl+p")
			ref := mustFrame(t, a)

			steps := []func(){
				func() { typeCommand(t, a, "c") },
				func() { typeCommand(t, a, "o") },
				func() { typeCommand(t, a, "n") },
				func() { press(t, a, "down", "down") },
				func() { press(t, a, "backspace", "backspace", "backspace") },
				func() { typeCommand(t, a, "zz") },
				func() { press(t, a, "enter") }, // the warning
				func() {
					press(t, a, "backspace", "backspace", "down", "down", "down", "down", "down", "down", "down", "down", "down")
				},
			}
			for i, step := range steps {
				step()
				f := mustFrame(t, a)
				if f.X != ref.X || f.Y != ref.Y || f.Width != ref.Width || len(f.Lines) != len(ref.Lines) {
					t.Errorf("step %d: frame x=%d y=%d w=%d h=%d, want x=%d y=%d w=%d h=%d",
						i, f.X, f.Y, f.Width, len(f.Lines), ref.X, ref.Y, ref.Width, len(ref.Lines))
				}
			}
		})
	}
}

func TestPaletteTitle(t *testing.T) {
	t.Run("command stage", func(t *testing.T) {
		a := commanding(t, &fakeView{})
		typeCommand(t, a, "ct")
		if top := mustFrame(t, a).Lines[0]; !strings.HasPrefix(top, "┌─ Go to ") {
			t.Errorf("title line %q, want Go to", top)
		}
	})
	t.Run("ctx argument stage", func(t *testing.T) {
		a := argRouter(t)
		ctxOpen(t, a, "ctx ")
		if top := mustFrame(t, a).Lines[0]; !strings.HasPrefix(top, "┌─ Contexts ") {
			t.Errorf("title line %q, want Contexts", top)
		}
	})
}

// "ctx <name>" lives in the same grid: same place, same size, other title.
func TestTheContextStageIsDrawnInTheSameFrame(t *testing.T) {
	a := argRouter(t)
	loadContexts(t, a)
	typeCommand(t, a, "ct")
	ref := mustFrame(t, a)

	press(t, a, "backspace", "backspace")
	typeCommand(t, a, "ctx w")
	f := mustFrame(t, a)
	if f.X != ref.X || f.Y != ref.Y || f.Width != ref.Width || len(f.Lines) != len(ref.Lines) {
		t.Errorf("the argument stage moved or resized the frame: x=%d y=%d w=%d h=%d, was x=%d y=%d w=%d h=%d",
			f.X, f.Y, f.Width, len(f.Lines), ref.X, ref.Y, ref.Width, len(ref.Lines))
	}
	if !hasRowWith(f.rows(), "web") {
		t.Errorf("rows %q lack the matching context", f.rows())
	}
}

// ── Keys ─────────────────────────────────────────────────────────────────────

func TestEnterOnAnUnknownCommandWarnsOnTheRuleAndStaysOpen(t *testing.T) {
	for _, sz := range paletteSizes {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			a := routerAt(t, footedFake("body"), sz[0], sz[1])
			press(t, a, "ctrl+p")
			typeCommand(t, a, "zzz")
			press(t, a, "enter")

			if !a.commandMode || a.currentView != command.ViewDashboard {
				t.Errorf("commandMode=%v view=%s: an unknown command did not leave the palette open", a.commandMode, a.currentView)
			}
			if got, want := filet(t, a), theme.IconWarning+` "zzz" is not a view or a command`; !strings.Contains(got, want) {
				t.Errorf("rule %q lacks %q", got, want)
			}
		})
	}
}

// ── Reduced frame under five free lines (Rule 130: no silent refusal) ────────

func TestReducedFrameUnderFiveLines(t *testing.T) {
	for _, h := range []int{14, 13} { // V = 4 and 3 with the 2-line footer
		t.Run(fmt.Sprintf("80x%d", h), func(t *testing.T) {
			a := routerAt(t, footedFake("body"), 80, h)
			press(t, a, "ctrl+p")
			typeCommand(t, a, "co")

			f := mustFrame(t, a)
			if len(f.Lines) != 3 {
				t.Fatalf("frame is %d lines, want 3:\n%s", len(f.Lines), strings.Join(f.Lines, "\n"))
			}
			press(t, a, "enter")
			if got := filet(t, a); !strings.Contains(got, theme.IconWarning) {
				t.Errorf("enter on a prefix did not warn on the rule: %q", got)
			}
			press(t, a, "backspace", "backspace")
			typeCommand(t, a, "containers")
			press(t, a, "enter")
			if a.currentView != command.ViewContainers || a.commandMode {
				t.Errorf("view=%s commandMode=%v, want containers with the palette closed", a.currentView, a.commandMode)
			}
		})
	}
}

// ── No fragment, no wrong width (the overlay never cuts an escape sequence) ──

// styledBody gives every cell of every row a colour of its own, plus a row of
// double-width glyphs, so a cut anywhere lands inside a sequence or a glyph.
func styledBody(width, rows int) string {
	lines := make([]string, rows)
	for r := range lines {
		var b strings.Builder
		for c := 0; c < width; c++ {
			color := lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", (c*7+r*13)%256, (c*11+r*3)%256, (c*5+r*17)%256))
			ch := string(rune('a' + (c+r)%26))
			if r%5 == 4 {
				ch = "日"
				c++
			}
			b.WriteString(lipgloss.NewStyle().Foreground(color).Background(theme.ColorBackground).Render(ch))
		}
		lines[r] = b.String()
	}
	return strings.Join(lines, "\n")
}

func TestTheOverlayNeverCutsAnEscapeSequence(t *testing.T) {
	testutil.TrueColor(t)
	steps := map[string]func(*testing.T, *App){
		"open":    func(*testing.T, *App) {},
		"matches": func(t *testing.T, a *App) { typeCommand(t, a, "co") },
		"warning": func(t *testing.T, a *App) { typeCommand(t, a, "zz"); press(t, a, "enter") },
	}
	for _, sz := range paletteSizes {
		for name, step := range steps {
			t.Run(fmt.Sprintf("%dx%d/%s", sz[0], sz[1], name), func(t *testing.T) {
				a := routerAt(t, footedFake(styledBody(sz[0], sz[1])), sz[0], sz[1])
				press(t, a, "ctrl+p")
				step(t, a)

				raw := strings.Split(a.View(), "\n")
				if len(raw) != sz[1] {
					t.Fatalf("View is %d lines, want %d", len(raw), sz[1])
				}
				for i, line := range raw {
					if w := ansi.StringWidth(line); w != sz[0] {
						t.Errorf("line %d is %d cells wide, want %d", i, w, sz[0])
					}
					bare := sgrRe.ReplaceAllString(line, "")
					if strings.Contains(bare, "\x1b") || strings.Contains(bare, "[38;2") ||
						strings.Contains(bare, "[48;2") || strings.Contains(bare, "[0m") {
						t.Errorf("line %d holds a cut escape sequence: %q", i, bare)
					}
					if stripped := ansi.Strip(line); strings.Contains(stripped, "[38;2") || strings.Contains(stripped, "[48;2") {
						t.Errorf("line %d shows a sequence fragment as text: %q", i, stripped)
					}
				}
				f := mustFrame(t, a)
				if f.X > 0 && strings.TrimSpace(ansi.Cut(screen(a)[f.Y+2], 0, f.X)) == "" {
					t.Error("test setup: the view is not visible left of the frame")
				}
			})
		}
	}
}

// ── The input line is styled by the router, on a copy ────────────────────────

func TestThePromptAndTheTypedTextCarryTheirColors(t *testing.T) {
	testutil.TrueColor(t)
	a := routerAt(t, footedFake("body"), 80, 24)
	press(t, a, "ctrl+p")
	typeCommand(t, a, "co")

	var row string
	for _, l := range strings.Split(a.View(), "\n") {
		if strings.Contains(l, "❯") {
			row = l
		}
	}
	if row == "" {
		t.Fatal("no input row with a prompt on screen")
	}
	prompt := activeParams(row, strings.Index(row, "❯"))
	if want := fgParams(t, theme.ColorPaletteBorder); !strings.Contains(prompt, want) {
		t.Errorf("the prompt is drawn with %q, want ColorPaletteBorder %q", prompt, want)
	}
	typed := activeParams(row, strings.Index(row, "co"))
	if want := fgParams(t, theme.ColorText); !strings.Contains(typed, want) {
		t.Errorf("the typed text is drawn with %q, want ColorText %q", typed, want)
	}
	if bg := "48;2;"; !strings.Contains(typed, bg) {
		t.Errorf("the typed text has no explicit background: %q", typed)
	}
}

// The input is sized to the frame (width - 2 borders - 2 margins - prompt), so
// a long line scrolls with the cursor in view instead of being cut, and the
// right border keeps a margin.
func TestALongInputScrollsInsteadOfBeingCut(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {40, 16}} {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			a := routerAt(t, &fakeView{body: "body"}, sz[0], sz[1])
			press(t, a, "ctrl+p")
			typeCommand(t, a, strings.Repeat("abcdefghij", 4)+"WXYZ")

			f := mustFrame(t, a)
			in := f.input()
			if w := ansi.StringWidth(in); w != f.Width {
				t.Errorf("input row is %d cells, want %d: %q", w, f.Width, in)
			}
			if !strings.Contains(in, "WXYZ") {
				t.Errorf("the end of the line (cursor) is cut off: %q", in)
			}
			if !strings.HasPrefix(in, "│") || !strings.HasSuffix(in, " │") {
				t.Errorf("the input row lost its sides or its right margin: %q", in)
			}

			// Home shows the start; the window still ends before the border.
			press(t, a, "home")
			in = mustFrame(t, a).input()
			if !strings.HasSuffix(in, " │") || !strings.Contains(in, "abcdefghij") {
				t.Errorf("after home the input row is %q", in)
			}
			if w := ansi.StringWidth(in); w != f.Width {
				t.Errorf("after home the input row is %d cells, want %d", w, f.Width)
			}
		})
	}
}

// Keys that reach the view stay the same: nothing but the palette sees them.
func TestThePaletteOwnsTheKeyboardAndTheViewHearsNothing(t *testing.T) {
	view := footedFake("body")
	a := routerAt(t, view, 80, 24)
	received := len(view.received)

	cmdOpen := press(t, a, "ctrl+p")
	typeCommand(t, a, "co")
	press(t, a, "down", "tab", "esc")

	if requestsResize(cmdOpen) {
		t.Error("opening requested a resize")
	}
	if got := view.keysSeen(); len(got) != 0 {
		t.Errorf("the view received %v while the palette was open", got)
	}
	if len(view.received) != received {
		t.Errorf("the view received %d message(s) between ctrl+p and esc", len(view.received)-received)
	}
}
