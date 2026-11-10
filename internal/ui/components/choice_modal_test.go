package components

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Enter and Space pick the focused button alike (Rule 135). DEVDESK-200
// characterization: nothing covered the space before the key name moved to a
// constant.
func TestChoiceModalSpacePicksTheFocusedButtonLikeEnter(t *testing.T) {
	for _, key := range []string{"enter", " "} {
		t.Run("action "+key, func(t *testing.T) {
			m := NewChoiceModal("Stop", "How?", "Stop", "Restart")
			m.focused = 1

			_, cmd := m.Update(testutil.Key(key))
			got, ok := testutil.MsgOf[ChoiceModalPickedMsg](cmd)
			if !ok {
				t.Fatalf("%q on an action produced %T, want ChoiceModalPickedMsg", key, testutil.Msg(cmd))
			}
			if got.Index != 1 || got.Label != "Restart" {
				t.Errorf("%q picked %+v, want Index 1 / Restart", key, got)
			}
		})
		t.Run("cancel "+key, func(t *testing.T) {
			m := NewChoiceModal("Stop", "How?", "Stop", "Restart")
			m.focused = 2 // Cancel

			_, cmd := m.Update(testutil.Key(key))
			if _, ok := testutil.MsgOf[ChoiceModalCancelledMsg](cmd); !ok {
				t.Errorf("%q on Cancel produced %T, want ChoiceModalCancelledMsg", key, testutil.Msg(cmd))
			}
		})
	}
}
