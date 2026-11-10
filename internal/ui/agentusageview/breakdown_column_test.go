package agentusageview

import (
	"fmt"
	"image/color"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/agentusage/pricing"
	"github.com/anthnel/devdesk/internal/testutil/colorprofile"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

const breakdownTitle = "Breakdown"

// allAgentTitles is every title the agents header can carry, in column order.
var allAgentTitles = []string{"Agent", "ID", "Est. cost", "Tools", "Duration", "State", "Model", breakdownTitle, "In", "Out", "Cache read", "Cache write", "Total", "Share"}

// shownTitles reads the titles of the rendered header row, Breakdown included
// (the shared splitTitles predates the column and does not know it).
func shownTitles(t *testing.T, view string) []string {
	t.Helper()
	for _, line := range strings.Split(stripANSI(view), "\n") {
		line = strings.NewReplacer("│", " ", "▲", " ", "▼", " ").Replace(line)
		if !strings.Contains(line, "Agent") || !strings.Contains(line, "Total") {
			continue
		}
		var got []string
		cells := strings.Split(line, "  ")
		for _, title := range allAgentTitles {
			for _, c := range cells {
				if strings.TrimSpace(c) == title {
					got = append(got, title)
				}
			}
		}
		return got
	}
	t.Fatalf("no header row in:\n%s", view)
	return nil
}

// mixAgent is an agent whose four categories apportion to 1/3/1/5 cells.
func mixAgent() agentusage.Agent {
	return agentusage.Agent{
		Main: true, Models: []string{"model-a"},
		Tokens: agentusage.Tokens{Input: 100, Output: 300, CacheWrite: 100, CacheRead: 500},
	}
}

func rowOf(t *testing.T, a agentusage.Agent) agentRow {
	t.Helper()
	rows := agentRows(agentusage.Usage{Agents: []agentusage.Agent{a}}, true, statusClock{}, pricing.Table{})
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	return rows[0]
}

func TestBreakdownColumnLayout(t *testing.T) {
	cols := agentColumns()
	idx := slices.IndexFunc(cols, func(c datatable.Column[agentRow]) bool { return c.Title == breakdownTitle })
	if idx < 0 {
		t.Fatalf("agentColumns has no %q column", breakdownTitle)
	}
	if cols[idx-1].Title != "Model" || cols[idx+1].Title != "In" {
		t.Errorf("%s sits between %q and %q, want between Model and In", breakdownTitle, cols[idx-1].Title, cols[idx+1].Title)
	}
	c := cols[idx]
	if !c.Optional || c.DropFirst {
		t.Errorf("Optional=%v DropFirst=%v, want Optional and not DropFirst", c.Optional, c.DropFirst)
	}
	if c.Sizing != datatable.SizingFixed || c.MinWidth != 10 || breakdownBarWidth != 10 {
		t.Errorf("Sizing=%v MinWidth=%d, want SizingFixed and 10", c.Sizing, c.MinWidth)
	}
	if c.Less != nil || c.Search != nil {
		t.Errorf("Less set=%v Search set=%v, want neither", c.Less != nil, c.Search != nil)
	}
	if c.Spans == nil || c.Cell == nil || c.Style == nil {
		t.Errorf("Spans/Cell/Style set = %v/%v/%v, want all", c.Spans != nil, c.Cell != nil, c.Style != nil)
	}
	if c.Cut != nil || c.TailStyle != nil {
		t.Error("Spans takes precedence over Cut/TailStyle; the column must not declare them")
	}
}

func TestBreakdownCellAndSpansForATypicalAgent(t *testing.T) {
	c := column(t, breakdownTitle)
	r := rowOf(t, mixAgent())
	if got, want := c.Cell(r), theme.GaugeFill(10); got != want || strings.Contains(got, "\x1b") || len([]rune(got)) != 10 {
		t.Errorf("Cell = %q, want ten plain gauge glyphs %q", got, want)
	}
	want, ok := segmentCells(r.Tokens)
	if !ok {
		t.Fatal("precondition: the fixture agent has tokens")
	}
	if want != [segCount]int{1, 3, 1, 5} {
		t.Fatalf("precondition: segmentCells = %v, want [1 3 1 5]", want)
	}
	spans := c.Spans(r)
	if len(spans) != segCount {
		t.Fatalf("got %d spans, want %d", len(spans), segCount)
	}
	roles := theme.TokenRoles() // Input, Output, Cache write, Cache read
	if len(roles) != segCount {
		t.Fatalf("theme has %d token roles, want %d", len(roles), segCount)
	}
	total := 0
	for i, s := range spans {
		if s.Width != want[i] {
			t.Errorf("span %d width = %d, want %d", i, s.Width, want[i])
		}
		if got, wantFg := s.Style.GetForeground(), theme.TokenStyle(roles[i]).GetForeground(); got != wantFg {
			t.Errorf("span %d foreground = %v, want %v (%s)", i, got, wantFg, roles[i])
		}
		total += s.Width
	}
	if total != 10 {
		t.Errorf("spans add up to %d cells, want 10", total)
	}
}

func TestBreakdownOfAnAgentWithNoTokenIsADimDash(t *testing.T) {
	c := column(t, breakdownTitle)
	for name, a := range map[string]agentusage.Agent{
		"orchestrator": {Main: true},
		"sub-agent":    {ID: "a01", Type: "idle"},
	} {
		r := rowOf(t, a)
		if got := c.Cell(r); got != noValue {
			t.Errorf("%s: Cell = %q, want %q", name, got, noValue)
		}
		if spans := c.Spans(r); spans != nil {
			t.Errorf("%s: Spans = %v, want nil", name, spans)
		}
		if got, want := c.Style(r).GetForeground(), theme.DimStyle.GetForeground(); got != want {
			t.Errorf("%s: Style foreground = %v, want the dim one %v", name, got, want)
		}
	}
	// The same column shows colour for an agent that has tokens.
	if got, dim := c.Style(rowOf(t, mixAgent())).GetForeground(), theme.DimStyle.GetForeground(); got == dim {
		t.Errorf("an agent with tokens must not be dim, got %v", got)
	}
}

func TestBreakdownGlyphFollowsGaugeGlyph(t *testing.T) {
	t.Cleanup(func() { theme.SetGaugeGlyph("") })
	c := column(t, breakdownTitle)
	r := rowOf(t, mixAgent())
	for _, tt := range []struct{ setting, glyph string }{
		{"", "░"}, {"block", "█"}, {"", "░"}, // back to the default after block
	} {
		theme.SetGaugeGlyph(tt.setting)
		if got, want := c.Cell(r), strings.Repeat(tt.glyph, 10); got != want {
			t.Errorf("gauge_glyph %q: Cell = %q, want %q", tt.setting, got, want)
		}
	}
}

// The segments are computed once in agentRows, not on every cell render: Spans
// reads the row's mix and does not look at its tokens.
func TestBreakdownMixIsComputedInAgentRowsNotPerCell(t *testing.T) {
	r := rowOf(t, mixAgent())
	want, _ := segmentCells(r.Tokens)
	if !r.hasMix || r.mix != want {
		t.Errorf("agentRows left hasMix=%v mix=%v, want true and %v", r.hasMix, r.mix, want)
	}
	if rz := rowOf(t, agentusage.Agent{Main: true}); rz.hasMix {
		t.Errorf("an agent with no token has hasMix = true (mix %v)", rz.mix)
	}

	c := column(t, breakdownTitle)
	planted := agentRow{Agent: agentusage.Agent{Tokens: agentusage.Tokens{Output: 1}}, mix: [segCount]int{5, 5, 0, 0}, hasMix: true}
	spans := c.Spans(planted)
	if len(spans) != segCount || spans[0].Width != 5 || spans[1].Width != 5 {
		t.Errorf("Spans on a planted mix = %v, want widths 5,5,0,0 read from the row", spans)
	}
}

// Criterion 1: an agent's per-model buckets add up to its Tokens, on the real
// aggregation (a fixture session), so the bar and the sums tell one story.
func TestByModelSumsEqualAgentTokens(t *testing.T) {
	m, _ := atFixture(t)
	withBuckets := 0
	for _, a := range m.usage.Agents {
		if len(a.ByModel) == 0 {
			continue
		}
		withBuckets++
		var sum agentusage.Tokens
		for _, b := range a.ByModel {
			sum.Input += b.Tokens.Input
			sum.Output += b.Tokens.Output
			sum.CacheRead += b.Tokens.CacheRead
			sum.CacheWrite += b.Tokens.CacheWrite
		}
		if sum != a.Tokens {
			t.Errorf("agent %q: ByModel sums to %+v, Tokens is %+v", a.ID, sum, a.Tokens)
		}
	}
	if withBuckets == 0 {
		t.Fatal("no agent of the fixture has a ByModel split: the check ran on nothing")
	}
}

// ── On screen ────────────────────────────────────────────────────────────────

// The first width at which Breakdown appears with costUsage, measured column by
// column from 50 to 230 (DEVDESK-151: the activity dot costs 4 cells and State
// sits right of Duration): Tools 65, Duration 75, State 84, Model 98,
// Breakdown 110, In 118, Cache read 130, Est. cost 142, Cache write 155. Before
// the dot: Model 94, Breakdown 106, In 114, Cache read 126, Est. cost 138,
// Cache write 151.
func TestBreakdownLayoutAtEachWidth(t *testing.T) {
	tests := []struct {
		width int
		want  []string
	}{
		{230, []string{"Agent", "ID", "Est. cost", "Tools", "Duration", "State", "Model", breakdownTitle, "In", "Out", "Cache read", "Cache write", "Total", "Share"}},
		{155, []string{"Agent", "ID", "Est. cost", "Tools", "Duration", "State", "Model", breakdownTitle, "In", "Out", "Cache read", "Cache write", "Total", "Share"}},
		{154, []string{"Agent", "ID", "Est. cost", "Tools", "Duration", "State", "Model", breakdownTitle, "In", "Out", "Cache read", "Total", "Share"}},
		{142, []string{"Agent", "ID", "Est. cost", "Tools", "Duration", "State", "Model", breakdownTitle, "In", "Out", "Cache read", "Total", "Share"}},
		{141, []string{"Agent", "ID", "Tools", "Duration", "State", "Model", breakdownTitle, "In", "Out", "Cache read", "Total", "Share"}},
		{130, []string{"Agent", "ID", "Tools", "Duration", "State", "Model", breakdownTitle, "In", "Out", "Cache read", "Total", "Share"}},
		{129, []string{"Agent", "ID", "Tools", "Duration", "State", "Model", breakdownTitle, "In", "Out", "Total", "Share"}},
		{118, []string{"Agent", "ID", "Tools", "Duration", "State", "Model", breakdownTitle, "In", "Out", "Total", "Share"}},
		{117, []string{"Agent", "ID", "Tools", "Duration", "State", "Model", breakdownTitle, "Out", "Total", "Share"}}, // In goes before Breakdown
		{110, []string{"Agent", "ID", "Tools", "Duration", "State", "Model", breakdownTitle, "Out", "Total", "Share"}}, // narrowest with the bar
		{109, []string{"Agent", "ID", "Tools", "Duration", "State", "Model", "Out", "Total", "Share"}},                 // Breakdown goes before Model
		{100, []string{"Agent", "ID", "Tools", "Duration", "State", "Model", "Out", "Total", "Share"}},                 // unchanged
		{98, []string{"Agent", "ID", "Tools", "Duration", "State", "Model", "Out", "Total", "Share"}},                  // narrowest with Model
		{97, []string{"Agent", "ID", "Tools", "Duration", "State", "Out", "Total", "Share"}},                           // Model goes
		{84, []string{"Agent", "ID", "Tools", "Duration", "State", "Out", "Total", "Share"}},                           // narrowest with State
		{83, []string{"Agent", "ID", "Tools", "Duration", "Out", "Total", "Share"}},                                    // State goes before Duration
		{80, []string{"Agent", "ID", "Tools", "Duration", "Out", "Total", "Share"}},                                    // the user's 80-column layout (DEVDESK-151, Q1 B)
		{60, []string{"Agent", "ID", "Out", "Total", "Share"}},                                                         // unchanged
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.width), func(t *testing.T) {
			m := atPricedAgents(t, nil, tt.width, costUsage())
			if got := m.agentTable.RenderedWidth(); got != tt.width-2 {
				t.Errorf("rendered width = %d, want %d (Rule 116)", got, tt.width-2)
			}
			got := shownTitles(t, m.Render())
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("columns at %d = %v, want %v:\n%s", tt.width, got, tt.want, m.Render())
			}
		})
	}
}

// Rule 116 at every width from 50 to 230, the bar column coming and going, and
// the order in which optional columns give way holds at each of them.
func TestBreakdownRule116AndDropOrderAtEveryWidth(t *testing.T) {
	// Optional columns in the order they disappear: Cache write, Est. cost, Cache read, In, Breakdown, Model, State, Duration, Tools.
	vanish := []string{"Cache write", "Est. cost", "Cache read", "In", breakdownTitle, "Model", "State", "Duration", "Tools"}
	shown, widths := 0, 0
	for w := 50; w <= 230; w++ {
		m := atPricedAgents(t, nil, w, costUsage())
		widths++
		if got := m.agentTable.RenderedWidth(); got != w-2 {
			t.Fatalf("width %d: rendered width = %d, want %d (Rule 116)", w, got, w-2)
		}
		cols := shownTitles(t, m.Render())
		if i := slices.Index(cols, breakdownTitle); i >= 0 {
			shown++
			if cols[i-1] != "Model" || cols[i+1] != "In" && cols[i+1] != "Out" {
				t.Fatalf("width %d: %s sits between %q and %q: %v", w, breakdownTitle, cols[i-1], cols[i+1], cols)
			}
		}
		// A column that is gone means every column that goes before it is gone.
		for i, gone := range vanish {
			if slices.Contains(cols, gone) {
				continue
			}
			for _, kept := range vanish[:i] {
				if slices.Contains(cols, kept) {
					t.Fatalf("width %d: %s is gone but %s, which goes first, is still shown: %v", w, gone, kept, cols)
				}
			}
		}
	}
	if widths != 181 {
		t.Fatalf("checked %d widths, want 181 (50..230)", widths)
	}
	if shown == 0 {
		t.Fatal("Breakdown never appears between 50 and 230 columns")
	}
	if shown == widths {
		t.Fatal("Breakdown appears at every width: it never gives way")
	}
}

// The bar is ten cells wide on screen: one orchestrator line carries exactly ten
// gauge glyphs, whatever the width it is shown at.
func TestBreakdownBarIsTenCellsOnScreen(t *testing.T) {
	for _, w := range []int{110, 150, 230} {
		m := atPricedAgents(t, nil, w, costUsage())
		var line string
		for _, l := range strings.Split(stripANSI(m.Render()), "\n") {
			if strings.Contains(l, orchestratorLabel) {
				line = l
			}
		}
		if line == "" {
			t.Fatalf("width %d: no orchestrator line:\n%s", w, m.Render())
		}
		if got := strings.Count(line, "░"); got != 10 {
			t.Errorf("width %d: %d gauge glyphs on the orchestrator line, want 10: %q", w, got, line)
		}
	}
}

// AC5: a session without a single token has a dash for every bar, a dash for
// every Share, and does not panic.
func TestBreakdownZeroTokenSession(t *testing.T) {
	u := agentusage.Usage{Agents: []agentusage.Agent{{Main: true}, {ID: "a01", Type: "coder"}, {ID: "a02", Type: "tester"}}}
	m := atPricedAgents(t, nil, 200, u)
	if !slices.Contains(shownTitles(t, m.Render()), breakdownTitle) {
		t.Fatalf("Breakdown is not shown at 200 columns:\n%s", m.Render())
	}
	c := column(t, breakdownTitle)
	rows := m.agentTable.Items()
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	for _, r := range rows {
		if got := c.Cell(r); got != noValue {
			t.Errorf("%s: bar cell = %q, want %q", roleCell(r), got, noValue)
		}
		if got := shareCell(r); got != noValue {
			t.Errorf("%s: Share = %q, want %q", roleCell(r), got, noValue)
		}
	}
	if strings.Contains(stripANSI(m.Render()), "░") {
		t.Errorf("a gauge glyph is drawn for an agent with no token:\n%s", m.Render())
	}
}

// AC6: the mix follows a live session. A full read that adds cache read to one
// agent changes its spans and only its spans, and the cursor stays on its agent.
func TestBreakdownFollowsALiveUpdate(t *testing.T) {
	before := agentusage.Usage{Agents: []agentusage.Agent{
		{Main: true, Tokens: agentusage.Tokens{Input: 500, Output: 500}},
		{ID: "a01", Type: "coder", Tokens: agentusage.Tokens{Input: 500, Output: 500}},
		{ID: "a02", Type: "tester", Tokens: agentusage.Tokens{Input: 500, Output: 500}},
	}}
	after := agentusage.Usage{Agents: []agentusage.Agent{
		before.Agents[0],
		{ID: "a01", Type: "coder", Tokens: agentusage.Tokens{Input: 500, Output: 500, CacheRead: 1000}},
		before.Agents[2],
	}}
	m := atPricedAgents(t, nil, 200, before)
	m = selectAgent(t, m, "a02")
	was := spanWidthsByAgent(t, m)
	if len(was) != 3 {
		t.Fatalf("precondition: want spans for three agents, got %v", was)
	}
	m = answer(t, m, after)
	now := spanWidthsByAgent(t, m)
	if slices.Equal(now["a01"], was["a01"]) {
		t.Errorf("coder spans did not change after cache read was added: %v", now["a01"])
	}
	if want, _ := segmentCells(after.Agents[1].Tokens); !slices.Equal(now["a01"], want[:]) {
		t.Errorf("coder spans = %v, want %v", now["a01"], want)
	}
	for _, id := range []string{"", "a02"} {
		if !slices.Equal(now[id], was[id]) {
			t.Errorf("agent %q spans changed from %v to %v", id, was[id], now[id])
		}
	}
	if got := selectedID(t, m); got != "a02" {
		t.Errorf("the cursor moved to %q, want it to stay on a02", got)
	}
}

// spanWidthsByAgent is each row's span widths, keyed by agent ID ("" for the
// orchestrator; roleCell carries the tree branch and is not an identity).
func spanWidthsByAgent(t *testing.T, m Model) map[string][]int {
	t.Helper()
	c := column(t, breakdownTitle)
	out := map[string][]int{}
	for _, r := range m.agentTable.Items() {
		for _, s := range c.Spans(r) {
			out[r.ID] = append(out[r.ID], s.Width)
		}
	}
	return out
}

// cacheReadLine is an assistant line whose usage is cache read only.
func cacheReadLine(msgID, model string, cacheRead int) string {
	return fmt.Sprintf(`{"type":"assistant","uuid":"u-%[1]s","message":{"id":%[1]q,"model":%[2]q,"role":"assistant","stop_reason":"end_turn","content":[],"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":%[3]d,"cache_creation_input_tokens":0}}}`,
		msgID, model, cacheRead)
}

// AC6 through the real chain: a poll reads only what was appended (a partial
// delta, not a full read) and the appended agent's bar is recomputed.
func TestBreakdownFollowsAPartialDelta(t *testing.T) {
	m, parent := atFixture(t)
	sub := filepath.Join(subDir(parent), "agent-a01.jsonl")
	m = selectAgent(t, m, "a02")
	was := spanWidthsByAgent(t, m)
	if len(was) < 2 {
		t.Fatalf("precondition: want several bars, got %v", was)
	}
	var coder string
	for _, r := range m.agentTable.Items() {
		if r.Type == "coder" {
			coder = r.ID
		}
	}
	if coder == "" {
		t.Fatal("precondition: the fixture has a coder")
	}

	var wasCacheRead int64
	for _, a := range m.usage.Agents {
		if a.Type == "coder" {
			wasCacheRead = a.Tokens.CacheRead
		}
	}
	appendLine(t, sub, cacheReadLine("msg_cr", "model-gamma", 100_000_000))
	m, msgs := pollTick(t, m, m.agentGen)
	reads := usageMsgs(msgs)
	if len(reads) != 1 || reads[0].delta.Full() {
		t.Fatalf("want one incremental read, got %+v", reads)
	}
	m, _ = answered(t, m, reads[0])

	now := spanWidthsByAgent(t, m)
	var coderTokens agentusage.Tokens
	for _, a := range m.usage.Agents {
		if a.Type == "coder" {
			coderTokens = a.Tokens
		}
	}
	if coderTokens.CacheRead != wasCacheRead+100_000_000 {
		t.Fatalf("precondition: the coder's cache read = %d, want %d plus the appended 100000000", coderTokens.CacheRead, wasCacheRead)
	}
	if want, _ := segmentCells(coderTokens); !slices.Equal(now[coder], want[:]) || slices.Equal(now[coder], was[coder]) {
		t.Errorf("coder spans: before %v, after %v, want %v (changed)", was[coder], now[coder], want)
	}
	for id, w := range was {
		if id != coder && !slices.Equal(now[id], w) {
			t.Errorf("agent %q spans changed from %v to %v", id, w, now[id])
		}
	}
	if got := selectedID(t, m); got != "a02" {
		t.Errorf("the cursor moved to %q, want it to stay on a02", got)
	}
}

// ── Help (Rule 114) ──────────────────────────────────────────────────────────

func agentColumnsHelp(t *testing.T) string {
	t.Helper()
	for _, s := range New(nil).GetHelpContent().Sections {
		if s.Title == "Agent columns" {
			return s.Body
		}
	}
	t.Fatal("no Agent columns section in the help")
	return ""
}

func TestHelpDescribesTheBreakdownColumn(t *testing.T) {
	body := agentColumnsHelp(t)
	i := strings.Index(body, breakdownTitle+":")
	if i < 0 {
		t.Fatalf("the Agent columns section does not describe %s:\n%s", breakdownTitle, body)
	}
	para := body[i:]
	if nl := strings.Index(para, "\n"); nl >= 0 {
		para = para[:nl]
	}
	// The colours as a legend in words, in bar order.
	pos := -1
	for _, w := range []string{"Input", "Output", "Cache write", "Cache read"} {
		j := strings.Index(para, w)
		if j < 0 || j <= pos {
			t.Fatalf("the %s paragraph must name Input, Output, Cache write, Cache read in that order (stuck at %q):\n%s", breakdownTitle, w, para)
		}
		pos = j
	}
	for _, want := range []string{"half a cell", "app.gauge_glyph"} {
		if !strings.Contains(para, want) {
			t.Errorf("the %s paragraph lacks %q:\n%s", breakdownTitle, want, para)
		}
	}
}

func TestHelpDropOrderPlacesBreakdownBetweenInAndModel(t *testing.T) {
	body := agentColumnsHelp(t)
	i := strings.Index(body, "On a narrow terminal")
	if i < 0 {
		t.Fatalf("the narrow-terminal sentence is gone:\n%s", body)
	}
	sentence := body[i:]
	if nl := strings.Index(sentence, "\n"); nl >= 0 { // the sentence ends the line; "Est. cost" holds a dot
		sentence = sentence[:nl]
	}
	pos := -1
	for _, w := range []string{"Cache write", "Est. cost", "Cache read", "In,", breakdownTitle, "Model", "State", "Duration", "Tools"} {
		j := strings.Index(sentence, w)
		if j < 0 || j <= pos {
			t.Fatalf("drop order must read Cache write, Est. cost, Cache read, In, Breakdown, Model, State, Duration, Tools (stuck at %q):\n%s", w, sentence)
		}
		pos = j
	}
}

// ── Rendered row ─────────────────────────────────────────────────────────────

// sgr is one escape sequence followed by a run of gauge glyphs; the first run
// of the cell also carries the cell's left padding space.
var sgr = regexp.MustCompile("\x1b\\[([0-9;]*)m ?(░+)")

// sgrColours reads an SGR parameter list: the foreground and background as
// "r;g;b" (empty when absent) and whether bold is set.
func sgrColours(params string) (fg, bg string, bold bool) {
	f := strings.Split(params, ";")
	for i := 0; i < len(f); i++ {
		switch {
		case (f[i] == "38" || f[i] == "48") && i+4 < len(f) && f[i+1] == "2":
			rgb := strings.Join(f[i+2:i+5], ";")
			if f[i] == "38" {
				fg = rgb
			} else {
				bg = rgb
			}
			i += 4
		case f[i] == "1":
			bold = true
		}
	}
	return fg, bg, bold
}

// rgbOf is a colour as "r;g;b", 8 bits per channel.
func rgbOf(t *testing.T, c color.Color) string {
	t.Helper()
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%d;%d;%d", r>>8, g>>8, b>>8)
}

// sameRGB compares two "r;g;b" colours within one unit per channel: the colour
// library's hex to 8-bit conversion rounds differently from fmt's.
func sameRGB(a, b string) bool {
	var ra, ga, ba, rb, gb, bb int
	if _, err := fmt.Sscanf(a, "%d;%d;%d", &ra, &ga, &ba); err != nil {
		return false
	}
	if _, err := fmt.Sscanf(b, "%d;%d;%d", &rb, &gb, &bb); err != nil {
		return false
	}
	near := func(x, y int) bool { return x-y <= 1 && y-x <= 1 }
	return near(ra, rb) && near(ga, gb) && near(ba, bb)
}

// barRuns returns the (params, length) of each glyph run on the line of the
// agent carrying role in its Agent cell.
func barRuns(t *testing.T, view, role string) (params []string, lengths []int) {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if !strings.Contains(stripANSI(line), role) {
			continue
		}
		for _, m := range sgr.FindAllStringSubmatch(line, -1) {
			params = append(params, m[1])
			lengths = append(lengths, len([]rune(m[2])))
		}
		return params, lengths
	}
	t.Fatalf("no line for %s in:\n%s", role, view)
	return nil, nil
}

// On a rendered row the four segments carry the four token colours in bar
// order with the right lengths, unselected, and selected with each segment
// repainting the selection background in bold.
func TestBreakdownRenderedRowColoursAndSelection(t *testing.T) {
	colorprofile.TrueColor(t)
	u := agentusage.Usage{Agents: []agentusage.Agent{
		{Main: true, Tokens: agentusage.Tokens{Input: 100, Output: 300, CacheWrite: 100, CacheRead: 500}},
		{ID: "a01", Type: "coder", Tokens: agentusage.Tokens{Input: 600, Output: 200, CacheWrite: 100, CacheRead: 100}},
	}}
	wantLens := map[string][]int{"orchestrator": {1, 3, 1, 5}, "coder": {6, 2, 1, 1}}
	roles := theme.TokenRoles()
	var wantFg []string
	for _, r := range roles {
		wantFg = append(wantFg, rgbOf(t, theme.TokenColor(r)))
	}
	for i, a := range wantFg {
		for _, b := range wantFg[i+1:] {
			if a == b {
				t.Fatalf("the four token colours must differ to be told apart on screen: %v", wantFg)
			}
		}
	}
	selBg := rgbOf(t, theme.ColorTableLineSelected)

	m := atPricedAgents(t, nil, 200, u) // the cursor starts on the orchestrator
	checked := 0
	for _, cursorOn := range []string{"orchestrator", "coder"} {
		if cursorOn == "coder" {
			m, _ = opened(t, m, "down")
		}
		for _, role := range []string{"orchestrator", "coder"} {
			params, lengths := barRuns(t, m.Render(), role)
			if len(params) != segCount {
				t.Fatalf("cursor on %s, row %s: %d coloured runs, want %d (%v)", cursorOn, role, len(params), segCount, params)
			}
			selected := role == cursorOn
			for i, p := range params {
				fg, bg, bold := sgrColours(p)
				if lengths[i] != wantLens[role][i] {
					t.Errorf("cursor on %s, row %s, segment %d (%s): %d cells, want %d", cursorOn, role, i, roles[i], lengths[i], wantLens[role][i])
				}
				if !sameRGB(fg, wantFg[i]) {
					t.Errorf("cursor on %s, row %s, segment %d (%s): foreground %q, want %q", cursorOn, role, i, roles[i], fg, wantFg[i])
				}
				if selected && (!sameRGB(bg, selBg) || !bold) {
					t.Errorf("cursor on %s, selected row, segment %d: background %q bold %v, want %q and bold", cursorOn, i, bg, bold, selBg)
				}
				if !selected && sameRGB(bg, selBg) {
					t.Errorf("cursor on %s, unselected row %s, segment %d carries the selection background", cursorOn, role, i)
				}
				checked++
			}
		}
	}
	if checked != 16 {
		t.Fatalf("checked %d segments, want 16 (2 cursors x 2 rows x 4 segments)", checked)
	}
}

// ── The bar does not take the Agent column's width ───────────────────────────

// At 160 columns the Agent column keeps the width it had before the bar
// existed: the type's own width behind its branch (27 to 33 cells), never cut.
func TestBreakdownDoesNotTruncateTheAgentColumnAt160(t *testing.T) {
	checked := 0
	for _, n := range []int{24, 27, 30} {
		typ := strings.Repeat("t", n)
		u := agentusage.Usage{Agents: []agentusage.Agent{orchestrator(1, 0), subAgent("b01", typ, 1, 0)}}
		m := atPricedAgents(t, nil, 160, u)
		want := utf8.RuneCountInString(lastStem + typ)
		if want < 27 || want > 33 {
			t.Fatalf("test setup: want %d outside 27..33", want)
		}
		if got := m.agentTable.Table().Columns()[agentColIdx].Width; got < want {
			t.Errorf("type of %d chars: Agent column is %d wide at 160 columns, want at least %d", n, got, want)
		}
		if !strings.Contains(stripANSI(m.Render()), lastStem+typ) {
			t.Errorf("type of %d chars is cut on screen at 160 columns", n)
		}
		checked++
	}
	if checked != 3 {
		t.Fatalf("checked %d contents, want 3", checked)
	}
}
