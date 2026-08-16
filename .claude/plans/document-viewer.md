# Plan: A viewer for documents — text, JSON, XML and logs

**Source**: free-form request (2026-08-16), revised after the logs-reuse note
**Backlog entry to write**: `docs/backlog.md` §3.25
**Complexity**: Large (new domain package + new view + three producers + one dependency),
but **net-negative in `internal/ui/containers`**, which loses its whole logs pane.

## Summary

A new view — `viewer` — displays a document read-only. A JSON or an XML document
opens on a **navigable tree**; either can be switched to its **own text**, with
syntax highlighting on or off. A **log** document opens as text with a verbosity
filter.

Three producers, one destination:

| Producer | Key | Document |
|---|---|---|
| `workspaces` | `enter` on a file | detected from the extension, then the content |
| `containers` | `i` | `docker inspect` — JSON |
| `containers` | `l` | `docker logs` — log |

That is the shape the security view already has (opened from workspaces *and*
from the OCI images list), and it is built the same way: the producer hands the
router one message, the router installs the view and records where `esc` returns.

## What this deletes

The containers view already owns a text pane — `stateLogs`, a `viewport`, soft
wrap, ANSI stripping, CR normalisation, scroll keys, reload, an external pager,
follow and a timestamps toggle. **None of it is containers-specific except the
last three**, and those are properties of *where the text came from*, not of the
pane. So the pane moves into the viewer and containers loses:

`viewState` · `stateLogs` · `logsViewport` · `logsContainerID` ·
`logsContainerName` · `logsLoading` · `logsRawContent` · `logsWrapEnabled` ·
`logsTimestamps` · `handleLogsKeyMsg` · `refreshLogsViewport` · `wrapLines` ·
`toggleLogsWrap` · `toggleLogsTimestamps` · `openExternalPager` ·
`followContainerLogs` · `handleContainerLogsLoaded` · `ContainerLogsLoadedMsg` ·
`inspectSelectedContainer`'s pager path · the `stateLogs` branches in
`InEditMode`, `GetFooterHeight`, `RenderFooter`, `GetShortcuts`, `View` and
`resize`.

`wrapLines` and the ANSI/CR normalisation are **moved**, not rewritten — they are
correct, and the reasons in their comments (systemd colours the output, progress
bars overwrite with `\r`) apply to any text this app displays, not just to logs.

`PagerExitMsg` stays: the shell path still uses it. Only its `stateLogs` branch
goes.

---

## Decisions taken

### Two display axes, not four names — **confirmed**

The request named *tree*, *raw*, *plain text* and a highlight toggle. Four names,
two independent facts:

| Axis | Values | Key |
|---|---|---|
| Display | `tree` or `text` | `f` |
| Highlight | on / off | `c` |

"Plain text" is `text` with colour off. A third display that additionally
discarded the detected format was put to the user and declined: it would make
`plain + colour on` and `text + colour off` two names for one screen. A document
with no structure (`.md`, `.go`, a log) is always `text`, and `f` is hidden for it
(Rule 130).

### chroma as a lexer, never as a formatter — **confirmed**

`github.com/alecthomas/chroma/v2` (v2.27.0, pure Go, no cgo), chosen over ~250
hand-written lines of escape and CDATA handling, and the only route to Go, YAML
or Dockerfile later. It embeds every lexer, so the binary grows by a few MB —
the accepted cost, to be measured and recorded in §3.25.

`chroma/v2/formatters` is **never imported**. It writes its own ANSI colour and
reset sequences, and a reset inside a line strips the app background for
everything after it (Rule 115). Only `lexers.Get(...).Tokenise` is used; the
classes are mapped to theme styles by the UI package.

### A source, not a byte slice

The logs pane can reload, follow and re-fetch with timestamps. A viewer holding
only `[]byte` cannot do any of that, and neither could it reload a file the user
has just edited. So a document carries **where it came from**:

```go
type Source interface {
    Name() string                 // what the header shows
    Kind() Kind                   // declared by the producer, or KindAuto
    Load() ([]byte, error)
}
```

and three optional capabilities the view probes by type assertion — the router's
own idiom for `FooterView` and `FramelessView`:

| Interface | Method | Unlocks |
|---|---|---|
| `Timestamped` | `SetTimestamps(bool)` | `t` |
| `Followable` | `FollowCmd() *exec.Cmd` | `ctrl+f` |
| `Pageable` | `PagerCmd() *exec.Cmd` | `e` |

`ctrl+r` (reload) needs no interface — every source loads.

CLAUDE.md's warning about `HeaderView` applies: an interface probed silently is
a good behaviour with a bad failure mode. Here each is a **single method**, so
there is no half-satisfied case to fall into — which is precisely why they are
three interfaces rather than one with three methods.

Implementations: `FileSource` (workspaces), `DockerInspectSource` and
`DockerLogsSource` (containers). Only the last one implements all three
optional interfaces.

### The log format is declared, never sniffed

`KindLog` comes from the producer or from a `.log` extension. There is no content
sniff for it — "this looks like a log" is not a decidable question, and the
registry `provider` field is the precedent: *declared, never sniffed from the
URL*, so registration order decides nothing.

JSON and XML keep their content sniff, but only as a **fallback for a file with
no useful extension**. The extension wins when there is one.

### A line with no level is never hidden by accident

This is the decision the whole filter rests on. A stack trace is a dozen lines
with no level on them; a filter set to *≥ warn* that drops them has destroyed
exactly what the user opened the logs to read.

So an unlevelled line **inherits the level of the line above it**, and a line
before any levelled line is `LevelUnknown`, which every filter shows. The
trade-off, stated so it is not discovered later: a genuinely unrelated
unlevelled line following an `INFO` is filtered out with it. That is the
behaviour `lnav` and `stern` settled on, and it is the right side of the trade —
losing a stray line beats losing a stack trace.

Levels are recognised in three shapes, in this order, because a Docker log is
just as likely to be any of them:

1. **JSON lines** — `{"level":"error",…}`, `"severity"`, `"lvl"`
2. **logfmt** — `level=error`, `lvl=warn`
3. **bare text** — a token in the first ~48 characters: `FATAL` `PANIC` `ERROR`
   `ERRO` `ERR` `WARN` `WARNING` `WRN` `INFO` `INF` `DEBUG` `DBG` `TRACE`

### Verbosity is one cycling minimum, not four toggles

`v` cycles `all → debug → info → warn → error`, shown as one `FilterBar` token
(`≥ warn`). One key rather than four, it is what "niveau de verbosité" means, and
it is **monotone** — which log levels are. Nobody wants *warn but not error*.

`components.FilterBar` is used, not a local filter (Rule 136), with
`NewFilterBarWithTokens` exactly as the netdiag ports tab does. That also brings
`/` text search to the text pane for free — which closes the gap the first draft
of this plan left open on a 400-line `docker inspect`.

Four independent toggles were put to the user and declined. Two reasons beyond
the monotonicity: `e` is already the external pager on the one document kind
that has levels, and four tokens fill the filter bar to say what one says.
`FilterBar` supports several tokens, so adding them later is one handler — which
is the "easy to add filters" the request asked for.

---

## Patterns to mirror

| Category | Source | Pattern |
|---|---|---|
| Shared destination view | `internal/app/scan_details.go:90` `openSecurityView` | install a built view, record `OriginView`, `tea.Batch(Init, requestResize)` |
| Return to origin | `internal/app/app.go:349` `security.BackToOriginMsg` | view emits, router calls `switchView(msg.Origin)` |
| Text pane | `internal/ui/containers/update.go:423` `refreshLogsViewport`, `:433` `wrapLines`, `:411` `ansi.Strip` | **moved wholesale**, comments included |
| Filter tokens | `internal/ui/netdiag/ports_model.go` `NewFilterBarWithTokens` | tokens in the footer bar, `/` for text (Rule 136) |
| Optional interface probed | `internal/app/view.go:94` `FramelessView` | type assertion with a safe default |
| Producer does its own I/O | `internal/ui/containers/commands.go:88` `fetchContainerLogs` | `Cmd` fetches, returns a message (Rule 110) |
| Row type carrying decoration | `internal/ui/gitlab/explorer/row.go` `explorerRow` | columns built once in `New`, closing over nothing |
| Table of rows | `internal/ui/datatable` | the only way to draw rows; `Cell` plain, colour via `Style` (Rule 122) |
| Footer messages | Rule 128, `internal/ui/workspaces/commands.go:22` | set + 3s timer; never `return m, nil` after setting one |
| Semantic colour aliases | `internal/ui/theme/colors.go:61` severity block | aliases of existing colours; no hex outside the palette (Rule 119) |

No precedent exists for a **tree** or for **syntax highlighting**; both are new
and are stated as such rather than dressed as conventions.

---

## Phase 0 — `internal/viewer`, the domain package (no TUI)

| File | Contents |
|---|---|
| `document.go` | `Kind` (`KindAuto`, `KindPlain`, `KindJSON`, `KindXML`, `KindLog`), `Document{Name, Kind, Raw, Lines, Root, ParseErr}`, `Open(name string, kind Kind, data []byte) Document` |
| `source.go` | `Source`, `Timestamped`, `Followable`, `Pageable`, `FileSource` |
| `detect.go` | `DetectKind(name, data)` — extension, then a content sniff for JSON/XML only; `IsBinary(data)` |
| `read.go` | the 5 MiB cap and the binary refusal, in one place so every producer gets the same answer |
| `normalize.go` | `ansi.Strip`, `\r\n` → `\n`, stray `\r` dropped — **moved from containers**, unconditional for every document |
| `wrap.go` | `WrapLines(content, width)` — **moved from containers** |
| `json.go` | `parseJSON` via `json.Decoder.Token()` — **ordered**, which `map[string]any` is not |
| `xml.go` | `parseXML` via `xml.Decoder.Token()` — elements, attributes, text, comments |
| `node.go` | `Node{ID int, Key, Value string, Kind NodeKind, Children []*Node}` |
| `log.go` | `LogLine{Level, Text}`, `ParseLog(text) []LogLine`, `Level` and its ordering |
| `highlight.go` | `Tokenize(kind, text) []Token{Class TokenClass, Text string}` — chroma, lexer only |

Decisions worth keeping:

- **A node's identity is an `ID` assigned in parse order**, not a JSONPath. The
  tree is parsed once per open, so an int is stable and the expansion map is
  `map[int]bool` in the view. A path string is a second representation of the
  same thing with more ways to disagree.
- **An XML attribute is a node of its own**, a child of its element — the only
  representation that answers for a document whose information is in its
  attributes, which is most of them.
- **A malformed document is not an error, it is a text document.** `Open`
  records `ParseErr`, falls back to `KindPlain`, and the view says so in the
  footer (`Malformed JSON — showing text`). Refusing to open it would hide
  precisely the content needed to fix it.
- **`ReadFile` refuses two things and names both**: over `MaxSize` (5 MiB) and
  binary (a NUL byte in the first 8 KiB). A refusal is a footer error, never a
  screen. The cap is a *file* concern — a source that fetches (`docker logs
  --tail 500`) is bounded by its own command.
- The invariant everything rests on — **concatenating every token's text
  reproduces the input exactly** — gets a test of its own.

## Phase 1 — theme colours

`internal/ui/theme/colors.go` gains a syntax block of **semantic aliases**, in
the section that references the palette rather than declaring hex (Rule 119):

```go
ColorSyntaxKey     = ColorSecondary
ColorSyntaxString  = ColorOK
ColorSyntaxNumber  = ColorHighlight
ColorSyntaxLiteral = ColorPrimary   // true / false / null
ColorSyntaxPunct   = ColorDim
ColorSyntaxTag     = ColorSecondary
ColorSyntaxAttr    = ColorPrimary
ColorSyntaxComment = ColorDim
```

No theme file changes: the six files in `themes/` declare no syntax keys, so
every theme gets a coherent palette for free and a future theme can override
them by adding keys — the escape hatch the severity colours already have.

**Log levels get no new colours.** `StatusErrorStyle`, `StatusWarningStyle`,
`DimStyle` and the default text colour already mean exactly error / warn / debug
/ info. Adding `ColorLogError = ColorError` would be a third name for one thing.

The `TokenClass → lipgloss.Style` mapping lives in
`internal/ui/viewer/syntax.go`, which imports both `internal/viewer` and
`theme`, so nothing new is coupled.

## Phase 2 — `internal/ui/viewer`, the view

| File | Contents |
|---|---|
| `model.go` | `Model`, `New(cfg)`, `NewWithSource(cfg, src)` |
| `messages.go` | `OpenRequestMsg{Source}`, `LoadedMsg`, `BackToOriginMsg{Origin}` |
| `tree.go` | `treeRow`, two columns, `rebuildRows()` |
| `text.go` | the text pane: highlight once, wrap on demand, level filter |
| `syntax.go` | `TokenClass` → style |
| `update.go` | keys, resize, the source capability probes |
| `view.go` | `View`, `HeaderView` (all four), `FooterView`, `help.Provider`, `InEditMode` |

### The tree is a `datatable`, the text pane is a `viewport`

CLAUDE.md: *every table in the application is a `datatable`; there is no second
way to build one*. The tree qualifies — two columns, **Key** and **Value**, with
the indentation and the expand chevron as plain text inside the Key cell
(Rule 122). It buys the width solver (Rule 116), the cursor clamp and the
selected-row rendering.

It works here for a reason worth stating: **a tree cell holds exactly one syntax
class**, so one `Style` per cell is enough. `datatable` cannot express several
colours inside one cell and never has to.

Neither column sets `Less` or `Search`. Sorting a JSON tree destroys the order
the file gave it — which is why `json.Decoder.Token()` is used at all — and a
text filter hides parents and orphans their children. `.` and `/` are therefore
unbound in the tree, and the shortcut list says so by omission (Rule 138).

The text pane is a `viewport.Model`, the containers logs pane moved. Rule 122
does not apply — it is not a table — so a line may carry many colours. **Rule 115
does**: every token style sets `Background(theme.ColorBackground)` and every line
is padded with `theme.PadWithBg`, or one reset mid-line shows the terminal's own
background for the rest of it.

### Highlighting and filtering are computed once

Tokenising 5 MiB per frame is not viable, and neither is re-filtering 500 log
lines. The rendered lines are rebuilt when the document loads, when `c`, `w` or
`v` changes, and when the width changes with wrap on — nowhere else. `View()`
stays read-only (Rule 110).

### Keys

| Key | Effect | Shown when |
|---|---|---|
| `↑↓` `g` `G` `pgup/pgdn` | move / scroll | always (omitted from `GetShortcuts`, Rule 138) |
| `←` / `→` | collapse / expand | tree display |
| `f` | switch display: tree ↔ text | structured document |
| `c` | syntax highlighting on / off | always |
| `w` | soft wrap | text display |
| `v` | verbosity: `all · debug · info · warn · error` | `KindLog` |
| `/` | search the text | text display |
| `t` | timestamps | source is `Timestamped` |
| `ctrl+r` | reload | always |
| `ctrl+f` | follow live output | source is `Followable` |
| `e` | open in the system pager | source is `Pageable` |
| `esc` | back to the origin view | always |

Three collisions were resolved rather than accepted:

- **`h`/`l` stay unbound.** Rule 111 reserves them as `←`/`→` aliases, which is
  exactly the tree's collapse and expand. That is why the highlight toggle is
  `c` (coloration), not `h`.
- **Follow moved from `f` to `ctrl+f`.** `f` is the display toggle; `F` would
  read as a variant of it. `ctrl+r` and `ctrl+f` now sit together as *reload
  once* and *reload continuously*, which is what they are.
- **`q` no longer closes.** The logs pane accepted `esc` or `q`; `q` is the
  application-level quit key (Rule 111) and a view that swallows it is the
  exception, not the rule. `esc` alone.

### Header

`GetTitle()` → `󱀫 Viewer · <name>`. `GetHeaderInfo` carries `Context`, `Format`
(`json · tree`, `json · text`, `log · ≥ warn`) and `Lines` or `Nodes`. All four
`HeaderView` methods are implemented — CLAUDE.md notes that supplying two of them
satisfies nothing and renders an empty title in silence.

An empty viewer renders `Nothing open` and advertises `esc`. It exists so
`createView` can build one for the router's contract test.

## Phase 3 — router wiring

- `command.ViewViewer ViewType = "viewer"`, **deliberately absent from
  `viewNames`**: `:viewer` on an empty viewer is a screen saying nothing, and the
  configuration view's `default_view` would offer it as a landing view — the
  defect `ViewNames()` was split from `FullNames()` to fix.
- `internal/command/parser.go` gains `AllViewNames()` = `ViewNames()` plus the
  router-only views. `TestEveryViewSuppliesItsHeaderAndHelp` and
  `TestOnlyTheDashboardIsFrameless` iterate it; `default_view` and completion
  keep `ViewNames()`.
- `createView`: `case command.ViewViewer: a.views[view] = viewer.New(a.config)`.
- `internal/app/viewer.go`: `handleViewerOpenRequest` sets
  `OriginView = a.currentView`, installs, switches, `tea.Batch(Init,
  requestResize)`; `case viewer.BackToOriginMsg: return a, a.switchView(msg.Origin)`.

The router does **no I/O**: the message carries a `Source` and the view loads it
in its own `Init`. That is the difference from `handleWorkspaceScanDetails`,
which loads a cache file the router owns.

## Phase 4 — workspaces opens a file

`enter` already means *scan details*, and only for a git repository with a cached
result. A file is never a git repository, so the branches cannot collide:
`openScanDetails` gains an early branch returning
`viewer.OpenRequestMsg{Source: viewer.NewFileSource(entry.Path)}` for `!entry.IsDir`.

The read happens in the viewer's `Init`, so a refusal (too large, binary) is a
footer error *in the viewer* rather than in workspaces — one place that decides,
one place that reports.

`GetShortcuts` gains `{enter, "View file"}` when the selection is a file
(Rule 130); `GetHelpContent` gains a **Viewing files** section in the same commit
(Rule 114). `ModeSelecting` is untouched — `enter` there confirms a directory.

## Phase 5 — containers: `i` and `l` both open the viewer

- `internal/docker/client.go` gains `Inspect(id) ([]byte, error)`, keeping the
  existing `docker.IsContainerID` guard. `GetContainerLogs` already exists.
- `i` → `viewer.OpenRequestMsg{Source: DockerInspectSource{ID, Name}}`.
- `l` → `viewer.OpenRequestMsg{Source: DockerLogsSource{ID, Name, Tail: 500}}`.
- Both sources live in `internal/ui/containers/sources.go`: they know docker,
  and `internal/viewer` must not.
- **Every pager path goes**, and the Windows branches with them — the temp files
  (`%TEMP%\devdesk-inspect.txt`, `devdesk-logs.txt`) and the `more` fallback
  existed only because `less` is absent on Windows. The viewer is the answer on
  every platform. The `e` escape hatch survives only for logs, through
  `Pageable`, because `less` still handles a gigabyte better than a `viewport`
  does.
- The deletion list at the top of this plan applies here.
- `GetShortcuts` keeps `{i, "Inspect"}` and `{l, "Logs"}`; the help entries stop
  naming `less`, `q`, `w`, `t` and `f`, which now belong to the viewer (Rule 114).

## No configuration setting — **decided**

An `app.syntax_highlight` default was considered and dropped. The viewer opens
with highlighting on and `c` toggles it for the session; nothing is persisted,
so nothing can disagree about it. The configuration view already carries 29
settings, and a preference nobody has asked for twice is not one of them (YAGNI).

If it is wanted later it is one bool and one row in the `app` tab, on the same
terms: the config holds the **opening** value, the viewer never writes it, and
the configuration view stays the single writer — the point of `applyThemeNow`
replacing the old theme picker.

## Phase 6 — tests, docs, backlog

---

## Files to change

| File | Action | Why |
|---|---|---|
| `internal/viewer/*.go` | CREATE | domain package, 11 files, ~850 lines (two moved from containers) |
| `internal/ui/viewer/*.go` | CREATE | the view, 7 files, ~800 lines |
| `internal/app/viewer.go` | CREATE | router handler + return-to-origin |
| `internal/ui/containers/sources.go` | CREATE | the two docker sources |
| `internal/app/app.go` | UPDATE | two cases in `Update` |
| `internal/app/command_line.go` | UPDATE | `createView` case |
| `internal/command/parser.go` | UPDATE | `ViewViewer`, `AllViewNames()` |
| `internal/app/view_contract_test.go` | UPDATE | iterate `AllViewNames()` |
| `internal/ui/theme/colors.go` | UPDATE | the syntax alias block |
| `internal/ui/workspaces/{update,actions,view}.go` | UPDATE | `enter` on a file, shortcuts, help |
| `internal/ui/containers/{model,update,view,commands}.go` | UPDATE | **the logs pane is removed**; `i` and `l` emit open requests |
| `internal/docker/client.go` | UPDATE | `Inspect` |
| `go.mod` / `go.sum` | UPDATE | chroma v2 |
| `.claude/CLAUDE.md` | UPDATE | a Viewer section; the containers logs and inspect lines |
| `docs/backlog.md` | UPDATE | §3.25 |

`internal/ui/containers/update.go` is 707 lines today and drops well under 500 —
progress against §2's 800-line ceiling. No new file exceeds 250.

## Tests

**`internal/viewer`**
- `TestAJSONObjectKeepsTheOrderTheFileGaveIt`
- `TestXMLAttributesAreNodesOfTheirOwn`
- `TestAMalformedJSONOpensAsTextAndSaysSo`
- `TestABinaryFileIsRefusedRatherThanRendered`
- `TestAFileOverTheSizeCapIsRefused`
- `TestDetectionPrefersTheExtensionThenTheContent`
- `TestALogKindIsNeverInferredFromContent`
- `TestAnUnlevelledLineInheritsTheLineAbove` — the stack-trace case
- `TestLevelsAreReadFromJSONLogfmtAndBareText`
- `TestNormalizationStripsANSIAndCarriageReturns`
- `TestHighlightingReproducesTheInputExactly` — the chroma invariant

**`internal/ui/viewer`**
- `TestCollapsingANodeHidesItsDescendants`
- `TestTheCursorStaysOnARowThatSurvivesACollapse`
- `TestAPlainDocumentOffersNoDisplayToggle` (Rule 130)
- `TestOnlyALogDocumentOffersTheVerbosityFilter`
- `TestVerbosityIsMonotone` — `≥ warn` shows errors
- `TestTimestampsAndFollowAppearOnlyForASourceThatSupportsThem`
- `TestEveryTreeCellIsPlainText` (Rule 122)
- `TestEveryTextLineCarriesTheAppBackground` (Rule 115)
- `TestTheHeaderNamesTheFormatAndTheFilter`

**`internal/app`**
- `TestAViewerOpenRequestRecordsWhereItCameFrom`
- `TestEscFromTheViewerReturnsToItsOrigin`
- `TestTheViewerIsNotTypeableAsACommand`
- the two contract tests, now over `AllViewNames()`

**`internal/ui/workspaces`**
- `TestEnterOnAFileAsksForTheViewer`
- `TestEnterOnAScannedGitRepoStillOpensScanDetails`
- `TestTheViewFileShortcutAppearsOnlyForAFile`

**`internal/ui/containers`**
- `TestLogsAsksForTheViewerRatherThanOpeningAPane`
- `TestInspectAsksForTheViewerRatherThanThePager`
- `TestInspectStillRejectsAMalformedContainerID` (existing, retargeted)
- the existing logs-pane tests (`update_test.go:813`, `:959`–`:981`, `:1111`)
  **move to `internal/ui/viewer`** rather than being deleted: they pin scroll and
  resize behaviour that still exists, just somewhere else.

## Validation

```bash
mise run fmt
mise run vet
mise run lint          # golangci-lint — Rule 301, mandatory, zero warnings
mise run test
mise run test-race     # needs gcc/clang on PATH
go build ./...
go mod tidy && go mod verify
```

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| chroma embeds every lexer (several MB) | HIGH — certain | Decided. Measure the delta and record it in §3.25 so the cost is on the record rather than assumed |
| Moving the logs pane regresses behaviour nobody re-tested | MEDIUM | The existing logs tests move with the code instead of being deleted; that is what makes this a move rather than a rewrite |
| An unlevelled line is filtered away with its parent | MEDIUM | Inheritance is the stated trade; `all` is the default verbosity, so nothing is hidden until the user asks |
| Highlighting or filtering on every frame | MEDIUM | Rebuilt on load / `c` / `w` / `v` / width only; `View()` read-only |
| An ANSI reset inside a line strips the app background (Rule 115) | MEDIUM | chroma formatters never used; every token style sets the background; a test asserts it |
| A source capability interface silently unsatisfied | LOW | Each is a single method — there is no half-satisfied case, unlike `HeaderView` |
| `AllViewNames()` drifts from the `createView` switch | LOW | The contract test iterates it and fails on a missing case |
| A minified JSON is one enormous line in the text display | LOW | `w` wraps; the tree is the answer and is the default |

## Acceptance

- [ ] `enter` on a `.json` opens the tree; `f` shows the file's own text; `c`
      turns colour off; `esc` returns to the same directory and cursor
- [ ] `enter` on a `.xml` opens the tree, attributes included
- [ ] `enter` on a `.md` or `.go` opens it as text; on a `.png`, the footer says
      `Not a text file` and clears after three seconds
- [ ] `i` in containers opens the inspect JSON in the viewer, Windows and Linux
      alike; the TUI is never suspended
- [ ] `l` in containers opens the logs in the viewer: `v` filters by level, `/`
      searches, `w` wraps, `t` toggles timestamps, `ctrl+r` reloads, `ctrl+f`
      follows, `e` still reaches the system pager
- [ ] a stack trace after an `ERROR` line survives a `≥ warn` filter
- [ ] `:viewer` is not a command and `viewer` is not offered as `default_view`
- [ ] `mise run check` and `mise run test-race` pass
- [ ] `.claude/CLAUDE.md` and `docs/backlog.md` §3.25 written in the same branch
