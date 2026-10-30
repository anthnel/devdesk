# Test fixtures

Every file here is **synthetic**: the shape (keys, nesting, file layout) follows
what Claude Code 2.1.302 writes under `~/.claude/projects/`, but every value is
invented. No real prompt, answer, path, description or token count was copied.
`SENTINEL-DO-NOT-LEAK` sits in the `content`, `description`, `cwd` and tool
`input` fields of the transcripts, and in the `description` of the `.meta.json`
files, on purpose: a test checks that it never reaches a `Usage` value or an
error. That check guards `ParseSession`/`Usage` only: since DEVDESK-41 `ListSessions`
does read `cwd` on purpose, and its tests (`sessions_test.go`) build their own
trees in a temp directory instead of using these fixtures.

Layout: `<fixture>/projects/<project>/<session>.jsonl`, sub-agents in
`<session>/subagents/agent-<id>.jsonl` with `agent-<id>.meta.json` beside them.

| Fixture | Purpose |
|---|---|
| `v2.1.302-basic/` | orchestrator + 3 sub-agents, streaming repeats, null/absent usage fields, a `<synthetic>` message, a partial last message, and a `tool-results/` folder that must not be read |
| `synthetic-other-version/` | an invented "9.0.0": renamed or absent `usage`, unknown line type, `meta.json` without `finished` |
| `v2.1.302-activity/` | tool calls, timestamps and states (DEVDESK-46): the orchestrator with 2 `Agent` calls, timestamps out of order and an `attachment` claiming `queued_command.usage.toolUses: 99` (ignored); `b01` (meta `finished: false`) with 5 distinct tool calls over 7 `assistant` lines (one message spread over 3 lines, one streamed twice), 2 `api-request-blob` lines replaying calls (null timestamp, count nothing) and 2 tool results, spanning 10:01:00Z to 10:07:30Z (6m30s); `b02` (`finished: true`) with 1 call and no timestamp (unknown duration) |
| `v2.1.302-cost/` | `cost-state` lines (DEVDESK-47): a parent with two models, a 5 min / 1 h cache write split, a first `cost-state` (7.08) followed by more usage and a second cumulative one (12.61, the last line, so `Billed` is known); a sub-agent file holding its own `cost-state` (99, ignored) |
| `damaged/` | a non-JSON line in the middle of a file and a sub-agent file cut mid-JSON without a final newline |

Expected totals are written by hand in `parse_test.go` (and, for `v2.1.302-activity/`,
tool calls and times in `fixture_activity_test.go`), not computed from these files.

In `v2.1.302-activity/` the sentinel also sits in a tool `name`, `text`, `thinking`
and inside one `tool_use.id` (`toolu_SENTINEL-DO-NOT-LEAK_1`), because since
DEVDESK-46 the content array is walked for block type and id. A test checks that no
rendering of the `Usage` (`%v`, `%+v`, `%#v`) contains it.
