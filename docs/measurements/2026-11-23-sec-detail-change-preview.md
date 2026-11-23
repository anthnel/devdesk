# Cost of the change preview of the `:sec` finding detail (DEVDESK-753, H6)

Measurement note of 2026-11-23. It backs the decision on H6 of the plan ("is a loading state needed?").
A dated measure is never corrected afterwards: if the computation changes, add a new note.

| | |
|---|---|
| What is measured | `computeMisconfigFix(target, finding, scannedAt)` (`internal/ui/security/misconfig_change.go`): the whole of what the Cmd of the detail runs: `FixFor`, the lexical and link checks of the path, two `os.Stat`, the file read, the catalog's edits, `patch.Rewrite`, `patch.Diff`, the formatting of the lines, `scope`, and `git.StateOf` (a `git` process) |
| Fixture | the largest change of the catalog: `AVD-DS-0002` on a 3-line Dockerfile (a user, a group and a switch added: 11 lines of diff). `largestChangeFixture` in `details_change_perf_test.go` picks it by counting lines |
| Without git | `BenchmarkComputeMisconfigFix`, in a directory that is not a git repository: `go test -run xxx -bench BenchmarkComputeMisconfigFix -benchmem ./internal/ui/security` |
| With git | a throwaway test, not kept: `git init`, `git add`, `git commit` in a temporary directory, then 101 calls of `computeMisconfigFix`, sorted; median is the 51st, p95 the 96th |
| Machine | Intel Core Ultra 7 155H, 22 threads, Linux 6.12 (Debian 13), Go 1.27.4, git 2.47.3, warm page cache, local disk, nothing else measured at the same time |

## Results

| Case | Median | p95 | Max | Allocations |
|---|---|---|---|---|
| not a git repository (the benchmark) | 1.04 ms | | | 124 KB, 456 allocs per call |
| not a git repository (101 calls) | 1.02 ms | 1.34 ms | 1.71 ms | |
| tracked and clean file in a git repository (101 calls) | 3.80 ms | 4.28 ms | 5.89 ms | |

`TestThePreviewOfTheLargestChangeIsFarUnderTheBudget` checks the median of 15 runs of the Cmd itself against
100 ms, so a computation that became slower by two orders of magnitude fails the suite without being flaky on a
loaded runner.

## Decision

The threshold of H6 was 100 ms at p95: the measure is 25 to 100 times under it. The computation is nonetheless
run by a Cmd, never in `Update` or `View` (Rule 110), because it reads the disk and starts `git`, and a file on
a network share or a repository on a slow disk can take much longer than this machine does. For that case, and
for the cell between the key press and the answer, the detail already shows `Change: preparing the preview...`
and the footer says `Preparing the change preview...` with the activity dot (`WantsBlink`). That costs one line
of status and removes any doubt about a slow disk, so it was kept even though, on this machine, the answer
arrives before the next frame.

## Limits

One machine, one fixture, a warm cache. `git.StateOf` is the part that scales with the repository (index size,
a slow filesystem); it was not measured on a large repository. The 100 ms budget of the test is a smoke alarm,
not a performance claim.

## Addendum, same day: after the git hardening

`git.StateOf` now runs with `-c core.fsmonitor=false -c core.hooksPath=<null>` and `GIT_OPTIONAL_LOCKS=0` (see
`docs/architecture/scanning.md`, "The Change section"). Same machine, same fixture and same method (101 calls in a
throwaway test): tracked and clean file in a git repository, median 3.68 ms, p95 4.22 ms, max 4.95 ms; the benchmark
without git, 1.02 ms and 469 allocs per call (the file is now opened and measured on its descriptor). The change is
inside the noise of the figures above, which stay as they were measured.
