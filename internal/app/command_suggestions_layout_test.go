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

// DEVDESK-89 step 7 (DEVDESK-108), redone for the palette (DEVDESK-109): the
// command palette and its suggestion list keep the layout whole at awkward
// sizes, on both frames, and across resizes.

// framelessFake is a fakeView that draws its own frames, like the dashboard.
type framelessFake struct{ fakeView }

func (*framelessFake) Frameless() bool { return true }

// Update keeps the wrapper in the router's map; the promoted one would hand
// back the inner fakeView and lose Frameless.
func (v *framelessFake) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := v.fakeView.Update(msg)
	return v, cmd
}

// resizeTo feeds a terminal resize through Update, as bubbletea would.
func resizeTo(t *testing.T, a *App, w, h int) {
	t.Helper()
	a.Update(tea.WindowSizeMsg{Width: w, Height: h})
}

// tinyHeights are terminal heights where the free height V = H - 8 (a fakeView
// has no footer) is 4 or less, so the palette has no row to draw: it shrinks to
// title, input and rule. n = min(8, V-4) is 0 up to H = 12.
var tinyHeights = []int{12, 11, 10}

// ── Free height of 4 or less: no rows, the input stays usable ────────────────

func TestNoRowsWhenTheViewportIsTooShortButTheInputStaysUsable(t *testing.T) {
	for _, h := range tinyHeights {
		t.Run(fmt.Sprintf("80x%d", h), func(t *testing.T) {
			a := routerAt(t, &fakeView{body: "body"}, 80, h)
			press(t, a, "ctrl+p")
			typeCommand(t, a, "co")

			f := mustFrame(t, a)
			if len(f.Lines) != 3 {
				t.Errorf("the reduced frame is %d lines, want 3 (title, input, rule):\n%s", len(f.Lines), strings.Join(f.Lines, "\n"))
			}
			if !strings.Contains(f.input(), "❯ co") {
				t.Errorf("the input row %q does not show the typed text", f.input())
			}
			if r := f.rows(); len(r) != 0 {
				t.Errorf("rows drawn with no room for a list: %q", r)
			}
			if f.rule() == "" || !strings.HasPrefix(f.rule(), "└") || !strings.HasSuffix(f.rule(), "┘") {
				t.Errorf("the reduced frame is not closed by └…┘: %q", f.rule())
			}
			if got := lipgloss.Height(a.View()); got != h {
				t.Errorf("View is %d lines, want %d", got, h)
			}
		})
	}
}

// Decision (see the report): with no list on screen there is no choice to
// make, so enter runs the text as typed, like the argument stage. A prefix is
// not a name: "co" runs nothing rather than opening a view nobody saw.
func TestEnterNeverOpensAnInvisibleChoiceWhenThereIsNoList(t *testing.T) {
	for _, h := range tinyHeights {
		t.Run(fmt.Sprintf("80x%d", h), func(t *testing.T) {
			a := routerAt(t, &fakeView{body: "body"}, 80, h)
			press(t, a, "ctrl+p")
			typeCommand(t, a, "co")
			press(t, a, "enter")

			if a.currentView != command.ViewDashboard || !a.commandMode {
				t.Errorf("view=%s commandMode=%v: enter opened a choice nobody could see", a.currentView, a.commandMode)
			}
			if _, ok := a.highlightedEntry(); ok {
				t.Error("a row is highlighted while no list is drawn")
			}
		})
	}
}

func TestEnterRunsTheExactTypedNameWhenThereIsNoList(t *testing.T) {
	a := routerAt(t, &fakeView{body: "body"}, 80, 12)
	press(t, a, "ctrl+p")
	typeCommand(t, a, "containers")
	press(t, a, "enter")

	if a.currentView != command.ViewContainers || a.commandMode {
		t.Errorf("view=%s commandMode=%v, want containers with the line closed", a.currentView, a.commandMode)
	}
}

// The silent refusal, a declared exception to Rule 130, is gone: the reduced
// frame has a bottom rule, and it carries the notice.
func TestEnterOnAnEmptyLineWithNoRowsChangesNothingAndWarns(t *testing.T) {
	a := routerAt(t, &fakeView{body: "body"}, 80, 12)
	press(t, a, "ctrl+p")
	cmd := press(t, a, "enter")

	if _, quit := testutil.MsgOf[tea.QuitMsg](cmd); quit {
		t.Fatal("enter on an empty line quit the application")
	}
	if a.currentView != command.ViewDashboard || !a.commandMode {
		t.Errorf("view=%s commandMode=%v: the empty enter acted", a.currentView, a.commandMode)
	}
	if got, want := filet(t, a), theme.IconWarning+" Type a name or choose one"; !strings.Contains(got, want) {
		t.Errorf("rule %q lacks %q: the refusal is silent", got, want)
	}
}

// A prefix is not a name: with no rows, enter on "co" warns instead of staying
// mute, and the exact name still opens the view.
func TestEnterOnAPrefixWithNoRowsWarnsOnTheRule(t *testing.T) {
	a := routerAt(t, &fakeView{body: "body"}, 80, 12)
	press(t, a, "ctrl+p")
	typeCommand(t, a, "co")
	press(t, a, "enter")

	if got, want := filet(t, a), theme.IconWarning+` "co" is not a view or a command`; !strings.Contains(got, want) {
		t.Errorf("rule %q lacks %q", got, want)
	}
	if a.currentView != command.ViewDashboard || !a.commandMode {
		t.Errorf("view=%s commandMode=%v: enter acted on a prefix", a.currentView, a.commandMode)
	}
}

func TestEscStillClosesThePaletteWhenThereAreNoRows(t *testing.T) {
	a := routerAt(t, &fakeView{body: "body"}, 80, 12)
	press(t, a, "ctrl+p")
	typeCommand(t, a, "co")
	press(t, a, "esc")
	if a.commandMode {
		t.Error("esc left the command line open")
	}
}

// ── Height of View(), closed or open ─────────────────────────────────────────

// View is exactly H lines at every size, down to 1x1, with the palette closed or
// open: the floor of 9 lines the permanent command line imposed is gone, and
// nothing replaces it.
func TestViewHeightIsTheTerminalsWithThePaletteClosedOrOpen(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 40}, {60, 30}, {40, 16}, {40, 8}, {80, 6}, {20, 5}, {10, 3}, {1, 1}}
	for _, sz := range sizes {
		t.Run(fmt.Sprintf("%dx%d/closed", sz[0], sz[1]), func(t *testing.T) {
			a := routerAt(t, &fakeView{body: "body"}, sz[0], sz[1])
			if h := lipgloss.Height(a.View()); h != sz[1] {
				t.Errorf("View is %d lines, want %d", h, sz[1])
			}
		})
		t.Run(fmt.Sprintf("%dx%d/open", sz[0], sz[1]), func(t *testing.T) {
			a := routerAt(t, &fakeView{body: "body"}, sz[0], sz[1])
			press(t, a, "ctrl+p")
			typeCommand(t, a, "co")
			if h := lipgloss.Height(a.View()); h != sz[1] {
				t.Errorf("View is %d lines, want %d", h, sz[1])
			}
		})
	}
}

// From 9 lines the typed text is on screen (the frame is pulled up onto the
// blank line under the header); below that the header wins, the input is off
// screen, and nothing panics.
func TestTheInputIsVisibleDownToNineLines(t *testing.T) {
	for _, h := range []int{10, 9} {
		t.Run(fmt.Sprintf("80x%d", h), func(t *testing.T) {
			a := routerAt(t, &fakeView{body: "body"}, 80, h)
			press(t, a, "ctrl+p")
			typeCommand(t, a, "co")

			if !strings.Contains(strings.Join(screen(a), "\n"), "❯ co") {
				t.Errorf("the typed text is not on screen:\n%s", strings.Join(screen(a), "\n"))
			}
		})
	}
	t.Run("80x8", func(t *testing.T) {
		a := routerAt(t, &fakeView{body: "body"}, 80, 8)
		press(t, a, "ctrl+p")
		typeCommand(t, a, "co")

		lines := screen(a)
		if len(lines) != 8 {
			t.Fatalf("View is %d lines, want 8", len(lines))
		}
		openHeader := strings.Split(ansi.Strip(a.renderHeader()), "\n")
		for i := 0; i < 7; i++ {
			if lines[i] != openHeader[i] {
				t.Errorf("header row %d was covered: %q, want %q", i, lines[i], openHeader[i])
			}
		}
	})
}

// ── Frames ───────────────────────────────────────────────────────────────────

// The palette closes with └…┘ whatever frame the view has: it is a box of its
// own, not the continuation of the viewport. A filter bar's ├…┤ under the view
// is untouched while the frame does not reach it.
func TestThePaletteAlwaysClosesWithCornersWhateverTheView(t *testing.T) {
	views := map[string]func() tea.Model{
		"framed":    func() tea.Model { return &fakeView{body: "body"} },
		"frameless": func() tea.Model { return &framelessFake{fakeView{body: "body"}} },
		"filterbar": func() tea.Model {
			return &fakeView{body: "body", filterBar: true, footerHeight: 2, footer: "bar\nbar"}
		},
	}
	for name, mk := range views {
		t.Run(name, func(t *testing.T) {
			a := routerAt(t, mk(), 80, 30)
			press(t, a, "ctrl+p")
			typeCommand(t, a, "co")

			f := mustFrame(t, a)
			if r := f.rule(); !strings.HasPrefix(r, "└") || !strings.HasSuffix(r, "┘") {
				t.Errorf("rule %q is not └…┘", r)
			}
			if len(f.rows()) == 0 {
				t.Fatalf("no rows in the frame:\n%s", strings.Join(f.Lines, "\n"))
			}
			for _, r := range f.rows() {
				if !strings.HasPrefix(r, "│") || !strings.HasSuffix(r, "│") {
					t.Errorf("row %q lacks its sides", r)
				}
			}
			if got := lipgloss.Height(a.View()); got != 30 {
				t.Errorf("View is %d lines, want 30", got)
			}
		})
	}

	t.Run("filter bar junctions survive", func(t *testing.T) {
		a := routerAt(t, &fakeView{body: "body", filterBar: true, footerHeight: 2, footer: "bar\nbar"}, 80, 30)
		hasJunction := func() bool {
			for _, l := range screen(a) {
				if strings.HasPrefix(l, "├") && strings.HasSuffix(l, "┤") {
					return true
				}
			}
			return false
		}
		if !hasJunction() {
			t.Fatalf("no ├…┤ above the filter bar when closed:\n%s", strings.Join(screen(a), "\n"))
		}
		press(t, a, "ctrl+p")
		typeCommand(t, a, "co")
		if !hasJunction() {
			t.Errorf("the palette removed the viewport's ├…┤ above the filter bar:\n%s", strings.Join(screen(a), "\n"))
		}
	})
}

// ── Resize while the list is open ────────────────────────────────────────────

func TestResizingWithTheListOpenKeepsTheHighlightAndTheHeight(t *testing.T) {
	a := routerAt(t, &fakeView{body: "body"}, 80, 40)
	press(t, a, "ctrl+p")
	typeCommand(t, a, "c")
	press(t, a, "down", "down", "down")

	for _, sz := range [][2]int{{80, 24}, {60, 20}, {40, 16}, {120, 40}} {
		resizeTo(t, a, sz[0], sz[1])
		if got := lipgloss.Height(a.View()); got != sz[1] {
			t.Errorf("%dx%d: View is %d lines", sz[0], sz[1], got)
		}
		if len(highlighted(listRows(t, a))) != 1 {
			t.Errorf("%dx%d: highlight not visible exactly once", sz[0], sz[1])
		}
	}
}

func TestShrinkingToATinyTerminalAndBackDoesNotPanicNorLoseTheHighlight(t *testing.T) {
	a := routerAt(t, &fakeView{body: "body"}, 80, 40)
	press(t, a, "ctrl+p")
	typeCommand(t, a, "co")
	press(t, a, "down")

	for _, sz := range [][2]int{{40, 12}, {20, 5}, {10, 3}, {1, 1}, {80, 12}, {80, 40}} {
		resizeTo(t, a, sz[0], sz[1])
		_ = a.View()
	}
	if got := lipgloss.Height(a.View()); got != 40 {
		t.Errorf("View is %d lines after coming back, want 40", got)
	}
	if len(highlighted(listRows(t, a))) != 1 {
		t.Error("the highlight did not come back with the list")
	}
	press(t, a, "enter")
	if a.currentView != command.ViewContainers {
		t.Errorf("view=%s, want containers (the row chosen before the shrink)", a.currentView)
	}
}

// ── Width ────────────────────────────────────────────────────────────────────

func TestNarrowWidthsNeverWrapTheHeaderAndKeepRowsExact(t *testing.T) {
	for _, w := range []int{50, 40, 30} {
		t.Run(fmt.Sprintf("%d", w), func(t *testing.T) {
			a := routerAt(t, &fakeView{body: "body"}, w, 30)
			press(t, a, "ctrl+p")
			typeCommand(t, a, "co")

			header := ansi.Strip(a.renderHeader())
			if h := lipgloss.Height(a.renderHeader()); h != paletteHeaderLines {
				t.Errorf("header is %d lines, want %d", h, paletteHeaderLines)
			}
			for _, l := range strings.Split(header, "\n") {
				if got := ansi.StringWidth(l); got > w {
					t.Errorf("header line is %d cells, wider than %d: %q", got, w, l)
				}
			}
			// Under 54 columns the frame takes the whole width.
			f := mustFrame(t, a)
			if f.X != 0 || f.Width != w {
				t.Errorf("frame at x=%d, %d wide; want the full %d columns", f.X, f.Width, w)
			}
			for _, r := range f.rows() {
				if got := ansi.StringWidth(r); got != w {
					t.Errorf("row is %d cells, want %d: %q", got, w, r)
				}
			}
			if got := ansi.StringWidth(filet(t, a)); got != w {
				t.Errorf("rule is %d cells, want %d", got, w)
			}
			if got := lipgloss.Height(a.View()); got != 30 {
				t.Errorf("View is %d lines, want 30", got)
			}
		})
	}
}

// ── enter's availability: one computation, two readers (Rule 130) ────────────

// With no list drawn, enter is lit as soon as there is text to run, and the
// header's key set is the one it has with a list: only Disabled moves.
func TestEnterIsLitWithTypedTextWhenThereIsNoList(t *testing.T) {
	a := routerAt(t, &fakeView{body: "body"}, 80, 12)
	press(t, a, "ctrl+p")
	empty := a.commandLineShortcuts()
	if !testutil.ShortcutDisabled(empty, "enter") {
		t.Error("enter is lit on an empty line")
	}

	typeCommand(t, a, "containers")
	typed := a.commandLineShortcuts()
	if testutil.ShortcutDisabled(typed, "enter") {
		t.Error("enter is greyed while runCommand would run the typed name")
	}
	if !testutil.ShortcutDisabled(typed, "tab") {
		t.Error("tab is lit with nothing highlighted")
	}
	if got, want := testutil.ShortcutKeys(typed), testutil.ShortcutKeys(empty); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("key set changed between states: %v vs %v", got, want)
	}

	press(t, a, "enter")
	if a.currentView != command.ViewContainers {
		t.Errorf("view = %s: the lit enter did not act", a.currentView)
	}
}

// ── fitHeight ────────────────────────────────────────────────────────────────

func numbered(prefix string, n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return strings.Join(lines, "\n")
}

func TestFitHeightLeavesAFittingStackAlone(t *testing.T) {
	got := fitHeight(numbered("h", 2), numbered("b", 2), numbered("f", 2), 10)
	if want := "h0\nh1\nb0\nb1\nf0\nf1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFitHeightCutsTheBodyBottomFirstThenKeepsHeaderAndFooter(t *testing.T) {
	got := fitHeight(numbered("h", 2), numbered("b", 4), numbered("f", 2), 6)
	if want := "h0\nh1\nb0\nb1\nf0\nf1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// The body is gone entirely before the footer loses a line.
	got = fitHeight(numbered("h", 2), numbered("b", 4), numbered("f", 2), 4)
	if want := "h0\nh1\nf0\nf1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFitHeightCutsTheFooterBeforeTheHeaderWhenNoBodyIsLeft(t *testing.T) {
	got := fitHeight(numbered("h", 3), numbered("b", 2), numbered("f", 3), 4)
	if want := "h0\nh1\nh2\nf0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFitHeightWithAHeaderTallerThanTheTerminalKeepsItsFirstLines(t *testing.T) {
	got := fitHeight(numbered("h", 10), numbered("b", 3), numbered("f", 2), 4)
	if want := "h0\nh1\nh2\nh3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFitHeightNonPositiveHeightCutsNothing(t *testing.T) {
	for _, h := range []int{0, -3} {
		got := fitHeight(numbered("h", 2), numbered("b", 2), numbered("f", 2), h)
		if want := "h0\nh1\nb0\nb1\nf0\nf1"; got != want {
			t.Errorf("height %d: got %q, want %q", h, got, want)
		}
	}
}

// The input is inside the frame at every height from 9, and the footer's info
// line stays on screen while the free height V = H - 8 - footer is at least 3.
// Below that the frame may cover the footer; that is a documented floor.
func TestTheInputAndTheFooterSurviveShortTerminals(t *testing.T) {
	for _, h := range []int{13, 12} { // V = 4 and 3 with a one-line footer
		t.Run(fmt.Sprintf("80x%d", h), func(t *testing.T) {
			a := routerAt(t, &fakeView{body: "body", footerHeight: 1, footer: "FOOTERINFO"}, 80, h)
			press(t, a, "ctrl+p")
			typeCommand(t, a, "co")
			lines := screen(a)
			if len(lines) != h {
				t.Fatalf("View is %d lines, want %d", len(lines), h)
			}
			all := strings.Join(lines, "\n")
			if f := mustFrame(t, a); !strings.Contains(f.input(), "❯ co") {
				t.Errorf("the input is cut off:\n%s", all)
			}
			if !strings.Contains(all, "FOOTERINFO") {
				t.Errorf("the footer is cut off:\n%s", all)
			}
		})
	}
	for _, h := range []int{11, 10, 9} { // V < 3: only the input is guaranteed
		t.Run(fmt.Sprintf("80x%d/input", h), func(t *testing.T) {
			a := routerAt(t, &fakeView{body: "body", footerHeight: 1, footer: "FOOTERINFO"}, 80, h)
			press(t, a, "ctrl+p")
			typeCommand(t, a, "co")
			if got := len(screen(a)); got != h {
				t.Fatalf("View is %d lines, want %d", got, h)
			}
			if !strings.Contains(strings.Join(screen(a), "\n"), "❯ co") {
				t.Errorf("the input is off screen at %d lines", h)
			}
		})
	}
}
