package netdiag

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Construction ─────────────────────────────────────────────────────────────

// TestTheFormAsksThreeQuestions is the shape of §netcheck: the seven checkboxes
// asked the user to select tools, which the pipeline derives.
func TestTheFormAsksThreeQuestions(t *testing.T) {
	m := New(testConfig())

	if m.state != StateInput {
		t.Errorf("state = %v, want StateInput", m.state)
	}
	if fieldCount != 4 {
		t.Errorf("the form has %d fields, want 4 — target, port, resolver, button", fieldCount)
	}
	if m.focusedField != fieldTarget {
		t.Errorf("focus starts on %d, want the target field", m.focusedField)
	}
}

// TestTheResolverDefaultsToTheSystemOne — the old default read the first
// nameserver out of /etc/resolv.conf, which does not exist on Windows, so the
// field was silently empty on the platform this is developed on. Empty now
// means the system resolver, and says so.
func TestTheResolverDefaultsToTheSystemOne(t *testing.T) {
	m := New(testConfig())

	if got := m.dnsServerInput.Value(); got != "" {
		t.Errorf("resolver field starts as %q, want empty", got)
	}
	if !strings.Contains(m.dnsServerInput.Placeholder, "system") {
		t.Errorf("placeholder %q does not say what empty means", m.dnsServerInput.Placeholder)
	}
}

func TestInitLoadsBothSubTabs(t *testing.T) {
	if New(testConfig()).Init() == nil {
		t.Fatal("Init returned no command")
	}
}

// ── Tabs ─────────────────────────────────────────────────────────────────────

func TestTabCyclesThroughTheThreeTabs(t *testing.T) {
	m := newTestModel(t)
	for _, want := range []int{tabPorts, tabInterfaces, tabDiagnostics} {
		m = feed(t, m, testutil.Key("tab"))
		if m.activeTab != want {
			t.Fatalf("activeTab = %d, want %d", m.activeTab, want)
		}
	}
	for _, want := range []int{tabInterfaces, tabPorts, tabDiagnostics} {
		m = feed(t, m, testutil.Key("shift+tab"))
		if m.activeTab != want {
			t.Fatalf("activeTab = %d, want %d", m.activeTab, want)
		}
	}
}

func TestTabSwitchesFromEveryState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model func(*testing.T) *Model
	}{
		{"form", newTestModel},
		{"running", func(t *testing.T) *Model { return runningModel(t, "example.com") }},
		{"results", resultsModel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := feed(t, tc.model(t), testutil.Key("tab"))
			if m.activeTab != tabPorts {
				t.Fatalf("activeTab = %d, want the ports tab", m.activeTab)
			}
		})
	}
}

// ── The form ─────────────────────────────────────────────────────────────────

func TestVerticalNavigationWrapsAcrossEveryField(t *testing.T) {
	m := newTestModel(t)
	for i := 1; i <= fieldCount; i++ {
		m = feed(t, m, testutil.Key("down"))
		want := i % fieldCount
		if m.focusedField != want {
			t.Fatalf("after %d downs focus = %d, want %d", i, m.focusedField, want)
		}
	}
	m = feed(t, m, testutil.Key("up"))
	if m.focusedField != fieldCount-1 {
		t.Fatalf("up from the first field = %d, want %d", m.focusedField, fieldCount-1)
	}
}

func TestFocusFollowsTheTextFields(t *testing.T) {
	m := newTestModel(t)
	if !m.targetInput.Focused() {
		t.Fatal("the target field does not start focused")
	}
	m = feed(t, m, testutil.Key("down"))
	if m.targetInput.Focused() || !m.portInput.Focused() {
		t.Fatal("focus did not move to the port field")
	}
	m = feed(t, m, testutil.Key("down"))
	if m.portInput.Focused() || !m.dnsServerInput.Focused() {
		t.Fatal("focus did not move to the resolver field")
	}
	m = feed(t, m, testutil.Key("down"))
	if m.dnsServerInput.Focused() {
		t.Fatal("the resolver field kept focus on the button")
	}
}

func TestTypingReachesTheFocusedInput(t *testing.T) {
	m := typeInto(t, newTestModel(t), "example.com")
	if got := m.targetInput.Value(); got != "example.com" {
		t.Fatalf("target = %q", got)
	}
}

// TestEnterRunsFromAnywhereInTheForm: with three fields and one button there is
// nothing else enter could mean, and walking to the button first buys nothing.
func TestEnterRunsFromAnywhereInTheForm(t *testing.T) {
	for _, field := range []int{fieldTarget, fieldPort, fieldDNSServer, fieldButton} {
		m := typeInto(t, newTestModel(t), "example.com")
		m.focusedField = field
		m = feed(t, m, testutil.Key("enter"))
		if m.state != StateRunning {
			t.Errorf("enter on field %d did not start the run", field)
		}
	}
}

func TestRunRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		port   string
	}{
		{"no target", "", ""},
		{"target with a shell metacharacter", "example.com; rm -rf /", ""},
		{"port out of range", "example.com", "70000"},
		{"port that is not a number", "example.com", "https"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := typeInto(t, newTestModel(t), tc.target)
			if tc.port != "" {
				m.focusedField = fieldPort
				m.portInput.Focus()
				m = feed(t, m, testutil.Type(tc.port)...)
			}
			m.focusedField = fieldButton
			m = feed(t, m, testutil.Key("enter"))

			if m.state == StateRunning {
				t.Fatal("the run started on input that should have been refused")
			}
			if !strings.Contains(m.View(), "Target") {
				t.Error("the form is no longer on screen")
			}
		})
	}
}

func TestAnEmptyPortFallsBackToTheDefault(t *testing.T) {
	m := typeInto(t, newTestModel(t), "example.com")
	tg, err := m.buildTarget()
	if err != nil {
		t.Fatalf("buildTarget: %v", err)
	}
	if tg.Port != 443 {
		t.Fatalf("port = %d, want the 443 default", tg.Port)
	}
}

// ── The pipeline ─────────────────────────────────────────────────────────────

// TestTheRunChainsItsStages pins that each stage schedules the next: the whole
// point of chaining through messages is that the footer can name the question
// being asked while it is being asked.
func TestTheRunChainsItsStages(t *testing.T) {
	m := runningModel(t, "example.com")
	steps := netcheck.Steps()

	for i := 0; i < len(steps)-1; i++ {
		next, cmd := step(t, m, stageDoneMsg{
			gen: m.runGen, stage: steps[i], next: i + 1, results: netcheck.ResultsOf(),
		})
		m = next
		if cmd == nil {
			t.Fatalf("stage %d landed without scheduling the next", i)
		}
		if m.state != StateRunning {
			t.Fatalf("state = %v after stage %d, want StateRunning", m.state, i)
		}
		if m.runStage != steps[i+1] {
			t.Fatalf("runStage = %q, want %q", m.runStage, steps[i+1])
		}
	}

	m = feed(t, m, stageDoneMsg{
		gen: m.runGen, stage: steps[len(steps)-1], next: len(steps), results: netcheck.ResultsOf(),
	})
	if m.state != StateResults {
		t.Fatalf("state = %v after the last stage, want StateResults", m.state)
	}
}

func TestTheHeadlineVerdictIsTheWorstOfTheChecks(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []netcheck.Check
		want netcheck.Verdict
	}{
		{"all clean", []netcheck.Check{
			check(netcheck.CheckResolve, netcheck.OK, "resolves"),
			check(netcheck.CheckTCP, netcheck.OK, "open"),
		}, netcheck.OK},
		{"a warning shows", []netcheck.Check{
			check(netcheck.CheckResolve, netcheck.OK, "resolves"),
			check(netcheck.CheckICMP, netcheck.Warn, "filtered"),
		}, netcheck.Warn},
		{"a failure wins", []netcheck.Check{
			check(netcheck.CheckICMP, netcheck.Warn, "filtered"),
			check(netcheck.CheckTCP, netcheck.Fail, "refused"),
		}, netcheck.Fail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := deliver(t, runningModel(t, "example.com"), tc.in...)
			if m.verdict != tc.want {
				t.Fatalf("verdict = %v, want %v", m.verdict, tc.want)
			}
		})
	}
}

func TestStaleResultsFromASupersededRunAreDiscarded(t *testing.T) {
	m := runningModel(t, "example.com")
	stale := m.runGen
	m = feed(t, m, testutil.Key("esc")) // cancels, bumping the generation

	m = feed(t, m, stageDoneMsg{
		gen: stale, stage: netcheck.StageResolve, next: 1,
		results: netcheck.ResultsOf(check(netcheck.CheckResolve, netcheck.OK, "resolves")),
	})
	if len(m.results.All()) != 0 {
		t.Fatalf("a superseded run's results landed: %v", m.results.All())
	}
}

// TestCancellingKeepsWhatCompleted — the stage in flight is a network read that
// times out on its own; what has already been answered is worth keeping.
func TestCancellingKeepsWhatCompleted(t *testing.T) {
	m := runningModel(t, "example.com")
	m = feed(t, m, stageDoneMsg{
		gen: m.runGen, stage: netcheck.StageResolve, next: 1,
		results: netcheck.ResultsOf(check(netcheck.CheckResolve, netcheck.OK, "resolves")),
	})
	m = feed(t, m, testutil.Key("esc"))

	if m.state != StateResults {
		t.Fatalf("state = %v after cancelling, want StateResults", m.state)
	}
	if len(m.results.All()) != 1 {
		t.Fatalf("cancelling threw away %d completed checks", 1-len(m.results.All()))
	}
}

func TestOtherKeysAreInertWhileRunning(t *testing.T) {
	m := runningModel(t, "example.com")
	for _, key := range []string{"enter", "p", "/", "H", "up", "down"} {
		m = feed(t, m, testutil.Key(key))
		if m.state != StateRunning {
			t.Fatalf("%q left StateRunning", key)
		}
	}
}

func TestSpinnerTicksOnlyWhileSomethingRuns(t *testing.T) {
	idle := newTestModel(t)
	if _, cmd := step(t, idle, spinner.TickMsg{}); cmd != nil {
		t.Error("the spinner ticks on the idle form")
	}
	running := runningModel(t, "example.com")
	if _, cmd := step(t, running, spinner.TickMsg{}); cmd == nil {
		t.Error("the spinner does not tick while the pipeline runs")
	}
}

// ── Results ──────────────────────────────────────────────────────────────────

func TestEnterOpensTheCheckUnderTheCursor(t *testing.T) {
	m := resultsModel(t)
	m.checksTable.SetCursor(1)
	m = feed(t, m, testutil.Key("enter"))

	if m.state != StateDetails {
		t.Fatalf("state = %v, want StateDetails", m.state)
	}
	if m.selected.ID != netcheck.CheckTCP {
		t.Fatalf("opened %q, want the row under the cursor", m.selected.ID)
	}
}

func TestEnterIsInertWithoutResults(t *testing.T) {
	m := runningModel(t, "example.com")
	m = feed(t, m, stageDoneMsg{
		gen: m.runGen, stage: netcheck.Steps()[len(netcheck.Steps())-1],
		next: len(netcheck.Steps()), results: netcheck.ResultsOf(),
	})
	if m = feed(t, m, testutil.Key("enter")); m.state == StateDetails {
		t.Fatal("enter opened a detail pane with no rows")
	}
}

// TestTheProblemsFilterDropsTheSettledRows — one token rather than four: ten
// rows do not need cumulative severity filters, they need the noise gone.
func TestTheProblemsFilterDropsTheSettledRows(t *testing.T) {
	m := resultsModel(t)
	if got := len(m.checksTable.Visible()); got != 3 {
		t.Fatalf("the table shows %d rows, want all 3", got)
	}

	m = feed(t, m, testutil.Key("p"))
	visible := m.checksTable.Visible()
	if len(visible) != 1 {
		t.Fatalf("problems only shows %d rows, want 1", len(visible))
	}
	if visible[0].ID != netcheck.CheckTCP {
		t.Fatalf("kept %q, want the failing check", visible[0].ID)
	}

	m = feed(t, m, testutil.Key("p"))
	if got := len(m.checksTable.Visible()); got != 3 {
		t.Fatalf("toggling back shows %d rows, want 3", got)
	}
}

func TestSearchNarrowsTheChecks(t *testing.T) {
	m := resultsModel(t)
	m = feed(t, m, testutil.Key("/"))
	if !m.filterBar.InEditMode() {
		t.Fatal("/ did not open the search")
	}
	// "TCP" would also match the blocked row, whose summary names what blocked
	// it — the search covers the observation as well as the title, on purpose.
	m = feed(t, m, testutil.Type("resolution")...)
	if got := len(m.checksTable.Visible()); got != 1 {
		t.Fatalf("searching resolution shows %d rows, want 1", got)
	}
}

// TestAQueryCanContainABoundLetter — the filter bar takes every key while it is
// in edit mode, or "p" would toggle the filter instead of being typed.
func TestAQueryCanContainABoundLetter(t *testing.T) {
	m := resultsModel(t)
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("p")...)

	if m.filterBar.IsTokenActive(problemsToken) {
		t.Fatal("typing p into the search toggled the problems filter")
	}
	if got := m.filterBar.SearchQuery(); got != "p" {
		t.Fatalf("query = %q, want %q", got, "p")
	}
}

func TestEscReturnsToTheFormAndClearsTheFilters(t *testing.T) {
	m := resultsModel(t)
	m = feed(t, m, testutil.Key("p"))
	m = feed(t, m, testutil.Key("esc"))

	if m.state != StateInput {
		t.Fatalf("state = %v, want StateInput", m.state)
	}
	if m.filterBar.IsTokenActive(problemsToken) {
		t.Error("the problems filter survived the return to the form")
	}
	if len(m.results.All()) != 0 {
		t.Error("the previous run's checks survived the return to the form")
	}
}

func TestCtrlRRunsAgain(t *testing.T) {
	m := resultsModel(t)
	gen := m.runGen
	m = feed(t, m, testutil.Key("ctrl+r"))

	if m.state != StateRunning {
		t.Fatalf("state = %v, want StateRunning", m.state)
	}
	if m.runGen == gen {
		t.Error("the generation did not advance, so the previous run's results would land")
	}
}

// ── Details ──────────────────────────────────────────────────────────────────

func TestTheDetailPaneCarriesTheExplanation(t *testing.T) {
	m := resultsModel(t)
	m.checksTable.SetCursor(1)
	m = feed(t, m, testutil.Key("enter"))

	out := m.View()
	for _, want := range []string{"Observed", "What it means", "What to do"} {
		if !strings.Contains(out, want) {
			t.Errorf("the detail pane has no %q section", want)
		}
	}
}

// TestABlockedCheckPointsAtWhatBlockedIt — the cascade's whole payoff is that
// one row tells you where to go.
func TestABlockedCheckPointsAtWhatBlockedIt(t *testing.T) {
	m := resultsModel(t)
	m.checksTable.SetCursor(2)
	m = feed(t, m, testutil.Key("enter"))

	if !strings.Contains(m.View(), "TCP connect") {
		t.Error("the blocked check does not name what blocked it")
	}
}

func TestEscLeavesTheDetails(t *testing.T) {
	m := resultsModel(t)
	m = feed(t, m, testutil.Key("enter"))
	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateResults {
		t.Fatalf("state = %v, want StateResults", m.state)
	}
}

// ── Router contracts ─────────────────────────────────────────────────────────

func TestInEditModeIsTrueOnlyWhereAFieldHasTheKeyboard(t *testing.T) {
	form := newTestModel(t)
	if !form.InEditMode() {
		t.Error("the target field does not claim the keyboard")
	}
	form.focusedField = fieldButton
	if form.InEditMode() {
		t.Error("the button claims the keyboard")
	}
	if runningModel(t, "example.com").InEditMode() {
		t.Error("a running pipeline claims the keyboard")
	}
	if resultsModel(t).InEditMode() {
		t.Error("the results table claims the keyboard")
	}
	searching := feed(t, resultsModel(t), testutil.Key("/"))
	if !searching.InEditMode() {
		t.Error("an open search does not claim the keyboard")
	}
}

func TestTheInterfacesTabDoesNotBlockCommandModeAtRest(t *testing.T) {
	m := newTestModel(t)
	m.activeTab = tabInterfaces
	if m.InEditMode() {
		t.Fatal("the interfaces tab claims the keyboard with no search open")
	}
}

func TestClearFooterMessageEmptiesBoth(t *testing.T) {
	m := newTestModel(t)
	m, _ = step(t, m, testutil.Key("enter")) // refused: no target
	m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
	if strings.Contains(m.RenderFooter(120), "Target is required") {
		t.Error("the footer message outlived its expiry")
	}
}

func TestResizeIsForwardedToEverySubModel(t *testing.T) {
	m := feed(t, New(testConfig()), tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.width != 100 || m.height != 30 {
		t.Fatalf("model kept %dx%d", m.width, m.height)
	}
	if m.portsModel.width != 100 || m.interfacesModel.width != 100 {
		t.Error("a sub-model did not receive the resize")
	}
}

func TestNarrowTerminalDoesNotCollapseTheLayout(t *testing.T) {
	m := deliver(t, runningModel(t, "example.com"),
		check(netcheck.CheckTCP, netcheck.Fail, "Port 443 does not accept connections"))
	// The resize comes last: runningModel lays out at 120 columns, so sizing
	// before it would be overwritten and the test would not be narrow at all.
	m = feed(t, m, tea.WindowSizeMsg{Width: 46, Height: 20})
	if out := m.View(); strings.TrimSpace(out) == "" {
		t.Fatal("the view is empty at 46 columns")
	}
}
