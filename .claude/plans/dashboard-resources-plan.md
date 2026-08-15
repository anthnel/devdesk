# The dashboard stops reflowing, and gains resource charts

Not started. Supersedes §3.5 of `docs/backlog.md` ("Resource dashboard —
embedded mini-htop"), which asked for per-container braille charts. This plan
keeps that idea but moves it: **the dashboard shows aggregates, the containers
view shows a container**. A row in `containers` *is* a container; the dashboard
has no row to hang one on.

## What was decided

| | Decision |
|---|---|
| Frame | **No outer viewport border.** One titled box per logical group, four of them. |
| Layout | A grid, tiered on the terminal's size. **Text-only boxes left, chart-bearing boxes right.** |
| Skeleton | A section declares its height and fills it. Data changes values, never line count. |
| Tabs | `Overview` and `Resources`. A tab exists only for content that has no view of its own. |
| Overflow | Designed away, not scrolled. The `viewport` stays as a safety net below 26 rows. |
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

## The frame, the groups, the tabs and the tiers

Decided in review, before phase 1 was written. Three questions, and they answer
each other.

### The dashboard is the one view that is not one thing

Every other view is a table or a form, so one border around it is a border
around one object. The dashboard is seven heterogeneous cards, and a single
frame around them says nothing about which value belongs with which. So: **no
outer frame, one titled box per logical group.**

The frame is not the view's to remove — `app/view.go:19` stacks
`renderHeader()` + `renderTitleLine()` (the top border carrying `GetTitle()`) and
`viewportStyle()` (`app.go:97`) draws the other three sides. Drawing boxes inside
the viewport gives a box in a box. So the router grows an opt-out, and its
default — framed — is the safe one, unlike `HeaderView`, whose missing half
renders an empty title in silence. `renderTitleLine` draws a **titled rule with
no corners** for a frameless view (`󰕮 Dashboard ──────`), so `GetTitle()` keeps
a reader and the `HeaderView` contract stays true.

### Four boxes, because the arithmetic says four

`resize()` hands the view `height - 13`: **17 lines on a 30-row terminal**, and
this plan already establishes the budget is overspent.

| Chrome, per column | Lines |
|---|---|
| Today — 4 sections, 3 blank separators | 3 |
| Two stacked boxes — 2 borders each, 1 separator | 5, less the 2 the outer frame gives back → **≈ neutral** |
| Three or four stacked boxes | +4 to +6, on a budget that already truncates in silence |

Boxes are free at four and expensive at seven.

**A box's height is derived from the sections on screen, not declared by the
tier** — decided while wiring it. A constant (6 at `standard`, 8 at `wide`) is a
number phase 3 would have to remember to raise, and the failure is invisible: an
over-long section is truncated inside a box that still looks well formed. The
frame takes the height of its tallest section instead, so a chart added later
simply makes every box on screen taller. What stays fixed is phase 1's actual
contract — a section renders the same number of lines whatever its data.

Growing the boxes to fill a tall terminal was considered and **rejected for
now**: with 24 facts and no charts it trades empty screen for empty boxes. The
slack is phase 3's to spend.

The groups regroup by **question asked**, not by data source — which is what the
current seven sections do:

| Box | Contents | Column |
|---|---|---|
| ` Code` | GitLab session, MRs assigned / to review, issues, workspace count, free space | text |
| `󰒃 Health` | monitors up/down, certificates and nearest expiry, security posture (targets, open CRITICALs, oldest scan) | text |
| `󰒋 Host (Windows)` | CPU and RAM chart, disk, detected tools | chart |
| `󰡨 Docker (VM)` | containers by state, images/volumes, reclaimable, CPU and RAM chart | chart |

Two of those merges carry the argument. **Workspaces joins GitLab**: the explorer
creates what does not exist and workspaces reconciles what does — one subject
seen from both ends. **Tools joins Host**: they are this machine's binaries,
measured by the same probe. That is what takes seven sections to four without
dropping a fact.

### A tab exists only for content with no view of its own

Tabs remove the overflow instead of scrolling it, which is the better answer: a
dashboard that scrolls is not a glance, it is a page. The cost is one line — the
footer goes from 2 to 3 (Rule 124) — leaving **16 lines at 30 rows**, which a
2×2 of six-line boxes fills exactly. So the inner height is a tested constant,
not an estimate.

The trap is a `Health` tab reprinting the monitors and a `Security` tab
reprinting the inventory: `:status` and `:sec` own those rows, and a fourth copy
is a fourth thing to keep in step. Hence the rule in the heading. By it, exactly
one tab joins `Overview` today:

| Tab | Contents | Why it earns itself |
|---|---|---|
| `Overview` | the four boxes, aggregates only | it is the dashboard |
| `Resources` | host and Docker series in detail — braille, RX/TX, disk per volume, reclaimable breakdown, and the tool list | **nothing else shows this**: there is no `:host`, and `:containers` shows a container, not the machine |

The **tool list** is there rather than on the Overview for the same reason: the
Host box answers "can I scan?" with a count, and *which* tool is missing is a
detail of the machine. An undetected tool keeps its line and reads `-`, since
dropping it would read as "there are only four".

**At `wide` the Resources tab is not offered at all** — reported from use, after
a first attempt kept it. The third column already carries its three boxes, so
offering the tab as well puts the same boxes in two places, which is precisely
what a tab is meant to avoid. The rule completes itself: a tab exists only for
content that has no view of its own **and is not already on screen**. Growing a
terminal into `wide` while on Resources returns to the Overview rather than
stranding the view on a tab that no longer exists, `tab` does nothing, and
Rule 130 takes the shortcut off the header.

**The tab *bar* is still drawn at every tier**, showing one tab at `wide`: the
router asks a view for its footer height *before* handing it its new size
(`app.go:205`), so a line that appeared with the tier would lag one resize
behind the boundary that summons it.

### The tier decides where a fact is, never whether it exists

A terminal does not know it is 4K — it knows columns and rows, and two font
sizes on one screen are two terminals. The tier is therefore computed from
`tea.WindowSizeMsg`, on both dimensions separately: width decides the column
count and the chart kind, height decides what fits.

| Tier | Condition | Layout |
|---|---|---|
| `compact` | width < 100 **or** height < 16 | one column, four stacked boxes, one-line sparkline |
| `standard` | ≥ 100 × ≥ 16 | 2×2 grid, block charts, 3 rows — the HD target |
| `wide` | ≥ 180 × ≥ 30 | **three columns**, braille 4 rows, RX/TX inline, detailed breakdown |

The heights are **content lines, not terminal rows**: the view is handed what the
router leaves it — header 9, title line 1, footer 3. Expressing them any other
way would make the view guess at a number the router already knows.

**Only `wide` reads the height, and a test imposed that.** Dropping columns as a
terminal shortens is backwards: fewer columns means more boxes stacked, which
needs *more* height, not less. At twelve content lines a 2×2 loses six lines and
a single column loses twenty-four. `TestTheChosenPalierLosesTheFewestLines`
compares each palier's loss against the alternative's at every height, and it is
what caught the inversion.

That test exists because **the router's viewport does not scroll**: nothing
forwards a key to it, so what overflows is lost silently rather than pushed
below a scrollbar. Calling it a safety net, as this plan first did, was wrong.
Below `gridHeight()` no layout fits and the palier stops choosing the best one
and starts choosing the least bad.

At 240 columns two columns give 118-cell boxes to write `MRs 3 assigned` in;
that is framed emptiness. Three columns of ≈78 are the answer to a 4K terminal.

**And the third column is the `Resources` tab.** That is what makes the tier
system safe: the same content is inline at `wide` and one `Tab` away below it.
Nothing is ever unreachable, and nothing is written twice — a fact that vanishes
at a small size is indistinguishable from a bug, which is the defect phase 1
exists to remove.

Two consequences for the code:

- **One place computes the tier** (`layoutTier(w, h)`); no renderer decides its
  own. Same reasoning as `scan.Categorize` being the only thing that decides a
  finding's family: two tier rules, and the boxes of one grid stop agreeing on
  their height.
- **The sample history belongs to the model, not to the chart.**
  `ntcharts.Resize` rescales its own ring buffer, so a `standard` → `wide`
  change truncates the history at the exact moment the user enlarged the window
  to see more of it. The dashboard keeps N samples and calls `PushAll` after a
  tier change — which means `HostSample` is not only the latest reading.

### Tests this adds

- `TestTheOverviewFitsWithoutScrollingAt30Rows` — the viewport is a safety net,
  not the reading mechanism.
- `TestNoFactIsUnreachableAtAnyTier` — every fact rendered at `wide` is present,
  inline or in a tab, at `compact`.
- `TestOnlyTheDashboardIsFrameless` — the router's opt-out stays a deliberate
  exception rather than a habit.
- `TestATierChangeKeepsTheChartHistory` — pins `PushAll` against the `Resize`
  truncation.

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

**Superseded by the tiers — and there is no scroll to fall back on.** The
router's `viewport` receives no key, so it never scrolls: overflow is silently
cut. The overflow therefore has to be designed away rather than absorbed.

A box costs `theme.BoxChrome` = 3 lines (two borders plus the blank line under
the title), so a two-row grid needs `gridHeight()` = 18 content lines, i.e. a
31-row terminal. `TestTheOverviewFitsAtTheHeightItNeeds` pins that number and
fails if the arithmetic and the documentation drift apart.

Never one viewport per column: two independently scrolling columns means two
cursors and a reading position that cannot be described.

### Tests

- `TestEverySectionKeepsItsHeightWhateverItsState` — renders each section
  unknown / loaded / unavailable and compares line counts. Without it, the first
  section added afterwards reintroduces the reflow.
- `TestAnUnavailableSourceKeepsItsLabels` — a section with no Docker still
  prints its rows.
- `TestAnUnmeasuredValueIsNotZero` — `-` and `0` are distinguishable.

---

## Phase 2 — the metrics — **done**

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

**The package keeps no state, and that was decided by Rule 110.** A rate needs
the previous cumulative reading, and holding it in the package would mean a
`Cmd` mutating shared state — two overlapping samples would then compute a rate
against the wrong instant. So `SampleHost(prev Counters) (HostSample, Counters)`
takes the previous counters and returns the next: the state travels on the
message, the model stores it, and `Update` is still the only writer.

`docker.FetchAggregateMetrics()` sums the running containers' CPU and memory
shares on top of the existing `GetContainerMetrics`.

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
- `TestLoadAverageIsDisplayedNowhere` — pins the Windows trap so nobody adds it
  back on the strength of it "working on Linux". **It parses imports rather than
  grepping text**: a grep would forbid the comments that explain the decision,
  which are the only thing keeping it alive.
- `TestEachClockRearmsItself` — a clock that stops after one tick leaves frozen
  values that look like values.
- `TestTheModelKeepsTheSampleHistoryBounded` — the history belongs to the model,
  and keeps its newest end.

### What was dropped, and what replaced it

`calculateDiskUsage` (`du -sh` over the workspaces tree) is **gone**, with
`WorkspaceStatsMsg.DiskSize`: an unfilled field reads as a value someone forgot
to display. The tree's own size is therefore no longer shown anywhere; free
space on its volume is, at 1 ms instead of seconds, and it is the question the
dashboard was actually being asked.

---

## Phase 3 — the charts — **done**

`internal/ui/dashboard/chart.go` wraps `ntcharts` so the view never touches it
directly, and so `Draw()`/`DrawColumnsOnly()` cannot be chosen at a call site.

**The chart keeps no history, and that turned out to settle two problems at
once.** It is rebuilt from the model's samples on every frame, so there is no
`Push` to place in `Update` — Rule 110 holds by construction rather than by
review — and the `Resize` truncation the tier section warns about cannot happen,
because ntcharts never owns the history it would rescale.

A chart **replaces a blank line** at `standard` rather than adding one: the
grid there is exactly 16 lines, so a chart that grew the box would make the
overview scroll. At `wide` it takes four lines and braille.

At 4K the right column is ≈118 cells: a braille chart of 100 cells holds 200
samples, over three minutes of history. That is a graph, not an ornament.

**At `wide` the three chart boxes hold the first row and share one order** —
CPU, its curve, RAM, its curve — so the curves fall on the same lines from one
column to the next and can be compared without hunting for which is which. It
also keeps a row from mixing a two-curve box with a text box, which would leave
the taller of the two padding the shorter.

**The chart height is measured, not derived.** `fitCharts` renders the grid once
with no curve at all, and what remains between that skeleton and the available
height is what the curves may occupy. Constants describing the text heights were
right the day they were written and wrong the moment a box gained a line — which
happened to Health the same day it became a tree. The floor is one line, not
three: a braille curve wants three to beat a sparkline, but forcing three where
two fit overflows the grid, and what overflows is lost rather than pushed down.

The chart kind follows the layout tier — it is not a second tier system, and
`layoutTier(w, h)` is the only thing that decides:

| Tier | Chart |
|---|---|
| `wide` | braille, 4 rows |
| `standard` | blocks, 3 rows |
| `compact` | one-line sparkline |

At `compact` the chart-bearing boxes stack under the text ones rather than
crushing both into half a terminal.

### Tests

- `TestAChartLineIsExactlyTheColumnWidth` — Rule 116.
- `TestEveryChartCellCarriesABackground` — Rule 115, the `DrawColumnsOnly` trap.
  **It forces the colour profile for the duration of the test**: `go test` has no
  TTY, so lipgloss degrades to Ascii and drops every escape — a test looking for
  styling without that would pass with `DrawColumnsOnly` too. Confirmed to fail
  when the call is swapped.
- `TestNothingCallsDrawColumnsOnly` — the same invariant from the source side,
  on calls rather than on text, so the comment explaining the trap may name it.
- `TestAChartIsRebuiltFromTheModelRatherThanKeptByTheWidget` — Rule 110: two
  renderings of one model are identical and rendering does not touch the model.
- `TestAChartDoesNotGrowTheBoxAtStandard` — the 16-line budget.

---

## Phase 4 — what the dashboard starts saying — **done**

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

### What the wiring cost

**The Health box lost its blank separator to the expiry line**, and the test is
what decided it: a seventh row took the grid to 19 lines against the 17 a 30-row
terminal leaves, so `TestTheOverviewFitsWithoutScrollingAt30Rows` failed. The
box is six rows — Monitors, Certs, Expiry, Scanned, Critical, Oldest — because
the budget is six.

**Three distinctions that each needed their own state**, and each is a test:

- an *unread* posture is not an *empty* one — the caches unread print `-`, an
  empty cache prints a measured `0`;
- an entry with **no timestamp does not become the oldest scan**: `time.Time`'s
  zero precedes everything and would make a scanned inventory read as never
  scanned;
- SSL monitors none of which could be **read** print `-`, not a distant expiry.

**`docker system df` already carried the reclaimable figure**; only the format
string had to ask for it. The percentage Docker attaches to each size is
relative to its own family and is dropped — it would not survive the sum. The
total is formatted with `docker.formatSize`, in Docker's decimal units rather
than binary ones, because the number exists to be compared with what
`docker system df` prints.

**Deferred: free space on the Docker root.** It needs `docker info` for the path,
and on Docker Desktop that path lives inside the VM, so `disk.Usage` cannot
reach it from the host — an extra call that would read `n/a` on this machine
every time.

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
