# Journal

Dated entries, most recent first (AAAA-MM-JJ). Each entry records what was
decided, why, and which alternative was rejected. Sources live in `docs/product/`.

## 2026-11-20 — DEVDESK-514 (NX-11, task 514.3): IS-16 criteria against the measures (D169)

Source: `docs/backlog.md` D169. Documentation only, no decision, no ADR. Measures frozen: `docs/measurements/2026-11-17-nexus-catalog.md`, `2026-11-20-nexus-cached-only.md`, `2026-11-20-nexus-access.md`. Bench only (Nexus 3.98.1-01 Community); the company instance is not measured.

1. **Contradicted:** IS-03 H1 (prefixed names), IS-07 and D140 (members sharing a catalogue), D161 (403 expected; 404 measured on the bench, 403 for other instances not settled; DEVDESK-578), IS-14 and D137 (`Cached only`: a proxy lists its upstream, not its cache). **Confirmed:** H2, routing by path and by group connector, Bearer realm on the same origin. **Open:** see the « Measured Nexus behavior » section of `docs/architecture/network.md`.
2. **IS-16 criteria:** 1 partial, 2 partial, 3 corrected, 4 corrected, 5 corrected (partial), 6 partial (IS-03 text not in this repository). Details and missing evidence: D169.
3. **Closure of IS-16 and any follow-up task are left to the user.** Nothing is closed or created in Plane.

## 2026-11-20 — DEVDESK-629 (NX-04, NX-05): eight Nexus and registry corrections (D168)

Source: `docs/backlog.md` D168; plan `.claude/plans/DEVDESK-629-plan.md` (not versioned); tasks DEVDESK-630 to DEVDESK-637 (task 629.1 to 629.8) on `feat/devdesk-629-nexus-measure-fixes`. Decided by the user on 2026-11-20 (story DEVDESK-629). The measure notes in `docs/measurements/` are frozen and describe the code before this lot.

1. **Decided: a Bearer realm off the origin is never followed with credentials (F1, option B).** Refused only when credentials are set, with `token realm on another host`; the frozen exceptions are Docker Hub and `registry.gitlab.com` to `gitlab.com`. Self-hosted GitLab stays refused (option C, a per-configuration exception, is a follow-up). Rejected: option A (Docker Hub only, GitLab.com broken with credentials) and following the realm without credentials.
2. **Decided: the 404 `Remote Auto Blocked` of a proxy with a cut upstream is `failed`, temporary (F3).** Detected on the exact JSON prefix, body read up to 4 KiB; any other 404 stays `refused`.
3. **Decided: the timeout reads `no answer within 15 s` (F5).** The delay stays at 15 s; only the wording changes.
4. **Decided: a Docker login of an entry with a path is found by its exact key, then by `host[:port]` as written (F7).** Logout stays on the exact key. Known risk B1, left as is by the user: a login stored for `host` also serves `http://host/path`, a clear Basic over http.
5. **Decided: `Cached only` becomes `May omit images` (F2), carried by the type of each discovered member.** A group always carries it; a proxy when `failed` or `refused`. The type costs up to 32 GET per discovery, 8 at a time. F4: a group that silently omits names is never said `absent`; DevDesk cannot detect it.
6. **Decided: duplicate catalogue names are deduplicated silently (F8).** First appearance order kept; the caps count distinct names.
7. **Decided: the non-200 of `oci.ManifestDigest` is a typed `*oci.StatusError` (F6, DEVDESK-631); the text is unchanged.** Frozen notes that call that error untyped describe the code before this lot.
8. **Observed during implementation:** the security audit found M1 (a `management_url` on another host received the registry host's login); fixed before merge (D168 point 4). B1 is not fixed; the decision is documented in D168.
9. **Not measured:** the logout of a path entry (R6 in the plan) is inferred from the CLI behaviour, not checked against Nexus.
10. **No ADR.** Reversible by a revert; the cache field of point 5 is additive. Number D168 is free on `main` as of 2026-11-20; re-check at merge.

**Proposed follow-ups (not started):**
- A separate task: the realm `query` kept on a non-timeout network error in `fetch token:` (predates this lot).
- A story: refuse Basic over http outside loopback (B1).
- Verify the logout of a path entry (R6); a story if the whole host is logged out.
- After merge: close NX-09 (DEVDESK-512); then NX-11 (DEVDESK-514).

## 2026-11-20 — DEVDESK-579 (NX-12): a group member's catalog is read under its path (D167)

Source: `docs/backlog.md` D167, plan `.claude/plans/DEVDESK-579-plan.md`, measurement `docs/measurements/2026-11-17-nexus-catalog.md` (Nexus 3.98.1-01 Community). Decided by the user on 2026-11-20. Closes the open decisions of the 2026-11-17 entry (H1, IS-07, NX-10) and item 3 of the 2026-11-18 entry.

1. **Decided: a member's catalog is read at `/v2/<prefix>/_catalog`, each name rebuilt as `<prefix>/<name>`.** One walk per member (`walkKey` includes the catalogue prefix; amends D140 points 4 and 9). Rejected: `/repository/<name>/v2/_catalog` (D39 removed it: it finds images that cannot be pulled), the Nexus search API (kept for a restricted account, DEVDESK-513), and changing nothing.
2. **Decided: a discovered group member on a group's connector becomes one search unit, `<alias> (group)`.** Selecting it in the picker is bypassed by design. The suffix is never truncated; a help phrase is shown on `?`. Alternatives `nx/*`, `(all)` and `(members)` rejected. An undiscovered group is unchanged.
3. **Decided: the prefix is validated before any request, and the `Link` next page is followed only on the member's own path.** No new configuration field, no automatic fallback on a 404.
4. **Not measured, accepted:** the `?n=1000` tolerance on a path (inferred, not measured); a 404 of the group with `?n=1000` (criterion 6 test on the group is Skip, DEVDESK-580); upper-case names and prefixes (refused and ignored); a restricted account; other Nexus versions, reverse proxies and HTTPS. The catalogue of a proxy lists the upstream, not its cache, which contradicts the `Cached only` label (NX-09).
5. **Visible effect:** ticking a single bare member now searches the whole group.
6. **Closes:** 2026-11-17, H1 (point 1) and IS-07 (point 4) are settled by the approach above; the NX-10 decision left to the user is taken (the search API stays a separate option, DEVDESK-513). 2026-11-18, point 3: the interface tests are annotated with DEVDESK-579 as the approach reference. Point 4 of 2026-11-18 (`?n=1000` on the root) stays open, DEVDESK-580.
7. **No ADR.** Reversible by a revert; no schema or disk cache change. Number D167 checked on 2026-11-20; a parallel session could take it.

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
