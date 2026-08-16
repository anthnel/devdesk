# A row says what it is, and what is happening to it

**§3.22.** `datatable` gains a status column and a notion of a row being busy.
Containers is the first client.

## The problem

`handleConfirmYes` fires the `Cmd` and returns without touching the model
(`oci_resources/results.go:17`, `containers/update.go:463`). Nothing on screen
says an action is running, and `internal/docker/containers.go:150` calls
`docker stop` with no `-t`, so the default ten-second grace period is ten
seconds of a frozen-looking table. That is the symptom §3.16 recorded for the
clone's `"Pulling..."` modal: indistinguishable from a hang.

**The intent was already there and lost its reader.** `containers.Model` has a
`pendingAction` field written in six places, three of them human sentences —
`"Stopping web"`, `"Restarting api"`, `"Pruning containers..."` — and asserted by
four tests. **No rendering code reads it.** The field is overloaded: it also
carries the confirm modal's routing key (`"confirm-delete"`, `"confirm-prune"`),
which `handleConfirmYes` does read. One field, two meanings, and the one nobody
reads is the one that was supposed to be visible.

## The decision behind the shape

Two things share one glyph and must not share one function:

| | What it is | Source of truth | Lifetime |
|---|---|---|---|
| **State** | `running`, `exited`, `paused` | docker, at the next refresh | as long as it is true |
| **Transition** | `stopping`, `removing` | DevDesk itself | one command |

Merging them in the **glyph** is the point — one column answers "what about this
row". Merging them in the **API** would make every table re-implement the
precedence. So `datatable` owns one rule: **busy wins over state**, and it wins
for a reason worth stating — `exited` is precisely what is about to stop being
true.

## Why it cannot be a closure

The package's stated invariant is that columns are built once, in `New`, and
close over nothing; `imageRow` exists because of it. A `Busy func(T) string` on
the `Config` would have to close over the model's map of in-flight actions —
the exact trap the row types avoid.

So identity and state are separated: the config supplies a **`Key func(T) string`**,
and the busy set is held by the table and written from `Update`.

That buys the thing that matters most: **`IsBusy(key)` answers before the row
exists**, so it is also the guard on the confirm path. Today nothing stops a
second `ctrl+d` on a slow removal from firing a second `docker rmi`; the second
fails with "No such image" and the user sees `Action failed` on a removal that
worked.

Keying on identity rather than a flag on the row also survives the periodic
refresh, which replaces the items mid-action.

## API

```go
datatable.Config[T]{
    Key:          func(c docker.Container) string { return c.ID }, // nil ⇒ off
    StatusColumn: 0,                                               // cell the spinner replaces
}

m.table.MarkBusy(id, "Stopping web")   // from Update, Rule 110
m.table.ClearBusy(id)                  // on *every* outcome, error included
m.table.IsBusy(id)                     // the guard
m.table.BusyLabels()                   // sorted, for the footer line
m.table.AdvanceSpinner()               // from the view's existing spinner tick
```

`datatable` cannot hold a `spinner.Model` — a spinner needs a `Cmd`, so a tick,
so the view (Rule 110). **The frame lives in the table, the tick stays in the
view.** A moving spinner rather than a static gear, deliberately: an immobile
glyph cannot tell "working" from "stuck", which is the question being answered.

## Rendering

- The cell at `StatusColumn` renders the spinner frame instead of the column's
  own `Cell`, **whether or not the row is selected** — the glyph is the primary
  signal and has to work everywhere.
- Off the selected row, the busy row's cells go `DimStyle`, the status cell
  `ColorHighlight`. This follows the existing rule that `Style` is not consulted
  for the selected row.
- **On** the selected row the highlight itself changes, because a colour inside
  it would close with a reset and end the highlight mid-row (`render.go`). So
  `theme.TableStylesForState("busy")` — `ColorHighlight` background — overrides
  whatever `SelectedStyles` returned. That is "busy wins over state" again, and
  the colour is not arbitrary: containers already paints docker's own
  transitional states (`created`, `restarting`) in `ColorHighlight`.

## Cost and limits

- The column costs ~3 cells on every table that opts in, so it is **opt-in** and
  **must not be sortable**: `askFor` reserves `width(Title)+2` for a sortable
  column, which the Secrets column already calls expensive for a glyph. Title
  empty or one character.
- A busy entry cleared only on success leaves a row spinning for good — and
  worse, hides the real state. Cleared on **every** outcome, with a test on the
  error path. Same family as §3.11's "a reload keeps an in-flight scan's marker".

## Deliberately out of scope

- **Scans.** Their spinner is in the Scanned column and they do not block the
  object the same way. Folding them in is a separate question.
- **`prune`.** It acts on no row, so marking every row would say something
  false. It belongs to a footer line rendered from the operation's state, like
  `syncStatusLine` (§3.17) — not `footerInfo`, whose three-second timer would
  expire mid-operation.

## Steps

1. `theme.TableStylesForState("busy")`.
2. `datatable`: `Key`, `StatusColumn`, the busy set, `MarkBusy`/`ClearBusy`/
   `IsBusy`/`BusyLabels`/`AdvanceSpinner`, the cell override and the two style
   paths. Tests: the override lands on the right column, the selected row keeps
   one unbroken highlight, `Key: nil` changes nothing, a busy row survives
   `SetItems`.
3. **containers**: a status column at 0 carrying `stateIcon`, removed from the
   Image cell; `Search` keeps matching the state. `MarkBusy` in the five action
   handlers, `ClearBusy` in `handleContainerAction`, the guard on the confirm
   path, and `BusyLabels()` rendered in the footer — which is what `pendingAction`
   was written for and never got.
4. Split `pendingAction` back into what it actually is: the confirm routing key
   only.
