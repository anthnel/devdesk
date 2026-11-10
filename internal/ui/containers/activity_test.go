package containers

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-152 step 2: column 0 of the containers table is the activity column
// (Rule 125). It shows docker's observed state at rest and the running dot while
// a row action (stop, restart, pause, resume, delete) is in flight. Nothing goes
// through the jobs registry; the row spinner is gone.

// runID is the ID ctr builds for "a1".
var runID = "a1" + strings.Repeat("0", 14)

func facts(code int) docker.StatusFacts {
	return docker.StatusFacts{ExitCode: code, HasExitCode: true}
}

func ctr(id, name, state string, f docker.StatusFacts) docker.Container {
	return docker.Container{
		ID: id + strings.Repeat("0", 16-len(id)), Name: name, Image: "busybox",
		State: state, Facts: f,
	}
}

// stateFixtures holds one container per observed state. Names are chosen so none
// is a substring of another or of a header.
func stateFixtures() []docker.Container {
	return []docker.Container{
		ctr("a1", "run1", "running", docker.StatusFacts{}),
		ctr("a2", "rst1", "restarting", facts(1)),
		ctr("a3", "pau1", "paused", docker.StatusFacts{}),
		ctr("a4", "cre1", "created", docker.StatusFacts{}),
		ctr("a5", "ex000", "exited", facts(0)),
		ctr("a6", "ex137", "exited", facts(137)),
		ctr("a7", "dea1", "dead", docker.StatusFacts{}),
	}
}

func activityModel(t *testing.T, width int, cs ...docker.Container) Model {
	t.Helper()
	m := feed(t, New(config.Default()), tea.WindowSizeMsg{Width: width, Height: 40})
	return allStates(t, feed(t, m, ContainersListMsg{Containers: cs}))
}

func selectNamed(t *testing.T, m Model, name string) Model {
	t.Helper()
	// Down to the end of the list, then up: the list is sorted by name, so the
	// target can be on either side of the cursor.
	for _, key := range []string{"down", "up"} {
		for i := 0; i < 20; i++ {
			if c := m.getSelectedContainer(); c != nil && c.Name == name {
				return m
			}
			m = feed(t, m, testutil.Key(key))
		}
	}
	t.Fatalf("could not select %q", name)
	return m
}

func rowGlyphs(s string) bool {
	for _, st := range indicator.States() {
		for _, on := range []bool{true, false} {
			if g := indicator.Glyph(st, on); g != "" && strings.Contains(s, g) {
				return true
			}
		}
	}
	return false
}

func hasSpinnerFrame(s string) bool {
	for _, f := range spinner.Dot.Frames {
		if strings.Contains(s, f) {
			return true
		}
	}
	return false
}

func TestColumnZeroIsTheActivityColumn(t *testing.T) {
	m := activityModel(t, 120, stateFixtures()...)
	shapes := m.containerTable.Shapes()
	d := shapes[0]
	if d.Title != "" || d.Sizing != datatable.SizingFixed || d.MinWidth != datatable.IconColumnWidth {
		t.Errorf("column 0 = %+v, want untitled, SizingFixed, IconColumnWidth", d)
	}
	if !d.Indicator || d.Less || d.Search {
		t.Errorf("column 0 Indicator/Less/Search = %v/%v/%v, want true/false/false", d.Indicator, d.Less, d.Search)
	}
	for i, s := range shapes[1:] {
		if s.Indicator {
			t.Errorf("column %d declares an indicator; only column 0 may", i+1)
		}
	}
	if columnIndicator != 0 || columnName != 1 || columnImage != 2 {
		t.Errorf("column indexes = %d/%d/%d, want 0/1/2", columnIndicator, columnName, columnImage)
	}
}

func TestObservedState(t *testing.T) {
	tests := []struct {
		state string
		f     docker.StatusFacts
		want  indicator.State
	}{
		{"running", docker.StatusFacts{}, indicator.Idle},
		{"restarting", facts(1), indicator.Restarting},
		{"restarting", docker.StatusFacts{}, indicator.Restarting},
		{"paused", docker.StatusFacts{}, indicator.Suspended},
		{"created", docker.StatusFacts{}, indicator.Unknown},
		{"configured", docker.StatusFacts{}, indicator.Unknown},
		{"initialized", docker.StatusFacts{}, indicator.Unknown},
		{"exited", facts(0), indicator.Skipped},
		{"exited", facts(137), indicator.Failed},
		{"exited", facts(1), indicator.Failed},
		{"exited", docker.StatusFacts{}, indicator.Failed}, // a possible crash is never shown as clean
		{"stopped", facts(0), indicator.Skipped},
		{"stopped", facts(2), indicator.Failed},
		{"stopped", docker.StatusFacts{}, indicator.Failed},
		{"dead", docker.StatusFacts{}, indicator.Failed},
		{"dead", facts(0), indicator.Failed},
		{"removing", docker.StatusFacts{}, indicator.Unknown},
		{"stopping", docker.StatusFacts{}, indicator.Unknown},
		{"whatever", docker.StatusFacts{}, indicator.Unknown},
	}
	for _, tc := range tests {
		got := observedState(docker.Container{State: tc.state, Facts: tc.f})
		if got != tc.want {
			t.Errorf("observedState(%s, %+v) = %v, want %v", tc.state, tc.f, got, tc.want)
		}
	}
}

func TestEachObservedStateHasItsOwnGlyphAndColour(t *testing.T) {
	type tc struct {
		name  string
		c     docker.Container
		glyph string
		color lipgloss.Color
	}
	cases := []tc{
		{"restarting", ctr("b1", "x", "restarting", facts(1)), "◕", theme.ColorActivitySuspended},
		{"paused", ctr("b2", "x", "paused", docker.StatusFacts{}), "◐", theme.ColorActivitySuspended},
		{"created", ctr("b3", "x", "created", docker.StatusFacts{}), "◌", theme.ColorActivityNeutral},
		{"exited0", ctr("b4", "x", "exited", facts(0)), "⊖", theme.ColorActivityNeutral},
		{"exited137", ctr("b5", "x", "exited", facts(137)), "⊗", theme.ColorActivityFailed},
		{"dead", ctr("b6", "x", "dead", docker.StatusFacts{}), "⊗", theme.ColorActivityFailed},
	}
	seen := map[string]string{}
	for _, c := range cases {
		s := observedState(c.c)
		if got := indicator.Glyph(s, true); got != c.glyph {
			t.Errorf("%s: glyph %q, want %q", c.name, got, c.glyph)
		}
		if got := indicator.Color(s); got != c.color {
			t.Errorf("%s: colour %v, want %v", c.name, got, c.color)
		}
		for _, busy := range []string{indicator.Glyph(indicator.Running, true), indicator.Glyph(indicator.Running, false), indicator.Glyph(indicator.Done, true)} {
			if c.glyph == busy {
				t.Errorf("%s shares the glyph %q with a running or done state", c.name, busy)
			}
		}
		// dead and exited non-zero share a glyph on purpose; every other pair differs.
		meaning := c.name
		if c.name == "dead" {
			meaning = "exited137"
		}
		if prev, ok := seen[c.glyph]; ok && prev != meaning {
			t.Errorf("%s and %s share %q though they mean different things", prev, c.name, c.glyph)
		}
		seen[c.glyph] = meaning
	}
}

func TestEveryObservedStateIsDrawnInColumnZero(t *testing.T) {
	m := activityModel(t, 120, stateFixtures()...)
	want := map[string]string{
		"rst1":  indicator.Glyph(indicator.Restarting, true),
		"pau1":  indicator.Glyph(indicator.Suspended, true),
		"cre1":  indicator.Glyph(indicator.Unknown, true),
		"ex000": indicator.Glyph(indicator.Skipped, true),
		"ex137": indicator.Glyph(indicator.Failed, true),
		"dea1":  indicator.Glyph(indicator.Failed, true),
	}
	for name, g := range want {
		if row := renderedRowFor(t, m, name); !strings.Contains(row, g) {
			t.Errorf("row %s lacks %q:\n%s", name, g, row)
		}
	}
}

func TestARunningContainerShowsTheContainerIconAndNoDot(t *testing.T) {
	m := activityModel(t, 120, stateFixtures()...)
	row := renderedRowFor(t, m, "run1")
	if !strings.Contains(row, theme.IconContainer) {
		t.Errorf("the running row lacks the container icon:\n%s", row)
	}
	if rowGlyphs(row) {
		t.Errorf("the running row shows an activity glyph at rest:\n%s", row)
	}
	if strings.Contains(row, indicator.Glyph(indicator.Done, true)) {
		t.Error("a running container shows the done glyph")
	}
	if m.WantsBlink() {
		t.Error("WantsBlink with nothing busy")
	}
}

func TestRestIconAndStyle(t *testing.T) {
	for _, state := range []string{"running", "paused", "exited"} {
		c := docker.Container{State: state}
		if got := restIcon(c); got != theme.IconContainer {
			t.Errorf("restIcon(%s) = %q, want theme.IconContainer", state, got)
		}
		if got := restStyle(c).GetForeground(); got != theme.IconColor(theme.IconRoleContainer) {
			t.Errorf("restStyle(%s) foreground = %v, want the container role colour", state, got)
		}
	}
}

func TestActivityTablesNamesTheContainersTable(t *testing.T) {
	m := activityModel(t, 120, stateFixtures()...)
	tables := m.ActivityTables()
	if len(tables) != 1 || tables["containers"] == nil {
		t.Fatalf("ActivityTables() = %v, want exactly \"containers\"", tables)
	}
}

// The selected row is red only for the Failed states (Q4-A).
func TestSelectedRowIsRedOnlyForFailedContainers(t *testing.T) {
	withTrueColor(t)
	errSel := theme.TableStylesForState("error").Selected.Render("x")
	normSel := theme.TableStylesForState("normal").Selected.Render("x")
	for _, tc := range []struct {
		name string
		c    docker.Container
		red  bool
	}{
		{"exited 0", ctr("c1", "x", "exited", facts(0)), false},
		{"exited 137", ctr("c2", "x", "exited", facts(137)), true},
		{"exited unknown code", ctr("c3", "x", "exited", docker.StatusFacts{}), true},
		{"dead", ctr("c4", "x", "dead", docker.StatusFacts{}), true},
		{"stopped 0", ctr("c5", "x", "stopped", facts(0)), false},
		{"running", ctr("c6", "x", "running", docker.StatusFacts{}), false},
		{"paused", ctr("c7", "x", "paused", docker.StatusFacts{}), false},
		{"restarting", ctr("c8", "x", "restarting", facts(1)), false},
	} {
		got := containerSelectedStyles(tc.c).Selected.Render("x")
		want := normSel
		if tc.red {
			want = errSel
		}
		if got != want {
			t.Errorf("%s: selected style = %q, want the %s one", tc.name, got, map[bool]string{true: "error", false: "normal"}[tc.red])
		}
	}
}

// ── Actions: the dot, the blink, the return ─────────────────────────────────

func busyVia(t *testing.T, how string) Model {
	t.Helper()
	m := selectNamed(t, activityModel(t, 120, stateFixtures()...), "run1")
	switch how {
	case "stop":
		m, _ = pressK(t, m, choiceStop)
	case "restart":
		m, _ = pressK(t, m, choiceRestart)
	case "pause":
		m, _ = step(t, m, testutil.Key(" "))
	case "remove":
		m = feed(t, m, testutil.Key(keymap.Delete))
		m, _ = step(t, m, sharedcomponents.ConfirmModalYesMsg{})
	}
	if !m.containerTable.IsBusy(runID) {
		t.Fatalf("%s did not mark the row busy", how)
	}
	return m
}

func TestAnActionShowsTheRunningDotThatBlinks(t *testing.T) {
	on, off := indicator.Glyph(indicator.Running, true), indicator.Glyph(indicator.Running, false)
	for _, how := range []string{"stop", "restart", "pause", "remove"} {
		t.Run(how, func(t *testing.T) {
			m := busyVia(t, how)
			row := renderedRowFor(t, m, "run1")
			if !strings.Contains(row, on) {
				t.Fatalf("busy row lacks %q:\n%s", on, row)
			}
			if hasSpinnerFrame(row) {
				t.Errorf("busy row still shows a spinner frame:\n%s", row)
			}
			if !m.WantsBlink() {
				t.Error("WantsBlink is false while an action runs")
			}
			before := append([]docker.Container(nil), m.containerTable.Items()...)

			m = feed(t, m, jobs.FrameMsg{Blink: false})
			if !strings.Contains(renderedRowFor(t, m, "run1"), off) {
				t.Errorf("off phase lacks %q", off)
			}
			if !reflect.DeepEqual(before, m.containerTable.Items()) {
				t.Error("a blink frame rebuilt the items")
			}
			if !m.containerTable.IsBusy(runID) {
				t.Error("a blink frame cleared the busy marker")
			}

			m = feed(t, m, jobs.FrameMsg{Blink: true})
			if !strings.Contains(renderedRowFor(t, m, "run1"), on) {
				t.Errorf("on phase lacks %q", on)
			}
			// Another row is untouched.
			if strings.Contains(renderedRowFor(t, m, "pau1"), on) {
				t.Error("another row shows the running dot")
			}
		})
	}
}

func TestTheFrameMsgDoesNotTouchTheFooterSpinner(t *testing.T) {
	m := activityModel(t, 120, stateFixtures()...)
	m2, cmd := step(t, m, jobs.FrameMsg{Frame: "X", RenderedFrame: "X", Blink: false})
	if cmd != nil {
		t.Error("a FrameMsg returned a command")
	}
	if m2.containerTable.BlinkPhase() {
		t.Error("SetBlinkPhase(false) was not applied")
	}
}

func TestTheRowReturnsToItsObservedStateAfterTheAction(t *testing.T) {
	m := busyVia(t, "stop")
	m = feed(t, m, ContainerActionMsg{Action: "stop", ID: runID, Name: "run1"})
	if m.containerTable.IsBusy(runID) {
		t.Fatal("the marker survived a successful action")
	}
	if m.WantsBlink() {
		t.Error("WantsBlink after the action completed")
	}
	row := renderedRowFor(t, m, "run1")
	if rowGlyphs(row) || !strings.Contains(row, theme.IconContainer) {
		t.Errorf("the row does not show its last observed state until the next poll:\n%s", row)
	}

	next := ctr("a1", "run1", "exited", facts(0))
	m = feed(t, m, ContainersListMsg{Containers: []docker.Container{next}})
	if !strings.Contains(renderedRowFor(t, m, "run1"), indicator.Glyph(indicator.Skipped, true)) {
		t.Error("the next poll did not show the new observed state")
	}
}

func TestAFailedActionStaysAFooterError(t *testing.T) {
	m := busyVia(t, "stop")
	m, _ = step(t, m, ContainerActionMsg{Action: "stop", ID: runID, Name: "run1", Err: errors.New("boom")})
	if m.containerTable.IsBusy(runID) || m.WantsBlink() {
		t.Error("a failed action left the row busy")
	}
	if m.footer.Text() != "Action failed — check logs" {
		t.Errorf("footer = %q", m.footer.Text())
	}
	row := renderedRowFor(t, m, "run1")
	if strings.Contains(row, indicator.Glyph(indicator.Failed, true)) {
		t.Errorf("a failed action marks the row as failed (D5):\n%s", row)
	}
}

func TestARefreshKeepsTheDotOfABusyRow(t *testing.T) {
	m := busyVia(t, "stop")
	m = feed(t, m, ContainersListMsg{Containers: stateFixtures()})
	if !strings.Contains(renderedRowFor(t, m, "run1"), indicator.Glyph(indicator.Running, true)) {
		t.Error("a refresh dropped the dot")
	}
}

// ── No blink without an action (criterion 6) ───────────────────────────────

func TestNothingBlinksWithoutAnAction(t *testing.T) {
	m := activityModel(t, 120, stateFixtures()...)
	// The commands of these messages are not run: some sleep, some call the engine.
	for _, msg := range []tea.Msg{
		RefreshTickMsg{},
		ContainerMetricsMsg{Metrics: map[string]docker.Container{}},
		ContainersListMsg{Containers: stateFixtures()},
	} {
		m = feed(t, m, msg)
		if m.WantsBlink() {
			t.Errorf("WantsBlink after %T", msg)
		}
	}
	on := renderedRowFor(t, feed(t, m, jobs.FrameMsg{Blink: true}), "rst1")
	off := renderedRowFor(t, feed(t, m, jobs.FrameMsg{Blink: false}), "rst1")
	if on != off {
		t.Errorf("a restarting row changes with the blink phase:\n%q\n%q", on, off)
	}
	if !strings.Contains(on, indicator.Glyph(indicator.Restarting, true)) {
		t.Error("the restarting row lost its glyph")
	}
}

// ── The row spinner is gone ────────────────────────────────────────────────

func TestAnActionSchedulesNoSpinnerTick(t *testing.T) {
	m := selectNamed(t, activityModel(t, 120, stateFixtures()...), "run1")
	c := docker.Container{ID: webID, Name: "web"}
	_, cmd := m.startAction(&c, "Stopping", func() tea.Msg { return nil })
	if _, ok := testutil.MsgOf[spinner.TickMsg](cmd); ok {
		t.Error("startAction batches a spinner tick: the row animation is the blink now")
	}
}

func TestTheSpinnerStopsWhileAnActionRunsAndNothingLoads(t *testing.T) {
	m := busyVia(t, "stop")
	m.loading = false
	if _, cmd := step(t, m, spinner.TickMsg{}); cmd != nil {
		t.Error("the spinner chain keeps going for a busy row")
	}
}

// ── Filters unchanged (criterion 5) ────────────────────────────────────────

func TestStateFilterKeysKeepTheirRows(t *testing.T) {
	fresh := func() Model {
		m := feed(t, New(config.Default()), tea.WindowSizeMsg{Width: 120, Height: 40})
		return feed(t, m, ContainersListMsg{Containers: stateFixtures()})
	}
	names := func(m Model) []string {
		out := rowNames(tableRows(m))
		sort.Strings(out)
		return out
	}
	for _, tc := range []struct {
		keys []string
		want []string
	}{
		{nil, []string{"run1"}},
		{[]string{"r"}, []string{"run1"}},
		{[]string{"p"}, []string{"pau1"}},
		{[]string{"s"}, []string{"dea1", "ex000", "ex137"}},
		{[]string{"t"}, []string{"cre1", "rst1"}},
		{[]string{"p", "s", "z"}, []string{"run1"}},
	} {
		m := fresh()
		for _, k := range tc.keys {
			m = feed(t, m, testutil.Key(k))
		}
		if got := names(m); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("keys %v show %v, want %v", tc.keys, got, tc.want)
		}
	}
}

// ── Rule 116 ───────────────────────────────────────────────────────────────

func TestTheActivityColumnKeepsTheTableOnTheViewportAtEveryWidth(t *testing.T) {
	for _, w := range []int{40, 60, 80, 120, 200} {
		m := activityModel(t, w, stateFixtures()...)
		if got, want := m.containerTable.RenderedWidth(), w-2; got != want {
			t.Errorf("width %d: line spans %d, want %d", w, got, want)
		}
		if !strings.Contains(renderedRowFor(t, m, "run1"), theme.IconContainer) {
			t.Errorf("width %d: the activity column was dropped", w)
		}
	}
	// At 60 the gauges still go first, as before.
	m := activityModel(t, 60, stateFixtures()...)
	header := strings.Split(m.containerTable.View(), "\n")[0]
	if strings.Contains(header, "1 core") || strings.Contains(header, "Limit") {
		t.Errorf("gauges survive at 60 columns:\n%s", header)
	}
}
