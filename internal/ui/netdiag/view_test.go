package netdiag

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── View states ──────────────────────────────────────────────────────────────

func TestViewRendersTheForm(t *testing.T) {
	out := newTestModel(t).View()

	for _, want := range []string{"Target", "Port", "DNS Server", "Diagnostic Tests", "Run Diagnostics"} {
		if !strings.Contains(out, want) {
			t.Errorf("the form is missing %q", want)
		}
	}
	for _, test := range New(testConfig()).tests {
		if !strings.Contains(out, test.name) {
			t.Errorf("the form is missing the %q checkbox", test.name)
		}
	}
}

func TestViewRendersProgressWhileRunning(t *testing.T) {
	m := runningModel(t, "example.com", "Ping", "Netcat")

	out := m.View()
	if !strings.Contains(out, "Running diagnostics") {
		t.Error("the running view does not say what it is doing")
	}
	if !strings.Contains(out, "0 / 2 tests completed") {
		t.Errorf("the running view does not show progress:\n%s", out)
	}

	m = feed(t, m, testCompleteMsg{gen: m.runGen, name: "Ping", success: true, output: "ok"})
	if !strings.Contains(m.View(), "1 / 2 tests completed") {
		t.Error("the progress counter did not advance")
	}
}

func TestRunningViewMarksEachTestAsItLands(t *testing.T) {
	m := runningModel(t, "example.com", "Ping", "Netcat")
	m = feed(t, m,
		testCompleteMsg{gen: m.runGen, name: "Ping", success: true, output: "ok"},
	)

	// One finished, one still spinning — both named.
	out := m.View()
	if !strings.Contains(out, "Ping") || !strings.Contains(out, "Netcat") {
		t.Errorf("the running view does not list both tests:\n%s", out)
	}
}

func TestResultsTableShowsStatusAndFirstOutputLine(t *testing.T) {
	m := resultsModel(t)

	out := m.View()
	if !strings.Contains(out, "Ping") || !strings.Contains(out, "Netcat") {
		t.Error("the results table does not list both tests")
	}
	if !strings.Contains(out, "OK") || !strings.Contains(out, "FAIL") {
		t.Errorf("the results table does not distinguish success from failure:\n%s", out)
	}
	// The Output column carries the first non-empty line, not the whole log.
	if !strings.Contains(out, "64 bytes from example.com") {
		t.Error("the results table does not show the first output line")
	}
	if strings.Contains(out, "ttl=57") {
		t.Error("the results table shows more than the first output line")
	}
}

func TestCancelledTestsAreMarkedInTheTable(t *testing.T) {
	m := runningModel(t, "example.com", "Ping")
	m = feed(t, m, testutil.Key("esc"))

	if !strings.Contains(m.View(), "Ping") {
		t.Error("a cancelled test disappeared from the results")
	}
}

func TestDetailsViewRendersTheFullOutput(t *testing.T) {
	m := feed(t, resultsModel(t), testutil.Key("enter"))

	out := m.View()
	if !strings.Contains(out, "64 bytes from example.com") || !strings.Contains(out, "ttl=57") {
		t.Errorf("the details view does not show the full output:\n%s", out)
	}
}

func TestDetailsViewHandlesEmptyOutput(t *testing.T) {
	m := runningModel(t, "example.com", "Ping")
	m = feed(t, m, testCompleteMsg{gen: m.runGen, name: "Ping", success: false, output: ""})
	m = feed(t, m, testutil.Key("enter"))

	if !strings.Contains(m.View(), "(no output)") {
		t.Error("the details view is blank for a test that produced nothing")
	}
}

// ── firstOutputLine ──────────────────────────────────────────────────────────

func TestFirstOutputLine(t *testing.T) {
	tests := []struct {
		name   string
		output string
		maxLen int
		want   string
	}{
		{"first non-empty line", "\n\n  hello\nworld", 40, "hello"},
		{"trims", "   spaced   \n", 40, "spaced"},
		{"empty output", "", 40, ""},
		{"only blank lines", "\n  \n\t\n", 40, ""},
		{"truncates", strings.Repeat("a", 50), 10, strings.Repeat("a", 7) + "..."},
		{"exact length is kept", strings.Repeat("a", 10), 10, strings.Repeat("a", 10)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstOutputLine(tc.output, tc.maxLen); got != tc.want {
				t.Errorf("firstOutputLine(%q, %d) = %q, want %q", tc.output, tc.maxLen, got, tc.want)
			}
		})
	}
}

// The result lands in a table cell, so a cut mid-rune produces invalid UTF-8
// that bubbles then truncates again — the same defect as D1/D6 in the backlog.
func TestFirstOutputLineKeepsMultibyteRunesIntact(t *testing.T) {
	// Each "é" is two bytes: a byte-wise cut at 7 lands inside one.
	got := firstOutputLine(strings.Repeat("é", 20), 10)

	if !utf8.ValidString(got) {
		t.Errorf("firstOutputLine produced invalid UTF-8: %q", got)
	}
}

// ── Header, footer and help ──────────────────────────────────────────────────

func TestGetTitleNamesTheOpenTest(t *testing.T) {
	m := resultsModel(t)
	if strings.Contains(m.GetTitle(), "Ping") {
		t.Error("the results title already names a test")
	}

	m = feed(t, m, testutil.Key("enter"))
	if !strings.Contains(m.GetTitle(), "Ping") {
		t.Errorf("GetTitle() = %q in the details, want the test name", m.GetTitle())
	}
}

// Unlike the other views, this one does supply an icon separately from the
// title, because the app router renders it in the tab strip.
func TestGetIconIsPopulated(t *testing.T) {
	if newTestModel(t).GetIcon() == "" {
		t.Error("GetIcon() is empty")
	}
}

func TestGetHeaderInfoCarriesTheContext(t *testing.T) {
	info := newTestModel(t).GetHeaderInfo("work")

	if len(info) != 1 || info[0].Key != "Context" || info[0].Value != "work" {
		t.Errorf("GetHeaderInfo() = %+v, want the active context", info)
	}
}

// Rule 130: the shortcut set follows the state.
func TestShortcutsFollowTheState(t *testing.T) {
	form := newTestModel(t)
	if !hasShortcut(form.GetShortcuts(), "space") || !hasShortcut(form.GetShortcuts(), "tab") {
		t.Error("the form does not advertise toggle and tab switching")
	}

	running := runningModel(t, "example.com", "Ping").GetShortcuts()
	if len(running) != 1 || !hasShortcut(running, "esc") {
		t.Errorf("the running state advertises %v, want cancel only", running)
	}

	results := resultsModel(t).GetShortcuts()
	if !hasShortcut(results, "enter") || !hasShortcut(results, "r") {
		t.Error("the results state does not advertise details and restart")
	}
}

// The formatted/raw toggle only exists for outputs that have a formatter, so it
// must not be advertised for the others.
func TestFormattedToggleIsAdvertisedOnlyWhereItApplies(t *testing.T) {
	tests := []struct {
		name string
		test string
		want bool
	}{
		{"traceroute has a formatter", "Traceroute", true},
		{"dns has a formatter", "DNS Resolution", true},
		{"ping has none", "Ping", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := runningModel(t, "example.com", tc.test)
			m = feed(t, m, testCompleteMsg{gen: m.runGen, name: tc.test, success: true, output: "output"})
			m = feed(t, m, testutil.Key("enter"))

			if got := hasShortcut(m.GetShortcuts(), "f"); got != tc.want {
				t.Errorf("the f toggle advertised = %v for %q, want %v", got, tc.test, tc.want)
			}
		})
	}
}

// The label has to say what pressing it does, not what is on screen.
func TestFormattedToggleLabelFlips(t *testing.T) {
	m := runningModel(t, "example.com", "Traceroute")
	m = feed(t, m, testCompleteMsg{gen: m.runGen, name: "Traceroute", success: true, output: "1 gateway"})
	m = feed(t, m, testutil.Key("enter"))

	if got := shortcutDescription(m.GetShortcuts(), "f"); got != "Raw output" {
		t.Errorf("f label = %q while formatted, want \"Raw output\"", got)
	}

	m = feed(t, m, testutil.Key("f"))
	if got := shortcutDescription(m.GetShortcuts(), "f"); got != "Formatted output" {
		t.Errorf("f label = %q while raw, want \"Formatted output\"", got)
	}
}

func TestShortcutsFollowTheActiveTab(t *testing.T) {
	m := feed(t, newTestModel(t), testutil.Key("tab")) // ports

	ports := m.GetShortcuts()
	if !hasShortcut(ports, "ctrl+k") || !hasShortcut(ports, "/") {
		t.Error("the ports tab does not advertise its own shortcuts")
	}
	if hasShortcut(ports, "space") && shortcutDescription(ports, "space") == "Toggle checkbox" {
		t.Error("the ports tab still advertises the form's checkbox toggle")
	}

	m = feed(t, m, testutil.Key("tab")) // topology, still loading
	loading := m.GetShortcuts()
	if len(loading) != 1 || !hasShortcut(loading, "tab") {
		t.Errorf("the loading topology tab advertises %v, want tab switching only", loading)
	}

	m.topologyModel.state = topoStateReady
	if !hasShortcut(m.GetShortcuts(), "ctrl+r") {
		t.Error("the loaded topology tab does not advertise refresh")
	}
}

// Rule 137: descriptions are capitalised.
func TestShortcutDescriptionsAreCapitalised(t *testing.T) {
	states := []shortcut.Shortcuts{
		newTestModel(t).GetShortcuts(),
		runningModel(t, "example.com", "Ping").GetShortcuts(),
		resultsModel(t).GetShortcuts(),
		feed(t, resultsModel(t), testutil.Key("enter")).GetShortcuts(),
		feed(t, newTestModel(t), testutil.Key("tab")).GetShortcuts(),
	}

	for _, set := range states {
		for _, s := range set {
			if s.Description == "" {
				t.Errorf("shortcut %q has no description", s.Key)
				continue
			}
			if first := s.Description[0]; first < 'A' || first > 'Z' {
				t.Errorf("shortcut %q description %q does not start with a capital", s.Key, s.Description)
			}
		}
	}
}

func hasShortcut(shortcuts shortcut.Shortcuts, key string) bool {
	return shortcutDescription(shortcuts, key) != ""
}

func shortcutDescription(shortcuts shortcut.Shortcuts, key string) string {
	for _, s := range shortcuts {
		if s.Key == key {
			return s.Description
		}
	}
	return ""
}

// Rule 124: the promised footer height must match what is rendered, in every
// tab — the ports filter bar adds a line.
func TestFooterHeightMatchesWhatRenderFooterEmits(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T) *Model
	}{
		{"form", newTestModel},
		{"running", func(t *testing.T) *Model { return runningModel(t, "example.com", "Ping") }},
		{"results", resultsModel},
		{"footer error", func(t *testing.T) *Model {
			m := newTestModel(t)
			m.footerError = "Invalid target"
			return m
		}},
		{"footer info", func(t *testing.T) *Model {
			m := newTestModel(t)
			m.footerInfo = "Nothing to do"
			return m
		}},
		{"ports", func(t *testing.T) *Model { return feed(t, newTestModel(t), testutil.Key("tab")) }},
		{"ports searching", func(t *testing.T) *Model {
			return feed(t, newTestModel(t), testutil.Key("tab"), testutil.Key("/"))
		}},
		{"topology", func(t *testing.T) *Model {
			return feed(t, newTestModel(t), testutil.Key("tab"), testutil.Key("tab"))
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.build(t)

			want := m.GetFooterHeight()
			got := strings.Count(m.RenderFooter(120), "\n") + 1
			if got != want {
				t.Errorf("RenderFooter() emitted %d lines, GetFooterHeight() promised %d", got, want)
			}
		})
	}
}

func TestFooterShowsTheTabsAndTheActiveMessage(t *testing.T) {
	m := newTestModel(t)
	m.footerError = "Invalid target"

	footer := m.RenderFooter(120)
	for _, tab := range []string{"Diagnostics", "Ports", "Topology"} {
		if !strings.Contains(footer, tab) {
			t.Errorf("the footer is missing the %q tab", tab)
		}
	}
	if !strings.Contains(footer, "Invalid target") {
		t.Error("the footer does not surface the error message")
	}
}

// Each tab owns its own footer message; switching tabs must not carry one over.
func TestFooterMessageIsPerTab(t *testing.T) {
	m := newTestModel(t)
	m.footerError = "Invalid target"

	m = feed(t, m, testutil.Key("tab")) // ports

	if strings.Contains(m.RenderFooter(120), "Invalid target") {
		t.Error("the diagnostics error leaked into the ports tab footer")
	}
}

func TestGetHelpContentIsPopulated(t *testing.T) {
	content := newTestModel(t).GetHelpContent()

	if content.Title == "" || content.Description == "" {
		t.Error("the help content has no title or description")
	}
	if len(content.KeyBindings) == 0 || len(content.Sections) == 0 {
		t.Error("the help content has no key bindings or sections")
	}
}
