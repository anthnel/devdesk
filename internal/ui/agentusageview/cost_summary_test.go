package agentusageview

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/agentusage/pricing"
	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-47 step 6: the header (Est. cost, Billed, Unattributed), the footer
// price hint, and the help section. Names assumed from the plan: Model.cost
// (pricing.Summary, stored by withRows); header keys "Est. cost", "Billed",
// "Unattributed"; help section titled "Costs". pricesYAML is cost_test.go's:
// model-a {1, 10, 2, 4, 0.1}, model-b {2, 20, 4, 8, 0.2} USD per million.

const (
	estKey          = "Est. cost"
	billedKey       = "Billed"
	unattributedKey = "Unattributed"

	// headerMax is internal/app's headerMinHeight (unexported there): the
	// header never shows more than this many info lines.
	headerMax = 7
)

// sumUsage prices to 8.20: the orchestrator 1 + 6 = 7.00 on model-a, the coder
// 1.20 on model-b. With Billed 10.00 the unattributed part is 1.80 (criterion 5).
func sumUsage(b agentusage.Billed) agentusage.Usage {
	return agentusage.Usage{
		Billed: b,
		Agents: []agentusage.Agent{
			byModel(agentusage.Agent{Main: true},
				mu("model-a", agentusage.Tokens{Input: million, Output: 600_000}, 0)),
			byModel(agentusage.Agent{ID: "a01", Type: "coder"},
				mu("model-b", agentusage.Tokens{Output: 60_000}, 0)),
		},
	}
}

func billed(usd float64) agentusage.Billed { return agentusage.Billed{USD: usd, Known: true} }

// headerEntry returns the header entry of key, if any.
func headerEntry(m Model, key string) (shortcut.HeaderInfo, bool) {
	for _, h := range m.GetHeaderInfo("") {
		if h.Key == key {
			return h, true
		}
	}
	return shortcut.HeaderInfo{}, false
}

func headerKeys(m Model) []string {
	var keys []string
	for _, h := range m.GetHeaderInfo("") {
		keys = append(keys, h.Key)
	}
	return keys
}

func TestWithRowsStoresTheCostSummary(t *testing.T) {
	m := atPricedAgents(t, cfgWith(t, pricesYAML), 160, sumUsage(billed(10)))
	if len(m.cost.Agents) != 2 {
		t.Fatalf("cost.Agents = %d estimates, want 2 (same order as Usage.Agents)", len(m.cost.Agents))
	}
	if !m.cost.Billed.Known || m.cost.Billed.USD != 10 {
		t.Errorf("cost.Billed = %+v, want {10 true}", m.cost.Billed)
	}
	if !m.cost.UnattributedKnown || math.Abs(m.cost.Unattributed-1.80) > 1e-9 {
		t.Errorf("cost.Unattributed = %v (known %v), want 1.80 (criterion 5)", m.cost.Unattributed, m.cost.UnattributedKnown)
	}

	// Every rebuild refreshes it: a later read changes the figures.
	m = m.withRows(sumUsage(agentusage.Billed{}), false)
	if m.cost.Billed.Known || m.cost.UnattributedKnown {
		t.Errorf("after a read with no cost-state, cost = %+v, want Billed and Unattributed unknown", m.cost)
	}
}

func TestHeaderCostFields(t *testing.T) {
	unpriced := sumUsage(billed(10))
	unpriced.Agents = append(unpriced.Agents, byModel(agentusage.Agent{ID: "a02", Type: "tester"},
		mu("model-z", agentusage.Tokens{Output: 5}, 0))) // no rate for model-z
	noTokens := agentusage.Usage{Billed: billed(3.5), Agents: []agentusage.Agent{
		{Main: true}, {ID: "a01", Type: "coder"},
	}}

	tests := []struct {
		name string
		u    agentusage.Usage
		want map[string]string // key -> value; "" means the entry must be absent
	}{
		{"criterion 5: 10.00 billed, 8.20 estimated, 1.80 unattributed", sumUsage(billed(10)),
			map[string]string{estKey: "~$8.20", billedKey: "$10.00", unattributedKey: "$1.80"}},
		{"criterion 6: a running session has neither Billed nor Unattributed", sumUsage(agentusage.Billed{}),
			map[string]string{estKey: "~$8.20", billedKey: "", unattributedKey: ""}},
		{"a stale price table: negative remainder, real minus sign", sumUsage(billed(7.80)),
			map[string]string{estKey: "~$8.20", billedKey: "$7.80", unattributedKey: "\u2212$0.40"}},
		{"one unpriced agent: Est. cost and Unattributed are dashes, Billed stays", unpriced,
			map[string]string{estKey: "-", billedKey: "$10.00", unattributedKey: "-"}},
		{"unpriced and no cost-state: still neither Billed nor Unattributed", func() agentusage.Usage {
			u := unpriced
			u.Billed = agentusage.Billed{}
			return u
		}(), map[string]string{estKey: "-", billedKey: "", unattributedKey: ""}},
		{"no agent has tokens: Unattributed is the whole bill", noTokens,
			map[string]string{estKey: "-", billedKey: "$3.50", unattributedKey: "$3.50"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := atPricedAgents(t, cfgWith(t, pricesYAML), 160, tt.u)
			for key, want := range tt.want {
				h, ok := headerEntry(m, key)
				switch {
				case want == "" && ok:
					t.Errorf("%s = %q, want no such entry", key, h.Value)
				case want != "" && !ok:
					t.Errorf("no %q entry in %v, want %q", key, headerKeys(m), want)
				case want != "" && h.Value != want:
					t.Errorf("%s = %q, want %q", key, h.Value, want)
				}
			}
			if h, ok := headerEntry(m, billedKey); ok && strings.Contains(h.Value, "~") {
				t.Errorf("Billed = %q: it is Claude Code's own figure, no estimate mark", h.Value)
			}
		})
	}
}

func TestHeaderCostOrder(t *testing.T) {
	m := atPricedAgents(t, cfgWith(t, pricesYAML), 160, sumUsage(billed(10)))
	want := []string{"Agents", "Tokens", estKey, billedKey, unattributedKey, "Updates"}
	if got := headerKeys(m); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("header = %v, want %v", got, want)
	}
}

func TestANegativeUnattributedIsOrangeAndTheRestIsNot(t *testing.T) {
	orange := lipgloss.TerminalColor(theme.ColorSeverityMedium)
	normal := theme.HeaderValueStyle.GetForeground()

	neg := atPricedAgents(t, cfgWith(t, pricesYAML), 160, sumUsage(billed(7.80)))
	h, ok := headerEntry(neg, unattributedKey)
	if !ok {
		t.Fatal("no Unattributed entry")
	}
	if got := h.Style.GetForeground(); got != orange {
		t.Errorf("negative Unattributed foreground = %v, want ColorSeverityMedium %v", got, orange)
	}
	if strings.Contains(h.Value, "\x1b") {
		t.Errorf("Value %q must be plain text: the color is the Style's", h.Value)
	}

	pos := atPricedAgents(t, cfgWith(t, pricesYAML), 160, sumUsage(billed(10)))
	for _, key := range []string{estKey, billedKey, unattributedKey} {
		h, _ := headerEntry(pos, key)
		if got := h.Style.GetForeground(); got == orange || got != normal {
			t.Errorf("%s foreground = %v, want the ordinary header value color %v", key, got, normal)
		}
	}
}

// Float noise below half a cent renders $0.00 and must not turn orange.
func TestFloatNoiseInUnattributedIsNotOrange(t *testing.T) {
	m := atPricedAgents(t, cfgWith(t, pricesYAML), 160, sumUsage(billed(8.20-1e-12)))
	h, ok := headerEntry(m, unattributedKey)
	if !ok {
		t.Fatal("no Unattributed entry")
	}
	if got, want := h.Style.GetForeground(), theme.HeaderValueStyle.GetForeground(); got != want {
		t.Errorf("Unattributed %q foreground = %v, want the ordinary color %v", h.Value, got, want)
	}
	m = atPricedAgents(t, cfgWith(t, pricesYAML), 160, sumUsage(billed(8.20-0.006)))
	h, _ = headerEntry(m, unattributedKey)
	if got := h.Style.GetForeground(); got != lipgloss.TerminalColor(theme.ColorSeverityMedium) {
		t.Errorf("Unattributed %q at -$0.01 foreground = %v, want orange", h.Value, got)
	}
}

// H7: Est. cost sums the visible rows, like Tokens; Billed and Unattributed
// are facts of the session and ignore the filter.
func TestEstCostFollowsTheFilterButBilledDoesNot(t *testing.T) {
	filtered := func(t *testing.T, m Model, query string) Model {
		t.Helper()
		m = send(t, m, testutil.Key("/"))
		for _, k := range testutil.Type(query) {
			m = feed(t, m, k)
		}
		return send(t, m, testutil.Key("enter"))
	}

	m := atPricedAgents(t, cfgWith(t, pricesYAML), 160, sumUsage(billed(10)))
	m = filtered(t, m, "coder")
	for key, want := range map[string]string{estKey: "~$1.20", billedKey: "$10.00", unattributedKey: "$1.80"} {
		if got := headerValue(t, m, key); got != want {
			t.Errorf("filter coder: %s = %q, want %q", key, got, want)
		}
	}

	// A filter hiding the unpriced row gives the sum of the rest.
	u := sumUsage(agentusage.Billed{})
	u.Agents = append(u.Agents, byModel(agentusage.Agent{ID: "a02", Type: "tester"},
		mu("model-z", agentusage.Tokens{Output: 5}, 0)))
	all := atPricedAgents(t, cfgWith(t, pricesYAML), 160, u)
	if got := headerValue(t, all, estKey); got != "-" {
		t.Errorf("unpriced row visible: Est. cost = %q, want -", got)
	}
	if got := headerValue(t, filtered(t, all, "orchestrator"), estKey); got != "~$7.00" {
		t.Errorf("filter hiding the unpriced row: Est. cost = %q, want ~$7.00", got)
	}
}

// The header holds headerMax lines (internal/app headerMinHeight). Before
// DEVDESK-47 the agents level used 3 (+ Idle); with Est. cost, Billed and
// Unattributed the worst case is exactly 7, so the plan (Q1 = A) leaves no
// room for another field.
func TestTheHeaderNeverExceedsSevenLines(t *testing.T) {
	tn := time.Now()
	u := sumUsage(billed(10))
	u.Agents[1] = stamped(withState(u.Agents[1], agentusage.StateUnfinished), 1, tn.Add(-time.Hour), tn.Add(-20*time.Minute))

	cfg := cfgWith(t, pricesYAML)
	m := atPricedAgentsAt(t, cfg, u, statusClock{now: tn, alive: true})
	if got := headerValue(t, m, "Idle"); got != "1" {
		t.Fatalf("precondition: Idle = %q, want 1", got)
	}
	keys := headerKeys(m)
	if len(keys) != headerMax {
		t.Errorf("worst case has %d entries %v, want exactly %d", len(keys), keys, headerMax)
	}
	if keys[len(keys)-1] != "Idle" {
		t.Errorf("Idle stays last, got %v", keys)
	}

	// Unpriced or unknown Billed only ever remove entries or keep the count.
	for name, mm := range map[string]Model{
		"no prices":  atPricedAgentsAt(t, nil, u, statusClock{now: tn, alive: true}),
		"no billing": atPricedAgentsAt(t, cfg, withBilled(u, agentusage.Billed{}), statusClock{now: tn, alive: true}),
	} {
		if n := len(mm.GetHeaderInfo("")); n > headerMax {
			t.Errorf("%s: %d header entries, max %d", name, n, headerMax)
		}
	}
}

func withBilled(u agentusage.Usage, b agentusage.Billed) agentusage.Usage {
	u.Billed = b
	return u
}

// atPricedAgentsAt is atPricedAgents with the activity clock set before the answer.
func atPricedAgentsAt(t *testing.T, cfg *config.Config, u agentusage.Usage, c statusClock) Model {
	t.Helper()
	isolate(t)
	m := feed(t, New(cfg), testutil.Resize(160, 20))
	s := session("id-a000000-0000", "/srv/pa", 0)
	s.Parent = "/p/a.jsonl"
	m = loaded(t, m, s)
	m, _ = opened(t, m, "right")
	m.clock = c
	return answer(t, m, u)
}

// A delta that brings the cost-state makes Billed appear with no ctrl+r, and a
// line written after it takes it away again (the session was resumed).
func TestBilledAppearsWhenTheCostStateIsAppended(t *testing.T) {
	m, parent := atFixture(t)
	if _, ok := headerEntry(m, billedKey); ok {
		t.Fatal("precondition: the fixture has no cost-state, so no Billed")
	}

	appendLine(t, parent, `{"type":"cost-state","totalCostUSD":10}`)
	m, msgs := pollTick(t, m, m.agentGen)
	reads := usageMsgs(msgs)
	if len(reads) != 1 {
		t.Fatalf("precondition: one read, got %+v", msgs)
	}
	m, _ = answered(t, m, reads[0])
	if got := headerValue(t, m, billedKey); got != "$10.00" {
		t.Errorf("Billed = %q after the cost-state arrived, want $10.00", got)
	}

	appendLine(t, parent, usageLine("msg_resumed", "model-alpha", 1, 2))
	m, msgs = pollTick(t, m, m.agentGen)
	reads = usageMsgs(msgs)
	if len(reads) != 1 {
		t.Fatalf("precondition: one read, got %+v", msgs)
	}
	m, _ = answered(t, m, reads[0])
	if v, ok := headerEntry(m, billedKey); ok {
		t.Errorf("a line after the cost-state: Billed = %q, want it gone (criterion 6)", v.Value)
	}
}

// ── Footer ───────────────────────────────────────────────────────────────────

const pricesKey = "agent_usage.prices"

func threeUnpriced() agentusage.Usage {
	return agentusage.Usage{Agents: []agentusage.Agent{
		byModel(agentusage.Agent{Main: true}, mu("model-a", agentusage.Tokens{Output: 1}, 0)),
		byModel(agentusage.Agent{ID: "a01", Type: "coder"}, mu("model-e", agentusage.Tokens{Output: 1}, 0)),
		byModel(agentusage.Agent{ID: "a02", Type: "tester"}, mu("model-c", agentusage.Tokens{Output: 1}, 0)),
		byModel(agentusage.Agent{ID: "a03", Type: "writer"}, mu("model-d", agentusage.Tokens{Output: 1}, 0)),
	}}
}

func TestFooterPriceHint(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		u        agentusage.Usage
		contains []string
		excludes []string
	}{
		{"empty table, session with tokens: say where to set prices", nil, sumUsage(billed(1)),
			[]string{pricesKey, "?"}, nil},
		{"default config is an empty table too", config.Default(), sumUsage(billed(1)),
			[]string{pricesKey}, nil},
		{"one model without a rate is named", cfgWith(t, pricesYAML),
			byModel1(sumUsage(billed(1)), "model-z"), []string{"model-z", pricesKey}, nil},
		{"three models without a rate: the first (sorted) and a count", cfgWith(t, pricesYAML), threeUnpriced(),
			[]string{"model-c", "2 more models", pricesKey}, []string{"model-d", "model-e"}},
		{"everything priced: nothing", cfgWith(t, pricesYAML), sumUsage(billed(1)), nil, []string{pricesKey}},
		{"empty table but no tokens at all: nothing to estimate, nothing said", nil,
			agentusage.Usage{Agents: []agentusage.Agent{{Main: true}, {ID: "a01", Type: "coder"}}}, nil, []string{pricesKey}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := atPricedAgents(t, tt.cfg, 160, tt.u)
			st := m.status()
			for _, w := range tt.contains {
				if !strings.Contains(st.Text, w) {
					t.Errorf("status %q lacks %q", st.Text, w)
				}
			}
			for _, w := range tt.excludes {
				if strings.Contains(st.Text, w) {
					t.Errorf("status %q must not contain %q", st.Text, w)
				}
			}
			if len(tt.contains) > 0 {
				if st.Level != sharedcomponents.LevelInfo || st.Spinner {
					t.Errorf("the hint is a plain Info line (nothing failed, nothing is waiting), got %+v", st)
				}
				if !strings.Contains(stripANSI(footerText(m)), tt.contains[0]) {
					t.Errorf("RenderFooter lacks %q:\n%s", tt.contains[0], footerText(m))
				}
			} else if st.Text != "" {
				t.Errorf("status = %+v, want none", st)
			}
		})
	}
}

// byModel1 returns u with one more sub-agent whose only tokens are under model.
func byModel1(u agentusage.Usage, model string) agentusage.Usage {
	u.Agents = append(append([]agentusage.Agent{}, u.Agents...),
		byModel(agentusage.Agent{ID: "a09", Type: "extra"}, mu(model, agentusage.Tokens{Output: 3}, 0)))
	return u
}

// Rule 128: the hint is a state derived on every frame, not a timed event.
func TestThePriceHintIsAStateNotAMessage(t *testing.T) {
	m := atPricedAgents(t, nil, 160, sumUsage(billed(1)))
	if !strings.Contains(m.status().Text, pricesKey) {
		t.Fatalf("precondition: no hint in %+v", m.status())
	}
	m = feed(t, m, sharedcomponents.ClearFooterMsg{ID: m.footer.ID()})
	if !strings.Contains(stripANSI(footerText(m)), pricesKey) {
		t.Errorf("the hint went away with a footer expiry, it is not a message:\n%s", footerText(m))
	}
	// Once priced it is gone on the very next rebuild: no stale text.
	priced := atPricedAgents(t, cfgWith(t, pricesYAML), 160, sumUsage(billed(1)))
	if priced.status().Text != "" {
		t.Errorf("status = %+v with every model priced", priced.status())
	}
}

func TestPriceHintPrecedence(t *testing.T) {
	u := sumUsage(billed(1))
	u.Diagnostics.UnreadableLines = 2

	m := atPricedAgents(t, nil, 160, u)
	if st := m.status(); st.Level != sharedcomponents.LevelWarning || !strings.Contains(st.Text, "approximate") {
		t.Errorf("degraded + hint = %+v, want the approximate warning first", st)
	}
	m.agentsLoading = true
	if st := m.status(); !st.Spinner || strings.Contains(st.Text, pricesKey) {
		t.Errorf("loading + hint = %+v, want the spinner line", st)
	}
	m.agentsErr = true
	if st := m.status(); st.Level != sharedcomponents.LevelError || strings.Contains(st.Text, pricesKey) {
		t.Errorf("error + hint = %+v, want the error", st)
	}
}

func TestThereIsNoPriceHintAtTheSessionsLevel(t *testing.T) {
	m := atPricedAgents(t, nil, 160, sumUsage(billed(1)))
	if !strings.Contains(m.status().Text, pricesKey) {
		t.Fatal("precondition: the agents level shows the hint")
	}
	back := feedBack(t, m)
	if strings.Contains(back.status().Text, pricesKey) {
		t.Errorf("sessions level status = %+v, want no price hint", back.status())
	}
	if st := withSessions(t, "/p/a.jsonl").status(); strings.Contains(st.Text, pricesKey) {
		t.Errorf("sessions level status = %+v, want no price hint", st)
	}
}

// ── Keys (Rule 130) ──────────────────────────────────────────────────────────

func TestCostFieldsAddNoShortcutAndChangeNoText(t *testing.T) {
	want := []string{"ctrl+r", openKey, closeKey, "/", ".", "?"}
	for name, m := range map[string]Model{
		"no prices":   atPricedAgents(t, nil, 160, sumUsage(billed(10))),
		"priced":      atPricedAgents(t, cfgWith(t, pricesYAML), 160, sumUsage(billed(10))),
		"not billed":  atPricedAgents(t, cfgWith(t, pricesYAML), 160, sumUsage(agentusage.Billed{})),
		"unpriced":    atPricedAgents(t, cfgWith(t, pricesYAML), 160, threeUnpriced()),
		"stale table": atPricedAgents(t, cfgWith(t, pricesYAML), 160, sumUsage(billed(7.8))),
	} {
		if got := testutil.ShortcutKeys(m.GetShortcuts()); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: keys %v, want %v", name, got, want)
		}
	}
	descriptions := map[string]string{
		"ctrl+r": "Refresh", openKey: "Open agents", closeKey: "Go back", "/": "Filter", ".": "Sort", "?": "Help",
	}
	for _, sc := range atPricedAgents(t, nil, 160, sumUsage(billed(10))).GetShortcuts() {
		if sc.Description != descriptions[sc.Key] {
			t.Errorf("%s description = %q, want %q (unchanged)", sc.Key, sc.Description, descriptions[sc.Key])
		}
	}
}

// ── formatUSD, review carry-overs from step 5 ────────────────────────────────

func TestFormatUSDDegenerateAmounts(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want string
	}{
		{"NaN", math.NaN(), "-"},
		{"+Inf", math.Inf(1), "-"},
		{"-Inf", math.Inf(-1), "-"},
		{"negative under half a cent", -0.004, "\u2212<$0.01"},
		{"negative tiny", -1e-9, "\u2212<$0.01"},
		{"negative half a cent", -0.005, "\u2212$0.01"},
		{"negative whole dollars", -1234.5, "\u2212$1235"},
		{"negative saturated", -123456, "\u2212$99999+"},
		{"largest finite float saturates", math.MaxFloat64, "$99999+"},
		{"negative largest finite float", -math.MaxFloat64, "\u2212$99999+"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatUSD(tt.in)
			if got != tt.want {
				t.Errorf("formatUSD(%v) = %q, want %q", tt.in, got, tt.want)
			}
			if strings.ContainsAny(got, "NI") || strings.Contains(got, "\x1b") {
				t.Errorf("formatUSD(%v) = %q leaks a float spelling or ANSI", tt.in, got)
			}
		})
	}
}

// ── Help (Rule 114) ──────────────────────────────────────────────────────────

func costsHelp(t *testing.T) string {
	t.Helper()
	for _, s := range New(nil).GetHelpContent().Sections {
		if s.Title == "Costs" {
			return s.Body
		}
	}
	t.Fatal(`help needs a section titled "Costs"`)
	return ""
}

func TestHelpDescribesCosts(t *testing.T) {
	body := costsHelp(t)
	lower := strings.ToLower(body)
	for _, want := range []string{
		"est. cost", "estimate", "~", "\u2265 ~", // the estimate mark and the sub-agent floor
		"dash",                               // an unknown rate shows a dash, never zero
		"billed", "unattributed", "negative", // criterion 5, and the stale-table signal
		"no tokens",              // no agent with tokens: Unattributed is the whole bill
		"web search",             // what is never priced (H9)
		pricesKey, "per million", // where, and in what unit
		"input", "output", "cache_write_5m", "cache_write_1h", "cache_read", // the five rates
		"case-insensitive", "yyyymmdd", // the matching rule
		"restart", "context", // when prices are read again
	} {
		if !strings.Contains(lower, strings.ToLower(want)) {
			t.Errorf("the Costs section should mention %q:\n%s", want, body)
		}
	}
	if !strings.Contains(lower, "ctrl+r") {
		t.Errorf("the Costs section should say ctrl+r does not re-read the prices:\n%s", body)
	}
}

// The example is meant to be pasted: it must load, and every rate must be a number.
func TestTheHelpYAMLExampleLoadsAndIsComplete(t *testing.T) {
	var block []string
	in := false
	for _, line := range strings.Split(costsHelp(t), "\n") {
		switch {
		case strings.HasPrefix(line, "agent_usage:"):
			in = true
			block = append(block, line)
		case in && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")):
			block = append(block, line)
		case in:
			in = false
		}
	}
	if len(block) == 0 {
		t.Fatal("the Costs section needs a YAML example starting at a line `agent_usage:`")
	}
	var doc struct {
		AgentUsage config.AgentUsageConfig `yaml:"agent_usage"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(block, "\n")), &doc); err != nil {
		t.Fatalf("the example is not valid YAML: %v\n%s", err, strings.Join(block, "\n"))
	}
	if len(doc.AgentUsage.Prices) == 0 {
		t.Fatalf("the example holds no model:\n%s", strings.Join(block, "\n"))
	}
	table, problems := pricing.FromConfig(doc.AgentUsage.Prices)
	if len(problems) > 0 || table.Len() != len(doc.AgentUsage.Prices) {
		t.Errorf("pasting the example gives problems %+v and %d usable models of %d (all five rates are required)",
			problems, table.Len(), len(doc.AgentUsage.Prices))
	}
}

// Review carry-over: the narrow-terminal sentence of the help must follow the
// real drop order, which now includes Est. cost (right after Cache write).
func TestHelpDropOrderMentionsEstCost(t *testing.T) {
	var body string
	for _, s := range New(nil).GetHelpContent().Sections {
		if s.Title == "Agent columns" {
			body = s.Body
		}
	}
	i := strings.Index(body, "On a narrow terminal")
	if i < 0 {
		t.Fatalf("the Agent columns section lost its narrow-terminal sentence:\n%s", body)
	}
	sentence := body[i:]
	write, est, read := strings.Index(sentence, "Cache write"), strings.Index(sentence, "Est. cost"), strings.Index(sentence, "Cache read")
	if est < 0 || write < 0 || read < 0 || write >= est || est >= read {
		t.Errorf("drop order must read Cache write, Est. cost, Cache read (got %d, %d, %d):\n%s", write, est, read, sentence)
	}
	if !strings.Contains(body, "Est. cost:") && !strings.Contains(body, "Est. cost,") {
		t.Errorf("the Agent columns section should describe the Est. cost column:\n%s", body)
	}
}
