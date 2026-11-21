# Measurement: one test run (`-race`) in `mise run ci` (S-04, DEVDESK-648)

Measurement note of 2026-11-21. A dated measurement is never corrected afterwards: a later run goes in a new file.
The code change of the same commit is the removal of the `test` step from `[tasks.ci]` in `mise.toml`; the figures
below were taken from the worktree that carries it, but the measured command (`go test -race -count=1 ./...`) is
independent of `mise.toml`.

Reading convention: **Measured** = observed in this session, output at hand. **Reported** = taken from
`docs/product/ci-tests-rapides/measures.md` (the "before" measurement, same machine, same day, not repeated here).
**Inferred** = computed from the figures, not executed.

## Conditions

- Machine: 22 logical cores (`nproc`), Linux 6.12 (Debian 13), go1.27.4 linux/amd64, gcc 14.2.0, `CGO_ENABLED=1`.
- Load average just before the "after" run: 0.80 (1 min), 0.69 (5 min), 0.72 (15 min). During the run, samples of
  `uptime`: 4.66 and 5.00 in the first two minutes (the run's own compilation and the first packages), then 1.51,
  0.83 at the end. Another agent works on the same machine on another branch; no heavy competing process was seen
  in `ps` at the start (top processes: `kube-apiserver`, `caretta`, `kubelet`, `containerd`, all under 9 % CPU).
  The machine was therefore close to idle: the run is comparable to the "before" runs (load 0.30 to 4.2 there).
- Build cache: the shared `GOCACHE` of the machine, not emptied; whether every instrumented object was already cached was
  not checked, so the 344.1 s may include a little compilation (30 s for a cold instrumented build in `measures.md`).
  `-count=1` forces every test to execute.
- No `-v`, no `-json`: the output is the plain `go test` output.

## Inventory of tests that only run without the race detector (criterion 2)

Command, run at the root of the worktree:

```
git grep -nE 'go:build.*race|race\.Enabled|!race' -- '*.go'
```

Result: **no match** (exit status 1). No file carries a `//go:build` line that mentions `race`, and nothing reads
`race.Enabled`. Hypothesis H5 of `feasibility.md` ("no test depends on the absence of the race detector") is
**confirmed for the build tags and the `race.Enabled` constant**. Limit: the grep cannot see a test that behaves
differently under `-race` without naming it (a timing assumption, for instance); the full run below passes with
the detector on, which is the only evidence for those.

## Missing C compiler (criterion 6)

`test-race` sets `CGO_ENABLED=1` in `mise.toml` and runs `go test -race ./...`. Reproduced in a throw-away Go module
created by `mktemp -d` (one package, one test), outside the repository, with `gcc` removed from `PATH` (a `PATH`
holding only a symlink to `go`):

```
# runtime/cgo
cgo: C compiler "gcc" not found: exec: "gcc": executable file not found in $PATH
FAIL	example.com/tmp/pkg [build failed]
FAIL
```

Exit status 1. With `CC=/nonexistent` the message names that compiler instead:
`cgo: C compiler "/nonexistent" not found: exec: "/nonexistent": stat /nonexistent: no such file or directory`.
The failure is plain and names the missing compiler: nothing is skipped or masked. The behaviour is Go's, not
DevDesk's; the `test-race` description in `mise.toml` already says "requires a C toolchain: gcc/clang".

## A failing test without `-v` (criterion 3)

Same throw-away module, a test that calls `t.Errorf("expected 1, got 2")`, command `go test -race ./...` (no `-v`):

```
--- FAIL: TestAlwaysFails (0.00s)
    a_test.go:5: expected 1, got 2
FAIL
FAIL	example.com/tmp/pkg	0.009s
FAIL
```

The test name (`TestAlwaysFails`), the package (`example.com/tmp/pkg`) and the failure message come out without
`-v`, and exit status is 1, so `mise run ci` stops there. In a real run the package line also carries the import
path of the DevDesk package.

## Before and after on the same machine (criterion 5)

| | Command | Wall time | Source |
|---|---|---|---|
| Before, `test` step | `go test -v ./...` (via `mise run test`), 22 cores | 303.4 s | Reported (`measures.md`, run 3) |
| Before, `test-race` step | `go test -race ./...`, 22 cores | 328.6 s | Reported (`measures.md`, run 2) |
| **Before, both test steps of `ci`** | | **632.0 s** (303.4 + 328.6) | Inferred (sum) |
| **After, one `-race` run** | `CGO_ENABLED=1 go test -race -count=1 ./...`, 22 cores | **344.1 s** | Measured, exit status 0 |

Detail of the measured run: 89 packages `ok`, 0 `FAIL`, 2 packages without test files (91 in total, as in
`measures.md`). Slowest packages (the `ok` line of each): `internal/app` 327.3 s, `internal/scan` 144.8 s,
`internal/ui/oci_resources` 132.1 s, `internal/agentusage` 110.7 s. `internal/app` alone is 95 % of the wall time
and sets the floor, as `measures.md` found.

Reading:

- The test part of `mise run ci` goes from about 632 s to 344 s: 288 s less, a 46 % reduction. The story expected
  about 303 s less (632 s to 329 s); the measured gain is 15 s smaller because this `-race` run took 344.1 s where
  `measures.md` recorded 328.6 s.
- **The 316 s threshold (half of 632 s) is NOT met by S-04 alone**: 344.1 s is 28 s above it (and 15 s above the
  328.6 s expected). As the story foresees, the threshold is to be re-evaluated once S-01 (the `internal/app` theme
  tests, which wait on real ten-second ticks) and S-02a are merged; `internal/app` is 327 s of the 344 s.
- Run-to-run spread: the two `-race` runs of the same suite differ by 15.5 s (328.6 s, 344.1 s). A single "after"
  sample cannot say more than that the figure is in the 330 to 345 s range on this machine.
- Not measured: the whole `mise run ci` (the four checks before the tests are not part of this comparison, as in
  `measures.md`); a machine with fewer cores (the 2-core simulation of `measures.md` gave 529.8 s for `-race`
  against 832 s for both runs; not repeated here).

## What this does not show

- That nothing lost its coverage: the non-race run is gone, so a test that only fails without `-race` would no
  longer be caught. None is known (no `race.Enabled` or `!race` file), and the race build runs the same test code.
- Stability over several runs: one run only, exit status 0.
