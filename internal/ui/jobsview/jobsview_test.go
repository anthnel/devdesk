package jobsview

import (
	"io"
	"log"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// This view fetches nothing and launches nothing, so every test here drives
// Update with the one message it lives on — jobs.ChangedMsg — and reads what
// the tables and the header say back.

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	// Reading a Cmd runs it, and a refusal carries a three-second footer expiry
	// (Rule 128). The test that asserts on that duration lives in the package
	// that owns it.
	sharedcomponents.FooterMsgDuration = time.Millisecond

	code := m.Run()

	log.SetOutput(os.Stderr)
	os.Exit(code)
}

// ── Fixtures ─────────────────────────────────────────────────────────────────

// testContext is the name the router would have stamped. Nothing reads it from
// disk — the view is told, which is the point.
const testContext = "work"

var startedAt = time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)

// run builds a snapshot run. The registry stamps ID and StartedAt when it
// admits one; here they are set by hand because no registry is involved.
func run(id jobs.JobID, kind jobs.Kind, label string, states ...jobs.ItemState) jobs.Run {
	targets := make([]string, len(states))
	for i := range states {
		targets[i] = label + "/target" + string(rune('a'+i))
	}
	r := jobs.NewRun(kind, command.ViewWorkspaces, testContext, label, targets...)
	for i, state := range states {
		r.Items[i].State = state
	}
	r.ID = id
	r.StartedAt = startedAt
	return r
}

func newModel(t *testing.T) Model {
	t.Helper()
	m := New(config.Default(), testContext)
	m.resize(120, 20)
	return m
}

// feed sends one message and returns the settled model.
func feed(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want jobsview.Model", updated)
	}
	return next
}

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want jobsview.Model", updated)
	}
	return next, cmd
}

func withRuns(t *testing.T, runs ...jobs.Run) Model {
	t.Helper()
	return feed(t, newModel(t), jobs.ChangedMsg{Runs: runs, Frame: "⠋"})
}

// ── The snapshot is the only source ──────────────────────────────────────────

func TestTheRowsComeFromTheSnapshot(t *testing.T) {
	m := withRuns(t,
		run(1, jobs.KindScan, "~/work", jobs.ItemDone, jobs.ItemRunning),
		run(2, jobs.KindSync, "~/perso", jobs.ItemDone),
	)

	if got := len(m.runTable.Items()); got != 2 {
		t.Fatalf("the table holds %d runs, want the 2 in the snapshot", got)
	}
	view := m.View()
	for _, want := range []string{"~/work", "~/perso", "scan", "sync"} {
		if !strings.Contains(view, want) {
			t.Errorf("the table does not show %q:\n%s", want, view)
		}
	}
}

// D8: a run is stamped with the context it started in and kept for the session,
// so a context switch hides it rather than dropping it. The view filters.
func TestARunFromAnotherContextIsNotListed(t *testing.T) {
	mine := run(1, jobs.KindScan, "~/work", jobs.ItemDone)
	theirs := run(2, jobs.KindScan, "~/elsewhere", jobs.ItemDone)
	theirs.Context = "another-context"

	m := withRuns(t, mine, theirs)

	if got := len(m.runTable.Items()); got != 1 {
		t.Fatalf("the table holds %d runs, want only the current context's", got)
	}
	if strings.Contains(m.View(), "~/elsewhere") {
		t.Error("a run stamped with another context is on screen")
	}
}

// ── The drill-down (D2) ──────────────────────────────────────────────────────

func TestRightOpensTheRunAndEscComesBack(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemDone, jobs.ItemRunning, jobs.ItemQueued))

	m, _ = step(t, m, testutil.Key("right"))
	if m.level != levelItems {
		t.Fatal("→ did not open the run")
	}
	if got := len(m.itemTable.Items()); got != 3 {
		t.Errorf("the run shows %d targets, want its 3", got)
	}
	if !strings.Contains(m.GetTitle(), "~/work") {
		t.Errorf("the title = %q, want the run named in it", m.GetTitle())
	}

	m, _ = step(t, m, testutil.Key("esc"))
	if m.level != levelRuns {
		t.Error("esc did not come back to the list")
	}
}

// The item level is where D6 is visible: queued and running are different
// things, and the whole point of keeping them apart is being able to say
// "1 running, 1 waiting".
func TestTheTargetsShowQueuedApartFromRunning(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemRunning, jobs.ItemQueued))
	m, _ = step(t, m, testutil.Key("right"))

	view := m.View()
	for _, want := range []string{"running", "queued"} {
		if !strings.Contains(view, want) {
			t.Errorf("the targets do not show %q:\n%s", want, view)
		}
	}
}

// The registry keeps only the last MaxFinishedRuns settled runs, so a run left
// open long enough is pruned out from under the cursor. Falling back to the
// list is what stops the view showing a breadcrumb for a run that is gone.
func TestTheItemLevelClosesWhenItsRunIsPruned(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemDone))
	m, _ = step(t, m, testutil.Key("right"))
	if m.level != levelItems {
		t.Fatal("→ did not open the run")
	}

	m = feed(t, m, jobs.ChangedMsg{Runs: []jobs.Run{run(2, jobs.KindSync, "~/perso", jobs.ItemDone)}})

	if m.level != levelRuns {
		t.Error("the view stayed on a run the snapshot no longer holds")
	}
	if m.openRun != 0 {
		t.Errorf("openRun = %d, want it forgotten", m.openRun)
	}
}

// ── The spinner is the router's (D5) ─────────────────────────────────────────

func TestARunningRunCarriesTheRoutersFrame(t *testing.T) {
	m := feed(t, newModel(t), jobs.ChangedMsg{
		Runs:  []jobs.Run{run(1, jobs.KindScan, "~/work", jobs.ItemRunning)},
		Frame: "⣾",
	})

	row, ok := m.runTable.Selected()
	if !ok {
		t.Fatal("no row is selected")
	}
	cell := runStateGlyph(row)
	if cell != "⣾" {
		t.Errorf("the state cell = %q, want the frame the router broadcast", cell)
	}
	if strings.Contains(cell, "\x1b") {
		t.Error("the cell carries an escape sequence — Rule 122: it is measured before it is styled")
	}
}

func TestASettledRunShowsItsOutcomeRatherThanASpinner(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state jobs.ItemState
	}{
		{"done", jobs.ItemDone},
		{"failed", jobs.ItemFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := withRuns(t, run(1, jobs.KindScan, "~/work", tc.state))
			row, _ := m.runTable.Selected()
			if glyph := runStateGlyph(row); glyph == "⠋" {
				t.Errorf("a %s run still shows the spinner frame", tc.name)
			}
		})
	}
}

// ── Rule 130: the column does not move ───────────────────────────────────────

func TestTheShortcutColumnIsTheSameAtBothLevels(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemDone))
	atRuns := testutil.ShortcutKeys(m.GetShortcuts())

	m, _ = step(t, m, testutil.Key("right"))
	atItems := testutil.ShortcutKeys(m.GetShortcuts())

	if !slices.Equal(atRuns, atItems) {
		t.Errorf("the shortcut keys change with the level: %v then %v", atRuns, atItems)
	}
}

func TestEscIsGreyedAtTheTopLevelAndLiveInsideARun(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemDone))

	if !testutil.ShortcutDisabled(m.GetShortcuts(), "esc") {
		t.Error("esc is offered at the top level, where there is nothing to go back to")
	}

	m, _ = step(t, m, testutil.Key("right"))
	if !testutil.ShortcutEnabled(m.GetShortcuts(), "esc") {
		t.Error("esc is greyed inside a run, where it is what comes back")
	}
}

// Rule 130: what the header greys, the handler refuses — and it says why.
func TestASortInsideARunIsRefusedWithItsReason(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemDone))
	m, _ = step(t, m, testutil.Key("right"))

	if !testutil.ShortcutDisabled(m.GetShortcuts(), ".") {
		t.Fatal("the sort is offered on a table that declares no comparator")
	}

	m, cmd := step(t, m, testutil.Key("."))
	if cmd == nil {
		t.Fatal("the refusal was silent — Rule 130 forbids a bare return")
	}
	if !m.footer.IsSet() {
		t.Error("nothing was posted to the footer")
	}
}

func TestOpeningAnEmptyRunIsRefusedWithItsReason(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work"))

	if !testutil.ShortcutDisabled(m.GetShortcuts(), "→") {
		t.Fatal("→ is offered on a run with no targets")
	}

	m, cmd := step(t, m, testutil.Key("right"))
	if m.level != levelRuns {
		t.Error("the empty run opened anyway")
	}
	if cmd == nil || !m.footer.IsSet() {
		t.Error("the refusal was silent")
	}
}

// ── The footer, and the empty screens ────────────────────────────────────────

// Rule 139: a table view never renders a loading body. This one cannot even be
// loading — it fetches nothing — so what its body says when empty is the whole
// of the question, and the two emptinesses are different sentences.
func TestAnEmptySessionAndAnEmptyFilterSayDifferentThings(t *testing.T) {
	if body := newModel(t).View(); !strings.Contains(body, "Nothing has run") {
		t.Errorf("an empty session renders %q", body)
	}

	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemDone))
	m, _ = step(t, m, testutil.Key("/"))
	for _, msg := range testutil.Type("nothing-matches-this") {
		m, _ = step(t, m, msg)
	}
	if body := m.View(); !strings.Contains(body, "filter") {
		t.Errorf("a filter hiding every row renders %q, want it to say so", body)
	}
}

func TestTheFooterCountsWhatIsRunning(t *testing.T) {
	m := withRuns(t,
		run(1, jobs.KindScan, "~/work", jobs.ItemRunning),
		run(2, jobs.KindSync, "~/perso", jobs.ItemQueued),
		run(3, jobs.KindScan, "~/old", jobs.ItemDone),
	)

	status := m.status()
	if want := "2 jobs running"; status.Text != want {
		t.Errorf("the footer says %q, want %q", status.Text, want)
	}
	if !status.Spinner {
		t.Error("the running count carries no spinner")
	}
	// The sentence stops there: ":jobs for details" is what every other footer
	// adds, and this is where that link leads.
	if strings.Contains(status.Text, ":jobs") {
		t.Error("the jobs view points at itself")
	}
}

func TestTheFooterIsSilentWhenNothingRuns(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemDone))
	if got := m.status().Text; got != "" {
		t.Errorf("the footer says %q with nothing running", got)
	}
}

// Rule 124: the breadcrumb is a footer line, and the budget has to know.
func TestTheBreadcrumbCostsAFooterLine(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemDone))
	atRuns := m.GetFooterHeight()

	m, _ = step(t, m, testutil.Key("right"))
	if got, want := m.GetFooterHeight(), atRuns+1; got != want {
		t.Errorf("the footer is %d lines inside a run, want %d", got, want)
	}
	if !strings.Contains(m.RenderFooter(120), "Runs") {
		t.Error("the breadcrumb does not name the level above")
	}
}

// The named indexes are what makes runColumnStarted right, and they are right
// only as long as the block matches the table. Inserting a column above the
// last one is the change that would otherwise open the list sorted by the wrong
// thing, in silence.
func TestTheNamedColumnsAreWhereTheirNamesSay(t *testing.T) {
	cols := runColumns()
	for _, want := range []struct {
		index int
		title string
	}{
		{runColumnState, ""},
		{runColumnKind, "Kind"},
		{runColumnLabel, "Label"},
		{runColumnProgress, "Progress"},
		{runColumnStateText, "State"},
		{runColumnStarted, "Started"},
	} {
		if want.index >= len(cols) {
			t.Fatalf("index %d is past the %d columns declared", want.index, len(cols))
		}
		if got := cols[want.index].Title; got != want.title {
			t.Errorf("column %d is %q, want %q", want.index, got, want.title)
		}
	}
}

// Rule 116: every column declares its Sizing, at both levels. The package-wide
// test walks the sources; this one walks the values, which is what catches a
// column built by a helper.
func TestEveryColumnDeclaresItsSizing(t *testing.T) {
	for _, col := range runColumns() {
		if col.Sizing == datatable.SizingUnset {
			t.Errorf("run column %q has no Sizing", col.Title)
		}
	}
	for _, col := range itemColumns() {
		if col.Sizing == datatable.SizingUnset {
			t.Errorf("target column %q has no Sizing", col.Title)
		}
	}
}

// Rule 122: a cell is measured before it is styled, so nothing a Cell returns
// may carry an escape sequence — the spinner frame included, which is why the
// router broadcasts it bare beside its rendered form.
func TestNoCellCarriesAnEscapeSequence(t *testing.T) {
	testutil.TrueColor(t)

	// Every state, because the styles differ by state: a Style applied inside a
	// Cell would go unnoticed on the one branch whose style happens to be empty.
	for _, state := range []jobs.ItemState{
		jobs.ItemQueued, jobs.ItemRunning, jobs.ItemDone, jobs.ItemSkipped, jobs.ItemFailed,
	} {
		row := runRow{Run: run(1, jobs.KindScan, "~/work", state), Frame: "\u2807"}
		for _, col := range runColumns() {
			if strings.Contains(col.Cell(row), "\x1b") {
				t.Errorf("run column %q returns a styled cell for a %s run", col.Title, state)
			}
		}
		item := itemRow{Item: jobs.Item{Target: "/repos/a", State: state, Detail: "a reason"}, Frame: "\u2807"}
		for _, col := range itemColumns() {
			if strings.Contains(col.Cell(item), "\x1b") {
				t.Errorf("target column %q returns a styled cell for a %s target", col.Title, state)
			}
		}
	}
}

// ── Stopping work (D7, poste 8) ──────────────────────────────────────────────

// kindRun builds a run of a given kind, which is what the two halves of D7 turn
// on: the queue stops whatever the kind, one target in flight only where
// cutting leaves nothing behind.
func kindRun(id jobs.JobID, kind jobs.Kind, states ...jobs.ItemState) jobs.Run {
	targets := make([]string, len(states))
	for i := range states {
		targets[i] = "target" + string(rune('a'+i))
	}
	r := jobs.NewRun(kind, command.ViewWorkspaces, testContext, "~/work", targets...)
	for i, state := range states {
		r.Items[i].State = state
	}
	r.ID = id
	r.StartedAt = startedAt
	return r
}

func TestStopIsOfferedOnARunThatHasSomethingLeftToStop(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  jobs.Run
		want bool
	}{
		{"a scan in flight", kindRun(1, jobs.KindScan, jobs.ItemRunning), true},
		{"a scan already done", kindRun(1, jobs.KindScan, jobs.ItemDone), false},
		// The case the availability exists for: one item, in flight, of a kind
		// that must never be cut. `!Finished()` would offer the key and then
		// refuse it, which is the silent refusal Rule 130 removes.
		{"a delete in flight", kindRun(1, jobs.KindDelete, jobs.ItemRunning), false},
		{"a sync with a queue left", kindRun(1, jobs.KindSync, jobs.ItemRunning, jobs.ItemQueued), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := withRuns(t, tc.run)
			if got := testutil.ShortcutEnabled(m.GetShortcuts(), keymap.Kill); got != tc.want {
				t.Errorf("K enabled = %v, want %v", got, tc.want)
			}
			if !testutil.HasShortcut(m.GetShortcuts(), keymap.Kill) {
				t.Error("K was dropped rather than greyed (Rule 130)")
			}
		})
	}
}

// Inside a run the question is narrower, and it is the kind that answers it.
func TestStopOnATargetIsOfferedOnlyWhereCuttingLeavesNothingBehind(t *testing.T) {
	for _, tc := range []struct {
		kind jobs.Kind
		want bool
	}{
		{jobs.KindScan, true},
		{jobs.KindClone, false},
		{jobs.KindSync, false},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			m := withRuns(t, kindRun(1, tc.kind, jobs.ItemRunning))
			m, _ = step(t, m, testutil.Key("right"))

			if got := testutil.ShortcutEnabled(m.GetShortcuts(), keymap.Kill); got != tc.want {
				t.Errorf("K enabled = %v, want %v", got, tc.want)
			}
		})
	}
}

// A settled target is never stoppable, whatever the kind.
func TestStopIsGreyedOnASettledTarget(t *testing.T) {
	m := withRuns(t, kindRun(1, jobs.KindScan, jobs.ItemDone))
	m, _ = step(t, m, testutil.Key("right"))

	if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Kill) {
		t.Error("K is offered on a target that has already finished")
	}
}

// Rule 130: what the header greys, the handler refuses — and it says why.
func TestStoppingWhatCannotBeStoppedIsRefusedWithItsReason(t *testing.T) {
	m := withRuns(t, kindRun(1, jobs.KindDelete, jobs.ItemRunning))

	m, cmd := step(t, m, testutil.Key(keymap.Kill))

	if cmd == nil {
		t.Fatal("the refusal was silent — Rule 130 forbids a bare return")
	}
	if _, asked := testutil.MsgOf[jobs.CancelMsg](cmd); asked {
		t.Error("a run that cannot be stopped was asked to stop anyway")
	}
	if !m.footer.IsSet() {
		t.Error("nothing was posted to the footer")
	}
	if !strings.Contains(m.footer.Text(), "delete") {
		t.Errorf("footer = %q, want the kind named — a greyed key has to say why", m.footer.Text())
	}
}

// The view asks; the router acts. What it holds is the identifier it read off
// the row it is rendering, not a pointer to the registry (D1).
func TestStopAsksTheRouterToCancelTheSelectedRun(t *testing.T) {
	m := withRuns(t, kindRun(7, jobs.KindScan, jobs.ItemRunning))

	_, cmd := step(t, m, testutil.Key(keymap.Kill))

	msg, ok := testutil.MsgOf[jobs.CancelMsg](cmd)
	if !ok {
		t.Fatal("K asked the router for nothing")
	}
	if msg.ID != 7 {
		t.Errorf("ID = %d, want the run under the cursor", msg.ID)
	}
}

func TestStopInsideARunNamesTheTarget(t *testing.T) {
	m := withRuns(t, kindRun(7, jobs.KindScan, jobs.ItemRunning, jobs.ItemQueued))
	m, _ = step(t, m, testutil.Key("right"))

	_, cmd := step(t, m, testutil.Key(keymap.Kill))

	msg, ok := testutil.MsgOf[jobs.CancelItemMsg](cmd)
	if !ok {
		t.Fatal("K inside a run asked the router for nothing")
	}
	if msg.ID != 7 || msg.Target != "targeta" {
		t.Errorf("message = %+v, want the run and the target under the cursor", msg)
	}
}
