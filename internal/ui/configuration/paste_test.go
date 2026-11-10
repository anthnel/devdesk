package configuration

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: a paste reaches the focused text field, a newline becomes a
// space, and no key action fires (Rule 111: "D", "q", "c" and "?" are text
// here). Nothing is committed, so nothing is saved.
func TestPasteReachesTheFocusedTextField(t *testing.T) {
	tests := []struct {
		name, label, paste, want string
	}{
		{"URL", "URL", "https://git.example.com", "https://git.example.com"},
		{"with a space", "IDE command", "code -n", "code -n"},
		{"multi-line", "IDE command", "a\nb", "a b"},
		{"action letters", "Terminal command", "Dqc?:", "Dqc?:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := focusOn(t, newModel(t), tc.label)
			m.input.SetValue("")
			tab, field := m.activeTab, m.focusedField

			m = feed(t, m, testutil.Paste(tc.paste))

			if got := m.input.Value(); got != tc.want {
				t.Errorf("%s field = %q after pasting %q, want %q", tc.label, got, tc.paste, tc.want)
			}
			if m.confirmModal != nil {
				t.Error("the paste opened a confirmation")
			}
			if m.activeTab != tab || m.focusedField != field {
				t.Errorf("the paste moved focus: tab %d->%d field %d->%d", tab, m.activeTab, field, m.focusedField)
			}
		})
	}
}
