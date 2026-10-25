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
| `damaged/` | a non-JSON line in the middle of a file and a sub-agent file cut mid-JSON without a final newline |

Expected totals are written by hand in `parse_test.go`, not computed from these files.
