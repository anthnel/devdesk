# Agent usage S-02: parse a session's usage per agent (DEVDESK-40)

Package `internal/agentusage`, stdlib only, no UI dependency. Plane tasks DEVDESK-49..54.

## Steps

1. (49) Capped line reader (`scanLines`, 4 MiB max) and `Tokens`.
2. (50) `decodeLine` (record decoding) plus a fuzz test.
3. (51) `meta.json` reading, `LocateSession` / `ProjectsRoot`.
4. (52) Aggregation, last line wins.
5. (53) `ParseSession` with fixtures (basic, other-version, damaged) and a sentinel test.
6. (54) ccusage cross-check test (`DEVDESK_CCUSAGE_REF`, skipped by default), `agentusage-ref` mise task, `docs/architecture/agent-usage.md`, a line in `.claude/CLAUDE.md`.

## Public interfaces (contract for S-03b / S-04 / S-05a)

- `Tokens{Input, Output, CacheRead, CacheWrite int64}` with `Total()` and `Add()`.
- `Agent{ID, Main, Type, Models, Tokens, Messages, PartialMessages, State, SpawnDepth}`, with `PartialOutput()` (true when `PartialMessages > 0`).
- `Usage{SessionID, Agents, Versions, Diagnostics}`. `Versions` is the list of distinct transcript versions seen (at most 64); there is no counter of "unknown versions".
- `Diagnostics{UnreadableLines, OversizedLines, NoUsage, NoMessageID, CrossFileDuplicates, MetaMissing, MetaUnreadable, TruncatedFiles []string, Files []FileIssue}` and `Degraded()`.
- `FileIssue{File (base name), Offset, Kind}` with `IssueKind` = `IssueUnreadable`, `IssueOversized`, `IssueOpenFailed`, `IssueReadError`.

These field lists are final; keep them stable.

## Assumptions

- Max line length is 4 MiB (`MaxLineBytes`); longer complete lines are skipped and counted as oversized.
- A record without `message.id` is counted once (`NoMessageID`).
- `NoUsage` counts a record with a non-empty `message.id` OR a non-empty `model` but no `usage`.
- A missing `stop_reason` means the output is partial.
- Symlinks and non-regular files are refused (no FIFO is opened). A FIFO swapped in between the check and the open (TOCTOU) is accepted as a known residual risk.
- Last line wins per message; attribution is per file.
- `PartialOutput` is computed for every agent; S-03b decides how to display it.
- An over-long final line with no newline is dropped (not in `Tail`, not counted).

## Bounds (`limits.go`, `lines.go`, `meta.go`)

- Line: 4 MiB.
- `FileIssue` entries: 100 per file for `IssueUnreadable` and `IssueOversized`; the counters keep counting past it.
- Lines without a message id: 100000 table entries per session; past it they still count in `NoMessageID`, `Messages` and tokens.
- Models per agent: 64. Versions per session: 64.
- Labels (model, version, id, file name): 128 bytes, control characters replaced by `?`.
- Meta file: 1 MiB.
- Errors carry fixed messages only, never file content or full paths.

## Step 6 detail

- Test `crosscheck_test.go` compares `ParseSession` with a ccusage reference file; skipped unless `DEVDESK_CCUSAGE_REF` is set.
- Mise task `agentusage-ref` (`set -euo pipefail`, ccusage pinned, output to a file outside the repo): `mise run agentusage-ref -- <uuid>... > /path/outside/repo/ref.json`. A failing ccusage run fails the task, so no partial reference is written with exit code 0.
- Fixtures carry `SENTINEL-DO-NOT-LEAK` in content, description, cwd and tool input; `TestParseSessionNeverLeaksContent` fails if a decoding struct picks any of them up.
