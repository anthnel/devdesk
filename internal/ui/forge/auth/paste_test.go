package auth

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: a paste reaches the focused token field and fires no action
// (Rule 111). The multi-line row proves newlines become spaces in a one-line
// field and that the submit key is not fired by a pasted newline.
func TestPasteReachesTheFocusedTokenField(t *testing.T) {
	tests := []struct {
		name  string
		typed string
		paste string
		want  string
	}{
		{"plain", "", "glpat-abc", "glpat-abc"},
		{"with spaces", "", "x y", "x y"},
		{"appends to typed text", "ab", "cd", "abcd"},
		{"multi-line becomes spaces", "", "a\nb", "a b"},
		{"letters that are actions elsewhere", "", "Dq?:", "Dq?:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t, testConfig(), newFakeStorage())
			if tc.typed != "" {
				m = feed(t, m, testutil.Type(tc.typed)...)
			}
			m, cmd := step(t, m, testutil.Paste(tc.paste))

			if got := m.tokenInput.Value(); got != tc.want {
				t.Errorf("token field = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if m.authenticating {
				t.Error("the paste started a login")
			}
			if cmd != nil {
				if _, quit := cmd().(tea.QuitMsg); quit {
					t.Error("the paste quit the application")
				}
			}
		})
	}
}
