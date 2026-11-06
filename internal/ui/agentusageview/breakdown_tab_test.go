package agentusageview

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/agentusage"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Step 8 of DEVDESK-98: the Agents / Breakdown tabs, their keys, the footer,
// the header and the help. The panel's own layout is covered by
// breakdown_view_test.go; here only the wiring is under test.

// manySubs is an orchestrator and n sub-agents, every one with tokens, so the
// panel is much taller than the viewport.
func manySubs(n int) agentusage.Usage {
	agents := []agentusage.Agent{bdAgent("", "", true, bdModel("model-a", 100, 50, 30, 0, 1000))}
	for i := range n {
		agents = append(agents, bdAgent(fmt.Sprintf("w%03d", i), "worker", false,
			bdModel("model-b", 10, 5, 3, 0, 100)))
	}
	return agentusage.Usage{Agents: agents}
}

// atBreakdown is the agents level of u on the Breakdown tab.
func atBreakdown(t *testing.T, u agentusage.Usage) Model {
	t.Helper()
	m := feed(t, atAgents(t, u), testutil.Key("tab"))
	if m.tab != tabBreakdown {
		t.Fatalf("precondition: tab = %v, want tabBreakdown", m.tab)
	}
	return m
}

// typed types s into the focused filter and confirms it with enter.
func typed(t *testing.T, m Model, s string) Model {
	t.Helper()
	m = feed(t, m, testutil.Key("/"))
	for _, k := range testutil.Type(s) {
		m = feed(t, m, k)
	}
	return feed(t, m, testutil.Key("enter"))
}

// panelLines is what the panel renders for m, whatever the window shows.
func panelLines(m Model) []string { return renderBreakdown(m.breakdown, m.width-2) }

func viewText(m Model) string { return ansi.Strip(m.View()) }

// refusedWith checks the Warn a refused key leaves, and that nothing else moved.
func refusedWith(t *testing.T, before Model, key, reason string) {
	t.Helper()
	updated, cmd := before.Update(testutil.Key(key))
	after := updated.(Model)
	if cmd == nil {
		t.Errorf("%s: the refusal must come with its footer timer", key)
	}
	if !strings.Contains(footerText(after), reason) {
		t.Errorf("%s: footer lacks %q:\n%s", key, reason, footerText(after))
	}
	if after.footer.Level() != sharedcomponents.LevelWarning {
		t.Errorf("%s: a refusal is a Warn (Rule 130), got level %v", key, after.footer.Level())
	}
	if after.tab != tabBreakdown || after.level != levelAgents {
		t.Errorf("%s: tab/level changed (%v, %v)", key, after.tab, after.level)
	}
	if after.InEditMode() || after.FilterBarVisible() {
		t.Errorf("%s: the filter must not open on the Breakdown tab", key)
	}
	bc, bd := before.agentTable.SortState()
	ac, ad := after.agentTable.SortState()
	if bc != ac || bd != ad {
		t.Errorf("%s: the sort changed (%d,%v) -> (%d,%v)", key, bc, bd, ac, ad)
	}
}

// ── Switching ────────────────────────────────────────────────────────────────

func TestBreakdownTabTabAndShiftTabToggleBetweenTheTwoTabs(t *testing.T) {
	for _, key := range []string{"tab", "shift+tab"} {
		t.Run(key, func(t *testing.T) {
			m := atAgents(t, threeSubs())
			if m.tab != tabAgents {
				t.Fatalf("a session opens on Agents, got %v", m.tab)
			}
			m = feed(t, m, testutil.Key(key))
			if m.tab != tabBreakdown {
				t.Errorf("%s from Agents: tab = %v, want Breakdown", key, m.tab)
			}
			m = feed(t, m, testutil.Key(key))
			if m.tab != tabAgents {
				t.Errorf("%s from Breakdown: tab = %v, want Agents (two tabs: both directions toggle)", key, m.tab)
			}
		})
	}
}

func TestBreakdownTabSwitchTabMethodToggles(t *testing.T) {
	m := atAgents(t, threeSubs())
	next, _ := m.switchTab()
	if next.(Model).tab != tabBreakdown {
		t.Errorf("switchTab from Agents = %v", next.(Model).tab)
	}
	back, _ := next.(Model).switchTab()
	if back.(Model).tab != tabAgents {
		t.Errorf("switchTab from Breakdown = %v", back.(Model).tab)
	}
}

func TestBreakdownTabLeftAndRightDoNotSwitchTabs(t *testing.T) {
	// Rule 135: ← is drill up and → drill down; neither switches tabs.
	m := atAgents(t, threeSubs())
	right := feed(t, m, testutil.Key("right"))
	if right.tab != tabAgents || right.level != levelAgents {
		t.Errorf("→ inside a session changes nothing (tab %v, level %v)", right.tab, right.level)
	}
	left := feed(t, atBreakdown(t, threeSubs()), testutil.Key("left"))
	if left.level != levelSessions {
		t.Errorf("← on Breakdown goes back to the sessions, level = %v", left.level)
	}
}

func TestBreakdownTabAtTheSessionsLevelDoesNothingAndSaysNothing(t *testing.T) {
	for name, m := range map[string]Model{
		"sessions loading": newModel(t),
		"sessions loaded":  withSessions(t, "/p/a.jsonl", "/p/b.jsonl"),
	} {
		for _, key := range []string{"tab", "shift+tab"} {
			t.Run(name+"/"+key, func(t *testing.T) {
				cursor := m.table.Cursor()
				updated, cmd := m.Update(testutil.Key(key))
				next := updated.(Model)
				if cmd != nil {
					t.Error("a structural key at the sessions level is silent: no footer timer")
				}
				if next.tab != tabAgents || next.level != levelSessions {
					t.Errorf("tab/level changed: %v, %v", next.tab, next.level)
				}
				if next.table.Cursor() != cursor {
					t.Error("tab must not move the sessions cursor")
				}
				if strings.Contains(footerText(next), reasonNoTabs) {
					t.Errorf("no message for a greyed control (Rule 130):\n%s", footerText(next))
				}
			})
		}
	}
}

func TestBreakdownTabResetsToAgentsWhenASessionIsOpenedAgain(t *testing.T) {
	// Assumption of the plan: opening and closing reset the tab.
	m := atBreakdown(t, threeSubs())
	m = feed(t, m, testutil.Key("esc"))
	if m.level != levelSessions || m.tab != tabAgents {
		t.Fatalf("esc on Breakdown: level %v, tab %v, want the sessions on Agents", m.level, m.tab)
	}
	m, _ = opened(t, m, "right")
	if m.level != levelAgents || m.tab != tabAgents {
		t.Errorf("reopening: level %v, tab %v, want Agents", m.level, m.tab)
	}
	if !strings.Contains(viewText(m), "State") {
		t.Errorf("reopening shows the agents table:\n%s", viewText(m))
	}
}

// ── Rule 130: seven keys, greyed by state ───────────────────────────────────

func TestBreakdownTabShortcutsAreGreyedPerState(t *testing.T) {
	failed := agentsLoadingModel(t)
	failed = feed(t, failed, usageDeltaMsg{gen: failed.agentGen, err: os.ErrNotExist})
	type want struct{ tab, filter, sort, open, close bool }
	tests := []struct {
		name string
		m    Model
		want want
	}{
		{"sessions loading", newModel(t), want{false, true, true, false, false}},
		{"sessions loaded", withSessions(t, "/p/a.jsonl"), want{false, true, true, true, false}},
		{"agents loading", agentsLoadingModel(t), want{true, true, true, false, true}},
		{"agents loaded", atAgents(t, threeSubs()), want{true, true, true, false, true}},
		{"agents error", failed, want{true, true, true, false, true}},
		{"agents filtering", feed(t, atAgents(t, threeSubs()), testutil.Key("/")), want{true, true, true, false, true}},
		{"breakdown", atBreakdown(t, threeSubs()), want{true, false, false, false, true}},
		{"breakdown loading", feed(t, agentsLoadingModel(t), testutil.Key("tab")), want{true, false, false, false, true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.m.GetShortcuts()
			for k, enabled := range map[string]bool{
				tabKey: tt.want.tab, "/": tt.want.filter, ".": tt.want.sort,
				openKey: tt.want.open, closeKey: tt.want.close, "ctrl+r": true, "?": true,
			} {
				if !testutil.HasShortcut(s, k) {
					t.Errorf("%q must be greyed, never dropped (Rule 130)", k)
					continue
				}
				if got := testutil.ShortcutEnabled(s, k); got != enabled {
					t.Errorf("%q enabled = %v, want %v", k, got, enabled)
				}
			}
		})
	}
}

func TestBreakdownTabShortcutKeysNeverChangeWithinTheAgentsScreen(t *testing.T) {
	agents := atAgents(t, threeSubs())
	want := strings.Join(testutil.ShortcutKeys(agents.GetShortcuts()), ",")
	if want != strings.Join([]string{"ctrl+r", openKey, closeKey, tabKey, "/", ".", "?"}, ",") {
		t.Fatalf("keys = %s, want the seven in order", want)
	}
	for name, m := range map[string]Model{
		"breakdown":        atBreakdown(t, threeSubs()),
		"breakdown scroll": feed(t, atBreakdown(t, manySubs(40)), testutil.Key("down")),
		"breakdown empty":  atBreakdown(t, agentusage.Usage{}),
		"agents filtered":  typed(t, agents, "coder"),
	} {
		if got := strings.Join(testutil.ShortcutKeys(m.GetShortcuts()), ","); got != want {
			t.Errorf("%s: keys %s, want %s", name, got, want)
		}
	}
}

func TestBreakdownTabTabShortcutIsDescribedOnce(t *testing.T) {
	n := 0
	for _, sc := range atAgents(t, threeSubs()).GetShortcuts() {
		if sc.Key == tabKey {
			n++
			if sc.Description != "Switch tab" {
				t.Errorf("tab description = %q, want %q (Rules 137, 138)", sc.Description, "Switch tab")
			}
		}
		if strings.Contains(sc.Key, "shift+tab") {
			t.Errorf("Rule 138: shift+tab is implied by tab, got key %q", sc.Key)
		}
	}
	if n != 1 {
		t.Errorf("tab is listed %d times, want once", n)
	}
}

// ── Refusals on the Breakdown tab ───────────────────────────────────────────

func TestBreakdownTabSlashIsRefusedWithItsReason(t *testing.T) {
	refusedWith(t, atBreakdown(t, threeSubs()), "/", reasonBreakdownNoFilter)
}

func TestBreakdownTabDotIsRefusedWithItsReason(t *testing.T) {
	refusedWith(t, atBreakdown(t, threeSubs()), ".", reasonBreakdownNoSort)
}

func TestBreakdownTabReasonsAreWrittenInEnglishAndDiffer(t *testing.T) {
	for _, r := range []string{reasonNoTabs, reasonBreakdownNoFilter, reasonBreakdownNoSort} {
		if r == "" {
			t.Error("a reason is never empty: an empty reason means available")
		}
	}
	if reasonBreakdownNoFilter == reasonBreakdownNoSort {
		t.Error("the two Breakdown refusals say different things")
	}
}

func TestBreakdownTabCtrlRReadsTheSessionAgain(t *testing.T) {
	m := atBreakdown(t, threeSubs())
	updated, cmd := m.Update(testutil.Key("ctrl+r"))
	next := updated.(Model)
	if cmd == nil || !next.agentsLoading {
		t.Errorf("ctrl+r on Breakdown starts a full read (cmd %v, loading %v)", cmd != nil, next.agentsLoading)
	}
	if next.tab != tabBreakdown {
		t.Error("a refresh does not change the tab")
	}
}

func TestBreakdownTabEnterAndRightAreRefusedLikeOnAgents(t *testing.T) {
	for _, key := range []string{"right", "enter"} {
		m := atBreakdown(t, threeSubs())
		next, _ := opened(t, m, key)
		if next.level != levelAgents || next.agentGen != m.agentGen {
			t.Errorf("%s changed the session", key)
		}
		if !strings.Contains(footerText(next), reasonInsideSession) {
			t.Errorf("%s: footer lacks %q", key, reasonInsideSession)
		}
	}
}

// ── Round trip keeps the Agents state ───────────────────────────────────────

func TestBreakdownTabRoundTripKeepsCursorAndSort(t *testing.T) {
	m := atWideAgents(t, sortUsage(1))
	m = feed(t, m, testutil.Key(".")) // Total, largest first
	m = feed(t, m, testutil.Key("down"))
	key, ok := m.selectedAgentKey()
	if !ok {
		t.Fatal("no selected agent")
	}
	col, desc := m.agentTable.SortState()
	cursor, ids := m.agentTable.Cursor(), visibleIDs(m)

	m = feed(t, feed(t, m, testutil.Key("tab")), testutil.Key("tab"))

	if got, _ := m.selectedAgentKey(); got != key || m.agentTable.Cursor() != cursor {
		t.Errorf("cursor: key %q at %d, want %q at %d", got, m.agentTable.Cursor(), key, cursor)
	}
	if c, d := m.agentTable.SortState(); c != col || d != desc {
		t.Errorf("sort (%d,%v), want (%d,%v)", c, d, col, desc)
	}
	if got := visibleIDs(m); got != ids {
		t.Errorf("row order %s, want %s", got, ids)
	}
}

func TestBreakdownTabRoundTripKeepsTheFilter(t *testing.T) {
	m := typed(t, atWideAgents(t, sortUsage(1)), "coder")
	if len(m.agentTable.Visible()) != 1 {
		t.Fatalf("precondition: one row after the filter, got %d", len(m.agentTable.Visible()))
	}
	m = feed(t, m, testutil.Key("tab"))
	m = feed(t, m, testutil.Key("tab"))
	if q := m.agentTable.FilterBar().SearchQuery(); q != "coder" {
		t.Errorf("filter query = %q, want it kept", q)
	}
	if len(m.agentTable.Visible()) != 1 || !m.FilterBarVisible() {
		t.Errorf("back on Agents the filter shows again (rows %d, bar %v)", len(m.agentTable.Visible()), m.FilterBarVisible())
	}
}

func TestBreakdownTabScrollKeysNeverReachTheAgentsTable(t *testing.T) {
	m := atBreakdown(t, manySubs(40))
	cursor := m.agentTable.Cursor()
	for _, k := range []string{"down", "down", "pgdown", "up", "end", "home", "pgup"} {
		m = feed(t, m, testutil.Key(k))
	}
	if m.agentTable.Cursor() != cursor {
		t.Errorf("the table cursor moved %d -> %d while scrolling the panel", cursor, m.agentTable.Cursor())
	}
}

// ── Scrolling ───────────────────────────────────────────────────────────────

func TestBreakdownTabScrollsAPanelTallerThanTheViewport(t *testing.T) {
	m := atBreakdown(t, manySubs(40)) // 120x20 -> height 19
	if n := len(panelLines(m)); n <= m.height {
		t.Fatalf("precondition: the panel (%d lines) must exceed the viewport (%d)", n, m.height)
	}
	top := viewText(m)
	if !strings.Contains(top, "By category") || m.breakdownTop != 0 {
		t.Fatalf("opens at the top:\n%s", top)
	}
	if strings.Count(top, "worker") >= 40 {
		t.Fatal("the window shows fewer lines than the panel has")
	}
	if got, want := viewHeight(m), viewHeight(atAgents(t, manySubs(40))); got != want {
		t.Errorf("the panel window is %d lines, the table %d: the viewport border would move", got, want)
	}

	m = feed(t, m, testutil.Key("down"))
	if m.breakdownTop != 1 {
		t.Errorf("down: top = %d, want 1", m.breakdownTop)
	}
	if viewText(m) == top {
		t.Error("down must change what is shown")
	}
	m = feed(t, m, testutil.Key("up"))
	if m.breakdownTop != 0 {
		t.Errorf("up: top = %d, want 0", m.breakdownTop)
	}

	m = feed(t, m, testutil.Key("end"))
	limit := len(panelLines(m)) - m.height
	if m.breakdownTop != limit {
		t.Errorf("end: top = %d, want %d (lines - height)", m.breakdownTop, limit)
	}
	bottom := viewText(m)
	if strings.Contains(bottom, "By category") || !strings.Contains(bottom, "Total") {
		t.Errorf("end shows the last lines of the panel:\n%s", bottom)
	}
	m = feed(t, m, testutil.Key("down"))
	if m.breakdownTop != limit || viewText(m) != bottom {
		t.Errorf("down at the bottom must not overscroll (top %d, limit %d)", m.breakdownTop, limit)
	}

	m = feed(t, m, testutil.Key("home"))
	if m.breakdownTop != 0 || viewText(m) != top {
		t.Errorf("home: top = %d, want 0 and the first screen again", m.breakdownTop)
	}
	m = feed(t, m, testutil.Key("pgdown"))
	if m.breakdownTop <= 1 {
		t.Errorf("pgdown moves by a page, top = %d", m.breakdownTop)
	}
	m = feed(t, m, testutil.Key("pgup"))
	if m.breakdownTop != 0 {
		t.Errorf("pgup after pgdown from the top: top = %d, want 0", m.breakdownTop)
	}
	m = feed(t, m, testutil.Key("up"))
	if m.breakdownTop != 0 {
		t.Errorf("up at the top must not go negative, top = %d", m.breakdownTop)
	}
}

func TestBreakdownTabScrollBreakdownMethod(t *testing.T) {
	m := atBreakdown(t, manySubs(40))
	next, _ := m.scrollBreakdown("down")
	if got := next.(Model).breakdownTop; got != 1 {
		t.Errorf("scrollBreakdown(down): top = %d, want 1", got)
	}
	next, _ = next.(Model).scrollBreakdown("end")
	if got := next.(Model).breakdownTop; got != len(panelLines(m))-m.height {
		t.Errorf("scrollBreakdown(end): top = %d", got)
	}
}

func TestBreakdownTabAShortPanelDoesNotScroll(t *testing.T) {
	m := atBreakdown(t, threeSubs())
	m = feed(t, m, testutil.Resize(120, 80))
	m = feed(t, feed(t, m, testutil.Key("end")), testutil.Key("down"))
	if m.breakdownTop != 0 {
		t.Errorf("a panel that fits cannot scroll, top = %d", m.breakdownTop)
	}
}

func TestBreakdownTabAResizeKeepsTheOffsetWithinBounds(t *testing.T) {
	m := feed(t, atBreakdown(t, manySubs(40)), testutil.Key("end"))
	m = feed(t, m, testutil.Resize(120, 200)) // everything fits now
	if out := viewText(m); !strings.Contains(out, "By category") {
		t.Errorf("after a resize that makes the panel fit, it shows from its first line:\n%s", out)
	}
}

// ── Live updates (AC6) ──────────────────────────────────────────────────────

func TestBreakdownTabPanelFollowsALiveUpdate(t *testing.T) {
	m := atBreakdown(t, threeSubs())
	bigger := threeSubs()
	bigger.Agents = append(bigger.Agents, bdAgent("a04", "planner", false, bdModel("model-c", 1, 2, 3, 0, 4000)))
	m = m.withUsage(bigger, false)

	if !reflect.DeepEqual(m.breakdown, buildFor(bigger)) {
		t.Error("the panel is rebuilt from every read")
	}
	if !strings.Contains(viewText(m), "planner") || m.tab != tabBreakdown {
		t.Errorf("the new agent shows in the panel and the tab stays:\n%s", viewText(m))
	}
}

func TestBreakdownTabLiveShrinkClampsTheOffset(t *testing.T) {
	m := feed(t, atBreakdown(t, manySubs(40)), testutil.Key("end"))
	m = m.withUsage(threeSubs(), false)
	if out := viewText(m); !strings.Contains(out, "By category") {
		t.Errorf("a shorter panel is shown from its start, not past its end:\n%s", out)
	}
	m = feed(t, m, testutil.Key("down")) // an Update gives the clamp a chance too
	if limit := max(0, len(panelLines(m))-m.height); m.breakdownTop > limit {
		t.Errorf("top = %d beyond %d", m.breakdownTop, limit)
	}
}

func TestBreakdownTabARealDeltaUpdatesThePanel(t *testing.T) {
	m, parent := atFixture(t)
	m = feed(t, m, testutil.Key("tab"))
	before := viewText(m)

	sub := subDir(parent)
	meta := `{"agentType":"planner","spawnDepth":1,"finished":false}`
	if err := os.WriteFile(sub+"/agent-a00.meta.json", []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	appendLine(t, sub+"/agent-a00.jsonl", usageLine("msg_plan", "model-alpha", 2, 30))

	m, msgs := pollTick(t, m, m.agentGen)
	reads := usageMsgs(msgs)
	if len(reads) != 1 {
		t.Fatalf("want one read, got %d", len(reads))
	}
	m, _ = answered(t, m, reads[0])
	if m.tab != tabBreakdown {
		t.Error("a poll never changes the tab")
	}
	after := viewText(m)
	if after == before || !strings.Contains(after, "planner") {
		t.Errorf("the panel did not take the delta:\n%s", after)
	}
}

// ── The filter does not apply to the panel (AC6) ────────────────────────────

func TestBreakdownTabIgnoresTheAgentsFilter(t *testing.T) {
	u := sortUsage(1)
	m := typed(t, atWideAgents(t, u), "coder")
	m = feed(t, m, testutil.Key("tab"))
	out := viewText(m)
	for _, want := range []string{"orchestrator", "tester", "reviewer", "coder"} {
		if !strings.Contains(out, want) {
			t.Errorf("the panel covers the whole session, %q is missing:\n%s", want, out)
		}
	}
}

// ── Header (Q4, AC4) ────────────────────────────────────────────────────────

func TestBreakdownTabHeaderCoversTheWholeSessionAndAgentsFollowsTheFilter(t *testing.T) {
	m := typed(t, atWideAgents(t, sortUsage(1)), "coder")
	if headerValue(t, m, "Agents") != "1" || headerValue(t, m, "Tokens") != "5.0k" {
		t.Fatalf("Agents under a filter: %s / %s, want 1 / 5.0k", headerValue(t, m, "Agents"), headerValue(t, m, "Tokens"))
	}
	if n := len(m.headerRows()); n != 1 {
		t.Errorf("headerRows on Agents = %d, want the visible rows (1)", n)
	}

	m = feed(t, m, testutil.Key("tab"))
	if n := len(m.headerRows()); n != 4 {
		t.Errorf("headerRows on Breakdown = %d, want every row (4)", n)
	}
	if got := headerValue(t, m, "Agents"); got != "4" {
		t.Errorf("Agents on Breakdown = %q, want 4 (Q4)", got)
	}
	total := m.breakdown.blocks[0].total
	if got, want := headerValue(t, m, "Tokens"), formatTokens(total); got != want {
		t.Errorf("Tokens = %q, want the panel's total %q (AC4)", got, want)
	}
	for _, b := range m.breakdown.blocks {
		if b.total != total {
			t.Errorf("block %q total %d, want %d", b.title, b.total, total)
		}
	}

	m = feed(t, m, testutil.Key("tab"))
	if headerValue(t, m, "Agents") != "1" || headerValue(t, m, "Tokens") != "5.0k" {
		t.Error("back on Agents the header follows the filter again")
	}
}

// Q4 with a price table: Est. cost is the whole session on Breakdown and
// follows the filter on Agents.
func TestBreakdownTabEstCostHeaderIsTheWholeSessionToo(t *testing.T) {
	all := costUsage()
	// Keep only priced agents so that the sum is a figure, not a dash.
	u := agentusage.Usage{Agents: []agentusage.Agent{all.Agents[0], all.Agents[1], all.Agents[2]}}
	m := atPricedAgents(t, cfgWith(t, pricesYAML), 160, u)

	whole := headerValue(t, m, estKey)
	if whole == "-" || whole == "" {
		t.Fatalf("precondition: priced session shows a figure, got %q", whole)
	}

	m = typed(t, m, "coder")
	if n := len(m.agentTable.Visible()); n != 1 {
		t.Fatalf("precondition: the filter keeps 1 row, got %d", n)
	}
	filtered := headerValue(t, m, estKey)
	if filtered == whole {
		t.Fatalf("Agents tab: filtered Est. cost %q must differ from the whole session %q", filtered, whole)
	}
	if want := "~$2.00"; filtered != want {
		t.Errorf("Agents tab, filter coder: Est. cost = %q, want %q", filtered, want)
	}

	m = feed(t, m, testutil.Key("tab"))
	if got := headerValue(t, m, estKey); got != whole {
		t.Errorf("Breakdown tab: Est. cost = %q, want the whole session %q (Q4)", got, whole)
	}

	m = feed(t, m, testutil.Key("tab"))
	if got := headerValue(t, m, estKey); got != filtered {
		t.Errorf("back on Agents: Est. cost = %q, want the filtered %q", got, filtered)
	}
}

// ── Footer (Rules 123, 124, 136, Q1) ────────────────────────────────────────

func TestBreakdownTabFooterHeights(t *testing.T) {
	filtered := typed(t, atAgents(t, threeSubs()), "coder")
	typing := feed(t, atAgents(t, threeSubs()), testutil.Key("/"))
	tests := []struct {
		name string
		m    Model
		want int
	}{
		{"sessions", withSessions(t, "/p/a.jsonl"), 2},
		{"agents", atAgents(t, threeSubs()), 4},
		{"agents with the bar (typing)", typing, 6},
		{"agents with the bar (confirmed)", filtered, 6},
		{"breakdown", atBreakdown(t, threeSubs()), 4},
		{"breakdown, a query set on Agents", feed(t, filtered, testutil.Key("tab")), 4},
		{"agents loading", agentsLoadingModel(t), 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.GetFooterHeight(); got != tt.want {
				t.Errorf("GetFooterHeight = %d, want %d", got, tt.want)
			}
			if got := len(strings.Split(tt.m.RenderFooter(120), "\n")); got != tt.want {
				t.Errorf("RenderFooter has %d lines, want GetFooterHeight() = %d (Rule 124)", got, tt.want)
			}
		})
	}
}

func TestBreakdownTabFooterOrderOnBothTabs(t *testing.T) {
	for name, m := range map[string]Model{"agents": atAgents(t, threeSubs()), "breakdown": atBreakdown(t, threeSubs())} {
		t.Run(name, func(t *testing.T) {
			lines := strings.Split(footerText(m), "\n")
			if !strings.Contains(lines[0], "›") {
				t.Errorf("line 0 is the breadcrumb, got %q", lines[0])
			}
			row := ansi.Strip(lines[1])
			if !strings.Contains(row, "Agents") || !strings.Contains(row, "Breakdown") {
				t.Errorf("line 1 is the tab row, got %q", row)
			}
			if strings.TrimSpace(ansi.Strip(lines[2])) != "" {
				t.Errorf("line 2 is blank (Rule 124), got %q", lines[2])
			}
		})
	}
}

func TestBreakdownTabMessageSitsOnTheLastLine(t *testing.T) {
	m := atBreakdown(t, threeSubs())
	next, _ := m.Update(testutil.Key("/"))
	lines := strings.Split(footerText(next.(Model)), "\n")
	if !strings.Contains(lines[len(lines)-1], reasonBreakdownNoFilter) {
		t.Errorf("the message is the last footer line, got %q", lines[len(lines)-1])
	}
}

func TestBreakdownTabSessionsFooterHasNoTabRow(t *testing.T) {
	if out := footerText(withSessions(t, "/p/a.jsonl")); strings.Contains(out, "Breakdown") {
		t.Errorf("no tab row at the sessions level:\n%s", out)
	}
}

func TestBreakdownTabRowIsPaddedAndMarksTheActiveTab(t *testing.T) {
	testutil.TrueColor(t) // the default test profile emits no styling at all
	agents := atAgents(t, threeSubs())
	breakdownTab := atBreakdown(t, threeSubs())
	for _, m := range []Model{agents, breakdownTab} {
		row := m.tabRow(100)
		if w := lipgloss.Width(row); w != 100 {
			t.Errorf("tab row is %d cells wide, want 100 (Rule 124)", w)
		}
		plainRow := ansi.Strip(row)
		// Rule 123: one cell of padding, then RenderTabs' own label padding.
		if !strings.HasPrefix(plainRow, "  Agents ") {
			t.Errorf("Rule 123: one cell of left padding before the first tab, got %q", plainRow)
		}
	}
	if agents.tabRow(100) == breakdownTab.tabRow(100) {
		t.Error("the active tab must be marked (ColorSecondary), the rows differ between tabs")
	}
	if ansi.Strip(agents.tabRow(100)) != ansi.Strip(breakdownTab.tabRow(100)) {
		t.Error("only the styling changes between tabs, not the text")
	}
}

func TestBreakdownTabFilterBarIsHiddenAndNeverInEditMode(t *testing.T) {
	m := typed(t, atAgents(t, threeSubs()), "coder")
	if !m.FilterBarVisible() {
		t.Fatal("precondition: the confirmed query shows the bar on Agents")
	}
	m = feed(t, m, testutil.Key("tab"))
	if m.FilterBarVisible() || m.InEditMode() {
		t.Errorf("Breakdown: bar visible %v, edit mode %v, want neither", m.FilterBarVisible(), m.InEditMode())
	}
	if out := footerText(m); strings.Contains(out, "coder") {
		t.Errorf("the bar is not drawn on Breakdown:\n%s", out)
	}
}

// ── Panel wiring ────────────────────────────────────────────────────────────

func TestBreakdownTabPanelGetsEveryRowInCanonicalOrderWhateverTheTableDoes(t *testing.T) {
	u := sortUsage(1)
	m := atWideAgents(t, u)
	want := buildFor(u)

	m = feed(t, m, testutil.Key(".")) // sorted by Total: the table is flat and reordered
	m = typed(t, m, "coder")          // and filtered to one row
	if len(m.agentTable.Visible()) != 1 {
		t.Fatalf("precondition: the table shows one row, got %d", len(m.agentTable.Visible()))
	}
	if !reflect.DeepEqual(m.breakdown, want) {
		t.Errorf("the panel must be built from all rows in canonical order, not from the table's:\ngot  %+v\nwant %+v", m.breakdown, want)
	}
	agentBlock := blockByTitle(t, m.breakdown, "By agent")
	if len(agentBlock.lines) != 4 || agentBlock.lines[0].label != orchestratorLabel {
		t.Errorf("By agent holds the 4 agents, orchestrator first: %+v", agentBlock.lines)
	}

	// A later read keeps the property.
	m = m.withUsage(u, false)
	if !reflect.DeepEqual(m.breakdown, want) {
		t.Error("after a read, still all rows in canonical order")
	}
	// The agent block equals the Share column, which a filter does not change.
	rows := agentRows(u, true, statusClock{}, m.prices)
	for i, l := range agentBlock.lines {
		if l.tenths != rows[i].share {
			t.Errorf("line %d (%s): share %d, want the Share column's %d", i, l.label, l.tenths, rows[i].share)
		}
	}
}

// ── Zero tokens and loading (AC5) ───────────────────────────────────────────

func TestBreakdownTabSessionWithoutTokenShowsDashesAndNoNaN(t *testing.T) {
	u := agentusage.Usage{Agents: []agentusage.Agent{
		{Main: true, Models: []string{"model-a"}},
		{ID: "a01", Type: "coder"},
	}}
	m := atBreakdown(t, u)
	out := viewText(m)
	for _, bad := range []string{"NaN", "Inf", "%!", "100.0%"} {
		if strings.Contains(out, bad) {
			t.Errorf("a session with no token must not print %q:\n%s", bad, out)
		}
	}
	for _, want := range []string{"By category", "By model", "By agent", "-"} {
		if !strings.Contains(out, want) {
			t.Errorf("panel lacks %q:\n%s", want, out)
		}
	}
	if got := headerValue(t, m, "Tokens"); got != "0" {
		t.Errorf("Tokens = %q, want 0", got)
	}
}

func TestBreakdownTabBeforeTheFirstReadItIsEmptyNotBroken(t *testing.T) {
	m := feed(t, agentsLoadingModel(t), testutil.Key("tab"))
	if m.tab != tabBreakdown {
		t.Fatalf("tab is available while the read is in flight, got %v", m.tab)
	}
	_ = m.View() // must not panic
	if headerValue(t, m, "Agents") != "0" {
		t.Errorf("Agents = %q while loading", headerValue(t, m, "Agents"))
	}
	if !strings.Contains(footerText(m), "Reading session") {
		t.Errorf("the loading state stays in the footer (Rule 139):\n%s", footerText(m))
	}
}

// ── Help (Rule 114) ─────────────────────────────────────────────────────────

func TestBreakdownTabHelpListsTheKeyAndDocumentsThePanel(t *testing.T) {
	c := New(nil).GetHelpContent()
	found := false
	for _, b := range c.KeyBindings {
		for _, k := range strings.Split(b.Key, "/") {
			if strings.TrimSpace(k) == "tab" {
				found = true
			}
		}
	}
	if !found {
		t.Error("help lacks the tab key")
	}
	var body string
	for _, s := range c.Sections {
		if s.Title == "Breakdown" {
			body = strings.ToLower(s.Body)
		}
	}
	if body == "" {
		t.Fatal("help needs a section titled Breakdown")
	}
	for _, want := range []string{"category", "model", "agent", "unknown", "whole session", "filter", "scroll"} {
		if !strings.Contains(body, want) {
			t.Errorf("the Breakdown section should mention %q", want)
		}
	}
}

// viewHeight is the number of lines View renders.
func viewHeight(m Model) int { return len(strings.Split(m.View(), "\n")) }

// The panel and the table must fill the same number of lines at every
// terminal height, the shortest included, so the viewport border never moves
// when tab switches between them.
func TestBreakdownTabHasTheTableHeightAtEveryTerminalHeight(t *testing.T) {
	for _, h := range []int{1, 2, 3, 4, 10, 20, 60} {
		agents := feed(t, atAgents(t, manySubs(40)), testutil.Resize(120, h))
		panel := feed(t, atBreakdown(t, manySubs(40)), testutil.Resize(120, h))
		if got, want := viewHeight(panel), viewHeight(agents); got != want {
			t.Errorf("terminal height %d: breakdown view is %d lines, agents view %d", h, got, want)
		}
	}
}

// On the shortest terminal the window is taller than m.height; end must still
// reach the last panel line rather than stopping one line short.
func TestBreakdownTabEndShowsTheLastLineAtHeightOne(t *testing.T) {
	m := feed(t, atBreakdown(t, manySubs(40)), testutil.Resize(120, 1))
	lines := panelLines(m)
	if len(lines) <= m.panelHeight() {
		t.Fatalf("precondition: panel (%d lines) must exceed the window (%d)", len(lines), m.panelHeight())
	}
	m = feed(t, m, testutil.Key("end"))
	if want := len(lines) - m.panelHeight(); m.breakdownTop != want {
		t.Errorf("end: top = %d, want %d", m.breakdownTop, want)
	}
	last := strings.TrimSpace(ansi.Strip(lines[len(lines)-1]))
	if got := ansi.Strip(m.View()); !strings.Contains(got, last) {
		t.Errorf("end must show the last panel line %q:\n%s", last, got)
	}
}
