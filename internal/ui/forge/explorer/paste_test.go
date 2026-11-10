package explorer

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: a paste reaches the filter bar once `/` has opened it,
// newlines become spaces, and no view action fires (Rule 111: the uppercase
// vocabulary is text while the bar has the keyboard).
func TestPasteReachesTheFilterBar(t *testing.T) {
	tests := []struct{ name, paste, want string }{
		{"word", "alpha", "alpha"},
		{"with space", "x y", "x y"},
		{"multi-line", "a\nb", "a b"},
		{"action letters", "DSCqK", "DSCqK"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := loadedModel(t)
			m, _ = step(t, m, testutil.Key("/"))
			m, _ = step(t, m, testutil.Paste(tc.paste))
			if got := m.table.FilterBar().SearchQuery(); got != tc.want {
				t.Errorf("search query = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
		})
	}
}

// A paste is text for whichever field has the keyboard, not only for the
// filter bar: the creation form, the "g" prompt and the clone list's own
// filter bar each own a field and each must be reached (DEVDESK-201 D3).

func TestPasteReachesTheCreationFormFields(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.New))
	if m.mode != ModeCreatingProject || m.creationForm == nil {
		t.Fatalf("mode = %v, form = %v, want the creation form open", m.mode, m.creationForm)
	}
	m = feed(t, m, testutil.Key("down")) // name
	m = feed(t, m, testutil.Paste("pastedname"))
	if view := m.creationForm.View(); !strings.Contains(view, "pastedname") {
		t.Errorf("the name field does not show the pasted text:\n%s", view)
	}
	m = feed(t, m, testutil.Key("down")) // description
	m = feed(t, m, testutil.Paste("pasteddesc"))
	if view := m.creationForm.View(); !strings.Contains(view, "pasteddesc") {
		t.Errorf("the description field does not show the pasted text:\n%s", view)
	}
	if m.mode != ModeCreatingProject {
		t.Errorf("mode = %v after the pastes, want the form still open", m.mode)
	}
}

func TestPasteReachesTheFuzzyFinderQuery(t *testing.T) {
	m := feed(t, indexedModel(t), testutil.Key("g"))
	if m.mode != ModeFuzzyFinding || m.finder == nil {
		t.Fatalf("mode = %v, want the prompt open", m.mode)
	}
	m = feed(t, m, testutil.Paste("pastedquery"))
	if bar := m.finder.RenderBar(80); !strings.Contains(bar, "pastedquery") {
		t.Errorf("the finder's query bar does not show the pasted text:\n%s", bar)
	}
	if m.mode != ModeFuzzyFinding {
		t.Errorf("mode = %v after the paste, want the prompt still open", m.mode)
	}
}

func TestPasteReachesTheCloneListFilterBar(t *testing.T) {
	h := cloning(t)
	m := feed(t, h.m, testutil.Key("/"))
	if !m.clone.table.InEditMode() {
		t.Fatal("the clone list's filter bar did not open on /")
	}
	m = feed(t, m, testutil.Paste("pastedfilter"))
	if got := m.clone.table.FilterBar().SearchQuery(); got != "pastedfilter" {
		t.Errorf("clone list search query = %q, want %q", got, "pastedfilter")
	}
	if m.mode != ModeCloning {
		t.Errorf("mode = %v after the paste, want the clone list still open", m.mode)
	}
}

// With no field holding the keyboard, a paste changes nothing: it is not read
// as keys (Rule 111), so neither "g" nor "C" inside it opens anything.
func TestPasteWithoutAFocusedFieldIsIgnored(t *testing.T) {
	m := loadedModel(t)
	got := feed(t, m, testutil.Paste("gCNDW\nx"))
	if got.mode != ModeNormal || got.finder != nil || got.creationForm != nil {
		t.Errorf("mode = %v, finder = %v, form = %v after a paste, want none", got.mode, got.finder, got.creationForm)
	}
	if q := got.table.FilterBar().SearchQuery(); q != "" {
		t.Errorf("search query = %q, want empty", q)
	}

	// Behind the delete confirmation the paste is dropped too.
	m = feed(t, drilledModel(t), testutil.Key("down"), testutil.Key(keymap.Delete))
	before := m.mode
	m = feed(t, m, testutil.Paste("y"))
	if m.mode != before {
		t.Errorf("mode = %v after a paste behind the modal, want %v", m.mode, before)
	}
}
