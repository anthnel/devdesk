# Journal

Dated entries, most recent first (AAAA-MM-JJ). Each entry records what was
decided, why, and which alternative was rejected. Sources live in `docs/product/`.

## 2026-11-18 — DEVDESK-577 (NX-08.10): recorded Nexus answers serve as regression tests (D162)

Source: `.claude/plans/DEVDESK-511-plan.md` (read-only), `docs/backlog.md` §1.1 D162. Partial scope: NX-03 answers only. Branch `feat/nexus-recorded-fixtures-devdesk-511`, changes not committed yet.

1. **Decided: the NX-03 answers are regression fixtures in `internal/nexusfixture/fixtures/`, embedded.** A fake Nexus (`fakenexus`, test only) replays them; `nexus-bench record` regenerates them from the bench. A fixture that stales against `internal/nexusbench/descriptor.json` fails `mise run test`. Details and rejected options in D162.
2. **Decided with the user: the pull account's 404 on the detailed group endpoint stays as measured.** The test pins a failed discovery (`unreachable`), not `denied`. Changed by DEVDESK-578 (NX-07b).
3. **Decided with the user: the interface tests contradicted by H1 and IS-07 are annotated, not changed.** Approach left to the user, DEVDESK-579.
4. **Decided with the user: the root `?n=1000` 404 waits for a measure.** No measured answer exists; the skip test activates itself on the fixture. DEVDESK-580 (NX-08b).
5. **Not decided, to settle before NX-05:** whether skip and multi-exchange fixtures are exempt from the stale check.
6. **Open limits, accepted:** the data-set check is anonymous, so a bench with anonymous access cut cannot be recorded at all (NX-05 may need it); exotic encodings are not caught by the leak check; the Basic header reaches a local process on the bench port before the version check; `storedSecrets` duplicates `adminFromStore`.
7. **No ADR.** Test tooling and a dev-tool subcommand, reversible by a revert.

## 2026-11-17 — DEVDESK-506 (NX-03): `_catalog` measured on the bench (no decision)

Source: `docs/measurements/2026-11-17-nexus-catalog.md` (raw responses in `docs/measurements/nexus-catalog/`). Nexus `3.98.1-01` Community, bench `datomir`. Facts are measured; nothing is decided.

1. **H1 contradicted.** `_catalog` does not prefix names with the member repository (`api`, not `docker-hosted/api`). There is no global `/v2/_catalog` on the HTTP port (404); only the group connector on its own port (30482) answers at the root.
2. **H2 confirmed for the bench account.** The `bench-pull` and anonymous accounts get the same full lists as admin. The filtering case stays untested: no account in the bench is restricted to one repository.
3. **Correction of 2026-08-10 (`docs/backlog.md` §3.18).** The detailed repository endpoint refuses the pull account with 404 "Repository not found" on this version, not 403. Anonymous gets 403. The version of the earlier instance is unknown.
4. **IS-07 contradicted for members addressed by host and path.** Each member answers on its own path catalogue, so one walk per member is needed. On the group connector one walk finds all six names, but names carry no member, and `shared/base` resolves to the first member only.
5. **Side effect on the bench.** A HEAD on a manifest through `docker-proxy` cached `shared/base:1.0` there. `nexus-bench:status` reports `dataset: differs (1 image)`. Reset it before NX-04.

Sections to revisit (none decided):
- **IS-03** criteria 2 and 5 (`repo_prefix`, parade R1): the prefix filter keeps nothing on this version.
- **IS-07** criterion 2, `walks.go` and D140 (shared-walk key): one walk per member, not per host; the group needs no per-member split.
- **NX-10**: its start condition (NX-03 contradicts H1 or H2) is met by H1. Whether to rebuild the prefix (`/v2/<prefix>/_catalog`) or use REST search is for the user to decide before NX-10 is promoted.
- **NX-04**: start from a known state (see point 5).
- **NX-05**: group pagination with `n=2` returns duplicates on the group connector; not yet explained.

No ADR. Decision on H1 is the user's.

## 2026-11-11 — DEVDESK-341: footer activity dot instead of the braille spinner (D127)

1. **Decided: work the jobs registry tracks shows the shared activity dot (`●`/`○`) in the footer, not a braille frame.** The view passes the router's blink phase with `SetBlinkPhase`. Scope: the 8 `jobs.FrameMsg` consumers, plus the explorer's load lines (option B). Details and rejected options are in D127 (`docs/backlog.md` §1.1).
2. **Decided: the router sends only the blink.** `jobs.FrameMsg` drops its braille frame and the 100 ms cadence goes, so 2 ticks per second run while jobs run, instead of 10. Source: `.claude/plans/DEVDESK-341-plan.md` (branch `feat/DEVDESK-341-footer-activity-dot`, untracked).
3. **Open, separate stories (since closed, DEVDESK-369 to 377): seven view-owned loading spinners still showed braille and ignored `app.motion: static` and blur, and `:status` had a spinner that was never drawn.** To create in Plane, with the same plan as the source.
4. **Not checked by hand yet:** `ws` sync, `app.motion: static`, losing terminal focus, and an error during a sync.

## 2026-11-11 — DEVDESK-258: `app.theme: auto`, embedded themes (D125)

1. **Decided: `auto` picks `theme_dark` or `theme_light` from the terminal's background (OSC 11).** Detection runs once per process with a 500 ms timeout, then falls back to dark silently. Scope, guard and timing are in D125 (`docs/backlog.md` §1.1). Source: `.claude/plans/DEVDESK-258-plan.md` (branch `feat/devdesk-258-auto-theme`, uncommitted).
2. **Decided: the 12 shipped themes are embedded in the binary.** `dk setup` no longer downloads them. Rejected: leaving the download, which made `github-light` fail on a fresh install. The embedded list is now a durable contract.
3. **Decided: any theme that fails to load gives the default theme and a footer warning.** This replaces the DEVDESK-201 H3 rule that kept the previous palette.
4. **Open, separate story: a context switch does not re-apply the theme.** `handleContextSwitchComplete` is unchanged, and `auto` is not queried again after a switch.
5. **No ADR.** The repository keeps lasting decisions in `docs/backlog.md`. Manual checks on real terminals (light and dark, tmux, Windows Terminal, SSH) are still to do after merge.

## 2026-11-11 — DEVDESK-211: terminal tab progress bar (D123)

1. **Decided: the tab shows the current context's jobs, through OSC 9;4.** Scope, precedence and acknowledgement are in D123 (`docs/backlog.md` §1.1). Rejected: all contexts, which would show a failure `:jobs` does not show. Source: `.claude/plans/DEVDESK-211-plan.md` (main checkout).

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
