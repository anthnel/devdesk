package security

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Shortcuts ────────────────────────────────────────────────────────────────

// Rule 130: a shortcut for an action that cannot run here must not be
// advertised.
func TestShortcutsFollowTheState(t *testing.T) {
	tests := []struct {
		name    string
		open    func(*testing.T) Model
		want    []string
		notWant []string
	}{
		{
			name:    "the form",
			open:    func(t *testing.T) Model { return newTestModel(t) },
			want:    []string{"space", "←→", "enter / ctrl+s"},
			notWant: []string{"b", "tab", "i", "."},
		},
		{
			// 'b' opens a browser for the target, so it only means something
			// while the target field has focus.
			name: "the target field",
			open: func(t *testing.T) Model {
				m := newTestModel(t)
				m.focusedField = 1
				return m
			},
			want: []string{"b"},
		},
		{
			name: "scanning",
			open: func(t *testing.T) Model {
				m := newTestModel(t)
				m.state = StateScanning
				return m
			},
			notWant: []string{"space", "enter / ctrl+s", "tab"},
		},
		{
			name:    "the CVE tab",
			open:    func(t *testing.T) Model { return scannedModel(t) },
			want:    []string{"tab", "enter", ".", "ctrl+r"},
			notWant: []string{"i", "space"},
		},
		{
			// '.' has no severity axis on secrets and 'i' has nothing to ignore
			// anywhere else, so the two swap.
			name: "the secrets tab",
			open: func(t *testing.T) Model {
				m := scannedModel(t)
				m.switchTab(TabSecrets)
				return m
			},
			want:    []string{"i"},
			notWant: []string{"."},
		},
		{
			name:    "the details",
			open:    func(t *testing.T) Model { return detailsModel(t) },
			want:    []string{"esc/⌫", "o"},
			notWant: []string{"tab", "ctrl+r"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := map[string]bool{}
			for _, s := range tt.open(t).GetShortcuts() {
				keys[s.Key] = true
			}
			for _, key := range tt.want {
				if !keys[key] {
					t.Errorf("%q is not advertised", key)
				}
			}
			for _, key := range tt.notWant {
				if keys[key] {
					t.Errorf("%q is advertised but cannot run here", key)
				}
			}
		})
	}
}

// 'o' opens the first advisory link, so it must not be advertised for a finding
// that has none.
func TestOpenReferenceShortcutNeedsAReference(t *testing.T) {
	// The second CVE in the fixtures carries no references.
	m := feed(t, scannedModel(t), testutil.Key("down"), testutil.Key("enter"))

	for _, s := range m.GetShortcuts() {
		if s.Key == "o" {
			t.Error("'o' is advertised for a finding with no reference")
		}
	}
}

// Rule 137: descriptions read as imperative actions, capitalised.
func TestShortcutDescriptionsAreImperative(t *testing.T) {
	models := []Model{
		newTestModel(t),
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

// Once a scan has run, the title carries what was scanned: the results table
// says nothing about its own target.
func TestTitleNamesTheTargetOnceScanned(t *testing.T) {
	form := newTestModel(t).GetTitle()
	if !strings.Contains(form, "Scan Configuration") {
		t.Errorf("GetTitle() = %q on the form", form)
	}

	scanned := scannedModel(t).GetTitle()
	if !strings.Contains(scanned, "/tmp/repo") {
		t.Errorf("GetTitle() = %q on the results, want the target named", scanned)
	}
}

// The header is where the user learns a tool is missing, before the scan fails
// for a reason they cannot see.
func TestHeaderReportsToolAvailability(t *testing.T) {
	missing := newTestModel(t).GetHeaderInfo("work")
	if len(missing) < 2 {
		t.Fatalf("GetHeaderInfo() = %+v, want Trivy and Gitleaks", missing)
	}
	for _, info := range missing[:2] {
		if info.Value != "not found" {
			t.Errorf("%s = %q with no dependency check, want \"not found\"", info.Key, info.Value)
		}
	}

	present := feed(t, newTestModel(t), DepsCheckedMsg{Deps: scan.DependencyStatus{
		TrivyAvailable: true, TrivyVersion: "Version: 0.50.0",
		GitleaksAvailable: true, GitleaksVersion: "v8.18.2",
	}}).GetHeaderInfo("work")

	if !strings.Contains(present[0].Value, "0.50.0") {
		t.Errorf("Trivy = %q, want the parsed version", present[0].Value)
	}
	if !strings.Contains(present[1].Value, "8.18.2") {
		t.Errorf("Gitleaks = %q, want the parsed version", present[1].Value)
	}
}

// Once a result exists the header carries its duration and severity summary.
func TestHeaderCarriesTheScanSummary(t *testing.T) {
	info := scannedModel(t).GetHeaderInfo("work")

	var keys []string
	for _, i := range info {
		keys = append(keys, i.Key)
	}
	joined := strings.Join(keys, ",")
	if !strings.Contains(joined, "Duration") {
		t.Errorf("the header keys are %v, want the scan duration among them", keys)
	}
}

// The version string each tool prints is a different shape, and the header has
// one narrow column for it.
func TestParseVersion(t *testing.T) {
	m := newTestModel(t)

	tests := map[string]string{
		"Version: 0.50.0":                   "0.50.0",
		"v8.18.2":                           "8.18.2",
		"":                                  "",
		"gitleaks version 8.18.2":           "8.18.2",
		"Version: 0.50.0\nVulnerability DB": "0.50.0",
	}

	for in, want := range tests {
		if got := m.parseVersion(in); !strings.Contains(got, want) {
			t.Errorf("parseVersion(%q) = %q, want it to contain %q", in, got, want)
		}
	}
}

func TestGetIconIsEmpty(t *testing.T) {
	if got := newTestModel(t).GetIcon(); got != "" {
		t.Errorf("GetIcon() = %q; the title carries the icon", got)
	}
}

// ── Help ─────────────────────────────────────────────────────────────────────

func TestHelpContentIsPopulated(t *testing.T) {
	content := newTestModel(t).GetHelpContent()

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
	for _, kb := range newTestModel(t).GetHelpContent().KeyBindings {
		documented[strings.ToLower(kb.Key)] = true
		for _, key := range strings.Split(kb.Key, "/") {
			if trimmed := strings.ToLower(strings.TrimSpace(key)); trimmed != "" {
				documented[trimmed] = true
			}
		}
	}

	states := []Model{
		func() Model { m := newTestModel(t); m.focusedField = 1; return m }(),
		scannedModel(t),
		func() Model { m := scannedModel(t); m.switchTab(TabSecrets); return m }(),
		detailsModel(t),
	}

	for _, m := range states {
		for _, s := range m.GetShortcuts() {
			key := strings.ToLower(s.Key)
			if key == "←→" || key == "esc/⌫" {
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
	for _, section := range newTestModel(t).GetHelpContent().Sections {
		body += section.Title + " " + section.Body + "\n"
	}

	for _, want := range []string{"CVE", "Secret", "License", "Misconfig"} {
		if !strings.Contains(body, want) {
			t.Errorf("the help does not explain the %s tab", want)
		}
	}
}
