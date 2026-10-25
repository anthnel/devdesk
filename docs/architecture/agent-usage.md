# Agent usage — `internal/agentusage`

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

Two pieces: the data layer `internal/agentusage` (sections up to
"Verification") and the `:agent-usage` view `internal/ui/agentusageview`
(last section, DEVDESK-41).

## What the package is for

`internal/agentusage` reads the transcripts Claude Code writes under its
projects directory and returns, for one session, the **token counts per
agent** (the orchestrator and each sub-agent). It is the data layer of the
agent usage monitor (DEVDESK-40, spike S-01 then step S-02).

What it does **not** do:

- no cost, price or quota computation;
- no storage, no cache, nothing written to disk;
- no UI, no Bubble Tea import (the view lives in `internal/ui/agentusageview`);
- no dependency beyond the Go standard library.

**The public API is a contract.** Every exported symbol in the table below
(`Tokens`, `Agent`, `State`, `Usage`, `Diagnostics`, `FileIssue`, `IssueKind`,
`SessionFiles`, `SubAgentFiles`, `ParseSession`, `LocateSession`, `ProjectsRoot`,
`ListSessions`, `Session`, `Listing`)
is consumed by S-03b, S-04 and S-05a. Changing a field is a change to those
stories; add, do not rename.

## Public API

| Symbol | Role |
|---|---|
| `ProjectsRoot()` | `$CLAUDE_CONFIG_DIR/projects`, else `~/.claude/projects` |
| `LocateSession(parentTranscript)` | from `<dir>/<uuid>.jsonl`, finds `<dir>/<uuid>/subagents/agent-<id>.jsonl` and their meta files. Returns `SessionFiles{ID, Parent, SubAgents}`. A missing session directory means no sub-agents. Returns an error if the parent is missing, is a symbolic link or is not a regular file. |
| `SessionFiles` | `ID`, `Parent` (path of the parent transcript), `SubAgents []SubAgentFiles` |
| `SubAgentFiles` | `ID`, `Transcript`, `Meta` (`""` if the meta file is absent) |
| `ParseSession(SessionFiles)` | returns `Usage`. Fails only if the parent transcript cannot be opened: a missing file, a symbolic link or a non-regular file is an **error**, not an ignored input. |
| `ListSessions(root)` | lists `<root>/<project>/<uuid>.jsonl`, one level deep, never recursively. Returns `Listing`. A missing root is an empty `Listing` and no error; a root that is not a directory or cannot be read is an error naming the base name only. The root itself may be a symbolic link (the user's own choice); anything below it may not. |
| `Session{ID, Parent, Project, ModTime}` | `Parent` is a full path, for `LocateSession`, and is never logged. `Project` is the working directory read from the transcript (`cwd`), sanitized and capped at 4096 bytes, or `""` if none. `ModTime` is the transcript's modification time. |
| `Listing{Sessions, Omitted}` | `Sessions` newest `ModTime` first, ties by ID ascending. `Omitted` counts what was not listed: linked or non-regular transcripts, linked or unreadable project directories, and sessions past the cap. |
| `Tokens{Input, Output, CacheRead, CacheWrite}` | `int64` counters; `Total()` and `Add()` saturate at `math.MaxInt64` |
| `Agent` | `ID` (empty for the orchestrator), `Main`, `Type`, `Models`, `Tokens`, `Messages`, `PartialMessages`, `State`, `SpawnDepth`; `PartialOutput()` |
| `State` | `StateUnknown`, `StateFinished`, `StateUnfinished` |
| `Usage` | `SessionID`, `Agents` (orchestrator first, then sub-agents by ID), `Versions`, `Diagnostics`; `Total()` |
| `Diagnostics` | `UnreadableLines`, `OversizedLines`, `NoUsage`, `NoMessageID`, `CrossFileDuplicates`, `MetaMissing`, `MetaUnreadable`, `TruncatedFiles`, `Files []FileIssue`; `Degraded()` |
| `FileIssue{File, Offset, Kind}` | one skipped line or file problem. `File` is a base name. `Offset` is the byte offset of the line (for `IssueReadError`, the offset consumed; for `IssueOpenFailed`, 0). Entries are sorted by offset within each file. |
| `IssueKind` | `IssueUnreadable` (not JSON, truncated, or bad counters), `IssueOversized` (longer than `MaxLineBytes`), `IssueOpenFailed` (a sub-agent transcript could not be opened), `IssueReadError` (reading stopped on an I/O error) |

`State` is read from the `finished` key of the sub-agent's meta file: `true`
is `StateFinished`, `false` is `StateUnfinished`. An absent, null or
wrongly typed key, a missing or unreadable meta file, and the orchestrator
are `StateUnknown`.

## Transcript format

```
projects/<project>/<uuid>.jsonl                          orchestrator
projects/<project>/<uuid>/subagents/agent-<id>.jsonl     one per sub-agent
projects/<project>/<uuid>/subagents/agent-<id>.meta.json agentType, finished, spawnDepth
projects/<project>/<uuid>/tool-results/                  never read
```

Each transcript line is a JSON object. Only `version` and `message.{id, model,
stop_reason, usage}` are read; the four counters are `input_tokens`,
`output_tokens`, `cache_read_input_tokens` and `cache_creation_input_tokens`.
Content, tools and descriptions are not declared in the decoding structs, so
`encoding/json` skips them without keeping them. Nothing is searched
recursively. Symbolic links are handled by position:

- a **parent transcript** that is a symbolic link or not a regular file is an
  **error** (`LocateSession` and `ParseSession`);
- a sub-agent transcript that is a link or not a regular file is not listed by
  `LocateSession`; if one is passed to `ParseSession` anyway, it stays in
  `Agents` with an `IssueOpenFailed` entry;
- a meta file that is a link or not a regular file counts as missing
  (`MetaMissing`);
- a session directory or `subagents/` directory that is a link means no
  sub-agents.

## Aggregation rules

- **Last line wins per `message.id`**, over the whole session. Streaming writes
  the same message several times; only the final state counts.
- **A message seen in several files is attributed to the last file read**
  (orchestrator first, then sub-agents sorted by ID). Each such overwrite counts
  in `Diagnostics.CrossFileDuplicates`.
- **A line without `message.id` counts once**, keyed by file and offset, and
  increments `NoMessageID`.
- **`<synthetic>` is excluded**: locally generated turns are never billed.
- **A line with neither id, model nor usage is ignored** (user and tool-result
  lines).
- **Partial output**: a message whose last line has a null or absent
  `stop_reason` is counted in `Agent.PartialMessages`, and `PartialOutput()` is
  true. Its output count is a floor. This is computed for every agent; the UI
  decides how to show it.
- **Saturation**: sums stop at `math.MaxInt64` instead of wrapping.
- An incomplete last line (no newline) is not counted and its file is listed in
  `Diagnostics.TruncatedFiles`. If it already exceeds the line cap it is dropped
  silently.

## Tolerance

The format is not a documented API, so the parser degrades rather than fails.

- Unknown fields are ignored.
- A message with an id or model but **no `usage`** (absent, or renamed by a
  newer client) is counted in `Diagnostics.NoUsage` and adds nothing. A
  sub-agent with no usable meta `finished` is `StateUnknown`.
- Metadata of an unexpected type (`model`, `version`, `agentType`,
  `spawnDepth`) costs that field only, not the line.
- A counter that is negative, fractional or not a number makes the line
  unreadable (`UnreadableLines`). An absent or null counter is 0.
- A sub-agent that cannot be opened stays in `Agents` with no tokens and an
  `IssueOpenFailed` entry. A read error mid-file keeps what was read and adds
  `IssueReadError` at the offset consumed.
- A missing meta file counts in `MetaMissing`; an unreadable one in
  `MetaUnreadable`.

## Privacy

`ParseSession` returns **numeric aggregates only**. No prompt, answer, tool
input, description or path is ever stored in a `Usage`.

**Amendment (DEVDESK-41): `cwd` is now read and displayed.** `ListSessions`
alone reads it, to tell sessions apart by project. It is the first non-empty
string `cwd` found in the first 8 MiB of a transcript (twice `MaxLineBytes`, so
one maximal line can be skipped); the scan stops there and nothing else of the
transcript is kept. The view shows the **full path**, with the home folder
folded to `~`. The path is sanitized and capped, and is **never logged**: the
view logs a failed listing, but its error carries a base name only. A `Usage`
still carries no path. This replaces the earlier wording that no path was ever
read.

- Errors carry the base name of a file, never its full path (`baseOnly`), and
  fixed messages instead of `encoding/json` errors, which can quote content.
- Text that comes from disk goes through `sanitize`: labels (model, version,
  agent type, session and agent IDs, file names; `sanitizeLabel`, cut to 128
  bytes) and the `cwd` (cut to 4096 bytes). Replaced by `?`: invalid UTF-8
  bytes, every control character (C0, DEL and **C1**, so a raw or encoded
  0x9B CSI is caught) and every format character (Unicode category **Cf**:
  bidi overrides and isolates, zero-width marks). Text read from disk reaches a
  terminal, where an escape sequence would be interpreted and a bidi override
  would reorder what follows.
- `FileIssue.File` is a base name, never a path.
- The fixtures plant `SENTINEL-DO-NOT-LEAK` in the `content`, `description`,
  `cwd` and tool `input` fields of the transcripts and in the `description`
  of the `.meta.json` files. Tests check that it never appears in a `Usage` or
  an error.

## Limits and caps

Memory and output size do not depend on what a transcript contains.

| Bound | Value | Behavior past it |
|---|---|---|
| Line length (`MaxLineBytes`) | 4 MiB | line skipped, counted in `OversizedLines`, never held in memory |
| `FileIssue` entries per file | 100, for `IssueUnreadable` and `IssueOversized` only | counters keep counting, entries stop. `IssueOpenFailed` and `IssueReadError` occur once per file and are always kept |
| Lines without id in the table | 100000, for the whole session (all files together) | still counted in tokens, `Messages` and `NoMessageID`, but aggregated without a table entry |
| Models per agent / versions per session | 64 | extra ones dropped |
| Label length | 128 bytes | truncated |
| Meta file | 1 MiB | refused (`MetaUnreadable`) |
| Sessions listed (`ListSessions`) | 10000 | the oldest are dropped and counted in `Listing.Omitted` |
| `cwd` probe (`ListSessions`) | first 8 MiB of each kept transcript | `Project` is `""` |
| `Session.Project` length | 4096 bytes | truncated |

The 10000 cap applies after the directories are read: there is **no bound on
memory or time before it**. Every entry of every project directory is read and
stat-ed first, so a very large projects tree costs that much before the cap
trims the result. Only the `cwd` probe, run on the kept sessions, is bounded.

## Known risk: a FIFO swapped in between `Lstat` and `Open`

`openPlain` (`safeopen.go`) calls `Lstat`, refuses links and non-regular
files, opens the file, then compares the handle with the `Lstat` result using
`os.SameFile`. That closes the check-then-open window for links. It does not
for a regular file replaced by a **FIFO** between `Lstat` and `Open`: `os.Open`
blocks until a writer appears, before `SameFile` can run. Closing it would need
`O_NONBLOCK`, which the `os` package does not offer portably. The window is tiny
and the attacker must already control the transcripts directory. Accepted and
documented in the code. `ListSessions` has the same exposure: it opens each
kept transcript through `openPlain` to read its `cwd`, so a FIFO swapped in
there can block the listing (and so the view's read, which runs in a `Cmd`).
Carried over unchanged, not closed.

## Diagnostics and `Degraded`

`Diagnostics.Degraded()` is true if anything was skipped or guessed: unreadable
or oversized lines, `NoUsage`, `NoMessageID`, `CrossFileDuplicates`, meta
missing or unreadable, truncated files, or any `FileIssue`. A consumer should
show the totals as usual and flag them as approximate when it is true. A new
counter must be added to `Degraded()`.

## Verification

- **Spike S-01**: the parsing rules were checked against ccusage on 3 real
  sessions, with a **0.00 %** difference.
- **Fixtures** in `internal/agentusage/testdata/` are synthetic: shape follows
  Claude Code 2.1.302, every value is invented. `v2.1.302-basic/`,
  `synthetic-other-version/` (renamed or absent usage, unknown line type) and
  `damaged/` (bad line, file cut mid-JSON). Expected totals are written by hand
  in `parse_test.go`. See `testdata/README.md`.
- **Cross-check against ccusage** (step 6 of S-02, DEVDESK-54), reference
  version `ccusage@20.0.43`. `crosscheck_test.go` compares `ParseSession` with
  a reference file and is **skipped unless `DEVDESK_CCUSAGE_REF` is set**
  (path of the reference file). CI never sets it, so **CI does not run this
  test**: run it by hand. `DEVDESK_CLAUDE_PROJECTS` points to the projects
  directory to read (default: `ProjectsRoot()`). Each session is looked up as
  `<root>/<project>/<id>.jsonl`.
  The reference file is produced by the `mise` task:

  ```bash
  mise run agentusage-ref -- <uuid>... > /path/outside/repo/ref.json
  DEVDESK_CCUSAGE_REF=/path/outside/repo/ref.json go test ./internal/agentusage -run TestSessionTotalsMatchCcusage -v
  ```

  The task needs a POSIX shell, `npx` and `jq` (no Windows), and runs with
  `set -euo pipefail`: a failing ccusage call fails the task. For each session
  id it runs `ccusage claude session --id <uuid> --json` and writes
  `{"sessions": [{id, total, input, output, cacheRead, cacheWrite}, ...]}`,
  where `total` is ccusage's `totalTokens` and the four others are sums over its
  `entries`. Write the file **outside the repository**: it comes from real
  sessions and must never be committed.

  Only `total` is required. `input`, `output`, `cacheRead` and `cacheWrite` are
  optional per session: when present, each is compared too, and each must be
  within the tolerance (2 %, strictly below). A session whose transcript is not
  found, or whose parsing fails, fails the test.

## The `:agent-usage` view

`internal/ui/agentusageview`, opened with `:agent-usage` or `:au`. It lists the
Claude Code sessions found on this machine, newest first, and reads only where
and when a session ran at this level (see the privacy amendment above). `→` or
`enter` opens the selected session's agents (DEVDESK-42, below).

| Column | Content |
|---|---|
| Project | the `cwd`, **full path** with home folded to `~` (`theme.FoldHome`, done once per row in `Update`, never in a `Cell`). Cut from the left when too narrow, so the leaf stays visible. `-` (dim) when the transcript named none. |
| Last active | the transcript's `ModTime`, local time, `2006-01-02 15:04` |
| Session | the first 8 characters of the ID. `Optional`: the first column dropped on a narrow terminal. The filter (`/`) matches the full ID. |

- **Rule 139.** The body is always `m.table.View()`. Loading, an empty
  machine and a filter with no match all leave a table with its header.
- **Header** (`GetHeaderInfo`): `Sessions` (rows visible after the filter) and,
  only when non-zero, `Skipped` (`Listing.Omitted`).
- **Footer** (Rule 128), derived on every frame with no timer, in this order:
  a failed read (red, "Failed to list Claude Code sessions — check logs"; the
  error is logged, never shown, since it can name a path), then loading (spinner,
  "Reading Claude Code sessions..."), then "No Claude Code sessions found in
  <root>" when the listing is empty before any filter.
- **Shortcuts**: `ctrl+r` Refresh, `/` Filter, `?` Help. The same keys in every
  state, none greyed (Rule 130): each always acts. `.` cycles the sort column
  and is listed in `?` only (Rule 138).
- **Reload on every entry.** The view is built once and stays cached in the
  router (`createView` stops if it already exists). The reload comes from
  `switchView` (`internal/app/command_line.go`), which calls `Init()` on every
  entry, and `Init` starts a read. The rows from the previous visit stay on
  screen until the answer arrives. Unlike `ctrl+r`, this entry reload shows no
  spinner and no "Reading..." line, because `Init` cannot set `loading` to
  true. `ctrl+r` reads again under a new generation number, and a stale answer
  (older generation) is dropped. A failed refresh keeps the previous rows: they
  are dated, not wrong.
- **Rule 110.** `listSessionsCmd` resolves `ProjectsRoot()` and calls
  `ListSessions` inside the `Cmd` and returns a message; `Update` alone touches
  the model.

### Second level: the session's agents (DEVDESK-42)

`→`/`enter` on a session opens its agents; `←`/`esc` goes back to the list.
`agents.go` builds the table, `availability.go` decides what the keys allow.

| Column | Content |
|---|---|
| Agent | `orchestrator`, then each sub-agent's type behind `├─ ` / `└─ ` in tree order, bare when sorted (`unknown` when the meta file gave none). `/` matches the role only. |
| ID | the sub-agent's short ID (`shortAgentIDs`): the smallest common length >= 8 runes at which all IDs of the session differ, capped at each ID's own length. `-` for the orchestrator, `?` for a sub-agent with an empty ID. `/` matches the full ID here. Never dropped, so two agents of one type stay apart. |
| Model | the models the agent used, comma-separated; `-` (dim) when none. `Optional`. |
| In, Out, Cache read, Cache write, Total | token counts. On a narrow terminal the drop order is `Cache write`, `Cache read`, `In`, `Model`. `Out` and `Total` stay. |
| Share | the agent's part of the session total (`Usage.Total().Total()`), see below. Fixed width. |

- **Share (`share.go`).** Tenths of a percent, floored, computed on 128 bits
  (`bits.Mul64`/`Div64`) so no product overflows; a part is clamped to
  `[0, total]`. The orchestrator absorbs the rounding residue, so the column
  sums to `100.0%`; the residue is clamped at 0 when saturated totals overshoot.
  With no orchestrator row nothing is absorbed. A session with no tokens gives
  `noShare` and the cell shows a dim `-`. Rendered as `12.3%`.
- **Sort.** Only `Total` is sortable (`Less`, `DescFirst`). `.` cycles tree,
  `Total ▼`, `Total ▲`, tree. A table that opened unsorted returns to the tree
  after the last step. `agentRows(usage, tree)` draws the branches only when no
  sort is active: a sorted list is flat, and a branch there would lie about who
  is whose child. `cycleAgentSort` rebuilds the rows after each press and
  remeasures the Agent column. The sort lives in the table, so it survives
  opening another session and `ctrl+r`.

- **Compact tokens.** `formatTokens` floors to one decimal (`1099` is `1.0k`,
  never `1.1k`) and drops the decimal from 100 of a unit up. Units: `k M B T`.
  Zeros are dim (Rule 122).
- **`≥` on Out.** A sub-agent whose output is partial (`Agent.PartialOutput()`,
  transcript still being written) shows `≥ ` before its Out count: a floor, not
  an exact value. The orchestrator never does.
- **Flat tree, two levels.** Whatever `SpawnDepth` a sub-agent has, it is a
  direct child of the orchestrator in the table. Order: orchestrator first, then
  sub-agents sorted by ID (stable) until the user sorts by Total.
- **Breadcrumb.** `Sessions › <project> › <short ID>` sits in the footer, one row
  above the filter bar and the message line (Rule 123), and adds one row to
  `GetFooterHeight`.
- **Header**: `Agents` and `Tokens` (sum of Total), both following the filter.
- **Footer status**: a failed read (red, "Failed to read this session — check
  logs"), then loading (spinner, "Reading session..."), then, when
  `Diagnostics.Degraded()`, a warning "Totals are approximate — some transcript
  data could not be read".
- **Availability (Rule 130).** The shortcut keys are identical at both levels;
  only `Disabled` changes, computed once in `availability()` and read by
  `GetShortcuts` and by the handlers through `guard`, which refuses with a Warn
  carrying the reason. `→` is greyed with no session selected or already inside
  a session; `esc` at the session list. `.` is always available, at both levels, and shown as `. Sort` in the header.
- **Rule 110.** `parseSessionCmd` calls `ParseSession` inside a `Cmd`. Each parse
  carries `agentGen`, bumped on open, on `ctrl+r` and on leaving the level; an
  answer with another generation, or arriving after the user went back, is
  dropped. `ctrl+r` at the agents level re-parses the open session.

**Privacy.** The view displays the project directory, the date, the session ID,
agent types, models and token counts. It never displays or logs the paths of
files a session touched, nor any content (prompt, reply, tool input or output):
`Usage` holds numbers and labels only, as stated in "Privacy" above.

Known limits: the FIFO risk and the lack of a memory or time bound before the
10000-session cap (see "Limits and caps") both apply to this view's read. Claude
Code deletes old transcripts itself (30 days by default), so older sessions
disappear from the list.
