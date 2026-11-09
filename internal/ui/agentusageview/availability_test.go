package agentusageview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/agentusage"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/charmbracelet/lipgloss"
)

// Step 3 of DEVDESK-42: Rules 130, 123, 128 and 114 for the two levels.

// openKey is the header entry that opens a session's agents.
const (
	openKey  = "→"
	closeKey = "esc"
	tabKey   = "tab"
)

const wideID = "abcdef12-3456-4789-8abc-def012345678"

// atAgents opens the first session (project "/srv/pa") and answers the parse.
func atAgents(t *testing.T, u agentusage.Usage) Model {
	t.Helper()
	m := withSessions(t, "/p/a.jsonl")
	m, _ = opened(t, m, "right")
	return answer(t, m, u)
}

// agentsLoadingModel is the agents level before the parse answers.
func agentsLoadingModel(t *testing.T) Model {
	t.Helper()
	m, _ := opened(t, withSessions(t, "/p/a.jsonl"), "right")
	return m
}

// footerText is the whole footer, with the message line included.
func footerText(m Model) string { return m.RenderFooter(120) }

// ── Rule 130: same keys, Disabled follows the level ──────────────────────────

func TestAgentsLevelAdvertisesTheSameSevenKeys(t *testing.T) {
	want := []string{"ctrl+r", openKey, closeKey, tabKey, "/", ".", "?"}
	states := map[string]Model{
		"sessions loading": newModel(t),
		"sessions empty":   loaded(t, newModel(t)),
		"sessions loaded":  withSessions(t, "/p/a.jsonl"),
		"agents loading":   agentsLoadingModel(t),
		"agents loaded":    atAgents(t, threeSubs()),
	}
	failed := agentsLoadingModel(t) // a read is in flight, so its error is accepted
	failed = feed(t, failed, usageDeltaMsg{gen: failed.agentGen, err: os.ErrNotExist})
	states["agents error"] = failed
	states["agents filtering"] = feed(t, atAgents(t, threeSubs()), testutil.Key("/"))
	states["agents breakdown"] = feed(t, atAgents(t, threeSubs()), testutil.Key("tab"))

	for name, m := range states {
		got := testutil.ShortcutKeys(m.GetShortcuts())
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: keys %v, want %v", name, got, want)
		}
		// `.` sorts on both Agents and the sessions level; only the Breakdown
		// tab, which has nothing to sort, greys it (see breakdown_tab_test.go).
		if name != "agents breakdown" && !testutil.ShortcutEnabled(m.GetShortcuts(), ".") {
			t.Errorf("%s: `.` sorts at both levels, it must never be greyed outside the Breakdown tab", name)
		}
		for _, sc := range m.GetShortcuts() {
			if sc.Key == "." && sc.Description != "Sort" {
				t.Errorf("%s: `.` description = %q, want %q", name, sc.Description, "Sort")
			}
		}
	}
}

func TestShortcutsAreGreyedByLevelAndByRow(t *testing.T) {
	tests := []struct {
		name         string
		m            Model
		openEnabled  bool
		closeEnabled bool
	}{
		{"sessions with a row", withSessions(t, "/p/a.jsonl"), true, false},
		{"sessions with none", loaded(t, newModel(t)), false, false},
		{"agents loading", agentsLoadingModel(t), false, true},
		{"agents loaded", atAgents(t, threeSubs()), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.m.GetShortcuts()
			for _, k := range []string{openKey, closeKey} {
				if !testutil.HasShortcut(s, k) {
					t.Fatalf("%q must be greyed, never dropped (Rule 130)", k)
				}
			}
			if got := testutil.ShortcutEnabled(s, openKey); got != tt.openEnabled {
				t.Errorf("%s enabled = %v, want %v", openKey, got, tt.openEnabled)
			}
			if got := testutil.ShortcutEnabled(s, closeKey); got != tt.closeEnabled {
				t.Errorf("%s enabled = %v, want %v", closeKey, got, tt.closeEnabled)
			}
			for _, k := range []string{"ctrl+r", "/", ".", "?"} {
				if !testutil.ShortcutEnabled(s, k) {
					t.Errorf("%q always acts, it must not be greyed", k)
				}
			}
		})
	}
}

func TestAFilterMatchingNoSessionGreysOpen(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	m = feed(t, m, testutil.Key("/"))
	for _, msg := range testutil.Type("zzz-no-match") {
		m = feed(t, m, msg)
	}
	if len(m.table.Visible()) != 0 {
		t.Fatalf("fixture error: the filter should match nothing, %d rows left", len(m.table.Visible()))
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), openKey) {
		t.Errorf("%s must be greyed when no row is selectable", openKey)
	}
}

func TestShortcutDescriptionsAtBothLevelsStartWithACapital(t *testing.T) {
	for name, m := range map[string]Model{"sessions": withSessions(t, "/p/a.jsonl"), "agents": atAgents(t, threeSubs())} {
		for _, s := range m.GetShortcuts() {
			if s.Description == "" || s.Description[0] < 'A' || s.Description[0] > 'Z' {
				t.Errorf("%s: %q description %q must start with a capital (Rule 137)", name, s.Key, s.Description)
			}
		}
	}
}

// ── Rule 130: a refusal says why, once, from the same Availability ───────────

func TestOpeningWithNoSessionWarnsAndStays(t *testing.T) {
	m := loaded(t, newModel(t))
	next, cmd := opened(t, m, "right")
	if next.level != levelSessions {
		t.Fatalf("level = %v, want to stay on the sessions", next.level)
	}
	if cmd == nil {
		t.Fatal("the refusal must come with its footer timer")
	}
	if !strings.Contains(footerText(next), reasonNoSession) {
		t.Errorf("footer lacks %q:\n%s", reasonNoSession, footerText(next))
	}
}

func TestOpeningInsideASessionWarnsAndStays(t *testing.T) {
	for _, key := range []string{"right", "enter"} {
		t.Run(key, func(t *testing.T) {
			m := atAgents(t, threeSubs())
			gen := m.agentGen
			next, _ := opened(t, m, key)
			if next.level != levelAgents || next.agentGen != gen {
				t.Errorf("%s inside a session must change nothing (level %v, gen %d->%d)", key, next.level, gen, next.agentGen)
			}
			if !strings.Contains(footerText(next), reasonInsideSession) {
				t.Errorf("footer lacks %q:\n%s", reasonInsideSession, footerText(next))
			}
		})
	}
}

// `.` at the agents level now sorts: the old refusal is replaced by
// TestDotSortsTheAgentsLargestFirst in agents_level_test.go.

func TestDotAtTheSessionsLevelStillCyclesTheSort(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	beforeCol, beforeDesc := m.table.SortState()
	next, _ := opened(t, m, ".")
	if col, desc := next.table.SortState(); col == beforeCol && desc == beforeDesc {
		t.Errorf("`.` must cycle the sessions sort, state stayed (%d, %v)", col, desc)
	}
}

func TestEscAtTheSessionsLevelIsSilent(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	next, cmd := opened(t, m, "esc")
	if next.level != levelSessions {
		t.Fatalf("level = %v", next.level)
	}
	if cmd != nil {
		t.Error("esc at the top level is a silent no-op, with no footer timer")
	}
	if strings.Contains(footerText(next), reasonTopLevel) {
		t.Errorf("footer must not name %q: a stray esc is not a refusal worth a line", reasonTopLevel)
	}
}

func TestEveryReasonIsAWarnNotAnError(t *testing.T) {
	m := loaded(t, newModel(t))
	m, _ = opened(t, m, "right")
	if got := m.footer.View(120, sharedcomponents.Status{}); !strings.Contains(got, reasonNoSession) {
		t.Fatalf("fixture error: %q not in %q", reasonNoSession, got)
	}
	if m.footer.Level() != sharedcomponents.LevelWarning {
		t.Errorf("a refusal is a Warn (Rule 130), got level %v", m.footer.Level())
	}
}

// ── Header (agents level) ────────────────────────────────────────────────────

func headerValue(t *testing.T, m Model, key string) string {
	t.Helper()
	for _, h := range m.GetHeaderInfo("") {
		if h.Key == key {
			return h.Value
		}
	}
	t.Fatalf("no %q header field in %+v", key, m.GetHeaderInfo(""))
	return ""
}

func TestAgentsHeaderCountsAgentsAndTokens(t *testing.T) {
	u := agentusage.Usage{Agents: []agentusage.Agent{
		{Main: true, Models: []string{"model-a"}, Tokens: agentusage.Tokens{Input: 5000, Output: 1000}},
		{ID: "a01", Type: "coder", Tokens: agentusage.Tokens{Output: 400}},
		{ID: "a02", Type: "tester", Tokens: agentusage.Tokens{CacheRead: 300, Output: 100}},
		{ID: "a03", Type: "reviewer", Tokens: agentusage.Tokens{CacheWrite: 21}},
	}}
	m := atAgents(t, u)
	if got := headerValue(t, m, "Agents"); got != "4" {
		t.Errorf("Agents = %q, want 4", got)
	}
	if got := headerValue(t, m, "Tokens"); got != "6.8k" { // 6821, floored
		t.Errorf("Tokens = %q, want 6.8k", got)
	}
	for _, h := range m.GetHeaderInfo("") {
		if h.Key == "Sessions" {
			t.Error("the sessions count belongs to the sessions level")
		}
	}
}

func TestAgentsHeaderFollowsTheFilter(t *testing.T) {
	m := atAgents(t, threeSubs())
	m = feed(t, m, testutil.Key("/"))
	for _, msg := range testutil.Type("coder") {
		m = feed(t, m, msg)
	}
	if got := headerValue(t, m, "Agents"); got != "1" {
		t.Errorf("Agents = %q under the filter `coder`, want 1", got)
	}
}

func TestSessionsHeaderIsUnchangedAtTheTopLevel(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	if got := sessionsValue(t, m); got != "1" {
		t.Errorf("Sessions = %q", got)
	}
}

// ── Title ────────────────────────────────────────────────────────────────────

func TestAgentsTitleNamesTheProjectLeaf(t *testing.T) {
	s := session(wideID, "/srv/work/app", 0)
	s.Parent = "/p/a.jsonl"
	m := loaded(t, newModel(t), s)
	m, _ = opened(t, m, "right")
	if title := m.GetTitle(); !strings.Contains(title, "Agent usage") || !strings.Contains(title, "app") {
		t.Errorf("title = %q, want the view and the project's last segment", title)
	}
}

func TestAgentsTitleFallsBackToTheShortID(t *testing.T) {
	s := session(wideID, "", 0)
	s.Parent = "/p/a.jsonl"
	m := loaded(t, newModel(t), s)
	m, _ = opened(t, m, "right")
	if title := m.GetTitle(); !strings.Contains(title, wideID[:8]) {
		t.Errorf("title = %q, want the 8-char ID when the project is unknown", title)
	}
}

// ── Frame trail (DEVDESK-134), footer height (Rule 124) ──────────────────────

func TestFooterHeightAtTheAgentsLevelIsTheTabRow(t *testing.T) {
	if got := withSessions(t, "/p/a.jsonl").GetFooterHeight(); got != 2 {
		t.Errorf("sessions level footer = %d, want 2", got)
	}
	m := atAgents(t, threeSubs())
	if got := m.GetFooterHeight(); got != 3 {
		t.Errorf("agents level footer = %d, want 3 (tab row, blank, message)", got)
	}
	m = feed(t, m, testutil.Key("/"))
	if got := m.GetFooterHeight(); got != 3+m.agentTable.FilterBar().ExtraHeight() || got != 5 {
		t.Errorf("agents level footer with the filter bar = %d, want 5", got)
	}
}

// trailModel opens a session whose project is $HOME/<rel>.
func trailModel(t *testing.T, rel ...string) Model {
	t.Helper()
	m := newModel(t) // isolates HOME, so read it afterwards
	project := ""
	if len(rel) > 0 {
		project = filepath.Join(append([]string{os.Getenv("HOME")}, rel...)...)
	}
	s := session(wideID, project, 0)
	s.Parent = "/p/a.jsonl"
	m = loaded(t, m, s)
	m, _ = opened(t, m, "right")
	return m
}

func TestFrameTrailReadsSessionsProjectAndShortID(t *testing.T) {
	m := trailModel(t, "w", "app")
	trail := stripANSI(m.FrameTrail(120))
	for _, want := range []string{"Sessions", "~/w/app", wideID[:8], "›"} {
		if !strings.Contains(trail, want) {
			t.Errorf("trail lacks %q: %q", want, trail)
		}
	}
	if strings.Contains(trail, wideID[:9]) {
		t.Errorf("the ID is cut to 8 characters: %q", trail)
	}
	if want := "Sessions › ~/w/app › " + wideID[:8]; trail != want {
		t.Errorf("trail = %q, want %q", trail, want)
	}
}

// The border adds the parentheses and the spacing around the trail; the view's
// text carries none.
func TestFrameTrailHasNoParenthesesNorMargin(t *testing.T) {
	trail := stripANSI(trailModel(t, "w", "app").FrameTrail(120))
	if strings.ContainsAny(trail, "()") {
		t.Errorf("the border adds the parentheses: %q", trail)
	}
	if trail != strings.TrimSpace(trail) {
		t.Errorf("no leading or trailing space: %q", trail)
	}
}

func TestFrameTrailWithNoProjectShowsADash(t *testing.T) {
	trail := stripANSI(trailModel(t).FrameTrail(120))
	if !strings.Contains(trail, "- › "+wideID[:8]) {
		t.Errorf("trail = %q, want a dash for the unknown project", trail)
	}
}

func TestSessionsLevelHasNoFrameTrail(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	if got := m.FrameTrail(120); got != "" {
		t.Errorf("FrameTrail at the sessions level = %q, want empty", got)
	}
	if strings.Contains(footerText(m), "›") {
		t.Errorf("no breadcrumb in the footer at the top level:\n%s", footerText(m))
	}
}

func TestFrameTrailNeverExceedsItsBudget(t *testing.T) {
	for name, m := range map[string]Model{
		"long path": trailModel(t, "projects", "workspace-entire", "anthnel", "app"),
		"no path":   trailModel(t),
	} {
		t.Run(name, func(t *testing.T) {
			for w := 0; w <= 120; w++ {
				if got := lipgloss.Width(m.FrameTrail(w)); got > w {
					t.Errorf("FrameTrail(%d) is %d cells wide", w, got)
				}
			}
		})
	}
}

// fixed is "Sessions › " + " › " + the 8-character ID: 22 cells.
func TestFrameTrailShortensThePathThenDrops(t *testing.T) {
	m := trailModel(t, "projects", "workspace-entire", "anthnel", "app")
	const full = "~/projects/workspace-entire/anthnel/app"
	const fixed = 11 + 3 + 8 // cells, not bytes: "›" is one cell

	if got := stripANSI(m.FrameTrail(120)); !strings.Contains(got, full) {
		t.Errorf("a wide budget keeps the whole path, got %q", got)
	}
	mid := stripANSI(m.FrameTrail(fixed + 12))
	if mid == "" || strings.Contains(mid, full) || !strings.Contains(mid, "app") || !strings.Contains(mid, wideID[:8]) {
		t.Errorf("a medium budget shortens the path but keeps app and the ID, got %q", mid)
	}
	if got := m.FrameTrail(fixed + minTrailProject); got == "" {
		t.Error("the trail still fits with the path at its 8-cell floor")
	}
	if got := m.FrameTrail(fixed + minTrailProject - 1); got != "" {
		t.Errorf("below the floor the whole trail is dropped, got %q", stripANSI(got))
	}
}

func TestFooterOrderIsFilterBarTabRowBlankMessage(t *testing.T) {
	m := feed(t, atAgents(t, threeSubs()), testutil.Key("/"))
	lines := strings.Split(footerText(m), "\n")
	if len(lines) != m.GetFooterHeight() {
		t.Fatalf("footer has %d lines, budget is %d", len(lines), m.GetFooterHeight())
	}
	bar := m.agentTable.FilterBar().ExtraHeight()
	if bar != 2 || len(lines) != 5 {
		t.Fatalf("precondition: a 2-line bar and a 5-line footer, got %d and %d", bar, len(lines))
	}
	for i, l := range lines {
		if strings.Contains(stripANSI(l), "›") {
			t.Errorf("line %d holds a breadcrumb, which now lives on the frame: %q", i, l)
		}
	}
	row := stripANSI(lines[bar])
	if !strings.Contains(row, "Agents") || !strings.Contains(row, "Breakdown") {
		t.Errorf("line %d is the tab row, got %q", bar, row)
	}
	if strings.TrimSpace(stripANSI(lines[bar+1])) != "" {
		t.Errorf("line %d is the blank line (Rule 124), got %q", bar+1, lines[bar+1])
	}
}

// ── Footer: status precedence (Rule 128, H11) ────────────────────────────────

func degraded() agentusage.Usage {
	u := threeSubs()
	u.Diagnostics.UnreadableLines = 2
	return u
}

func TestDegradedUsageWarnsThatTotalsAreApproximate(t *testing.T) {
	m := atAgents(t, degraded())
	st := m.status()
	if st.Level != sharedcomponents.LevelWarning {
		t.Errorf("level = %v, want LevelWarning", st.Level)
	}
	if !strings.Contains(strings.ToLower(st.Text), "approximate") {
		t.Errorf("status = %q, want it to say the totals are approximate", st.Text)
	}
	if !strings.Contains(footerText(m), "approximate") {
		t.Errorf("footer lacks the approximate warning:\n%s", footerText(m))
	}
}

// allPricedYAML prices every model the tests' usages and fixture mention.
const allPricedYAML = `prices:
  model-a: {input: 1, output: 10, cache_write_5m: 2, cache_write_1h: 4, cache_read: 0.1}
  model-b: {input: 2, output: 20, cache_write_5m: 4, cache_write_1h: 8, cache_read: 0.2}
  model-alpha: {input: 1, output: 10, cache_write_5m: 2, cache_write_1h: 4, cache_read: 0.1}
  model-beta: {input: 1, output: 10, cache_write_5m: 2, cache_write_1h: 4, cache_read: 0.1}
  model-gamma: {input: 1, output: 10, cache_write_5m: 2, cache_write_1h: 4, cache_read: 0.1}
`

// withAllPriced gives m a price table covering every model of the fixtures.
// An empty table has its own status, tested in cost_summary_test.go.
func withAllPriced(t *testing.T, m Model) Model {
	t.Helper()
	m.prices = pricesFrom(cfgWith(t, allPricedYAML))
	return m
}

func TestACleanUsageHasNoStatus(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	m = withAllPriced(t, m)
	m, _ = opened(t, m, "right")
	u := agentusage.Usage{Agents: []agentusage.Agent{
		byModel(agentusage.Agent{Main: true}, mu("model-a", agentusage.Tokens{Output: 320}, 0)),
		byModel(agentusage.Agent{ID: "a01", Type: "coder"}, mu("model-b", agentusage.Tokens{Output: 140}, 0)),
	}}
	m = answer(t, m, u)
	st := m.status()
	st.Detail = sharedcomponents.RowDetail{} // the selected row's state is not a status
	if st != (sharedcomponents.Status{}) {
		t.Errorf("status = %+v, want none (the empty-table hint is cost_summary_test.go's)", st)
	}
}

func TestStatusPrecedenceAtTheAgentsLevel(t *testing.T) {
	// loading wins over degraded
	m := atAgents(t, degraded())
	m.agentsLoading = true
	if st := m.status(); !st.Spinner || strings.Contains(st.Text, "approximate") {
		t.Errorf("loading + degraded = %+v, want the spinner line", st)
	}
	// error wins over loading and degraded
	m.agentsErr = true
	if st := m.status(); st.Level != sharedcomponents.LevelError || st.Spinner {
		t.Errorf("error + loading + degraded = %+v, want the error", st)
	}
}

func TestADegradedUsageIsForgottenWhenAnotherSessionOpens(t *testing.T) {
	m := atAgents(t, degraded())
	m, _ = opened(t, feedBack(t, m), "right")
	if strings.Contains(m.status().Text, "approximate") {
		t.Errorf("a new session starts without the previous one's warning: %+v", m.status())
	}
}

// feedBack returns to the sessions level.
func feedBack(t *testing.T, m Model) Model {
	t.Helper()
	next, _ := opened(t, m, "esc")
	if next.level != levelSessions {
		t.Fatalf("fixture error: esc did not leave the agents level")
	}
	return next
}

// ── Rule 114: help ───────────────────────────────────────────────────────────

func TestHelpDocumentsTheAgentsLevel(t *testing.T) {
	c := New(nil).GetHelpContent()
	keys := map[string]bool{}
	for _, b := range c.KeyBindings {
		keys[b.Key] = true // "/" cannot survive the split below
		for _, k := range strings.Split(b.Key, "/") {
			keys[strings.TrimSpace(k)] = true
		}
	}
	for _, k := range []string{"→", "enter", "←", "esc", "ctrl+r", "/", ".", "?"} {
		if !keys[k] {
			t.Errorf("help lacks key %q", k)
		}
	}
	var agents, privacy, columns string
	for _, s := range c.Sections {
		switch s.Title {
		case "Agent columns":
			columns = strings.ToLower(s.Body)
		case "Agents":
			agents = strings.ToLower(s.Body)
		case "Privacy":
			privacy = strings.ToLower(s.Body)
		}
	}
	if agents == "" {
		t.Fatal("help needs a section titled Agents")
	}
	for _, want := range []string{"share", "id", "largest first", "smallest first", "tree"} {
		if !strings.Contains(columns, want) {
			t.Errorf("the Agent columns section should mention %q", want)
		}
	}
	for _, want := range []string{"≥", "orchestrator", "approximate"} {
		if !strings.Contains(agents, want) {
			t.Errorf("the Agents section should mention %q", want)
		}
	}
	for _, want := range []string{"token", "model", "never"} {
		if !strings.Contains(privacy, want) {
			t.Errorf("the Privacy section should now mention %q", want)
		}
	}
	if strings.Contains(privacy, "only the working directory and the file date") {
		t.Error("the Privacy section still claims nothing but the directory and the date is read")
	}
}

// stripANSI drops escape sequences so a position can be asserted on plain text.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// DEVDESK-134: the Agents section says where the breadcrumb went.
func TestHelpDescribesTheBreadcrumbOnTheFrame(t *testing.T) {
	var body string
	for _, s := range New(nil).GetHelpContent().Sections {
		if s.Title == "Agents" {
			body = strings.ToLower(s.Body)
		}
	}
	if body == "" {
		t.Fatal("help has no Agents section")
	}
	if !strings.Contains(body, "breadcrumb") && !strings.Contains(body, "top border") {
		t.Errorf("the Agents section must describe the trail on the frame (breadcrumb / top border):\n%s", body)
	}
	if !strings.Contains(body, "border") && !strings.Contains(body, "frame") {
		t.Errorf("the Agents section must say it sits on the border or frame:\n%s", body)
	}
}
