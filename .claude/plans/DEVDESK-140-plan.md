# DEVDESK-140 — Per-row footer: what runs on the selected row, or why it failed (A-05)

Story A-05 (`docs/product/activity-indicator/stories.md`), Plane DEVDESK-140, epic E1.
Inputs: A-05 criteria 1–8, D8, D10, `ux.md` §1.2 (validated phrases), §3.6 (footer in
three moments), architect annex Q3/Q4. Builds on DEVDESK-137 (`internal/ui/indicator`,
`datatable.IndicatorColumn`), DEVDESK-138 (`jobs.Failure`, `ChangedMsg.Failures`,
`FailuresOn`), DEVDESK-139 (`jobs.FrameMsg.Blink`). Code read at `4b505ee`:
`internal/ui/components/{footer_message,jobs_status,footer_guard_test}.go`,
`internal/ui/indicator/*`, `internal/jobs/{jobs,failure,message,registry}.go`, every
`jobs.Reporter` that writes `Transition.Detail`, `theme/{path,text}.go`.

One PR. **Nothing changes on screen**: no view passes a row detail until its migration
story (A-06 onwards). A `Status` with no detail must render byte for byte as today
(tested). Out of scope: wiring in any view, instant-action messages (`busy`: stop, kill,
remove, login), observed-state sentences of containers / agents / `:jobs` done (their
stories write them, through the same composer).

Order: {1, 2 in parallel} → 3 → 4. Docs ship in step 4, same PR (`.claude/CLAUDE.md`:
architecture notes follow the code).

---

## Questions bloquantes (user)

None blocks the start of steps 1–4: each has a default that ships nothing visible, and
the answer changes one function body and one test table. They belong to the user because
they change words on screen.

**Q-A. Who writes a readable cause?** `ux.md` promises
`⊗ ~/w/app/api: last scan failed — trivy could not pull the database`. Today **no**
producer writes such a cause: every `Transition.Detail` on failure is
`"<kind> failed — check logs"` (ws, `:sec`, OCI, templates, filebrowser, explorer), and two
write a raw `err.Error()` (`explorer/clone_jobs.go:25,38`, `app/forge_index.go:204`).
A-05 can only *carry* the cause; no story in E2 owns classifying errors.

| Option | Effect |
|---|---|
| **A (recommended)** | Accept `check logs` as the cause for now: the footer reads `⊗ ~/w/app/api: last scan failed — check logs`. Each migration story may improve its own causes; no new story. |
| B | Add a story (E2, Should) "readable failure causes": classify Trivy / Gitleaks / git / docker errors into short English causes, and rewrite the two raw `err.Error()` sites. |
| C | A, plus make the clone / index raw causes a condition of A-10b (they can carry a URL — see Risks). |

Recommendation: **C** (A for everything, and the two raw sites fixed where they are first
shown, A-10b; `index` failures are shown by no view under D5).

**Q-B. Redundant verb in the composed line.** Criterion 3 composes "row detail · global
progress", and the global progress is `JobsStatusLine`, which says `Scanning — 3/12`.
The row then reads `● Scanning ~/w/app/api · Scanning — 3/12`, while the mock-ups
(`ux.md` §3.1, §3.6, illustrative, not the validated §1.2) show `· 3/12 scans` /
`· 3/12 repositories`.

| Option | Effect |
|---|---|
| **A (default of this plan)** | Keep `JobsStatusLine` unchanged; accept the repeated verb. Zero regression on ws, templates, dashboard. |
| B | The composer elides `"<Verb> — "` from a one-part progress whose kind is the row's running kind: `● Scanning ~/w/app/api · 3/12`. Needs a structured progress (`JobsProgress{Parts}` with `String()`; `JobsStatusLine` becomes `JobsProgress(...).String()`, output unchanged). +S. |
| C | Adopt the mock-up wording (`3/12 scans`) everywhere, `JobsStatusLine` included: visible change on three existing footers. |

Recommendation: **A** now, **B** decided at the A-06 pilot when the line is seen for real.
If B is chosen before coding, it is a contained addition to step 2.

---

## Design decisions (the plan's, not the user's)

### Where things live

| Piece | Package | Why |
|---|---|---|
| Which unsettled items sit on a target (`ActiveOn`) | `internal/jobs` | pure data over a snapshot, sibling of `FailuresOn` / `FailureFor`; no UI copy |
| The phrases (running, queued, failed, masked failure, event) | `internal/ui/components`, new `row_activity.go` | interface copy that names a command (`:jobs`) — the same reason `JobsStatusLine` lives there; one builder (criterion 8) |
| The composer "row detail · progress", with truncation | `internal/ui/components/jobs_status.go`, beside `JobsStatusLine` (criterion 3) | needs the width, so it is called from `FooterMessage.View` |
| Glyphs, precedence | `internal/ui/indicator` (unchanged) | `indicator.Glyph`, `indicator.Resolve` are the only sources |

Import graph: `components` → `jobs`, `command`, `theme` (today) + `indicator` (new).
`indicator` → `theme`, `help`, `config`; neither imports `components`: no cycle (checked
with `go list -deps`).

### One computation, two readers (pastille and footer never disagree)

The first column (`datatable.IndicatorColumn`) and the footer must resolve the same state
for the same row. Both go through one unexported `resolveRow(q RowQuery) rowFacts` in
`components`; `RowState` returns only the state (cheap, called per visible row per
render), `DescribeRow` also builds the sentence. A test asserts
`DescribeRow(q).State == RowState(q)` over the whole case table.

### The footer glyph does not blink

The footer prefix is always `indicator.Glyph(state, true)` (`●`, never `○`). `ux.md` §3.6:
"no footer spinner here: the dot already moves on the row; a spinner would double the
motion" — the same holds for a blinking prefix. For the same reason a `Status` carrying a
detail **with a glyph** drops its spinner prefix. Hypothèse : a detail without a glyph
(running container's `Running since 3 hr ago`) keeps the spinner if the view asks for one.

### Cause normalization (transitional)

`Failure.Detail` today repeats the verb (`scan failed — check logs`). Rewriting the ~12
producer strings would change the `:jobs` Detail column, which is not this story's. The
builder therefore normalizes once, in `failureCause(kind, detail)`:

1. every control rune (`\n`, `\r`, `\t`, …) becomes a space, runs of spaces collapse,
   trimmed — a newline in a footer is a second line and breaks Rule 124;
2. one exact leading `string(kind) + " failed — "` is cut (so `scan failed — check logs`
   and `check logs` give the same sentence);
3. empty → the sentence ends at `failed` (no dangling ` — `).

It does **not** redact (it cannot know what a raw error contains): screen-ready causes are
the producer's job (Q-A, Risks). Hypothèse : step 2 of the normalization is removed by the
final A-16 PR once every producer writes a cause only.

### Exact phrases (single source, `ux.md` §1.2)

`<T>` = target display: `Item.Name()` for an active item, `Failure.Display` for a failure;
an absolute path is folded with `theme.FoldHome` (`~`) and shortened under width pressure
by the composer; anything else is verbatim. `<k>` = `string(kind)` (`scan`, `sync`,
`clone`, `pull`, `create`, `delete`, `index`); `<Verb>` = `kind.Verb()`.

| Winner | Detail |
|---|---|
| running | `● <Verb> <T>` → `● Scanning ~/w/app/api` |
| queued | `◴ Queued: <k> <T>` → `◴ Queued: scan ~/w/app/api` |
| failed | `⊗ <T>: last <k> failed — <cause>` (`⊗ <T>: last <k> failed` when no cause) |
| observed (view state, not Idle) | `<glyph of Observed> <ObservedText>` (glyph imposed, text from the view) |
| idle | `<ObservedText>` with **no glyph** (container running, D8), or nothing at all |
| masked failures (suffix) | ` · earlier failure: <k> (see :jobs)`; several kinds: ` · earlier failures: <k1>, <k2> (see :jobs)` (Hypothèse : plural form, kinds in `jobs.Kinds()` order) |
| event, 3 s `Error` (criterion 2) | `Failed to <k> <T> — <cause>` (`Failed to <k> <T>` when no cause) |

Rules of resolution (criterion 6, `indicator.Resolve` order):

- running > queued > remembered failure > observed > idle;
- several running items on the row: the first in run order wins (runs are oldest first);
  the others are not named (Hypothèse : the global progress already counts them);
- several failures: the newest (`At`) wins when failure is the winner; every failure that
  is not the winner is listed in the suffix — including when a running/queued item masks
  them;
- only kinds in `q.Kinds` are read (D5); nil = every kind;
- only `q.Context` is read (runs and failures of other contexts are invisible).

### Composer and truncation (criteria 3, 7)

`RowStatusLine(detail RowDetail, progress string, width int) string` returns plain text
of at most `width` columns, one line. Order of what gives way (criterion 3: the row detail
first, the progress last):

1. full: `detail · progress` fits → as is;
2. the detail's target is an absolute path: shorten it with `theme.ShortPath(target, room)`
   where `room` = what is left after the rest of the detail and ` · progress`, provided
   `room ≥ minTargetWidth` (6) → fits by construction;
3. cut the detail's tail with `theme.TruncateWidth(detail, width − w(" · progress"))`,
   provided that budget is `≥ minRowDetailWidth` (8: glyph, space, a few runes, `...`);
4. drop the detail, keep `TruncateWidth(progress, width)`;
5. no progress: `TruncateWidth(detail, width)` (path shortened first as in 2).

Hypothèse : the two minimum widths are constants of the file, pinned by tests.
Hypothèse : the separator is ` · ` (U+00B7), the one `JobsStatusLine` already uses.

Precedence is untouched (criterion 4): `FooterMessage.View` returns the timed message
before it ever looks at `Status`; the detail returns on its own when the timer expires
because it is derived every frame, never stored. A sync **summary** is a timed `Info`
(`workspaces.syncSummary`), so it also wins over the detail for 3 s (criterion 3, second
half) — no code needed, a test pins it.

---

## Step 1 — `jobs.ActiveOn` (pure, no UI) — parallel with step 2

**Files**: `internal/jobs/active.go` (new), `internal/jobs/active_test.go` (new).

```go
// Active is one unsettled item (queued or running) on a target, with its run's kind.
type Active struct {
	Kind  Kind
	RunID JobID
	Item  Item
}

// ActiveOn returns the unsettled items on target in contextName: running ones first,
// then queued ones, each group in run order (oldest first). With no kinds, every kind
// is kept. Targets are compared through TargetKey on both sides, like FailuresOn.
func ActiveOn(runs []Run, contextName, target string, kinds ...Kind) []Active
```

**Tests** (table-driven, `active_test.go`):

| Given | Expect |
|---|---|
| one run, item running on `/w/api` | one `Active{KindScan, …, Item.State: running}` |
| scan queued (run 1) + sync running (run 2) on the same target | `[sync running, scan queued]` (running first) |
| two runs running on the target | run order |
| item `done`, `failed`, `skipped` | excluded |
| run in another context | excluded |
| `kinds = [KindSync]`, scan running | empty |
| target given as `/w/api/` vs item `/w/api` | matched (TargetKey) |
| image ref `nginx` vs item `nginx:latest` | not matched (TargetKey keeps refs verbatim) |
| open run (clone) with a running item | included |
| empty runs / nil | `nil`, no panic |
| returned `Item` | a copy: mutating it does not change the input run |

**Done when**: `go test ./internal/jobs/` and `mise run lint` pass.

---

## Step 2 — Composer: `RowDetail`, `RowStatusLine`, `Status.Detail` — parallel with step 1

**Files**: `internal/ui/components/row_activity.go` (new — only the `RowDetail` type in
this step), `internal/ui/components/jobs_status.go` (composer beside `JobsStatusLine`),
`internal/ui/components/footer_message.go` (`Status.Detail`, `View`),
tests in `internal/ui/components/row_status_test.go` (new) and
`footer_message_test.go` (extended).

```go
// RowDetail is what the footer says about the selected row. Plain text, no style
// (Rule 128 forbids a footer colour of its own; the glyph links it to the pastille).
type RowDetail struct {
	Glyph  string // bare indicator glyph; "" for none (idle row, D8)
	Lead   string // text before the target, "Scanning "; the whole sentence when Target is ""
	Target string // path or reference; an absolute path is folded to ~ and shortened under pressure
	Tail   string // text after the target, ": last scan failed — check logs · earlier failure: sync (see :jobs)"
}

func (d RowDetail) IsZero() bool
// String renders the detail at full length: Glyph + " " + Lead + FoldHome(Target) + Tail,
// with no leading space when Glyph is "". It is what the phrase tests compare.
func (d RowDetail) String() string

// RowStatusLine composes "row detail · progress" in at most width columns, one line,
// giving way in the order: target path, detail tail, whole detail, then progress.
func RowStatusLine(detail RowDetail, progress string, width int) string
```

```go
type Status struct {
	Text    string
	Spinner bool
	Level   Level
	// Detail describes the selected row (A-05). Zero = the line renders exactly as
	// before. With a glyph, the spinner prefix is dropped (the pastille already moves).
	Detail RowDetail
}
```

`FooterMessage.View`: unchanged up to the timed-message branch; then, when
`status.Detail` is non-zero, the text is `RowStatusLine(status.Detail, status.Text, budget)`
(budget = width, or width minus the spinner when the detail has no glyph and
`Spinner` is set), styled with `status.Level.Style()`, centred. `status.Text == "" &&
Detail zero` → empty line as today.

**Tests**:

| Given | Expect |
|---|---|
| any `Status` with zero `Detail` (golden over: empty, text, text+spinner, Level error, width 1/20/80) | byte-identical to the output before the change (capture the goldens first, from `main`) |
| detail `● Scanning /home/u/w/app/api`, progress `Scanning — 3/12`, width 120, `HOME=/home/u` | `● Scanning ~/w/app/api · Scanning — 3/12` |
| same, width so that only the path must shrink | `● Scanning ~/w/a/api · Scanning — 3/12` (ShortPath reduction), progress intact |
| width where even the reduced path does not fit | detail cut with `...`, ` · Scanning — 3/12` intact |
| width < progress + separator + 8 | progress alone (truncated if needed), no detail |
| no progress, narrow | detail alone, path shortened first then `...` |
| image ref target (`nginx:1.27`) under pressure | never passed to ShortPath; tail cut instead |
| widths 1..140 × {long detail, long progress, both} | `lipgloss.Width(View(...)) == width`, no `"\n"` (Rule 124) |
| same loop with `runewidth.DefaultCondition.EastAsianWidth = true` (restore in `t.Cleanup`, pattern of `datatable/indicator_eastasian_test.go`) | same two invariants (`●` counts 2) |
| `Error` set, detail present | error text shown; after `Handle(ClearFooterMsg{ID: f.ID()})` the detail line is back (criterion 4; build the expiry, do not run the Cmd) |
| same for `Warn`, `Info`, and a `PostFooterMsg` | same |
| detail with glyph + `Spinner: true` + spinner frame set | no frame in the output |
| detail without glyph (`Running since 3 hr ago`) + `Spinner: true` | frame kept |
| `RowDetail{}.IsZero()`; `String()` without glyph | true; no leading space |

Tests that touch `FoldHome` set `t.Setenv("HOME", …)` and `USERPROFILE` (CLAUDE.md, HOME
shared across a test process).

**Done when**: `go test ./internal/ui/components/` passes, and `go test ./internal/ui/...`
shows no golden change in any view (nothing passes a detail yet).

---

## Step 3 — Phrase builder: `RowState`, `DescribeRow`, `FailureEvent` (depends on 1, 2)

**Files**: `internal/ui/components/row_activity.go`, `row_activity_test.go` (new).

```go
// RowQuery names one row: the registry snapshot, the current context, the row's target
// as the registry knows it (Item.Target), the kinds this view can rerun (D5), and what
// the view observes on its own.
type RowQuery struct {
	Runs     []jobs.Run     // ChangedMsg.Runs (any context; filtered here)
	Failures []jobs.Failure // ChangedMsg.Failures
	Context  string
	Target   string
	Kinds    []jobs.Kind     // nil = every kind
	Observed     indicator.State // the view's own state (container, agent); Idle if none
	ObservedText string          // its sentence, without glyph; shown when Observed wins, or on an idle row (D8)
}

// RowActivity is what the first column and the footer say about one row.
type RowActivity struct {
	State  indicator.State
	Detail RowDetail
}

// RowState resolves the row's state without building any text. It is what
// datatable.IndicatorColumn's state func calls; DescribeRow agrees with it by construction.
func RowState(q RowQuery) indicator.State

// DescribeRow resolves the row and builds its footer sentence (ux.md §1.2).
func DescribeRow(q RowQuery) RowActivity

// FailureEvent is the 3-second Error a view posts when an operation fails (criterion 2):
// "Failed to <kind> <target> — <cause>". Same normalization of cause as the persistent line.
func FailureEvent(kind jobs.Kind, target, cause string) string
```

Internals: `resolveRow(q) rowFacts` (uses `jobs.ActiveOn` and `jobs.FailuresOn`, feeds
`indicator.Resolve`), `failureCause(kind, detail string) string`, `targetDisplay(s string)`
(no width; the composer shortens), `maskedSuffix([]jobs.Failure) string`.

**Tests** (table-driven; `HOME` set so `/home/u/w/app/api` folds to `~/w/app/api`):

| Given | `State` | `Detail.String()` |
|---|---|---|
| scan running on the target | Running | `● Scanning ~/w/app/api` |
| sync running | Running | `● Syncing ~/w/app/web` |
| scan queued | Queued | `◴ Queued: scan ~/w/app/api` |
| scan failure, Detail `scan failed — check logs` | Failed | `⊗ ~/w/app/api: last scan failed — check logs` |
| scan failure, Detail `check logs` | Failed | identical to the previous line |
| failure, Detail `"line1\nline2\tx"` | Failed | `… — line1 line2 x` (one line) |
| failure, Detail `""` | Failed | `⊗ ~/w/app/api: last scan failed` |
| scan running + sync failure | Running | `● Scanning ~/w/app/api · earlier failure: sync (see :jobs)` |
| queued + sync failure | Queued | `◴ Queued: scan ~/w/app/api · earlier failure: sync (see :jobs)` |
| sync failure (older) + delete failure (newer) | Failed | `⊗ …: last delete failed — … · earlier failure: sync (see :jobs)` |
| running + two failures | Running | `… · earlier failures: sync, delete (see :jobs)` |
| sync failure, `Kinds = [KindScan]` (`:sec`, D5) | Idle | zero detail |
| failure in another context | Idle | zero detail |
| nothing, `Observed = Idle`, `ObservedText = "Running since 3 hr ago"` | Idle | `Running since 3 hr ago` (no glyph, D8) |
| nothing, no observed text | Idle | zero detail (criterion 5) |
| `Observed = Restarting`, text `Restarting — 4 restarts, last exit code 1` | Restarting | `◕ Restarting — 4 restarts, last exit code 1` |
| `Observed = Restarting` + scan failure | Failed | failure sentence (failure > observed) |
| image target `nginx:1.27`, pull running | Running | `● Pulling nginx:1.27` (verbatim, no fold) |
| failure with `Display` set by the launcher | Failed | uses `Display` |

Cross-cutting tests:

- `TestRowStateAgreesWithDescribeRow`: every case above, `RowState(q) == DescribeRow(q).State`.
- `TestRowStateFollowsResolve`: for the 8 combinations of running/queued/failed × an
  observed state, `RowState` equals `indicator.Resolve` of the same facts (criterion 6).
- `TestEveryKindHasPhrases` (criterion 8): walking `jobs.Kinds()`, the running sentence
  never contains `Working` (the `Verb()` fallback), and queued / failed / event sentences
  contain `string(kind)`.
- `TestTheFooterGlyphIsTheSteadyOne`: running detail uses `indicator.Glyph(Running, true)`;
  every detail glyph equals `indicator.Glyph(State, true)` (no literal).
- `FailureEvent`: `Failed to scan ~/w/app/web — trivy could not pull the database`;
  `scan failed — check logs` → `Failed to scan ~/w/app/web — check logs`; empty cause →
  `Failed to scan ~/w/app/web`.
- Optional `BenchmarkRowState`: 20 runs × 50 items, 500 failures; recorded in the PR, no
  threshold (sizing input for the wiring stories, which may cache per `ChangedMsg`).

**Done when**: `go test ./internal/ui/components/ ./internal/jobs/` pass, `mise run lint`
clean.

---

## Step 4 — Guard test and documentation (depends on 3)

**Files**: `internal/ui/components/footer_guard_test.go` (extended),
`docs/architecture/ui-components.md`, `docs/backlog.md`.

1. `TestNoViewSpellsARowActivityPhrase` (criterion 8), on the existing `forEachUIFile`
   walker: outside `internal/ui/components/`, a string literal containing
   `"earlier failure"`, `"Queued: "` or `": last "` fails, naming file and line. (None
   exists today — checked.) `"Failed to "` is **not** banned: several views already write
   their own (`explorer/create.go`, `templates/update.go`, …); their migration stories move them to
   `FailureEvent`, and A-16 may then add the ban.
2. `ui-components.md`, section "Le footer — `components.FooterMessage`": the row detail
   (`Status.Detail`), the composer and its truncation order, `DescribeRow` / `RowState`
   (one computation, two readers), the steady footer glyph, the cause normalization and
   its end date (A-16), the phrase table.
3. `docs/backlog.md` §1.1: entry for DEVDESK-140 (decisions above, Q-A/Q-B answers once
   given).

Rules files (`.claude/rules/tui-behavior.md`, Rule 128) are **not** edited here: A-17
owns the rules; the architecture note is enough for the code to be read.

**Done when**: `mise run check` passes (fmt, vet, lint, test); `mise run test-race` on
`internal/ui/components` and `internal/jobs`.

---

## Final check (whole PR)

- `go test ./...` with no golden change outside `internal/ui/components` and `internal/jobs`.
- `git grep -n 'RowDetail\|DescribeRow' internal/ui | grep -v components/` → empty (no wiring).
- The PR description lists Q-A / Q-B and their answers (or "default kept").

## Risks

| Risk | Mitigation |
|---|---|
| **Raw `err.Error()` reaches the footer** (clone, index): a git error can carry a remote URL, possibly with credentials in it, and the footer is on screen and in screenshots. | Normalization keeps it on one line only. Not shown by any view until A-10b; Q-A option C makes rewriting those two sites a condition of A-10b. Flag for `security` review at A-10b. |
| `RowState` called per visible row per render scans runs and up to 500 failures. | Linear and allocation-light; benchmark in step 3; wiring stories may resolve once per `ChangedMsg` and keep a map. |
| Prefix stripping (`<kind> failed — `) is a text heuristic. | Exact, generated from the kind, covered by a table of every current producer string; removed at A-16 when producers write a cause only. |
| Under `EastAsianWidth`, `●` is 2 columns: a 1-column assumption would overflow. | All widths through `runewidth`/`theme.StringWidth`; the EastAsian loop in step 2. |
| `FoldHome` depends on `HOME`: flaky tests under `-count=2`. | `t.Setenv("HOME")` + `USERPROFILE` in every test that folds. |

**Irreversible points**: none (no persistence, no config key, no public format).
