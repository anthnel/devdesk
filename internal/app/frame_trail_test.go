package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ── FrameTrailView (DEVDESK-134) ──────────────────────────────────────────────

// trailedView is a fakeView that also contributes a trail to the title line,
// recording every budget the router hands it.
type trailedView struct {
	fakeView
	trail   string
	budgets []int
}

func (v *trailedView) FrameTrail(maxWidth int) string {
	v.budgets = append(v.budgets, maxWidth)
	return v.trail
}

// Update keeps the wrapper alive: the promoted fakeView.Update returns the
// inner *fakeView, which would make the router replace this view and lose
// FrameTrail.
func (v *trailedView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := v.fakeView.Update(msg)
	return v, cmd
}

// framelessTrailedView is a trailedView that draws its own frames.
type framelessTrailedView struct{ trailedView }

func (v *framelessTrailedView) Frameless() bool { return true }

func (v *framelessTrailedView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := v.trailedView.Update(msg)
	return v, cmd
}

// The test view must satisfy the interface the router probes for.
var _ FrameTrailView = (*trailedView)(nil)

const trailTitle = "Test title"

func trailRouter(t *testing.T, view *trailedView, width int) *App {
	t.Helper()
	view.title = trailTitle
	return routerAt(t, view, width, 40)
}

func TestAViewWithoutFrameTrailKeepsTheBorderLineUnchanged(t *testing.T) {
	for _, w := range []int{20, 80, 120} {
		a := routerAt(t, &fakeView{title: trailTitle}, w, 40)
		if got, want := a.renderTitleLine(), theme.RenderBorderTitle(trailTitle, w); got != want {
			t.Errorf("width %d: got %q, want %q", w, got, want)
		}
	}
}

func TestAnEmptyFrameTrailKeepsTheBorderLineUnchanged(t *testing.T) {
	view := &trailedView{}
	a := trailRouter(t, view, 120)
	if got, want := a.renderTitleLine(), theme.RenderBorderTitle(trailTitle, 120); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFrameTrailLandsInParenthesesRightAfterTheTitle(t *testing.T) {
	const trail = "Sessions › x › 1a2b3c4d"
	view := &trailedView{trail: trail}
	a := trailRouter(t, view, 120)

	line := a.renderTitleLine()
	plain := ansi.Strip(line)
	if lipgloss.Width(line) != 120 {
		t.Errorf("title line is %d cells, want 120", lipgloss.Width(line))
	}
	head := "┌─ " + trailTitle + " (" + trail + ") "
	if !strings.HasPrefix(plain, head) {
		t.Errorf("trail must hug the title in parentheses:\n got %q\nwant prefix %q", plain, head)
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(plain, head), "┐")
	if rest == "" || strings.Trim(rest, "─") != "" || !strings.HasSuffix(plain, "┐") {
		t.Errorf("only filler may follow the trail, up to the corner: %q", plain)
	}
}

func TestFrameTrailReceivesTheThemeBudget(t *testing.T) {
	for _, w := range []int{80, 160} {
		view := &trailedView{trail: "x"}
		a := trailRouter(t, view, w)
		a.renderTitleLine()

		want := theme.BorderTrailBudget(trailTitle, w)
		if len(view.budgets) == 0 {
			t.Fatalf("width %d: FrameTrail was never called", w)
		}
		for _, got := range view.budgets {
			if got != want {
				t.Errorf("width %d: maxWidth = %d, want BorderTrailBudget = %d", w, got, want)
			}
		}
	}
}

func TestATrailWiderThanItsBudgetIsDroppedNotCut(t *testing.T) {
	const w = 80
	budget := theme.BorderTrailBudget(trailTitle, w)
	view := &trailedView{trail: strings.Repeat("x", budget+1)}
	a := trailRouter(t, view, w)

	line := a.renderTitleLine()
	if want := theme.RenderBorderTitle(trailTitle, w); line != want {
		t.Errorf("a contract-violating trail must be dropped:\n got %q\nwant %q", line, want)
	}
	if lipgloss.Width(line) != w {
		t.Errorf("title line is %d cells, want %d", lipgloss.Width(line), w)
	}
}

func TestATrailExactlyAtItsBudgetIsKept(t *testing.T) {
	const w = 80
	budget := theme.BorderTrailBudget(trailTitle, w)
	view := &trailedView{trail: strings.Repeat("x", budget)}
	a := trailRouter(t, view, w)

	line := a.renderTitleLine()
	if lipgloss.Width(line) != w {
		t.Errorf("title line is %d cells, want %d", lipgloss.Width(line), w)
	}
	if !strings.Contains(ansi.Strip(line), strings.Repeat("x", budget)) {
		t.Errorf("a trail that fits was dropped: %q", ansi.Strip(line))
	}
}

func TestFrameTrailIsNotAskedWhenThereIsNoRoom(t *testing.T) {
	// "Test title" is 10 cells; chrome is 9: a 19-cell window has a budget of 0.
	for _, w := range []int{1, 10, 19} {
		view := &trailedView{trail: "x"}
		a := trailRouter(t, view, w)

		if got, want := a.renderTitleLine(), theme.RenderBorderTitle(trailTitle, w); got != want {
			t.Errorf("width %d: got %q, want %q", w, got, want)
		}
		if len(view.budgets) != 0 {
			t.Errorf("width %d: FrameTrail called with %v, want no call when budget <= 0", w, view.budgets)
		}
	}
}

func TestAFramelessViewGetsNoTrail(t *testing.T) {
	view := &framelessTrailedView{trailedView{trail: "Sessions › x"}}
	view.title = trailTitle
	a := routerAt(t, view, 120, 40)

	line := a.renderTitleLine()
	if want := theme.RenderTitledRule(trailTitle, 120); line != want {
		t.Errorf("frameless view must get the titled rule:\n got %q\nwant %q", line, want)
	}
	if strings.Contains(ansi.Strip(line), "Sessions") {
		t.Errorf("a frameless view must not show a trail: %q", ansi.Strip(line))
	}
	if len(view.budgets) != 0 {
		t.Errorf("FrameTrail called on a frameless view with %v", view.budgets)
	}
}

func TestTheTitleLineIsExactlyAsWideAsTheWindowAtEveryWidth(t *testing.T) {
	for w := 30; w <= 140; w += 7 {
		view := &trailedView{}
		a := trailRouter(t, view, w)
		view.trail = strings.Repeat("y", max(theme.BorderTrailBudget(trailTitle, w)/2, 1))

		if got := lipgloss.Width(a.renderTitleLine()); got != w {
			t.Errorf("width %d: title line is %d cells", w, got)
		}
	}
}
