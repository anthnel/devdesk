# CI on GitHub-hosted runners: the full CI against the 10-minute target (DEVDESK-650, S-06)

Measurement note of 2026-11-21. No production file, workflow or repository setting was changed by the
measure: it ran on throwaway pull requests (#447, #448, #449), never merged and closed at the end, cut from
`main` at `9f556e13` (PR #446), which contains S-01, S-02a, S-04 and S-05. A dated measure is never corrected
afterwards: if a later change moves these numbers, add a new note.

| | |
|---|---|
| Subject | the whole `CI` workflow (`Lint and build` + `Test`, in parallel) on `ubuntu-latest` |
| Runner image | `ubuntu-latest` = Ubuntu 26.04.1 LTS, image version `20261116.202.1` (probe job, `echo "$ImageVersion"`) |
| Hardware | 4 vCPU (`nproc` = 4; Intel Xeon Platinum 8370C, 2 cores x 2 threads), 15 982 MB RAM, 3 GB swap |
| Toolchain | gcc (Ubuntu 15.2.0-16ubuntu1) 15.2.0; go1.27.4; Docker 29.4.2 present on the runner |
| Go cache | key `Linux-go-go1.27.4-<hash>-race`, 183 847 171 bytes (about 175 MiB), saved by `actions/cache/save@v5` from `refs/heads/main` at the end of the `Test` job of run 44913651991 |
| mise cache | 126 465 319 bytes, saved by the same run |
| Local pre-check (S-06 criterion 1) | 4 simulated cores: 287 s cold / 240 s warm, recorded before the runner runs |

End-to-end duration is `run_started_at` to `updated_at` of the run (attempt) from the Actions API. Durations
of jobs and steps come from the jobs API (`started_at` to `completed_at`).

## Adaptation of criterion 5 (cold run)

Criterion 5 asks for a first run after a `gh cache delete`. No cache was deleted: the cache holds other
runs' entries and the task forbade touching it. Two existing runs, whose Go cache lookup was a miss
(`Cache not found for input keys: Linux-go-go1.27.4-...-race`), are used as the cold runs instead:

- the `pull_request` run of PR #446 (44912875669), before any `-race` Go cache existed on `main`;
- the `push` run on `main` after the merge of #446 (44913651991), which then saved the Go cache.

The warm runs are the 10 attempts of run 44914241572 (PR #447), all with an exact restore of the Go cache key
saved by the `main` run (`Cache hit for: Linux-go-go1.27.4-ca2185...-race`). Both runs and attempts are the
same commit family, so the 10 attempts test the variation of the environment, not of the code.

## Runs

| Run | Kind | End to end | `Lint and build` | `Test` | Go cache restore | `test-race` | Result |
|---|---|---|---|---|---|---|---|
| 44912875669 (PR #446) | cold | 348 s | 125 s | 344 s | 0 s, miss | 329 s | success |
| 44913651991 (`main` push) | cold | 397 s | 187 s | 393 s | 0 s, miss | 373 s | success |
| 44914241572 attempt 1 | warm | 344 s | 145 s | 339 s | 4 s, exact hit | 324 s | success |
| attempt 2 | warm | 339 s | 167 s | 335 s | 4 s, exact hit | 322 s | success |
| attempt 3 | warm | 285 s | 172 s | 280 s | 2 s, exact hit | 270 s | success |
| attempt 4 | warm | 339 s | 184 s | 334 s | 3 s, exact hit | 324 s | success |
| attempt 5 | warm | 318 s | 134 s | 312 s | 6 s, exact hit | 290 s | success |
| attempt 6 | warm | 338 s | 141 s | 334 s | 3 s, exact hit | 323 s | success |
| attempt 7 | warm | 314 s | 183 s | 308 s | 5 s, exact hit | 286 s | success |
| attempt 8 | warm | 320 s | 145 s | 315 s | 5 s, exact hit | 294 s | success |
| attempt 9 | warm | 292 s | 192 s | 287 s | 3 s, exact hit | 275 s | success |
| attempt 10 | warm | 418 s | 170 s | 413 s | 3 s, exact hit | 402 s | success |

The cold run of PR #446 reports `Lint and build` at 2 min 5 s and `Test` at 5 min 44 s, as seen when it ran.
The `Lint and build` job of the `main` run is 187 s as measured by the jobs API; its docs-only step is skipped
there because the merge touches code. The `Test` job is the longest job in every run, so it sets the end to end
duration.

Warm attempts, 10 of 10: end to end minimum 285 s, median 329 s, maximum 418 s (6 min 58 s). Cold runs: 348 s
and 397 s. The difference between cold and warm is within the spread between attempts (the Go cache restore
saves little: the race test binaries are rebuilt in any case on a changed tree, and the 4 s restore does not
change the order of magnitude).

## Slowest packages (`ok  github.com/anthnel/devdesk/<pkg>`, `Test` job log)

Range over the 10 warm attempts (the two cold runs lie inside the same ranges: agentusage 135 s and 163 s,
oci_resources 138 s and 152 s, scan 146 s and 147 s):

| Package | Time under `-race` |
|---|---|
| `internal/agentusage` | 112 s to 211 s |
| `internal/ui/oci_resources` | 128 s to 171 s |
| `internal/scan` | 145 s to 148 s |
| `internal/ui/agentusageview` | 53 s to 96 s |
| `internal/app` | 53 s to 75 s |
| `internal/ui/netdiag` | 43 s to 71 s |
| `internal/nexusbench` | 23 s to 33 s |
| `internal/ui/keymap` | 18 s to 31 s |

The three slowest packages run concurrently, so the job lasts about as long as the longest of them plus the
packages that do not fit in the 4 vCPUs. The slowest attempt (10) is the one where the three largest packages
were all slow at once (`agentusage` 211 s).

Failures and skips: 89 packages `ok` in every run, 0 `FAIL`, 0 `--- SKIP`.

## Verification of S-05 on two throwaway pull requests

| PR | Change | Result |
|---|---|---|
| #448 `tmp/s06-docs-only` | one empty line at the end of `docs/architecture/network.md` (a `.md` read by tests) | `Test` ran for real (`mise run test-race` success, not skipped), check green. In `Lint and build` the docs-only step ran and `fmt-check`, `vet`, `lint` and `build` were `skipped`, as intended. `mergeStateStatus` `UNSTABLE`, `mergeable` `MERGEABLE` (only `Entire Gates` red, as documented) |
| #449 `tmp/s06-failing-test` | `internal/ui/components/zz_s06_fail_test.go` with `t.Fatal("s06 deliberate failure")` | `Test` red. The job log shows `--- FAIL: TestS06DeliberateFailure`, `zz_s06_fail_test.go:6: s06 deliberate failure` and `FAIL github.com/anthnel/devdesk/internal/ui/components`, with no artifact to download. `mergeStateStatus` `BLOCKED`. The merge was never attempted |

All three throwaway PRs were closed with `--delete-branch`; no `tmp/` pull request is left open.

## External pull request approval setting

`gh api repos/anthnel/devdesk/actions/permissions/fork-pr-contributor-approval` answers
`{"approval_policy":"first_time_contributors"}`: workflows of first-time contributors require approval. This
is not less strict than the level named in criterion 6. It was read, not modified.

## Conclusion

- The target "full CI under 10 minutes" holds for the cold runs (348 s and 397 s) and for the warm runs
  (285 s to 418 s, median 329 s). The slowest run, 418 s, still leaves 182 s.
- 10 warm attempts out of 10 are green, plus the 2 cold runs: no unstable test was seen on these 12 runs
  (89 `ok` packages each time, 0 failure, 0 skip). Criterion 2 is met on one commit family; it measures the
  variation of the runner, not of the code.
- The 5 first warm attempts are all under 10 min (criterion 3).
- Criterion 7 is not triggered: S-08a and S-08b are not started by this measure.
- Limit: a cold run here means a Go cache miss on an existing run, not a run after `gh cache delete`
  (criterion 5 adapted above), and only one runner image (`20261116.202.1`) was observed.
