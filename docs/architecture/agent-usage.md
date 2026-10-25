# Agent usage — `internal/agentusage`

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## What the package is for

`internal/agentusage` reads the transcripts Claude Code writes under its
projects directory and returns, for one session, the **token counts per
agent** (the orchestrator and each sub-agent). It is the data layer of the
agent usage monitor (DEVDESK-40, spike S-01 then step S-02).

What it does **not** do:

- no cost, price or quota computation;
- no storage, no cache, nothing written to disk;
- no UI, no Bubble Tea import;
- no dependency beyond the Go standard library.

**The public API is a contract.** Every exported symbol in the table below
(`Tokens`, `Agent`, `State`, `Usage`, `Diagnostics`, `FileIssue`, `IssueKind`,
`SessionFiles`, `SubAgentFiles`, `ParseSession`, `LocateSession`, `ProjectsRoot`)
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

The package returns **numeric aggregates only**. No prompt, answer, tool input,
description or path is ever stored in a `Usage`.

- Errors carry the base name of a file, never its full path (`baseOnly`), and
  fixed messages instead of `encoding/json` errors, which can quote content.
- Labels that come from disk (model, version, agent type, session and agent
  IDs, file names) go through `sanitizeLabel`: control characters become `?`
  and the label is cut to 128 bytes. Text read from disk reaches a terminal,
  where an escape sequence would be interpreted.
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

## Known risk: a FIFO swapped in between `Lstat` and `Open`

`openPlain` (`safeopen.go`) calls `Lstat`, refuses links and non-regular
files, opens the file, then compares the handle with the `Lstat` result using
`os.SameFile`. That closes the check-then-open window for links. It does not
for a regular file replaced by a **FIFO** between `Lstat` and `Open`: `os.Open`
blocks until a writer appears, before `SameFile` can run. Closing it would need
`O_NONBLOCK`, which the `os` package does not offer portably. The window is tiny
and the attacker must already control the transcripts directory. Accepted and
documented in the code.

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
