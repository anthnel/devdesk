# DEVDESK-148 (A-10b) — Explorer clone list: activity column, per-row footer, safe causes

Story: `docs/product/activity-indicator/stories.md` §A-10b. UX: `ux.md` §1.1, §1.2, §5 (legend).
Inventory: `inventory.md` row "Explorer liste de clone" and gap 4 (parallel clones, criterion 2
corrected). Pattern copied from DEVDESK-142/145/147 (`:jobs` targets level is the closest
twin: `components.ItemIndicator` + `DescribeItem`, D98). Security condition inherited from
**D97** (`docs/backlog.md`, DEVDESK-140 plan Q-A option C): the raw `err.Error()` written by
`explorer/clone_jobs.go:25,38` (and `app/forge_index.go:204`, see Q4) must be rewritten by
A-10b before the footer shows it, because a git/forge error can carry a URL with credentials.

Out of scope: the forge index walk (except Q4), auth, the tree (A-10a, merged), the
clone pipeline itself (`pipeline.go`), `:jobs`.

## Blocking questions (user)

| # | Question | Options | Recommendation |
|---|---|---|---|
| Q1 | **Glyphs for "cloned" and the remaining state** (story hypothesis: done = `◉`). The five `ItemState`s are queued, running, done (`cloned`), skipped (`already there`, and also a queued clone skipped by `esc`, Detail `cancelled`), failed. `components.ItemIndicator` already maps them for `:jobs`' targets level (D98, shipped): `◴`, `●/○`, `◉`, `⊖`, `⊗`. Legend extension labels are new (not in `ux.md` §5, which D10 validated). | **A** reuse `ItemIndicator` as is: cloned `◉`, already there / cancelled `⊖`; legend extension `◉ cloned (clone list)` · `⊖ already on disk, or not started (cancelled) (clone list)` · **B** cloned `◉`, but "already there" back to the row at rest (no glyph; D8 logic "nominal = no dot") — loses a state the criterion asks to keep · **C** other glyph choice (UX ticket) | **A**: same projection as `:jobs` targets for the same run (the user sees one alphabet for one run in two views), no new code path, D7 respected (`◉` = finished only, `⊖` = skipped). |
| Q2 | **What the kept Status column shows.** Today: `IconPending queued`, `<frame> cloning`, `IconOK cloned` (green), `IconSkipped already there`, `IconError failed`. The frame must go (criterion 2); the other icons now duplicate the dot, and Rule 121's amendment (D9) says the activity column replaces them. A cancelled queued clone reads `already there` today (wrong word, pre-existing). | **A** words only, coloured like `:jobs`' State word (`indicator.Style(ItemIndicator(state))`, D98: no green), `cancelled` when `Item.Detail == "cancelled"`; `colCloneStatusMin` 16 → 13 · **B** keep icons and today's colours (green `cloned`), drop only the frame (`cloning` without prefix); width unchanged · **C** A without the `cancelled` word fix | **A**: one alphabet with `:jobs` (D98), no green for done (Rule 125), and the cancelled row stops claiming "already there". |
| Q3 | **Criterion 2 wording differs between sources.** The brief relayed for this story says "only one blinks, the N−1 others are fixed `◴`". `stories.md` §A-10b (corrected after `inventory.md` gap 4) says "the running clones blink (**several rows may blink at once**), the others queued `◴`": clones run in parallel (`forge.pull.parallel_jobs`, `clone.go:112`, `pipeline.go`). | **A** follow `stories.md`: every running item blinks, queued items `◴` · **B** force one blinking row (would need a "leader" rule the registry does not have, and would show a running clone as `◴`, i.e. lie) | **A**: it is the validated story text and the only one true to the pipeline. With `parallel_jobs: 1` A and B are identical. |
| Q4 | **`app/forge_index.go:204` (raw `msg.err.Error()` of the index walk)** is listed by D97 as an A-10b condition, but the brief puts the forge index out of scope. The index cause is shown today in `:jobs` (targets level Detail column and footer, A-06). | **A** fix it here with the same helper (one line + one router test) · **B** leave it, open a bug | **A**: same helper, one line, closes D97 entirely; it is the cause text only, not the index feature. |

Hypotheses (prudent default, no question):
- H1 **The footer describes the selected row in every state** (A-05 / `:jobs` targets level): `components.DescribeItem(jobs.KindClone, item).Detail` as `Status.Detail`, `cloneStatusLine()` as `Status.Text`. `RowStatusLine` keeps the progress last to give way (A-05 rule): at 60 columns the cause may be cut, the progress stays.
- H2 **A skipped (already on disk) clone gets a Detail** `alreadyOnDisk = "already on disk"` in `CloneEventMsg.Transition()`, so the footer reads `⊖ Clone of alpha/api skipped — already on disk` instead of a bare "skipped", and `:jobs` says why too. The Detail column then shows it (it was empty).
- H3 **Cause redaction lives at the producer**, in one helper of `internal/jobs`, applied to every `Transition.Detail` built from an `error` in this story (clone `cloneEnded`, `cloneWalkFailed`, Q4 index) and to the matching `log.Printf` in `handleCloneEvent`. It removes URL userinfo (`https://user:secret@host/…` → `https://host/…`) and the value of `token`, `private_token`, `access_token` query parameters; nothing else (no classification of git errors, D97 point 1 keeps "check logs" elsewhere). `git.Clone` already passes the token by environment, so a leak needs a URL with userinfo (config or an `insteadOf` rewrite) — rare, but on screen and in screenshots.
- H4 **The run-end event is unchanged**: `failureSummary` (`Failed to clone alpha/api and 2 others — check logs`, 3 s `Error`) stays; it is a batch summary, not one row, so `FailureEvent` does not fit. After its timer the selected row's detail is back.
- H5 **No per-frame rebuild**: `handleFrameMsg` calls `m.clone.table.SetBlinkPhase(msg.Blink)`; rows are rebuilt on `ChangedMsg` only. `Model.jobFrame` and `cloneRow.frame` are deleted. A new clone list copies the tree table's current phase (`m.table.BlinkPhase()`) so its first frame matches.
- H6 **`ActivityTables()` always reports `explorer/clone-list`**: the real table when `m.clone != nil`, otherwise a table built by the same constructor (`newCloneTable()`) resized to `m.width × m.height`. The guard builds the view with no run (`builtView`), and a table that appears only mid-clone could not be checked at 60 columns. Same constructor → same columns; nothing renders it.
- H7 **Rule 130**: `cloningShortcuts()` does not depend on the selected row; unchanged. A test pins that the key set is identical for a queued, running, done, skipped and failed selected row.
- H8 **Discovery order kept** (`SortColumn: -1`); the indicator column has no `Less`, so `.` cycles Repository → Status as today (Status keeps its `Less` on `State`).
- H9 `m.spinner.Tick` batched in `handleCloneDestinationSelected` is left as is (the load spinner; `handleSpinnerTick` ignores it outside a load). Not a row animation; the guard does not flag `spinner.Model.Tick`.

## Width arithmetic (Rule 116, `cellPadding = 2`, inside = width − 2)

| Layout | Required (min + 2 each) | Detail (Optional, 20+2) shown from |
|---|---|---|
| today: Repository 30, Status 16 | 32 + 18 = 50 | inside ≥ 72 → width ≥ 74 |
| Q2-A: dot 2, Repository 30, Status 13 | 4 + 32 + 15 = 51 | inside ≥ 73 → **width ≥ 75** |
| Q2-B: dot 2, Repository 30, Status 16 | 4 + 32 + 18 = 54 | inside ≥ 76 → **width ≥ 78** |

At 60 (inside 58) the required set fits in both cases: dot + Repository (flex) + Status,
Detail dropped, `RenderedWidth() == 58`. The tester pins the threshold with a sweep (step 2).

## Steps (TDD — tests first, each step green and mergeable alone)

Base command for every step:
`go test ./internal/ui/forge/explorer/ ./internal/jobs/ ./internal/ui/indicator/ ./internal/app/ && go vet ./...`
Step 4 adds `mise run lint`, `go test -count=2 ./internal/app/`, `mise run test-race` (cgo).

### Step 1 — Safe causes (D97, security) · S (parallel with 2)

Files: new `internal/jobs/cause.go` + `cause_test.go`; `internal/ui/forge/explorer/clone_jobs.go`
(lines 25, 38, + H2), `clone.go` (`handleCloneEvent` logs); if Q4-A
`internal/app/forge_index.go:204` + a test in `internal/app/forge_index_test.go` (or nearest).

```go
// internal/jobs/cause.go
// SafeCause is the text of err fit to be stored in a Transition.Detail and shown
// on screen: URL userinfo and token query values are removed. "" for a nil err.
func SafeCause(err error) string
// (unexported helper, if useful) func redactURLs(s string) string

// explorer/clone_jobs.go
const alreadyOnDisk = "already on disk" // H2
// cloneEnded+err:    t.State, t.Detail = jobs.ItemFailed, jobs.SafeCause(m.event.err)
// cloneEnded+skip:   t.State, t.Detail = jobs.ItemSkipped, alreadyOnDisk
// cloneWalkFailed:   t.Detail = "discovery: " + jobs.SafeCause(m.event.err)
```

Tests (input → expected), table-driven:
- `SafeCause(nil)` → `""`; `errors.New("Repository not found.")` → unchanged.
- `fatal: unable to access 'https://oauth2:glpat-abc@gitlab.example/g/app.git/': 403` → no `glpat-abc`, no `oauth2:`; contains `https://gitlab.example/g/app.git/`.
- `https://user@host/x` (user only) → `https://host/x`; `ssh://git@host:22/x` → `ssh://host:22/x`; `git@github.com:o/r.git` (scp form, no scheme) → unchanged (no secret there).
- `GET https://h/api/v4/groups?private_token=abc&page=2: 401` → `private_token=` value gone (`private_token=REDACTED` or parameter removed — the coder picks one, the test asserts `abc` absent and `page=2` kept); same for `token`, `access_token`, case-insensitive key.
- two URLs in one message → both redacted; a message with no URL → byte-identical.
- `CloneEventMsg{cloneEnded, err: <url with userinfo>}.Transition().Detail` → redacted; `{cloneEnded, skipped: true}` → `State ItemSkipped`, `Detail "already on disk"`; `{cloneWalkFailed, err: 403}` → `Detail "discovery: 403"` (existing `TestAFailedWalkGetsItsOwnRow` stays green).
- `handleCloneEvent` with a userinfo URL error: captured `log` output (`log.SetOutput` to a buffer, restored in `t.Cleanup`) does not contain the secret.
- Q4-A: router `forgeIndexBuiltMsg{err: <url with userinfo>}` → the registry item's `Detail` has no secret.

Done when: green; no `err.Error()` / `%v err` reaches a `Transition.Detail` in `clone_jobs.go` (and `forge_index.go` if Q4-A).

### Step 2 — Column 0 = activity column; Status without frame; blink read at render · M (parallel with 1)

Files: `clone_list.go` (columns, `cloneRow`, `setRun`, labels/styles, constants),
`clone.go` (`handleJobsChanged`, `handleFrameMsg`, `ActivityTables`,
`handleCloneDestinationSelected` phase copy), `model.go` (delete `jobFrame` + its comment);
tests: rewrite `frame_test.go:TestAFrameMovesTheCloneListSpinner`,
`model_test.go:TestARunningRowCarriesTheRoutersFrame` (~l.1361),
`indicator_test.go:TestACloneListKeepsItsFrameWhileTheTreeTakesThePhase` (~l.219) and the
keys assertion (~l.81); harness `cloning(t).broadcast()` sends `jobs.FrameMsg{Blink: …}`;
new `clone_indicator_test.go`.

```go
// clone_list.go
type cloneRow struct{ item jobs.Item } // frame removed
const (
    colCloneIndicator = iota // new, untitled
    colCloneRepository
    colCloneDetail
    colCloneStatus
)
const colCloneStatusMin = 13 // Q2-A ("already there"); 16 if Q2-B

func newCloneTable() datatable.Model[cloneRow] // the Config newCloneList used, extracted (H6)
func cloneColumns() []datatable.Column[cloneRow]
// [0] datatable.IndicatorColumn(func(r cloneRow) indicator.State {
//         return components.ItemIndicator(r.item.State) }, nil, nil)   // no rest icon: the row has none
// [1..3] Repository, Detail, Status as today; Status per Q2.
func cloneStatusLabel(r cloneRow) string        // Q2-A: "queued" | "cloning" | "cloned" | "already there" | "cancelled" | "failed"
func cloneStatusStyle(r cloneRow) lipgloss.Style // Q2-A: indicator.Style(components.ItemIndicator(r.item.State))
func (l *cloneList) setRun(run jobs.Run)         // frame parameter removed

// clone.go
func (m Model) handleFrameMsg(msg jobs.FrameMsg) (tea.Model, tea.Cmd)
// m.footer.SetSpinnerFrame(msg.RenderedFrame); m.table.SetBlinkPhase(msg.Blink);
// if m.clone != nil { m.clone.table.SetBlinkPhase(msg.Blink) } — NO setRun
func (m Model) ActivityTables() map[string]datatable.ActivityTable
// {"explorer/tree": &m.table, "explorer/clone-list": <&m.clone.table, or a resized newCloneTable()>} (H6)
```
The cancelled test for Q2-A compares `item.Detail` with the registry's literal; to avoid a
second copy, export it from `internal/jobs` (`const CancelledDetail = "cancelled"`, used at
`registry.go:308,346`; `forge_index.go:201` may use it too). If Q2-B/C, skip that constant.

Tests (input → expected), via the `cloning(t)` harness:
- column 0 shape (`m.clone.table.Shapes()[0]`): untitled, `SizingFixed`, `IconColumnWidth`, `Indicator`, no `Less`/`Search`.
- five states → glyph in column 0: found → `◴` (both phases); began → `indicator.Glyph(Running, true)` with phase on, `Glyph(Running, false)` with phase off; ended → `◉` (Q1-A); skipped → `⊖`; failed → `⊗` red (`ColorActivityFailed`); walk failed → `⊗`; `esc` on a queued item → `⊖` and Status `cancelled` (Q2-A).
- criterion 2: three found, two began (`parallel_jobs` 2) → exactly the two running rows blink, the third is `◴`; a `FrameMsg{Blink:false}` flips both running glyphs **without** changing `m.clone.table.Items()` (deep-equal before/after).
- Status cells: exact words per state (Q2), no cell contains a spinner rune (`spinner.Dot.Frames`), `theme.IconOK`/`IconError`/`IconPending`/`IconSkipped` (Q2-A) or an escape sequence; `cloned` style foreground is not `theme.ColorOK`/green (Q2-A).
- `Model` has no `jobFrame` field (`reflect.TypeOf(Model{}).FieldByName`), `cloneRow` has no `frame` field.
- `ActivityTables()` reports exactly `explorer/tree` and `explorer/clone-list`, with and without a run; without a run at 60×30 the clone table's `RenderedWidth() == 58`.
- Rule 116 sweep on a cloning model with long paths: widths 60, 74, 75, 78, 80, 120 → `RenderedWidth() == width-2`; Detail header absent below the threshold of the chosen Q2 option, present from it (table above).
- a new clone list started while the tree phase is off renders `○` for its first running row (H5).

Done when: green; the tree tests from A-10a untouched and green.

### Step 3 — Footer: selected row detail + global progress (criteria 2, 3) · S (after 1 and 2)

Files: `view.go` (`status()`, ModeCloning branch), `clone_list.go` (`selectedItem()`),
new `clone_footer_test.go`.

```go
// clone_list.go
func (l *cloneList) selectedItem() (jobs.Item, bool) // l.table.Selected()

// view.go, status()
case m.mode == ModeCloning && m.clone != nil:
    return m.cloneStatus()
// clone.go (or view.go)
func (m Model) cloneStatus() components.Status
// st := components.Status{Text: m.cloneStatusLine()}
// if it, ok := m.clone.selectedItem(); ok { st.Detail = components.DescribeItem(jobs.KindClone, it).Detail }
```

Tests (width 120 unless stated):
- failed row selected (cause `Repository not found.`) → footer line contains `⊗ alpha/api: last clone failed — Repository not found.` **and** `1 found · 0 cloned · 0 already there · 1 failed` (criterion 3), Info level.
- running row selected → `● Cloning alpha/api · 3 found · …` (verb from `KindClone.Verb()`), no footer spinner frame.
- queued → `◴ Queued: clone alpha/api · …`; done → `◉ Clone of alpha/api finished · …`; skipped → `⊖ Clone of alpha/api skipped — already on disk · …` (H2); cancelled → `… skipped — cancelled`.
- cursor moves (`down`) → the detail follows the new row; empty list (walk just started) → progress alone.
- failure with a userinfo URL in the cause (through `CloneEventMsg`) → footer has no secret (end to end of step 1).
- `CloneRunFinishedMsg` with a failure → `Error` `Failed to clone alpha/api — check logs` wins (H4); `ClearFooterMsg{ID: m.footer.ID()}` → the selected row's detail + `Done — …` come back.
- 60 columns: one line, `GetFooterHeight()` unchanged (2, or 2 + filter bar), the progress is present; the detail is cut or dropped, never wraps.
- Rule 130 (H7): `testutil.ShortcutKeys(m.GetShortcuts())` identical for a selected queued, running, done, skipped and failed row.

### Step 4 — Legend, help, guard (criteria 1, 4) · S (after 2–3)

Files: `internal/ui/indicator/legend.go` (+ `legend_test.go`), `internal/ui/forge/explorer/view.go`
(`GetHelpContent`), `internal/app/activity_guard_test.go`, `activity_help_test.go` (explorer).

```go
// indicator/legend.go (Q1-A wording; adjust if Q1 decides otherwise)
CloneLegend = []LegendEntry{
    {Done, "cloned (clone list)"},
    {Skipped, "already on disk, or not started (cancelled) (clone list)"},
}
// explorer/view.go: indicator.Legend() → indicator.Legend(indicator.CloneLegend...)
```
- Help "Cloning" section: replace "each row showing what it is doing" with: the first column shows
  `◴` queued, a blinking dot while cloning (several at once with parallel jobs), `◉` cloned,
  `⊖` already on disk or cancelled, `⊗` failed; select a row to read its state and cause in the
  footer, beside the run's progress. Glyphs via `indicator.Glyph`, never literals.
- Guard: remove `{"explorer/clone-list", command.ViewGitExplorer, "A-10b"}`;
  `maxPendingTables` 9 → 8; `migratedTables[ViewGitExplorer] = {"explorer/tree", "explorer/clone-list"}`;
  `migratedSources[ViewGitExplorer] = {"ui/forge/explorer/"}` (whole package; checked on `main`:
  after step 2 no source file there has a `Frame`/`frame`/`SpinnerFrame` field or selector,
  a bubbletea `Tick`/`Every`, `AdvanceSpinner`, `StatusColumn:` or a glyph literal —
  `jobs.FrameMsg`, `RenderedFrame`, `SetSpinnerFrame`, `m.spinner.Tick` are not matched).
  Update the comment that explained the file list. `seedExplorerFooter` unchanged (one seed per view).
- Tests: `TestActivityGuardMigratedViews/explorer` covers both tables at 60 (shape + `RenderedWidth`);
  `TestActivityGuardNoStaleException`, `TestActivityGuardMigratedSourcesAreDeclared` green;
  legend test: `Legend(CloneLegend...)` contains `◉` and `⊖` lines; explorer help test: the
  Cloning section names the five glyphs and the legend section is present (exactly once);
  `TestNoShortcutAdvertisesAGlyphAsAKey`, `TestEveryJobReporterIsRoutedToTheRegistry` still green.

Done when: `go test ./internal/app/ ./internal/ui/forge/explorer/ ./internal/ui/indicator/ ./internal/jobs/` green,
`go test -count=2 ./internal/app/` green, `mise run test-race` green on `internal/app` and
`internal/ui/forge/explorer`, `mise run lint` clean.

### Step 5 — Docs · S (parallel with 4, same PR)

- `docs/architecture/forge.md`: §"Creating and deleting" (l.~382) drop "The clone list is not
  migrated (A-10b): it keeps `jobFrame` and its Status frame"; §"The clone is a run of the
  registry" table: "the frame" row → "the activity column: `components.ItemIndicator`, blink phase
  from `jobs.FrameMsg.Blink`, read at render"; add the Status wording (Q2), the footer
  (detail + progress, H1, H4), the skipped Detail (H2), and a paragraph "Causes are redacted"
  (`jobs.SafeCause`, H3, D97 closed). Width thresholds (table above, chosen option).
- `docs/architecture/app-shell.md` l.~988–994: guard table — `pendingTables` 9 → 8,
  `migratedTables` adds `explorer/clone-list`, `migratedSources` for the explorer is the whole
  package; registry section: `jobs.SafeCause` (and `CancelledDetail` if Q2-A).
- `docs/product/activity-indicator/ux.md` §5 "Libellé de la légende": add the clone-list extension line (Q1).
- `docs-site/docs/explanation/forge.md`: the clone list's activity dot (five glyphs, footer cause).
- `docs/backlog.md` §1.1: new entry at the top (next free D-number), DEVDESK-148, plan path,
  decisions Q1–Q4, H1–H9; amend D97's sentence "must be rewritten by A-10b" with "done in DEVDESK-148"
  (or point to the bug if Q4-B).
- `.claude/CLAUDE.md`: no change.

## Order and parallelism

1 ∥ 2 → 3 → 4, with 5 alongside 4. Steps 1 and 2 touch different files (step 1: `internal/jobs/cause.go`,
`clone_jobs.go`, the log line in `clone.go::handleCloneEvent`; step 2: `clone_list.go`, `model.go`,
the other functions of `clone.go`): one worktree, two commits; if run by two coders, merge `clone.go`
by function. Effort: **M** overall (S, M, S, S, S).

## Risks / irreversible points

- R1 **Security**: the footer is a new place where a clone cause shows (screenshots). Step 1 must land
  before or with step 3; `security` review on the PR (DEVDESK-140 plan, Risks). The Detail column
  and `:jobs` showed the raw text already; step 1 fixes them too.
- R2 At 60 columns the cause is cut first (progress wins, A-05). The full cause stays in the Detail
  column (from width 75/78), in `:jobs` and in the log. Accepted, stated in help.
- R3 `ActivityTables()` building a throwaway table when no run exists (H6) must stay cheap and
  unrendered; it is only read by the guard. Alternative if review rejects it: keep the clone table
  on `Model` for the life of the view (more churn in tests reading `m.clone.table`).
- R4 Q2-A changes visible words/colours (`cloned` no longer green, icons gone, `cancelled` word):
  intended, aligned with D98.
- R5 Router e2e is limited: the explorer's ModeCloning cannot be entered from `internal/app`
  without starting the real pipeline (network, git). The package harness `cloning(t)` covers the
  behaviour; the router side is covered by the guard and the existing `jobs_test.go` clone tests.
- Nothing irreversible: no disk format, no config key, no persisted state.
