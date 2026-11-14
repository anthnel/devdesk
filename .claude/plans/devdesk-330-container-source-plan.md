# DEVDESK-330 (S-05a) — Path tab: a running container as the source (Docker Engine)

Base: `origin/main` = `65b47296` (PR 402, DEVDESK-329: `pathStep`, `pathSteps`, route via
`netcheck.Env.Route` on `pathResolvedMsg`). The main checkout was still on `e096ef7c` when this
plan was written; every file below was read from `origin/main`.
View: `internal/ui/netdiag` (`path_*.go`). Engine reads: `internal/docker`.
Out of scope: Docker Desktop (S-06), Podman, overlay, Kubernetes, named VMs, the bridge row
(S-05b), WSL labels (S-07), failure reading (S-09).

## Blocking questions (for the orchestrator to ask the user)

**Q1 — Which key opens the source selector?** (blocks step 6 only; steps 1–5 do not depend on it)

The Path tab is in `pathEditing` most of the time, and there the target field takes every
printable key, `↑↓ pgup pgdown home end` move the table (DEVDESK-329 criterion 4), `←→` move the
caret and `tab` switches tab. No letter and no structural key is free in that state.

| Option | Works while editing | Cost |
|---|---|---|
| **A. A declared Ctrl exception on the Path surface, `ctrl+o` ("origin") (recommended)** | yes | One entry in `keymap.exceptions` (`TestOnlyThreeCtrlCombinationsSurvive` requires it). `ctrl+o` is not bound by `textinput` (checked in bubbles v2.2.1) and is already vetted in this codebase (raw mode disables the tty's discard). It already means "write" on `security/results/remediation-tab`: same key, other surface, other meaning. |
| A'. Same, `ctrl+t` | yes | No reuse across surfaces; no mnemonic; SIGINFO on BSD/macOS when not in raw mode (it is in raw mode). |
| B. A lowercase `s` declared on a `netdiag/path` surface, live only outside editing | no: only in `pathDone` | The first trace of a session can only come from this machine; choosing a container first means trace, then `s`, then trace again. |
| C. A `Source` cycle field next to `Target` in the bar (Rule 132) | needs a focus move | `↑↓` (Rule 135, between fields) already scroll the table on this tab; `←→` move the caret. Breaks DEVDESK-329 criterion 4 or Rule 135. Rule 132 also reserves a dropdown, not a cycle, for long lists (containers can be many). |

Recommendation: A (`ctrl+o`). The plan is written for A; with A' only the constant and the
exception entry change; with B, step 6 adds a `localToggles` surface instead and refuses the key
while editing.

**Q2 — Where is the manual acceptance run?** (does not block coding; blocks the PR's manual check)

This workstation runs Docker Desktop (measured below), which S-05a refuses by design (S-06 owns
it). Criteria 1–4 and 6 can only be checked by hand against a Docker Engine on Linux.
Options: (a) the Debian 13 machine used for S-01, with `docker-ce` (installed if absent)
— **recommended**; (b) `docker-ce` inside a WSL distro with Desktop integration off for it;
(c) automated tests only, plus a manual check of the refusal on Docker Desktop.

## Acceptance criteria (Plane DEVDESK-330)

1. Docker Engine: the source selector offers this machine and the running containers.
2. Container selected: the column starts with the container and its network, before this machine.
3. Container on several networks: the column names the network of the default route, or
   `undetermined` when the engine cannot tell.
4. Container in host mode: the column says it shares this machine's stack and adds no bridge.
5. No running container: only this machine is offered, and a message says why.
6. The column says the hops after this machine are traced from this machine, not from the container.
7. The engine reads start no container.

## Hypothesis H8 of the brief, checked: "container network, IP and gateway are read by the existing engine inspection"

**Partly true.** The engine answers it, the existing functions do not:

- `docker.InspectContainer(id)` (`internal/docker/containers.go`) returns the **whole** `docker
  inspect` JSON, untyped, with stdout and stderr merged (`dockerCombined`), no context, and its
  error carries the raw output. The whole JSON also carries `Config.Env`, i.e. the container's
  secrets. Not reusable as is for a view that parses and logs.
- `docker.InspectNetwork(id)` lists the containers of a network (name, IPv4, MAC): no gateway, no
  priority, wrong direction.
- `docker.ListContainers(all)` gives ID, name, image, state (`templates().PS`), without a context.

Measured on this workstation (read-only `docker ps` and `docker container inspect --format`, no
container started; engine 29.9.2, API 1.56, Docker Desktop 4.96.3): the fields exist with these
names and types — `HostConfig.NetworkMode` (`"bridge"`), `State.Running` (bool),
`NetworkSettings.Networks.<name>.{GwPriority (int, 0 by default), Gateway, IPAddress,
IPPrefixLen}`. `docker info --format '{{.OperatingSystem}}'` answers `Docker Desktop` (and
`{{.Name}}` `docker-desktop`), which is the Desktop signal used below. moby's `EndpointSettings`
documents `GwPriority`: the endpoint with the highest value provides the default gateway; ties
are broken by sorting on network name (direction not stated, not measured). docs.docker.com says
the default gateway "is selected by Docker, and may change whenever a container's network
connections change". `GwPriority` exists from API 1.48 (Docker 28); older engines omit it.

So: **one new typed read**, `docker.InspectContainerNetworking(ctx, id)`, with a narrow
`--format` that selects only the four fields above (no `Env`, no labels, no mounts in memory), plus
context variants of the listing and an engine OS read. Nothing else in `internal/docker` changes.

## Options compared

### Where the container's network is read

| Option | For | Against |
|---|---|---|
| **A. In the trace producer, after the route, carried by `pathResolvedMsg` (recommended)** | Same moment as the route and the trace (D145 pattern); "trace again" reads it again; no new message, `pathChanCap` unchanged; I/O in the Cmd (Rule 110) | ~100 ms more before the first rows; one more seam field |
| B. When the container is picked, stored on the model | Rows could show before launch | Stale by the time of the trace; a second message type and generation; a picked container that stops is shown as attached |
| C. Read the container's routing table, `/proc/<pid>/net/route` from `State.Pid` | Answers criterion 3 exactly, without `GwPriority` | Linux-only, permission-dependent on another user's process, unmeasured, wrong under rootless/userns; out of S-05a's effort |

### What the selector is

| Option | For | Against |
|---|---|---|
| **A. A list overlaid on the Path table, palette variant of Rule 112 (recommended)** | Rule 112 "selector" row: anchored under the header, view visible around, holds the keyboard, no buttons; the body stays the table (Rule 139); `internal/ui/overlay.Compose` exists | A small new component (~150 lines) in `netdiag` |
| B. Reuse the router's `renderPickerOverlay` (`internal/app/context.go`) | Exists | Unexported in `app`, no scrolling, no width bound, replaces the whole screen (`lipgloss.Place`) |
| C. A cycle field in the bar (Rule 132) | No overlay | See Q1, option C |

### How Docker Desktop and Podman are kept out

| Option | For | Against |
|---|---|---|
| **A. Engine name, then `info` `OperatingSystem`, before listing (recommended)** | Podman refused with no exec; Desktop recognised by what the engine says (measured: `Docker Desktop`); one read-only call | One call per selector opening |
| B. `docker context` endpoint inspection | Also catches remote engines | Several cases (`DOCKER_HOST`, contexts, ssh, tcp); does not catch Desktop under WSL integration (`default` context on `/var/run/docker.sock`) |
| C. Let it through, S-05b's bridge check decides later | No code now | S-05a would draw a Desktop container's network as passing through this machine: false |

## Decisions (made here; record in docs, D146 by the archivist)

- **D-a** The source is a value of the Path tab (`pathSource`, default `this machine`), chosen in
  an overlay list opened by `pathSourceKey`. It is read by the launch, not by the trace engine:
  **the probes always leave from this machine** (criterion 6).
- **D-b** Listing runs in a Cmd: engine name (no exec) → `info` OS → `ps` (running only). Podman:
  refused with no exec. Desktop: refused after `info`. Unreachable or absent engine: only this
  machine. The reason is a fixed US English note in the footer, the raw error goes to the log.
- **D-c** The container's attachment is read **in the producer** after the route lookup, with
  `InspectContainerNetworking`, and rides on `pathResolvedMsg` (option A above). Failure is not
  fatal: rows read `undetermined`, the trace runs.
- **D-d** Three new row kinds, ordered by `pathSteps` only: `container`, `network` (before
  `source`), and `trace` (after `gateway`, before the hops) saying where the hops come from.
- **D-e** Default-route network: never guessed. Candidates are the endpoints with an IPv4 gateway;
  one candidate → it; several → the unique highest `GwPriority` when the engine reports it on
  every candidate; otherwise `undetermined (N networks)`.
- **D-f** Engine reads are read-only by construction: `ps`, `info`, `container inspect` only, each
  with a context and a timeout; IDs are validated (`docker.IsContainerID`) and passed as argv
  elements, never names. Two guards pin it (step 1 and step 7).

## Design hypotheses (prudent defaults; flag in the PR, none blocks)

- **H1 Key**: `ctrl+o` (Q1), active in `pathEditing` and `pathDone`; in `pathRunning` it warns
  `A path trace is running — esc cancels it` (Rule 130); greyed with the platform reason where the
  trace is unsupported (`launchable()`).
- **H2 Selector**: opens at once with `this machine` and a loading footer (`Reading running
  containers...`, shared activity dot, Rule 128), then fills. Re-read at every opening (no cache).
  Holds the keyboard: `↑↓ pgup pgdown home end` move, `enter` chooses, `esc` closes, every other key
  (including `tab`) is swallowed; a paste is dropped (no field, Rule 111). The cursor starts on the
  source in use, marked `(current)`. Title `Trace from` in the top border, border
  `theme.ColorPaletteBorder`, `IconCircleSmall` on the highlighted row (Rule 120), width
  `clamp(width-4, 20, 60)`, centred, anchored on the first body line.
- **H3 Entries**: `this machine` first, then running containers (state `running` only: paused and
  restarting are left out) sorted by name, shown as `<name> · <image>`; rows with an invalid ID are
  skipped and logged.
- **H4 Notes** (neutral footer level while the selector is open; ≤ 60 cells):
  `No running container — only this machine is offered` (criterion 5);
  `No Docker Engine reachable — only this machine is offered`;
  `Docker Desktop is not supported as a container source yet`;
  `podman is not supported as a container source`.
- **H5 Row texts** (Step column fixed 11; Address 47 cells at 80 columns, measured from the
  DEVDESK-329 layout: 78 − 4×2 padding − 3 − 11 − 9):

  | Kind | Step | Address |
  |---|---|---|
  | container | `container` | `web-1 (172.18.0.5)`; no address known: `web-1`; stopped at launch: `web-1 (not running)` |
  | network | `network` | `app_net via 172.18.0.1`; host mode: `host (shares this machine's stack)`; `none`: `none (no network)`; `container:<x>`: `shares another container's stack`; several, undecidable: `undetermined (3 networks)`; inspection failed or not running: `undetermined` |
  | trace | `trace` | `from this machine, not from the container`; host mode: `from this machine (shared with the container)` |

  Hop and RTT empty for the three rows (as for the local rows of D145). `undetermined…`, `none…`,
  the trace row: dim. Others default colour (Rule 122: plain cells, colour in `Style`).
- **H6 A source change clears the column** (rows, route, container, outcome, error) and returns a
  done tab to editing with the field focused and its value kept: the column never mixes two
  sources. Choosing the source already in use only closes the selector. `H` from Diagnostics
  (`prefill`) resets the source to `this machine` (the route row it comes from is this machine's).
- **H7 Desktop check**: `info --format '{{.OperatingSystem}}'`, prefix `Docker Desktop`. The format
  is a docker-only literal in `internal/docker`, refused under podman before any exec, and **not**
  added to `engine.Templates` (`TestEveryParsedOutputHasATemplatePerEngine` would need an
  unmeasured podman template). Alternative: a `Templates.OperatingSystem` field with a podman
  template measured first.
- **H8 Multi-network tie**: `undetermined`, even though moby documents a tie-break by network name
  (direction unmeasured). Alternative after one measurement: apply it.
- **H9 Remote engines** (`DOCKER_HOST=tcp://…`, ssh contexts) are not detected in S-05a; their
  containers would be listed. The trace row still says the hops are from this machine. S-05b's
  bridge check (brief H10) is the signal. Risk, below.
- **H10 Network drivers** other than `bridge` (macvlan, ipvlan) are not distinguished (the driver
  is not in the container inspection). Risk, below. Alternative (+S): read drivers with
  `network ls` (`templates().NetworkLS` exists) and show `<name> (<driver>)`.
- **H11 Header**: a `Source` field before `Target`: `this machine` or the container name.
- **H12 Display-safe text**: every engine string (names, images, network names) goes through
  `engineText` once, at conversion: `strings.ToValidUTF8`, `ansi.Strip`, control (Cc) and format
  (Cf: bidi overrides, zero-width) runes dropped, whitespace collapsed, capped at 128 runes. Same
  rule as `imagesearch.sanitizeDescription`; a shared helper is a later refactor, not this story.
- **H13 Inspection output** larger than 256 KiB is refused (narrow format makes it ~1 KiB).

## Interfaces (exact signatures)

```go
// internal/docker/container_network.go (new)
type ContainerNetworking struct {
    Name     string            // engine's name without the leading "/"; untrusted text
    Running  bool
    Mode     string            // HostConfig.NetworkMode: "bridge", "host", "none", "container:<id>", a network name
    Networks []EndpointNetwork // sorted by Name
}
type EndpointNetwork struct {
    Name       string
    IPv4       netip.Addr // zero when the endpoint has none
    Gateway    netip.Addr // zero when the endpoint has none (IPv4 only)
    GwPriority *int       // nil: the engine does not report it (API < 1.48)
}
var ErrDockerOnly = errors.New("only Docker Engine answers this read")
const containerNetworkingFormat = `{"Name":{{json .Name}},"Mode":{{json .HostConfig.NetworkMode}},"Running":{{json .State.Running}},"Networks":{{json .NetworkSettings.Networks}}}`
const maxInspectBytes = 256 << 10
func InspectContainerNetworking(ctx context.Context, id string) (ContainerNetworking, error)
func parseContainerNetworking(out []byte) (ContainerNetworking, error)
func ListContainersContext(ctx context.Context, all bool) ([]Container, error) // ListContainers(all) becomes a wrapper with a nil ctx
func EngineOperatingSystem(ctx context.Context) (string, error)               // info --format {{.OperatingSystem}}

// internal/ui/netdiag/path_source.go (new, pure)
type pathSource struct {
    ID    string // validated container ID; "" for this machine
    Name  string // display-safe
    Image string // display-safe
}
func hostSource() pathSource
func (s pathSource) isHost() bool
func (s pathSource) label() string // "this machine" or Name
type pathSourceList struct {
    Containers []pathSource // running, sorted by Name; this machine is not in it
    Note       string       // why only this machine is offered; "" otherwise
}
func sourceListFrom(engineName, osName string, osErr error, cs []docker.Container, psErr error) pathSourceList
func isDockerDesktop(osName string) bool
func engineText(s string) string
const (
    noteNoContainer = "No running container — only this machine is offered"
    noteNoEngine    = "No Docker Engine reachable — only this machine is offered"
    noteDesktop     = "Docker Desktop is not supported as a container source yet"
    notePodman      = "podman is not supported as a container source"
)

// internal/ui/netdiag/path_container.go (new, pure)
type containerAttach int
const (
    attachNetwork containerAttach = iota
    attachHost
    attachNone
    attachShared
    attachUndetermined
)
type pathContainer struct {
    Name     string // display-safe
    Known    bool   // false: the inspection failed
    Running  bool
    Attach   containerAttach
    Network  string     // attachNetwork only, display-safe
    Networks int        // number of endpoints, for "undetermined (N networks)"
    Addr     netip.Addr // the container's IPv4 on Network
    Gateway  netip.Addr // Network's gateway
}
func newPathContainer(src pathSource, n docker.ContainerNetworking, err error) pathContainer
func defaultRouteNetwork(nets []docker.EndpointNetwork) (docker.EndpointNetwork, bool)
func (c pathContainer) containerText() string
func (c pathContainer) networkText() string
func (c pathContainer) traceText() string

// internal/ui/netdiag/path_steps.go (changed)
const (
    stepContainer pathStepKind = iota
    stepNetwork
    stepSource
    stepInterface
    stepGateway
    stepTrace
    stepHop
)
type pathStep struct {
    Kind      pathStepKind
    Container pathContainer // stepContainer, stepNetwork, stepTrace
    Route     pathRoute
    Hop       nettrace.Hop
}
func pathSteps(ctr *pathContainer, route *pathRoute, hops []nettrace.Hop) []pathStep

// internal/ui/netdiag/path_run.go (changed)
type pathEngine struct {
    supported  func() error
    resolve    func(ctx context.Context, host string) ([]netip.Addr, error)
    route      func(ctx context.Context, a netip.Addr) (netcheck.RouteHop, error)
    trace      func(ctx context.Context, a netip.Addr, o nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error)
    engineName func() string                                                   // engine.Current().Name
    engineOS   func(ctx context.Context) (string, error)                        // docker.EngineOperatingSystem
    running    func(ctx context.Context) ([]docker.Container, error)            // docker.ListContainersContext(ctx, false)
    inspect    func(ctx context.Context, id string) (docker.ContainerNetworking, error)
}
type pathResolvedMsg struct {
    gen      int
    addr     netip.Addr
    route    netcheck.RouteHop
    routeErr error
    source   pathSource                 // the source at launch
    ctr      docker.ContainerNetworking // zero for this machine
    ctrErr   error                      // logged by the producer, never shown
}
type pathSourcesMsg struct {
    gen  int // PathModel.sourcesGen
    list pathSourceList
}
func listPathSources(ctx context.Context, e pathEngine) pathSourceList
func loadPathSourcesCmd(gen int, timeout time.Duration, e pathEngine) tea.Cmd
func producePath(ctx context.Context, gen int, tg pathTarget, src pathSource, resolveTimeout time.Duration, e pathEngine, out chan tea.Msg)
func startPathRun(ctx context.Context, gen int, tg pathTarget, src pathSource, timeout time.Duration, e pathEngine, ch chan tea.Msg) tea.Cmd

// internal/ui/netdiag/path_source_picker.go (new, value type, no I/O)
type pathSourcePicker struct {
    sources []pathSource // this machine first
    current string       // ID of the source in use ("" = this machine)
    cursor  int
    top     int
    loading bool
    note    string
}
func newPathSourcePicker(current pathSource) pathSourcePicker
func (p pathSourcePicker) withList(l pathSourceList) pathSourcePicker
func (p pathSourcePicker) move(key string, rows int) pathSourcePicker
func (p pathSourcePicker) selected() pathSource
func (p pathSourcePicker) render(width, height int) []string
func pickerBoxWidth(width int) int

// internal/ui/netdiag/path_model.go (changed)
const pathSourceKey = "ctrl+o" // Q1
// fields: source pathSource; picker *pathSourcePicker; sourcesGen int; container *pathContainer; height int
func (pm *PathModel) pickerOpen() bool
func (pm *PathModel) sourceChoosable() shortcut.Availability
func (pm *PathModel) openSourcePicker() (*PathModel, tea.Cmd)
func (pm *PathModel) handleSources(msg pathSourcesMsg) (*PathModel, tea.Cmd)
func (pm *PathModel) handlePickerKey(msg tea.KeyPressMsg) (*PathModel, tea.Cmd)
func (pm *PathModel) chooseSource(s pathSource) (*PathModel, tea.Cmd)
func (pm *PathModel) clearColumn() // shared by launch, prefill, chooseSource, error done
```

## Steps

Order: **1 ∥ 2** → **3 ∥ 4 ∥ 5** → 6 → 7 → 8. Each step ends green on its targeted command and
`go vet` of the touched packages; step 8 on `mise run ci`. `-race` needs cgo (gcc on PATH);
without it, run the same command without `-race` and say so. Unused-symbol lint findings between
steps 2 and 6 are expected (as in DEVDESK-328/329); lint is clean at step 8.

### Step 1 — typed, read-only engine reads (`internal/docker`) — parallel with 2 — S

Files: new `internal/docker/container_network.go`, `container_network_test.go`;
`containers.go` (`ListContainersContext`, `ListContainers` wraps it); `system.go` or new
`engine_os.go` (`EngineOperatingSystem`).
Behaviour: `InspectContainerNetworking` refuses a non-ID (`IsContainerID`) and a non-docker engine
(`engine.Current().Name != engine.Docker` → `ErrDockerOnly`) **before** any `runner.Run`; runs
`container inspect --format <containerNetworkingFormat> <id>` with `Ctx: ctx`, stdout only (not
`Combined`); error → `wrapErr(cmdLabel("container inspect"), err)` (no output in it). Same for
`EngineOperatingSystem` (`info --format {{.OperatingSystem}}`, trimmed) and
`ListContainersContext` (`ps [--all] --format templates().PS`, `Ctx: ctx`).
Tests (stub runner, table-driven):
- parse: one bridge endpoint `{IPAddress 172.17.0.3, Gateway 172.17.0.1, GwPriority 0}` →
  `Networks[0]` with both addresses `Is4()`, `*GwPriority == 0`, `Running`, `Mode "bridge"`,
  `Name` without `/`.
- parse: `GwPriority` key absent → `nil`; two endpoints given in map order `z, a` → sorted `a, z`.
- parse: host mode (`Networks {"host": {IPAddress ""}}`) → `Mode "host"`, `IPv4` zero.
- parse: `IPAddress "not-an-ip"`, IPv6 gateway → zero `netip.Addr`, no error; invalid JSON → error;
  257 KiB output → error.
- args: inspect records exactly `container inspect --format <fmt> <id>`, `dc.Ctx != nil`,
  `Combined == false`; `ps` without `--all` for `all=false`; `info --format {{.OperatingSystem}}`.
- refusals: `"web-1"`, `"--help"`, `""` as ID → error, **zero calls**; podman engine → `ErrDockerOnly`
  for inspect and OS, **zero calls**.
- **Criterion 7 (engine level)**: `TestPathSourceReadsStartNothing` calls the three functions and
  asserts every recorded `Args[0]` is in `{ps, info, container}` and, for `container`, `Args[1] ==
  "inspect"`; no arg in `{start, run, exec, create, restart, unpause, attach, kill, stop}`.
- existing `ListContainers` tests stay green unchanged.
Command: `go test -race -count=1 ./internal/docker`
Done when: green; no change to `engine.Templates`; `InspectContainer` untouched.

### Step 2 — source list, pure (`path_source.go`) — parallel with 1 — S

Files: new `internal/ui/netdiag/path_source.go`, `path_source_test.go`.
`sourceListFrom` (inputs → result):
- `"podman", …` → no containers, `notePodman` (whatever the other inputs).
- `"docker", "", osErr` → `noteNoEngine`; `"docker", "Docker Desktop", nil, …` → `noteDesktop`
  (also `"Docker Desktop 4.96"`); `"docker", "Debian GNU/Linux 13 (trixie)", nil, nil, psErr` →
  `noteNoEngine`.
- Engine OK, `ps` rows `web-1 running`, `db paused`, `api running`, `bad-id running` (ID `xyz`) →
  `[api, web-1]`, note `""` (**criterion 1**, paused and bad ID left out).
- Engine OK, no running row → empty, `noteNoContainer` (**criterion 5**).
- A name `"we\x1b[31mb‮-1"` and an image with `\n` → `Name == "web-1"`, no rune `< 0x20`, no
  `\x1b`, no Cf rune; a 300-rune name → 128 runes.
`engineText`: table of the cases above plus valid text unchanged. `isDockerDesktop`: `"Docker
Desktop"` true, `"Ubuntu 24.04"` false, `""` false. `hostSource().label() == "this machine"`.
Every note: `ansi.StringWidth ≤ 60`.
Command: `go test -race -count=1 -run 'PathSource|EngineText|DockerDesktop' ./internal/ui/netdiag`

### Step 3 — container rows in the column (`path_container.go`, `path_steps.go`, `path_view.go`) — after 1 and 2 — S

Files: new `path_container.go`, `path_container_test.go`; `path_steps.go`, `path_steps_test.go`,
`path_view.go` (Step/Address styles for the new kinds); `path_model.go` call site
`pathSteps(nil, pm.route, pm.hops)` (one-line change; step 6 passes the container).
`defaultRouteNetwork` (**criterion 3**):
- one endpoint with gateway → it; `[a: gw, b: no gw]` → `a` (internal network cannot hold the
  default route);
- `[a: gw prio 0, b: gw prio 10]` → `b`; `[a: prio 5, b: prio 5]` → not found (H8);
  `[a: gw prio nil, b: gw prio 3]` → not found (engine did not report it on every candidate);
  `[a: gw nil-prio, b: gw nil-prio]` → not found; none with a gateway → not found.
`newPathContainer`: err → `Known false`, texts `web-1` / `undetermined`; `Mode "host"` →
`attachHost`; `"none"` → `attachNone`; `"container:abc"` → `attachShared`; `Running false` →
`web-1 (not running)` / `undetermined`; one network → `web-1 (172.18.0.5)` /
`app_net via 172.18.0.1`; two undecidable → `web-1` / `undetermined (2 networks)`.
`pathSteps` (new slice, inputs untouched):
- `nil, route, hops` → today's order exactly (every DEVDESK-329 table stays green).
- `ctr, route, [ttl1]` → kinds `[container, network, source, interface, gateway, trace, hop1]`
  (**criteria 2 and 6**).
- host-mode `ctr` → same kinds, network text `host (shares this machine's stack)`, trace text
  `from this machine (shared with the container)`, **no other kind** (**criterion 4**: no bridge).
- `ctr, nil, nil` → `[container, network, source, trace]`; `nil, nil, nil` → nil.
Cells: every new kind returns plain text (`ansi.Strip(c) == c`); Hop and RTT empty; the source row
still reads `this machine` and is the only row whose Address equals it.
Command: `go test -race -count=1 -run 'PathStep|PathRoute|PathContainer|DefaultRoute' ./internal/ui/netdiag`

### Step 4 — seam, listing Cmd and producer (`path_run.go`) — after 1 and 2 — S

Files: `path_run.go`, `path_run_test.go` (fake gains `engineName "docker"`, `engineOS "Debian
GNU/Linux 13 (trixie)"`, `running`, `inspect`, each with a call counter and recorded args),
`path_flow_test.go`, `path_model.go` (`startPathRun` gets `pm.source`; until step 6 it is always
`hostSource()`).
`systemPathEngine()` wires `engine.Current().Name`, `docker.EngineOperatingSystem`,
`docker.ListContainersContext(ctx, false)`, `docker.InspectContainerNetworking`.
`listPathSources`: name ≠ docker → returns `notePodman` with **no** `engineOS`/`running` call;
`engineOS` error → `noteNoEngine`, `running` not called; Desktop → `noteDesktop`, `running` not
called; else `sourceListFrom(...)`. Raw errors logged `ERROR [netdiag/path] sources: …`.
`loadPathSourcesCmd` runs it under `context.WithTimeout(timeout)` and returns `pathSourcesMsg{gen,
list}`.
`producePath`: unchanged for `hostSource()` (**inspect never called**); for a container, after the
route: `inspect(ctx with resolveTimeout, src.ID)`; error logged `ERROR [netdiag/path] inspect <id>:
…` when `ctx.Err() == nil`; then `pathResolvedMsg{…, source, ctr, ctrErr}`; then the trace as today.
Tests:
- host source → resolved msg `source.isHost()`, inspect calls 0, message order resolved, hops, done.
- container source → inspect called once with the ID, the message carries the fake's networking;
  inspect error → message carries it, **trace still called**, done last.
- resolution error → neither route nor inspect called.
- `pathChanCap + 5` hops with a container source → done still last; `pathChanCap` unchanged.
- listing: the four paths above, with the call counters.
Command: `go test -race -count=1 -run 'ProducePath|WaitForPath|PathFlow|ListPathSources' ./internal/ui/netdiag`

### Step 5 — the selector (`path_source_picker.go`) — after 2 — S

Files: new `path_source_picker.go`, `path_source_picker_test.go`.
Value type, every method returns a new value (immutability). Tests:
- `newPathSourcePicker(hostSource())` → one row `this machine (current)`, cursor 0, `loading`.
- `withList({[api, web-1], ""})` with current `web-1` → rows `this machine, api, web-1`, cursor on
  `web-1`, not loading; current not listed any more → cursor 0.
- `move`: `down` at the end stays, `up` at 0 stays, `end`/`home`, `pgdown` by `rows`; `top` follows
  the cursor so it is always visible with 3 visible rows and 10 entries.
- `render(80, 10)`: every line `ansi.StringWidth == pickerBoxWidth(80)` (56), first line carries
  `Trace from` in the border, the highlighted row starts with `theme.IconCircleSmall`, the others
  with two spaces (Rule 120); rows ≤ height − 2.
- `render(60, 5)` and `render(24, 3)`: same width rule, truncation mark on a 60-rune name, no line
  wider than the box.
- A source whose Name was built through `engineText` from `"\x1b]0;pwn\a"` → no `]0;` in
  `ansi.Strip(line)`.
Command: `go test -race -count=1 -run 'PathSourcePicker' ./internal/ui/netdiag`

### Step 6 — wiring in the tab and the view — after 3, 4, 5 and Q1 — M

Files: `path_model.go`, `path_view.go`, `path_from_route.go` (`prefill` resets the source),
`update.go` (route `pathSourcesMsg`; on `tabPath` with the selector open, keys go to the Path model
**before** the `tab`/`shift+tab` switch; `InEditMode` is true while it is open), `view.go`
(`tabPath`: `pm.view()` composes the selector with `overlay.Compose(lines, box, x, 0,
ansi.StringWidth(lines[0]))`), `internal/ui/keymap/keymap.go` (exception `{Key: "ctrl+o",
Surface: "netdiag/path", Why: …}`); tests in `path_model_test.go`, new `path_source_flow_test.go`,
`netdiag_path_tab_test.go`.
Behaviour: `pathSourceKey` in editing or done → selector open, `sourcesGen++`,
`loadPathSourcesCmd`; in running → `Warn("A path trace is running — esc cancels it")`, nothing
opens; unsupported platform → `Warn(reasonPathUnsupported)`. `pathSourcesMsg` of another `gen`
or with the selector closed → dropped. `enter` → `chooseSource(selected)` (H6), `esc` → close.
`launch` passes `pm.source`; `handleResolved` builds `pm.container` from the message (nil for
this machine); `handleDone` error and `clearColumn` drop it. Header: `Source` info (H11). Shortcuts:
editing and done gain `{pathSourceKey, "Trace from…", Disabled: !sourceChoosable}`; selector open
→ `↑↓ Move`, `enter Choose`, `esc Close`. `statusLine`: selector open → loading `Reading running
containers...` (Activity) or the note (neutral) or nothing; otherwise as today.
Tests (full `netdiag.Model`, fake engine, input → expected):
- **Criterion 1**: fake `running` = `web-1`, `api` running, `db` paused → after the key and the
  Cmd's message, the rendered selector lists `this machine`, `api`, `web-1`, not `db`.
- **Criterion 5**: `running` = none → only `this machine`, footer text `noteNoContainer`, level
  neutral; podman, Desktop and unreachable → their note, only `this machine`.
- **Criterion 2**: choose `web-1`, launch `1.1.1.1`, resolved with route and networking
  `app_net 172.18.0.5 via 172.18.0.1`, hops, done → Step column top to bottom `container, network,
  source, interface, gateway, trace, router…`; Address of rows 0, 1, 2 = `web-1 (172.18.0.5)`,
  `app_net via 172.18.0.1`, `this machine`.
- **Criterion 3**: networking with two gateway endpoints, priorities 0 and 0 → network row
  `undetermined (2 networks)`; 0 and 10 → the priority-10 network.
- **Criterion 4**: host mode → network row `host (shares this machine's stack)`, row kinds exactly
  as in step 3.
- **Criterion 6**: the `trace` row sits between `gateway` and hop 1 in running and done states,
  and is absent with `this machine` as source.
- inspect error → rows `web-1` / `undetermined`, hops still appended, no raw text in the footer.
- the selector open: `tab` does not switch tab, a paste changes nothing, `q` and `:` reach the
  view (InEditMode true), `esc` closes and the source is unchanged; a stale `pathSourcesMsg`
  (gen − 1) changes nothing.
- source change in done → rows 0, state editing, field focused with its value, header `Source ›
  web-1`; same source re-chosen → rows kept. `H` from Diagnostics → source back to `this machine`.
- every existing Path test green with only `pathSteps(nil, …)` call-site changes.
Command: `go test -race -count=1 ./internal/ui/netdiag && go test -count=1 ./internal/ui/keymap`
Done when: green, including `TestOnlyThreeCtrlCombinationsSurvive` and
`TestEveryLowercaseBindingIsDeclared`.

### Step 7 — 80 columns, guards and help — after 6 — S

Files: `path_column_layout_test.go`; new `path_engine_reads_test.go`; `header.go` (help);
`internal/app/activity_guard_test.go` and `netdiag_path_guard_test.go` (`path_source.go`,
`path_container.go`, `path_source_picker.go` scanned; `path_run.go` still out).
Tests:
- 80 × 24, container `web-1` renamed to a 60-character name, network name 40 characters, 30 hops,
  done: every line of `Render()` has `ansi.StringWidth == 78`; the stripped body contains
  `from this machine, not from the container` in full and the truncation mark on the container row;
  no `\x1b` after `ansi.Strip`. Repeat at 60 (texts truncated, widths exact). Same with the selector
  open over a 40-entry list: every line still 78 (the overlay never changes a width).
- Scrolling: 36 rows in a short body: `end` shows TTL 30, `home` shows `container` again.
- **Criterion 7 (view level)**: `TestPathTabCallsOnlyReadOnlyEngineFunctions` parses every
  non-test `.go` file of `internal/ui/netdiag` (go/parser) and fails on any `docker.<X>` selector
  outside `{Container, ContainerNetworking, EndpointNetwork, ListContainersContext,
  EngineOperatingSystem, InspectContainerNetworking}`.
Help ("Path Tab — How it works", Rule 114): one paragraph — `ctrl+o` chooses the source among this
machine and the running containers of a Docker Engine on this machine; the column then starts with
the container and its network; the hops are still traced from this machine; Docker Desktop and
podman are not offered yet; DevDesk only reads the engine (`ps`, `info`, `inspect`) and starts
nothing. Key list: `ctrl+o (Path)`.
Command: `go test -race -count=1 ./internal/ui/netdiag && go test -count=1 -run 'Activity|Guard|EveryColumn' ./internal/app ./internal/ui/datatable`

### Step 8 — documentation — after 7 — S

- `docs/architecture/network.md`: Path tab entry (source selector, the three rows, `path_source.go`,
  `path_container.go`, `path_source_picker.go`); "The Path tab — streaming…" gains "The container
  source (DEVDESK-330)": D-a … D-f, the measured inspect fields, the `GwPriority` rule and its tie,
  Desktop/podman refusal, the narrow format (no `Env` read), the two guards; `internal/docker`
  section: the three new reads and why `InspectContainer` was not reused.
- `docs/architecture/app-shell.md` keyboard vocabulary: the `ctrl+o` exception on `netdiag/path`.
- `docs-site/docs/explanation/network.md` § "The Path tab": choosing a container, what the rows
  mean, why the hops still come from this machine.
- `.claude/CLAUDE.md` overview line on network diagnostics: "… a Path tab (…; `ctrl+o` traces from a
  running Docker Engine container: container and network rows, hops still from this machine) …".
- `docs/backlog.md`: D146 for D-a … D-f and H1–H13 (archivist). No ADR: one view, reversible, no
  persisted format, no public API.
Command: `mise run ci`.

## Manual check (PR description; Linux with Docker Engine, see Q2)

1. `docker run -d --name web nginx`; `dk` → `:net` → Path → `ctrl+o`: `this machine`, `web`.
   `enter`, target `1.1.1.1`, `enter`: rows `container web (172.17.0.x)`, `network bridge via
   172.17.0.1`, `source`, `interface`, `gateway`, `trace`, hops.
2. `docker network create n2 && docker network connect n2 web`: network `undetermined (2
   networks)`; `docker network connect --gw-priority 10 …`: that network.
3. `docker run -d --network host --name h nginx`: `host (shares this machine's stack)`.
4. `docker stop` everything: the selector offers `this machine` only, footer note.
5. `docker events --since <start>` during 1–4 shows no `start` event caused by DevDesk.
6. On Docker Desktop (this workstation): the selector offers `this machine` and the Desktop note.

## Risks

- **Remote or VM engines not caught** (H9): a `DOCKER_HOST=ssh://…` engine's containers are listed
  and drawn before `this machine`, which they never pass through. The trace row limits the damage;
  S-05b's bridge presence check is the planned fix. Flag in the PR.
- **macvlan / ipvlan** (H10): same shape of error; same mitigation.
- **Default gateway rule changes across engine versions** ("may change", docs.docker.com): the rule
  answers only with a gateway-bearing single endpoint or a strict `GwPriority` maximum, otherwise
  `undetermined` — never a guess.
- **One more CLI call per launch** (~50–150 ms) with a container source; bounded by the resolve
  timeout and killed by the run's context on `esc`.
- **Keyboard** (Q1): a Ctrl exception is a debt the keymap tests make visible; its `Why` is written
  in the same commit.
- **Test churn**: `pathSteps` gains a parameter (one call site, the step-1 tests of DEVDESK-329 pass
  `nil`); `startPathRun`/`producePath` gain `src` (fake-based tests only).
- **Irreversible**: none. No config key, no persisted state, no public API; the keymap exception is
  one line to remove.

## Effort

M overall, as Plane says: steps 1–5 and 7–8 are S each, step 6 is M (selector routing across
`netdiag.Model` and the Path model). Parallel: 1 ∥ 2, then 3 ∥ 4 ∥ 5.

## Sources

- Docker networking overview (default gateway, `gw-priority`): https://docs.docker.com/engine/network/
- moby `EndpointSettings.GwPriority` (highest wins, ties by network name): https://pkg.go.dev/github.com/moby/moby/api/types/network
- `docker network connect --gw-priority`: https://docs.docker.com/reference/cli/docker/network/connect/
