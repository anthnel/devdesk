package configuration

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-266: Space reaches a focused text field (a path or a command can
// hold one), and still toggles when the focus is on a checkbox.
func TestSpaceIsInsertedInTheFocusedTextField(t *testing.T) {
	for _, label := range []string{"IDE command", "Terminal command", "Workspaces dir"} {
		t.Run(label, func(t *testing.T) {
			m := focusOn(t, newModel(t), label)
			if !m.current().takesText() {
				t.Fatalf("%q is not a text field", label)
			}
			m.input.SetValue("")

			m = feed(t, m, testutil.Type("a")[0], testutil.Key("space"), testutil.Type("b")[0])

			if got := m.input.Value(); got != "a b" {
				t.Errorf("%s field = %q after typing a, space, b, want %q", label, got, "a b")
			}
		})
	}
}

func TestSpaceStillTogglesACheckbox(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	m := focusOn(t, newModel(t), "Show hidden files")
	f := m.current()
	if f.Kind != kindToggle {
		t.Fatalf("Show hidden files is not a toggle")
	}
	before := f.Bool(m.config)

	m = feed(t, m, testutil.Key("space"))

	if got := f.Bool(m.config); got == before {
		t.Errorf("space left the checkbox at %v, want it flipped", before)
	}
}

// DEVDESK-266: Space on the secret row still reveals it, through the real key
// path (nothing sets m.shown by hand).
func TestSpaceStillRevealsTheSecret(t *testing.T) {
	m := focusOn(t, serving(t), "Token")
	if m.current().Kind != kindSecret {
		t.Fatalf("Token is not a secret row")
	}
	if m.shown["Token"] {
		t.Fatalf("Token starts revealed")
	}

	m = feed(t, m, testutil.Key("space"))

	if !m.shown["Token"] {
		t.Errorf("space left the Token hidden, want it revealed")
	}
}
