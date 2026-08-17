package netdiag

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/keymap"
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
		want   string
	}{
		{"first non-empty line", "\n\n  hello\nworld", "hello"},
		{"trims", "   spaced   \n", "spaced"},
		{"empty output", "", ""},
		{"only blank lines", "\n  \n\t\n", ""},
		{"long lines are left whole for the renderer", strings.Repeat("a", 50), strings.Repeat("a", 50)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstOutputLine(tc.output); got != tc.want {
				t.Errorf("firstOutputLine(%q) = %q, want %q", tc.output, got, tc.want)
			}
		})
	}
}

// The output cell is cut to the column by the table's renderer now rather than
// by firstOutputLine, and a cut mid-rune would produce invalid UTF-8 — the same
// defect as D1/D6 in the backlog, one layer down.
func TestTheOutputColumnIsCutOnRuneBoundaries(t *testing.T) {
	// Each "é" is two bytes: a byte-wise cut lands inside one.
	m := runningModel(t, "example.com", "Ping")
	m = feed(t, m, testCompleteMsg{gen: m.runGen, name: "Ping", success: true, output: strings.Repeat("é", 200)})

	view := m.resultsTable.View()
	if !utf8.ValidString(view) {
		t.Errorf("the results table rendered invalid UTF-8: %q", view)
	}
	if !strings.Contains(view, "é") {
		t.Error("the output column shows none of the output")
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
	// esc goes back to the form; ctrl+r means refresh and only refresh (§3.26).
	if !hasShortcut(results, "enter") || !hasShortcut(results, "esc") {
		t.Error("the results state does not advertise details and going back")
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
	if !hasShortcut(ports, keymap.Kill) || !hasShortcut(ports, "/") {
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

// ── The results table after §3.21 ────────────────────────────────────────────

// The results table used to be rebuilt with table.New on every update, which
// dropped the cursor back to the top. A late result — the metadata for a test
// that finished after the others, a cancellation — moved the row under the
// user's cursor while they were reading it.
func TestALateResultKeepsTheCursorWhereItWas(t *testing.T) {
	m := resultsModel(t)
	m = feed(t, m, testutil.Key("down"))

	if got := m.resultsTable.Cursor(); got != 1 {
		t.Fatalf("cursor = %d before the refresh, want 1", got)
	}
	m.rebuildResultsTable()

	if got := m.resultsTable.Cursor(); got != 1 {
		t.Errorf("cursor = %d after a refresh, want it left where the user put it", got)
	}
}

// Rule 116, now the solver's job rather than three fixed widths and a
// subtraction: the columns must sum to exactly the space they have at every
// width, or the selected row stops short of the right border.
func TestResultColumnsHoldTheWidthInvariant(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120, 200} {
		m := feed(t, resultsModel(t), tea.WindowSizeMsg{Width: width, Height: 40})

		total := 0
		cols := m.resultsTable.Table().Columns()
		for i, col := range cols {
			total += col.Width
			if col.Width < 0 {
				t.Errorf("at width %d column %d is %d cells wide", width, i, col.Width)
			}
		}
		if want := width - 2 - len(cols)*2; total != want {
			t.Errorf("at width %d the columns sum to %d, want %d", width, total, want)
		}
	}
}

// Enter opens the row under the cursor, resolved through the table rather than
// by indexing resultOrder. The two orderings agree today only because this
// table does not sort — which is exactly the dependency nothing signalled, and
// what would have made adding one a silent defect.
func TestEnterOpensTheRowUnderTheCursor(t *testing.T) {
	m := resultsModel(t)
	m = feed(t, m, testutil.Key("down"), testutil.Key("enter"))

	row, ok := m.resultsTable.Selected()
	if !ok {
		t.Fatal("no row is selected")
	}
	if m.selectedTest != row.name {
		t.Errorf("opened %q, want the selected row %q", m.selectedTest, row.name)
	}
}
