# DEVDESK-150 (A-12) — netdiag Ports: activity column for the kill

Story: `docs/product/activity-indicator/stories.md` §A-12. UX: `ux.md` §1 (glyphs), §5 (legend).
Worktree: `.worktrees/feat/DEVDESK-150-netdiag-ports-activity` (base `e037b2f`).
Pattern: DEVDESK-146 (OCI images, the closest twin: a `busy` action, `MarkBusy`/`ClearBusy`,
reported as `Observed: Running` through `components.DescribeRow`), DEVDESK-148/149 (guard, help).

**What makes this story small.** The Ports table has no registry run at all: its only row
operation is `K` (SIGKILL), held in `datatable`'s `busy` map, keyed by PID. `datatable` already
writes the precedence "busy ⇒ `indicator.Running`" once (`render.go::indicatorState`), so the
view only adds an `IndicatorColumn` whose own state is always `Idle`. `jobs.ChangedMsg`,
`jobs.ForgetTarget`, `FailureEvent` and the failure memory are **not used**: nothing here goes
through the registry, and the kill failure stays a footer `Error` (criterion 1, D5: there is no
kill kind to remember).

Out of scope: `H`, the Diagnostics (checks) and Interfaces tables, the Forward tab, `internal/ports`.

## Finding in the current code (fixed by the story itself)

`portsSpinnerTickMsg` is not routed by `netdiag.Model.Update` (`update.go:90` lists
`portsTickMsg, portsDataMsg, portsKillResultMsg` only): the tick lands in the fall-through and
is dropped, so in production the row spinner shows frame 0 and never turns.
`TestTheKillSpinnerTickStopsWhenTheKillLands` calls `handleSpinnerTick` directly and does not see it.
The story deletes the chain; no separate bug.

## Blocking questions (user)

| # | Question | Options | Recommendation |
|---|---|---|---|
| Q1 | **Open question 4 of `stories.md`: a network test that fails** (TCP refused, invalid TLS) — row result or remembered operation failure? | **A** result: it stays in the Verdict column of the checks table, nothing changes there; the red dot is reserved for an operation that failed (criterion 4 as written) · **B** remembered failure: a `⊗` and the failure memory for a diagnostic, which needs a column on the checks table, a kind in the registry, and is no longer an S story | **A**: the Verdict already says FAIL in red with its reason; a `⊗` on top would say the same thing twice, and "the check could not look" (UNKNOWN) is a result too. The plan is written for A (criterion 4 = a regression test). B would be a new story. |
| Q2 | **What the State cell shows during a kill.** Today the State cell is replaced by the spinner (§3.22: "the row must not show the state the signal is about to change"; `TestAKillSpinsTheSocketState` pins it). With the dot in column 0 that replacement goes (`StatusColumn` is a retired mechanism). | **A** State keeps its text, dimmed like every busy row (`datatable.cellStyle` busy branch, no view code; same as OCI images after DEVDESK-146) · **B** State reads `killing` (dim) while busy, from a `Cell` that checks `IsBusy` (the view reads the busy map in a cell: a second path to the same fact) · **C** State blank while busy | **A**: the dot says "in progress", the dim says "ceasing to be true" (the datatable comment's wording), and one alphabet with OCI. It reverses the letter of §3.22 (LISTEN visible, dim) but keeps its intent. |

## Hypotheses (prudent default, no question)

- H1 **Column 0** = `datatable.IndicatorColumn(func(ports.Socket) indicator.State { return indicator.Idle }, nil, nil)`:
  no rest icon (the row has none), so an idle row shows an empty two-cell column. A busy row is
  `Running` through `datatable`'s precedence: every socket of the PID shows `●`/`○` (today's
  "every row of that process spins" kept).
- H2 **The blink phase comes from `jobs.FrameMsg`** handled by `netdiag.Model.Update`:
  `m.portsModel.table.SetBlinkPhase(msg.Blink)`, nothing rebuilt. **The footers' spinner frames
  are not touched**: the Diagnostics run and the Interfaces load keep their own `spinner.TickMsg`
  chain (D2, criterion 5).
- H3 **`WantsBlink()`** (router's `BlinkingView`) on `*netdiag.Model` = `len(m.portsModel.table.BusyLabels()) > 0`,
  whatever the active tab (a level; the router only frames the view on screen; a kill lasts a moment).
- H4 **Per-row footer** (A-05): `PortsModel.statusLine()` adds `Detail` = `components.DescribeRow(RowQuery{Observed: indicator.Running, ObservedText: label}).Detail`
  when the selected row's PID is busy (label = the existing `"Killing <process> (<pid>)"`), zero
  otherwise. Text/Level (stale error, paused) unchanged and placed after the detail by
  `RowStatusLine`. At 60 columns this is where the process name is readable (PID and Process
  columns are dropped there).
- H5 **Kill outcomes unchanged**: success `Info` "Process N terminated", failure `Error`
  `killFailureMessage` (§3.49, D5), `ClearBusy` on both; the row returns to rest (no `⊗`).
  `K` stays lit on a busy row (Rule 130: an operation in progress is not a reason to grey);
  `Already killing PID N` warning kept.
- H6 **The periodic refresh never blinks** (criterion 3): `SetItems` does not touch `busy`; a
  refresh with nothing busy leaves every column-0 cell empty and `WantsBlink()` false.
- H7 **Scan scope of the guard**: the I/O commands (`fetchPortsCmd`, `portsTickCmd`,
  `killProcessCmd`, `defaultPortsRefresh`, `portsFetchTimeout`) move to a new
  `internal/ui/netdiag/ports_commands.go`, **not scanned**, exactly like OCI's `commands.go`
  (its refresh `tea.Tick` is data, not a row animation). Scanned: `ports_model.go`,
  `ports_activity.go`, `header.go`, `update.go`. `forward_model.go` (its own refresh tick) and the
  other tabs are not scanned (not migrated). Alternative rejected: an entry in
  `declaredViewTicks`, documented as "empty today, may only shrink".
- H8 **Guard footer seed**: the seed must not read the real socket table nor kill anything. It
  needs an exported way to hand rows to the view: `func NewPortsDataMsg(sockets []ports.Socket) tea.Msg`
  in `ports_commands.go` (returns the unexported `portsDataMsg`; no rename, no test churn). The
  seed drives the view's `Update` directly (not the router: the Init `Cmd` and the kill `Cmd` are
  never executed), with a PID that cannot exist on Linux (`"4194305"` > `pid_max`).
- H9 `TestDeclaredRowChainsLogic` keeps its fictitious `ports_model.go` fixtures (its own local
  pending list); the `portsSpinnerCmd` detector in `TestRowAnimationChainsOutsideTheRouterAreDeclared`
  stays (it now catches a reintroduction).

## Width arithmetic (Rule 116; `cellPadding = 2`; inside = width − 2)

Columns (min + 2): dot 4, Proto 8, State 12, Local 28, Peer 28, PID 9, Process 12. None is
`Optional`, so required columns give way rightmost first. With addresses at their 26-cell floor:

| Columns shown | today from width | after from width |
|---|---|---|
| all | 99 | **103** |
| without Process | 87 | **91** |
| without PID, Process | 78 | **82** |
| dot, Proto, State, Local | 50 | **54** |

At 60: dot, Proto, State, Local Address (same set as today), `RenderedWidth() == 58`. These are
estimates: the tester pins them with the sweep of step 1; the measured values go into `network.md`.

## Steps (TDD — tests first; each step green and mergeable alone)

Base command: `go test ./internal/ui/netdiag/ ./internal/app/ && go vet ./...`.
Step 3 adds `mise run lint`, `go test -count=2 ./internal/app/`, `mise run test-race`.

### Step 1 — Activity column, row chain removed, blink, refresh · S

Files: `internal/ui/netdiag/ports_model.go` (columns, config, update, kill), new
`ports_commands.go` (H7, moved code + `NewPortsDataMsg`), new `ports_activity.go`, `update.go`
(`jobs.FrameMsg` case), tests `ports_model_test.go` (+ new `ports_activity_test.go`).

```go
// ports_model.go
const ( // column indexes, replacing portsColumnState
    portsColIndicator = iota
    portsColProto
    portsColState
    portsColLocal
    portsColPeer
    portsColPID
    portsColProcess
)
func portsColumns() []datatable.Column[ports.Socket] // [0] = IndicatorColumn (H1), then the six of today
// newPortsModel: StatusColumn removed, Key (PID) kept.
// deleted: portsSpinnerTickMsg, portsSpinnerCmd, handleSpinnerTick, its case in update().
// killSelected: return pm, killProcessCmd(entry.PID)   // no tick batched

// ports_activity.go
func (m *Model) ActivityTables() map[string]datatable.ActivityTable // {"netdiag/ports": &m.portsModel.table}
func (m *Model) WantsBlink() bool                                  // H3

// update.go, Model.Update
case jobs.FrameMsg:
    m.portsModel.table.SetBlinkPhase(msg.Blink) // H2: no SetSpinnerFrame
    return m, nil

// ports_commands.go
func NewPortsDataMsg(sockets []ports.Socket) tea.Msg // H8
```

Tests (input → expected), driven through `Model.Update` like the existing ones:
- `m.portsModel.table.Shapes()[0]`: untitled, `SizingFixed`, `IconColumnWidth`, `Indicator`, no `Less`/`Search`; columns 1..6 = Proto…Process (titles unchanged).
- data loaded, nothing busy → column-0 cell of every row is blank, `WantsBlink()` false.
- `K` + `ConfirmModalYesMsg` on sshd (PID 812) → the sshd row's column 0 renders `indicator.Glyph(Running, true)`; the other rows blank; `WantsBlink()` true; the returned `Cmd` is not a tick (no `portsSpinnerTickMsg` type exists: compile-level).
- two sockets of PID 812 → both rows show the dot (rewrite of `TestAKillSpinsEveryRowOfThatProcess`).
- `jobs.FrameMsg{Blink: false}` → `Glyph(Running, false)` (`○`) on those rows; `Items()` deep-equal before/after; `Blink: true` → `●` back.
- Q2-A: the busy row still contains `LISTEN` (dim) and `sshd` (rewrite of `TestAKillSpinsTheSocketState`); no cell contains a `spinner.Dot` frame rune.
- `portsKillResultMsg{pid:"812"}` → no dot anywhere, `WantsBlink()` false, footer Info; with `err` (`os.ErrPermission`) → no dot, footer `Error` `Refused by the system — …`, and the rendered table contains no `indicator.Glyph(Failed, true)` (criterion 1).
- criterion 3: `NewPortsDataMsg(fixtures)` refresh with nothing busy → no dot, `WantsBlink()` false; refresh while 812 is busy (fixtures plus a new 812 socket) → every 812 row dotted, cursor kept.
- criterion 5: a `jobs.FrameMsg` while a diagnostic runs (`StateRunning`) does not change the Diagnostics footer's spinner frame (render before/after equal); a `spinner.TickMsg` still does.
- criterion 4 / 2 (Q1-A): `checkColumns()` and the interfaces columns have no `Indicator` (shapes unchanged); `ActivityTables()` reports exactly `netdiag/ports`.
- Rule 116 sweep: widths 40, 54, 60, 82, 91, 103, 120, 200 → `RenderedWidth() == width-2`; at 60 the header holds Proto/State/Local Address and not PID; replace `TestPortsResizeFillsTheViewportWidth`'s column sum (forbidden by Rule 116) by `RenderedWidth()`; `portRows` reads `row[portsColProcess]`.
- delete `TestTheKillSpinnerTickStopsWhenTheKillLands` (replaced by the `WantsBlink` cases).

Done when: green; `grep -n "portsSpinner\|StatusColumn\|AdvanceSpinner" internal/ui/netdiag/*.go` empty.

### Step 2 — Footer of the selected row, help legend · S (after 1)

Files: `ports_model.go` (`statusLine`), `header.go` (`GetHelpContent`), tests `ports_model_test.go`, `view_test.go` (or the help test file of the package).

```go
// ports_model.go
func (pm *PortsModel) selectedDetail() components.RowDetail // H4; zero when not busy
// statusLine(): Status{Text, Level as today, Detail: pm.selectedDetail()}
```
- Help: append `indicator.Legend()` to `Sections` (base legend, no extension: only `●`/`○` occur here). In "Ports Tab — How it works", add one sentence built from `indicator.Glyph(indicator.Running, true)` (never a literal): while K runs, the first column of every socket of that process shows a blinking dot; a refusal is reported in the footer, the row goes back to rest.

Tests (width 120 unless stated):
- busy sshd selected → footer contains `indicator.Glyph(Running, true) + " Killing sshd (812)"`, Info level; cursor moved to an idle row → no glyph in the footer.
- busy + paused → detail then `Paused — press space to resume` on one line; busy + stale → detail then the stale text (error level, as today).
- after the kill result (failure) → the `Error` wins for its 3 s; `ClearFooterMsg{ID}` → no detail (row at rest).
- 60 columns: one line, `GetFooterHeight()` unchanged.
- Rule 130: `testutil.ShortcutKeys(GetShortcuts())` identical for a busy, an idle and a PID-less selected row; `K` enabled on the busy row.
- help: exactly one section titled `indicator.Legend().Title`; the Ports section names the running glyph; `TestNoShortcutAdvertisesAGlyphAsAKey` green.

### Step 3 — Guards and documentation · S (after 1–2; the docs part can run alongside)

Files: `internal/app/activity_guard_test.go`, `internal/app/animation_guard_test.go`, new seed in
`internal/app/netdiag_activity_test.go`; docs below.

- `pendingTables`: remove `{"netdiag/ports", command.ViewNetdiag, "A-12"}`; `maxPendingTables` 7 → 6.
- `migratedTables[command.ViewNetdiag] = {"netdiag/ports"}`.
- `migratedSources[command.ViewNetdiag] = {"ui/netdiag/ports_model.go", "ui/netdiag/ports_activity.go", "ui/netdiag/header.go", "ui/netdiag/update.go"}` with a comment naming the files left out and why (H7).
- `migratedFooterSeeds[command.ViewNetdiag] = seedNetdiagFooter`:
  ```go
  // seedNetdiagFooter opens netdiag on its Ports tab with one socket whose kill is in
  // flight. It drives the view's Update directly and never runs a returned Cmd: the
  // Init would read the real socket table, the kill would signal a process (H8).
  func seedNetdiagFooter(t *testing.T, a *App) indicator.State
  // a.createView(ViewNetdiag); a.currentView = ViewNetdiag; v := a.views[ViewNetdiag]
  // v.Update(tea.KeyMsg tab) → v.Update(netdiag.NewPortsDataMsg([]ports.Socket{{Protocol:"tcp", State:"LISTEN", LocalAddr:"127.0.0.1:65000", PID:"4194305", Process:"devdesk-guard"}}))
  // → v.Update(K) → v.Update(components.ConfirmModalYesMsg{}); store v back; return indicator.Running
  ```
- `animation_guard_test.go`: remove `"ui/netdiag/ports_model.go"` from `declaredRowChains`; `maxDeclaredRowChains` 4 → 3 (H9 for the rest).
- Tests that must pass: `TestActivityGuardEveryViewIsClassified`, `…MigratedViews/netdiag` (shape + `RenderedWidth` 58 at 60 + legend), `…NoStaleException`, `…MigratedSourcesAreDeclared`, `…MigratedSourcesUseNoRetiredMechanism`, `…MigratedFooterCarriesTheRowGlyph/netdiag`, `TestDeclaredRowChainsFollowTheirStories`, `TestRowAnimationChainsOutsideTheRouterAreDeclared`, `TestOnlyIndicatorColumnSetsIndicator`, `TestAnIconColumnIsUntitledAndTwoCellsWide`.

Docs (same PR, "documentation in the same commit"):
- `docs/architecture/network.md` §netdiag, Ports tab bullet: activity column (H1), `K` → `●`/`○` from `datatable`'s busy precedence, failure in the footer only, refresh never blinks (H6), footer detail (H4), `ports_commands.go` (H7); a width paragraph with the **measured** thresholds of step 1 (none was documented before).
- `docs/architecture/app-shell.md`: "Row chains still outside the router" → three, drop the `netdiag` row (~l.899); guard table (~l.993–994): `migratedTables` adds `netdiag/ports`, DEVDESK-150; `pendingTables` "7 to 6 with `netdiag/ports`, DEVDESK-150"; mention the netdiag file list in `migratedSources`.
- `docs-site/docs/explanation/network.md`: the dot while K runs.
- `docs/product/activity-indicator/stories.md` question 4: "Résolu (date, DEVDESK-150)" with the user's answer to Q1.
- `docs/backlog.md` §1.1: new entry **D105**, DEVDESK-150, plan path, Q1–Q2 decisions, H1–H9, the frozen-spinner finding.
- `.claude/CLAUDE.md`: no change.

Done when: `go test ./internal/ui/netdiag/ ./internal/app/` green, `go test -count=2 ./internal/app/` green, `mise run test-race` green on both packages, `mise run lint` clean.

## Order and parallelism

1 → 2 → 3 (all three touch `ports_model.go` or depend on it). Only the docs of step 3 can be
written alongside step 2. One worktree, three commits. Effort **S** overall.

## Risks / irreversible points

- R1 The guard seed is the only place a kill is set up outside the package's own tests: a future
  edit that runs the seed's `Cmd`s would signal a real PID. Mitigation H8 (nonexistent PID, view
  `Update` only, comment saying why).
- R2 At widths 99–102 the Process column now drops (4 cells spent on the dot, D1 accepted). The
  footer detail names the process during a kill (H4).
- R3 Q2-A shows `LISTEN` (dim) during a kill, against the wording of §3.22. If the user picks B
  or C, step 1 gains a `Cell` reading `IsBusy` and one test; no other step changes.
- Nothing irreversible: no config key, no disk format, no persisted state. The exported
  `NewPortsDataMsg` is the only new API.
