# Why six `internal/app` tests ran the scanners of the host, and what stops it (DEVDESK-645, S-02b)

Measurement note of 2026-11-21. The fix adds one seam to a production file
(`var detectTools = scan.Detect` in `internal/app/scan_tools.go`, called by `detectScanTools`) and replaces it
in the `TestMain` of `internal/app`. The production behaviour is unchanged: the seam holds `scan.Detect`
unless a test binary replaces it. A dated measure is never corrected afterwards: if a later change moves
these numbers, add a new note.

| | |
|---|---|
| Subject | the tests of `internal/app` that play `App.Init` ("cause C" of the feasibility note) |
| Machine | 22 logical cores (`nproc`), Linux 6.12 (Debian 13), go1.27.x, `-race` with cgo. Not a dedicated machine: other sessions were working in parallel worktrees |
| Load | `uptime` at the start of each run: 1.09 / 1.04 / 1.01 then 4.62 / 1.87 / 1.28 (before, the first reading is a few seconds before the run), 3.60 / 1.77 / 1.26 (after, race), 1.94 / 1.64 / 1.25 (`-count=2`) |
| "Before" figures | the same tree with the guard test of the change (`TestInitDoesNotRunTheScannersOfTheHost`, red) but without the seam: `go test -race -count=1 -v ./internal/app` |

## Cause

`App.Init` starts `detectScanTools`, whose `Cmd` called `scan.Detect(tools)` directly. A test that runs the
`Cmd` returned by `Init` (through `testutil.Msgs` or `drive`) therefore executed `trivy --version`,
`gitleaks --version` and `plumber --version` from the `PATH` of the machine, plus the engine probe. The
answer, and the duration, depended on what was installed: on a workstation that has the tools, about 1.3 s
per test; on a runner that does not, a different code path.

## The fix

- `detectTools` (`internal/app/scan_tools.go`) is a package variable holding `scan.Detect`, in the style of
  `containerForward` and `proxyForward`. `detectScanTools` calls it. Nothing else changes in production.
- `TestMain` replaces it with a detection that answers an empty `scan.Report` (an empty `Tools` map, engine
  absent), never non-test code. The production value is read before in `productionDetectTools`, a package
  variable of a `_test.go` file, which is initialised before `TestMain` runs.
- `TestDetectToolsIsScanDetectInProduction` compares the function pointers of `productionDetectTools` and
  `scan.Detect` (the real wiring), and checks that the test binary does hold something else.
- `TestInitDoesNotRunTheScannersOfTheHost` (written first, red before the seam) puts decoys of the three tools
  first on `PATH`; an executed decoy leaves a witness file.

## Inventory of the readers of `scanToolsDetectedMsg` (criterion 2)

`grep -rn scanToolsDetectedMsg internal/app`:

| Site | Kind | Result used |
|---|---|---|
| `internal/app/scan_tools.go` | type definition, and the `Cmd` that builds it | production |
| `internal/app/app.go:598` | the `Update` case (`handleScanToolsDetected`) | production |
| `internal/app/scan_tools_test.go:14` | the `detected(gen)` helper | a controlled report (`trivy` available) |

The consumers in tests (`TestADetectionReachesEveryView`, `TestAStaleDetectionIsDropped`, the other tests of
`scan_tools_test.go`) call `detectScanTools()` without running its `Cmd` and feed `detected(gen)` back to
`Update`: none relies on the real detection. Only `Init` reached it. The hypothesis of the story holds, and
no test needed to be kept on the real detection.

## Before

The six tests of the story (`go test -race -count=1 -v ./internal/app`, guard red, no seam):

| Test | Before | After |
|---|---|---|
| `TestNothingRunsTheStartViewInitTwice` | 1.41 s | 0.02 s |
| `TestCtxDashAlternatesBetweenTwoContexts` | 1.30 s | 0.02 s |
| `TestPreviousContextIsNeverPersisted` | 1.32 s | 0.02 s |
| `TestWithoutAutoInitRunsInAppInitAndNoAnswerRerunsIt` | 1.36 s | 0.01 s |
| `TestSwitchViewDuringTheWaitIsNotInitialisedAgainBySettling` | 1.31 s | 0.02 s |
| `TestReinitializeViewsDuringTheWaitIsNotInitialisedAgainBySettling` | 1.35 s | 0.03 s |
| **Total** | **8.05 s** | **0.12 s** |

The guard `TestInitDoesNotRunTheScannersOfTheHost` fails before the seam (0.08 s) because the decoys run.
The package under `-race`: 58.6 s (`go test` package time, one run, with `-v`).

## After

| Run | Command | `go test` package time |
|---|---|---|
| race | `go test -race -count=1 -v ./internal/app` | 38.3 s, green |
| `-count=2` | `go test -race -count=2 ./internal/app` | 75.6 s for two passes (about 38 s each), green |

Other checks: `go test -race -count=1 ./internal/ui/security ./internal/scan` green (5.8 s and 144.8 s);
`gofmt -l internal` empty, `go vet ./internal/app` clean, `golangci-lint run ./internal/app/...`: 0 issues.

The package gained about 20 s, more than the 8 s of the six tests. Not verified test by test: the likely
reason is that the other tests that play `Init` or a context switch paid the same `--version` calls for
less than 1 s each, which the per-test figures above do not isolate. The two runs are also not under
identical load.

## Limits

- One run of each, not a variance; the machine was not dedicated, so the figures are indicative. A 4-vCPU
  runner will differ (S-06).
- The "before" run includes the compile of the package and ran with the guard test red, which adds nothing
  measurable (0.08 s).
- `TestPasteIntoCommandPaletteReachesTheField` (1.07 s, two subtests of 0.53 s) is slow before and after: it
  is not a detection test and is left to S-02c.
- Not measured: a machine without the three tools (the point of the story is that the result no longer
  depends on it; the decoy test checks it).
