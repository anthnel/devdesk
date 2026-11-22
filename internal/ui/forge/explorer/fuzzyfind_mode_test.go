package explorer

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/forgeindex"
	"github.com/anthnel/devdesk/internal/ui/fuzzy"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-678: tab flips the "g" prompt between fuzzy and exact matching. The
// same scenario runs in ws (internal/ui/workspaces/fuzzyfind_mode_test.go).

// modeFixture opens the prompt over an index holding workspaces/devdesk and
// docs/data-env (with their parent groups) and types "dev": a fuzzy query
// matches both paths, a literal one only the first.
func modeFixture(t *testing.T) Model {
	t.Helper()
	entries := []forgeindex.Entry{
		{ID: "1", Path: "workspaces", Name: "workspaces", Kind: forgeindex.KindNamespace},
		{ID: "2", Path: "docs", Name: "docs", Kind: forgeindex.KindNamespace},
		{ID: "10", Path: "workspaces/devdesk", Name: "devdesk", Parent: "workspaces", Kind: forgeindex.KindRepository},
		{ID: "20", Path: "docs/data-env", Name: "data-env", Parent: "docs", Kind: forgeindex.KindRepository},
	}
	state := authenticatedState(t)
	state.ForgeIndex = forgeindex.New("https://gitlab.example.com", "u", time.Now(), entries, nil)
	m := feed(t, New(testConfig(), state), tea.WindowSizeMsg{Width: 160, Height: 30})
	m = feed(t, m, testutil.Key("g"))
	if m.finder == nil {
		t.Fatal("g did not open the prompt")
	}
	for _, msg := range testutil.Type("dev") {
		m, _ = step(t, m, msg)
	}
	return m
}

func TestTabTogglesTheFinderMode(t *testing.T) {
	m := modeFixture(t)

	if got := m.finder.Mode(); got != fuzzy.ModeFuzzy {
		t.Fatalf("mode = %v on open, want fuzzy", got)
	}
	if footer := m.RenderFooter(120); !strings.Contains(footer, "2 match(es)") {
		t.Fatalf("fuzzy: RenderFooter = %q, want \"2 match(es)\"", footer)
	}

	m, _ = step(t, m, testutil.Key("tab"))
	if got := m.finder.Mode(); got != fuzzy.ModeExact {
		t.Errorf("mode = %v after tab, want exact", got)
	}
	footer := m.RenderFooter(120)
	if !strings.Contains(footer, "1 match(es)") {
		t.Errorf("exact: RenderFooter = %q, want \"1 match(es)\" with no retyping", footer)
	}
	if !strings.Contains(footer, "exact") {
		t.Errorf("exact: RenderFooter = %q, want the bar to say \"exact\"", footer)
	}
	if m.mode != ModeFuzzyFinding {
		t.Errorf("view mode = %v after tab, want the prompt still open", m.mode)
	}

	m, _ = step(t, m, testutil.Key("tab"))
	if got := m.finder.Mode(); got != fuzzy.ModeFuzzy {
		t.Errorf("mode = %v after the second tab, want fuzzy", got)
	}
	footer = m.RenderFooter(120)
	if !strings.Contains(footer, "2 match(es)") || !strings.Contains(footer, "fuzzy") {
		t.Errorf("fuzzy again: RenderFooter = %q, want \"2 match(es)\" and \"fuzzy\"", footer)
	}
}

// Criterion 6: whichever way the prompt closes, the next one starts in fuzzy.
func TestFinderReopensInFuzzyAfterExact(t *testing.T) {
	for _, closer := range []string{"esc", "enter"} {
		t.Run(closer, func(t *testing.T) {
			m := modeFixture(t)
			m, _ = step(t, m, testutil.Key("tab"))
			if got := m.finder.Mode(); got != fuzzy.ModeExact {
				t.Fatalf("mode = %v after tab, want exact", got)
			}

			m, cmd := step(t, m, testutil.Key(closer))
			switch closer {
			case "esc":
				cancel, ok := testutil.MsgOf[fuzzy.CancelMsg](cmd)
				if !ok {
					t.Fatal("esc did not cancel the prompt")
				}
				m = feed(t, m, cancel)
			case "enter":
				confirm, ok := testutil.MsgOf[fuzzy.ConfirmMsg](cmd)
				if !ok {
					t.Fatal("enter did not confirm the single exact match")
				}
				m = feed(t, m, confirm)
			}
			if m.mode == ModeFuzzyFinding || m.finder != nil {
				t.Fatalf("the prompt survived %s", closer)
			}

			m, _ = step(t, m, testutil.Key("g"))
			if m.finder == nil {
				t.Fatal("g did not reopen the prompt")
			}
			if got := m.finder.Mode(); got != fuzzy.ModeFuzzy {
				t.Errorf("mode = %v on reopening after %s, want fuzzy", got, closer)
			}
		})
	}
}

// Criterion 8, header half (Rule 130, 134): tab is advertised while the prompt
// is open, named after the mode it switches to, and the column is the same
// before and after the switch.
func TestFinderHeaderAdvertisesTheModeToggle(t *testing.T) {
	m := modeFixture(t)
	before := m.GetShortcuts()

	steps := []struct{ state, want string }{
		{"fuzzy", "Exact match"},
		{"exact", "Fuzzy match"},
	}
	for i, s := range steps {
		if i > 0 {
			m, _ = step(t, m, testutil.Key("tab"))
		}
		shortcuts := m.GetShortcuts()
		if !testutil.HasShortcut(shortcuts, "tab") {
			t.Fatalf("%s: tab is missing from the header: %v", s.state, testutil.ShortcutKeys(shortcuts))
		}
		if !testutil.ShortcutEnabled(shortcuts, "tab") {
			t.Errorf("%s: tab is greyed, it always applies", s.state)
		}
		for _, sc := range shortcuts {
			if sc.Key == "tab" && sc.Description != s.want {
				t.Errorf("%s: tab description = %q, want %q", s.state, sc.Description, s.want)
			}
		}
		if got, want := testutil.ShortcutKeys(shortcuts), testutil.ShortcutKeys(before); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: header keys = %v, want the same set as on open %v", s.state, got, want)
		}
	}
}

// Criterion 8, help half (Rule 114): the help names tab and the switch. The
// help is static (? is typed into the query while the prompt is open), so it
// is read from the view as it stands.
func TestHelpDocumentsTheFinderModeToggle(t *testing.T) {
	content := loadedModel(t).GetHelpContent()

	documented := false
	var keys []string
	for _, kb := range content.KeyBindings {
		keys = append(keys, kb.Key)
		if strings.EqualFold(kb.Key, "tab") {
			documented = true
		}
	}
	if !documented {
		t.Errorf("help has no tab key binding for the finder; bindings: %v", keys)
	}
	for _, s := range content.Sections {
		if !strings.HasPrefix(s.Title, "Finding anything") {
			continue
		}
		if !strings.Contains(s.Body, "tab") || !strings.Contains(strings.ToLower(s.Body), "exact") {
			t.Errorf("the finding help section does not explain tab and exact matching: %q", s.Body)
		}
	}
}
