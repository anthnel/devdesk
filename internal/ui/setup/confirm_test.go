package setup

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

func TestNewConfirmPromptDefaultsToNo(t *testing.T) {
	c := newConfirmPrompt("Title", "Message?")
	if c.yes {
		t.Error("newConfirmPrompt() defaults to Yes; the safe default is No (Rule 104)")
	}
}

func TestConfirmPromptArrowsToggleSelection(t *testing.T) {
	for _, key := range []string{"left", "right"} {
		t.Run(key, func(t *testing.T) {
			c := newConfirmPrompt("Title", "Message?")

			c, cmd := c.Update(testutil.Key(key))
			if !c.yes {
				t.Errorf("after %q, selection did not move to Yes", key)
			}
			if cmd != nil {
				t.Errorf("after %q, expected no command, got one", key)
			}

			c, _ = c.Update(testutil.Key(key))
			if c.yes {
				t.Errorf("after a second %q, selection did not move back to No", key)
			}
		})
	}
}

func TestConfirmPromptEnterConfirmsTheCurrentSelection(t *testing.T) {
	tests := []struct {
		name    string
		yes     bool
		wantYes bool
	}{
		{"enter on No", false, false},
		{"enter on Yes", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newConfirmPrompt("Title", "Message?")
			c.yes = tt.yes

			_, cmd := c.Update(testutil.Key("enter"))
			if cmd == nil {
				t.Fatal("enter produced no command")
			}
			assertConfirmResult(t, cmd(), tt.wantYes)
		})
	}
}

func TestConfirmPromptYNShortcutsBypassTheCurrentSelection(t *testing.T) {
	c := newConfirmPrompt("Title", "Message?") // starts on No
	_, cmd := c.Update(testutil.Key("y"))
	assertConfirmResult(t, cmd(), true)

	c = newConfirmPrompt("Title", "Message?")
	c.yes = true
	_, cmd = c.Update(testutil.Key("n"))
	assertConfirmResult(t, cmd(), false)

	c = newConfirmPrompt("Title", "Message?")
	_, cmd = c.Update(testutil.Key("esc"))
	assertConfirmResult(t, cmd(), false)
}

func assertConfirmResult(t *testing.T, msg any, wantYes bool) {
	t.Helper()
	switch msg.(type) {
	case components.ConfirmModalYesMsg:
		if !wantYes {
			t.Error("got ConfirmModalYesMsg, want ConfirmModalNoMsg")
		}
	case components.ConfirmModalNoMsg:
		if wantYes {
			t.Error("got ConfirmModalNoMsg, want ConfirmModalYesMsg")
		}
	default:
		t.Fatalf("unexpected message type %T", msg)
	}
}
