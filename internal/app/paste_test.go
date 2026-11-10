package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// A paste reaches the field that has focus and triggers nothing else: its text
// holds letters ("q", "?") that are actions when typed outside a field
// (DEVDESK-199, Rule 111). A single-line field keeps the text and turns each
// newline into a space.

// sawQuit reports whether cmd, once run, yields tea.QuitMsg.
func sawQuit(cmd tea.Cmd) bool {
	for _, msg := range testutil.Msgs(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			return true
		}
	}
	return false
}

func TestPasteIntoCommandPaletteReachesTheField(t *testing.T) {
	tests := []struct{ name, paste, want string }{
		{"one line holding the quit letter", "q", "q"},
		{"several lines", "l1\nl2", "l1 l2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			view := &fakeView{title: "view"}
			a := router(t, view)
			a.Update(testutil.Key(commandModeKey))
			if !a.commandMode {
				t.Fatal("ctrl+p did not open the palette")
			}
			before := len(view.received)

			_, cmd := a.Update(testutil.Paste(tc.paste))

			if got := a.commandInput.Value(); got != tc.want {
				t.Errorf("command field = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if !a.commandMode {
				t.Error("the paste closed the palette")
			}
			if len(view.received) != before {
				t.Errorf("the view received %d message(s) while the palette was open", len(view.received)-before)
			}
			if sawQuit(cmd) {
				t.Error("the paste quit the application")
			}
		})
	}
}

func TestPasteWhileHelpIsOpenTriggersNothing(t *testing.T) {
	view := &fakeView{title: "view"}
	a := router(t, view)
	a.showHelp = true
	before := len(view.received)

	_, cmd := a.Update(testutil.Paste("q?"))

	if !a.showHelp {
		t.Error("the paste closed the help")
	}
	if len(view.received) != before {
		t.Errorf("the view received %d message(s) while the help was open: %#v", len(view.received)-before, view.received[before:])
	}
	if sawQuit(cmd) {
		t.Error("the paste quit the application")
	}
}

func TestPasteIntoAnEditingViewIsForwardedUntouched(t *testing.T) {
	for _, paste := range []string{"q?:", "l1\nl2"} {
		view := &fakeView{title: "view", editing: true}
		a := router(t, view)
		before := len(view.received)

		_, cmd := a.Update(testutil.Paste(paste))

		if len(view.received)-before != 1 {
			t.Fatalf("view received %d message(s) for paste %q, want 1", len(view.received)-before, paste)
		}
		got := view.received[before]
		if text, ok := testutil.Text(got); !ok || text != paste {
			t.Errorf("view received %#v, want the paste %q as is", got, paste)
		}
		if a.showHelp || a.commandMode {
			t.Errorf("paste %q triggered a router action (help=%v palette=%v)", paste, a.showHelp, a.commandMode)
		}
		if sawQuit(cmd) {
			t.Errorf("paste %q quit the application", paste)
		}
	}
}
