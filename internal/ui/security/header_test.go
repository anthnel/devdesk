package security

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Shortcuts ────────────────────────────────────────────────────────────────

// Rule 130: a shortcut for an action that cannot run here must not be
// advertised.
func TestShortcutsFollowTheState(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Model
		// enabled: advertised and acts here.
		// disabled: advertised, greyed — the same screen, a row or a tab it
		//   does not apply to (Rule 130).
		// absent: belongs to another screen entirely, so the list is replaced.
		enabled  []string
		disabled []string
		absent   []string
	}{
		{
			name:    "the inventory",
			open:    func(t *testing.T) Model { return inventoryModel(t, inventoryFixtures()...) },
			enabled: []string{"enter", keymap.Scan, keymap.ScanAll, "/"},
			absent:  []string{"tab", keymap.Exclude, "."},
		},
		{
			// The four severity toggles were bound and unadvertised: a user had
			// to read the help to learn the view filters at all.
			name:     "the CVE tab",
			open:     func(t *testing.T) Model { return scannedModel(t) },
			enabled:  []string{"tab", "enter", ".", "/", "c", "h", "m", "l", "ctrl+r"},
			disabled: []string{keymap.Exclude},
			absent:   []string{"space"},
		},
		{
			// '.' is the sort, so it applies to every tab — it was advertised
			// on three of four back when it cycled the severity floor.
			name: "the secrets tab",
			open: func(t *testing.T) Model {
				m := scannedModel(t)
				m.switchTab(TabSecrets)
				return m
			},
			enabled: []string{keymap.Exclude, "."},
			absent:  []string{"space"},
		},
		{
			// While the search has the keyboard, every other key is a
			// character; advertising them would be advertising what they no
			// longer do.
			name: "the results, searching",
			open: func(t *testing.T) Model {
				return feed(t, scannedModel(t), testutil.Key("/"))
			},
			enabled: []string{"enter/esc"},
			absent:  []string{"c", ".", "tab"},
		},
		{
			name:    "the details",
			open:    func(t *testing.T) Model { return detailsModel(t) },
			enabled: []string{"esc", keymap.Web},
			absent:  []string{"tab", "ctrl+r"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.open(t).GetShortcuts()

			for _, key := range tt.enabled {
				if !testutil.ShortcutEnabled(got, key) {
					t.Errorf("%q is not offered here", key)
				}
			}
			for _, key := range tt.disabled {
				if !testutil.HasShortcut(got, key) {
					t.Errorf("%q disappeared instead of being greyed", key)
				} else if !testutil.ShortcutDisabled(got, key) {
					t.Errorf("%q is offered where it cannot run", key)
				}
			}
			for _, key := range tt.absent {
				if testutil.HasShortcut(got, key) {
					t.Errorf("%q is advertised but belongs to another screen", key)
				}
			}
		})
	}
}

// 'o' opens the first advisory link, so it is greyed — never dropped — for a
// finding that has none (Rule 130).
func TestOpenReferenceShortcutNeedsAReference(t *testing.T) {
	// The second CVE in the fixtures carries no references.
	m := feed(t, scannedModel(t), testutil.Key("down"), testutil.Key("enter"))

	if !testutil.HasShortcut(m.GetShortcuts(), keymap.Web) {
		t.Fatal("the reference key disappeared for a finding with no reference instead of being greyed")
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Web) {
		t.Error("'o' is offered for a finding with no reference")
	}
}

// Rule 137: descriptions read as imperative actions, capitalised.
func TestShortcutDescriptionsAreImperative(t *testing.T) {
	models := []Model{
		inventoryModel(t, inventoryFixtures()...),
		scannedModel(t),
		detailsModel(t),
	}

	for _, m := range models {
		for _, s := range m.GetShortcuts() {
			if s.Description == "" {
				t.Errorf("shortcut %q has no description", s.Key)
				continue
			}
			if first := s.Description[:1]; first != strings.ToUpper(first) {
				t.Errorf("description %q does not start with a capital", s.Description)
			}
		}
	}
}

// ── Title and header info ────────────────────────────────────────────────────

// The results table says nothing about its own target, so the title carries it.
// On the inventory the title carries the context instead: the caches are scoped
// to one, and two contexts hold rows that look identical.
func TestTitleNamesTheTargetOnceScanned(t *testing.T) {
	inventory := inventoryModel(t).GetTitle()
	if !strings.Contains(inventory, "Inventory") {
		t.Errorf("GetTitle() = %q on the inventory", inventory)
	}

	scanned := scannedModel(t).GetTitle()
	if !strings.Contains(scanned, "/tmp/repo") {
		t.Errorf("GetTitle() = %q on the results, want the target named", scanned)
	}
}

// The header carries the context and one count — and, on the results, how much
// of it is fixable — and nothing else.
//
// buildInfoLines renders exactly headerMinHeight lines and drops the rest in
// silence, so an unbounded info list is not a cosmetic problem. The states are
// checked together because the old header ignored the state entirely and showed
// the same seven fields whatever was on screen.
func TestTheHeaderCarriesTheContextAndOneCount(t *testing.T) {
	tests := []struct {
		name     string
		open     func(t *testing.T) Model
		wantKeys []string
	}{
		{"the inventory", func(t *testing.T) Model {
			return inventoryModel(t, inventoryFixtures()...)
		}, []string{"Context", "Targets"}},
		{"the results", scannedModel, []string{"Context", "Findings", "Fixable"}},
		{"the details", detailsModel, []string{"Context", "Findings", "Fixable"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := tt.open(t).GetHeaderInfo("work")

			var keys []string
			for _, i := range info {
				keys = append(keys, i.Key)
			}
			if !equal(keys, tt.wantKeys) {
				t.Errorf("header keys = %v, want exactly %v", keys, tt.wantKeys)
			}
			if info[0].Value != "work" {
				t.Errorf("Context = %q, want the context the router passed", info[0].Value)
			}
		})
	}
}

// The counts name what the state is a list of, and are the real ones.
func TestTheHeaderCountMatchesWhatIsOnScreen(t *testing.T) {
	inventory := inventoryModel(t, inventoryFixtures()...).GetHeaderInfo("work")
	if inventory[1].Value != "2" {
		t.Errorf("Targets = %q, want the two cached targets", inventory[1].Value)
	}

	results := scannedModel(t).GetHeaderInfo("work")
	want := strconv.Itoa(resultFixture().TotalFindings())
	if results[1].Value != want {
		t.Errorf("Findings = %q, want %q", results[1].Value, want)
	}
}

func TestGetIconIsEmpty(t *testing.T) {
	if got := inventoryModel(t, inventoryFixtures()...).GetIcon(); got != "" {
		t.Errorf("GetIcon() = %q; the title carries the icon", got)
	}
}

// ── Help ─────────────────────────────────────────────────────────────────────

func TestHelpContentIsPopulated(t *testing.T) {
	content := inventoryModel(t, inventoryFixtures()...).GetHelpContent()

	if content.Title == "" || content.Description == "" {
		t.Error("the help has no title or description")
	}
	if len(content.KeyBindings) == 0 || len(content.Sections) == 0 {
		t.Error("the help lists no key bindings or sections")
	}
}

// Every key the header advertises should be explained in the help — the check
// that caught the drift in the status, containers, workspaces and explorer
// views.
func TestHelpDocumentsTheAdvertisedShortcuts(t *testing.T) {
	documented := map[string]bool{}
	for _, kb := range inventoryModel(t, inventoryFixtures()...).GetHelpContent().KeyBindings {
		documented[strings.ToLower(kb.Key)] = true
		for _, key := range strings.Split(kb.Key, "/") {
			if trimmed := strings.ToLower(strings.TrimSpace(key)); trimmed != "" {
				documented[trimmed] = true
			}
		}
	}

	states := []Model{
		inventoryModel(t, inventoryFixtures()...),
		scannedModel(t),
		func() Model { m := scannedModel(t); m.switchTab(TabSecrets); return m }(),
		detailsModel(t),
	}

	for _, m := range states {
		for _, s := range m.GetShortcuts() {
			key := strings.ToLower(s.Key)
			if key == "←→" || key == "esc" {
				continue // documented as separate arrow and esc entries
			}
			for _, part := range strings.Split(key, "/") {
				part = strings.TrimSpace(part)
				if part != "" && !documented[part] {
					t.Errorf("shortcut %q is advertised in the header but absent from the help", s.Key)
				}
			}
		}
	}
}

// The four tabs are the view's organising idea, so the help has to say what
// each one holds.
func TestHelpExplainsTheTabs(t *testing.T) {
	var body string
	for _, section := range inventoryModel(t, inventoryFixtures()...).GetHelpContent().Sections {
		body += section.Title + " " + section.Body + "\n"
	}

	for _, want := range []string{"CVE", "Secret", "License", "Misconfig"} {
		if !strings.Contains(body, want) {
			t.Errorf("the help does not explain the %s tab", want)
		}
	}
}

// Fixable splits what has a fixed version by what has to move to clear it, and
// says so when a cached result cannot tell.
func TestTheHeaderSaysHowMuchIsFixableAndOfWhatKind(t *testing.T) {
	result := &scan.Result{
		Target: "nexus/api:1.4", TargetType: scan.TargetImage,
		Findings: []scan.Finding{
			{ID: "CVE-1", Source: scan.SourceTrivy, Severity: scan.SeverityHigh, PkgName: "openssl", Class: scan.ClassOSPackages, FixedIn: "3.1.4"},
			{ID: "CVE-2", Source: scan.SourceTrivy, Severity: scan.SeverityHigh, PkgName: "musl", Class: scan.ClassOSPackages, FixedIn: "1.2.4"},
			{ID: "CVE-3", Source: scan.SourceTrivy, Severity: scan.SeverityHigh, PkgName: "lodash", Class: scan.ClassLangPackages, FixedIn: "4.17.21"},
			{ID: "CVE-4", Source: scan.SourceTrivy, Severity: scan.SeverityHigh, PkgName: "old", FixedIn: "1.0.1"},
			{ID: "CVE-5", Source: scan.SourceTrivy, Severity: scan.SeverityHigh, PkgName: "unfixed"},
		},
	}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})

	got := headerValue(t, m, "Fixable")
	if want := "4 (2 base, 1 deps, 1 unclassified)"; got != want {
		t.Errorf("Fixable = %q, want %q", got, want)
	}
}

func TestTheHeaderShowsZeroFixableWhenNothingHasAFix(t *testing.T) {
	result := &scan.Result{
		Target: "/tmp/repo", TargetType: scan.TargetDirectory,
		Findings: []scan.Finding{{ID: "CVE-1", Source: scan.SourceTrivy, Severity: scan.SeverityHigh, PkgName: "x"}},
	}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})
	if got := headerValue(t, m, "Fixable"); got != "0" {
		t.Errorf("Fixable = %q, want 0", got)
	}
}

func headerValue(t *testing.T, m Model, key string) string {
	t.Helper()
	for _, i := range m.GetHeaderInfo("work") {
		if i.Key == key {
			return i.Value
		}
	}
	t.Fatalf("no %q field in the header", key)
	return ""
}
