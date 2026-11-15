package app

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
	"github.com/anthnel/devdesk/internal/viewer"
)

// DEVDESK-457: whatever a view puts at the top of its footer, the frame above
// it must join it. The router reads the bar from the footer itself, so no view
// can leave a closed box sitting on top of an open one.

func isBox(l, left, right string) bool {
	return strings.HasPrefix(l, left) && strings.HasSuffix(l, right)
}

// assertBarAttached checks the screen as drawn: the right height, and no
// closed floor (└…┘) directly over a bar line (│…│). It does not check the
// junction itself; assertJoined does.
func assertBarAttached(t *testing.T, lines []string, height int) {
	t.Helper()
	if len(lines) != height {
		t.Errorf("the screen is %d lines, want %d", len(lines), height)
	}
	for i := 1; i < len(lines); i++ {
		if isBox(lines[i-1], "└", "┘") && isBox(lines[i], "│", "│") {
			t.Errorf("line %d: a closed floor %q sits directly over a bar line %q", i, lines[i-1], lines[i])
		}
	}
}

// assertJoined checks that the bar starting with prefix is on screen with a
// junction line right above it, and a floor right below it.
func assertJoined(t *testing.T, lines []string, prefix string) {
	t.Helper()
	i := lineIndex(lines, prefix)
	if i < 1 {
		t.Errorf("no bar starting with %q:\n%s", prefix, strings.Join(lines, "\n"))
		return
	}
	if !isBox(lines[i-1], "├", "┤") {
		t.Errorf("the line above the bar is %q, want ├…┤", lines[i-1])
	}
	if i+1 >= len(lines) || !isBox(lines[i+1], "└", "┘") {
		t.Errorf("the line below the bar is not └…┘")
	}
}

// lineIndex returns the index of the first line starting with prefix, or -1.
func lineIndex(lines []string, prefix string) int {
	for i, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return i
		}
	}
	return -1
}

func netdiagApp(t *testing.T, w, h int) *App {
	t.Helper()
	isolateHome(t)
	a := newWithSize(testConfig(), w, h)
	a.createView(command.ViewNetdiag)
	a.currentView = command.ViewNetdiag
	a.resize(w, h)
	return a
}

// feedView sends msgs to the active view directly, as the view tests do, so
// no Cmd is run, then lets the router re-measure the footer the way it does
// after every message.
func feedView(a *App, msgs ...tea.Msg) {
	for _, msg := range msgs {
		updated, _ := a.views[a.currentView].Update(msg)
		a.setView(a.currentView, updated)
	}
	a.resize(a.width, a.height)
}

func TestPathTabBarIsAttachedToTheFrame(t *testing.T) {
	for _, size := range []struct{ w, h int }{{200, 50}, {80, 16}} {
		a := netdiagApp(t, size.w, size.h)
		feedView(a, testutil.Keys("tab", "tab", "tab", "tab")...)

		lines := screen(a)
		assertJoined(t, lines, "│Target ›")
		assertBarAttached(t, lines, size.h)
	}
}

func TestInterfacesTabFilterBarIsAttachedToTheFrame(t *testing.T) {
	a := netdiagApp(t, 160, 40)
	feedView(a, testutil.Keys("tab", "tab")...)
	feedView(a, testutil.Key("/"))

	lines := screen(a)
	assertJoined(t, lines, "│ /")
	assertBarAttached(t, lines, 40)
}

func TestContainersBarJoinsOnlyWhenItIsThere(t *testing.T) {
	isolateHome(t)
	a := newWithSize(testConfig(), 160, 40)
	a.createView(command.ViewContainers)
	a.currentView = command.ViewContainers
	a.resize(160, 40)

	without := screen(a)
	assertBarAttached(t, without, 40)
	if !strings.Contains(strings.Join(without, "\n"), "└") {
		t.Error("without a bar the frame lost its └…┘ floor")
	}

	feedView(a, testutil.Key("/"))
	with := screen(a)
	assertBarAttached(t, with, 40)
	assertJoined(t, with, "│ /")
}

func TestViewerTreeBarIsAttachedToTheFrame(t *testing.T) {
	path := t.TempDir() + "/config.json"
	if err := os.WriteFile(path, []byte(`{"name":"web","port":8080}`), 0o600); err != nil {
		t.Fatal(err)
	}

	a := newWithSize(testConfig(), 120, 30)
	a.createView(command.ViewWorkspaces)
	a.currentView = command.ViewWorkspaces
	_, cmd := a.Update(uiviewer.OpenRequestMsg{Source: viewer.NewFileSource(path)})
	for _, msg := range testutil.Msgs(cmd) {
		updated, _ := a.views[command.ViewViewer].Update(msg)
		a.setView(command.ViewViewer, updated)
	}
	a.currentView = command.ViewViewer

	// A search made on the text display survives the switch back to the tree:
	// the footer still draws the bar there.
	feedView(a, testutil.Key("f"), testutil.Key("/"))
	feedView(a, testutil.Type("web")...)
	feedView(a, testutil.Key("enter"), testutil.Key("f"))
	lines := screen(a)
	assertJoined(t, lines, "│ /")
	assertBarAttached(t, lines, 30)
}

// A frameless view draws its own boxes: the router must not rewrite them.
func TestFramelessBodyIsLeftUntouched(t *testing.T) {
	a := routerAt(t, &framelessBarFake{fakeView{
		body:         "┌──┐\n│  │\n└──┘",
		footerHeight: 3,
		footer:       components.BarFrame(80, "x") + "\ninfo",
	}}, 80, 20)

	joined := strings.Join(screen(a), "\n")
	if !strings.Contains(joined, "└──┘") || strings.Contains(joined, "├──┤") {
		t.Errorf("the frameless body was rewritten:\n%s", joined)
	}
}

type framelessBarFake struct{ fakeView }

func (*framelessBarFake) Frameless() bool { return true }

// fitHeight trims the body from just above its last line, so the junction the
// bar attaches to survives at every height where the body is cut but the
// footer still fits.
func TestPathTabBarStaysJoinedWhileTheBodyIsTrimmed(t *testing.T) {
	seen := 0
	for h := 12; h <= 24; h++ {
		a := netdiagApp(t, 120, h)
		feedView(a, testutil.Keys("tab", "tab", "tab", "tab")...)

		lines := screen(a)
		assertBarAttached(t, lines, h)
		i := lineIndex(lines, "│Target ›")
		if i < 0 {
			continue
		}
		if isBox(lines[i-1], "┌", "┐") {
			continue // the body is cut entirely: the documented floor
		}
		seen++
		if !isBox(lines[i-1], "├", "┤") {
			t.Errorf("height %d: the line above the bar is %q, want ├…┤", h, lines[i-1])
		}
	}
	if seen == 0 {
		t.Fatal("the Path bar was never on screen in the heights tried")
	}
}
