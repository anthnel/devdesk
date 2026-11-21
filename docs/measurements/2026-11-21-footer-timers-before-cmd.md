# Why seven `internal/app` tests slept 3 s each on a footer timer, and what removes the wait (DEVDESK-644, S-02a)

Measurement note of 2026-11-21. The fix changes test code only (a `FastTimers` call at the top of seven
tests, a duration guard, comments and documentation). No production file, constant or default is touched:
`components.FooterMsgDuration` stays 3 s. A dated measure is never corrected afterwards: if a later change
moves these numbers, add a new note. The "before" package figure comes from
`docs/measurements/2026-11-21-theme-tests-start-view.md` and is not corrected here.

| | |
|---|---|
| Subject | the 7 tests of `internal/app` named in S-02a ("cause B" of the feasibility note) |
| Machine | 22 logical cores (`nproc`), Linux 6.12 (Debian 13), go1.27.x, `-race` with cgo. Not a dedicated machine: other sessions were working in parallel worktrees |
| Load | `uptime` before each "after" run: 2.04 / 2.34 / 2.03 (race run 1), 7.21 / 3.68 / 2.51 (race run 2), 4.19 / 3.43 / 2.49 (race run 3), 3.20 / 3.31 / 2.51 (no race), 3.19 / 3.27 / 2.53 (`-count=2`). The one-minute average is high at the start of runs 2 and 3 because the previous run's compile and tests had just ended |
| "Before" figures | the 7 tests re-run on the same tree with only the `FastTimers` calls removed (the guard of this change fails them); the package figure is the one recorded after S-01 (`internal/app` about 83 s under `-race`) |

## Cause

`tea.Tick` arms its timer when the `Cmd` is **constructed**, not when it runs (`charm.land/bubbletea/v2`,
`commands.go`: `time.NewTimer(d)` sits outside the returned closure). The expiry of a footer message is such
a tick, built by `components.PostFooter` (`internal/ui/components/footer_message.go:148`) and by
`FooterMessage.set` (`footer_message.go:177`), both with `components.FooterMsgDuration` (3 s).

In the seven tests the `Update` that returns the footer `Cmd` ran before the duration was shortened:
the `drive` helper (`internal/app/context_exec_test.go`) called `testutil.FastTimers` only once it was
handed a `Cmd` that already carried its 3 s timer, and the other tests never called it. `testutil.Msgs`
runs the whole batch it is given, so it slept for the rest of the 3 s timer: 3.00 to 3.05 s per test.

## The fix

- `testutil.FastTimers(t, &components.FooterMsgDuration)` is the second line of each of the seven tests,
  before the `Update` that builds the `Cmd` (`TestErrorFooterPerOperation`, `TestHandlerMapsAFormatErrorToTheInvalidNameText`,
  `TestHandlerMapsAReservedErrorToItsText`, `TestHandlerMapsAnyOtherErrorToFailedToSwitchAndLogsIt`,
  `TestContextLoadErrorPostsFooterAndStaysNotReady`, `TestAFailedBindIsReportedOnScreen`,
  `TestOCISuccessfulRemovalClearsTheFailure`). `drive` keeps its own call, which still covers the `Cmd`s
  built inside it when it feeds a `PostFooterMsg` back through `Update`; its comment now says so.
- `requireNoRealFooterWait(t)` (`internal/app/footer_timer_guard_test.go`) is the first line of the seven
  tests. It fails the test when it ends if it took over 1.5 s, and if `FooterMsgDuration` is not back to the
  value it had at the start of the test after the cleanups. It only catches a real footer wait: the 3 s
  value itself is pinned by `TestAMessageGetsThreeSeconds` in the `components` package.
- `testutil.Msgs` is unchanged; only its comment, rule 128 (`.claude/rules/tui-behavior.md`) and
  `docs/architecture/ui-components.md` now say that the timer starts when the `Cmd` is built and that
  `FastTimers` goes before the `Update` that builds it.

## Before

The 7 tests, `go test -race -count=1 -v -run '<the 7 names>' ./internal/app`, with the `FastTimers` calls
removed: all 7 fail the guard, at 3.00 s to 3.05 s each (3.01, 3.00, 3.01, 3.01, 3.02, 3.00 and 3.05 s,
about 21 s together; 21.2 s for the package run). The package under `-race`: about 83 s (from
`2026-11-21-theme-tests-start-view.md`, after S-01).

## After

### The 7 tests, `go test -race -count=1 -v -run '<the 7 names>' ./internal/app`

All 7 pass: 0.00 s to 0.01 s each, except `TestOCISuccessfulRemovalClearsTheFailure` at 0.04 s. Package wall
time reported by `go test`: 1.25 s. The same durations (0.00 to 0.04 s) appear in the full-package runs.

### The whole package

| Run | Command | `go test` package time | Wall (with build) |
|---|---|---|---|
| race 1 | `go test -race -count=1 ./internal/app` | 57.7 s | 62.7 s |
| race 2 | same | 57.8 s | 58.6 s |
| race 3 | same | 58.3 s | 59.0 s |
| no race | `go test -count=1 ./internal/app` | 37.7 s | 39.8 s |
| `-count=2` | `go test -race -count=2 ./internal/app` | 116.4 s for two passes (about 58 s each), green | 117.1 s |

All runs are green (707 top-level tests, 0 failures). Both bounds of S-02a hold: under `-race` the package
stays under 90 s on 3 consecutive runs (57.7 to 58.3 s; the projection was about 63 s), and without `-race`
it stays under 90 s (37.7 s; the projection was about 38 s). The `-count=2` run is green, so the seven tests
do not depend on files written by a first pass under the shared `HOME`.

No top-level test exceeds 10 s. The three slowest under `-race`, identical across the three runs:
`TestTraceRoundTripKeepsTheScreenHeight` 5.0 to 5.2 s, `TestTheOverlayNeverCutsAnEscapeSequence` 2.2 to
2.4 s, `TestAgentUsagePollingAddsNoAnimationTick` 2.0 s. These belong to other causes of the feasibility note
(S-02c, and the race compute cost left as is) and are not touched here.

### Detection power (criterion 4)

`components.FooterMsgDuration` was set to 2 s in `internal/ui/components/footer_message.go`, then restored with
`git checkout -- internal/ui/components/footer_message.go` (`git status` showed no production file changed
afterwards). `go test ./internal/ui/components` then failed on `TestAMessageGetsThreeSeconds`
(`FooterMsgDuration = 2s, want the three seconds Rule 128 states`); with the 2 s value the seven `internal/app`
tests still pass, since they shorten it to 1 ms anyway. The production value is therefore pinned by
`TestAMessageGetsThreeSeconds` alone, and that test passes unmodified on the restored tree.

### Static checks

`gofmt -l internal/app internal/ui/testutil` empty, `go vet ./internal/app ./internal/ui/testutil` clean,
`golangci-lint run ./internal/app/... ./internal/ui/testutil/...`: 0 issues.
`go test -race -count=1 ./internal/ui/testutil ./internal/ui/components`: ok.

## Limits

- Three runs of the package under `-race`, one without, one `-count=2`: enough for the order of magnitude and
  to check the 90 s bound, not a variance.
- The machine is not dedicated and other worktrees were active; the durations are indicative. The 22-core
  figures are not those of a 4-vCPU CI runner (S-06).
- The "before" package figure is the one recorded after S-01, not a re-run on this commit's parent; the
  "before" figures of the 7 tests are a re-run with the fix removed.
- Not measured: the other slow tests of `internal/app` (S-02b, S-02c).
