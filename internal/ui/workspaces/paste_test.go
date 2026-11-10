package workspaces

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: a paste reaches the focused field of the view (filter bar,
// create and rename input, fuzzy prompt), newlines become spaces, and no key
// action fires (Rule 111).
func TestPasteReachesTheFilterBar(t *testing.T) {
	tests := []struct{ name, paste, want string }{
		{"word", "clean", "clean"},
		{"multi-line", "a\nb", "a b"},
		{"action letters", "DSq", "DSq"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := feed(t, loadedModel(t), testutil.Key("/"))
			m = feed(t, m, testutil.Paste(tc.paste))
			if got := m.table.FilterBar().SearchQuery(); got != tc.want {
				t.Errorf("search query = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if m.mode != ModeNormal {
				t.Errorf("mode = %v after the paste, want ModeNormal", m.mode)
			}
		})
	}
}

func TestPasteReachesTheNameInputs(t *testing.T) {
	tests := []struct {
		name, opener, paste, want string
		mode                      ViewMode
	}{
		{"create", keymap.New, "new-project", "new-project", ModeAdding},
		{"create, multi-line", keymap.New, "a\nb", "a b", ModeAdding},
		{"create, action letters", keymap.New, "DMq", "DMq", ModeAdding},
		{"rename, multi-line", keymap.Rename, "a\nb", "a b", ModeRenaming},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := loadedModel(t)
			m.table.SetCursor(1)
			m = feed(t, m, testutil.Key(tc.opener))
			if m.mode != tc.mode || m.input == nil {
				t.Fatalf("mode = %v, want %v with an input", m.mode, tc.mode)
			}
			m.input.input.SetValue("")
			m, cmd := step(t, m, testutil.Paste(tc.paste))

			if got := m.input.input.Value(); got != tc.want {
				t.Errorf("input = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if m.mode != tc.mode || m.input == nil {
				t.Error("the paste closed the input")
			}
			for _, msg := range testutil.Msgs(cmd) {
				switch msg.(type) {
				case WorkspaceInputSubmitMsg, RenameInputSubmitMsg:
					t.Error("the paste submitted the input")
				}
			}
		})
	}
}

func TestPasteReachesTheFuzzyPrompt(t *testing.T) {
	m := newTestModel(t)
	m, _ = step(t, m, testutil.Key("g"))
	if m.mode != ModeFuzzyFinding {
		t.Fatal("g did not open the prompt")
	}
	m, _ = step(t, m, testutil.Paste("devdesk"))
	if bar := ansi.Strip(m.RenderFooter(80)); !strings.Contains(bar, "devdesk") {
		t.Errorf("the bar does not show the pasted query: %q", bar)
	}
	if m.mode != ModeFuzzyFinding {
		t.Error("the paste left the prompt")
	}
}
