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
containers action line comes from `BusyLabels()`, which changes with no event to
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
`.` past it on every open is not a default. A direction with no sortable column
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
    Key:          func(c docker.Container) string { return c.ID }, // nil ⇒ facility off
    StatusColumn: 0,                                               // the cell the spinner replaces
}

m.table.MarkBusy(id, "Stopping web")  // from Update, never a Cmd (Rule 110)
m.table.ClearBusy(id)                 // on *every* outcome, failures included
m.table.IsBusy(id)                    // the guard
m.table.BusyLabels()                  // sorted, for the footer
m.table.AdvanceSpinner()              // from the view's own spinner tick
```

**It cannot be a `Busy func(T) bool` on the config.** Columns are built once in
`New` and close over nothing — that is why `imageRow` exists — so a predicate
there would have to close over the view's map of in-flight actions. Separating
the identity (`Key`) from the state is what avoids it, and it buys the thing
that matters most: **`IsBusy` answers for an object whose row does not exist
yet**, so it is also the guard on a confirm path. It is also what survives the
periodic refresh, which replaces the items mid-action.

The spinner's **frame lives in the table, its tick stays in the view**: a
spinner needs a `Cmd`, and this package returns none.

Rendering, and each part is a decision:

- The status cell shows the spinner **on the selected row too** — a signal that
  vanishes under the cursor is lost exactly when it is being looked at.
- Off the cursor the row goes `DimStyle`, the spinner `ColorHighlight`, and the
  column's own `Style` is dropped.
- **On** the cursor the highlight itself changes
  (`theme.TableStylesForState("busy")`), overriding whatever `SelectedStyles`
  returned — a colour inside the joined row would close with a reset and end the
  highlight mid-row (`render.go`).
- The override is applied **when the row is drawn**, not when it is built, so
  the stored cells do not go stale as the frame advances. Tests assert on
  `View()`, not on `Table().Rows()`.

The status column must **not be sortable**: `askFor` reserves `width(Title)+2`
for a column carrying a `Less`, which is expensive for a glyph.

**Which cell the spinner spends is a decision per table**, and it is the same
decision every time: the one the user does not need while the action runs. Only
`containers` gained a column, because only it has a state worth a column of its
own; everywhere else the spinner rides an existing cell, which is §3.16's
argument about the clone checkbox — a column costs cells on every screen to say
nothing on all but one row.

| Table | Key | Cell the spinner takes | Actions |
|---|---|---|---|
| `containers` | container ID | a status column of its own (the state icon left the Image cell) | stop, restart, pause/resume, remove |
| `oci` images | image ID | `ID` — it neither sorts nor searches | remove |
| `oci` networks | network ID | `ID` | remove |
| `oci` volumes | name | `Driver` — a volume has no ID, so its **name** is the one cell that cannot go | remove |
| `oci` registries | URL | `Logged` — precisely what the operation is about to change | login, logout |
| `netdiag` ports | **PID** | `State` | kill |

Two of those keys are worth the note. The ports table keys on the **PID, not the
socket**, so every row of a process spins at once — which is what happens, the
kill takes them all. The registries table keys on the **URL**, which is the one
place D40's rule does not apply, and it does not apply because the *operation*
is host-scoped: one `docker login` really does change the answer for every entry
on that host.

`oci_resources` gathers `BusyLabels()` from **all four tabs**, not the active
one: an action started on Images goes on running after `tab`, and a spinner that
stopped turning because the user looked elsewhere would read as a hang on the
way back. The spinner tick has to keep being scheduled while anything is busy —
that is what `advanceBusySpinners()` reports.

Deliberately outside this:

- **Scans** — their spinner is in the Scanned column and they do not block the
  object the same way.
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
