# Journal

Dated entries, most recent first (AAAA-MM-JJ). Each entry records what was
decided, why, and which alternative was rejected. Sources live in `docs/product/`.

## 2026-10-24 — agent-usage-monitor: product discussion

Idea: a live DevDesk view of Claude Code agent and sub-agent token usage.
Working files: `docs/product/agent-usage-monitor/` (`brief.md`, `research.md`,
`feasibility.md`, `stories.md`).

1. **Scope the idea into a view on this machine.** A live view of consumption
   by Claude Code agents and sub-agents.
   - Rejected: an open-ended usage dashboard.

2. **MVP cut: token split per agent and sub-agent, live.** Covers host sessions
   only, computed from transcripts, with no persisted history.
   - Why: the critic found overlap with ccusage and Usage-Monitor, a fragile
     quota source, and too broad a scope. The distinct value is the per-sub-agent split.
   - Deferred to v2+: 5h/weekly quota (only source is the statusline JSON,
     which competes with the user's own statusline; `/usage` already shows it);
     persisted history and per-feature comparison (no session-to-story link,
     transcripts purged after 30 days); other agent CLIs; cloud sessions;
     OpenTelemetry.

3. **Ranking unit: tokens.** Max/Pro accounts are token-quota based. The dollar
   figure is an indicative catalogue price only.
   - Rejected: dollars as the primary unit.

4. **Feasibility choices.**
   - Source: JSONL transcripts in `~/.claude/projects` (`subagents/*.jsonl` plus
     `meta.json`). No hooks in the MVP.
   - Deduplicate by `message.id`, keeping the last line.
   - Incremental polling every 2 s instead of a file watcher.
   - Line reader capped for lines up to about 1.3 MB.

5. **Command `:agent-usage` (alias `:au`).** No action letter in the MVP.
   Task titles (`meta.description`, `ai-title`) are not displayed. Numeric
   aggregates only, to limit exposure of prompt content.
   - Rejected: showing task titles.

6. **Reference for the 2 % accuracy target.** Claude Code `cost-state` lines
   are the reference; ccusage is the cross-check.

7. **Docker sbx sandbox: story S-08 blocked until spike S-01.** Recommended
   option A: a dedicated host directory mounted as the sandbox's
   `~/.claude/projects`, never all of `~/.claude`. Fallback option C: a hook
   that copies the transcript.
   - Open question: where does `sbx` run? Not decided.
   - Rejected: mounting all of `~/.claude`.

8. **Plane backlog: modules E1, E2, E3 and DEVDESK-39 to DEVDESK-48.**
   "Blocked by" relations are impossible: the API returns HTTP 404 on the
   Community edition. Execution order is written in the item descriptions instead.
