package jobsview

import (
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-142: :jobs on the activity column (Rule 125, "The activity column").

// ── Criterion 1: column 0 is datatable.IndicatorColumn at both levels ────────

func TestColumnZeroIsTheActivityColumnAtBothLevels(t *testing.T) {
	check := func(t *testing.T, name string, title string, sizing datatable.Sizing, minWidth int, hasIndicator, hasLess, hasSearch bool) {
		t.Helper()
		if title != "" {
			t.Errorf("%s: column 0 is titled %q, want untitled", name, title)
		}
		if sizing != datatable.SizingFixed || minWidth != datatable.IconColumnWidth {
			t.Errorf("%s: column 0 sizing/min = %v/%d, want SizingFixed/%d", name, sizing, minWidth, datatable.IconColumnWidth)
		}
		if !hasIndicator {
			t.Errorf("%s: column 0 is not built by datatable.IndicatorColumn (no Indicator)", name)
		}
		if hasLess || hasSearch {
			t.Errorf("%s: column 0 declares Less=%v Search=%v, want neither", name, hasLess, hasSearch)
		}
	}
	rc := runColumns()[0]
	check(t, "runs", rc.Title, rc.Sizing, rc.MinWidth, rc.Indicator != nil, rc.Less != nil, rc.Search != nil)
	ic := itemColumns()[0]
	check(t, "targets", ic.Title, ic.Sizing, ic.MinWidth, ic.Indicator != nil, ic.Less != nil, ic.Search != nil)
}

// ── Criterion 2: one glyph per state, and the State word follows the dot ─────

func TestEachRunStateMapsToItsOwnIndicator(t *testing.T) {
	col := runColumns()[runColumnState]
	if col.Indicator == nil {
		t.Fatal("the runs table has no activity column")
	}
	for _, tc := range []struct {
		name  string
		items []jobs.ItemState
		want  indicator.State
	}{
		{"queued", []jobs.ItemState{jobs.ItemQueued}, indicator.Queued},
		{"running", []jobs.ItemState{jobs.ItemRunning}, indicator.Running},
		{"done", []jobs.ItemState{jobs.ItemDone}, indicator.Done},
		{"failed", []jobs.ItemState{jobs.ItemFailed}, indicator.Failed},
	} {
		if got := col.Indicator(runRow{Run: run(1, jobs.KindScan, "~/work", tc.items...)}); got != tc.want {
			t.Errorf("%s run: indicator = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestEachItemStateMapsToItsOwnIndicator(t *testing.T) {
	col := itemColumns()[0]
	if col.Indicator == nil {
		t.Fatal("the targets table has no activity column")
	}
	for state, want := range map[jobs.ItemState]indicator.State{
		jobs.ItemQueued: indicator.Queued, jobs.ItemRunning: indicator.Running,
		jobs.ItemDone: indicator.Done, jobs.ItemFailed: indicator.Failed,
		jobs.ItemSkipped: indicator.Skipped,
	} {
		if got := col.Indicator(itemRow{Item: jobs.Item{Target: "/a", State: state}}); got != want {
			t.Errorf("%s item: indicator = %v, want %v", state, got, want)
		}
	}
}

func TestEachSettledStateShowsItsGlyphInTheTables(t *testing.T) {
	m := withRuns(t,
		run(1, jobs.KindScan, "~/a", jobs.ItemDone),
		run(2, jobs.KindScan, "~/b", jobs.ItemFailed),
		run(3, jobs.KindScan, "~/c", jobs.ItemQueued),
	)
	v := m.Render()
	for _, s := range []indicator.State{indicator.Done, indicator.Failed, indicator.Queued} {
		if g := indicator.Glyph(s, true); !strings.Contains(v, g) {
			t.Errorf("the runs table does not show %s (%s):\n%s", s, g, v)
		}
	}
	for _, old := range []string{theme.IconOK, theme.IconError, theme.IconPending} {
		if strings.Contains(v, old) {
			t.Errorf("the runs table still shows the retired glyph %q", old)
		}
	}

	m = withRuns(t, run(1, jobs.KindSync, "~/a", jobs.ItemSkipped, jobs.ItemQueued, jobs.ItemDone))
	m, _ = step(t, m, testutil.Key("right"))
	v = m.Render()
	for _, s := range []indicator.State{indicator.Skipped, indicator.Queued, indicator.Done} {
		if g := indicator.Glyph(s, true); !strings.Contains(v, g) {
			t.Errorf("the targets table does not show %s (%s):\n%s", s, g, v)
		}
	}
	for _, old := range []string{theme.IconSkipped, theme.IconPending, theme.IconOK} {
		if strings.Contains(v, old) {
			t.Errorf("the targets table still shows the retired glyph %q", old)
		}
	}
}

func TestTheStateWordTakesTheColourOfTheDot(t *testing.T) {
	stateText := runColumns()[runColumnStateText]
	for _, items := range [][]jobs.ItemState{
		{jobs.ItemQueued}, {jobs.ItemRunning}, {jobs.ItemDone}, {jobs.ItemFailed},
	} {
		r := run(1, jobs.KindScan, "~/work", items...)
		want := indicator.Style(sharedcomponents.RunIndicator(r.State())).GetForeground()
		if got := stateText.Style(runRow{Run: r}).GetForeground(); got != want {
			t.Errorf("run %s: State word colour = %v, want the dot's %v", r.State(), got, want)
		}
	}

	itemState := itemColumns()[2]
	for _, st := range []jobs.ItemState{jobs.ItemQueued, jobs.ItemRunning, jobs.ItemDone, jobs.ItemSkipped, jobs.ItemFailed} {
		want := indicator.Style(sharedcomponents.ItemIndicator(st)).GetForeground()
		if got := itemState.Style(itemRow{Item: jobs.Item{Target: "/a", State: st}}).GetForeground(); got != want {
			t.Errorf("item %s: State word colour = %v, want the dot's %v", st, got, want)
		}
	}
}

func TestDoneIsNotGreen(t *testing.T) {
	green := theme.StatusOKStyle.GetForeground()
	r := run(1, jobs.KindScan, "~/work", jobs.ItemDone)
	if got := runColumns()[runColumnStateText].Style(runRow{Run: r}).GetForeground(); got == green {
		t.Error("a done run's State word is still green")
	}
	it := itemRow{Item: jobs.Item{Target: "/a", State: jobs.ItemDone}}
	if got := itemColumns()[2].Style(it).GetForeground(); got == green {
		t.Error("a done target's State word is still green")
	}
}

// ── Criterion 3: the phase is read at render, nothing is rebuilt ─────────────

func TestAFrameSetsThePhaseOnBothTablesWithoutRebuildingRows(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemRunning))
	before := m.runTable.Items()

	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if m.runTable.BlinkPhase() || m.itemTable.BlinkPhase() {
		t.Errorf("phase after Blink=false: runs %v, targets %v, want both off", m.runTable.BlinkPhase(), m.itemTable.BlinkPhase())
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if !m.runTable.BlinkPhase() || !m.itemTable.BlinkPhase() {
		t.Error("Blink=true did not turn the phase back on")
	}
	after := m.runTable.Items()
	if len(before) != 1 || len(after) != 1 || &before[0] != &after[0] {
		t.Error("a frame rebuilt the rows; the phase is read at render")
	}
}

func TestAQueuedRowStaysSteadyWhateverThePhase(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemQueued))
	for _, blink := range []bool{true, false} {
		m = feed(t, m, jobs.FrameMsg{Blink: blink})
		if !strings.Contains(m.Render(), indicator.Glyph(indicator.Queued, true)) {
			t.Errorf("the queued run lost its glyph at blink=%v", blink)
		}
	}
}

func TestUnderStaticMotionARunningRowIsAFixedFilledDot(t *testing.T) {
	// Static motion is the router's: it keeps broadcasting Blink=true.
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemRunning))
	for range 3 {
		m = feed(t, m, jobs.FrameMsg{Blink: true})
		if v := m.Render(); !strings.Contains(v, "●") || strings.Contains(v, "○") {
			t.Fatalf("a running row is not a steady ●:\n%s", v)
		}
	}
}

func TestTheModelCarriesNoRowSpinnerAnyMore(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemRunning))
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if hasBraille(m.Render()) {
		t.Error("a spinner frame reaches the table")
	}
}

// ── Criterion 4: the footer carries the selected row's phrase ────────────────

func TestTheFooterDetailIsTheSelectedRunsSentence(t *testing.T) {
	r := run(1, jobs.KindScan, "~/work", jobs.ItemRunning)
	m := withRuns(t, r)

	status := m.status()
	if want := sharedcomponents.DescribeRun(r).Detail; status.Detail != want {
		t.Errorf("Detail = %+v, want %+v", status.Detail, want)
	}
	if want := "1 job running"; status.Text != want {
		t.Errorf("Text = %q, want %q", status.Text, want)
	}
	if status.Spinner {
		t.Error("the footer spins beside a row whose own dot moves")
	}
	footer := m.RenderFooter(120)
	if !strings.Contains(footer, "Scanning") || !strings.Contains(footer, "~/work") || !strings.Contains(footer, "1 job running") {
		t.Errorf("the footer lacks the sentence or the count:\n%s", footer)
	}
}

func TestASettledRunSelectedStillHasItsSentence(t *testing.T) {
	r := run(1, jobs.KindScan, "~/work", jobs.ItemDone)
	m := withRuns(t, r)
	status := m.status()
	if want := sharedcomponents.DescribeRun(r).Detail; status.Detail != want || status.Detail.IsZero() {
		t.Errorf("Detail = %+v, want %+v", status.Detail, want)
	}
	if status.Text != "" {
		t.Errorf("Text = %q with nothing running", status.Text)
	}
}

func TestTheFooterDetailFollowsTheCursorAtTheTargetLevel(t *testing.T) {
	r := run(1, jobs.KindSync, "~/work", jobs.ItemSkipped, jobs.ItemDone)
	r.Items[0].Detail = "already there"
	m := withRuns(t, r)
	m, _ = step(t, m, testutil.Key("right"))

	sel, ok := m.itemTable.Selected()
	if !ok {
		t.Fatal("no target is selected")
	}
	if want := sharedcomponents.DescribeItem(r.Kind, sel.Item).Detail; m.status().Detail != want {
		t.Errorf("Detail = %+v, want the target's %+v", m.status().Detail, want)
	}
}

func TestAWarnKeepsPriorityOverTheDetailAndTheDetailComesBack(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemDone))
	m, _ = step(t, m, testutil.Key("right"))
	m, _ = step(t, m, testutil.Key(".")) // refused: no comparator on the targets
	if !m.footer.IsSet() {
		t.Fatal("the refusal posted nothing")
	}
	if strings.Contains(m.RenderFooter(120), "finished") {
		t.Error("the row detail shows through a Warn")
	}
	m = feed(t, m, sharedcomponents.ClearFooterMsg{ID: m.footer.ID()})
	if !strings.Contains(m.RenderFooter(120), "finished") {
		t.Errorf("the detail did not return once the Warn expired:\n%s", m.RenderFooter(120))
	}
}

func TestTheStateStaysSearchableThroughTheKindColumn(t *testing.T) {
	m := withRuns(t,
		run(1, jobs.KindScan, "~/ok", jobs.ItemDone),
		run(2, jobs.KindScan, "~/bad", jobs.ItemFailed),
	)
	m, _ = step(t, m, testutil.Key("/"))
	for _, msg := range testutil.Type("failed") {
		m, _ = step(t, m, msg)
	}
	if got := len(m.runTable.Visible()); got != 1 || m.runTable.Visible()[0].Run.Label != "~/bad" {
		t.Errorf("/failed leaves %d run(s), want only ~/bad", got)
	}
}

func TestTheTargetStateStaysSearchable(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindSync, "~/work", jobs.ItemSkipped, jobs.ItemDone))
	m, _ = step(t, m, testutil.Key("right"))
	m, _ = step(t, m, testutil.Key("/"))
	for _, msg := range testutil.Type("skipped") {
		m, _ = step(t, m, msg)
	}
	if got := len(m.itemTable.Visible()); got != 1 {
		t.Errorf("/skipped leaves %d target(s), want 1", got)
	}
}

// ── Criterion 5: Rule 116 ────────────────────────────────────────────────────

func TestBothTablesFillTheViewportAt60Columns(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemDone, jobs.ItemRunning))
	m.resize(60, 20)
	if got := m.runTable.RenderedWidth(); got != 58 {
		t.Errorf("runs RenderedWidth = %d, want 58", got)
	}
	m, _ = step(t, m, testutil.Key("right"))
	if got := m.itemTable.RenderedWidth(); got != 58 {
		t.Errorf("targets RenderedWidth = %d, want 58", got)
	}
}

// ── Criterion 6: help legend, shortcuts untouched ────────────────────────────

func TestTheHelpCarriesTheLegendAndNoRetiredGlyph(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemDone))
	want := indicator.Legend(indicator.JobsLegend...)
	var found bool
	for _, s := range m.GetHelpContent().Sections {
		if s.Title == want.Title {
			found = true
			if s.Body != want.Body {
				t.Errorf("the legend section differs from indicator.Legend(JobsLegend...):\n%s", s.Body)
			}
		}
		if s.Title == "Runs" || s.Title == "Targets" {
			for _, old := range []string{theme.IconOK, theme.IconError, theme.IconPending, theme.IconSkipped, theme.IconCanceled, "spinner"} {
				if strings.Contains(s.Body, old) {
					t.Errorf("section %q still mentions %q", s.Title, old)
				}
			}
		}
	}
	if !found {
		t.Errorf("the help has no %q section", want.Title)
	}
	for _, g := range []string{"◉", "⊘", "⊖"} {
		if !strings.Contains(want.Body, g) {
			t.Errorf("the legend lacks %s", g)
		}
	}
}

func TestTheShortcutKeysAreUnchangedAtBothLevels(t *testing.T) {
	want := []string{keymap.Kill, "→", "esc", ".", "/", "?"}
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemDone))
	if got := testutil.ShortcutKeys(m.GetShortcuts()); !slices.Equal(got, want) {
		t.Errorf("keys at runs = %v, want %v", got, want)
	}
	m, _ = step(t, m, testutil.Key("right"))
	if got := testutil.ShortcutKeys(m.GetShortcuts()); !slices.Equal(got, want) {
		t.Errorf("keys at targets = %v, want %v", got, want)
	}
}

// cancelledRun builds a cancelled run the only way possible: through the
// registry, since the flag is unexported.
func cancelledRun(t *testing.T, label string) jobs.Run {
	t.Helper()
	reg := jobs.New()
	id := reg.Start(jobs.NewRun(jobs.KindScan, command.ViewWorkspaces, testContext, label, label+"/a", label+"/b"))
	if !reg.Cancel(id) {
		t.Fatal("Cancel refused")
	}
	snap := reg.Snapshot()
	if len(snap) != 1 || snap[0].State() != jobs.RunCancelled {
		t.Fatalf("fixture is not a cancelled run: %+v", snap)
	}
	return snap[0]
}

func TestACancelledRunShowsItsGlyphAtBothLevels(t *testing.T) {
	m := withRuns(t, cancelledRun(t, "~/stopped"))
	if g := indicator.Glyph(indicator.Cancelled, true); !strings.Contains(m.Render(), g) {
		t.Errorf("the runs table does not show cancelled (%s):\n%s", g, m.Render())
	}
	m, _ = step(t, m, testutil.Key("right"))
	// Targets never started are skipped by the cancellation, not cancelled.
	if g := indicator.Glyph(indicator.Skipped, true); !strings.Contains(m.Render(), g) {
		t.Errorf("the targets table does not show skipped (%s):\n%s", g, m.Render())
	}
}

func TestRunningJobsWithEveryRowFilteredOutKeepTheFooterDot(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemRunning))
	m, _ = step(t, m, testutil.Key("/"))
	for _, msg := range testutil.Type("nothing-matches-this") {
		m, _ = step(t, m, msg)
	}
	status := m.status()
	if !status.Activity || !status.Detail.IsZero() {
		t.Errorf("Activity = %v, Detail zero = %v; want a dot and no detail", status.Activity, status.Detail.IsZero())
	}
	if status.Text != "1 job running" {
		t.Errorf("Text = %q, want the count", status.Text)
	}
}
