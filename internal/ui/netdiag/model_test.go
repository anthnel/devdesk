package netdiag

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Construction ─────────────────────────────────────────────────────────────

func TestNewStartsOnTheFormWithEveryTestEnabled(t *testing.T) {
	m := newTestModel(t)

	if m.state != StateInput {
		t.Errorf("state = %d on a new model, want StateInput", m.state)
	}
	if m.activeTab != tabDiagnostics {
		t.Errorf("activeTab = %d on a new model, want the diagnostics tab", m.activeTab)
	}
	if m.focusedField != fieldTarget {
		t.Errorf("focusedField = %d on a new model, want the target field", m.focusedField)
	}
	if got := len(m.enabledTests()); got != 7 {
		t.Errorf("%d tests are enabled on a new model, want all 7", got)
	}
}

func TestInitLoadsBothSubTabs(t *testing.T) {
	if cmd := New(testConfig()).Init(); cmd == nil {
		t.Fatal("Init() returned no command, so the ports and topology tabs never load")
	}
}

// ── Tab switching (Rule 135) ─────────────────────────────────────────────────

func TestTabCyclesThroughTheThreeTabs(t *testing.T) {
	m := newTestModel(t)

	for _, want := range []int{tabPorts, tabTopology, tabDiagnostics} {
		m = feed(t, m, testutil.Key("tab"))
		if m.activeTab != want {
			t.Fatalf("activeTab = %d after tab, want %d", m.activeTab, want)
		}
	}

	m = feed(t, m, testutil.Key("shift+tab"))
	if m.activeTab != tabTopology {
		t.Errorf("activeTab = %d after shift+tab, want the topology tab (wrapped)", m.activeTab)
	}
}

// Tab must switch tabs even mid-run, and must not be swallowed by a text input.
func TestTabSwitchesFromEveryState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(t *testing.T) *Model
	}{
		{"form", newTestModel},
		{"running", func(t *testing.T) *Model { return runningModel(t, "example.com", "Ping") }},
		{"results", resultsModel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := feed(t, tc.build(t), testutil.Key("tab"))

			if m.activeTab != tabPorts {
				t.Errorf("activeTab = %d after tab, want the ports tab", m.activeTab)
			}
		})
	}
}

// ── Form navigation ──────────────────────────────────────────────────────────

func TestVerticalNavigationWrapsAcrossEveryField(t *testing.T) {
	m := newTestModel(t)

	for i := 1; i < fieldCount; i++ {
		m = feed(t, m, testutil.Key("down"))
		if m.focusedField != i {
			t.Fatalf("focusedField = %d after %d downs, want %d", m.focusedField, i, i)
		}
	}

	m = feed(t, m, testutil.Key("down"))
	if m.focusedField != fieldTarget {
		t.Errorf("focusedField = %d after wrapping past the button, want the target field", m.focusedField)
	}

	m = feed(t, m, testutil.Key("up"))
	if m.focusedField != fieldButton {
		t.Errorf("focusedField = %d after up from the first field, want the button", m.focusedField)
	}
}

func TestFocusFollowsTheTextFields(t *testing.T) {
	m := newTestModel(t)
	if !m.targetInput.Focused() {
		t.Fatal("the target input is not focused on a new form")
	}

	m = feed(t, m, testutil.Key("down"))
	if !m.portInput.Focused() || m.targetInput.Focused() {
		t.Error("focus did not move from the target to the port input")
	}

	m = feed(t, m, testutil.Key("down"))
	if !m.dnsServerInput.Focused() || m.portInput.Focused() {
		t.Error("focus did not move to the DNS server input")
	}

	// The checkboxes and the button hold no text input.
	m = feed(t, m, testutil.Key("down"))
	if m.targetInput.Focused() || m.portInput.Focused() || m.dnsServerInput.Focused() {
		t.Error("an input kept focus on the first checkbox")
	}
}

func TestTypingReachesTheFocusedInput(t *testing.T) {
	m := typeInto(t, newTestModel(t), "example.com")
	if got := m.targetInput.Value(); got != "example.com" {
		t.Errorf("target input = %q, want the typed value", got)
	}

	m = feed(t, m, testutil.Key("down"))
	m = feed(t, m, testutil.Type("8443")...)
	if got := m.portInput.Value(); got != "8443" {
		t.Errorf("port input = %q, want the typed value", got)
	}
	if m.targetInput.Value() != "example.com" {
		t.Error("typing into the port field also changed the target")
	}
}

// Rule 135: space is the only key that toggles a checkbox, and enter must not.
func TestSpaceTogglesTheFocusedCheckbox(t *testing.T) {
	m := newTestModel(t)
	m.focusedField = fieldPing

	m = feed(t, m, testutil.Key(" "))
	if enabledByName(m)["Ping"] {
		t.Error("space did not uncheck the focused test")
	}

	m = feed(t, m, testutil.Key(" "))
	if !enabledByName(m)["Ping"] {
		t.Error("space did not re-check the focused test")
	}
}

func TestEnterDoesNotToggleCheckboxes(t *testing.T) {
	m := newTestModel(t)
	m.focusedField = fieldPing

	m, cmd := step(t, m, testutil.Key("enter"))

	if !enabledByName(m)["Ping"] {
		t.Error("enter toggled a checkbox; Rule 135 reserves that for space")
	}
	if cmd != nil {
		t.Error("enter on a checkbox started a run")
	}
}

func TestSpaceOnATextFieldDoesNotToggleAnything(t *testing.T) {
	m := newTestModel(t)
	m.focusedField = fieldTarget
	before := enabledByName(m)

	m = feed(t, m, testutil.Key(" "))

	for name, was := range before {
		if enabledByName(m)[name] != was {
			t.Errorf("space on the target field toggled %q", name)
		}
	}
}

func enabledByName(m *Model) map[string]bool {
	out := map[string]bool{}
	for _, test := range m.tests {
		out[test.name] = test.enabled
	}
	return out
}

// ── Starting a run ───────────────────────────────────────────────────────────

func TestRunRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		port    string
		disable bool
		wantMsg string
	}{
		{"no target", "", "", false, "arget"},
		{"invalid target", "-bad.example.com", "", false, "arget"},
		{"invalid port", "example.com", "notaport", false, "ort"},
		{"port out of range", "example.com", "70000", false, "ort"},
		{"no test selected", "example.com", "", true, "at least one test"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			if tc.disable {
				m = withOnly(t, m)
			}
			m = typeInto(t, m, tc.target)
			m.portInput.SetValue(tc.port)
			m.focusedField = fieldButton

			m, cmd := step(t, m, testutil.Key("enter"))

			if m.state != StateInput {
				t.Errorf("state = %d after a rejected run, want to stay on the form", m.state)
			}
			if !strings.Contains(m.footerError, tc.wantMsg) {
				t.Errorf("footerError = %q, want it to mention %q", m.footerError, tc.wantMsg)
			}
			// Rule 128: a footer message must come with the timer that clears it.
			if cmd == nil {
				t.Error("no command returned, so the footer message would never clear")
			}
		})
	}
}

// Rule 128: footer messages are cleared by their own message, not left behind.
func TestClearFooterMessageEmptiesBoth(t *testing.T) {
	m := newTestModel(t)
	m.footerError = "something"
	m.footerInfo = "something else"

	m = feed(t, m, clearFooterMsg{})

	if m.footerError != "" || m.footerInfo != "" {
		t.Errorf("footer still holds %q / %q after the clear message", m.footerError, m.footerInfo)
	}
}

func TestRunStartsTheEnabledTestsOnly(t *testing.T) {
	m := runningModel(t, "example.com", "Ping", "SSL Certificate")

	if m.state != StateRunning {
		t.Fatalf("state = %d after a valid run, want StateRunning", m.state)
	}
	if m.totalTests != 2 {
		t.Errorf("totalTests = %d, want 2", m.totalTests)
	}
	if len(m.resultOrder) != 2 {
		t.Errorf("resultOrder = %v, want the two enabled tests", m.resultOrder)
	}
	if m.doneTests != 0 {
		t.Errorf("doneTests = %d at the start of a run, want 0", m.doneTests)
	}
}

// An empty port falls back to the default rather than failing validation.
func TestRunFallsBackToTheDefaultPort(t *testing.T) {
	m := newTestModel(t)
	m = typeInto(t, m, "example.com")
	m.portInput.SetValue("")
	m.focusedField = fieldButton

	m, _ = step(t, m, testutil.Key("enter"))

	if m.state != StateRunning {
		t.Errorf("state = %d with an empty port, want the run to start on the default", m.state)
	}
}

// An IP target turns the forward lookup into a reverse one, and the row is
// relabelled so the results table does not claim otherwise.
func TestDNSTestBecomesReverseDNSForAnIPTarget(t *testing.T) {
	m := runningModel(t, "1.1.1.1", "DNS Resolution")

	if len(m.resultOrder) != 1 || m.resultOrder[0] != "Reverse DNS" {
		t.Errorf("resultOrder = %v, want it relabelled to Reverse DNS", m.resultOrder)
	}
	if _, stale := m.results["DNS Resolution"]; stale {
		t.Error("the forward-lookup entry survived alongside the reverse one")
	}
}

func TestDNSTestStaysForwardForAHostname(t *testing.T) {
	m := runningModel(t, "example.com", "DNS Resolution")

	if len(m.resultOrder) != 1 || m.resultOrder[0] != "DNS Resolution" {
		t.Errorf("resultOrder = %v, want the forward lookup", m.resultOrder)
	}
}

// ── Results ──────────────────────────────────────────────────────────────────

func TestRunCompletesWhenEveryTestReports(t *testing.T) {
	m := runningModel(t, "example.com", "Ping", "Netcat")

	m = feed(t, m, testCompleteMsg{gen: m.runGen, name: "Ping", success: true, output: "ok"})
	if m.state != StateRunning {
		t.Error("the view left the running state before every test reported")
	}
	if m.doneTests != 1 {
		t.Errorf("doneTests = %d after one result, want 1", m.doneTests)
	}

	m = feed(t, m, testCompleteMsg{gen: m.runGen, name: "Netcat", success: false, output: "refused"})
	if m.state != StateResults {
		t.Errorf("state = %d once every test reported, want StateResults", m.state)
	}
}

// Cancelling bumps the generation so results from the abandoned run cannot
// resurrect it.
func TestStaleResultsFromACancelledRunAreDiscarded(t *testing.T) {
	m := runningModel(t, "example.com", "Ping", "Netcat")
	staleGen := m.runGen

	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateResults {
		t.Fatalf("state = %d after esc, want the run cancelled into StateResults", m.state)
	}
	doneAfterCancel := m.doneTests

	m = feed(t, m, testCompleteMsg{gen: staleGen, name: "Ping", success: true, output: "late"})

	if m.doneTests != doneAfterCancel {
		t.Errorf("doneTests = %d after a stale result, want it unchanged at %d", m.doneTests, doneAfterCancel)
	}
	if res := m.results["Ping"]; !res.cancelled {
		t.Error("a stale result overwrote the cancelled marker")
	}
}

func TestCancellingMarksOutstandingTestsOnly(t *testing.T) {
	m := runningModel(t, "example.com", "Ping", "Netcat")
	m = feed(t, m, testCompleteMsg{gen: m.runGen, name: "Ping", success: true, output: "ok"})

	m = feed(t, m, testutil.Key("esc"))

	if m.results["Ping"].cancelled {
		t.Error("a test that had already reported was marked cancelled")
	}
	if !m.results["Netcat"].cancelled {
		t.Error("the outstanding test was not marked cancelled")
	}
	if m.doneTests != m.totalTests {
		t.Errorf("doneTests = %d, want it to reach totalTests = %d after cancelling", m.doneTests, m.totalTests)
	}
}

// Only esc cancels; other keys must not disturb a run in flight.
func TestOtherKeysAreInertWhileRunning(t *testing.T) {
	m := runningModel(t, "example.com", "Ping")

	m = feed(t, m, testutil.Key("enter"), testutil.Key("q"), testutil.Key("down"))

	if m.state != StateRunning {
		t.Errorf("state = %d, want the run to continue", m.state)
	}
}

func TestSpinnerTicksOnlyWhileRunning(t *testing.T) {
	m := newTestModel(t)

	_, cmd := step(t, m, spinner.TickMsg{})
	if cmd != nil {
		t.Error("the spinner ran while the form was showing")
	}

	running := runningModel(t, "example.com", "Ping")
	_, cmd = step(t, running, spinner.TickMsg{})
	if cmd == nil {
		t.Error("the spinner stopped while tests were running")
	}
}

func TestResultsNavigationAndReset(t *testing.T) {
	m := resultsModel(t)

	m = feed(t, m, testutil.Key("down"))
	if m.resultsTable.Cursor() != 1 {
		t.Errorf("cursor = %d after down, want 1", m.resultsTable.Cursor())
	}
	m = feed(t, m, testutil.Key("home"))
	if m.resultsTable.Cursor() != 0 {
		t.Errorf("cursor = %d after g, want the top", m.resultsTable.Cursor())
	}
	m = feed(t, m, testutil.Key("end"))
	if m.resultsTable.Cursor() != 1 {
		t.Errorf("cursor = %d after G, want the last row", m.resultsTable.Cursor())
	}

	m = feed(t, m, testutil.Key("r"))
	if m.state != StateInput {
		t.Errorf("state = %d after r, want a fresh form", m.state)
	}
	if len(m.results) != 0 || m.totalTests != 0 || m.resultOrder != nil {
		t.Error("resetting to the form kept the previous run's results")
	}
	if !m.targetInput.Focused() {
		t.Error("the target input is not focused after resetting")
	}
}

// ── Details ──────────────────────────────────────────────────────────────────

func TestEnterOpensTheDetailsForTheSelectedTest(t *testing.T) {
	m := resultsModel(t)

	m = feed(t, m, testutil.Key("enter"))

	if m.state != StateDetails {
		t.Fatalf("state = %d after enter, want StateDetails", m.state)
	}
	if m.selectedTest != "Ping" {
		t.Errorf("selectedTest = %q, want the row under the cursor", m.selectedTest)
	}
	if m.rawDetails {
		t.Error("the details opened in raw mode; formatted is the default")
	}
}

func TestEnterIsInertWithoutResults(t *testing.T) {
	m := newTestModel(t)
	m.state = StateResults

	m = feed(t, m, testutil.Key("enter"))

	if m.state != StateResults {
		t.Errorf("state = %d after enter on an empty results table, want it unchanged", m.state)
	}
}

func TestDetailsToggleAndScroll(t *testing.T) {
	m := feed(t, resultsModel(t), testutil.Key("enter"))

	m = feed(t, m, testutil.Key("f"))
	if !m.rawDetails {
		t.Error("f did not switch to raw output")
	}
	m = feed(t, m, testutil.Key("f"))
	if m.rawDetails {
		t.Error("f did not switch back to formatted output")
	}

	m = feed(t, m, testutil.Key("down"))
	m = feed(t, m, testutil.Key("home"))
	if m.detailsViewport.YOffset != 0 {
		t.Errorf("YOffset = %d after g, want the top", m.detailsViewport.YOffset)
	}

	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateResults {
		t.Errorf("state = %d after esc, want to be back on the results", m.state)
	}
}

// ── Edit mode ────────────────────────────────────────────────────────────────

// InEditMode keeps the app router from stealing the character keys a focused
// field needs — a target can contain ':'. It says nothing about esc, which the
// router forwards whatever the view answers (D15), so a state that holds no
// field claims no key even when it uses esc.
func TestInEditModeIsTrueOnlyWhereAFieldHasTheKeyboard(t *testing.T) {
	m := newTestModel(t)

	m.focusedField = fieldTarget
	if !m.InEditMode() {
		t.Error("InEditMode() is false on the target field")
	}
	m.focusedField = fieldPing
	if m.InEditMode() {
		t.Error("InEditMode() is true on a checkbox, which needs no character keys")
	}

	if runningModel(t, "example.com", "Ping").InEditMode() {
		t.Error("InEditMode() is true while running, which costs the run ':' and '?'")
	}
	if feed(t, resultsModel(t), testutil.Key("enter")).InEditMode() {
		t.Error("InEditMode() is true in the details, which takes no text")
	}
	if resultsModel(t).InEditMode() {
		t.Error("InEditMode() is true on the results table, which takes no text")
	}
}

// Esc still closes the details and cancels a run — it arrives from the router
// now rather than through InEditMode, and that has to keep working.
func TestEscStillLeavesTheDetailsAndCancelsARun(t *testing.T) {
	details := feed(t, resultsModel(t), testutil.Key("enter"))
	if got := feed(t, details, testutil.Key("esc")).state; got != StateResults {
		t.Errorf("esc left the details in state %v, want the results", got)
	}

	running := runningModel(t, "example.com", "Ping")
	if got := feed(t, running, testutil.Key("esc")).state; got != StateResults {
		t.Errorf("esc left the run in state %v, want the results", got)
	}
}

// The topology tab takes no text, so a bare ":" reaches the router there
// whatever the topology is doing. The view used to carry an AllowCommandMode()
// that claimed to unlock ":" once the data had arrived; it could never fire,
// because InEditMode() is already false on this tab and the router only
// consulted it when InEditMode() was true. It is gone.
func TestTheTopologyTabNeverBlocksCommandMode(t *testing.T) {
	m := feed(t, newTestModel(t), testutil.Key("tab"), testutil.Key("tab")) // to topology

	if m.InEditMode() {
		t.Error("InEditMode() is true while the topology data is still loading")
	}

	m.topologyModel.state = topoStateReady
	if m.InEditMode() {
		t.Error("InEditMode() is true on the topology tab, which takes no text")
	}
}

// ── Layout ───────────────────────────────────────────────────────────────────

func TestResizeIsForwardedToEverySubModel(t *testing.T) {
	m := feed(t, newTestModel(t), tea.WindowSizeMsg{Width: 200, Height: 60})

	if m.width != 200 || m.height != 60 {
		t.Errorf("window size = %dx%d, want 200x60", m.width, m.height)
	}
	if m.targetInput.Width <= 0 {
		t.Error("the target input was not resized")
	}
}

func TestNarrowTerminalDoesNotCollapseTheLayout(t *testing.T) {
	m := feed(t, resultsModel(t), tea.WindowSizeMsg{Width: 10, Height: 4})

	if m.targetInput.Width < 20 {
		t.Errorf("target input width = %d on a narrow terminal, want the floor of 20", m.targetInput.Width)
	}
	if h := m.resultsTable.Table().Height(); h < 0 {
		t.Errorf("results table height = %d, want it non-negative", h)
	}
	if m.View() == "" {
		t.Error("View() returned nothing on a narrow terminal")
	}
}
