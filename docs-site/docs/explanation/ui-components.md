# UI Components

DevDesk's terminal UI is built around two shared components used throughout the application: a single footer-message system, and a single table implementation. This page explains why they're centralized and the reasoning behind their key design decisions — not a full API reference.

## The footer: one implementation, three severity levels

Every view shows one line of state below its main content, and that line has exactly one implementation shared across the whole application. Previously, each of roughly eight views implemented its own version — its own fields, its own expiry logic, its own styling — which is exactly how a subtle inconsistency crept in everywhere at once: none of the eight independently-written implementations happened to center its error message, even though success and info messages were centered, so errors ended up left-aligned across the entire app purely by omission. Centralizing removes that class of drift entirely, since there's only one place left for it to occur.

```go
type Model struct { footer sharedcomponents.FooterMessage }

// Only set from Update() — never from a Cmd. The returned Cmd is the
// expiry timer; a message set without it would never clear.
return m, m.footer.Error("Failed to load — check logs")
return m, m.footer.Warn("Scan already in progress")
return m, m.footer.Info("Image pulled: " + name)

m.footer.Handle(msg)                        // consumes its own expiry message
m.footer.SetSpinnerFrame(m.spinner.View())  // pushed from the spinner's tick

// Read-only, from View(): always renders one full-width, centered line
// — even when empty, since the layout budgets space for it either way.
m.footer.View(width, m.status())
```

### Three levels, defined by what happened — not by tone

The three severities are distinguished by what actually occurred, not by how urgent it feels:

| Level | Meaning | Color |
|---|---|---|
| Error | An operation failed, or was refused by the underlying system | The same red used for CRITICAL severity findings |
| Warning | The requested action can't be honored right now, but nothing failed — a precondition wasn't met, something is already running, or the action doesn't apply here | The same orange used for MEDIUM severity findings |
| Info | A neutral fact, or a successful outcome | The application's ordinary text color |

Reusing the severity palette (rather than defining separate error/warning colors) is a semantic choice, aliased centrally so a future theme could tell them apart if it wanted to — today the two happen to render identically. Info deliberately does not reuse the app's "highlighted" accent color, because that accent is a yellow close enough to the warning orange that the two were easy to confuse at a glance; info now uses plain text color instead, and is not bold, while the other two levels are.

!!! note
    Green does not appear in footer messages anywhere in the app — it's reserved for status icons elsewhere in the UI, to keep it meaningful there.

### Each message's expiry is independently identified

Every footer message carries an ID, and its expiry timer only clears the specific message it belongs to. This matters because, without an ID, a message set just before an earlier message's timer fires would be wiped out by that unrelated, stale timer — a defect every one of the original eight per-view implementations happened to share.

### `Status` is a different thing from a message, and has no timer

A `Status` — an in-progress action, a hint, a loading table — is derived fresh every frame from current state rather than posted as a one-off event with an expiry. This distinction matters concretely: a multi-step batch operation can run far longer than a message's fixed display duration, so if progress were expressed as a timed message, it would expire and disappear while the batch was still only partway done. Status has no such problem, because it's recomputed continuously rather than posted once.

Precedence when rendering the footer is **error → warning → info → status**: an unread failure always takes priority over showing that something is still in progress.

### A loading table stays a table

When a table-backed view is loading data, the *table itself* stays on screen — with its header and columns — and the loading indication is a `Status` in the footer, with a spinner. A body that swapped itself out for a spinner while loading would visibly lose its header and columns for the duration of every refresh, then regain them — a layout jump on every single reload. As a direct consequence, an "empty" message like "No images found" is only shown once loading has actually finished; otherwise the table would be announcing the absence of results it hasn't even finished looking for yet.

## Tables: one shared mechanism for width, sorting, and filtering

A shared table component (`datatable`) implements the mechanics common to every table in the application — column-width solving, sorting and sort arrows, filter matching, cursor clamping, and mapping a screen row back to the underlying object. Look-and-feel (colors, borders) stays a separate concern, layered on top.

```go
datatable.New(datatable.Config[T]{
    Columns: []datatable.Column[T]{{
        Title:    "Name",
        Sizing:   datatable.SizingContent, // or SizingFixed — no default
        MinWidth: 20,    // a floor; the exact width for a Fixed column
        MaxWidth: 48,    // 0 = no ceiling; only meaningful for SizingContent
        Optional: false, // true = this column may be dropped before non-optional ones
        Flex:     1,     // share of leftover space once every column has its minimum
        Cell:     func(x T) string { /* plain text only */ },
        Style:    func(x T) lipgloss.Style { /* nil = the table's default colors */ },
        Less:     func(a, b T) bool { /* nil = not sortable */ },
        Search:   func(x T) string { /* nil = not searchable */ },
    }},
})
```

### Every column must declare how it sizes

`Sizing` intentionally has no default value, and a source-level check fails the build if any column omits it, naming the exact file and column. The reasoning: either plausible default would be silently wrong somewhere. Defaulting to "fixed and never dropped" means no column is ever droppable, so the very last column left standing has to absorb the whole shortfall alone on a narrow terminal. Defaulting to "content-sized and droppable" means the column that identifies each row — usually the most important one on screen — becomes droppable by default. Neither failure mode is acceptable, so the choice is forced to be explicit every time.

### How columns give up space, in order

When a table doesn't fit the available width, three things happen in sequence:

1. Content-sized columns shrink toward their declared minimum width, truncating text.
2. Whole columns marked `Optional` are removed entirely, rightmost first — this is deliberate: a column at zero width would still be rendering its padding for nothing, so once a column can't usefully show anything, it's better removed than kept empty.
3. If that's still not enough, non-optional columns start being removed too, also rightmost first.

That last step matters conceptually: it makes "not optional" a preference the layout tries to honor rather than an absolute guarantee, on the reasoning that fewer *correct* columns beat a full row of columns each rendering truncated, potentially misleading values — silently rendering `142` as `14…` with no visual indication that truncation happened would be a worse failure.

`MaxWidth` has no default and `0` means unbounded, deliberately. A single global default cap would clip columns nobody had actually reasoned about — a full IPv6 address, for instance, is 45 characters — so a cap is only set explicitly on the specific columns known to need one.

### When column widths get recalculated

Measuring column widths against actual content is not free, so it's not done on every render. The rule: widths are recomputed on the first non-empty population of data, on an explicit resize, and on a user-driven filter change — but *not* on every routine data refresh (e.g., a periodic poll replacing the row set with fresh values). The reasoning is that the component can't distinguish "the underlying data just refreshed" from "the population fundamentally changed" purely from the API call used in both cases, so views that refresh frequently (a live container list, say) don't have their columns jitter in width every tick — while views that only replace their data occasionally still get remeasured when they explicitly say the population changed.

### The component renders its own rows

`datatable` renders table rows itself rather than delegating that to the underlying widget library, and this is what makes it safe to combine plain-text cell content with independent per-cell coloring. The underlying library measures a cell's width *before* applying styling, and a naive combination of the two would count a color escape sequence's raw bytes as visible width — badly under-estimating how much room the actual text needs, and risking a cut landing in the middle of an escape sequence, which then visibly corrupts every row rendered after it. Keeping cell content itself always plain text, and applying color as a separate pass afterward, makes that class of bug structurally impossible rather than something to catch in review.

Two direct consequences of that split:

- **The selected row is not colored per-cell.** The entire selected row is handed to one dedicated "selected" style as a whole, because a color applied to only part of a row would end with a reset code that also cancels the selection highlight for the remainder of the line.
- **Every cell on an unselected row needs an explicit background color**, even cells with no other styling — since colors don't cascade down from a container to its children in the underlying styling library, and one cell containing color without an explicit background would otherwise strip the background from all its neighbors.

!!! tip "Restraint in color use"
    A color that appears on every row communicates nothing. Zero counts, empty placeholders, and "never scanned" targets are all rendered dimmed; a normal, majority-case state (e.g., a container that's simply `running`) uses the plain default text color rather than green. Color is reserved for what's actually worth noticing without reading closely.

### Marking a row as busy

Tables can mark an individual row as having an action in progress on it — a spinner replacing one cell while a stop, restart, or delete runs — via a small, separate API (`MarkBusy` / `ClearBusy` / `IsBusy` / `BusyLabels`) rather than a predicate function on each column. This is deliberate: columns are constructed once, up front, and don't close over any later view state, so a `Busy` predicate on a column definition would have nowhere to read "is this specific row currently busy" from. Separating the row's *identity* (a `Key` function) from its *busy state* (tracked internally, keyed by that identity) avoids that problem, and has a useful side effect: it lets `IsBusy` answer correctly even for a row that doesn't exist on screen yet (useful as a guard before a confirmation dialog even opens), and it survives the row list being replaced wholesale by a periodic refresh mid-action.

Which cell a spinner takes over is a per-table decision — usually the cell holding the piece of state the action in question is about to change (e.g., a registry login button, or a container's status column) — chosen because it's specifically the cell the user doesn't need to read while the action is in flight, rather than adding an entirely new column just to hold a spinner that's blank on all but one row at a time.

A "row is selected" state and a "row is busy" state can coincide, and the rule is that busy wins visually: an object mid-transition (e.g., a container that's `exited` and about to be restarted) is precisely the state that's about to change, so the busy indicator overrides the row's normal status-based coloring rather than the two competing.

### Why this consolidation mattered

Before every table in the app used this shared component, several views had independently reimplemented pieces of this logic, each with slightly different bugs:

- Some views summed declared column widths plus fixed per-column padding to check whether a row filled the viewport — which looks reasonable but is wrong the moment any column gets dropped for space, since a dropped column returns its padding to the layout too. The correct check is against the row's actual *rendered* width, not the sum of what was declared.
- Layouts that clamped each column's width independently, one at a time, rather than solving the shortfall across all columns together, could overflow the viewport on narrow terminals even though each individual column looked reasonably sized.
- Several views had copied a manual `Foreground(...)` call into their per-column styling function just to restore the theme's normal text color, because a column with no explicit style otherwise fell back to the terminal's raw default color rather than the app's theme — an easy detail to miss once, and only worth fixing centrally once.

Consolidating into one implementation means these are now each fixed exactly once, and a dedicated test enforces that every table in the codebase actually goes through the shared component rather than reintroducing a bespoke one.

### A few specific tables worth knowing about

Not every table is a simple one-object-per-row list:

- The registries tab shows **two different kinds of rows in a single table** — configured registries, and a group's discovered members once expanded. Since the shared table type is generic over a single row type, this is handled by defining one row type that can represent either case and carries enough information to tell them apart.
- A small number of views (security findings, the container/host status view, the registry browser) keep their own filtering logic in front of the shared table rather than relying solely on the table's built-in search — because in those cases, filtering determines *which rows exist at all* (e.g., which tab's findings are shown), which is a different question from the table's own text-search-over-visible-rows behavior.
