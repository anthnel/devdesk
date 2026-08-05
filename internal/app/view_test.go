package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Assembly ─────────────────────────────────────────────────────────────────

// Rule 124: header, title border, viewport, footer — in that order, all of it
// inside the window.
func TestTheViewIsAssembledInOrder(t *testing.T) {
	a := router(t, &fakeView{
		title:        "OCI Resources",
		body:         "the table body",
		footerHeight: 2,
		footer:       "Images  Networks",
	})

	rendered := a.View()

	title := strings.Index(rendered, "OCI Resources")
	body := strings.Index(rendered, "the table body")
	footer := strings.Index(rendered, "Images  Networks")

	if title == -1 || body == -1 || footer == -1 {
		t.Fatalf("title=%d body=%d footer=%d; something is missing from:\n%s", title, body, footer, rendered)
	}
	if title >= body || body >= footer {
		t.Errorf("order is title=%d body=%d footer=%d, want title before body before footer", title, body, footer)
	}
}

// The window is filled exactly. A view rendering short would leave the
// terminal's own background showing below it (Rule 115).
func TestTheViewFillsTheWindow(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {180, 40}, {240, 60}} {
		a := routerAt(t, &fakeView{footerHeight: 2, body: "x"}, size[0], size[1])

		rendered := a.View()

		if got := lipgloss.Height(rendered); got != size[1] {
			t.Errorf("at %dx%d the view is %d rows, want %d", size[0], size[1], got, size[1])
		}
	}
}

// A view that has not been built yet says so instead of rendering blank, which
// would look like a hang.
func TestAMissingViewSaysSo(t *testing.T) {
	a := router(t, &fakeView{})
	a.currentView = command.ViewSecurity // never built

	if rendered := a.View(); !strings.Contains(rendered, "View not found") {
		t.Errorf("a missing view rendered:\n%s", rendered)
	}
}

// A view with no title still gets its border line, or the viewport loses its
// top edge.
func TestTheTitleLineIsDrawnWithoutATitle(t *testing.T) {
	withTrueColor(t)
	titled := router(t, &fakeView{title: "Workspaces", footerHeight: 2}).View()
	untitled := router(t, &bareView{}).View()

	if !strings.Contains(titled, "Workspaces") {
		t.Error("the title is missing from the border line")
	}
	if !strings.Contains(untitled, "─") {
		t.Error("a view with no title lost its top border line")
	}
}

// A view with no footer contributes none, rather than an empty line that would
// shift the layout by one row.
func TestAViewWithoutAFooterAddsNothing(t *testing.T) {
	withFooter := lipgloss.Height(router(t, &fakeView{footerHeight: 1, footer: "tabs"}).View())
	without := lipgloss.Height(router(t, &bareView{}).View())

	if withFooter != without {
		t.Errorf("the window is %d rows with a footer and %d without; both must fill it",
			withFooter, without)
	}
}

// ── Overlay precedence ───────────────────────────────────────────────────────

// An open overlay replaces the view rather than being drawn over part of it:
// the table underneath would still be readable and invite a keypress that goes
// nowhere.
func TestAnOpenOverlayReplacesTheView(t *testing.T) {
	tests := []struct {
		name string
		open func(*App)
		want string
	}{
		{"help", func(a *App) { a.handleKeyMsg(testutil.Key("?")) }, "Test Help"},
		{"context list", func(a *App) {
			a.Update(ContextListMsg{Contexts: []string{"default"}, Current: "default"})
		}, "Select Context"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := router(t, &fakeView{
				body:        "the table body",
				helpContent: help.Content{Title: "Test Help"},
			})
			tt.open(a)

			rendered := a.View()

			if !strings.Contains(rendered, tt.want) {
				t.Errorf("the %s overlay is not on screen:\n%s", tt.name, rendered)
			}
			if strings.Contains(rendered, "the table body") {
				t.Errorf("the view is still readable behind the %s overlay", tt.name)
			}
		})
	}
}

// Help is rendered first, matching the order the key handler checks them in —
// otherwise esc would close one overlay while another stayed on screen.
func TestHelpIsRenderedBeforeTheLists(t *testing.T) {
	a := router(t, &fakeView{helpContent: help.Content{Title: "Test Help"}})
	a.handleKeyMsg(testutil.Key("?"))
	a.showContextList = true
	a.contextList = []string{"default"}

	rendered := a.View()

	if !strings.Contains(rendered, "Test Help") {
		t.Error("the context list was drawn over the help overlay")
	}
}

// ── Rule 136: the filter bar joins the viewport ──────────────────────────────

// The viewport's bottom corners become T-junctions when the view below has a
// filter bar, so the two form one closed rectangle instead of two stacked
// boxes.
func TestTheFilterBarClosesTheRectangle(t *testing.T) {
	withTrueColor(t)
	plain := router(t, &fakeView{footerHeight: 2, footer: "info"}).View()
	filtering := router(t, &fakeView{footerHeight: 3, footer: "filter", filterBar: true}).View()

	if !strings.Contains(plain, "└") {
		t.Error("the plain viewport has no bottom-left corner")
	}
	if strings.Contains(filtering, "└") {
		t.Error("the corner was not turned into a T-junction for the filter bar")
	}
	if !strings.Contains(filtering, "├") || !strings.Contains(filtering, "┤") {
		t.Error("the filter bar does not join the viewport border")
	}
}

func TestReplaceViewportBottomCornersTouchesOnlyTheLastLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"a bordered box", "┌──┐\n│  │\n└──┘", "┌──┐\n│  │\n├──┤"},
		{"a single line has no bottom border", "└──┘", "└──┘"},
		{"corners above the last line are left alone", "└─┘\n│x│\n└─┘", "└─┘\n│x│\n├─┤"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := replaceViewportBottomCorners(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
