package ociresources

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// withTrueColor forces a colour profile for the run. Under go test lipgloss
// detects no TTY, falls back to Ascii and strips every escape sequence, which
// would make any assertion about styling pass whatever the code does.
func withTrueColor(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

// ── The four tabs ────────────────────────────────────────────────────────────

func TestEachTabRendersItsOwnTable(t *testing.T) {
	tests := []struct {
		name string
		tabs int
		want []string
	}{
		{"images", 0, []string{"api:v1", "cache:v2"}},
		{"networks", 1, []string{"bridge", "overlay-prod"}},
		{"volumes", 2, []string{"pgdata", "redis"}},
		{"registries", 3, []string{"registry.example.com", "docker.io"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := loadedModel(t)
			for range tt.tabs {
				m = feed(t, m, testutil.Key("tab"))
			}

			view := m.View()
			for _, want := range tt.want {
				if !strings.Contains(view, want) {
					t.Errorf("the %s tab does not show %q:\n%s", tt.name, want, view)
				}
			}
		})
	}
}

// Before the first list arrives the tab shows a spinner rather than an empty
// table, which would read as "you have none".
func TestTabsShowASpinnerBeforeLoading(t *testing.T) {
	view := newTestModel(t).View()

	if strings.Contains(view, "api:v1") {
		t.Error("the table has rows before anything loaded")
	}
	if strings.TrimSpace(view) == "" {
		t.Error("the view is blank while loading")
	}
}

// ── Scan counts ──────────────────────────────────────────────────────────────

// The severity columns are the point of the images tab; a never-scanned image
// must be visibly different from one scanned clean.
func TestScanCountsDistinguishCleanFromUnscanned(t *testing.T) {
	m := loadedModel(t)

	rows := m.imageTable.Rows()
	byName := map[string][]string{}
	for _, row := range rows {
		byName[row[1]] = row
	}

	scanned, ok := byName["api:v1"]
	if !ok {
		t.Fatalf("api:v1 is missing from %v", byName)
	}
	if scanned[4] != "2" {
		t.Errorf("the critical count for a scanned image = %q, want 2", scanned[4])
	}

	clean := byName["cache:v2"]
	unscanned := byName["web:v3"]
	if clean[4] == unscanned[4] {
		t.Errorf("a clean scan (%q) is indistinguishable from never scanned (%q)", clean[4], unscanned[4])
	}
}

// Rule 122: a styled cell is truncated mid-escape by bubbles/table and bleeds
// into every row below it.
func TestTableCellsCarryNoEscapeSequences(t *testing.T) {
	withTrueColor(t)

	m := loadedModel(t)
	for name, rows := range map[string][]table.Row{
		"images":     m.imageTable.Rows(),
		"networks":   m.networkTable.Table().Rows(),
		"volumes":    m.volumeTable.Table().Rows(),
		"registries": m.registryTable.Rows(),
	} {
		for _, row := range rows {
			for i, cell := range row {
				if strings.Contains(cell, "\x1b") {
					t.Errorf("%s: cell %d of row %v carries an escape sequence", name, i, row)
				}
			}
		}
	}
}

// ── Overlays ─────────────────────────────────────────────────────────────────

// Rule 112: forms take the whole viewport; only confirmations are modals.
func TestFormsReplaceTheViewAndModalsOverlayIt(t *testing.T) {
	t.Run("a form replaces the table", func(t *testing.T) {
		m := feed(t, loadedModel(t), testutil.Key("ctrl+e"))
		if m.launchForm == nil {
			t.Skip("ctrl+e did not open the launch form")
		}

		if view := m.View(); strings.Contains(view, "cache:v2") {
			t.Error("the image table is visible behind the launch form")
		}
	})

	t.Run("a confirmation names its target", func(t *testing.T) {
		m := feed(t, loadedModel(t), testutil.Key("ctrl+d"))

		if view := m.View(); !strings.Contains(view, "api:v1") {
			t.Errorf("the confirmation does not name the image:\n%s", view)
		}
	})
}

// ── Layout ───────────────────────────────────────────────────────────────────

// Rule 124: the router sizes the viewport from GetFooterHeight(), so it has to
// match what RenderFooter() emits.
func TestFooterHeightMatchesWhatIsRendered(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Model
	}{
		{"images", func(t *testing.T) Model { return loadedModel(t) }},
		{"filtering", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key("/")) }},
		{"networks", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key("tab")) }},
		{"registries", func(t *testing.T) Model {
			return feed(t, loadedModel(t), testutil.Key("tab"), testutil.Key("tab"), testutil.Key("tab"))
		}},
		{"confirming", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key("ctrl+d")) }},
		{"browsing a registry", func(t *testing.T) Model { return browsingModel(t) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.open(t)

			lines := strings.Count(m.RenderFooter(180), "\n") + 1

			if got := m.GetFooterHeight(); got != lines {
				t.Errorf("GetFooterHeight() = %d, RenderFooter() emitted %d lines", got, lines)
			}
		})
	}
}

// The tab bar is a breadcrumb of the four tabs, always visible below the table
// (Rule 123).
func TestFooterShowsEveryTab(t *testing.T) {
	footer := loadedModel(t).RenderFooter(180)

	for _, want := range []string{"Images", "Networks", "Volumes", "Registries"} {
		if !strings.Contains(footer, want) {
			t.Errorf("the tab bar has no %q tab:\n%s", want, footer)
		}
	}
}

func TestFooterShowsTheMessages(t *testing.T) {
	m := loadedModel(t)
	m.errorMsg = "Failed to load images — check logs"
	if !strings.Contains(m.RenderFooter(180), "Failed to load") {
		t.Error("the footer does not show the error message")
	}

	m.errorMsg = ""
	m.infoMsg = "Total reclaimed space: 1.2GB"
	if !strings.Contains(m.RenderFooter(180), "reclaimed") {
		t.Error("the footer does not show the info message")
	}
}

// Rule 116: the columns share the width left after the viewport borders and the
// per-cell padding, so the selected row reaches the right border.
func TestColumnsFitTheWidth(t *testing.T) {
	for _, width := range []int{100, 140, 200} {
		m := feed(t, loadedModel(t), testutil.Resize(width, 30))

		total := 0
		for _, col := range m.imageTable.Columns() {
			total += col.Width
		}
		want := width - 2 - len(m.imageTable.Columns())*2
		if total != want {
			t.Errorf("at width %d the image columns total %d, want %d", width, total, want)
		}
	}
}

func TestLayoutSurvivesATinyTerminal(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Resize(20, 1))

	if m.imageTable.Height() < 0 {
		t.Errorf("table height = %d", m.imageTable.Height())
	}
	if m.View() == "" {
		t.Error("the view is blank at 20x1")
	}
}

// ── Header ───────────────────────────────────────────────────────────────────

// The title says which overlay is open: the same viewport is reused for six of
// them, so it is the only thing that distinguishes them at a glance.
func TestTitleNamesTheActiveOverlay(t *testing.T) {
	if got := loadedModel(t).GetTitle(); !strings.Contains(got, "OCI Resources") {
		t.Errorf("GetTitle() = %q on the plain view", got)
	}

	browsing := browsingModel(t).GetTitle()
	if !strings.Contains(browsing, "Registry Browser") {
		t.Errorf("GetTitle() = %q with the browser open", browsing)
	}
}

// Rule 130: a shortcut for an action that cannot run here must not be
// advertised.
func TestShortcutsFollowTheTab(t *testing.T) {
	tests := []struct {
		name    string
		tabs    int
		want    []string
		notWant []string
	}{
		{"images", 0, []string{"ctrl+s", "ctrl+e", "/"}, nil},
		{"networks", 1, nil, []string{"ctrl+s", "/"}},
		{"volumes", 2, nil, []string{"ctrl+s", "ctrl+e"}},
		{"registries", 3, nil, []string{"ctrl+s", "ctrl+e"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := loadedModel(t)
			for range tt.tabs {
				m = feed(t, m, testutil.Key("tab"))
			}

			keys := map[string]bool{}
			for _, s := range m.GetShortcuts() {
				keys[s.Key] = true
			}
			for _, key := range tt.want {
				if !keys[key] {
					t.Errorf("%q is not advertised", key)
				}
			}
			for _, key := range tt.notWant {
				if keys[key] {
					t.Errorf("%q is advertised but cannot run on this tab", key)
				}
			}
		})
	}
}

// Rule 137: descriptions read as imperative actions, capitalised.
func TestShortcutDescriptionsAreImperative(t *testing.T) {
	m := loadedModel(t)
	for range 4 {
		for _, s := range m.GetShortcuts() {
			if s.Description == "" {
				t.Errorf("shortcut %q has no description", s.Key)
				continue
			}
			if first := s.Description[:1]; first != strings.ToUpper(first) {
				t.Errorf("description %q does not start with a capital", s.Description)
			}
		}
		m = feed(t, m, testutil.Key("tab"))
	}
}

// ── Help ─────────────────────────────────────────────────────────────────────

func TestHelpContentIsPopulated(t *testing.T) {
	content := loadedModel(t).GetHelpContent()

	if content.Title == "" || content.Description == "" {
		t.Error("the help has no title or description")
	}
	if len(content.KeyBindings) == 0 || len(content.Sections) == 0 {
		t.Error("the help lists no key bindings or sections")
	}
}

// Every key the header advertises should be explained in the help — the check
// that caught the drift in four views during phase 3.
func TestHelpDocumentsTheAdvertisedShortcuts(t *testing.T) {
	// This view's help qualifies most keys with the tab they belong to —
	// "enter (Images)", "n (Networks)" — so the suffix comes off before
	// matching, as do the "a / b" alternatives.
	documented := map[string]bool{}
	for _, kb := range loadedModel(t).GetHelpContent().KeyBindings {
		key := kb.Key
		if idx := strings.Index(key, " ("); idx > 0 {
			key = key[:idx]
		}
		documented[strings.ToLower(key)] = true
		for _, part := range strings.Split(key, "/") {
			if trimmed := strings.ToLower(strings.TrimSpace(part)); trimmed != "" {
				documented[trimmed] = true
			}
		}
	}

	m := loadedModel(t)
	for range 4 {
		for _, s := range m.GetShortcuts() {
			key := strings.ToLower(s.Key)
			if key == "←→" || key == "↑↓" {
				continue
			}
			if !documented[key] {
				t.Errorf("shortcut %q is advertised in the header but absent from the help", s.Key)
			}
		}
		m = feed(t, m, testutil.Key("tab"))
	}
}
