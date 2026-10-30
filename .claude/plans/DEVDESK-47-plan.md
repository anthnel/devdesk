# Agent usage S-07: indicative cost per agent (DEVDESK-47)

Story (S-07, `docs/product/agent-usage-monitor/stories.md`):

1. Agent + model → cost shown **marked as an estimate** (tokens × the model's rates).
2. Model with no known rate → the cell shows `-`, never zero.
3. The price table can be changed without recompiling (configuration).
4. Sub-agent cost is announced as **under-estimated** (output is a floor; some API
   calls are not in the transcripts: `cache_read` 24–32 % higher in `cost-state`).
   The cell keeps the estimate marker of criterion 1.
5. Finished session with a `cost-state` → the session's billed total, plus an
   "Unattributed" figure = billed − Σ agent costs (10.00 − 8.20 = 1.80).
6. Running session (no `cost-state`) → estimated agent costs only, no billed
   total, no Unattributed.

Out of scope: exact attribution of the bill per agent, quota, **automatic price
updates** (another story), **an editor in `:cfg`** (user decision).

User decisions (not negotiable): prices live in the YAML under
`agent_usage.prices`, keyed by model, with five distinct rates (input, output,
cache write 5 min, cache write 1 h, cache read). **No price is compiled into the
binary.** No `:cfg` editor.

## What exists today (checked on `main` @ 0026b6b)

- **Nothing reads `cost-state`.** `grep -rn 'cost-state\|costUSD\|modelUsage'
  --include=*.go` is empty; no fixture holds one. A `cost-state` line has no
  `message`, so `decodeLine` classifies it `kindIgnored`, and since it has no
  `timestamp` it does not even make a delta non-`Empty()`.
- `internal/agentusage` decodes four counters only (`wireUsage`): the 5 min / 1 h
  split of `cache_creation_input_tokens` is **not** read. `Agent.Tokens` is one
  sum over every model the agent used; `Agent.Models` is the list of names. No
  per-model token figure exists anywhere: **cost cannot be computed from today's
  `Usage`** for an agent that used two models, nor for any cache write.
- `Tokens{…}` is written **unkeyed** 30 times in 6 test files: adding a field to
  `Tokens` breaks compilation there. The breakdown therefore goes beside it
  (`ModelUsage`), not into it.
- `agentusageview.New(_ *config.Config)` ignores its config. The router builds the
  view with `a.config` (`internal/app/command_line.go:226`) and rebuilds every
  view except `:cfg` on `ConfigSavedMsg` and on a context switch.
- `config.SaveContext` is `yaml.Marshal(cfg)`: **comments in a context file are
  lost on the first save** from `:cfg`. A type error anywhere in the file (for
  example `input: "5$"`) makes `LoadContext` return `failed to parse context`,
  i.e. the whole context fails to load.
- The header holds at most `headerMinHeight = 7` info lines
  (`internal/app/app_header.go`); the agents level already uses 3, plus `Idle`
  when non-zero.

### What the real transcripts say (structure and numbers only, this machine, 49 parent transcripts)

- **`cost-state` exists in 46 of 49 parents, never in a sub-agent file, and is
  always the last line of the file** (46/46). 33 files have one, 11 have two
  identical ones, 2 have four whose `totalCostUSD` grows (7.08 → 12.61; 70.58 →
  72.60): a resumed session appends a new, **cumulative** `cost-state` at each
  close. `sessionId` equals the file's ID in 46/46.
- Keys: `type`, `sessionId`, `totalCostUSD`, `startTime`, `totalDuration`,
  `totalAPIDuration…`, `totalLinesAdded/Removed`, `totalToolDuration`, and in 40
  of 46 `modelUsage.<model>.{inputTokens, outputTokens, cacheReadInputTokens,
  cacheCreationInputTokens, thinkingTokens, webSearchRequests, costUSD}`. No
  `timestamp`. Only `type`, `sessionId` and `totalCostUSD` are needed.
- `modelUsage` names a model that **never appears in any transcript**
  (`claude-haiku-4-5-20251001`, in 21 sessions: Claude Code's background calls).
  That cost can only ever land in Unattributed — which is what criterion 5 is for.
- Model ids in transcripts: `claude-sonnet-5-5` (5699 lines), `claude-opus-5-5`
  (5314), `claude-sonnet-5` (1348), `claude-haiku-5-5` (124), `<synthetic>` (15).
  None dated. **`claude-sonnet-5` is a prefix of `claude-sonnet-5-5`.**
- `usage.cache_creation.ephemeral_{5m,1h}_input_tokens` is present on **all**
  12502 usage lines, and 5m + 1h = `cache_creation_input_tokens` on all of them.
  **1 h writes are 47 % of cache writes** (48.9 M of 103.2 M): pricing every
  write at one rate would be wrong by a large factor, so the split is required.

## Questions bloquantes

**Q1 — "une ligne Unattributed" : ligne d'en-tête ou ligne du tableau ?**
(changes step 6 only; steps 1–5 can start)

| Option | Cost | Trade-off |
|---|---|---|
| **A. Header lines** `Billed › $10.00` and `Unattributed › $1.80` (recommended) | S | No change to `datatable`, sorting, Share (still 100 %), the `Agents` count, selection keys, Rule 130 key set. **The header is then full**: worst case Agents, Tokens, Est. cost, Billed, Unattributed, Updates, Idle = 7/7 lines; any later header field forces a choice. |
| B. A pseudo-row `unattributed` pinned under the agents | +M | Reads like the bottom of the Cost column. `datatable` has no pinned row: `sorted()` applies one comparator in both directions, so "always last" needs a new `Config.Pinned func(T) bool` in a shared component (and tests across it), plus exclusions in `Agents`, `Idle`, Share, `agentKeys`, `populationOf`, and the filter. |

Recommendation **A**. The plan below is written for A; B replaces step 6's
header part with a step 6b (datatable `Pinned`, then the row).

**Q2 — Validate the three choices you asked me to quantify** (the plan is
written for the recommendation in each; none blocks steps 1–3).

**(a) How the YAML is filled when the binary carries no price.**

| Option | Numbers | Verdict |
|---|---|---|
| **A. Empty table by default; the Est. cost cells show `-`; the footer names the key; the help and `agent-usage.md` give a ready-to-paste example** (recommended) | 0 prices in the binary. Effort S (≈ ½ day, inside steps 6–7). The user pastes 5 rates × the models used — 4 models here, so 20 numbers. **Billed works at once with no price at all**: 46/49 sessions here have a `cost-state`. | Matches the decision; nothing goes stale in the binary. Cost: nothing is estimated until the user acts — the footer line is what makes that discoverable. |
| B. Write a pre-filled block when a context is created (`Default()` / `CreateContext`) | Values compiled into the binary; reaches **0 of the 2** existing context files here (only new contexts), unless a migration writes into user files | Contradicts the decision; stale with no update story. Rejected. |
| C. A commented example block in the file | Wiped by the first `:cfg` save (`SaveContext` = `yaml.Marshal`, comments dropped — read in the code) | Does not survive. Rejected. |
| D. Derive rates from `cost-state.modelUsage.costUSD` | — | That is the automatic-update story. Not here. |

Consequence of the decision to keep: `agent_usage` sits in the **context** file,
so each context needs its own block (2 files here). The alternative — a global
`~/.devdesk/agent-prices.yaml`, as `trust.yaml` is for signatures — would be a
change to your decision; I do not recommend reopening it for 2 contexts.

**(b) Matching a model to a price key.** Recommended: **exact match, case-
insensitive and trimmed; failing that, the same match after removing one
trailing `-YYYYMMDD` (8 digits) from the transcript's model id.** Nothing else.

- **No prefix matching**: measured collision here — with only a
  `claude-sonnet-5` key, prefix matching would price the 5699
  `claude-sonnet-5-5` lines at Sonnet 5's rate, silently. Longest-prefix only
  helps when both keys exist, i.e. exactly when it is not needed.
- **Dated fallback**: today 0 dated ids in transcripts, 1 in `cost-state`
  (`claude-haiku-4-5-20251001`). It lets one key `claude-haiku-4-5` cover a
  snapshot id. An exact dated key wins over the fallback.
- **Not stripped**: `[1m]` (a long-context variant can carry a different rate)
  and provider spellings (`us.anthropic.…-v1:0`, `…@2025…`): none observed; the
  user adds them as keys if they appear.
- A model with no match → that agent's cell is `-` and the footer names it.

**(c) Money format.** Recommended:

| Case | Cell |
|---|---|
| orchestrator, priced | `~$1.23` (`~` = estimate, criterion 1) |
| sub-agent, priced | `≥ ~$0.42` (`≥ ` = floor, same mark as Out, criterion 4; always, not only when `PartialOutput()`) |
| 0 < cost < $0.005 | `~<$0.01` (never `$0.00` for a non-zero cost) |
| $1000 and up | whole dollars, `~$1234`; past $99 999, `~$99999+` |
| any token class of any model of the agent unpriced | `-` (dim), criterion 2 |
| agent with no tokens | `-` (dim), as Share does |

USD only (`cost-state` is USD, rates are USD), `$` prefix, two decimals, no
thousands separator (the token columns are locale-free too). Column title
`Est. cost`, fixed width 10 (`≥ ~$999.99`). Billed has **no** `~` (it is Claude
Code's own figure). A negative Unattributed (the price table is higher than
what was recorded) is shown signed with U+2212, `−$0.40`, in the MEDIUM orange:
it is the one signal that the table is stale. Arithmetic in `float64`, rounded
only for display; cells rounded independently may not add up to the cent (the
help says so, as it does for Share).

## Hypotheses (prudent choices, reviewable, no answer needed to start)

- **H1 — Rates.** USD per million tokens, the unit of Anthropic's price page.
  Keys `input`, `output`, `cache_write_5m`, `cache_write_1h`, `cache_read`.
  **All five are required**: an entry missing one is ignored with a WARN log, so
  a forgotten `cache_read` cannot under-price silently (D12 shape: absent ≠ 0).
  `0` is a valid explicit rate.
- **H2 — A typo costs its entry, not the context.** Rates decode through a
  tolerant `config.Rate` (custom `UnmarshalYAML`): a value that is not a number
  marks that rate invalid and keeps its raw text so a `:cfg` save writes it back
  unchanged. Today a type error fails the whole context load (`LoadContext`).
- **H3 — Partial pricing is `-`.** An agent with tokens under one unpriced model
  shows `-`, not the priced part: a partial sum under `~` would under-state with
  no mark.
- **H4 — Reload = restart or context switch.** `ctrl+r` does not re-read the
  price table. Reason: the router holds one in-memory `Config`; a hand edit made
  while DevDesk runs is **overwritten by the next `:cfg` save** (existing
  behaviour for every key, but prices have no other edit path). Re-reading on
  `ctrl+r` would make the edit look applied right before a save erases it. The
  help says "edit the file while DevDesk is closed, or restart".
- **H5 — `Billed` is valid only if the parent's last complete line is a
  `cost-state`** (greatest position, `sessionId` equal to the session's or
  absent, `totalCostUSD` a finite number ≥ 0). A resumed session that writes
  again loses it until it closes again (criterion 6 holds for resumes). A
  position rule keeps the incremental oracle (= `ParseSession`) intact.
- **H6 — Unattributed is known only when Billed is known and every agent with
  tokens is priced**; otherwise `Unattributed › -`. Billed alone still shows.
- **H7 — Header scope.** `Est. cost` sums the visible rows (it follows the filter,
  like `Tokens`); `Billed` and `Unattributed` are session facts and do not
  (like `Idle`).
- **H8 — Column placement: `Est. cost` right after `ID`, `Optional`.** Optional
  columns drop rightmost first, so this makes it the last optional to go.
  Effect (to be pinned by the tester from the real layout): at 100 columns it
  costs `Model`, at 80 `Duration`; at 60 the five DEVDESK-43 columns stay.
  Placed after `Total` instead, it would be the second column dropped and
  invisible under about 110 columns. One-line change if UX prefers the other.
- **H9 — Not priced, so in Unattributed by construction**: web search requests
  (`server_tool_use`), the `speed` / `service_tier` / `inference_geo` modifiers,
  long-context premiums. Stated in the help.
- **H10 — Header key `Billed`** as the story says; the help adds that it is
  Claude Code's own recorded cost (what `/cost` shows), which on a subscription
  plan is not an invoice.

## Options compared (where the estimate is computed)

| Option | Pros | Cons |
|---|---|---|
| **A. `internal/agentusage` exposes per-model tokens and the 1 h split; a new `internal/agentusage/pricing` package turns them into money** (recommended) | Parent package keeps "no price computation"; pricing is pure and table-tested; the view only formats | Two packages touched; `Agent` gains a slice |
| B. Compute cost inside `agentusage` (`Agent.Cost`) | One package | Price table enters the data layer and the incremental oracle; every `ParseSession` caller needs a table |
| C. Compute in the view from `Agent.Tokens` | No package change | Impossible correctly: no per-model tokens, no 1 h split (47 % of writes) |

## Design

### `internal/agentusage` (additive; the contract says add, do not rename)

```go
// ModelUsage is the part of an agent's tokens recorded under one model.
type ModelUsage struct {
    Model        string // "" when the lines named none, or past maxModelsPerAgent
    Tokens       Tokens
    CacheWrite1h int64  // part of Tokens.CacheWrite written with a 1-hour TTL
}
func (m ModelUsage) CacheWrite5m() int64 // Tokens.CacheWrite - CacheWrite1h

type Agent struct { …; ByModel []ModelUsage } // first-appearance order, "" bucket last; Σ = Tokens

// Billed is the session cost Claude Code recorded itself in its last cost-state line.
type Billed struct {
    USD   float64
    Known bool // false while the session runs, or when lines follow the cost-state
}
type Usage struct { …; Billed Billed }
```

Decoding (`record.go`): `wireUsage.CacheCreation *struct{ Ephemeral1h
json.RawMessage `json:"ephemeral_1h_input_tokens"` }`; `wireLine` gains
`SessionID` and `TotalCostUSD` (`json.RawMessage`). A line whose root `type` is
`cost-state` is a new `kindCostState`, carrying `CostUSD float64, CostOK bool,
CostSession string`. `record.CacheWrite1h = min(parsed, CacheWrite)`; a bad
split value is 0 and **never** makes the line unreadable (the token totals, and
the ccusage 2 % check, must not move).

Table (`table.go`): `entry.cacheWrite1h` (last line wins with the rest);
`spill` keeps `byModel map[string]ModelUsage`; `finishWith` builds `ByModel`.
New fields `billed *billedAt{p pos; usd float64; ok bool; session string}`
(greatest position among parent `cost-state` lines) and `parentEnd pos`
(greatest position of any complete parent line, readable or not, oversized
included). Merge = max of each: commutative, so the oracle holds.
`hasLines()` also counts `billed != nil`, so the delta that brings the
`cost-state` is non-empty and the view rebuilds. `Aggregate.Usage()` sets
`Billed.Known = billed != nil && billed.ok && billed.p == parentEnd &&
(billed.session == "" || billed.session == a.sessionID)`.

Privacy: of a `cost-state` line only `type`, `sessionId`, `totalCostUSD` are
declared; `modelUsage` and the line counters are not.

### `internal/config` — `agent_usage` section

```go
type Config struct { …; AgentUsage AgentUsageConfig `yaml:"agent_usage,omitempty"` }

// AgentUsageConfig holds the :agent-usage settings. Prices is empty by
// default: DevDesk ships no price (DEVDESK-47).
type AgentUsageConfig struct {
    Prices map[string]ModelPrice `yaml:"prices,omitempty"` // model id → rates
}
type ModelPrice struct {
    Input        Rate `yaml:"input,omitempty"`
    Output       Rate `yaml:"output,omitempty"`
    CacheWrite5m Rate `yaml:"cache_write_5m,omitempty"`
    CacheWrite1h Rate `yaml:"cache_write_1h,omitempty"`
    CacheRead    Rate `yaml:"cache_read,omitempty"` // USD per million tokens, all
}
type RateState int // RateMissing, RateInvalid, RateSet
type Rate struct{ /* value float64; state RateState; raw string */ }
func (r Rate) Value() (float64, RateState)
func (r *Rate) UnmarshalYAML(n *yaml.Node) error // never fails: !!int/!!float → set, else invalid + raw
func (r Rate) MarshalYAML() (any, error)        // the number, or the raw text back
func (r Rate) IsZero() bool                     // missing: omitempty drops it
```

**No migration and no default**: a new section, nothing renamed; `applyDefaults`
and `Default()` write nothing for it, so a file without the section saves
without it. File format becomes user-written: renaming a key later needs a
`migrateX` in `applyDefaults` like `gitlab:` → `forge:`.

Example (documentation only, values filled from the vendor's page, dated):

```yaml
agent_usage:
  prices:                 # USD per million tokens
    claude-opus-5-5:
      input: …
      output: …
      cache_write_5m: …
      cache_write_1h: …
      cache_read: …
```

### `internal/agentusage/pricing` (new, imports `agentusage` and `config`)

```go
type Rates struct{ Input, Output, CacheWrite5m, CacheWrite1h, CacheRead float64 } // USD/MTok
type Problem struct{ Key, Reason string } // for a WARN log line; Key is the user's own config key
type Table struct{ /* rates map[string]Rates, normalised keys */ }

func FromConfig(prices map[string]config.ModelPrice) (Table, []Problem)
func (t Table) Len() int
func (t Table) Lookup(model string) (Rates, bool) // exact (case-insensitive, trimmed), then without one trailing -YYYYMMDD
func (r Rates) Cost(m agentusage.ModelUsage) float64

type Status int // NoTokens, Priced, Unpriced
type Estimate struct {
    USD      float64
    Status   Status
    Unpriced []string // models with tokens and no rate ("" shown as "(no model)")
}
func (t Table) Agent(a agentusage.Agent) Estimate
func Sum(es []Estimate) Estimate // Unpriced if any is; NoTokens if all are

type Summary struct {
    Agents            []Estimate // same order as Usage.Agents
    Billed            agentusage.Billed
    Unattributed      float64
    UnattributedKnown bool
    Unpriced          []string // sorted, distinct, over the whole session
}
func (t Table) Session(u agentusage.Usage) Summary
```

`FromConfig` rejects: a rate missing (`incomplete: missing cache_read`),
invalid, negative, NaN or ±Inf; two keys equal after normalisation (both dropped:
choosing one would be a guess).

### View `internal/ui/agentusageview`

- `Model.prices pricing.Table`, built in `New(cfg)` (nil cfg → empty table);
  each `Problem` logged once: `log.Printf("WARN [agentusageview] agent_usage.prices %q: %s", key, reason)`.
- `agentRow.cost pricing.Estimate`; `agentRows(u, tree, c, prices)` fills it
  (both callers: `withRows`, `cycleAgentSort`). `withRows` also stores
  `m.cost pricing.Summary`. The cost is computed in `Update`, never in a `Cell`.
- `cost.go`: `formatUSD(v float64) string`, `costCell(r agentRow) string`,
  `costColumn() datatable.Column[agentRow]` (`SizingFixed`, `MinWidth: 10`,
  `Optional`, no `Less`: `.` keeps its cycle). Cell plain text (Rule 122);
  `Style` dim for `-`.
- Header (Q1 = A): `Agents`, `Tokens`, `Est. cost` (sum of visible rows,
  `-` if any is unpriced), `Billed` and `Unattributed` only when `Billed.Known`,
  `Updates`, `Idle` (unchanged rule). Max 7 = `headerMinHeight`.
- Footer status, after the existing error > loading > degraded warning, Info:
  - table empty and the session has tokens: `Set agent_usage.prices in this context's config file to estimate costs — see ?`
  - table not empty, some model unpriced: `No price for <model> in agent_usage.prices — its cost shows -`
    (`… for <model> and N more models …` past one). Model labels are already sanitized.
- No new key: Rule 130's key set is unchanged.

## Steps

Each step is delivered and tested alone (TDD: the tester writes the listed tests
first). Commands: `go test ./<pkg>`, `mise run test-race` for steps 1–2,
`mise run lint` before each commit.

### 1. Per-model tokens and the 1 h cache split (M)

Files: `internal/agentusage/{record.go, table.go, usage.go}`; tests in
`record_test.go`, `table_test.go`, `parse_test.go`, `fixture_activity_test.go`
(their `want` structs gain `ByModel`), the random-session generator and
`incremental_fuzz_test.go`.

Tests (input → expected):
- usage `cache_creation_input_tokens: 300`, `cache_creation: {ephemeral_5m_input_tokens: 100, ephemeral_1h_input_tokens: 200}` → `CacheWrite 300`, `CacheWrite1h 200`, `CacheWrite5m() 100`.
- no `cache_creation` → `CacheWrite1h 0`.
- 1h = 500 with total 300 → clamped to 300.
- 1h `-1`, `1.5`, `"x"`, `null` → 1h 0, line still `kindUsage`, `Tokens` unchanged, `Diagnostics` unchanged.
- two lines of one message (1h 0 then 200) → 200 (last line wins).
- one agent, messages under `m-a` then `m-b` → `ByModel` = [m-a, m-b] in that order; Σ `Tokens` = `Agent.Tokens`.
- message with no model → a `""` bucket, last.
- spill past `maxNoIDEntries` (same technique as the existing spill tests) → `ByModel` still sums to `Tokens`.
- oracle: the generator emits a split and two models; chunked `Aggregate` = `ParseSession` (`reflect.DeepEqual`); fuzz `FuzzIncrementalMatchesFull` 60 s clean.

Done when: package green, `-race` green, fuzz clean, no change to any token total.

### 2. `cost-state` → `Usage.Billed` (M) — after 1 (same files)

Files: `record.go`, `table.go`, `incremental.go`, `usage.go`; new fixture
`testdata/v2.1.302-cost/` (+ `testdata/README.md` row), tests.

Tests:
- parent ending with `{"type":"cost-state","sessionId":<id>,"totalCostUSD":10}` → `Billed{10, true}`.
- a usage line, or any complete line (even unreadable), after it → `Known false`.
- two identical `cost-state`s, and two growing ones (7.08 then 12.61 last) → the last: 12.61.
- `sessionId` of another session → false; absent → true.
- `totalCostUSD` absent / string / negative → false; `UnreadableLines` and `Degraded()` unchanged.
- a `cost-state` in a sub-agent file → ignored.
- `SENTINEL-DO-NOT-LEAK` in an unknown key of the `cost-state` line → absent from every rendering of `Usage` and errors.
- incremental: the `cost-state` appended alone → delta not `Empty()`, `Billed` known after `Apply`; then a usage line → unknown; equal to `ParseSession` at each point.
- generator and fuzz emit `cost-state` lines at random places.

Done when: as step 1; the declared limit "a non-timestamped line after the
`cost-state` leaves the view's Billed stale until the next non-empty delta or
`ctrl+r`" is written in the test file as a comment.

### 3. `agent_usage` config section (S) — parallel with 1–2

Files: `internal/config/agent_usage.go` (+ `agent_usage_test.go`), `config.go`
(one field).

Tests:
- full entry → five rates `RateSet`; `input: 5` (int) → 5.0.
- entry without `cache_read` → that rate `RateMissing`.
- `input: "5$"` → `LoadContext` **succeeds**, rate `RateInvalid`, other sections intact.
- `.nan`, `.inf`, `-1` decode as values (validation is pricing's).
- round trip Load → Save → Load equal; the invalid raw `5$` written back as is.
- `Default()` and a file without the section → saved YAML has no `agent_usage` key; `Default().AgentUsage.Prices == nil`.

Done when: `go test ./internal/config` green.

### 4a. Price table and matching (S) — after 3

Files: `internal/agentusage/pricing/{pricing.go, match.go}` + tests.

Tests:
- complete entry → in the table; missing rate → `Problem{key, "incomplete: missing cache_read"}`, absent; negative / NaN / Inf / invalid → `Problem`, absent.
- keys `Claude-X ` and `claude-x` → both dropped, one `Problem` each.
- `Lookup`: exact; case-insensitive; `x-20251001` → key `x`; `x-2025` → no; key `x-20251001` present → exact wins; **key `claude-sonnet-5` does not price `claude-sonnet-5-5`**; `x[1m]` → not `x`.

### 4b. Estimates and Unattributed (S) — after 1, 2, 4a

Files: `pricing/estimate.go` + tests.

Tests:
- `Cost`: 1 M in, 1 M out, 1 M read, 1 M write of which 400 k 1 h, rates {1, 10, w5 2, w1 4, read 0.1} → 1 + 10 + 0.1 + 0.6×2 + 0.4×4 = **13.9**.
- agent with two priced models → sum; one unpriced with tokens → `Unpriced`, listed; unpriced with zero tokens → ignored; no tokens → `NoTokens`; `""` bucket with tokens → `Unpriced`.
- `Session`: billed 10.00, agents 8.20 → `Unattributed` 1.80 (|Δ| < 1e-9), known (criterion 5); billed unknown → not known (criterion 6); one agent unpriced → not known; agents 10.40 → −0.40, known.
- `Sum`: any `Unpriced` → `Unpriced`.

### 5. The Est. cost column (M) — after 4b

Files: `agentusageview/{cost.go, agents.go, model.go, selection.go, update.go}`;
tests `cost_test.go`, the existing column-layout tests
(`TestAgentColumnsLayout`, `TestNarrowTerminalsDropColumnsInTheOrderThePlanComputed`).

Tests:
- `formatUSD`: 0 → `$0.00`; 0.004 → `<$0.01`; 0.005 → `$0.01`; 1.234 → `$1.23`; 999.994 → `$999.99`; 999.996 → `$1000`; 1234.5 → `$1235`; 123456 → `$99999+`.
- `costCell`: orchestrator 1.234 → `~$1.23`; sub-agent (with or without partial output) → `≥ ~$1.23`; unpriced → `-`; no tokens → `-`; `-` is dim; no ANSI in any cell.
- Rule 116 at 60/80/100/140/200; new drop order pinned; at 60 the five DEVDESK-43 columns remain.
- `New` with an incomplete entry → one WARN line naming the key; the model shows `-`.
- shortcut key set identical across states (existing test still passes).

### 6. Header, footer, help (S) — after 2 and 5; Q1 decides its header half

Files: `agentusageview/{view.go, help.go}`; tests in `header_live_test.go`,
`status_test.go`, a help test.

Tests:
- `Est. cost`: priced visible rows → `~$8.20`; one unpriced visible → `-`; a filter hiding it → the sum of the rest.
- `Billed › $10.00` and `Unattributed › $1.80` when `Billed.Known`; `Unattributed › -` with an unpriced agent; `−$0.40` in MEDIUM when negative.
- running session (criterion 6) → neither key.
- worst case (Billed, Unattributed, Idle all present) → 7 entries, not more (comment points at `headerMinHeight`).
- a `usageDeltaMsg` whose delta brings the `cost-state` → `Billed` appears without `ctrl+r`.
- footer precedence: error > loading > degraded > price hint; the three hint wordings; nothing at the sessions level; nothing when all are priced.
- help (Rule 114) mentions `Est. cost`, `~`, `≥`, `-`, `Billed`, `Unattributed`, negative remainder, `agent_usage.prices`, the five key names, the unit, the matching rule, "restart after editing".

### 7. Documentation (S) — with 6

- `docs/architecture/agent-usage.md`: public API rows (`ModelUsage`,
  `Agent.ByModel`, `Billed`, `Usage.Billed`); amend "What it does not do" (the
  parent still computes no price; `pricing` does); a "`cost-state` and Billed"
  section (rule, measurements above, limits); privacy amendment (three keys);
  fixture; view section (column, header, footer); the YAML example, values copied
  from the vendor's page with the date and link, marked "check before use".
- `docs/architecture/configuration.md`: the `agent_usage` section, tolerant
  `Rate`, no default, not in `:cfg`, the `:cfg`-save-overwrites-hand-edits warning.
- `.claude/CLAUDE.md`: overview line and the `agent-usage.md` table row.
- `docs/backlog.md`: a §3 entry for the decisions (prices in YAML, matching
  rule, Billed position rule), per the repo's convention.

## Parallelism

- 1 → 2 sequential (same files). 3 in parallel with 1–2. 4a after 3 (parallel
  with 1–2). 4b after 1, 2, 4a. 5 after 4b. 6 after 2 and 5. 7 with 6.
- Effort total: M, as in the feasibility.

## Risks / irreversible

- **File format.** `agent_usage.prices.<model>.{input, output, cache_write_5m,
  cache_write_1h, cache_read}` is written by hand: renaming later costs a
  migration. Validate the names now.
- **Undocumented `cost-state`.** The rule rests on "written last, at close,
  cumulative" (46/46 here, one version line 2.1.28x–2.1.30x). A client that
  writes it mid-session or after other lines would hide or show Billed wrongly;
  the fixture and the help date the observation.
- **Header full (7/7)** with Q1 = A.
- **Hand edits vs `:cfg` save** (H4): an existing behaviour that now matters,
  since YAML is the only path. Documented, not fixed here.
- **Settling 5 min later.** Counting the `cost-state` line as a non-empty delta
  moves `Activity.LastChange` at close, so updates pause up to 5 min later than
  today. Accepted; it is real file activity.
- **Floating point**: display only; Unattributed computed from unrounded values.
- **Privacy**: three new keys read (`sessionId`, `totalCostUSD`,
  `cache_creation.ephemeral_1h_input_tokens`), all an id or a number; the
  sentinel test covers the `cost-state` line.
