package agentusageview

import (
	"bytes"
	"log"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/agentusage/pricing"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-47 step 5: the Est. cost column of the agents table, formatUSD,
// costCell, and where the price table comes from. Names follow the plan:
// formatUSD(float64) string, costCell(agentRow) string,
// costColumn() datatable.Column[agentRow], agentRow.cost pricing.Estimate,
// agentRows(u, tree, clock, prices). Header, footer and help are step 6.

const costTitle = "Est. cost"

func TestFormatUSD(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want string
	}{
		{"zero", 0, "$0.00"},
		{"under half a cent is never $0.00", 0.004, "<$0.01"},
		{"tiny but non-zero", 1e-9, "<$0.01"},
		{"half a cent rounds up", 0.005, "$0.01"},
		{"two decimals", 1.234, "$1.23"},
		{"rounds half up on the third decimal", 1.236, "$1.24"},
		{"top of the two-decimal range", 999.994, "$999.99"},
		{"from $1000, whole dollars", 999.996, "$1000"},
		{"no thousands separator", 1234.5, "$1235"},
		{"largest shown exactly", 99999, "$99999"},
		{"past 99999 it saturates", 123456, "$99999+"},
		{"negative keeps two decimals and a real minus sign", -0.40, "\u2212$0.40"},
		{"negative larger", -12.345, "\u2212$12.35"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatUSD(tt.in)
			if got != tt.want {
				t.Errorf("formatUSD(%v) = %q, want %q", tt.in, got, tt.want)
			}
			if strings.Contains(got, "~") {
				t.Errorf("formatUSD(%v) = %q: the estimate mark belongs to costCell, Billed has none", tt.in, got)
			}
			if strings.Contains(got, ",") || strings.Contains(got, "\x1b") {
				t.Errorf("formatUSD(%v) = %q: no separator, no ANSI", tt.in, got)
			}
		})
	}
	if strings.ContainsRune(formatUSD(-0.40), '-') {
		t.Error("a negative amount uses U+2212, not an ASCII hyphen")
	}
}

func priced(usd float64) pricing.Estimate {
	return pricing.Estimate{USD: usd, Status: pricing.Priced}
}

func TestCostCell(t *testing.T) {
	main := orchestrator(10, 0)
	sub := subAgent("a01", "coder", 10, 0)
	partial := subAgent("a02", "tester", 10, 2) // PartialMessages > 0
	tests := []struct {
		name string
		row  agentRow
		want string
	}{
		{"orchestrator", agentRow{Agent: main, cost: priced(1.234)}, "~$1.23"},
		{"sub-agent is always a floor", agentRow{Agent: sub, cost: priced(1.234)}, "\u2265 ~$1.23"},
		{"sub-agent with partial output, same mark", agentRow{Agent: partial, cost: priced(1.234)}, "\u2265 ~$1.23"},
		{"orchestrator under a cent", agentRow{Agent: main, cost: priced(0.004)}, "~<$0.01"},
		{"sub-agent under a cent", agentRow{Agent: sub, cost: priced(0.004)}, "\u2265 ~<$0.01"},
		{"orchestrator at half a cent", agentRow{Agent: main, cost: priced(0.005)}, "~$0.01"},
		{"a priced zero is an estimate of zero, not a dash", agentRow{Agent: main, cost: priced(0)}, "~$0.00"},
		{"sub-agent in whole dollars", agentRow{Agent: sub, cost: priced(1234.5)}, "\u2265 ~$1235"},
		{"widest two-decimal sub-agent", agentRow{Agent: sub, cost: priced(999.99)}, "\u2265 ~$999.99"},
		{"unpriced orchestrator", agentRow{Agent: main, cost: pricing.Estimate{Status: pricing.Unpriced, Unpriced: []string{"model-a"}}}, "-"},
		{"unpriced sub-agent", agentRow{Agent: sub, cost: pricing.Estimate{Status: pricing.Unpriced}}, "-"},
		{"no tokens", agentRow{Agent: sub, cost: pricing.Estimate{Status: pricing.NoTokens}}, "-"},
		{"zero value estimate is NoTokens", agentRow{Agent: main}, "-"},
		{"unpriced never shows a stale amount", agentRow{Agent: main, cost: pricing.Estimate{USD: 5, Status: pricing.Unpriced}}, "-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := costCell(tt.row)
			if got != tt.want {
				t.Errorf("costCell = %q, want %q", got, tt.want)
			}
			if tt.row.cost.Status != pricing.Priced && got != "-" {
				t.Errorf("%q: an unknown cost must be a dash, never an amount", got)
			}
			if w := utf8.RuneCountInString(got); w > 10 {
				t.Errorf("%q is %d cells wide, the column is 10", got, w)
			}
		})
	}
}

// Rule 122: the cell is measured before it is styled.
func TestCostCellsCarryNoEscapeSequence(t *testing.T) {
	testutil.TrueColor(t)
	col := column(t, costTitle)
	for _, r := range []agentRow{
		{Agent: orchestrator(1, 0), cost: priced(1.5)},
		{Agent: subAgent("a", "coder", 1, 0), cost: priced(1.5)},
		{Agent: subAgent("a", "coder", 1, 0), cost: priced(0.001)},
		{Agent: subAgent("a", "coder", 1, 0), cost: pricing.Estimate{Status: pricing.Unpriced}},
		{Agent: subAgent("a", "coder", 0, 0)},
	} {
		if got := col.Cell(r); strings.Contains(got, "\x1b") {
			t.Errorf("cost cell carries an escape sequence: %q", got)
		}
	}
}

// Rule 122 colour discipline: a dash is dim, an amount keeps the default colour.
func TestCostColumnDimsOnlyThePlaceholder(t *testing.T) {
	col := column(t, costTitle)
	dim := theme.DimStyle.GetForeground()
	plain := lipgloss.NewStyle().GetForeground()
	dashes := []agentRow{
		{Agent: orchestrator(1, 0), cost: pricing.Estimate{Status: pricing.Unpriced}},
		{Agent: subAgent("a", "coder", 0, 0), cost: pricing.Estimate{Status: pricing.NoTokens}},
	}
	for _, r := range dashes {
		if got := col.Style(r).GetForeground(); got != dim {
			t.Errorf("%q must be dim, got %v", costCell(r), got)
		}
	}
	for _, r := range []agentRow{
		{Agent: orchestrator(1, 0), cost: priced(1.5)},
		{Agent: subAgent("a", "coder", 1, 0), cost: priced(0.001)},
		{Agent: orchestrator(1, 0), cost: priced(0)},
	} {
		if got := col.Style(r).GetForeground(); got != plain {
			t.Errorf("%q keeps the default colour, got %v", costCell(r), got)
		}
	}
}

func TestCostColumnDeclaration(t *testing.T) {
	cols := agentColumns()
	if len(cols) < 3 || cols[2].Title != costTitle {
		t.Fatalf("%q must come right after ID, columns: %v", costTitle, titlesOf(cols))
	}
	c := cols[2]
	if c.Sizing != datatable.SizingFixed || c.MinWidth != 10 {
		t.Errorf("Sizing/MinWidth = %v/%d, want SizingFixed/10 (widest cell is \u2265 ~$999.99)", c.Sizing, c.MinWidth)
	}
	if !c.Optional {
		t.Error("Est. cost is Optional: it gives way on a narrow terminal")
	}
	if !c.DropFirst {
		t.Error("Est. cost is DropFirst: it is the first column sacrificed after Cache write, so State/Tools/Duration survive at 80 columns")
	}
	if c.Less != nil || c.DescFirst {
		t.Error("the plan gives Est. cost no Less: `.` keeps its Total-only cycle")
	}
	if c.Style == nil {
		t.Error("Est. cost declares a Style (Rule 122)")
	}
}

func titlesOf(cols []datatable.Column[agentRow]) []string {
	var out []string
	for _, c := range cols {
		out = append(out, c.Title)
	}
	return out
}

// ── Where the numbers come from ──────────────────────────────────────────────

// pricesYAML prices two models; every rate is distinct so a mix-up shows.
// model-a: in 1, out 10, w5m 2, w1h 4, read 0.1. model-b: twice that.
const pricesYAML = `prices:
  model-a: {input: 1, output: 10, cache_write_5m: 2, cache_write_1h: 4, cache_read: 0.1}
  model-b: {input: 2, output: 20, cache_write_5m: 4, cache_write_1h: 8, cache_read: 0.2}
`

// cfgWith builds a context config holding the given agent_usage YAML.
func cfgWith(t *testing.T, agentUsageYAML string) *config.Config {
	t.Helper()
	cfg := config.Default()
	if agentUsageYAML == "" {
		return cfg
	}
	if err := yaml.Unmarshal([]byte(agentUsageYAML), &cfg.AgentUsage); err != nil {
		t.Fatalf("bad test YAML: %v", err)
	}
	return cfg
}

const million = 1_000_000

// byModel builds an agent whose tokens are split over the given buckets.
func byModel(a agentusage.Agent, buckets ...agentusage.ModelUsage) agentusage.Agent {
	a.ByModel, a.Models, a.Tokens = buckets, nil, agentusage.Tokens{}
	for _, b := range buckets {
		a.Tokens.Input += b.Tokens.Input
		a.Tokens.Output += b.Tokens.Output
		a.Tokens.CacheRead += b.Tokens.CacheRead
		a.Tokens.CacheWrite += b.Tokens.CacheWrite
		if b.Model != "" {
			a.Models = append(a.Models, b.Model)
		}
	}
	return a
}

func mu(model string, tk agentusage.Tokens, write1h int64) agentusage.ModelUsage {
	return agentusage.ModelUsage{Model: model, Tokens: tk, CacheWrite1h: write1h}
}

// costUsage: the orchestrator (model-a), a sub-agent on model-b, a sub-agent
// that used both, one on a model with no rate, and one with no tokens.
func costUsage() agentusage.Usage {
	return agentusage.Usage{Agents: []agentusage.Agent{
		byModel(agentusage.Agent{Main: true},
			mu("model-a", agentusage.Tokens{Input: million, Output: million}, 0)), // 1 + 10 = 11
		byModel(agentusage.Agent{ID: "a01", Type: "coder"},
			mu("model-b", agentusage.Tokens{Output: 100_000}, 0)), // 2.00
		byModel(agentusage.Agent{ID: "a02", Type: "tester"},
			mu("model-a", agentusage.Tokens{Output: million}, 0),  // 10
			mu("model-b", agentusage.Tokens{Output: 500_000}, 0)), // 10
		byModel(agentusage.Agent{ID: "a03", Type: "reviewer"},
			mu("model-a", agentusage.Tokens{Output: 1}, 0),
			mu("model-c", agentusage.Tokens{Output: 1}, 0)), // model-c has no rate
		{ID: "a04", Type: "idle"}, // no tokens
		byModel(agentusage.Agent{ID: "a05", Type: "writer"},
			// 1M written: 400k for 1 h. 0.6*2 + 0.4*4 = 2.8, plus 1M read = 0.1.
			mu("model-a", agentusage.Tokens{CacheWrite: million, CacheRead: million}, 400_000)),
	}}
}

// atPricedAgents opens the agents level of cfg's view at the given width and shows u.
func atPricedAgents(t *testing.T, cfg *config.Config, width int, u agentusage.Usage) Model {
	t.Helper()
	isolate(t)
	m := feed(t, New(cfg), testutil.Resize(width, 20))
	s := session("id-a000000-0000", "/srv/pa", 0)
	s.Parent = "/p/a.jsonl"
	m = loaded(t, m, s)
	m, _ = opened(t, m, "right")
	return answer(t, m, u)
}

func costOf(t *testing.T, m Model, role string) string {
	t.Helper()
	col := column(t, costTitle)
	for _, r := range m.agentTable.Items() {
		if roleCell(r) == role || strings.HasSuffix(roleCell(r), role) {
			return col.Cell(r)
		}
	}
	t.Fatalf("no %q row", role)
	return ""
}

func TestCostsComeFromTheContextPriceTable(t *testing.T) {
	m := atPricedAgents(t, cfgWith(t, pricesYAML), 160, costUsage())
	want := map[string]string{
		"orchestrator": "~$11.00",
		"coder":        "\u2265 ~$2.00",
		"tester":       "\u2265 ~$20.00", // two models: 10 + 10
		"reviewer":     "-",              // one token under an unpriced model: no partial sum (H3)
		"idle":         "-",
		"writer":       "\u2265 ~$2.90", // 1 h cache write priced apart from the 5 min one
	}
	for role, w := range want {
		if got := costOf(t, m, role); got != w {
			t.Errorf("%s: cost = %q, want %q", role, got, w)
		}
	}
}

func TestAnEmptyPriceTableShowsADashEverywhere(t *testing.T) {
	for name, cfg := range map[string]*config.Config{
		"default config": config.Default(),
		"nil config":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			m := atPricedAgents(t, cfg, 160, costUsage())
			if len(m.agentTable.Items()) == 0 {
				t.Fatal("no rows")
			}
			col := column(t, costTitle)
			for _, r := range m.agentTable.Items() {
				if got := col.Cell(r); got != "-" {
					t.Errorf("%s: cost = %q, want - (DevDesk ships no price)", roleCell(r), got)
				}
			}
		})
	}
}

// H4: the table is read when the view is built (start, context switch,
// ConfigSavedMsg: the router calls New again), never by ctrl+r.
func TestCtrlRDoesNotReReadThePrices(t *testing.T) {
	cfg := cfgWith(t, pricesYAML)
	m := atPricedAgents(t, cfg, 160, costUsage())
	if got := costOf(t, m, "orchestrator"); got != "~$11.00" {
		t.Fatalf("setup: orchestrator = %q", got)
	}

	// The shared config changes underneath the view, as a hand edit or a
	// :cfg save would; the open view must not notice.
	cfg.AgentUsage.Prices = nil
	m, _ = opened(t, m, "ctrl+r")
	m = answer(t, m, costUsage())
	if got := costOf(t, m, "orchestrator"); got != "~$11.00" {
		t.Errorf("after ctrl+r the orchestrator = %q: prices must not be re-read", got)
	}

	// A view built from the new config (context switch) sees the new table.
	fresh := atPricedAgents(t, cfg, 160, costUsage())
	if got := costOf(t, fresh, "orchestrator"); got != "-" {
		t.Errorf("a rebuilt view = %q, want - (its context has no prices)", got)
	}

	other := cfgWith(t, "prices:\n  model-a: {input: 3, output: 30, cache_write_5m: 0, cache_write_1h: 0, cache_read: 0}\n")
	switched := atPricedAgents(t, other, 160, costUsage())
	if got := costOf(t, switched, "orchestrator"); got != "~$33.00" {
		t.Errorf("after a context switch the orchestrator = %q, want ~$33.00", got)
	}
}

// A live read (a delta) rebuilds the rows: the cost follows the new tokens.
func TestCostsFollowTheRowsOnEveryRebuild(t *testing.T) {
	m := atPricedAgents(t, cfgWith(t, pricesYAML), 160, costUsage())
	u := costUsage()
	u.Agents[0] = byModel(agentusage.Agent{Main: true},
		mu("model-a", agentusage.Tokens{Output: 2 * million}, 0))
	m = m.withRows(u, false)
	if got := costOf(t, m, "orchestrator"); got != "~$20.00" {
		t.Errorf("after a rebuild the orchestrator = %q, want ~$20.00", got)
	}
}

func TestSortingByTotalStillWorksWithCosts(t *testing.T) {
	m := atPricedAgents(t, cfgWith(t, pricesYAML), 160, costUsage())
	m, _ = opened(t, m, ".") // tree -> Total, largest first
	var totals []int64
	for _, r := range m.agentTable.Visible() {
		totals = append(totals, r.Tokens.Total())
	}
	for i := 1; i < len(totals); i++ {
		if totals[i] > totals[i-1] {
			t.Fatalf("`.` must sort by Total, largest first, got %v", totals)
		}
	}
	// The cost rides with its row through the sort.
	if got := costOf(t, m, "orchestrator"); got != "~$11.00" {
		t.Errorf("orchestrator cost after the sort = %q", got)
	}
}

// One WARN line per rejected entry, naming the user's own key; the model of
// that entry then shows a dash.
func TestAnIncompleteEntryIsLoggedOnceAndShowsADash(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(&bytes.Buffer{}) })

	m := atPricedAgents(t, cfgWith(t, "prices:\n  model-a: {input: 1, output: 10, cache_write_5m: 2, cache_write_1h: 4}\n"), 160, costUsage())
	var warns []string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, "WARN") && strings.Contains(l, "agent_usage.prices") {
			warns = append(warns, l)
		}
	}
	if len(warns) != 1 {
		t.Fatalf("want exactly one WARN line, got %d in:\n%s", len(warns), buf.String())
	}
	if !strings.Contains(warns[0], "[agentusageview]") || !strings.Contains(warns[0], `"model-a"`) || !strings.Contains(warns[0], "cache_read") {
		t.Errorf("the WARN line must name the view, the key and the missing rate: %q", warns[0])
	}
	if got := costOf(t, m, "orchestrator"); got != "-" {
		t.Errorf("a model whose entry was rejected shows %q, want -", got)
	}
}

// ── Layout ───────────────────────────────────────────────────────────────────

// Measured column by column from 50 to 230 with Est. cost DropFirst. It is
// dropped right after Cache write (also DropFirst, and further right), before
// any ordinary Optional column, so it needs 126 columns to appear. Switch
// widths: Est. cost appears at 126 (Cache write at 139); State at 63, Tools at
// 70, Duration at 80, Model at 94, In at 102, Cache read at 114.
func TestEstCostLayoutAtEachWidth(t *testing.T) {
	tests := []struct {
		width int
		want  []string
	}{
		{200, []string{"Agent", "ID", costTitle, "State", "Tools", "Duration", "Model", "In", "Out", "Cache read", "Cache write", "Total", "Share"}},
		{126, []string{"Agent", "ID", costTitle, "State", "Tools", "Duration", "Model", "In", "Out", "Cache read", "Total", "Share"}}, // narrowest with Est. cost
		{125, []string{"Agent", "ID", "State", "Tools", "Duration", "Model", "In", "Out", "Cache read", "Total", "Share"}},            // Est. cost goes first
		{100, []string{"Agent", "ID", "State", "Tools", "Duration", "Model", "Out", "Total", "Share"}},                                // loses In, Cache read first
		{80, []string{"Agent", "ID", "State", "Tools", "Duration", "Out", "Total", "Share"}},                                          // DEVDESK-46 criterion 1 kept
		{60, []string{"Agent", "ID", "Out", "Total", "Share"}},                                                                        // the five DEVDESK-43 columns; State needs 63
	}
	for _, tt := range tests {
		for name, cfg := range map[string]*config.Config{"no prices": nil, "priced": cfgWith(t, pricesYAML)} {
			t.Run(name+"/"+strconv.Itoa(tt.width), func(t *testing.T) {
				m := atPricedAgents(t, cfg, tt.width, costUsage())
				if got := m.agentTable.RenderedWidth(); got != tt.width-2 {
					t.Errorf("rendered width = %d, want %d (Rule 116)", got, tt.width-2)
				}
				got := header(t, m.View())
				if strings.Join(got, ",") != strings.Join(tt.want, ",") {
					t.Errorf("columns at %d = %v, want %v:\n%s", tt.width, got, tt.want, m.View())
				}
			})
		}
	}
}

// Wherever Est. cost is shown it is the third column, and Rule 116 holds at
// every width from 50 to 230, with and without it.
func TestEstCostIsThirdWhenShownAndRule116HoldsAtEveryWidth(t *testing.T) {
	shown := 0
	for w := 50; w <= 230; w++ {
		m := atPricedAgents(t, nil, w, costUsage())
		if got := m.agentTable.RenderedWidth(); got != w-2 {
			t.Fatalf("width %d: rendered width = %d, want %d (Rule 116)", w, got, w-2)
		}
		cols := header(t, m.View())
		for i, c := range cols {
			if c == costTitle {
				shown++
				if i != 2 {
					t.Fatalf("width %d: %s is column %d, want the third (index 2): %v", w, costTitle, i, cols)
				}
			}
		}
	}
	if shown == 0 {
		t.Fatal("Est. cost never appears between 50 and 230 columns")
	}
	if cols := header(t, atPricedAgents(t, nil, 200, costUsage()).View()); len(cols) < 3 || cols[2] != costTitle {
		t.Errorf("at 200 columns Est. cost must be third, got %v", cols)
	}
}

// Rule 130: no new key, and the set does not depend on whether prices exist.
func TestEstCostAddsNoShortcut(t *testing.T) {
	bare := atPricedAgents(t, nil, 160, costUsage())
	priced := atPricedAgents(t, cfgWith(t, pricesYAML), 160, costUsage())
	unpriced := atPricedAgents(t, cfgWith(t, pricesYAML), 160, agentusage.Usage{Agents: []agentusage.Agent{
		byModel(agentusage.Agent{Main: true}, mu("model-z", agentusage.Tokens{Output: 1}, 0)),
	}})
	want := strings.Join(testutil.ShortcutKeys(bare.GetShortcuts()), ",")
	if want == "" {
		t.Fatal("the agents level advertises no shortcuts")
	}
	for name, m := range map[string]Model{"priced": priced, "unpriced": unpriced} {
		if got := strings.Join(testutil.ShortcutKeys(m.GetShortcuts()), ","); got != want {
			t.Errorf("%s: keys %q differ from the no-price level's %q", name, got, want)
		}
	}
}

// Past $99 999 both kinds of row show "~$99999+" so the cell stays within 10
// cells; the "+" already means "at least", so the sub-agent drops its floor mark.
func TestCostCellSaturatesWithinTheColumn(t *testing.T) {
	for _, usd := range []float64{99999.5, 123456, 1e12} {
		for name, r := range map[string]agentRow{
			"orchestrator": {Agent: orchestrator(1, 0), cost: priced(usd)},
			"sub-agent":    {Agent: subAgent("a", "coder", 1, 0), cost: priced(usd)},
		} {
			if got := costCell(r); got != "~$99999+" {
				t.Errorf("%s at %v: costCell = %q, want ~$99999+", name, usd, got)
			}
		}
	}
	if got := costCell(agentRow{Agent: subAgent("a", "coder", 1, 0), cost: priced(99999)}); got != "≥ ~$99999" {
		t.Errorf("largest exact sub-agent amount = %q", got)
	}
}
