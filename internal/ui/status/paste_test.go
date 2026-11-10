package status

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/status/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: a paste reaches the focused field of the view (the filter bar
// or the monitor form), a multi-line paste becomes spaces in a one-line field,
// and nothing is triggered (Rule 111).
func TestPasteReachesTheFilterBar(t *testing.T) {
	tests := []struct{ name, paste, want string }{
		{"word", "api", "api"},
		{"with space", "x y", "x y"},
		{"multi-line", "a\nb", "a b"},
		{"action letters", "Dq", "Dq"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := feed(t, loadedModel(t), testutil.Key("/"))
			m = feed(t, m, testutil.Paste(tc.paste))
			if got := m.filterBar.SearchQuery(); got != tc.want {
				t.Errorf("search query = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if m.confirmModal != nil || m.componentForm != nil {
				t.Error("the paste opened a modal")
			}
		})
	}
}

func TestPasteReachesTheMonitorForm(t *testing.T) {
	tests := []struct{ name, paste, want string }{
		{"word", "web-monitor", "web-monitor"},
		{"with space", "x y", "x y"},
		{"multi-line", "a\nb", "a b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := feed(t, loadedModel(t), testutil.Key(keymap.New))
			if m.componentForm == nil {
				t.Fatal("the form did not open")
			}
			m, cmd := step(t, m, testutil.Paste(tc.paste))
			if m.componentForm == nil {
				t.Fatal("the paste closed the form")
			}
			if view := ansi.Strip(m.componentForm.View()); !strings.Contains(view, tc.want) {
				t.Errorf("form does not show %q after the paste:\n%s", tc.want, view)
			}
			for _, msg := range testutil.Msgs(cmd) {
				if _, ok := msg.(components.ComponentFormSubmitMsg); ok {
					t.Error("the paste submitted the form")
				}
			}
		})
	}
}
