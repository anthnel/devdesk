package setup

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: a paste reaches the text field of the current wizard step,
// newlines become spaces, and nothing advances or goes back (Rule 111). Pasting
// into the URL field counts as touching it, like typing does.
func TestPasteReachesTheFocusedStepField(t *testing.T) {
	tests := []struct {
		name  string
		step  int
		paste string
		want  string
		read  func(m Model) string
	}{
		{"context name", stepContextName, "staging", "staging", func(m Model) string { return m.contextNameInput.Value() }},
		{"context name, multi-line", stepContextName, "a\nb", "a b", func(m Model) string { return m.contextNameInput.Value() }},
		{"context name, action letters", stepContextName, "Dq?:", "Dq?:", func(m Model) string { return m.contextNameInput.Value() }},
		{"forge URL", stepForgeURL, "https://git.internal", "https://git.internal", func(m Model) string { return m.forgeURLInput.Value() }},
		{"forge URL, multi-line", stepForgeURL, "a\nb", "a b", func(m Model) string { return m.forgeURLInput.Value() }},
		{"namespace", stepForgeNamespace, "team/sub", "team/sub", func(m Model) string { return m.forgeNamespaceInput.Value() }},
		{"token", stepForgeToken, "glpat-abc", "glpat-abc", func(m Model) string { return m.forgeTokenInput.Value() }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateHome(t)
			m := past(t, New(), "")
			m.step = tc.step
			m.updateFocus()
			m.contextNameInput.SetValue("")
			m.forgeURLInput.SetValue("")
			m.forgeURLTouched = false

			m = feed(t, m, testutil.Paste(tc.paste))

			if got := tc.read(m); got != tc.want {
				t.Errorf("field = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if m.step != tc.step {
				t.Errorf("step = %d after the paste, want %d", m.step, tc.step)
			}
			if tc.step == stepForgeURL && !m.forgeURLTouched {
				t.Error("pasting into the URL did not mark it as touched")
			}
		})
	}
}
