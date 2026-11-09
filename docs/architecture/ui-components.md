# The footer and the tables — shared UI components

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## Le footer — `components.FooterMessage`

One line of state below every viewport, and **one implementation**. There were
eight, one per view, each with its own pair of fields, its own clear message and
its own lipgloss block — which is precisely how the errors ended up left-aligned
everywhere while the notices were centred: nobody ever centred the error branch,
in any of the eight.

```go
type Model struct { footer sharedcomponents.FooterMessage }

// From Update() only (Rule 110). The Cmd is the expiry timer; a message set
// without it never clears.
return m, m.footer.Error("Failed to load — check logs")
return m, m.footer.Warn("Scan already in progress")
return m, m.footer.Info("Image pulled: " + name)

m.footer.Handle(msg)                              // consumes its own expiry
m.footer.SetSpinnerFrame(m.spinner.View())        // from the spinner tick

// From View(), read-only. Always one full-width, centred line — empty included,
// because Rule 124 budgets one whatever it holds.
m.footer.View(width, m.status())
```

**Three levels, defined by what happened rather than by how it feels.** An
`Error` is an operation that failed or was refused by the system; a `Warn` is an
action that cannot be honoured as asked while nothing failed — a precondition
unmet, something already running, a setting with no meaning here; `Info` is a
neutral fact or a success.

| Level | Colour |
|---|---|
| Error | `ColorFooterError`, the **CRITICAL** severity red |
| Warning | `ColorFooterWarn`, the **MEDIUM** severity orange |
| Info | `ColorFooterInfo` — `ColorText`, the ordinary text colour |

The three are **semantic aliases assigned in `ApplyTheme`**, like the viewer's
syntax colours: no theme file gains a key. They alias the **severity** colours
rather than `ColorError`/`ColorWarn`, which the default theme makes identical —
the choice is invisible today and stops being so in a theme that separates them.
Info is `ColorText` because `ColorHighlight` carried it before and is a yellow
one notch from the warning's orange: the two levels were indistinguishable.
Info is not bold, the other two are.

**Green is gone from every footer.** It belongs to status icons (Rule 121); the
security view's status line was the one place it leaked in.

**The expiry is identified.** `ClearFooterMsg{ID}` only clears the message it
was started for, which is what makes the type safe to share between packages —
and what fixes the defect all eight local implementations had: a message set at
t+2.9s was wiped at t+3s by its predecessor's timer.

**`Status` is the second argument, and it has no timer.** A progress line, a
hint, a table loading its rows — these are states the view derives on every
frame, not events. A batch outlives the three seconds a message gets, so a line
set when the first repository started would clear while the tenth was still
fetching. It is a parameter rather than a field because it is derived: the
containers progress line (`progressLine()`, ex `actionLine()`) comes from `BusyLabels()`, which changes with no event to
push it on.

Precedence inside `View`: **error → warning → info → status**. A failure the
user has not read yet matters more than the progress of what is still running.

**A table's load is a footer status with a spinner** (Rule 139). The table stays
on screen: a body that swapped itself for a spinner lost its header and its
columns for the length of every `ctrl+r`, then got them back. The empty state
(`No images found`) is therefore conditional on the load being over, or the
table announces the absence of what it is in the middle of fetching.

The spinner frame is the **rendered** `spinner.View()`, not a bare frame: every
view already styles its spinner with `theme.SpinnerStyle()`, and restyling would
nest one escape inside another. It is measured with `lipgloss.Width`, which
ignores escapes — the opposite of a table cell's rule (Rule 122), and the
difference is which measurer is doing the work.

**`FooterMsgDuration` is an exported var, and only tests touch it.** `tea.Tick`
blocks for its whole duration and `testutil.Msgs` runs every command it is
handed, so a test inspecting a Cmd that carries the timer paid three real
seconds — four did. A test that wants to see a message expire builds
`components.ClearFooterMsg{ID: m.footer.ID()}` instead; one that must drain the
Cmd calls `testutil.FastTimers(t, &components.FooterMsgDuration)`.
`TestAMessageGetsThreeSeconds` pins the production value.

Two source-level tests hold the line, on the model of `internal/ui/keymap`'s:
`TestNoViewStylesItsOwnFooterMessage` fails on a `StatusErrorStyle`,
`StatusOKStyle`, `StatusWarningStyle` or `ColorHighlight` inside a
`RenderFooter`/`renderInfoLine`/`renderInfoText`, and
`TestNoTableViewRendersALoadingBody` fails on a `theme.SpinnerMessage` in a
table view. The one screen that legitimately fills itself with a spinner — the
registry browser mid-pull, which has no table behind it — is written down as a
declared exception, the way `keymap.DeclaredExceptions()` is.

## Tables — `internal/ui/datatable`

The shared mechanism behind the application's tables: column widths, sorting,
sort arrows, filter matching, cursor clamping and cursor-to-object resolution.
`theme.DefaultTableStyles()` and friends still own the *look*.

```go
datatable.New(datatable.Config[T]{
    Columns: []datatable.Column[T]{{
        Title:  "Name",
        Sizing: datatable.SizingContent, // or SizingFixed — no default (§3.45)
        MinWidth: 20,   // a floor now, and the exact width of a Fixed column
        MaxWidth: 48,   // 0 = no ceiling; only meaningful on SizingContent
        Optional: false, // true = droppable before any column that is not
        TruncateHead: true, // cut the start, for values told apart by their end
        Flex:   1,      // share of what is left once everyone has what it wants
        Cell:   func(x T) string { … },  // plain text — Rule 122 by construction
        Style:  func(x T) lipgloss.Style { … }, // nil = the table's own colours
        Less:   func(a, b T) bool { … }, // nil = not sortable
        Search: func(x T) string { … },  // nil = not searchable
    }},
    SortColumn:     0,
    SortDesc:       false, // true opens on the descending order
    SelectedStyles: func(x T) table.Styles { … }, // e.g. TableStylesForSeverity
})
```

### Every column declares its nature (§3.45)

`Sizing` has **no default**, and `TestEveryColumnDeclaresItsSizing` — a source
test over `internal/ui`, on `internal/ui/keymap`'s model — fails naming file,
line and column title. The zero value could have meant something and both
candidates are worse than the absence: "fixed, never dropped" leaves no column
droppable so the last resort decides everything, and "content, droppable" makes
the identifying column of twenty tables removable by default. That is D12 under
another name.

| | `SizingFixed` | `SizingContent` |
|---|---|---|
| **not `Optional`** | `CRIT`, `Secrets`, `Scanned` | `Target`, `Interface`, `Name` |
| **`Optional`** | `RX err`, `TX err`, `Kind` | `IPv6`, `Mountpoint`, `Remote` |

`Fixed` + `Optional` is what ruled out a single three-valued enum: the interface
error counters have an exact width *and* are the first thing worth losing.

**The degradation, in order.** Content columns shrink towards their `MinWidth`,
truncating; then whole columns are **removed**, the `Optional` ones first and
always the rightmost, and the budget is solved again — a removed column hands
back its two padding cells, which is what closes D61; then, when only columns
nobody marked optional are left, they go too, still right to left. That last
step makes the declaration a preference rather than a guarantee, deliberately:
fewer columns that are right beats every column wrong, and truncating a fixed
column instead renders `142` as `14…` with nothing on screen to say so.

**`DropFirst` is the one exception to "always the rightmost" (§3.71).** An
`Optional` column carrying it goes before every other `Optional` one, wherever
it sits. Right-to-left works because a table's column order *is* an order of
importance — true of every column whose place is chosen by what it **is**, and
false for one whose place is chosen by what it **illustrates**. The containers
gauges have to sit beside the numbers they draw, in the middle of the row; left
to position alone they would outlive the four I/O counters to their right, which
is backwards for the most expendable thing on the line. It is a bool and not a
rank: two tiers is what the case needs, and an int would invite every table to
number its columns against each other.

**`YieldsToContent` moves the moment, not the order (DEVDESK-128, D93).** By
default a shortfall is taken from the `SizingContent` columns first and a column
is dropped only when they are all at their floor. An `Optional` column carrying
`YieldsToContent` reverses that for itself: while it is the column `drop()`
would remove next, the solver cuts only the *Optional* content columns and, if
that is not enough, drops it before any required content column is cut below
its widest value. The drop order is untouched (`dropAt` is shared by `drop` and
the decision, so they cannot disagree); the flag only works on a prefix of that
order, and once the next column does not carry it the solver is the old one.
Flag at false: strictly unchanged. Only the agents table sets it (Cache write,
Est. cost, Cache read, In, Breakdown). Not global, because measured over 2,822
solver inputs a global rule changed 41 layouts in 7 tables.

**A kept column never goes under one cell.** A column at zero renders nothing
while its padding has already been spent — D61 word for word — so "too narrow to
serve" and "not there" have to stay different states, and the second one is a
removal.

**`MinWidth` keeps its name on a `Fixed` column, where it *is* the width.**
`Width` would be *false* on a content column that grows past it; `MinWidth` is
merely redundant where the minimum happens to be the maximum. A redundant name
is bearable, a false one is not.

**`MaxWidth` has no default and `0` means no ceiling.** One chosen globally
would apply to columns nobody has looked at, and a full IPv6 address is 45
cells — so the first "reasonable" default re-truncates exactly what the
measurement was for. It is set only where values are genuinely unbounded:
`Image` in containers, `Name` in the images list, `Target` in the `:sec`
inventory.

**`TruncateHead` is per column, not derived from `Sizing`.** A URL, an image
reference and a path share their prefix and are told apart by their end; an
address is identified by the network it starts with. It cannot be left to `Cell`
either — `Cell` does not know the width it will be rendered at, and that is what
makes the value measurable.

### When the measurement is taken

`Cell` is called once per cell of every visible row, off screen included, so it
is officially **pure and cheap**. Nothing can check that — it is an arbitrary
closure — so it is a sentence in the package doc rather than a test.

The hard half is *when*. `datatable` cannot tell a two-second refresh from a
change of population: both arrive through `SetItems`. So:

| | Measures again |
|---|---|
| the first non-empty population | yes — otherwise the table sits at its `MinWidth`s for the life of the view |
| `Resize` | yes — the user just changed how much room there is, and the content has not moved |
| a search edit, a token toggle | yes — a user action on a settled population |
| every other `SetItems` | **no** |
| `Remeasure()` | yes — the view saying the population changed for a reason |

`Resize` being a measurement moment removed half the wiring the entry expected:
several views already call `Resize` right after `SetItems` because they
recompute their height, which covers registries, netdiag's results and the
browser's tags for free. Four explicit `Remeasure()` calls are left — the
explorer's drill-down, `ws`'s change of directory, `:sec`'s change of tab, and
the viewer tree's fold. The tick-refreshed tables (containers, ports,
interfaces, status) call no `Resize`, so they do not measure again, which is
exactly the property wanted.

`ws` had to detach `refreshRows` from `setEntries` for this: both went through
one point and one of them arrives several times a second while a scan runs, so
measuring there would have moved the columns at the spinner's rhythm.

The cost is written down rather than discovered: a value that grew between two
measurements stays truncated until the next one.

### Rule 116 is stated on the rendered span

`RenderedWidth()` is what the table's lines actually span, and it is what a
view's layout test asserts on. Summing the declared columns and adding two each
is the tempting version, it is what all eleven of those tests did, and it is
wrong the moment a column is dropped: the dropped column renders nothing and its
padding goes back into the budget, so the formula asks for less than the line
spans and fails on a layout that is correct. That disagreement was D61 seen from
the other side.

`SortDesc` exists for count columns: ascending is their useless end, and cycling
`.` past it on every open is not a default.
`Column.DescFirst` is the same idea per column: the first `.` that lands on the
column sorts it descending, the second ascending, then on to the next sortable
column. A table that opened sorted never goes back to "no sort", DescFirst or
not; one that opened unsorted returns to it after the last column. Meaningless
without `Less`. The `:agent-usage` agents table uses it on `Total`. A direction with no sortable column
to apply it to is dropped with the column, or the first `.` opens descending
with the arrow on nothing.

**A sortable column's `MinWidth` is not its whole ask.** `titleFor` appends a
sort arrow the view never accounted for, so `solveWidths` reserves
`width(Title) + 2` for any column with a `Less` — otherwise a narrow one renders
`CRIT ▼` into five cells and loses exactly the character that says how it is
sorted. The reserve applies whether or not the column is the sorted one, so
cycling `.` does not resize it and shift every column beside it.

**The package renders its own rows** (`render.go`), and that is what makes
`Style` possible at all. `bubbles/table` measures a cell with `runewidth`
*before* styling it, and runewidth counts an escape sequence's bytes as width: a
seven-cell string carrying a colour measures 28, so it is truncated in a column
twice wide enough and the cut lands inside the escape — the unterminated
sequence then bleeds over every row below. That is Rule 122, it is a
`bubbles/table` limitation rather than a Bubble Tea one, and `bubbles v1.0.0`
has the same line. Inverting the order — `Cell` is measured while plain, `Style`
is applied to the finished cell — makes the failure unexpressible instead of
forbidden by review.

bubbles is still the state: rows, columns, cursor, focus and height. What moved
here is the drawing and the scroll offset (`clampOffset`), which its viewport
kept unexported. `Table()` reports the same thing it always did.

Two consequences worth keeping:

- **`Style` is not consulted for the selected row.** That row goes to
  `styles.Selected` whole, and a colour inside it closes with a reset that takes
  the selection background with it for the rest of the line. The highlight
  answers "where am I"; no per-cell colour is worth ending it mid-row.
- **Every cell on an unselected row carries an explicit background.** lipgloss
  does not inherit one (Rule 115) and the app's viewport style only reaches
  cells that emit nothing, so one coloured cell would otherwise strip the
  background from everything to its right. A column declaring only a foreground
  gets `ColorBackground` filled in.

A colour that appears on every row informs no one: a zero count, a `-` and a
never-scanned target are `DimStyle`, the nominal majority state (a `running`
container) keeps the default text colour, and the colour is spent on what is
worth spotting without reading.

**`Cut` and `TailStyle` give a cell two colours instead of one, and still measure
before colouring.** They exist for one caller — the containers load gauges
(§3.71): the fill takes `Style`, the track takes `TailStyle`, and `Cut` says
where the first stops, in cells, of the *already-fitted* text. This is not the
gradient Rule 122 forbids — nothing styled is ever measured, `Cut` reads a
plain string exactly as `Style` does — it is the same "measure first, colour
after" order applied to two runs instead of one. `splitCellRun` pads each run's
own outer edge with a literal space rather than `Style`'s `Padding(0, 1)`,
because two independent `Padding` calls would open a two-cell gap where the
runs meet. Neither `Cut` nor `TailStyle` is consulted on the selected row, for
the same reason `Style` is not.

**`Spans` generalises this to N runs.** `Column.Spans(item) []Span` returns
`Span{Width, Style}` values: each paints `Width` cells of the already-fitted
text, left to right, and the cells after the last span take `Style`. Negative
width counts as 0, zero renders nothing, a span past the end is cut; widths are
rune counts, so the cell holds single-width glyphs. A nil or empty result takes
the one-run path. `Spans` takes precedence over `Cut`/`TailStyle`, but mixing
them in one column is rejected (`TestNoColumnDeclaresSpansWithCutOrTailStyle`).
On a row a view has coloured whole (error, busy, severity) the spans are
dropped; on the plain normal selection every span repaints
`ColorTableLineSelected` and bold itself. A span `Style` must not carry Width,
Padding or Margin. First user: the token breakdown bars (DEVDESK-98).

Three things it guarantees that hand-wired tables did not:

- **`Selected()` cannot disagree with the screen.** The filtered, sorted slice is
  built once and kept; nothing replays the pipeline to resolve a cursor.
- **Rule 116 holds at every width.** The solver distributes the shortfall across
  columns rather than clamping each one after the remainder is computed, which is
  how several views overflowed on narrow terminals — and it removes a column
  rather than emptying one, which is what makes the rendered line span the
  viewport interior at every width (D61).
- **The cursor is clamped in one place** — `SetItems`, both ends — and otherwise
  left where it was, so a periodic refresh keeps the scroll position. `GotoTop()`
  is explicit for views that do want a reset.

`SelectedStyles` returns styles rather than a state keyword so the component
never learns what a severity is. `containers` is its one client: it is the only
table whose selection colour depends on the row (exited or dead reads as an
error).

When a row shows something that is not on the domain object — the images tab
shows the scan cache, whether a scan is running, and the alias-substituted name
— the view defines a **row type** carrying that decoration (`imageRow`) rather
than closing over the model. The columns are built once, in `New`, so they
cannot reach it; and carrying it means a column sorts by the same value it
prints.

`SetCursor` is clamped and exists for the views that remember a position across
a reload — workspaces and the explorer each restore one per drill-down level.
The selected row is pinned to the content width: column widths count cells, and
a Nerd Font icon does not always render as wide as it counts, so the highlight
would otherwise stop short of the right border.

**A cell's colours are decided here, not in `theme.DefaultTableStyles()`.**
Neither it nor bubbles sets a foreground on `Cell`, so a column with no `Style`
used to render in the terminal's own colour, whatever the theme said — four views
had copied `Foreground(theme.ColorText)` into a `Style` to get it back. It cannot
be fixed on `Cell`: the cells are rendered and *then* the whole line is handed to
`styles.Selected`, so a colour inside it opens a sequence whose reset ends the
highlight mid-row. `cellStyle` fills in both `ColorText` and `ColorBackground` on
unselected rows only, and a column declaring one of the two gets the other.

A column with no opinion should return the zero `lipgloss.Style` rather than
naming the theme's text colour itself.

### A row an action is running on (§3.22)

A row says two things: what the object **is**, and what is **happening** to it.
They share one glyph on purpose — one column answers "what about this row" — and
`datatable` owns the one rule that follows: **busy wins over state**, because
`exited` is precisely what the action is about to change.

```go
datatable.Config[T]{
    Key: func(c docker.Container) string { return c.ID }, // nil ⇒ facility off
}

m.table.MarkBusy(id, "Stopping web")  // from Update, never a Cmd (Rule 110)
m.table.ClearBusy(id)                 // on *every* outcome, failures included
m.table.IsBusy(id)                    // the guard
m.table.BusyLabels()                  // sorted, for the footer
```

**It cannot be a `Busy func(T) bool` on the config.** Columns are built once in
`New` and close over nothing — that is why `imageRow` exists — so a predicate
there would have to close over the view's map of in-flight actions. Separating
the identity (`Key`) from the state is what avoids it, and it buys the thing
that matters most: **`IsBusy` answers for an object whose row does not exist
yet**, so it is also the guard on a confirm path. It is also what survives the
periodic refresh, which replaces the items mid-action.

A busy row is drawn by the table's `Indicator` column: `●` / `○` in that column,
the router's `jobs.FrameMsg` supplying the blink phase. The table owns no
spinner and no frame (`StatusColumn` and `AdvanceSpinner` were removed by
DEVDESK-143).

Rendering, and each part is a decision:

- The glyph shows **on the selected row too** — a signal that vanishes under
  the cursor is lost exactly when it is being looked at.
- Off the cursor every cell of the row goes `DimStyle` and the column's own
  `Style` is dropped.
- **On** the cursor the highlight itself changes
  (`theme.TableStylesForState("busy")`), overriding whatever `SelectedStyles`
  returned — a colour inside the joined row would close with a reset and end the
  highlight mid-row (`render.go`).
- The override is applied **when the row is drawn**, not when it is built, so
  the stored cells do not go stale as the phase advances. Tests assert on
  `View()`, not on `Table().Rows()`.

The activity column is the table's column 0, untitled and not sortable (Rule 125).

Every table that calls `MarkBusy` has one: `containers` (container ID; stop,
restart, pause/resume, remove), `oci` images (image ID; remove), networks
(network ID; remove), volumes (name, a volume has no ID; remove), registries
(URL; login, logout) and `netdiag` ports (PID; kill).

Two of those keys are worth the note. The ports table keys on the **PID, not the
socket**, so every row of a process shows the dot at once — which is what happens, the
kill takes them all. The registries table keys on the **URL**, which is the one
place D40's rule does not apply, and it does not apply because the *operation*
is host-scoped: one `docker login` really does change the answer for every entry
on that host.

`oci_resources` gathers `BusyLabels()` from **all four tabs**, not the active
one: an action started on Images goes on running after `tab`, and a spinner that
stopped turning because the user looked elsewhere would read as a hang on the
way back. Row state is drawn by the activity column and animated by the router's
`jobs.FrameMsg`; the view's own spinner turns only while a tab loads.

Deliberately outside this:

- **Scans** — they do not block the object the same way, so they are not a
  `MarkBusy` key. Where a table shows them (Images, `:sec`, `ws`), the dot comes
  from the registry (`jobs.ActiveOn` through `RowState`).
- **`prune`** — it acts on no row, so marking every row would say something
  false. It gets a footer line rendered from the view's own state (`m.pruning`),
  like `syncStatusLine` (§3.17), because a footer *message* expires after three
  seconds (Rule 128) and `docker stop` outlives that by seven.
- **`workspaces`** — it already had all of this, its own way (`scanningPaths`,
  `syncingPaths`, `deletingPaths`, `busy(path)`), and is in fact where the
  design came from. Its delete was the one action its own machinery did not
  know about, and §3.23 step 1 taught it rather than migrating the view.
  Migrating would be a refactor of working code across the one busy notion that
  is *not* one object, one action: scan and sync exclude each other across
  nested paths, a sync targets a tree rather than a row, and the spinner does
  not spend the same cell for every operation.

**One rule for a busy row** (DEVDESK-137, DEVDESK-143): it shows `●` / `○` through
the `Indicator` column. A table with a `Key` but no `Indicator` column shows
nothing but greyed cells.

**Every table in the application is a `datatable`** (§3.21 moved the last four:
Registries, the registry browser's tags, network-inspect and netdiag's results).
A new table uses it; there is no second way to build one, and `bubbles/table` is
imported outside this package only for its `Styles` type.

The Registries tab is the one worth knowing about, because it shows **two
populations in one table** — the configured entries, and one group's discovered
members after `→`. `datatable.Model[T]` is generic over a single `T`, so
`registryRow` carries both and holds the index of the config entry it stands
for, `-1` for a member. That index is what `getSelectedRegistry` resolves
through: indexing `m.registries` by row number is right only while the table
neither sorts nor filters, which is exactly the dependency nothing signalled.
`explorerRow` exists for the same reason.

Three views keep a filter of their own, and deliberately. `security` selects
findings **by tab**, and `status` drives both its tables from one search box so
the header counts agree — in both cases the view filters and calls `SetItems`,
because a `FilterBar` query narrows a list that is already settled and these
decide which rows exist at all. `security` also calls `GotoTop` explicitly on a
tab change, which is the reset `SetItems` does not make. Its **severity** goes
the other way: four cumulative tokens the table owns, and its sort and its
search are the table's too. The
registry browser is the third: its text filter and its registry filter narrow
the tags *before* the table sees them, and its bar is drawn in the OCI view's
own footer rather than the table's. Its **sort** is the table's — `.` cycles
Tag and Updated through `CycleSort`, and `SetSort` is what puts a new search
back on the default order.

`status` is the two-table case: two `datatable.Model` plus a focus helper.
`Focus` and `Blur` carry the styles with them (Rule 118), so a tab switch does
not touch `SetStyles`.


### The activity indicator column (DEVDESK-137)

`internal/ui/indicator` is a pure package (no import of `theme`, `jobs` or
`datatable` in `indicator.go`) that names what a row's first column can say.

| State | Glyph | Meaning |
|---|---|---|
| `Idle` (zero value) | none | the row's own icon, or an empty cell |
| `Running` | `●` / `○` (blinks) | an operation is running; busy counts as Running |
| `Queued` | `◴` | waiting |
| `Failed` | `⊗` | failed, or container dead / exited non-zero |
| `Suspended` | `◐` | agent idle, container paused |
| `Restarting` | `◕` | container restarting |
| `Done` | `◉` | job or agent done |
| `Cancelled` | `⊘` | job cancelled |
| `Skipped` | `⊖` | job skipped, container exited 0 |
| `Unknown` | `◌` | agent unknown, container created |

`Resolve(Activity{Running, Queued, Failed, Observed})` applies one order:
**running > queued > remembered failure > observed > idle.** `Observed` is the
state the view computes itself (a container's status, an agent's age).
`Glyph(state, on)`, `Animated(state)` and `States()` are the only sources of
glyphs; `Color` and `Style` (`style.go`) map states to the four
`theme.ColorActivity*` aliases.

`Legend(extension...)` returns the "Activity indicator" help section every
migrated view shows; the per-view rows are `JobsLegend`, `CloneLegend`, `AgentsLegend` and
`ContainersLegend` (its busy entry: "an action (stop, restart, pause, resume, delete) is running", DEVDESK-152).

On the table side:

```go
datatable.IndicatorColumn(
    func(j job) indicator.State { return j.state },   // the state
    func(j job) string { return "" },                 // rest: the row's icon, nil ⇒ empty
    nil,                                              // rest style
)
m.table.SetBlinkPhase(on) // from Update, on jobs.FrameMsg (msg.Blink); default on = steady ●
```

- `Column.Indicator func(T) indicator.State` is set by `IndicatorColumn` only.
  If several columns carry one, only the first is honoured (`indicatorAt`).
- **Precedence at render** (`indicatorState`, `render.go`): a row that is busy
  (`MarkBusy`) is `Running` whatever the column says; otherwise the column's
  own state. Busy still wins over the view's state, as in §3.22.
- The phase is **read at render**, so `SetBlinkPhase` rebuilds nothing, and a
  table never calls it in a test that wants the steady glyph.
- **Padding follows the terminal.** The glyphs `● ○ ◐ ◕ ◉` have ambiguous
  width. lipgloss always counts them as one cell; a terminal in East Asian mode
  draws two. An indicator cell is therefore padded with
  `runewidth.FillRight`, which follows `runewidth.DefaultCondition`.
- **The selected row is padded structurally**, not by measuring the rendered
  line: `padToContent` adds `gap = width - 2 - RenderedWidth()` spaces, computed
  from the column widths. A second measurement with another library would
  disagree on the same glyphs. `TestRule116HoldsUnderEastAsianWidth` pins it.

- **`Shapes()` and `ActivityTable`** (`shape.go`, DEVDESK-142). A cross-view
  guard cannot call a generic `Model[T]` without knowing `T`, so
  `(*Model[T]).Shapes()` returns a fresh `[]ColumnShape` on each call
  (`Title`, `Sizing`, `MinWidth`, and whether the column declares
  `Indicator`, `Less`, `Search`). It reports the declared columns, in order,
  even for an empty table, and mutating the result changes nothing. The
  `ActivityTable` interface (`Shapes()` and `RenderedWidth()`) is the read-only
  face a view hands out for such a guard.

**Width cost.** The activity column takes `IconColumnWidth` (2) plus the
column gap (2): **4 rendered cells**. Every optional column of a migrated table
therefore appears 4 terminal columns later than before, and at 60 columns a
required column may give way (Rule 116 allows a whole column to go, never a
truncated value). The dot is never the column that gives way. The measured
thresholds live with each view: Images and the other OCI tabs in `network.md`
(table of before and after), the containers table in `network.md`, the
`:agent-usage` agents table in `agent-usage.md`, `:sec` in `scanning.md`.

**Known limit.** The header's sort arrow `▲` is also ambiguous-width and is not
fitted by `runewidth`: under East Asian width the header line can be one cell
off. Only the header, not the rows. It is accepted for now and the test skips
line 0.

### The selected row in the footer (DEVDESK-140)

The footer can say what the selected row is doing, in the same words the
activity column's state implies. Every migrated view uses it (DEVDESK-142 to
153); the exempt views have no row activity to say (see the guard in
`app-shell.md`).

**`jobs.ActiveOn(runs, context, target, kinds...) []jobs.Active`**
(`internal/jobs/active.go`). The running and queued items on a target in a
context: running first, then queued, each group in run order. Target is
normalised with `TargetKey` on both sides. No `kinds` keeps every kind. Items
are copied without their cancel hook. No match returns nil.

**`Status.Detail` and `RowDetail`.** `Status` gains a `Detail RowDetail`
(`footer_message.go`); `Status.Text` then becomes the batch progress that
follows it. `RowDetail{Glyph, Lead, Target, Tail}` is one sentence; `Target` is
a field of its own because only a path is ever shortened.

- **`RowStatusLine(detail, progress, width)`** (`jobs_status.go`) joins
  them as `<detail> · <progress>` (`rowSeparator`) on at most `width` columns.
  Zero detail: the progress alone. No progress: the detail alone.
- **Cut order.** A path target shrinks first with `theme.ShortPath`, never
  below `minPathCols` (6), then the end of the detail is truncated. An image
  reference is not a path (`isPathTarget`: absolute or `~`) and is only
  truncated. If fewer than `minDetailCols` (8) columns are left for the detail
  (or fewer than it needs, when it is shorter), the detail is dropped whole and
  the progress stands alone.
- **Precedence is unchanged:** error → warning → info → status. `Detail` is
  part of the status, so a footer message still wins over it.
- **A detail with a glyph suppresses the spinner:** the row's own dot already
  moves.
- **East Asian width.** `centerLine` pads by `lipgloss.Width`, which counts
  ambiguous glyphs as one cell. `View` shrinks its target width by
  `theme.StringWidth(line) - lipgloss.Width(line)` so the line is `width`
  columns for `runewidth` too.

**One constructor of sentences** (`row_activity.go`). The view fills a
`RowQuery{Runs, Failures, Context, Target, Kinds, Observed, ObservedText, Extra}`
(runs and failures of any context; they are filtered here) and calls:

- **`RowState(q)`**: the `indicator.State` for the activity column, no text.
- **`DescribeRow(q)`**: `RowActivity{State, Detail}`. Both go through
  `resolveRow`, so the glyph in the table and the sentence in the footer cannot
  disagree. The state follows `indicator.Resolve` (running > queued > failure >
  observed > idle).
- **`FailureEvent(kind, target, cause)`**: the text of the 3-second Error a
  view posts when an operation fails: `Failed to <kind> <target> — <cause>`.
- **`RunIndicator` / `ItemIndicator`** and **`DescribeRun` / `DescribeItem`**
  (`job_activity.go`): the same for a `:jobs` row, which is a run or one of its
  targets rather than a looked-up target. States map one to one (never `Idle`);
  the sentences reuse the helpers above, so `DescribeRun` of a running or queued
  run is word for word `DescribeRow`'s. Settled rows add: `<glyph> <Kind> of
  <target> finished[ <time ago>]`, `... was cancelled`, `... skipped[ — <detail>]`
  (item), and `<glyph> <target>: last <kind> failed — <cause>` (the failed
  item's detail, or `N of M targets failed` for a run with several).

`RowQuery.Extra []RowTarget` (DEVDESK-149) is for a row that answers for several
registry identities (`:templates`: sync on the slug, scan on its scan
directory). `RowTarget{Target, Kinds}` takes its own kinds (nil = all) and the
query's `Context`. Active runs and failures of every target are merged, then
sorted as for one target (running before queued; failures newest first). With
`Extra` nil the result is unchanged. `ws` keeps its own ranking for now.

What `DescribeRow` writes:

| Row | Sentence |
|---|---|
| running | `<glyph> <Verb> <target>` (`Scanning ~/w/app`) |
| queued | `<glyph> Queued: <kind> <target>` |
| remembered failure | `<glyph> <target>: last <kind> failed — <cause>` (newest failure wins) |
| none of these | the view's `ObservedText`, with a glyph unless the state is `Idle` (a resting row has no glyph) |

Rules behind the table:

- **The footer glyph is fixed** (`indicator.Glyph(state, true)`): it never
  blinks, only the column does.
- **The cause is put on one line** (`oneLine`: control characters become
  spaces, whitespace collapses) and a leading `<kind> failed` is removed with
  its separator (`—`, `--`, `-`, `:`), because the sentence already says it
  (`causeSuffix`). The same cause text serves `FailureEvent`.
- **Failures a winner hides** are listed by a suffix, in `jobs.Kinds()` order:
  ` · earlier failure: scan (see :jobs)` or ` · earlier failures: scan, clone
  (see :jobs)`. On a running or queued row they follow the sentence; on a
  failed row they list the older failures.
- A path target has its home folded (`~`); an image reference or forge path is
  kept verbatim.

**`TestNoViewSpellsARowActivityPhrase`** (`footer_guard_test.go`) scans every
UI source file outside `internal/ui/components` and fails on a string literal
that matches one of these patterns: `earlier failure`, `Queued: `, `: last `,
and `(?i)\blast [a-z]+ failed\b`. A view calls `DescribeRow` or `FailureEvent`
instead. Exceptions go in `declaredRowPhraseExceptions` (file suffix → reason);
it is empty. Deliberately not banned: the bare verbs (`Scanning`, `Cloning`),
used as progress titles, and `Failed to <verb>`, which explorer, templates and
`app/context.go` still write themselves. Their migration stories move them to
`FailureEvent`, and the ban can be added then.

## `PostFooterMsg` — the router's one line

`FooterMessage` belongs to a view, and a view calls `Info`, `Warn` or `Error` on
its own. The router has no footer at all: `RenderFooter` is the active view's.
`components.PostFooter` is the one door for the one thing the router has to say
— that the MCP server did not start (§3.61) — and it works because every view
already offers unhandled messages to `Handle`, so the broadcast lands wherever
the user is without any view knowing what it is about.

It carries its own ID, unlike a message a view sets: `PostFooter` builds the
message **and** the timer that expires it, so Rule 128's "a message set without
its timer never clears" holds. `Handle` adopts the ID whole rather than
re-numbering it, or the timer already in flight would name a message that no
longer exists.

A view must not use it. Going through here would be a second way to do what a
field already does, and the message would be adopted by whatever view is active
rather than by the one that meant it.
