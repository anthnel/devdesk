# DEVDESK-151 (A-13) — `:agent-usage`: activity column + Est. cost colouring

Model: DEVDESK-150 (#342, netdiag ports, new column) and DEVDESK-149 (templates, value-receiver view).
Package: `internal/ui/agentusageview` (value receivers: `func (m Model)`).

## Blocking question (Q1) — the 80-column layout

The new column costs 4 cells (2 + padding 2). Every non-yielding threshold moves +4:
State 63→67, Tools 70→74, **Duration 80→84**, Model 94→98, Breakdown 106→110, In 114→118,
Cache read 126→130, Est. cost 138→142, Cache write 151→155 (to be measured, not assumed).
Consequence: at 80 columns Duration disappears, which breaks DEVDESK-46 criterion 1
(`TestEveryAgentLineShowsStateToolsAndDurationAt80Columns`, `TestEstCostLayoutAtEachWidth` row 80).

- A. Accept it: at 80 → dot, Agent, ID, State, Tools, Out, Total, Share. Duration lost; the state is shown twice (dot + State).
- B. (recommended) Move the State column right of Duration (Agent, ID, Est. cost, Tools, Duration, State, Model, …).
  Drop order becomes …, Model, State, Duration, Tools: at 80 → dot, Agent, ID, Tools, Duration, Out, Total, Share.
  The dot carries the state (the story's own hypothesis: the dot matters on a narrow terminal, it repeats State on a wide one).
  State text column is kept (criterion 4). Cost: column order changes vs ux.md §3.4 mockup (120 cols).
- C. Keep everything and narrow something else (no candidate: Duration's MinWidth is its title).

## Hypotheses (prudent defaults, flag in PR)
- H1 Cost share denominator = sum of `Priced` estimates of **all** agents of the session (not the filter, not Billed).
  Unpriced/NoTokens agents excluded. "≥2 priced agents" = count of `Status == Priced`. Denominator ≤ 0 → no colour.
- H2 Footer sentences (ux.md §1.2 structure, times via `theme.TimeAgo`, Rule 127):
  running `Agent running — last transcript line <TimeAgo>`; idle `Agent idle — last transcript line <TimeAgo>`;
  done `Agent done — ran <formatDuration>` (just `Agent done` when no timestamp);
  unknown `Agent state unknown — transcript has no end marker` (sub-agent) / `Agent state unknown — updates are paused` (orchestrator).
- H3 Detail only on the Agents tab with a selected row; Breakdown tab and sessions level unchanged.
- H4 Blink only for a running row among `agentTable.Visible()`, level agents, tab Agents.
- H5 Risk, not a blocker: in a typical session the orchestrator holds ≥75 % of the cost, so it will often be orange/red (D6, user's call).

## Step 1 — state → indicator, cost share (pure)  [S]
Files: `status.go`, `agents.go`, `cost.go`; tests `status_test.go`, `cost_test.go`.
- `func (s runState) indicator() indicator.State` : running→Running, idle→Suspended, done→Done, unknown→Unknown.
- `agentRow` gains `costPct float64` and `costRanked bool`, set once in `agentRows` (never in a Cell):
  `func costShares(es []pricing.Estimate) (pct []float64, ranked bool)`.
- Tests: table on the 4 states (4 distinct `indicator.Glyph(…, true)`, none `●` except running; `○` off phase only for running);
  costShares: 0 priced → ranked=false; 1 priced → false; 2 priced 80/20 → true, [80,20]; unpriced agent excluded from the denominator;
  all priced at $0 → false; negative/NaN never panics.
Done when: `go test ./internal/ui/agentusageview/ -run 'State|CostShare'` green.

## Step 2 — activity column + layout recalibration  [M]  (depends on 1; decision Q1)
Files: `agents.go` (`agentColumns`: column 0 = `datatable.IndicatorColumn(func(r agentRow) indicator.State { return r.state.indicator() }, nil, nil)`;
if Q1=B move `stateColumn()` after `durationColumn()`), `help.go` (narrow-terminal drop-order sentence),
`docs/architecture/agent-usage.md` (columns table, "Column order and width", every threshold, the 80/60 layouts, Breakdown "Visible from …").
Tests to update/write (measure each switch width, do not add 4 blindly):
- `cost_test.go` `TestEstCostLayoutAtEachWidth` + its comment; `breakdown_column_test.go` threshold table (~l.217-234, `106` loop l.299);
  `activity_columns_test.go` 80/60 tests; `yield_test.go`; `agents_level_test.go` `TestTwoCodersStayDistinctOnANarrowTerminal` must still pass.
- New: `header()` helper must ignore the untitled column; column 0 untitled, `SizingFixed`, `IconColumnWidth`, no Less/Search;
  at 60 cols State absent and each row starts with its glyph (◐ for idle, ◉ done, ◌ unknown, ● running);
  Rule 116 at 50..230 via `RenderedWidth()`, also with `runewidth.EastAsianWidth = true`.
Done when: `go test ./internal/ui/agentusageview/...` green.

## Step 3 — blink (router frame)  [S]  (depends on 2)
Files: new `internal/ui/agentusageview/activity.go`, `update.go`.
- `func (m Model) ActivityTables() map[string]datatable.ActivityTable { return map[string]datatable.ActivityTable{"agent-usage/agents": &m.agentTable} }`
- `func (m Model) WantsBlink() bool` — H4.
- `update.go`: `case jobs.FrameMsg: m.agentTable.SetBlinkPhase(msg.Blink); return m, nil`.
Tests (package): WantsBlink true with a running agent on Agents tab; false on Breakdown tab, at sessions level, with only idle/done/unknown,
when the running row is filtered out; FrameMsg{Blink:false} → row shows `○`, true → `●`;
handlePollTick / applyRead never return an animation tick (`grep tea.Tick` stays empty; guard covers it).
New `internal/app/agent_usage_activity_test.go`: transcript on disk (pattern `TestAgentUsageTrailSitsOnTheTopBorder`, CLAUDE_CONFIG_DIR,
line with a fresh `timestamp`), open the session, `held.WantsBlink()` true, `jobs.FrameMsg` on/off flips the glyph (model: `netdiag_activity_test.go`);
with `a.motionStatic = true`, `ensureTick()` returns nil.
Done when: `go test ./internal/ui/agentusageview/... ./internal/app -run 'AgentUsage|Animation|Activity'` and `mise run test-race` on `internal/app` green.

## Step 4 — footer detail + help legend  [S]  (depends on 2; parallel with 3 if same branch rebased)
Files: `view.go` (`agentsStatus`), new `func (m Model) agentDetail() sharedcomponents.RowDetail` in `activity.go`, `help.go`.
- `st := …; st.Detail = m.agentDetail()` like `PortsModel.statusLine`; detail via
  `components.DescribeRow(components.RowQuery{Observed: r.state.indicator(), ObservedText: sentence}).Detail` (H2, H3).
- `help.go`: add `indicator.Legend(indicator.AgentsLegend...)` section (the extension already exists in `indicator/legend.go`);
  State paragraph says the dot repeats it.
Tests: each state → footer starts with its glyph + sentence (ansi-stripped); error/loading still win (Level error kept);
no detail on Breakdown tab; help has "Activity indicator" section with `◐ idle (no activity for 5 min)`; `GetShortcuts()` unchanged (Rule 130).

## Step 5 — Est. cost colouring  [S]  (depends on 1; parallel with 3/4)
File: `cost.go` `costColumn().Style`:
`!Priced → theme.DimStyle`; `!r.costRanked || r.costPct < theme.LoadWarnPercent → lipgloss.NewStyle()`;
else `theme.LoadTextStyle(r.costPct)`. **Never call LoadTextStyle below 75: it returns green (ColorOK).**
Tests: 80/20 → orchestrator cell Style fg = ColorSeverityMedium, 95/5 → ColorSeverityCritical, 60/40 → no fg, one priced agent at 100 % → no fg,
`-` dim; Cell text unchanged (plain, Rule 122); Style stays when the row is selected (normal selection); help "Costs" section mentions the colours.
Docs: agent-usage.md "Est. cost" paragraph (D6 exception, not an activity colour).

## Step 6 — guard + docs  [S]  (last, after 2-5)
- `internal/app/activity_guard_test.go`: `migratedTables[command.ViewAgentUsage] = {"agent-usage/agents"}`; drop the A-13 pendingTables entry;
  `maxPendingTables` 6→5; `migratedSources[command.ViewAgentUsage] = {"ui/agentusageview/"}` (no tea.Tick in the package: pollTickCmd sleeps in a Cmd);
  `migratedFooterSeeds[command.ViewAgentUsage] = seedAgentUsageFooter` (transcript on disk, returns `indicator.Running`; run Cmds but never the 2 s poll Cmd).
- `animation_guard_test.go`: nothing to remove (no declared chain for agent-usage).
- Docs same commit: `app-shell.md` l.993 (6 to 5 with `agent-usage/agents`, DEVDESK-151), `agent-usage.md` (columns, WantsBlink, footer, colour),
  `ui-components.md` if it lists the migrated views, `stories.md` A-13 status, `.claude/CLAUDE.md` overview line for :agent-usage (dot column, cost colour).
Done when: `go test ./internal/app -run 'Activity|Animation|AgentUsage'`, `mise run check`, `mise run test-race` green.
