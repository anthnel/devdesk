package netdiag

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/forward"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// onForwardTab returns a laid-out model sitting on the Forward tab.
func onForwardTab(t *testing.T) *Model {
	t.Helper()
	m := newTestModel(t)
	m.activeTab = tabForward
	return m
}

// sampleForwards is what the router's broadcast carries.
func sampleForwards() []forward.Forward {
	return []forward.Forward{
		{ID: "1", LocalPort: 8080, Target: "10.0.0.5:80", Opened: time.Now(), Active: 2, Total: 9},
		{ID: "2", LocalPort: 9090, Target: "db.internal:5432", Opened: time.Now()},
	}
}

// ── The snapshot ─────────────────────────────────────────────────────────────

// The tab holds no registry: everything it shows arrives on the router's
// broadcast, and it reaches the view wherever it stands.
func TestTheTabFillsFromTheRoutersBroadcast(t *testing.T) {
	m := onForwardTab(t)
	m = feed(t, m, forward.ChangedMsg{Forwards: sampleForwards()})

	if got := len(m.forwardModel.table.Items()); got != 2 {
		t.Fatalf("the table holds %d rows, want 2", got)
	}
	if got := m.forwardModel.summaryLine(); got != "2" {
		t.Errorf("summaryLine = %q, want %q", got, "2")
	}
}

func TestABroadcastReachesTheTabFromAnotherOne(t *testing.T) {
	m := newTestModel(t) // sitting on Diagnostics
	m = feed(t, m, forward.ChangedMsg{Forwards: sampleForwards()})

	if got := len(m.forwardModel.table.Items()); got != 2 {
		t.Errorf("the tab holds %d rows while off screen, want 2", got)
	}
}

// The chain exists to be refreshed, and to stop when there is nothing left to
// refresh: a tab asking the router to broadcast every two seconds forever, for
// a table that cannot change on its own, is the cost this guards against.
func TestTheSnapshotChainRunsOnlyWhileAForwardIsOpen(t *testing.T) {
	m := onForwardTab(t)

	m = feed(t, m, forward.ChangedMsg{Forwards: nil})
	if m.forwardModel.ticking {
		t.Error("a chain started with no forward open")
	}

	m = feed(t, m, forward.ChangedMsg{Forwards: sampleForwards()})
	if !m.forwardModel.ticking {
		t.Fatal("no chain started once a forward was open")
	}

	// A second snapshot must not start a second chain.
	before := m.forwardModel.ticking
	m = feed(t, m, forward.ChangedMsg{Forwards: sampleForwards()})
	if !before || !m.forwardModel.ticking {
		t.Error("the chain did not survive a second snapshot")
	}
	if cmd := m.forwardModel.ensureTick(); cmd != nil {
		t.Error("ensureTick started a second chain while one was alive")
	}

	// Emptied, the tick lets itself die.
	m = feed(t, m, forward.ChangedMsg{Forwards: nil})
	m = feed(t, m, forwardTickMsg{})
	if m.forwardModel.ticking {
		t.Error("the chain outlived the last forward")
	}
}

// ── The form ─────────────────────────────────────────────────────────────────

func TestNOpensTheFormAndEscapeClosesIt(t *testing.T) {
	m := onForwardTab(t)

	m = feed(t, m, testutil.Key(keymap.New))
	if m.forwardModel.form == nil {
		t.Fatal("N did not open the form")
	}
	if !m.InEditMode() {
		t.Error("the open form does not hold the keyboard — a ':' would open the command line")
	}

	// esc produces a cancel message rather than clearing the form in place —
	// the shape every form in the application uses — so the message has to be
	// delivered for the close to happen.
	m, cmd := step(t, m, testutil.Key("esc"))
	m = feed(t, m, testutil.Msgs(cmd)...)
	if m.forwardModel.form != nil {
		t.Error("esc did not close the form")
	}
}

// A confirmed form asks the router, and asks for exactly what was typed. The
// view never opens a listener itself.
func TestTheFormAsksTheRouterForWhatWasTyped(t *testing.T) {
	m := onForwardTab(t)
	m = feed(t, m, testutil.Key(keymap.New))

	m.forwardModel.form.portInput.SetValue("8080")
	m.forwardModel.form.targetInput.SetValue("10.0.0.5:80")

	// enter produces the submit; the submit is what makes the model ask the
	// router. Both hops are walked, because the second is where the view could
	// still have opened a listener itself.
	_, cmd := step(t, m, testutil.Key("enter"))
	var submitted *ForwardFormSubmitMsg
	for _, msg := range testutil.Msgs(cmd) {
		if submit, ok := msg.(ForwardFormSubmitMsg); ok {
			submitted = &submit
		}
	}
	if submitted == nil {
		t.Fatal("enter produced no submit")
	}

	var asked *forward.OpenMsg
	m, cmd = step(t, m, *submitted)
	for _, msg := range testutil.Msgs(cmd) {
		if open, ok := msg.(forward.OpenMsg); ok {
			asked = &open
		}
	}
	if asked == nil {
		t.Fatal("confirming the form asked the router for nothing")
	}
	if asked.LocalPort != 8080 || asked.Target != "10.0.0.5:80" {
		t.Errorf("the router was asked for %+v, want port 8080 to 10.0.0.5:80", *asked)
	}
	if m.forwardModel.form != nil {
		t.Error("the form stayed open after being confirmed")
	}
}

// The port is the one thing the form itself has to decide, because the message
// carries an int. Everything else is internal/forward's to refuse, and a
// keypress that does nothing without saying so is what Rule 130 forbids.
func TestAPortThatIsNotANumberIsRefusedAtTheFooter(t *testing.T) {
	m := onForwardTab(t)
	m = feed(t, m, testutil.Key(keymap.New))
	m.forwardModel.form.portInput.SetValue("eighty")

	_, cmd := step(t, m, testutil.Key("enter"))
	if cmd == nil {
		t.Fatal("the keypress was swallowed — nothing reached the footer")
	}
	if m.forwardModel.form == nil {
		t.Error("a refused form was closed anyway")
	}
	for _, msg := range testutil.Msgs(cmd) {
		if _, ok := msg.(ForwardFormSubmitMsg); ok {
			t.Fatal("a port that is not a number was sent to the router")
		}
	}
}

// ── Stopping one ─────────────────────────────────────────────────────────────

func TestKAsksBeforeStoppingAForward(t *testing.T) {
	m := onForwardTab(t)
	m = feed(t, m, forward.ChangedMsg{Forwards: sampleForwards()})

	m = feed(t, m, testutil.Key(keymap.Kill))
	if m.forwardModel.confirmModal == nil {
		t.Fatal("K stopped the forward with no confirmation")
	}

	_, cmd := step(t, m, components.ConfirmModalYesMsg{})
	var closed *forward.CloseMsg
	for _, msg := range testutil.Msgs(cmd) {
		if c, ok := msg.(forward.CloseMsg); ok {
			closed = &c
		}
	}
	if closed == nil {
		t.Fatal("confirming asked the router to close nothing")
	}
	if closed.ID != "1" {
		t.Errorf("the router was asked to close %q, want the selected forward %q", closed.ID, "1")
	}
}

// The confirmation used to go to the Ports tab unconditionally, which would
// have turned a "yes" here into a kill there.
func TestAConfirmationGoesToTheTabThatAskedForIt(t *testing.T) {
	m := onForwardTab(t)
	m = feed(t, m, forward.ChangedMsg{Forwards: sampleForwards()})
	m = feed(t, m, testutil.Key(keymap.Kill))

	_, cmd := step(t, m, components.ConfirmModalYesMsg{})
	for _, msg := range testutil.Msgs(cmd) {
		if _, ok := msg.(portsKillResultMsg); ok {
			t.Fatal("the Forward tab's confirmation reached the Ports tab's kill")
		}
	}
}

func TestKOnAnEmptyTableSaysWhyRatherThanDoingNothing(t *testing.T) {
	m := onForwardTab(t)

	_, cmd := step(t, m, testutil.Key(keymap.Kill))
	if cmd == nil {
		t.Fatal("K was swallowed on an empty table")
	}
	if m.forwardModel.confirmModal != nil {
		t.Error("K put up a confirmation with nothing selected")
	}
	if got := m.forwardModel.closable().Reason; got != reasonNoForwardRow {
		t.Errorf("closable().Reason = %q, want %q", got, reasonNoForwardRow)
	}
}

// ── Header and footer ────────────────────────────────────────────────────────

// Rule 130: the set of keys does not change from one state to another within
// the same screen — only whether they are greyed.
func TestTheTabOffersTheSameKeysEmptyOrNot(t *testing.T) {
	empty := testutil.ShortcutKeys(onForwardTab(t).GetShortcuts())

	filled := onForwardTab(t)
	filled = feed(t, filled, forward.ChangedMsg{Forwards: sampleForwards()})
	full := testutil.ShortcutKeys(filled.GetShortcuts())

	if len(empty) != len(full) {
		t.Fatalf("the key set changed with the rows: %v then %v", empty, full)
	}
	for i := range empty {
		if empty[i] != full[i] {
			t.Errorf("key %d is %q when empty and %q when filled", i, empty[i], full[i])
		}
	}
}

func TestStopIsGreyedWithNothingToStop(t *testing.T) {
	empty := onForwardTab(t).GetShortcuts()
	if !testutil.ShortcutDisabled(empty, keymap.Kill) {
		t.Errorf("%s is not greyed on an empty table", keymap.Kill)
	}

	filled := onForwardTab(t)
	filled = feed(t, filled, forward.ChangedMsg{Forwards: sampleForwards()})
	if !testutil.ShortcutEnabled(filled.GetShortcuts(), keymap.Kill) {
		t.Errorf("%s is greyed although a forward is selected", keymap.Kill)
	}
}

// A form is a mode, so it replaces the vocabulary rather than greying it
// (Rule 130).
func TestTheFormReplacesTheShortcutsRatherThanGreyingThem(t *testing.T) {
	m := onForwardTab(t)
	m = feed(t, m, testutil.Key(keymap.New))

	sc := m.GetShortcuts()
	if testutil.HasShortcut(sc, keymap.Kill) {
		t.Error("the form's shortcut list still offers the table's stop key")
	}
	if !testutil.HasShortcut(sc, "enter") || !testutil.HasShortcut(sc, "esc") {
		t.Errorf("the form offers %v, want enter and esc", testutil.ShortcutKeys(sc))
	}
}

func TestTheHeaderCountsTheOpenForwards(t *testing.T) {
	m := onForwardTab(t)
	m = feed(t, m, forward.ChangedMsg{Forwards: sampleForwards()})

	var found bool
	for _, info := range m.GetHeaderInfo("default") {
		if info.Key == "Forwards" {
			found = true
			if info.Value != "2" {
				t.Errorf("the header says %q forwards, want %q", info.Value, "2")
			}
		}
	}
	if !found {
		t.Error("the header carries no forward count")
	}
}

// Rule 116: the rendered row fills the inside of the viewport exactly.
func TestTheTableFillsTheViewport(t *testing.T) {
	const width = 120
	m := onForwardTab(t)
	m = feed(t, m, tea.WindowSizeMsg{Width: width, Height: 40})
	m = feed(t, m, forward.ChangedMsg{Forwards: sampleForwards()})

	if got, want := m.forwardModel.table.RenderedWidth(), width-2; got != want {
		t.Errorf("the rendered row is %d cells wide, want %d", got, want)
	}
}

// Rule 124: the footer is the tab bar, a blank line and the info line, plus
// the filter bar when it is showing.
func TestTheFooterKeepsItsBudget(t *testing.T) {
	m := onForwardTab(t)
	if got := m.GetFooterHeight(); got != 3 {
		t.Errorf("GetFooterHeight = %d with no filter bar, want 3", got)
	}
}

// A textinput with Width 0 shows one rune of its placeholder, so an unsized
// form read "8" and "h". The width has to be set, and it has to reach the
// inputs whichever way the form was built or resized.
func TestTheFormShowsWholePlaceholders(t *testing.T) {
	f := NewForwardForm(100)
	view := f.View()
	for _, want := range []string{"8080", "host:port"} {
		if !strings.Contains(view, want) {
			t.Errorf("the form renders %q, want the whole placeholder %q", view, want)
		}
	}

	f.SetWidth(60)
	if f.targetInput.Width <= 1 || f.portInput.Width < 5 {
		t.Errorf("after a resize the inputs are %d and %d wide, want room for a port and a target",
			f.portInput.Width, f.targetInput.Width)
	}
}

func TestTheFieldsAreSeparatedByABlankLine(t *testing.T) {
	lines := strings.Split(NewForwardForm(100).View(), "\n")
	// blank, port, blank, target
	if len(lines) != 4 {
		t.Fatalf("the form is %d lines, want 4 (top padding, port, spacer, target)", len(lines))
	}
	if strings.TrimSpace(ansi.Strip(lines[2])) != "" {
		t.Errorf("line 3 is %q, want a blank line between the fields", lines[2])
	}
}

// ── Pause and resume (§3.75) ─────────────────────────────────────────────────

// mixedForwards holds one row in each state.
func mixedForwards() []forward.Forward {
	return []forward.Forward{
		{ID: "1", LocalPort: 8080, Target: "10.0.0.5:80", Opened: time.Now(), Active: 2, Total: 9},
		{ID: "2", LocalPort: 9090, Target: "db.internal:5432", State: forward.StatePaused},
		{ID: "3", LocalPort: 7070, Target: "cache:6379", State: forward.StateUnbound, LastErr: "the local port is already in use"},
	}
}

func togglesRequested(cmd tea.Cmd) []forward.ToggleMsg {
	var out []forward.ToggleMsg
	for _, msg := range testutil.Msgs(cmd) {
		if tm, ok := msg.(forward.ToggleMsg); ok {
			out = append(out, tm)
		}
	}
	return out
}

func TestSpaceAsksTheRouterToToggleTheSelectedForward(t *testing.T) {
	m := onForwardTab(t)
	m = feed(t, m, forward.ChangedMsg{Forwards: mixedForwards()})

	_, cmd := step(t, m, testutil.Key("space"))
	got := togglesRequested(cmd)
	if len(got) != 1 || got[0].ID != "1" {
		t.Errorf("space asked for %+v, want one toggle of the selected forward %q", got, "1")
	}
}

func TestSpaceOnAnEmptyTableSaysWhyRatherThanDoingNothing(t *testing.T) {
	m := onForwardTab(t)

	_, cmd := step(t, m, testutil.Key("space"))
	if cmd == nil {
		t.Fatal("space was swallowed on an empty table")
	}
	if len(togglesRequested(cmd)) != 0 {
		t.Error("space asked the router to toggle a forward that does not exist")
	}
	if got := m.forwardModel.switchable().Reason; got != reasonNoForwardRow {
		t.Errorf("switchable().Reason = %q, want %q", got, reasonNoForwardRow)
	}
}

func TestSpaceIsGreyedWithNothingToSwitch(t *testing.T) {
	if !testutil.ShortcutDisabled(onForwardTab(t).GetShortcuts(), "space") {
		t.Error("space is not greyed on an empty table")
	}
	filled := feed(t, onForwardTab(t), forward.ChangedMsg{Forwards: sampleForwards()})
	if !testutil.ShortcutEnabled(filled.GetShortcuts(), "space") {
		t.Error("space is greyed although a forward is selected")
	}
}

// The entry keeps its slot and changes its label with the row: pausing a live
// forward and resuming a stopped one are the same key doing the opposite.
func TestSpaceSaysWhichWayItWillSwitch(t *testing.T) {
	m := onForwardTab(t)
	m = feed(t, m, forward.ChangedMsg{Forwards: mixedForwards()})

	label := func() string {
		for _, s := range m.GetShortcuts() {
			if s.Key == "space" {
				return s.Description
			}
		}
		t.Fatal("no space entry in the shortcuts")
		return ""
	}

	if got := label(); got != "Pause forward" {
		t.Errorf("on a live row space reads %q, want %q", got, "Pause forward")
	}
	m = feed(t, m, testutil.Key("down"))
	if got := label(); got != "Resume forward" {
		t.Errorf("on a paused row space reads %q, want %q", got, "Resume forward")
	}
	m = feed(t, m, testutil.Key("down"))
	if got := label(); got != "Resume forward" {
		t.Errorf("on an unbound row space reads %q, want %q", got, "Resume forward")
	}
}

func TestTheTabOffersTheSameKeysWhateverTheRowsState(t *testing.T) {
	live := testutil.ShortcutKeys(feed(t, onForwardTab(t), forward.ChangedMsg{Forwards: sampleForwards()}).GetShortcuts())
	mixed := testutil.ShortcutKeys(feed(t, onForwardTab(t), forward.ChangedMsg{Forwards: mixedForwards()}).GetShortcuts())
	if len(live) != len(mixed) {
		t.Fatalf("the key set changed with the rows' states: %v then %v", live, mixed)
	}
	for i := range live {
		if live[i] != mixed[i] {
			t.Errorf("key %d is %q with live rows and %q with mixed ones", i, live[i], mixed[i])
		}
	}
}

func TestTheTableSaysWhatIsNotListening(t *testing.T) {
	m := onForwardTab(t)
	m = feed(t, m, forward.ChangedMsg{Forwards: mixedForwards()})
	out := ansi.Strip(m.View())

	for _, want := range []string{"paused", "the local port is already in use"} {
		if !strings.Contains(out, want) {
			t.Errorf("the table does not show %q:\n%s", want, out)
		}
	}
}

func TestTheHeaderSaysHowManyAreListening(t *testing.T) {
	m := onForwardTab(t)
	m = feed(t, m, forward.ChangedMsg{Forwards: mixedForwards()})
	if got, want := m.forwardModel.summaryLine(), "1 live of 3"; got != want {
		t.Errorf("summaryLine = %q, want %q", got, want)
	}
}

// Deleting is no longer only stopping: it takes the row out of the file too,
// and the confirmation has to say so.
func TestTheDeleteConfirmationSaysTheForwardWillNotComeBack(t *testing.T) {
	m := onForwardTab(t)
	m = feed(t, m, forward.ChangedMsg{Forwards: sampleForwards()})
	m = feed(t, m, testutil.Key(keymap.Kill))
	if m.forwardModel.confirmModal == nil {
		t.Fatal("K put up no confirmation")
	}
	if !strings.Contains(ansi.Strip(m.forwardModel.confirmModal.View()), "next launch") {
		t.Errorf("the confirmation does not mention the next launch:\n%s", ansi.Strip(m.forwardModel.confirmModal.View()))
	}
}
