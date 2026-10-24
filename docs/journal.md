# Journal

Dated entries, most recent first (AAAA-MM-JJ). Each entry records what was
decided, why, and which alternative was rejected. Sources live in `docs/product/`.

## 2026-10-24 — agent-usage-monitor: spike S-01 result (DEVDESK-39)

Source: `docs/product/agent-usage-monitor/spike-s01.md`. Recommendation: **go with
changes**. Supersedes item 6 of the entry below.

1. **Reference for the 2 % accuracy target: ccusage, at session level.** Reverses
   the `cost-state` reference of item 6.
   - Why: `cost-state` is 19 to 22 % above the transcripts, per model rather than
     per agent, and is written only when the session closes. ccusage 20.0.43 matches
     the deduplicated transcript sum at 0.00 % on the 3 sessions measured.
   - Rejected: `cost-state` as the reference.

2. **ccusage gives no total per sub-agent.** The gap is confirmed. Its JSON entries
   carry no agent id, and `--by-agent` means per CLI, not per sub-agent.

3. **Sub-agent output tokens are a lower bound.** On the last line per `message.id`,
   `stop_reason` is null in 94 % of cases (Claude Code 2.1.302), and `output_tokens`
   is a streaming placeholder. The effect is at most 0.3 % of total tokens: ranking
   by tokens holds, but the cost per agent is under-estimated.

4. **Story changes validated.**
   - S-02: output is a lower bound, optional fields, synthetic fixture for an unseen
     version. Criterion 5 becomes: session total (sum of agents) within 2 % of ccusage.
   - S-03b and S-04: a sub-agent's "Out" is shown as partial (`≥`). Sort and share
     stay as specified.
   - S-06: tool calls are counted from the transcript (`tool_use` blocks), not from
     the completion notification.
   - S-07: billed session total plus an "Unattributed" remainder.

5. **Open question: `sbx` transcript visibility is not verified.** `sbx` is not
   installed on this machine. S-08 stays blocked until the criterion 2 commands in
   the spike are run on the host that runs `sbx`.

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
