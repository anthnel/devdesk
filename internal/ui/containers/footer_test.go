package containers

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-152 step 3: the footer details the selected row's state in words
// (ux.md section 1.2) and the help carries the shared legend.

func aged(code int, hasCode bool, d time.Duration) docker.StatusFacts {
	return docker.StatusFacts{ExitCode: code, HasExitCode: hasCode, Age: d, HasAge: true}
}

func footerFixtures() []docker.Container {
	return []docker.Container{
		ctr("a1", "run1", "running", aged(0, false, 3*time.Hour)),
		ctr("a2", "rst1", "restarting", facts(1)),
		ctr("a3", "pau1", "paused", docker.StatusFacts{}),
		ctr("a4", "cre1", "created", docker.StatusFacts{}),
		ctr("a5", "ex000", "exited", aged(0, true, 5*time.Minute)),
		ctr("a6", "ex137", "exited", aged(137, true, 5*time.Minute)),
		ctr("a7", "dea1", "dead", docker.StatusFacts{}),
		ctr("a8", "exnc", "exited", docker.StatusFacts{}),
		ctr("a9", "rem1", "removing", docker.StatusFacts{}),
		ctr("b1", "run2", "running", docker.StatusFacts{}),
	}
}

func footerModel(t *testing.T, width int) Model {
	t.Helper()
	return activityModel(t, width, footerFixtures()...)
}

func footerText(m Model, width int) string { return ansi.Strip(m.RenderFooter(width)) }

func TestFooterDetailsEachObservedState(t *testing.T) {
	g := func(s indicator.State) string { return indicator.Glyph(s, true) }
	cases := []struct {
		name, want string
	}{
		{"rst1", g(indicator.Restarting) + " Restarting — last exit code 1"},
		{"pau1", g(indicator.Suspended) + " Paused"},
		{"cre1", g(indicator.Unknown) + " Created, never started"},
		{"ex137", g(indicator.Failed) + " Exited with code 137 — 5 min ago"},
		{"ex000", g(indicator.Skipped) + " Exited cleanly (code 0) — 5 min ago"},
		{"dea1", g(indicator.Failed) + " Dead — the engine could not stop it cleanly"},
		{"exnc", g(indicator.Failed) + " Exited (exit code unknown)"},
		{"rem1", g(indicator.Unknown) + " Removing"},
	}
	for _, width := range []int{120, 60} {
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				m := selectNamed(t, footerModel(t, width), c.name)
				got := footerText(m, width)
				if !strings.Contains(got, c.want) {
					t.Errorf("width %d: footer = %q, want it to contain %q", width, got, c.want)
				}
				if h, n := m.GetFooterHeight(), strings.Count(m.RenderFooter(width), "\n")+1; h != n {
					t.Errorf("width %d: GetFooterHeight() = %d, RenderFooter emits %d lines", width, h, n)
				}
			})
		}
	}
}

func TestARunningContainerFooterHasNoGlyphPrefix(t *testing.T) {
	m := selectNamed(t, footerModel(t, 120), "run1")
	got := footerText(m, 120)
	if !strings.Contains(got, "Running since 3 hr ago") {
		t.Fatalf("footer = %q, want \"Running since 3 hr ago\"", got)
	}
	if i := strings.Index(got, "Running since"); rowGlyphs(got[:i]) {
		t.Errorf("a glyph precedes the running sentence: %q", got)
	}
	m = selectNamed(t, footerModel(t, 120), "run2")
	got = footerText(m, 120)
	if !strings.Contains(got, "Running") || strings.Contains(got, "Running since") {
		t.Errorf("no known age: footer = %q, want a bare \"Running\"", got)
	}
}

func TestTheFooterOfABusySelectedRowNamesTheActionOnce(t *testing.T) {
	m := busyVia(t, "stop") // run1 selected and busy
	got := footerText(m, 120)
	if want := indicator.Glyph(indicator.Running, true) + " Stopping run1"; !strings.Contains(got, want) {
		t.Errorf("footer = %q, want %q", got, want)
	}
	if strings.Contains(got, "Stopping run1…") {
		t.Errorf("the progress repeats the selected row's own detail: %q", got)
	}
	if m.progressLine() != "" {
		t.Errorf("progressLine = %q for the selected busy row, want empty", m.progressLine())
	}
}

func TestTheFooterOfAnIdleRowAddsTheProgressOfABusyOne(t *testing.T) {
	m := feed(t, busyVia(t, "stop"), testutil.Key("up")) // run1 (last by name) stays busy, cursor moves
	if c := m.getSelectedContainer(); c == nil || c.Name == "run1" {
		t.Fatal("the cursor did not leave run1")
	}
	got := footerText(m, 120)
	if !strings.Contains(got, " · Stopping run1…") {
		t.Errorf("footer = %q, want the progress after the row's sentence", got)
	}
	if m.progressLine() != "Stopping run1…" {
		t.Errorf("progressLine = %q, want \"Stopping run1…\"", m.progressLine())
	}
}

func TestTwoBusyRowsAndPruningAreProgress(t *testing.T) {
	m := busyVia(t, "stop")
	m = selectNamed(t, m, "pau1")
	m, _ = step(t, m, testutil.Key(" ")) // resume
	if len(m.containerTable.BusyLabels()) != 2 {
		t.Fatalf("expected 2 busy rows, got %v", m.containerTable.BusyLabels())
	}
	got := footerText(m, 120)
	if !strings.Contains(got, "2 actions running…") || !strings.Contains(got, indicator.Glyph(indicator.Running, true)) {
		t.Errorf("footer = %q, want the selected row's detail and \"2 actions running…\"", got)
	}

	p := selectNamed(t, footerModel(t, 120), "cre1")
	p.pruning = true
	if p.progressLine() != "Pruning containers…" {
		t.Errorf("progressLine = %q while pruning", p.progressLine())
	}
	if got := footerText(p, 120); !strings.Contains(got, "Created, never started") || !strings.Contains(got, "Pruning containers…") {
		t.Errorf("footer = %q, want the row sentence then the prune progress", got)
	}
}

func TestTheFooterLineNeverOutgrowsItsWidth(t *testing.T) {
	const width = 30
	m := feed(t, busyVia(t, "stop"), testutil.Key("up")) // idle row detail + progress
	m = selectNamed(t, m, "ex137")
	lines := strings.Split(m.RenderFooter(width), "\n")
	if line := lines[len(lines)-1]; lipgloss.Width(line) > width { // the filter bar, if any, is above
		t.Errorf("footer line is %d columns wide, want <= %d: %q", lipgloss.Width(line), width, ansi.Strip(line))
	}
	if got := footerText(m, width); !strings.Contains(got, "Stopping run1") {
		t.Errorf("footer = %q, want the progress to survive (truncated or not)", got)
	}
}

func TestLoadingStillOwnsTheFooterWhenThereAreNoRows(t *testing.T) {
	m := New(config.Default())
	if got := footerText(m, 120); !strings.Contains(got, "Loading containers...") {
		t.Errorf("footer = %q, want the loading line", got)
	}
}

func TestAnActionFailureWinsTheFooterThenTheRowSentenceReturns(t *testing.T) {
	m := busyVia(t, "stop")
	m, _ = step(t, m, ContainerActionMsg{Action: "stop", ID: runID, Name: "run1", Err: errors.New("boom")})
	if got := footerText(m, 120); !strings.Contains(got, "Action failed — check logs") {
		t.Fatalf("footer = %q, want the failure", got)
	}
	m = feed(t, m, sharedcomponents.ClearFooterMsg{ID: m.footer.ID()})
	got := footerText(m, 120)
	if strings.Contains(got, "Action failed") || !strings.Contains(got, "Running") {
		t.Errorf("after the timer the footer = %q, want the row's sentence", got)
	}
}

func TestShortcutKeysDoNotMoveWithTheSelectedRowOrAnAction(t *testing.T) {
	want := testutil.ShortcutKeys(selectNamed(t, footerModel(t, 120), "run1").GetShortcuts())
	states := map[string]Model{
		"busy":   busyVia(t, "stop"),
		"exited": selectNamed(t, footerModel(t, 120), "ex137"),
		"paused": selectNamed(t, footerModel(t, 120), "pau1"),
	}
	for name, m := range states {
		got := testutil.ShortcutKeys(m.GetShortcuts())
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: shortcut keys = %v, want %v", name, got, want)
		}
	}
}

func TestObservedTextSentences(t *testing.T) {
	cases := []struct {
		c    docker.Container
		want string
	}{
		{ctr("a1", "x", "running", aged(0, false, 3*time.Hour)), "Running since 3 hr ago"},
		{ctr("a1", "x", "running", docker.StatusFacts{}), "Running"},
		{ctr("a1", "x", "running", aged(0, false, 20*time.Second)), "Running for less than a minute"},
		{ctr("a5", "x", "exited", aged(1, true, 10*time.Second)), "Exited with code 1 — just now"},
		{ctr("a6", "x", "exited", aged(0, true, 10*time.Second)), "Exited cleanly (code 0) — just now"},
		{ctr("a2", "x", "restarting", facts(1)), "Restarting — last exit code 1"},
		{ctr("a2", "x", "restarting", docker.StatusFacts{}), "Restarting"},
		{ctr("a3", "x", "paused", docker.StatusFacts{}), "Paused"},
		{ctr("a4", "x", "created", docker.StatusFacts{}), "Created, never started"},
		{ctr("a5", "x", "exited", aged(137, true, 5*time.Minute)), "Exited with code 137 — 5 min ago"},
		{ctr("a5", "x", "exited", facts(137)), "Exited with code 137"},
		{ctr("a6", "x", "exited", aged(0, true, 5*time.Minute)), "Exited cleanly (code 0) — 5 min ago"},
		{ctr("a7", "x", "exited", docker.StatusFacts{}), "Exited (exit code unknown)"},
		{ctr("a8", "x", "dead", docker.StatusFacts{}), "Dead — the engine could not stop it cleanly"},
		{ctr("a9", "x", "removing", docker.StatusFacts{}), "Removing"},
	}
	for _, c := range cases {
		if got := observedText(c.c); got != c.want {
			t.Errorf("observedText(%s, %+v) = %q, want %q", c.c.State, c.c.Facts, got, c.want)
		}
	}
}

// ── Help ───────────────────────────────────────────────────────────────────

func helpSection(t *testing.T, c help.Content, title string) []help.Section {
	t.Helper()
	var out []help.Section
	for _, s := range c.Sections {
		if s.Title == title {
			out = append(out, s)
		}
	}
	return out
}

func TestHelpCarriesTheSharedActivityLegend(t *testing.T) {
	c := footerModel(t, 120).GetHelpContent()
	want := indicator.Legend(indicator.ContainersLegend...)
	got := helpSection(t, c, want.Title)
	if len(got) != 1 {
		t.Fatalf("%d sections titled %q, want 1", len(got), want.Title)
	}
	if got[0].Body != want.Body {
		t.Errorf("legend body differs from indicator.Legend(ContainersLegend...):\n%s", got[0].Body)
	}
	for _, s := range []indicator.State{indicator.Restarting, indicator.Suspended, indicator.Unknown, indicator.Skipped, indicator.Failed} {
		if !strings.Contains(got[0].Body, indicator.Glyph(s, true)) {
			t.Errorf("legend lacks %q", indicator.Glyph(s, true))
		}
	}
	if strings.Contains(got[0].Body, indicator.Glyph(indicator.Done, true)) {
		t.Error("legend shows the done glyph: a running container shows its usual icon")
	}
	if strings.Contains(got[0].Body, "kill") {
		t.Error("legend names kill")
	}
}

func TestHelpColumnsNoLongerListTheOldIconsOrASpinner(t *testing.T) {
	c := footerModel(t, 120).GetHelpContent()
	cols := helpSection(t, c, "Columns")
	if len(cols) != 1 {
		t.Fatalf("%d Columns sections", len(cols))
	}
	for _, ic := range []string{theme.IconCaretRight, theme.IconSmallPause, theme.IconSmallSquare, theme.IconCaretUp, theme.IconBan} {
		if strings.Contains(cols[0].Body, ic) {
			t.Errorf("Columns still names the old state icon %q", ic)
		}
	}
	for _, s := range c.Sections {
		if strings.Contains(strings.ToLower(s.Body), "spinner") {
			t.Errorf("section %q still mentions a spinner", s.Title)
		}
	}
	run := helpSection(t, c, "Running Actions")
	if len(run) != 1 {
		t.Fatalf("%d Running Actions sections", len(run))
	}
	for _, w := range []string{indicator.Glyph(indicator.Running, true), "next refresh"} {
		if !strings.Contains(run[0].Body, w) {
			t.Errorf("Running Actions lacks %q:\n%s", w, run[0].Body)
		}
	}
}
