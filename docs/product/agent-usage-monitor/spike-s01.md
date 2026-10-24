# Spike S-01 (DEVDESK-39): ccusage gap, sandbox, format drift

2026-10-24, this machine (Linux, Node 24.22.1). Structure and numbers only: no transcript content was read into this page.
Tool: **ccusage 20.0.43** (npm `latest`, published the same day), via `npx -y ccusage@20.0.43 claude session --id <uuid> --json --breakdown --offline`. The network was available (npx download). Pricing came from the offline cache.

## Criterion 1: does ccusage give tokens per sub-agent?

Three finished sessions with `subagents/`, all on 2.1.302. "Own" = sum over parent + `subagents/agent-*.jsonl`, keeping the **last** line per `message.id` (map shared by the whole session), with `<synthetic>` excluded. Totals = in + out + cache-read + cache-write.

| Session | Sub-agents | Msgs | Per sub-agent? | ccusage total | Own total | Δ own vs ccusage | `cost-state` total | Δ own vs cost-state (total / output) |
|---|---|---|---|---|---|---|---|---|
| d9acf4b8 (small) | 4 | 77 | **No** | 5,690,886 | 5,690,886 | 0.00 % | 7,060,896 | −19.4 % / −66.7 % |
| 8bc69d30 (medium) | 14 | 143 | **No** | 15,659,603 | 15,659,603 | 0.00 % | 20,048,361 | −21.9 % / −58.7 % |
| aa37ed75 (large) | 32 | 631 | **No** | 68,172,620 | 68,172,620 | 0.00 % | 84,286,088 | −19.1 % / −66.1 % |

- **The gap is confirmed.** ccusage returns one row per session, plus a per-model breakdown with `--breakdown`. Its JSON `entries` carry no agent id. `--by-agent` on the top-level `session` command means *per CLI* (every row is `agent: "claude"`), not per sub-agent. ccusage does read the sub-agent files and counts them in the session total: its entry count equals the number of `message.id`s.
- **Dedup.** ccusage matches last-line dedup on every field (0.00 %). Keeping the first line instead loses 0 %, −3.3 % and −23.6 % of `output_tokens`. A naive sum inflates the total ×1.52 / ×1.80 / ×1.95.
- **Reference for the 2 % rule: ccusage**, at the session level (it has no per-agent figure to compare against). `cost-state` cannot be the reference:
  - it is 19 to 22 % above the transcripts, structurally;
  - it is per model, not per agent;
  - it is only written when the session closes (34 of 36 files have it only in their last 5 lines; it is missing from the live session).
- **Root cause (new, affects S-02/S-07).** On sub-agent messages, the last line per `message.id` has `stop_reason: null` (94 % on 2.1.302, 80 % on 2.1.283), and its `output_tokens` is a streaming placeholder (median 8 tokens).
  - The completion notification's `totalTokens` minus the last message's context gives a real output of several hundred tokens for that last message. Sub-agent **output is therefore a lower bound**, in ccusage too.
  - `cache_read` is also 24 to 32 % higher in `cost-state`, with no matching `api-request` line (79 requests for 77 messages): those API calls are not in the transcripts.
  - Cache-write agrees within 0.7 to 3.2 %. Input tokens differ a lot in relative terms but are negligible in absolute terms. Output weighs ≤ 0.3 % of total tokens, so **ranking agents by tokens is unaffected**; cost per agent is.
- A per-agent signal exists in the parent (`attachment.queued_command.usage {totalTokens, toolUses, durationMs}`). But it is linked to its agent only through the notification text (content), and `totalTokens` is the final context size, not a cumulative total. **Not usable.**

## Criterion 2: `sbx` transcripts visible from the host: **not verified**

`sbx` is not installed on this machine, and the user chose to leave this point open. To run **on the host that runs `sbx`** (not executed here):

```bash
sbx ls                                                    # pick <name>, a sandbox running claude
sbx exec <name> sh -c 'ls -t "$HOME"/.claude/projects/*/*.jsonl | head -1; mount | grep -i claude'
# on the host, <sid> = the file name printed above, without .jsonl
find ~/.claude ~/.devdesk /mnt/c/Users/*/.claude -name '<sid>.jsonl' 2>/dev/null      # WSL/Linux
Get-ChildItem -Recurse -Filter '<sid>.jsonl' "$env:USERPROFILE\.claude" -ErrorAction SilentlyContinue  # PowerShell
```

What to observe and write down:
1. Is the file found on the host? Give its path or "not found".
2. If found, does its size grow while the agent works (two `stat` calls 10 s apart), and is there a `subagents/` directory next to it?
3. From `mount`, is `/home/agent/.claude` a sandbox-private volume? That is what option A (a dedicated transcripts mount) needs to know.

## Criterion 3: format drift, 2.1.283 (2026-09-27) to 2.1.302 (2026-10-24)

Corpus: 130 parent files and 92 sub-agents; 2.1.283 / .284 / .285 / .302. Version is **per line**: one session spans 2.1.285 → 2.1.302.

- **`usage`: additive only.** Nothing removed or renamed. New in 2.1.302: `output_tokens_details.thinking_tokens`, `service_tier`, `speed`, `inference_geo`, `iterations`, `fallback_credit`. All are **nullable** (null on some lines). `cache_creation.ephemeral_5m/1h` and `server_tool_use.*` are present in both versions.
- **Line types.** `api-request` and `api-request-shape` appear in 2.1.302. Sub-agent lines gain `requestedModel`, `requestRef`, `thinkingDurationMs`, `thinkingDisplay` and `attribution*`. 1,086 sub-agent lines carry `agentId` but no `isSidechain`, so filter by **file location**, not by that flag.
- **Tree.** `subagents/agent-<17 chars>.jsonl` + `.meta.json` in every version. 2.1.302 adds a sibling `tool-results/` directory (5 of 8 sessions), which discovery must ignore.
- **`meta.json`.** `finished` and `launchedWithDefaults` are missing in 2.1.283/285, and in 1 of 92 on 2.1.302: state "unknown". `spawnedWithWorktree`, `worktreeBranch` and `worktreePath` are optional (a path: do not display it).
- **The version spread is insufficient**: 19 patch releases of a single minor, all additive. The synthetic "other version" fixture is **still required** for S-02. Include: a renamed or missing `usage` field, an unknown line type, and a `meta.json` without `finished`.

## Recommendation: **go with changes**

The gap is real: no tool splits by sub-agent. The data is there for input and cache, and ranking holds. The changes:
- **S-02**:
  - criterion 5 = session total (Σ agents) within 2 % of `ccusage claude session --id`;
  - flag sub-agent `output_tokens` as a lower bound when `stop_reason` is null;
  - decode the nullable `usage` fields as optional.
- **S-03b/S-04**: mark sub-agent "Out" as partial (e.g. `≥`). The sort and the share stay as specified.
- **S-07**: per-agent cost under-estimates output. Once `cost-state` exists, show the billed session total and an "Unattributed" remainder.
- **S-06**: count `tool_use` blocks from the transcript; do not use the notification.
- **S-08 stays blocked** until criterion 2 is run on the `sbx` host.
