package app

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Option C (DEVDESK-201 step C): every top-level view keeps Render() string and
// wraps it in View() tea.View; the router alone decides the terminal fields
// (alternate screen now, colours and focus reporting later). These tests are
// written against the Bubble Tea v2 API: they do not compile until internal/app
// and every view have moved (end of step E), and that compile error is their red.

func TestAppViewUsesTheAltScreen(t *testing.T) {
	a := routerAt(t, &fakeView{body: "hello"}, 80, 24)

	v := a.View()

	if !v.AltScreen {
		t.Error("App.View() does not ask for the alternate screen; main.go no longer does")
	}
	if v.Content != a.Render() {
		t.Error("App.View().Content differs from App.Render()")
	}
}

// Every view the router can build satisfies Screen, and its View() adds
// nothing to Render(): in particular it does not set AltScreen, which belongs
// to the router.
func TestEveryViewIsAScreenWhoseViewWrapsRender(t *testing.T) {
	for _, name := range command.AllViewNames() {
		vt := command.ViewType(name)
		t.Run(name, func(t *testing.T) {
			isolateHome(t)
			a := newWithSize(testConfig(), 120, 40)
			a.createView(vt)

			screen, ok := a.views[vt]
			if !ok {
				t.Fatalf("createView(%q) built nothing", vt)
			}
			v := screen.View()
			if v.Content != screen.Render() {
				t.Errorf("%q: View().Content differs from Render()", vt)
			}
			if v.AltScreen {
				t.Errorf("%q sets AltScreen itself; only the router may", vt)
			}
			if v.ReportFocus {
				t.Errorf("%q sets ReportFocus itself; only the router may", vt)
			}
		})
	}
}

// noRender is a tea.Model that is not a Screen: it has no Render().
type noRender struct{}

func (noRender) Init() tea.Cmd                       { return nil }
func (noRender) Update(tea.Msg) (tea.Model, tea.Cmd) { return noRender{}, nil }
func (noRender) View() tea.View                      { return tea.NewView("") }

func TestSetViewPanicsNamingAViewThatIsNotAScreen(t *testing.T) {
	a := routerAt(t, &fakeView{}, 80, 24)
	const name = command.ViewType("security")

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("setView accepted a model without Render()")
		}
		if msg := fmt.Sprint(r); !strings.Contains(msg, string(name)) {
			t.Errorf("panic %q does not name the view %q", msg, name)
		}
	}()
	a.setView(name, noRender{})
}

func TestSetViewStoresAScreen(t *testing.T) {
	a := routerAt(t, &fakeView{}, 80, 24)
	v := &fakeView{title: "stored"}

	a.setView(command.ViewStatus, v)

	if a.views[command.ViewStatus] != Screen(v) {
		t.Error("setView did not store the screen")
	}
}

// A paste is text for a focused field, never a key: on a view with no field in
// edit mode it reaches nothing and fires nothing (Rule 111).
func TestPasteNeverFiresAKeyAction(t *testing.T) {
	for _, paste := range []string{"q", "?", ":", "ctrl+p"} {
		t.Run(paste, func(t *testing.T) {
			view := &fakeView{title: "view"}
			a := router(t, view)
			before := len(view.received)

			_, cmd := a.Update(testutil.Paste(paste))

			if sawQuit(cmd) {
				t.Error("the paste quit the application")
			}
			if a.showHelp {
				t.Error("the paste opened the help")
			}
			if a.commandMode {
				t.Error("the paste opened the command palette")
			}
			if n := len(view.received) - before; n != 0 {
				t.Errorf("a view not in edit mode received %d message(s): %#v", n, view.received[before:])
			}
		})
	}
}

// While a context switch is loading or the context list is shown, a paste is
// dropped: the previous context's views and the list take no text.
func TestPasteIsDroppedBehindAnOverlayOrASwitch(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*App)
	}{
		{"context switch", func(a *App) { a.switchingTo = "staging" }},
		{"context list", func(a *App) { a.showContextList = true; a.contextList = []string{"default", "staging"} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			view := &fakeView{title: "view", editing: true}
			a := router(t, view)
			tc.setup(a)
			before := len(view.received)

			_, cmd := a.Update(testutil.Paste("q?:"))

			if n := len(view.received) - before; n != 0 {
				t.Errorf("the view received %d message(s) behind the overlay", n)
			}
			if sawQuit(cmd) {
				t.Error("the paste quit the application")
			}
			if a.commandMode {
				t.Error("the paste opened the command palette")
			}
		})
	}
}
