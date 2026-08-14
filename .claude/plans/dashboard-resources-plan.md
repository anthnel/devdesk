# The dashboard stops reflowing, and gains resource charts

Not started. Supersedes §3.5 of `docs/backlog.md` ("Resource dashboard —
embedded mini-htop"), which asked for per-container braille charts. This plan
keeps that idea but moves it: **the dashboard shows aggregates, the containers
view shows a container**. A row in `containers` *is* a container; the dashboard
has no row to hang one on.

## What was decided

| | Decision |
|---|---|
| Layout | Two columns. **Left is text only, right is text plus a chart.** |
| Skeleton | A section declares its height and fills it. Data changes values, never line count. |
| Overflow | The whole content scrolls in one `viewport`. Not one per column. |
| Host metrics | `shirou/gopsutil/v4` |
| Charts | `NimbleMarkets/ntcharts` **v0.5.1**, not v2 |
| Bubble Tea v2 | **Out of scope.** Separate phase, after this ships. |

## The facts this plan rests on

Everything below was measured on the development machine (Windows 11, 16
logical cores, 33.4 GB, Docker Desktop, 9 containers of which 2 running) on
2026-08-14. Nothing here is quoted from documentation.

### The sections do not keep their height today

`render*Section` builds a line count that depends on state:

| Section | loading | loaded | other |
|---|---|---|---|
| GitLab | 3 | 7 | 4 (not connected) |
| OCI | 3 | 6 | 3 (no Docker) |
| Tools | 3 | 2+N | — |

`View()` stacks the sections, then equalises the two columns on the taller one.
So **a value landing in the right column moves the left column too**. That is the
reflow, and it is structural rather than incidental.

The vertical budget is already exceeded: the left column is ≈22 lines, the right
≈20, and the viewport gets `height - 11`. At a 30-row terminal, 17 lines remain
and the rest is cut **with nothing on screen saying so**. At 4K it is
comfortable — hence the scroll rather than a shrink.

### `gopsutil/v4@v4.26.7` — runs, and lies once

Built with `CGO_ENABLED=0`, so no C toolchain (same constraint that picked
`zalando/go-keyring`). Eight indirect dependencies, all pure Go.

```
cpu.Percent(500ms) -> 9.18 %                          501 ms  ← blocks for the interval
mem.VirtualMemory  -> 92.0 % used, 31.0 / 33.4 GB       0 ms
net.IOCounters     -> cumulative counters               5 ms
disk.Usage("C:\\") -> 78.9 % used, free 210.8 GB        1 ms
host.Info          -> Windows 11 Pro 25H2, uptime 4822s 1 ms
load.Avg           -> {0, 0, 0}   err=<nil>             0 ms  ← ⚠️
```

Three consequences, each of which changes the code:

- ⚠️ **`load.Avg()` returns zeroes with a nil error on Windows.** It does not
  fail; it produces a number that looks like data. A dashboard cannot show it.
  **Load average is not displayed on any platform** — a metric that exists on
  two of three is worse than one that exists on none, because its absence reads
  as "idle".
- `cpu.Percent(interval, …)` **blocks for the interval**. The periodic sampler
  uses `cpu.Percent(0, false)`, which computes since the previous call.
- `net.IOCounters` is **cumulative**. The rate is a delta between two samples,
  and the first sample after a start or a resume has no rate at all — it prints
  `-`, not `0`.

`disk.Usage` at 1 ms is the best value-per-millisecond in the whole dashboard,
and it is the metric this view is missing today.

### `ntcharts` — the version is the whole question

`@latest` is v2, and v2 requires `charm.land/bubbletea/v2` + `lipgloss/v2`.
DevDesk is on bubbletea v1.3.10 / lipgloss v1.1.0 / bubbles v0.21.0. **`go get
github.com/NimbleMarkets/ntcharts` would drag the whole application into Bubble
Tea v2.**

`v0.5.1` (March 2026) is the last of the v1 line and was compiled and run
against our exact versions:

```
sparkline 40×1, blocks                  ← the inline case
▄▅▆▇▇▇▇▇▇▇▇▆▅▃▂▂▁▁▁▁▁▁▁▂▃▄▅▆▇▇▇▇▇▇▇▇▆▅▃▂

sparkline 40×4, braille                 ← 80 samples in 40 cells
             ⣀⣀⠤⠒⠒⠒⠒⡄                 ⢀
          ⣀⠔⠉       ⠈⢆            ⢀⡠⠔⠊⠁
        ⢀⠜            ⠑⠢⢄⡀       ⡠⠃
⠉⠉⠒⠒⠤⠤⠤⠒⠁                ⠈⠑⠒⠢⠤⠤⠔⠊
```

MIT. Adds two direct dependencies (`ntcharts`, and `bubblezone` for the
linecharts' mouse support).

Three properties that matter to the house rules:

- **`Draw()`, never `DrawColumnsOnly()`.** `Draw()` styles the whole canvas, so
  a style carrying `Background(theme.ColorBackground)` fills the troughs.
  `DrawColumnsOnly()` styles only the columns and lets the terminal's native
  background show through the gaps — precisely the defect Rule 115 forbids.
- **Every rendered line is exactly `width` cells** (verified: `len=40`,
  `len=48`). Nothing to re-pad, and Rule 116's arithmetic is unaffected.
- **The model owns its ring buffer** (`Push`, `PushAll`, `Resize` rescales).
  No history to write by hand — but `Push()` belongs in `Update()`, never in a
  `Cmd` (Rule 110).

Fixed scale for percentages: `WithMaxValue(100)` + `WithNoAutoMaxValue()`. The
network keeps the default auto-scale, because its ceiling is unknown.

### `docker stats` costs two seconds

```
docker stats --no-stream :  1365 ms  /  1982 ms  /  1993 ms
docker system df         :   603 ms
```

A 2-second tick calling `docker stats` would keep the CLI running continuously.
This is what forces **three clocks** rather than two.

### Where each number is measured

`workspaces_dir` is `C:\Users\anthoni\workspaces`, so `dk.exe` runs natively on
Windows. gopsutil therefore reports Windows; `docker stats` reports usage
*inside* the Docker Desktop VM, whose own footprint is a subset of the Windows
totals.

The two are both true and **not additive**. So: two sections, never one shared
axis, and the label names the measurement point — `Host (Windows)` and
`Docker (VM)`, not `Host` and `Docker`.

Running `dk` *inside* WSL is the misleading case: gopsutil would read the
distro's `/proc` — the VM, not Windows — and Docker Desktop's containers live in
a different distro, so they would appear nowhere. Detectable (`/proc/version`
contains `microsoft`) and to be **stated in the section label**, not hidden.

---

## Phase 1 — the skeleton stops moving

### The rule

**A section declares its height and fills it.** The section table is the
mechanism, on the model of `configuration/fields.go`: one declaration per
section carrying its icon, title, height and renderer, rather than seven
`render*Section` methods each deciding their own size.

### Three value states, and they are not two

| State | Rendered | Meaning |
|---|---|---|
| unknown | `-` in `DimStyle` | not measured yet |
| unavailable | `n/a` + one dim line saying why | Docker absent, GitLab not connected |
| zero | `0` in `DimStyle` | measured, and it is zero |

Today "Docker not available" **replaces** the block. With a skeleton the labels
stay and the values read `n/a`: the user sees what the dashboard would show,
which is itself information. This also retires the per-section `Loading...`
string — there is nothing left for it to say.

### The consequence nobody asks for

Once a stale value is visually identical to a fresh one, **the age becomes
mandatory**. `m.lastRefresh` is stored today and never rendered; it goes in the
footer via `theme.TimeAgo` (Rule 127). This is not decoration — it is what the
placeholder scheme costs.

### Scroll

One `viewport` over the whole content. `bubbles/viewport` is already the house
idiom (`containers`, `netdiag` ×2, `security`), so this introduces no new
mechanism.

Not one viewport per column: two independently scrolling columns means two
cursors and a reading position that cannot be described.

### Tests

- `TestEverySectionKeepsItsHeightWhateverItsState` — renders each section
  unknown / loaded / unavailable and compares line counts. Without it, the first
  section added afterwards reintroduces the reflow.
- `TestAnUnavailableSourceKeepsItsLabels` — a section with no Docker still
  prints its rows.
- `TestAnUnmeasuredValueIsNotZero` — `-` and `0` are distinguishable.

---

## Phase 2 — the metrics

`internal/metrics`, a package of its own, deliberately not inside `docker/` or
`ui/`. It owns sampling and rate computation; the view owns display.

```go
type HostSample struct {
    CPUPercent  float64
    MemPercent  float64
    MemUsed     uint64
    MemTotal    uint64
    NetRXPerSec float64   // computed from the delta; zero-valued on the first sample
    NetTXPerSec float64
    HasRate     bool      // false on the first sample — the view prints `-`
}
```

`disk.Usage` is queried for the workspaces volume and the Docker root, and stays
on the slow clock: free space does not move in a second.

**`calculateDiskUsage` (`du -sh` over the workspaces tree) leaves the periodic
refresh.** It runs on every refresh today, and it is the one call whose cost
grows with the user's data. `disk.Usage` at 1 ms answers the useful question
(how much room is left); the tree's own size becomes on-demand.

### Three clocks

| Clock | Contents | Measured cost |
|---|---|---|
| ~1 s | gopsutil host sample | ≈ 5 ms |
| ~5 s | `docker stats` aggregate | ≈ 2000 ms |
| `status.refresh_interval` (≥ 30 s) | GitLab, `system df`, workspaces, tools | ≈ 600 ms + network |

Each is its own `tea.Cmd` returning its own message. The existing
`RefreshTickMsg` keeps the slow clock and is not otherwise touched — a single
tick driving all three is what makes the expensive call follow the cheap one.

`ctrl+r` fires all three.

### Tests

- `TestTheFirstSampleHasNoRate` — a cumulative counter read once yields no rate,
  and the view must not print `0`.
- `TestARateSurvivesACounterReset` — an interface reset makes the delta
  negative; the sample is dropped, not rendered as a negative throughput.
- `TestLoadAverageIsNeverDisplayed` — pins the Windows trap so nobody adds it
  back on the strength of it "working on Linux".

---

## Phase 3 — the charts

`internal/ui/dashboard/chart.go` wraps `ntcharts` so the view never touches it
directly, and so `Draw()`/`DrawColumnsOnly()` cannot be chosen at a call site.

At 4K the right column is ≈118 cells: a braille chart of 100 cells holds 200
samples, over three minutes of history. That is a graph, not an ornament.

Width tiers, one definition:

| Column width | Chart |
|---|---|
| ≥ 60 | braille, 4 rows |
| 30–59 | blocks, 3 rows |
| < 30 | one-line sparkline |

Below the tier where the right column can still hold a chart, it stacks under
the left column rather than crushing both.

### Tests

- `TestAChartLineIsExactlyTheColumnWidth` — Rule 116.
- `TestEveryChartCellCarriesABackground` — Rule 115, the `DrawColumnsOnly` trap.
- `TestSamplesArePushedInUpdateOnly` — Rule 110.

---

## Phase 4 — what the dashboard starts saying

Ordered by value per unit of cost:

| Added | Source | Cost |
|---|---|---|
| Free space, workspaces volume and Docker root | `disk.Usage` | 1 ms |
| Reclaimable Docker space | `docker system df`, **already fetched and discarded** | 0 |
| Security posture — targets scanned, open CRITICALs, oldest scan | `ImageScanCache` + `WorkspaceScanCache` | cache read (Rule 126) |
| Nearest certificate expiry | `serviceComponents`, already held | 0 |
| Age of the last refresh | `m.lastRefresh`, stored and unused today | 0 |

Two of these are free in the strict sense: the reclaimable figure is already in
the `system df` output the view parses and throws away, and the certificate
components are already in memory. The security posture finally connects the
dashboard to §3.11's inventory without running a single scan.

**Deferred**: workspace hygiene (`3 dirty, 2 behind` from §3.17's git status).
The data is right but the walk cost grows with the number of repositories, and
it belongs on the slow clock at best. Out of this plan.

**Rejected**: listening-port count. `ss` needs a privileged container; too
expensive for a view that refreshes itself.

---

## Not in this plan — Bubble Tea v2

v2 is GA (bubbletea v2.0.8, lipgloss v2.0.6, bubbles v2.1.1) and v1 is not
deprecated. **The migration is not required for anything here** — ntcharts
v0.5.1 delivers sparklines, braille and multi-series stream charts on v1, which
was verified by running it.

It is deliberately not bundled, for one reason above the others: in v2
`msg.String()` returns `"space"` instead of `" "`, and there are **10 `case " ":`
in 10 files**. The compiler says nothing — `case " ":` stays valid Go, it simply
stops matching. Rule 135 makes `Space` the only key allowed to tick a checkbox,
so a careless migration silently breaks every checkbox in the application. That
failure must surface in a pull request that does nothing else.

Measured surface, for whoever picks it up:

| Work | Extent | Nature |
|---|---|---|
| Import paths → `charm.land/*/v2` | 154 files / 301 | mechanical |
| `tea.KeyMsg` → `tea.KeyPressMsg` | 96 | mechanical, compiler-found |
| `View() string` → `View() tea.View` | 11 views + the router | **architecture** |
| `lipgloss.Color` as a type → `color.Color` | 46, nearly all in `theme/colors.go` | one file |
| `WithWhitespaceBackground` → `WithWhitespaceStyle` | 13 | mechanical (Rule 115) |
| `AdaptiveColor` / `TerminalColor` | 0 | nothing — no `compat` needed |

The house rules paid for themselves here: Rule 119 (no hex outside `colors.go`)
reduces lipgloss v2's largest change to a single file, and `datatable` rendering
its own rows (Rule 122) blunts the `bubbles/table` changes.

The one real piece of architecture: the router types views as `tea.Model`
(`app.go:47`), which in v2 would force all 11 views to return `tea.View`. The
better answer is the inverse — DevDesk declares its own `View` interface
(`Init` / `Update` / `View() string` plus the four `HeaderView` methods) and only
`*App` stays a `tea.Model`.

And ntcharts v2 is not yet a reason to hurry: its `go.mod` carries
`replace charm.land/bubbletea/v2 => github.com/neomantra/bubbletea/v2` with the
comment *"Awaiting upstream merges"*. Migrating for it today would trade a
stable v1 stack for a dependency on a fork.

Suggested order when it happens, in its own branch, with no functional change:
imports → `KeyPressMsg` → the router's `View` interface → `WithWhitespaceStyle`
→ `theme/colors.go` → **then the 10 `case " "`, with a contract test pinning
"Space ticks"**, on the model of `TestEveryViewSuppliesItsHeaderAndHelp`.

---

## Order, and why

1. **Phase 1 first, and it ships alone.** The fixed skeleton is visible on its
   own and is what makes the height of the content knowable — which is what a
   clean `viewport` needs. Doing it after the charts would mean measuring
   sections that are still moving.
2. **Phase 2 before phase 3.** A chart with no samples cannot be judged; a
   sample with no chart is a number, and prints fine.
3. **Phase 4 last**, because none of it is blocking and most of it is free.

Writing the dashboard on v1 now adds roughly one file to migrate later, against
a router refactor absorbed immediately. The trade is heavily in favour.

## Coverage

New packages (`internal/metrics`, `internal/ui/dashboard/chart.go`) carry unit
tests from the start. `dashboard_test.go` and `view_test.go` exist (818 lines
together) and the section-height contract lands there.

The rate arithmetic (deltas, counter resets, first sample) is pure and needs no
Docker, no network and no terminal — it is the part worth testing hardest, and
the part where a wrong answer is least visible on screen.
