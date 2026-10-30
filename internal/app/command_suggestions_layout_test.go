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
)

// DEVDESK-89 step 7 (DEVDESK-108): the command line and its suggestion list
// keep the layout whole at awkward sizes, on both frames, and across resizes.

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

// tinyHeights are terminal heights where the viewport has less than 2 usable
// lines for a list (header 10 + title 1 + viewport; a framed view keeps its bottom border).
var tinyHeights = []int{13, 12, 11}

// ── Usable height under 2: no list, the line stays usable ────────────────────

func TestNoListWhenTheViewportIsTooShortButTheLineStaysUsable(t *testing.T) {
	for _, h := range tinyHeights {
		t.Run(fmt.Sprintf("80x%d", h), func(t *testing.T) {
			a := routerAt(t, &fakeView{body: "body"}, 80, h)
			press(t, a, "ctrl+p")
			typeCommand(t, a, "co")

			if got := a.commandInput.Value(); got != "co" {
				t.Errorf("typed text = %q, want co: the line is not usable", got)
			}
			if i := filetIndex(screen(a)); i >= 0 {
				t.Errorf("a list rule is drawn at line %d with no room for a list", i)
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

func TestEnterOnAnEmptyLineWithNoListChangesNothing(t *testing.T) {
	a := routerAt(t, &fakeView{body: "body"}, 80, 12)
	press(t, a, "ctrl+p")
	cmd := press(t, a, "enter")

	if _, quit := testutil.MsgOf[tea.QuitMsg](cmd); quit {
		t.Fatal("enter on an empty line quit the application")
	}
	if a.currentView != command.ViewDashboard || !a.commandMode {
		t.Errorf("view=%s commandMode=%v: the empty enter acted", a.currentView, a.commandMode)
	}
}

func TestEscStillClosesTheLineWhenThereIsNoList(t *testing.T) {
	a := routerAt(t, &fakeView{body: "body"}, 80, 12)
	press(t, a, "ctrl+p")
	typeCommand(t, a, "co")
	press(t, a, "esc")
	if a.commandMode {
		t.Error("esc left the command line open")
	}
}

// ── Height of View() without a list, and with a short terminal ───────────────

func TestViewHeightIsTheTerminalsWithTheLineClosedOrListless(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 40}, {60, 30}, {40, 16}, {40, 8}, {80, 6}}
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

// ── Frames ───────────────────────────────────────────────────────────────────

func TestAFramelessViewKeepsItsSidesAndClosesTheRule(t *testing.T) {
	a := routerAt(t, &framelessFake{fakeView{body: "body"}}, 80, 30)
	press(t, a, "ctrl+p")
	typeCommand(t, a, "co")

	lines := screen(a)
	var rule = -1
	for i, l := range lines {
		if strings.HasPrefix(l, "└") && strings.HasSuffix(l, "┘") {
			rule = i
			break
		}
	}
	if rule < 0 {
		t.Fatalf("no closed └…┘ rule:\n%s", strings.Join(lines, "\n"))
	}
	if filetIndex(lines) >= 0 {
		t.Error("a joined ├…┤ rule is drawn on a frameless view")
	}
	// The rows sit above the rule, after the title rule, and carry their sides.
	n := 0
	for i := rule - 1; i >= 0 && strings.HasPrefix(lines[i], "│"); i-- {
		if !strings.HasSuffix(lines[i], "│") {
			t.Errorf("row %q lacks its right side", lines[i])
		}
		n++
	}
	if n == 0 {
		t.Errorf("no row with │ sides above the rule:\n%s", strings.Join(lines, "\n"))
	}
	if got := lipgloss.Height(a.View()); got != 30 {
		t.Errorf("View is %d lines, want 30", got)
	}
}

func TestAFramedViewClosesTheRuleWithTJunctions(t *testing.T) {
	a := routerAt(t, &fakeView{body: "body"}, 80, 30)
	press(t, a, "ctrl+p")
	typeCommand(t, a, "co")

	r := filet(t, a)
	if !strings.HasPrefix(r, "├") || !strings.HasSuffix(r, "┤") {
		t.Errorf("rule %q is not ├…┤", r)
	}
}

func TestAVisibleFilterBarKeepsTheViewportsBottomCorners(t *testing.T) {
	// Closed line: the viewport's own corners become ├…┤ above the bar.
	a := routerAt(t, &fakeView{body: "body", filterBar: true, footerHeight: 2, footer: "bar\nbar"}, 80, 30)
	var bottom string
	for _, l := range screen(a) {
		if strings.HasPrefix(l, "├") && strings.HasSuffix(l, "┤") {
			bottom = l
		}
	}
	if bottom == "" {
		t.Fatalf("no ├…┤ above the filter bar:\n%s", strings.Join(screen(a), "\n"))
	}

	// Open line: the list's rule must be that same single ├…┤, no stray corner.
	press(t, a, "ctrl+p")
	typeCommand(t, a, "co")
	lines := screen(a)
	count := 0
	for _, l := range lines {
		if strings.ContainsAny(l, "└┘") && strings.HasPrefix(l, "└") {
			count++
		}
		if strings.HasPrefix(l, "├") {
			if !strings.HasSuffix(l, "┤") {
				t.Errorf("rule %q lost its right T-junction", l)
			}
		}
	}
	if filetIndex(lines) < 0 {
		t.Fatalf("no ├…┤ rule with the filter bar visible:\n%s", strings.Join(lines, "\n"))
	}
	if count != 0 {
		t.Errorf("%d └ corner(s) on a viewport joined to a filter bar", count)
	}
	if got := lipgloss.Height(a.View()); got != 30 {
		t.Errorf("View is %d lines, want 30", got)
	}
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
			if h := lipgloss.Height(a.renderHeader()); h != 10 {
				t.Errorf("header is %d lines, want 10", h)
			}
			for _, l := range strings.Split(header, "\n") {
				if got := ansi.StringWidth(l); got > w {
					t.Errorf("header line is %d cells, wider than %d: %q", got, w, l)
				}
			}
			for _, r := range listRows(t, a) {
				if got := ansi.StringWidth(r); got != w {
					t.Errorf("list row is %d cells, want %d: %q", got, w, r)
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

// The command line and the footer's info line stay on screen at the heights
// where the viewport has been squeezed out.
func TestTheCommandLineAndTheFooterSurviveShortTerminals(t *testing.T) {
	for _, h := range []int{13, 12} {
		t.Run(fmt.Sprintf("80x%d", h), func(t *testing.T) {
			a := routerAt(t, &fakeView{body: "body", footerHeight: 1, footer: "FOOTERINFO"}, 80, h)
			press(t, a, "ctrl+p")
			typeCommand(t, a, "co")
			lines := screen(a)
			if len(lines) != h {
				t.Fatalf("View is %d lines, want %d", len(lines), h)
			}
			all := strings.Join(lines, "\n")
			header := strings.Join(lines[:10], "\n")
			if !strings.Contains(header, "co") {
				t.Errorf("the command line is cut off:\n%s", all)
			}
			if !strings.Contains(all, "FOOTERINFO") {
				t.Errorf("the footer is cut off:\n%s", all)
			}
		})
	}
}
