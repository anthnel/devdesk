# Agent usage monitor — brief (draft)

## Idea (one sentence)
A dedicated DevDesk view that shows, live, what AI agents (Claude Code sessions,
orchestrator and sub-agents) consume.

## Decisions so far
- Metrics wanted: tokens (in/out/cache), estimated cost ($), quota / usage-limit
  progress, activity (tool calls, elapsed time, current state per agent).
- Audience: the DevDesk user, live during long sessions AND afterwards (history).
- Today: built-in `/usage`, `/cost`, statusline — no per-sub-agent breakdown.
- Data source: undecided (transcript JSONL, Claude Code hooks, OpenTelemetry) —
  to settle in research / feasibility.

- Scope: Claude Code only, local sessions of this machine, sub-agents included.
- Success: spot the most expensive agent; truly live (no command to rerun);
  comparable history across sessions/features; see the quota limit coming.

## Open questions
- Data source (JSONL / hooks / OTel) and what each can actually provide
  (quota data in particular).
- Persisted history: where, how long, how large.
- MVP cut: which of the four metrics ships first.

## Research & critique outcome (2026-10-24)
- No existing tool combines live view + per-sub-agent split + DevDesk integration
  (see research.md). Quota is only available via the statusline JSON (Pro/Max).
- User account: Max/Pro -> rank agents by tokens; $ is indicative only.
- Sessions run both on host and in `sbx` sandboxes (50/50).

## MVP (validated)
Live per-agent / per-sub-agent token breakdown for Claude Code sessions on the host,
computed from transcripts (no persisted history).

- Tree orchestrator -> sub-agents: role (agentType), model, tokens (in/out/cache),
  state, tool-call count, duration; sort by tokens; indicative $.
- Out of scope (v2+): quota 5h/weekly, persisted history, per-feature comparison,
  other agent CLIs, cloud sessions, OpenTelemetry.
- Known limit: sandbox (`sbx`) sessions; transport solution to study in feasibility.
- Guardrails: tolerant parser (unknown fields, dedupe message.id), fixtures dated,
  numeric aggregates only (never prompts / tool output).

## Success metrics
- Most expensive agent identified in < 5 s on a multi-sub-agent session.
- Live lag < 3 s on an active session.
- Per-agent totals within 2% of ccusage on 3 reference sessions.
- Parser survives a transcript of another version, with visible degradation.
- First feasibility task: run ccusage on a real session to confirm the gap.
