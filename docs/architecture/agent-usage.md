# Agent usage — `internal/agentusage`

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

Two pieces: the data layer `internal/agentusage` (sections up to
"Verification") and the `:agent-usage` view `internal/ui/agentusageview`
(last section, DEVDESK-41).

## What the package is for

`internal/agentusage` reads the transcripts Claude Code writes under its
projects directory and returns, for one session, the **token counts per
agent** (the orchestrator and each sub-agent) and, since DEVDESK-46, each
agent's **tool-call count** and **time span**. It is the data layer of the
agent usage monitor (DEVDESK-40, spike S-01 then step S-02).

What it does **not** do:

- no cost, price or quota computation;
- no storage, no cache, nothing written to disk;
- no UI, no Bubble Tea import (the view lives in `internal/ui/agentusageview`);
- no dependency beyond the Go standard library.

**The public API is a contract.** Every exported symbol in the table below
(`Tokens`, `Agent`, `State`, `Usage`, `Diagnostics`, `FileIssue`, `IssueKind`,
`SessionFiles`, `SubAgentFiles`, `ParseSession`, `LocateSession`, `ProjectsRoot`,
`ListSessions`, `Session`, `Listing`, and, since DEVDESK-44, `Cursor`, `Delta`,
`ReadDelta`, `Aggregate`, `NewAggregate`, `Activity`, `ErrStaleDelta`)
is consumed by S-03b, S-04, S-05a, S-05b (DEVDESK-45) and S-06 (DEVDESK-46). Changing a
field is a change to those stories; add, do not rename.

## Public API

| Symbol | Role |
|---|---|
| `ProjectsRoot()` | `$CLAUDE_CONFIG_DIR/projects`, else `~/.claude/projects` |
| `LocateSession(parentTranscript)` | from `<dir>/<uuid>.jsonl`, finds `<dir>/<uuid>/subagents/agent-<id>.jsonl` and their meta files. Returns `SessionFiles{ID, Parent, SubAgents}`. A missing session directory means no sub-agents. Returns an error if the parent is missing, is a symbolic link or is not a regular file. |
| `SessionFiles` | `ID`, `Parent` (path of the parent transcript), `SubAgents []SubAgentFiles` |
| `SubAgentFiles` | `ID`, `Transcript`, `Meta` (`""` if the meta file is absent) |
| `ParseSession(SessionFiles)` | returns `Usage`. Fails only if the parent transcript cannot be opened: a missing file, a symbolic link or a non-regular file is an **error**, not an ignored input. Since DEVDESK-44 it is one full delta applied to an empty `Aggregate` (see "Incremental reading"): same result as before, one code path. |
| `Cursor` | how far each file of one session has been read. A plain value: copying it is safe and `ReadDelta` never mutates it. Holds full paths, so `String`/`GoString` redact them (`agentusage.Cursor{files: N}`). `Rewound()` returns it with every position cleared (next read is Full): the `ctrl+r` path. |
| `Delta` | what `ReadDelta` found since a `Cursor`. `Full()`: read from the start of every file. `Empty()`: nothing changed. `String`/`GoString` show counts only. Applied once, by `Aggregate.Apply`, which takes ownership. |
| `ReadDelta(Cursor)` | lists the session again (`Lstat` only for the parent, `subagents/` and the meta files) and reads what was appended since the cursor. Returns `(Delta, error)`. Errors name base names only; on error there is nothing to apply. |
| `NewAggregate(parentTranscript)` | an empty `*Aggregate` for `<dir>/<uuid>.jsonl`. No I/O. |
| `Aggregate` | the session's usage built from successive deltas. Methods: `Cursor()` (a copy, to hand to `ReadDelta`), `Apply(Delta) error`, `Usage() Usage` (a deep copy, same shape and rules as `ParseSession`), `Activity() Activity`. **Not safe for concurrent use**: one goroutine owns it. `String`/`GoString` show counts only. |
| `ErrStaleDelta` | returned by `Apply`, with nothing changed, for a delta that was not read from the aggregate's current cursor. Read again from `Cursor()`. |
| `Activity{LastChange, Unfinished}` | what the applied reads say about the session's life. `Settled(now, quiet)` tells a poller it can stop. See "Activity and `Settled`". |
| `ListSessions(root)` | lists `<root>/<project>/<uuid>.jsonl`, one level deep, never recursively. Returns `Listing`. A missing root is an empty `Listing` and no error; a root that is not a directory or cannot be read is an error naming the base name only. The root itself may be a symbolic link (the user's own choice); anything below it may not. |
| `Session{ID, Parent, Project, ModTime}` | `Parent` is a full path, for `LocateSession`, and is never logged. `Project` is the working directory read from the transcript (`cwd`), sanitized and capped at 4096 bytes, or `""` if none. `ModTime` is the transcript's modification time. |
| `Listing{Sessions, Omitted}` | `Sessions` newest `ModTime` first, ties by ID ascending. `Omitted` counts what was not listed: linked or non-regular transcripts, linked or unreadable project directories, and sessions past the cap. |
| `Tokens{Input, Output, CacheRead, CacheWrite}` | `int64` counters; `Total()` and `Add()` saturate at `math.MaxInt64` |
| `Agent` | `ID` (empty for the orchestrator), `Main`, `Type`, `Models`, `Tokens`, `Messages`, `PartialMessages`, `State`, `SpawnDepth`; since DEVDESK-46 `ToolCalls`, `FirstActivity`, `LastActivity`; `PartialOutput()`, `Duration()` |
| `State` | `StateUnknown`, `StateFinished`, `StateUnfinished` |
| `Usage` | `SessionID`, `Agents` (orchestrator first, then sub-agents by ID), `Versions`, `Diagnostics`; `Total()` |
| `Diagnostics` | `UnreadableLines`, `OversizedLines`, `NoUsage`, `NoMessageID`, `CrossFileDuplicates`, `MetaMissing`, `MetaUnreadable`, `TruncatedFiles`, `Files []FileIssue`; `Degraded()` |
| `FileIssue{File, Offset, Kind}` | one skipped line or file problem. `File` is a base name. `Offset` is the byte offset of the line (for `IssueReadError`, the offset consumed; for `IssueOpenFailed`, 0). Entries are sorted by offset within each file. |
| `IssueKind` | `IssueUnreadable` (not JSON, truncated, or bad counters), `IssueOversized` (longer than `MaxLineBytes`), `IssueOpenFailed` (a sub-agent transcript could not be opened), `IssueReadError` (reading stopped on an I/O error) |

`State` is read from the `finished` key of the sub-agent's meta file: `true`
is `StateFinished`, `false` is `StateUnfinished`. An absent, null or
wrongly typed key, a missing or unreadable meta file, and the orchestrator
are `StateUnknown`.

`ToolCalls` is the number of distinct `tool_use` blocks in the agent's own
transcript (see "Tool calls and time span"). `FirstActivity` and `LastActivity`
are the earliest and latest line timestamps of that transcript, in UTC, zero
when it has none. `Duration()` is `LastActivity - FirstActivity`, or 0 unless
both are known; a caller that must tell "unknown" from "instant" tests
`FirstActivity.IsZero()`.

## Transcript format

```
projects/<project>/<uuid>.jsonl                          orchestrator
projects/<project>/<uuid>/subagents/agent-<id>.jsonl     one per sub-agent
projects/<project>/<uuid>/subagents/agent-<id>.meta.json agentType, finished, spawnDepth
projects/<project>/<uuid>/tool-results/                  never read
```

Each transcript line is a JSON object. Only the root `type`, `timestamp`,
`apiBlockIndex` and `version`, and `message.{id, model, stop_reason, usage}` are
read; the four counters are `input_tokens`, `output_tokens`,
`cache_read_input_tokens` and `cache_creation_input_tokens`. Of
`message.content` (since DEVDESK-46) only the `type` and `id` of each block are
decoded, and only `tool_use` blocks are kept (`wireBlocks`). Text, thinking, tool
`name`, `input` and results, and descriptions are not declared in the decoding
structs, so `encoding/json` skips them without keeping them. Nothing is searched
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
- **Tool calls and time span are folded on their own keys, not by the last line
  of a message** (DEVDESK-46, below).
- An incomplete last line (no newline) is not counted and its file is listed in
  `Diagnostics.TruncatedFiles`. If it already exceeds the line cap it is dropped
  silently.

### Tool calls and time span (DEVDESK-46)

Claude Code 2.1.302 writes **one content block per `assistant` line**: a message
with a thinking block and two tool calls is three lines sharing one `message.id`.
"Last line wins" would keep one block and lose the others, so tool calls have
their own table.

- **Only root `type == "assistant"` lines count**, and `<synthetic>` ones never
  do. `api-request-blob` lines replay earlier `tool_use` blocks in their
  `message.content` and would double count; a line with no root `type` counts
  none. A `queued_command.usage.toolUses` (completion notification) is never
  read: the count comes from the transcript.
- **Key: `(message.id, tool_use.id)`.** The same pair on several lines (streaming
  repeats) is one call; the same tool id in two messages is two calls. The
  greatest position wins, as for tokens, so a call seen in two files goes to
  the later one.
- **Fallbacks, with their limits.** A block with no `tool_use.id` is keyed by
  `(message.id, apiBlockIndex, rank in content)`. With neither an id nor an
  `apiBlockIndex`, two calls of one message split over two lines both fall on
  rank 0 and count once: an undercount, accepted (2.1.302 always writes an id).
  A line with no `message.id` counts its blocks once per line (a tool id repeated
  inside the line counts once); each byte range is folded once, so a counter is
  exact there.
- **Ids are bounded.** A message or tool id longer than `maxLabelLen` (128 bytes)
  is replaced in the key by its SHA-256, so keys have a fixed size and two long
  ids never collide.
- **Timestamps.** The root `timestamp` is parsed as RFC 3339 and normalised with
  `.UTC()` (the oracle compares `Usage` with `reflect.DeepEqual`, so two spellings
  of one instant need one representation). Anything else (absent, null, not a
  string, not a date) is the zero time and costs only the timestamp. Every
  readable line of the agent's own transcript counts, whatever its kind
  (`<synthetic>`, tool results, attachments), so a tool-result line moves
  `LastActivity`. **The span is min to max, not first line to last**: timestamps
  are not monotonic within a file (hundreds of backward steps in one real
  transcript). It is wall clock: pauses and a resumed session are included.

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
- A `message.content` that is not an array, or an element that is not an object,
  yields no tool call and never an error: it costs the tool count of that line,
  not its usage. A timestamp that is not a date costs that timestamp only.
- A sub-agent that cannot be opened stays in `Agents` with no tokens and an
  `IssueOpenFailed` entry. A read error mid-file keeps what was read and adds
  `IssueReadError` at the offset consumed.
- A missing meta file counts in `MetaMissing`; an unreadable one in
  `MetaUnreadable`.

## Incremental reading (DEVDESK-44)

`ParseSession` reads a whole session once. A view that keeps a session open
needs to follow it as it grows without reading a multi-MB transcript again on
every update. `incremental.go` (reading, `Aggregate`, `Activity`) and `table.go`
(the merge) do that. There is no tick, no `gen` and no UI here: wiring them is
S-05b (DEVDESK-45).

```go
agg := agentusage.NewAggregate(parent)       // no I/O
d, err := agentusage.ReadDelta(agg.Cursor()) // in a tea.Cmd (Rule 110)
err = agg.Apply(d)                           // in Update(); ErrStaleDelta if d is not from agg.Cursor()
u := agg.Usage()                             // same shape as ParseSession
agg.Cursor().Rewound()                       // ctrl+r: next ReadDelta is Full
```

### How a file is read

Each file of the session has a read position (`fileCursor`): the `offset` just
past the last complete line folded in, the `size` seen at the last read, the
identity (`os.FileInfo`) of the handle opened, and a few flags below.

- **Unchanged file: `Lstat` only.** If the `Lstat` size equals the size seen and
  `os.SameFile` matches the identity, the file is not opened. A session that
  no longer writes costs one `Lstat` per transcript, one per meta file and one
  `ReadDir` of `subagents/`, and no content read. A cursor holding an
  `IssueReadError` is never "quiet": the bytes after the failure are read again.
- **Append: read from `offset`.** The file is opened (through `openPlain`), the
  handle is `Stat`-ed, and the read resumes at `offset`.
- **A half-written last line is held back and read again.** `offset` stops at
  the end of the last complete line. The next read seeks there and reads the
  incomplete line from its start, now hopefully complete. No bytes are kept in
  the cursor, so a `Cursor` stays a small copyable value and a `\r\n` or a
  UTF-8 sequence split across two reads needs no stitching. The file is listed
  in `Diagnostics.TruncatedFiles` while the incomplete line lasts. Cost: at most
  `MaxLineBytes+2` bytes read again per file per update, only while a line is
  half written.
- **An oversized line still being written is skipped, not held.** If the
  incomplete tail already exceeds `MaxLineBytes+2`, the cursor stores where that
  line started (`skipFrom`) and the next read resumes at the end of what was
  read (the stored size), skipping up to the next `\n`. The line is counted
  **once** in `OversizedLines`, at its start. If the newline never comes, it is
  dropped silently, as `ParseSession` does.
- **Anything that is not an append restarts the whole session.** The delta is
  then `Full()` and every file is read from byte 0, by the same code as
  `ParseSession`. Triggers: a file read before is gone, is a link or not
  regular, fails to open, has another identity (`os.SameFile` from the open
  handle, so a rotation is seen), is smaller than the size seen, or the byte
  just before `offset` is not `\n` (a 1-byte `ReadAt` guard against a
  truncate-and-regrow). A per-file patch is not possible: a message overwritten
  across files cannot be un-counted. A file that appears (a new sub-agent) is
  not a trigger: it is read from 0 inside a normal delta. A first read, or a
  `Rewound` cursor, is Full too.
- **Read errors and open failures are states, not events.** An `IssueReadError`
  or `IssueOpenFailed` describes the *last* read of that file and is cleared by
  a later successful read, as a fresh `ParseSession` would show. Unreadable and
  oversized lines are events: counted once each, since each byte range is
  folded once.

### Meta files

`ReadDelta` never opens a meta file whose `Lstat` size and mtime did not change
(and that was already known). It lists them with `Lstat` instead of
`openPlain`; `LocateSession` is unchanged and still opens them. A meta file is
read again when it appears or when its size or mtime differs.

### The incremental result equals `ParseSession`

This is the oracle of the whole feature: for the same final files,
`Apply`-ing any sequence of deltas gives the `Usage` `ParseSession` gives
(the tests compare them with `reflect.DeepEqual`: fixtures fed in chunks, seeded
random sessions, a fuzz target). It holds because every line gets a position
(`pos{rank, off}`: the file rank in `ParseSession`'s reading order, then the
byte offset) and the rules are functions of positions, not of arrival time:

- per message key, the line with the **greatest** position wins;
- `CrossFileDuplicates` is, over all keys, the number of distinct files minus one;
- models, versions and `Diagnostics.Files` follow the **smallest** position,
  which is first appearance in a sequential read.

Tool calls and spans follow the same rule: a tool key keeps the greatest
position, a span is a min and a max, so both merge in any order.
`hasLines()` also counts them: **a part holding only a timestamped line (a tool
result, an attachment) is a non-empty delta** and advances `Activity.LastChange`,
while a line with neither a timestamp nor a message (`mode`, `last-prompt`) stays
empty. The tests' random sessions and the fuzz target generate tool and
timestamp lines, so the new fields are under the same oracle.

The merge is associative and commutative. Sub-agents may therefore be read in
any order and appear between two updates.

### Declared exceptions to the equality

These are deliberate. A test that expects strict equality must avoid them.

1. **The 100-issue cap per file keeps the smallest positions.** The cap on
   `IssueUnreadable`/`IssueOversized` entries (see "Limits and caps") used to
   keep the first 100 *to arrive*. It now keeps the 100 *smallest positions* of
   the file, which is what makes it independent of how reads were cut. Only
   *which* 100 changes, and only for a file with more than 100; the counters
   are unaffected. Accepted by the user (DEVDESK-44).
2. **`maxNoIDEntries` and `maxToolEntries` are applied in the `Aggregate`.** Past 100000 lines
   without a message id, which lines get no table entry (are "spilled": still
   counted in tokens, `Messages` and `NoMessageID`) depends on how reads were
   cut, not only on position. Totals stay exact (for tool calls, see "Limits and
   caps": a call evicted from the table counts once more if it repeats); this is the one place the
   merge is not commutative. Within a single merge the earliest positions are
   kept. The models a spill contributes are also kept by arrival order (up to
   the 64-per-agent cap). Not asserted equal by the tests.
3. **An unreadable meta file** (for example `chmod 000`) is `MetaUnreadable`
   through `ReadDelta` but `MetaMissing` through `ParseSession(LocateSession(…))`,
   because `LocateSession` opens the meta file to check it and treats a failure
   as absent. Because metas are compared by `Lstat` size and mtime, an
   unreadable meta stays unreadable after `chmod +r` until it is rewritten.
4. **An unopenable listed file is not a `ReadDelta` error.** `ReadDelta` fails
   when the session cannot be listed: the parent is missing, a link, not a
   regular file, or the directories cannot be read. If a listed file cannot be
   opened (for example `chmod 000`), or is removed between the `Lstat` and the
   open, the delta carries an `IssueOpenFailed` on that file: it is `Full()`
   the first time, then `Empty()` while the issue persists. The caller can look
   at the file's `FileIssue`, in `Usage().Diagnostics.Files`. `ParseSession`
   turns the same case on the parent into an error. When `ReadDelta` does
   return an error, the caller must not treat the session as settled:
   `Activity` keeps its last applied state.

### Ownership and stale deltas

`Aggregate` has no mutex: the goroutine of `Update()` owns it (Rule 110). A
`Cursor` is a deep copy holding no pointer, map or shared slice except the
immutable `os.FileInfo`, so `ReadDelta` can run in a `Cmd` while `Update()`
goes on. `Apply` takes ownership of the `Delta`; `Usage()` returns a deep copy.

A `Cursor` carries the aggregate's identity and a sequence number bumped by every
`Apply`. A delta read from anything but the aggregate's current cursor makes
`Apply` return `ErrStaleDelta` and change nothing: two reads started from the
same cursor cannot both be applied. The caller's own generation counter stays
its first filter. **Rule for S-05b: keep a single read in flight per
`Aggregate`.**

### Activity and `Settled`

`Aggregate.Activity()` returns `Activity{LastChange, Unfinished}`.

- `LastChange` is the newest mtime among the parent, the sub-agents and the
  metas, as of the last applied delta that found a change, capped at the time
  of that read (an mtime in the future counts as "now"). It is zero before the
  first `Apply`. A delta that is `Empty()` does not advance it, so a bare
  `touch` (mtime changes, content does not) does not count as activity.
- `Unfinished` counts sub-agents whose readable meta says `finished: false`
  (`StateUnfinished`). `StateUnknown` does **not** count: the quiet period covers
  them, otherwise a session from a client that writes no `finished` key would
  be polled forever.
- `LastChange` never moves back: if the newest file disappears, the previous
  value is kept.
- `Settled(now, quiet)` is true when something was applied, no transcript
  (parent or sub-agent) has an `IssueOpenFailed` or `IssueReadError` (the
  poller keeps trying while such a failure lasts), no sub-agent is unfinished, and
  `now - LastChange >= quiet`. `quiet` is the caller's parameter. A poller
  that stops on `Settled` stays stale if the session resumes after more than
  `quiet` of idleness, until `ctrl+r` or re-entering the session.

### Known limits and risks

- **Append-only is assumed, not verified.** A file rewritten **at the same
  size**, or rewritten so that the byte before `offset` is still `\n`, is not
  detected until the next Full read (`ctrl+r`). A reused inode on ext4 is only
  protected by the size comparison and the guard byte.
- **Check-then-open window (`safeopen.go`).** Between the `Lstat` in
  `readFile` and the `os.Open` in `openPlain`, a file swapped for a FIFO blocks
  the call. This already existed (see "Known risk" below); it is now exercised on
  every update that sees growth, not once per open. Possible direction:
  `O_NONBLOCK` on Unix. Until then, S-05b must keep one read in flight per
  `Aggregate`, so a blocked read cannot pile up.
- **Tool calls overshoot, never undershoot, past the cap.** See "Limits and caps".
- **The message-id table is not bounded.** The table holds one entry per
  distinct `message.id` for as long as the `Aggregate` lives. `ParseSession`
  had the same property for the duration of one call. Only line length and
  lines without id are bounded.
- **Very long `MessageID`s are not hashed.** An id of up to a line's length is
  kept as the map key. (The tool-call keys of DEVDESK-46 do hash ids past 128 bytes; the message table does not.)
- **Windows is not covered by CI** (Linux only). File identity is taken from
  the open handle's `Stat()`, not from `Lstat`, because Go fills the Windows
  identity lazily by path and would read the *new* file after a rotation.
  Check by hand before a release.

## Privacy

`ParseSession` returns **numeric aggregates only**. No prompt, answer, tool
input, description or path is ever stored in a `Usage`.

**Amendment (DEVDESK-46): the content array is walked, for block type and id
only.** `message.content` is decoded on every line, but `wireBlocks` keeps the
`type` and `id` of `tool_use` blocks and nothing else; text, thinking, tool name,
input and result are never declared. A tool id stays inside the package as a
(possibly hashed) map key and never reaches a `Usage`: `Agent` gains an `int` and
two `time.Time`, no string. The `SENTINEL-DO-NOT-LEAK` fixture plants the marker
in a tool `input`, `name`, `text`, `thinking` and in one `tool_use.id`, and the
tests check that no rendering of the `Usage` contains it.

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
- `Cursor`, `Delta` and `Aggregate` hold full paths in unexported fields. They
  are never logged: `String`/`GoString` (and so `%v`, `%+v`, `%#v`) print counts
  only. Errors from `ReadDelta` name base names. A `Usage` still holds no path.
- The fixtures plant `SENTINEL-DO-NOT-LEAK` in the `content`, `description`,
  `cwd` and tool `input` fields of the transcripts and in the `description`
  of the `.meta.json` files. Tests check that it never appears in a `Usage` or
  an error.

## Limits and caps

Memory and output size do not depend on what a transcript contains.

| Bound | Value | Behavior past it |
|---|---|---|
| Line length (`MaxLineBytes`) | 4 MiB | line skipped, counted in `OversizedLines`, never held in memory |
| `FileIssue` entries per file | 100, for `IssueUnreadable` and `IssueOversized` only | counters keep counting, entries stop. The 100 kept are the **smallest positions** of the file, not the first to arrive (declared exception, see "Incremental reading"). `IssueOpenFailed` and `IssueReadError` occur once per file and are always kept |
| Lines without id in the table | 100000, for the whole session (all files together), enforced in the `Aggregate` | still counted in tokens, `Messages` and `NoMessageID`, but aggregated without a table entry. Which lines spill can depend on how reads were cut (declared exception) |
| Bytes read per update (`ReadDelta`) | the appended bytes, plus at most `MaxLineBytes+2` per file with a half-written line, plus 1 guard byte per file read | a Full delta costs what `ParseSession` costs |
| Tool-call entries (`maxToolEntries`) | 100000, for the whole session | a call new to a full table is counted (`extraTools`) without being remembered; a repeat of it would count again, so the total can **overshoot, never undershoot**. On a merge the greatest positions are evicted (`trimTools`, one pass over the excess when it is small, a full sort past 256) so the entries kept do not depend on map order |
| Message-id entries | **not bounded** | one entry per distinct `message.id`, held as long as the `Aggregate` lives |
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
Carried over unchanged, not closed. `ReadDelta` opens transcripts through the
same `openPlain`, so it has the same exposure, now repeated on every update that
sees growth (see "Known limits and risks" under "Incremental reading").

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
  `damaged/` (bad line, file cut mid-JSON), and, since DEVDESK-46,
  `v2.1.302-activity/` (tool calls, timestamps out of order, a replaying
  `api-request-blob`). Expected totals are written by hand
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
| In, Out, Cache read, Cache write, Total | token counts. On a narrow terminal the drop order is `Cache write`, `Cache read`, `In`, `Model`, `Duration`, `Tools`, `State`. `Out` and `Total` stay. |
| State | `running`, `idle`, `done` or `unknown` (DEVDESK-46, see "Agent state and activity"). Fixed width, `Optional`. `/` matches the label. |
| Tools | `Agent.ToolCalls`, compact like tokens; a dim `0`. Fixed, `Optional`. |
| Duration | `Agent.Duration()` (`formatDuration`): `45s`, `3m07s`, `1h05m`, `2d03h`, `>99d`, floored, at most 6 cells; a dim `-` when the transcript has no timestamp. Fixed, `Optional`. |
| Share | the agent's part of the session total (`Usage.Total().Total()`), see below. Fixed width. |

- **Share (`share.go`).** Tenths of a percent, floored, computed on 128 bits
  (`bits.Mul64`/`Div64`) so no product overflows; a part is clamped to
  `[0, total]`. The orchestrator absorbs the rounding residue, so the column
  sums to `100.0%`; the residue is clamped at 0 when saturated totals overshoot.
  With no orchestrator row nothing is absorbed. A session with no tokens gives
  `noShare` and the cell shows a dim `-`. Rendered as `12.3%`.
- **Column order and width.** Agent, ID, State, Tools, Duration, Model, In, Out,
  Cache read, Cache write, Total, Share. With three sub-agents, 100 columns show
  Agent, ID, State, Tools, Duration, Model, Out, Total, Share; 80 columns drop
  Model as well; 60 columns drop Duration, Tools and State and show the five
  columns DEVDESK-43 delivered. Rule 116 holds at each width (rendered extent =
  width - 2). The new columns have no sort: `.` keeps its cycle.
- **Sort.** Only `Total` is sortable (`Less`, `DescFirst`). `.` cycles tree,
  `Total ▼`, `Total ▲`, tree. A table that opened unsorted returns to the tree
  after the last step. `agentRows(usage, tree, clock)` draws the branches only when no
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
- **Header**: `Agents`, `Tokens` (sum of Total), both following the filter, and
  `Updates` (`live` or `paused`, see "Live updates").
- **Footer status**: a failed read (red, "Failed to read this session — check
  logs"), then loading (a full read only; spinner, "Reading session..."), then, when
  `Diagnostics.Degraded()`, a warning "Totals are approximate — some transcript
  data could not be read".
- **Availability (Rule 130).** The shortcut keys are identical at both levels;
  only `Disabled` changes, computed once in `availability()` and read by
  `GetShortcuts` and by the handlers through `guard`, which refuses with a Warn
  carrying the reason. `→` is greyed with no session selected or already inside
  a session; `esc` at the session list. `.` is always available, at both levels, and shown as `. Sort` in the header.
- **Rule 110.** `readDeltaCmd` calls `ReadDelta` inside a `Cmd` on a copy of the
  `Cursor` and returns a message; `Update` alone touches the model and the
  `Aggregate`. Each read carries `agentGen`, bumped on open, on a full `ctrl+r`,
  on leaving the level (`closeAgents`), on `Leave` and on resume; an answer with
  another generation, or arriving after the user went back, is dropped.
  `ctrl+r` at the agents level reads the open session again from the start.

#### Agent state and activity (DEVDESK-46)

`status.go` derives the State column. The package describes what the transcripts
say; what counts as "idle" is a display policy that depends on the view's clock,
so it lives here, not in `internal/agentusage`. `agentStates` is pure: the clock
is a parameter (`statusClock{now, alive}`).

- **`alive`** means the poll chain is live: `!Settled(at, quietPeriod)`. There is
  no other source (no process probe, no session registry): it also works for a
  transcript copied from elsewhere.
- **`quiet`** means `now - LastActivity > idleAfter` (5 min), **strictly**: at
  exactly 5 min an agent is still running. No timestamp, or a timestamp in the
  future (clock skew), is never quiet.
- **`now` is `usageDeltaMsg.at`**, the moment the read finished, not the wall
  clock at render time, so the same answer gives the same states on every frame.

| Agent | Condition | State |
|---|---|---|
| sub-agent | meta `finished: true` | `done` |
| sub-agent | meta `finished: false` | `running`, or `idle` when quiet, whether or not the session is alive |
| sub-agent | no `finished` key, session alive | `running` / `idle` as above |
| sub-agent | no `finished` key, session not alive | `unknown` |
| orchestrator | session alive | `running`, or `idle` when quiet **and no sub-agent is `running`** |
| orchestrator | session not alive | `unknown` |

The orchestrator is **never `done`**: Claude Code writes no end marker for it. A
foreground `Agent` call leaves it silent for as long as the sub-agent runs, so it
is not idle while one runs; if every running sub-agent is itself idle, it is idle
too. Once updates pause it reads `unknown`, so a session waiting at the prompt for
more than 5 min shows `unknown` and `Updates › paused`. Colours (Rule 122): `idle`
in the MEDIUM orange, `unknown` dim, `running` and `done` in the text colour.

**The rows are rebuilt on every successful read, even an empty one** (`applyRead`
in `update.go`). An agent turns idle precisely because nothing arrives, so an
empty delta must still move the clock. `settled` is computed once and used for
both the clock and the decision to stop polling; while a `ctrl+r` full read is
pending the session counts as alive, so the header does not flash `paused`.
`withUsage` is `withRows` plus clearing the loading and error flags; population
is unchanged on an empty delta, so there is no `Remeasure` and the selection
stays on its agent. On `os.ErrNotExist`, `failRead` marks the clock not alive and
calls `withRows` (not `withUsage`), so the red footer stays. Opening or leaving a
session resets the clock.

**Header.** `Idle › N` follows `Updates`, only when N > 0, counted over every row
(`agentTable.Items()`), not the filtered ones: an alert must not hide behind a
filter. No shortcut was added, so Rule 130's key set is unchanged.

**Known limits.**
1. *Idle is a hint.* A long tool call (a 10-minute test run) writes nothing and
   reads idle. The help says so.
2. *Duration is wall clock.* A resumed session spans days between its first and
   last line.
3. *Clock skew.* The idle test compares the transcript's clock with this
   machine's; a sandbox with a skewed clock shows wrong flags (a future timestamp
   is never idle, a past one is).
4. *A lone orchestrator pauses when it would turn idle.* The chain stops at the
   same 5 min, so it reads `unknown`, not `idle` (see "Known limits, deliberate").

#### Live updates (DEVDESK-45)

The agents level follows a session that is still being written. It uses the
incremental API of DEVDESK-44 (`Cursor`, `Delta`, `Aggregate`), not
`ParseSession`: the model holds one `*Aggregate` per open session, and each
read applies a `Delta` to it.

**The polling chain.** Opening a session starts a full read (cursor
`Rewound()`, spinner, "Reading session..."). Each answer schedules the next
step: wait `pollInterval` (2 s, `pollTickCmd`), then `startRead(false)` reads
from the cursor, silently (no spinner). At most one read and one tick exist per
`agentGen`: `reading` blocks a tick that arrives while a read is in flight, and
a tick or an answer of another generation is dropped. An empty delta changes
nothing on screen; a non-empty or full one rebuilds the rows (`withUsage`).
If `Aggregate.Apply` rejects a delta (not read from the current cursor), the
view logs it and reads again from the aggregate's own cursor.

**When the chain stops.** After a successful read, the chain ends if
`Activity().Settled(at, quietPeriod)` is true: `quietPeriod` is 5 min, and a
session with any sub-agent not finished is never settled (see "Activity and
`Settled`"). It also ends on `os.ErrNotExist` (the parent transcript was
deleted). Any other error retries after another 2 s from the same cursor and
keeps the rows already shown; the footer says "Failed to read this session —
check logs", and only the first failure of a streak is logged. A successful
read clears the error.

**`pollStopped`.** The model records the end of the chain as `pollStopped`
(true = stopped). The plan spoke of a `live` field; the code has the opposite
sense, so that the zero value of a fresh model is "polling". It makes a late
tick of the same generation start nothing, and every full read clears it.

**The `Updates` header.** `Updates › live` while an aggregate is open and
`pollStopped` is false, `paused` otherwise. It is derived on every frame
(`updatesState`), not stored. Pressing `ctrl+r` on a settled session clears
`pollStopped` for the duration of the full read, so the header can go
`paused → live → paused` in a few frames when the session is still quiet.

**`ctrl+r` is a full read.** It rewinds the cursor (`Cursor.Rewound()`), reads
everything again, and shows the spinner. If a read is already in flight, it is
never doubled (one per `Aggregate`): `fullPending` is set and the full read
starts when that read answers, whether it succeeded or failed.

**Leaving and coming back** (`LeavingView`, see `app-shell.md`).
- `Leave()` bumps `agentGen`, which drops any tick or read in flight, and
  clears `reading` and `agentsLoading`. The open session and its rows stay. If
  a full read was in flight (a `ctrl+r`, or the open's first read), `Leave` sets
  `fullPending`, so the resume repeats it. It always returns `true`.
- `switchView` calls `Init()` on every entry; at the agents level `Init` adds
  `resumeCmd()`, which answers `agentsResumeMsg`. `handleResume` bumps
  `agentGen` again, clears `pollStopped` and starts a read from the cursor,
  silent: coming back reads what changed while away, and goes back to live if
  the session is still being written. With `fullPending`, the read is full and
  has its spinner.

**The cursor stays on the same agent.** `selection.go` gives each agent a
stable key: `main` for the orchestrator, `sub:<ID>#n` for a sub-agent, `n`
counting the earlier agents with the same ID (two twins with one ID keep their
own keys). Keys are computed on the canonical order (`agentRows`), never on the
sorted or filtered one, otherwise twins would swap keys whenever they swap
ranks. `withUsage` records the key under the cursor before rebuilding the rows
and puts the cursor back on it, whatever the filter and the sort did to the
order; if the agent is gone, the cursor is clamped.

**Column widths are not reshuffled every two seconds.** `withUsage` calls
`Remeasure()` only after a full read, or when the *population* changed. The
population is the list of keys plus the models of each row (`populationOf`):
counts change on every delta and do not count. An agent's type can arrive late
(its meta file is written after its transcript); the type is deliberately not
part of the population, so a late type stays truncated to the width measured
earlier until the next `ctrl+r` or change of population.

**The sessions list is not live.** Only the open session is polled. The list
of sessions is read on entry and by `ctrl+r` at the sessions level.

**Privacy.** `usageDeltaMsg` implements `String` and `GoString` so that `%v`
and `%+v`, `%#v` print only `gen`, the delta's own redacted form and whether
the read failed. A `Delta` holds file paths behind unexported fields, which
`fmt` cannot hand to the `Delta`'s redacting methods when the message is
dumped. Read errors are logged and never shown.

**Known limits, deliberate.**
1. *An unreadable parent shows an empty orchestrator.* If the parent
   transcript exists but cannot be read (for instance `chmod 000`),
   `ReadDelta` returns `err == nil`: the screen shows an orchestrator at 0 and
   the warning "Totals are approximate…", where the earlier `ParseSession`
   path showed the red "Failed to read this session" footer. Accepted by the
   product owner; it is documented here rather than worked around.
2. *A session that resumes after `Settled` stays `paused`.* Once the chain has
   stopped on a quiet session, nothing polls the file any more. The header
   keeps saying `paused` until `ctrl+r` or until the user leaves the view and
   comes back (the resume reads the cursor and restarts the chain).
3. *A failed `ctrl+r` full read is not replayed.* If the full read started by
   `ctrl+r` fails for a cause other than `ErrNotExist`, the retry starts from
   the aggregate's cursor and does not rewind it: the `ctrl+r` is lost, and
   only the red footer says that something failed. Press `ctrl+r` again.
4. *Some exits do not call `Leave`.* The paths that leave the view by another
   means than `switchView` (the viewer, netdiag, the path picker) are not
   reachable from `:agent-usage` today. If one is added, it has to stop the
   chain the way `Leave` does, or a poll would continue under another view.

**Privacy.** The view displays the project directory, the date, the session ID,
agent types, models, token counts, states, tool-call counts and durations. It
never displays or logs the paths of files a session touched, nor any content
(prompt, reply, tool name, input or output):
`Usage` holds numbers and labels only, as stated in "Privacy" above.

Known limits: the FIFO risk and the lack of a memory or time bound before the
10000-session cap (see "Limits and caps") both apply to this view's read. Claude
Code deletes old transcripts itself (30 days by default), so older sessions
disappear from the list.
