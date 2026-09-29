# Files — the browser, the picker, and short paths

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

`:files` (alias `:fs`) browses the whole filesystem, and a form's path field
borrows the same view as a path picker with `enter` (§3.95). Three packages:

| Package | Holds |
|---|---|
| `internal/fsbrowse` | the filesystem half: `List`, `Create`, `Remove`, `ValidName`, `IsHidden`, `LeadsToDir`, `ExpandHome`, `NearestExisting` |
| `internal/ui/filebrowser` | the view, and the picker contract (`PickRequestMsg`, `PathPickedMsg`, `PickCancelledMsg`) |
| `internal/ui/theme/path.go` | `FoldHome` and `ShortPath`, how a path is shown when it does not fit |

## `internal/fsbrowse` — what a row is

It holds no git knowledge. `ws` enriches every directory with repository state
through a recursive walk, which is what a workspace listing is for and what a
listing of `/usr/lib` must not pay. What the two views share is what a row
**is**, and that is what moved here:

- **`IsHidden`** is the single rule for dot entries. `ws` calls it from its
  listing, its nested-repo walk and its fuzzy walk. One rule is the point: the
  rows `ws` shows are what `S` and `F` act on.
- **`LeadsToDir`** follows a symlink or a Windows junction (§1.3 D59). `ws`
  keeps its local `leadsToDir`/`isHidden` names as one-line delegations, so the
  walks there read unchanged.
- **`ValidName`** is one path segment: not empty, not `.`/`..`, no separator,
  no NUL. A separator is refused rather than interpreted, because `a/b` creates
  a directory the screen never showed being made, and `../x` writes outside the
  directory the screen names. `ws`'s `N` and `M` go through it too (§1.1 D79).
- **`Create`** never overwrites: `Mkdir` (not `MkdirAll`) and `O_EXCL`. A
  missing parent means the listing is stale, and that is better said than
  papered over.
- **`Remove`** refuses a filesystem root and the home directory
  (`Protected`). The view greys `D` from the same function (Rule 130), so the
  refusal can never be reached from the keyboard, only from a race.

## The view

Built like `ws`, minus the git: an icon column (Rule 125, the `directory` and
`file` roles), Name, Size (a dim `-` for a directory: its size is its inode,
not its content), and Modified. The order is `fsbrowse.List`'s — directories
first, then by name ignoring case — and there is no sort key.

**`requested` versus `currentPath`.** A navigation asks for a directory
(`requested`), and `currentPath` moves only when that listing arrives. A
listing for any other directory is dropped: two loads in flight, and this one
lost. A directory that cannot be read therefore leaves the screen on the one
that could, with the reason in the footer. The first listing is the one
exception: there is nothing to keep, so it leaves an empty table (Rule 139)
under the error.

| Key | Does |
|---|---|
| `→` | enters the selected directory |
| `←` | the parent, cursor on the directory just left; nothing at a root |
| `esc` | as `←` — in a picker, cancels |
| `enter` | opens a file in the viewer — in a picker, chooses |
| `N` | a form in the viewport (Rule 112): Type (`directory`/`file`, Rule 132) and Name |
| `D` | a confirmation defaulting to No, which counts what a directory holds, then a `jobs.KindDelete` run |
| `Y` | the absolute path to the clipboard |

`D` counts entries through a Cmd (`DeletePreparedMsg`) rather than in
`Update`: it is one `ReadDir`, but on a network mount one `ReadDir` can hang,
and `Update` must not. The delete is a registry run like `ws`'s, so
`EntryDeletedMsg` is a `jobs.Reporter` with its case in `app.go`.

Hidden entries follow `app.show_hidden_files`, as in `ws`. No lowercase key
is bound, so there is no `keymap.localToggles` surface.

## The picker

```go
filebrowser.PickRequestMsg{Kind: PickDir | PickFile, Start, Prompt, Tag}  // form → router
filebrowser.PathPickedMsg{Tag, Path}                                     // router → form
filebrowser.PickCancelledMsg{Tag}                                        // router → form
```

`internal/app/picker.go` handles the request: it remembers `a.currentView` in
`pickerReturnView`, installs `filebrowser.NewPicker` as `views[ViewFiles]`, and
on either answer deletes that entry and hands the message to the borrower
through `returnToOrigin`.

**Why this is not `selection.go`.** That file translates each borrower's
request into its own vocabulary, because the explorer borrows two *different*
views to answer two *different* questions. Here the question is always "which
path", so one pair of messages serves every borrower, and `Tag` says which of
its fields the answer is for. The return slot is separate for a concrete
reason: `:templates` can itself be lent to the explorer, and a picker opened
from its form must not overwrite where that borrow returns to.

**Where it opens.** `NearestExisting(ExpandHome(Start))`. On a file, it opens
the file's directory with the cursor on the file. When nothing along the path
exists, or the field is empty, it opens `$HOME`.

**What `enter` accepts.** A row of the requested kind, and nothing else: the
other kind is greyed, and pressing it says why (Rule 130). A directory picker
puts a `.` row first — the directory being browsed — so an empty directory
can be chosen, and so can the one the picker opened on without entering
anything. `N` stays available: create the destination, then choose it.

**`esc` cancels, always.** In `:files` it goes up. In a picker, whose scope is
the whole filesystem, there is no "starting level" whose meaning survives `←`,
so `esc` cancels from anywhere, and `←` is the only way up.

`InEditMode` is true for a picker: its `esc` answers the borrower, and a bare
`q` or `:` must not leave the view with the borrower still waiting. `ctrl+p`
still works. A command typed there abandons the picker, and
`resetSelectionModeFor(ViewFiles)` drops it, so the next `:files` is the
browser and not the orphaned picker.

### The borrowers

| Form | Field | Kind | Tag |
|---|---|---|---|
| `:cfg` app | Workspaces dir | dir | `/Workspaces dir` |
| `:cfg` app | Log file | file | `/Log file` |
| `:cfg` tools | each scanner's Binary | file | `<tool>/Binary` |
| `:cfg` tools | Config (rules file), when the tool has one | file | `<tool>/Config` |
| `:templates` form | Directory, local source only | dir | `template-path` |

**The marker.** A browsable field carries `theme.IconBrowse` after its label,
exactly where a closed-list field carries `IconSelect` (Rule 132): the label
says which key does something special on it. It stays a text field, because
typing a known path is still the fastest way to enter it. `enter` is what opens
the picker, and it was free on these fields: it did nothing in `:cfg`, and in
the templates form it only moved to the next field. A first version put a
*Browse…* button row under every path field instead. It was dropped the same
day because the rows cluttered the forms.

**`:cfg`.** `pathField(label, ref, hint, kind)` is a text field with
`path: true` and the kind to pick. `fieldHead` adds the marker, so the chevron
column measures it like a select icon. The answer is matched by `pickTag`
(tool + label, since every scanner has a "Binary"). `enter` is greyed on every
other field. The picker opens on what the input holds, even if it has not been
committed yet: that is the path on screen. The answer goes through the field's
`Apply` then `persist`, the same path a typed value takes, and the input is
rebound so the focused field shows the answer.

It is written **absolute, not folded to `~`**. `config.ExpandPaths` runs once,
at load, and the session reads what is in memory. A `~/bin/trivy` written
here would reach the scanner unexpanded until the next start. The form
shortens it on screen anyway.

**`:templates`.** The directory is browsable (`browsable()`) for the local
kind only: a git subdirectory or an OCI repository is not a path on this
machine, so neither carries the marker and `enter` there still moves on.
`fetch.go` expands `~` before handing the directory to `git archive`, so a
typed `~/templates/x` works too.

## Short paths — `theme.ShortPath(path, maxWidth)`

What gives way, in order of what matters least:

1. `$HOME` → `~`, always (`FoldHome`, which used to be `security`'s private
   `shortenHome`);
2. intermediate segments reduced to their first rune, left to right, **stopping
   as soon as the path fits**. A dot segment keeps its dot: `.config` becomes
   `.c`, because a lone `c` names a different directory;
3. only then `TruncateTailWidth` — the `...` prefix — when even the fully
   reduced form is too wide.

The last segment is never reduced: it is the name the field is about. Widths
are measured with `runewidth`, not `len`.

```
~/projects/workspace-entire/anthnel/devdesk   fits
~/p/workspace-entire/anthnel/devdesk          one reduced
~/p/w/a/devdesk                               all reduced
...a/devdesk                                  head cut
```

Where it is used: a `:cfg` path field at rest (the value column's width), the
templates form's local directory at rest (60 cells), the `:files` header's
Path (48), and the `N` form's title. A field being edited always shows the
whole path. The dashboard's `truncatePath` predates this and still keeps only
the tail; converging it is left for later.
