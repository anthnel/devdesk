package netdiag

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: a paste reaches the focused field on every tab (the
// diagnostics form, the three filter bars, the forward form), newlines become
// spaces, and no key action fires (Rule 111). Nothing here runs a check or
// opens a socket: the model is driven by messages only.
func TestPasteReachesTheDiagnosticsFields(t *testing.T) {
	tests := []struct {
		name  string
		downs int
		paste string
		want  string
		read  func(m *Model) string
	}{
		{"target", 0, "example.com", "example.com", func(m *Model) string { return m.targetInput.Value() }},
		{"target, multi-line", 0, "a\nb", "a b", func(m *Model) string { return m.targetInput.Value() }},
		{"target, action letters", 0, "Dqt?:", "Dqt?:", func(m *Model) string { return m.targetInput.Value() }},
		{"port", 1, "8443", "8443", func(m *Model) string { return m.portInput.Value() }},
		{"resolver", 2, "1.1.1.1", "1.1.1.1", func(m *Model) string { return m.dnsServerInput.Value() }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			for range tc.downs {
				m = feed(t, m, testutil.Key("down"))
			}
			m.targetInput.SetValue("")
			m.portInput.SetValue("")
			m.dnsServerInput.SetValue("")

			m, _ = step(t, m, testutil.Paste(tc.paste))

			if got := tc.read(m); got != tc.want {
				t.Errorf("field = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if m.state != StateInput || m.activeTab != tabDiagnostics {
				t.Errorf("state %v tab %v: the paste moved the view", m.state, m.activeTab)
			}
		})
	}
}

func TestPasteReachesTheTabFilterBars(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) *Model
		query func(m *Model) string
	}{
		{"ports", portsModel, func(m *Model) string { return m.portsModel.table.FilterBar().SearchQuery() }},
		{"interfaces", func(t *testing.T) *Model { return interfacesModel(t) },
			func(m *Model) string { return m.interfacesModel.table.FilterBar().SearchQuery() }},
		{"forward", onForwardTab, func(m *Model) string { return m.forwardModel.table.FilterBar().SearchQuery() }},
	}
	pastes := []struct{ name, paste, want string }{
		{"word", "nginx", "nginx"},
		{"multi-line", "a\nb", "a b"},
		{"action letters", "tulnzK", "tulnzK"},
	}
	for _, tc := range tests {
		for _, p := range pastes {
			t.Run(tc.name+"/"+p.name, func(t *testing.T) {
				m := feed(t, tc.setup(t), testutil.Key("/"))
				tab := m.activeTab
				m = feed(t, m, testutil.Paste(p.paste))
				if got := tc.query(m); got != p.want {
					t.Errorf("search query = %q after pasting %q, want %q", got, p.paste, p.want)
				}
				if m.activeTab != tab {
					t.Error("the paste changed the tab")
				}
			})
		}
	}
}

func TestPasteReachesTheForwardFormFields(t *testing.T) {
	tests := []struct {
		name  string
		http  bool
		downs int
		paste string
		want  string
		read  func(f *ForwardForm) string
	}{
		{"port", false, 1, "8080", "8080", func(f *ForwardForm) string { return f.portInput.Value() }},
		{"target", false, 2, "10.0.0.5:80", "10.0.0.5:80", func(f *ForwardForm) string { return f.targetInput.Value() }},
		{"target, multi-line", false, 2, "a\nb", "a b", func(f *ForwardForm) string { return f.targetInput.Value() }},
		{"route name", true, 1, "my-app", "my-app", func(f *ForwardForm) string { return f.nameInput.Value() }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := onForwardTab(t)
			m = feed(t, m, testutil.Key(keymap.New))
			form := m.forwardModel.form
			if form == nil {
				t.Fatal("the forward form did not open")
			}
			if tc.http {
				m = feed(t, m, testutil.Key("right")) // Type: TCP -> HTTP
			}
			for range tc.downs {
				m = feed(t, m, testutil.Key("down"))
			}
			m = feed(t, m, testutil.Paste(tc.paste))

			if m.forwardModel.form == nil {
				t.Fatal("the paste closed the form")
			}
			if got := tc.read(m.forwardModel.form); got != tc.want {
				t.Errorf("field = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
		})
	}
}
