package components

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Enter picks the focused button (Rule 135).
func TestChoiceModalEnterPicksTheFocusedButton(t *testing.T) {
	t.Run("action", func(t *testing.T) {
		m := NewChoiceModal("Stop", "How?", "Stop", "Restart")
		m.focused = 1

		_, cmd := m.Update(testutil.Key("enter"))
		got, ok := testutil.MsgOf[ChoiceModalPickedMsg](cmd)
		if !ok {
			t.Fatalf("enter on an action produced %T, want ChoiceModalPickedMsg", testutil.Msg(cmd))
		}
		if got.Index != 1 || got.Label != "Restart" {
			t.Errorf("enter picked %+v, want Index 1 / Restart", got)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		m := NewChoiceModal("Stop", "How?", "Stop", "Restart")
		m.focused = 2 // Cancel

		_, cmd := m.Update(testutil.Key("enter"))
		if _, ok := testutil.MsgOf[ChoiceModalCancelledMsg](cmd); !ok {
			t.Errorf("enter on Cancel produced %T, want ChoiceModalCancelledMsg", testutil.Msg(cmd))
		}
	})
}

// Rule 135: Space only toggles a checkbox; it must not pick an action or cancel.
func TestChoiceModalSpaceDoesNotAnswer(t *testing.T) {
	for focus := 0; focus <= 2; focus++ {
		m := NewChoiceModal("Stop", "How?", "Stop", "Restart")
		m.focused = focus

		m, cmd := m.Update(testutil.Key("space"))
		if cmd != nil {
			t.Errorf("space on button %d emitted %T, want no command", focus, testutil.Msg(cmd))
		}
		if m.focused != focus {
			t.Errorf("space moved focus from %d to %d", focus, m.focused)
		}
	}
}
