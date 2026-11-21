# Why the auto-theme tests of `internal/app` slept 10 s each, and what removes the wait (DEVDESK-643, S-01)

Measurement note of 2026-11-21. The fix changes test code only (the `autoConfig` helper, plus one guard
test). No production file, constant or default is touched. A dated measure is never corrected afterwards:
if a later change moves these numbers, add a new note.

The baseline measure (`docs/product/ci-tests-rapides/measures.md`) recorded the origin of the 10 s delay as "not identified"; this note supplies the cause and does not contradict any of those figures.

| | |
|---|---|
| Subject | the 14 tests of `internal/app` named in S-01 ("cause A" of the feasibility note) |
| Machine | 22 logical cores (`nproc`), Linux 6.12 (Debian 13), go1.27.x, `-race` with cgo. Not a dedicated machine: another session was working in a parallel worktree |
| Load | `uptime` before the "after" runs: load average 0.77 / 0.68 / 0.72 (run b), 0.80 / 0.69 / 0.72 (run c, rising to 7.9 while the race binary compiled) |
| "Before" figures | taken from the 2026-11-21 baseline measure (`measures.md` of the CI story), not re-run: about 243 s for these tests, `internal/app` 326.8 s under `-race` (301.9 s without) |

## Cause

Not `testutil.Msgs`, and not the theme logic: `Msgs` runs the commands it is given, synchronously, and one
of them was a real 10 s timer.

The path, element by element (paths relative to the repository root, lines as of this commit):

1. A router built with `app.theme: auto` starts with the theme detection pending.
   `newWithSize` sets `app.theme.startInitDeferred = app.theme.pending`
   (`internal/app/app.go:307`), so `App.Init` does not run the start view's `Init`: it waits for the
   terminal's answer (or the timeout).
2. The tests then settle the theme: an answer (`tea.BackgroundColorMsg`, handled at
   `internal/app/theme_auto.go:199`) or the timeout (`themeDetectTimeoutMsg`, `theme_auto.go:209`; the
   timeout itself is `themeDetectTimeout = 500 * time.Millisecond`, `theme_auto.go:19`). Both call
   `settleTheme` (`theme_auto.go:215`).
3. `settleTheme` ends with `a.runDeferredStartInit()` (`theme_auto.go:223`). That function
   (`theme_auto.go:231`) clears `startInitDeferred` and returns `view.Init()` for the view on screen
   (`theme_auto.go:236-238`): the **deferred `Init` of the start view**, batched into the `Cmd` the test
   receives.
4. The start view is picked by `defaultView(cfg)` (`internal/app/app.go:325`): `dashboard` when
   `app.default_view` is empty. `testConfig()` is `config.Default()` with an empty forge URL
   (`internal/app/harness_test.go:76-80`), so the start view was the dashboard.
5. `dashboard.Model.Init` (`internal/ui/dashboard/model.go:212`) batches, among about a dozen `Cmd`s,
   `m.scheduleRefresh()` (`model.go:219`), `m.scheduleHostTick()` (1 s) and `m.scheduleDockerTick()` (5 s).
6. `scheduleRefresh` (`internal/ui/dashboard/model.go:657`) is `tea.Tick(interval, ...)` with
   `interval = refreshInterval`, and a floor: a value under 10 s becomes 30 s
   (`model.go:658-661`). `refreshInterval` is `Status.RefreshInterval` in seconds
   (`model.go:207`), which defaults to 10 (`internal/config/config.go:626` for a loaded file,
   `config.go:727` for `Default()`). The config therefore cannot shorten it: 10 s is the minimum.
7. The tests drain the `Cmd` with `testutil.Msgs` (`internal/ui/testutil/testutil.go:169-192`), which
   runs every command of a batch in turn and returns when all have answered. `Msgs` therefore blocks until
   the longest tick of the batch fires: the 10 s refresh tick.

The same dashboard batch also does real I/O (docker calls, disk reads, `du`), which these tests had no
reason to run.

### Why 10.09 s and not 10.00 s

`tea.Tick` arms its timer when the `Cmd` is **constructed**, not when it runs
(`charm.land/bubbletea/v2@v2.1.1/commands.go:154-155`: `t := time.NewTimer(d)` sits outside the returned
closure). The three ticks of the batch (1 s, 5 s, 10 s) therefore overlap instead of adding up, and the wait
is the longest of them, 10 s. The 0.05 to 0.09 s on top is real work: building the router, and the
batch's I/O `Cmd`s, run one after another by `Msgs` once the earlier ones have returned.

### The fix, and why it is a test-helper change

`autoConfig` (`internal/app/theme_auto_test.go:38`) now sets `cfg.App.DefaultView = "workspaces"`. It builds
the config for both `autoRouter` and `autoRouterWith`, so every auto-theme router starts on a view whose
`Init` (`internal/ui/workspaces/update.go:20`, `tea.Batch(m.loadEntries(), loadScanCacheCmd())`) carries no
timer. `themeDetectTimeout`, `Status.RefreshInterval` and the dashboard's 10 s floor are unchanged. A
production seam for the floor was rejected: it would still leave the dashboard's real docker calls in
these tests.

Residual risk, written down rather than discovered: a future test that builds an auto router from a raw
`testConfig()` brings the 10 s wait back without any noise. Two defences: the comment on `autoConfig`, and
the guard `TestAutoThemeHelpersDoNotStartOnTheDashboard`
(`internal/app/theme_auto_start_view_test.go`), which fails if `autoConfig`, `autoRouter` or
`autoRouterWith` yields a dashboard start view. The guard does not see a test that bypasses the helpers.

## Before

From the baseline measure, not re-run (the commit before this one still has the dashboard as start view):

- the 14 tests: about 243 s together (each 10.09 s per settled-theme case; the three slowest, 60.5 s,
  40.4 s and 30.3 s, hold 6, 4 and 3 such cases);
- `internal/app` as a whole: 326.8 s under `-race` (22 cores), 301.9 s without.

## After

### (b) The 14 tests, `go test -race -count=1 -v -run '^(<the 14 names>)$' ./internal/app`

All 14 pass. Package wall time reported by `go test`: 2.308 s (3.0 s with the process start). Per test:
0.01 s for eight of them, 0.02 s for two, 0.05 s, 0.07 s, 0.43 s and 0.59 s for the others. The largest
single test is 0.59 s, below the 1 s bound, and the cumulative duration is about 1.3 s, below the 10 s
bound (against about 243 s before). The two slowest
(`TestANamedThemeChosenInSessionReplacesAutoAndIsKept`,
`TestChoosingAutoInSessionReusesTheCachedAnswerWithoutAskingAgain`) were not diagnosed here; both stay under
1 s.

The red guard `TestAutoThemeHelpersDoNotStartOnTheDashboard` passes (0.01 s, three subtests).

### (c) The whole package, `go test -race -count=2 ./internal/app`

Green on both passes (`ok`, 166.4 s for the two passes, so about 83 s per pass; 22 cores, load average
about 3 to 8 during the run because of the compile and the parallel worktree). The `-count=2` run
passes, so the tests do not depend on files written by a first pass under the shared `HOME`. About 243 s
less than the baseline per pass (326.8 s to about 83 s) is consistent with the figures above; the rest of
the package's time (about 83 s) belongs to the other causes of the feasibility note (S-02a, S-02b,
S-02c), which this change does not touch.

### Detection power (criterion 5)

A deliberate regression of the theme fallback was added to production code, then removed. In
`warnTheme` (`internal/app/theme_auto.go:243`), each warning was posted twice
(`components.PostFooter(components.LevelWarning, t)` appended two times per text). The 14 tests then ran:
6 failed (`TestAnInvalidUnusedCandidateWarnsAtStartup`, `TestBothCandidatesInvalidWarnOnceEach`,
`TestAnInvalidThemeDarkAndADarkAnswerWarnOnce`, `TestAnInvalidThemeDarkAndATimeoutWarnOnce`,
`TestAnInvalidThemeLightAndALightAnswerWarnOnce`,
`TestThemeAutoCandidateThatCannotBeUsedGivesDefaultAndOneWarning`), 8 passed. The regression was reverted
completely with `git checkout -- internal/app/theme_auto.go`; `git status` showed no production file
changed afterwards. The fast tests still catch a regression of the warning count, which is the case
named in the story.

### (d) Static checks on `internal/app`

`gofmt -l internal/app` empty, `go vet ./internal/app` clean, `golangci-lint run ./internal/app/...`:
0 issues.

## Limits

- One run per configuration for (b) and (c): enough to show the order of magnitude (about 243 s to about
  1.3 s), not a variance.
- The machine is not dedicated and the parallel worktree was active; the durations are indicative.
- The "before" figures are the baseline's, not a re-run on this commit's parent.
- Not measured: the other slow tests of `internal/app` (S-02a, S-02b, S-02c); the CI runner's cores.
