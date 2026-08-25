package configuration

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Rule 130: the three controls are always listed, and the two the focused field
// does not take are greyed. This is a form, where the cursor moves constantly —
// a column that changed shape on every ↑↓ was changing more often than any
// other in the application.
func TestTheControlColumnKeepsItsShapeAcrossFieldKinds(t *testing.T) {
	m := newModel(t)

	var want []string
	seen := map[fieldKind]bool{}

	for i := range m.sections[m.activeTab].Fields {
		m.focusedField = i
		got := testutil.ShortcutKeys(m.GetShortcuts())
		if want == nil {
			want = got
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("field %q advertises %v, want the same keys as the first field %v",
				m.current().Label, got, want)
		}
		seen[m.current().Kind] = true
	}

	// A tab with one kind of field would pass the loop above by testing
	// nothing, so the assertion is that the walk actually crossed several.
	if len(seen) < 2 {
		t.Fatalf("the first tab holds only %d kind(s) of field; this test needs a mix", len(seen))
	}
}

func TestOnlyTheControlTheFieldTakesIsOffered(t *testing.T) {
	for _, tt := range []struct {
		label   string
		enabled string
		greyed  []string
	}{
		{"Theme", "←→", []string{"space"}},
		{"Show hidden files", "space", []string{"←→"}},
		{"Workspaces dir", "", []string{"←→", "space"}},
	} {
		t.Run(tt.label, func(t *testing.T) {
			m := focusOn(t, newModel(t), tt.label)
			got := m.GetShortcuts()

			if tt.enabled != "" && !testutil.ShortcutEnabled(got, tt.enabled) {
				t.Errorf("%q is not offered on a %q field", tt.enabled, tt.label)
			}
			for _, key := range tt.greyed {
				if !testutil.HasShortcut(got, key) {
					t.Errorf("%q disappeared instead of being greyed", key)
				} else if !testutil.ShortcutDisabled(got, key) {
					t.Errorf("%q is offered on a %q field, where it does nothing", key, tt.label)
				}
			}
			// ↑↓ always moves; only its wording changes.
			if !testutil.ShortcutEnabled(got, "↑↓") {
				t.Error("↑↓ is greyed; it moves between fields whatever the kind")
			}
		})
	}
}
