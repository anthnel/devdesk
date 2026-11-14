# DEVDESK-329 (S-04) — Path tab: the column starts with this machine, its interface and its gateway

Base: `main` with PR 401 (DEVDESK-328, Path tab) and PR 400 (DEVDESK-327, `internal/nettrace`).
View: `internal/ui/netdiag` (`path_model.go`, `path_view.go`, `path_run.go`, `path_target.go`).
Out of scope: container as source (S-05a), bridge (S-05b), Engine VM (S-06), WSL labels (S-07),
greyed tab (S-08), failure reading (S-09), graph, switches.

## Acceptance criteria (Plane DEVDESK-329)

1. Reachable target: step order is source, interface with source address, gateway, hops.
2. A VPN that captures the default route: the interface shown is the VPN's.
3. 80-column terminal: no line overflows, no escape sequence is cut.
4. More hops than lines: the navigation keys scroll the column.
5. The source is labelled `this machine`, never as another machine.

## Hypotheses checked in the code

**(a) "The `route` stage of `netcheck` provides interface and gateway" — true for the data, false
for the stage.** `runRoute` (`internal/netcheck/stage_route.go`) turns the answer into a `Check`
whose facts are **strings** (`Interface`, `Source address`, `Gateway`, `Route`): reading them back
would mean parsing text. The structured answer is one level down: `netcheck.Env.Route(ctx, net.IP)
(RouteHop, error)`, `RouteHop{Interface string; Source, Gateway net.IP}` (`env.go`), implemented by
`systemEnv.Route` (`env_route.go`) over `github.com/libp2p/go-netroute` v0.4.0. The plan reuses
`netcheck.SystemEnv(netcheck.DefaultSettings()).Route` (exported, sends nothing, ~2 ms, no
privilege). **No change to `internal/netcheck`.**

Criterion 2 rests on how go-netroute answers on Linux. Read in the module cache
(`go-netroute@v0.4.0/netroute_linux.go`): it sends an `RTM_GETROUTE` request for the destination to
the kernel (`Table: RT_TABLE_UNSPEC`), i.e. the same question as `ip route get <dst>`. The kernel
applies its policy rules, so a VPN that captures the default route through a separate table
(wg-quick: `ip rule not fwmark 51820 table 51820`; OpenVPN `def1`: two /1 routes) answers with the
VPN interface. The trace's own UDP socket is unmarked and not bound to a device (`trace_linux.go`,
`DialContext("udp4", …)`), so the kernel gives it the same route: the interface row and the probes
agree.

**(b) "Display goes through a `datatable`" — true.** `PathModel.table` is a
`datatable.Model[nettrace.Hop]` (`path_model.go`), columns in `pathColumns()` (`path_view.go`), body
`pm.table.View()`. Navigation keys already reach it in every state (`isPathNavKey` →
`pm.table.Update`, which handles `up/down/pgup/pgdown/home/end`), so criterion 4 is already met by
the component and only needs pinning with the new rows. **Effort stays S.** The cost of S is one
type change: the table's item becomes a `pathStep` (a hop or one of the three local steps), which
touches the existing tests that read `pm.table.Items()` as hops (listed in step 3).

## Options compared — where the source, interface and gateway come from

| Option | How | For | Against |
|---|---|---|---|
| **A. `Env.Route` in the producer, carried by `pathResolvedMsg` (recommended)** | After resolution, before the trace, `producePath` asks the route for the resolved address; the result rides on the message that already says where the run goes | Same address as the trace; same kernel answer as the probes' socket; no new message, so `pathChanCap` (`MaxHopsLimit + 2`) is unchanged; I/O stays in the Cmd (Rule 110); fakeable through `pathEngine` | One more seam field to fake |
| B. Re-run the `route` stage (`runRoute`) | Build a `Target`, run the stage, read `Check.Facts` | Same wording as Diagnostics | Parses display strings; the stage expects `Results` from the resolve stage for a name |
| C. Read the trace socket's local address in `nettrace` | `getsockname` on the connected UDP socket | Exact source of the probes | No interface name, no gateway; widens `nettrace`'s API for a UI concern |
| D. Reuse the Diagnostics result when the tab is opened by `H` | Carry the route row's facts over | No lookup | Only on the `H` path; stale; Diagnostics may route several addresses, the trace uses the first |

A separate `pathRouteMsg` (variant of A) is rejected: it would need `pathChanCap + 1` and one more
re-arm for a datum that is always known at the same moment as the address.

## Decisions (made here, record in docs)

- **D-a Route lookup in the producer (option A).** `producePath`: resolve → `route(ctx, addr)` →
  send `pathResolvedMsg{gen, addr, route, routeErr}` → trace. A route error is **not fatal**: the
  trace still runs (if there is truly no route, `nettrace` returns `ErrNoRoute` and the existing error
  path applies). The raw error is logged in the producer
  (`ERROR [netdiag/path] route <host>: …`), never shown.
- **D-b One row type, four kinds.** `pathStep{Kind: stepSource | stepInterface | stepGateway |
  stepHop}`; `pathSteps(route, hops)` is the only place that orders the column. S-05a/S-06/S-07 add
  kinds or change the source label here, nothing else moves.
- **D-c The source never shows a host name or an address.** Its Address cell is the fixed text
  `this machine`. Not `os.Hostname()`: under WSL2 the distro's host name defaults to the Windows
  computer name, which would label the source as another machine (criterion 5, S-07). The source
  address is on the interface row (criterion 1).
- **D-d No rows before the route is known.** Before `pathResolvedMsg` the table is empty (unchanged
  "Resolving …" footer); a resolution error leaves 0 rows (DEVDESK-328 criterion 3 unchanged). The
  source row is shown as soon as any row is shown.

## Hypotheses (prudent defaults, flag in the PR; none blocks)

- H1 Column 2 is retitled `Answer` → `Step`: `source`, `interface`, `gateway` are not answers. Width
  stays fixed 11 (`unreachable` is the longest value). If the UX prefers to keep `Answer`, only the
  title and one test string change.
- H2 Row layout (US English, Rule 129; labels from `brief.md` "Libellés"):

  | Kind | Hop | Step | Address | RTT |
  |---|---|---|---|---|
  | source | (empty) | `source` | `this machine` | (empty) |
  | interface | (empty) | `interface` | `wlan0 (192.168.1.21)`; no source: `wlan0`; no name: `unnamed (192.168.1.21)`; lookup failed: `undetermined` (dim) | (empty) |
  | gateway | (empty) | `gateway` | `192.168.1.1`; none: `directly connected` (dim, same words as the route check's fact); lookup failed: `undetermined` (dim) | (empty) |
  | hop | TTL | `HopKind.String()` | as today | as today |

  Empty (not `-`) in Hop and RTT for the local rows: `-` means "no answer", and these rows asked
  nobody. Kind and Address of the local rows use the default text colour (no green, Rule 122).
- H3 The gateway row stays even when it repeats hop 1's address (criterion 1 lists both).
- H4 An error done (`ErrNoRoute`, …) still clears every row, as in DEVDESK-328; showing the local
  rows of a failed run is S-09's business.
- H5 The status counter counts hop rows only (`Tracing x — N hops answered`), not the three local
  rows.

## Interfaces (exact signatures)

```go
// internal/ui/netdiag/path_steps.go (new)
type pathStepKind int
const (
    stepSource pathStepKind = iota
    stepInterface
    stepGateway
    stepHop
)

// pathRoute is the way out of this machine for the traced address.
type pathRoute struct {
    Known     bool       // false: the lookup failed; interface and gateway are undetermined
    Interface string
    Source    netip.Addr // invalid: the stack did not say
    Gateway   netip.Addr // invalid: directly connected
}

// pathStep is one row of the Path column.
type pathStep struct {
    Kind  pathStepKind
    Route pathRoute   // stepInterface, stepGateway
    Hop   nettrace.Hop // stepHop
}

const sourceLabel = "this machine"      // criterion 5, brief "Libellés"
const routeUndetermined = "undetermined"
const gatewayDirect = "directly connected"

func newPathRoute(h netcheck.RouteHop, err error) pathRoute // net.IP -> netip.Addr, Unmap()
func pathSteps(route *pathRoute, hops []nettrace.Hop) []pathStep
func (s pathStep) hopCell() string
func (s pathStep) stepCell() string
func (s pathStep) addressCell() string
func (s pathStep) rttCell() string

// internal/ui/netdiag/path_run.go (changed)
type pathEngine struct {
    supported func() error
    resolve   func(ctx context.Context, host string) ([]netip.Addr, error)
    route     func(ctx context.Context, a netip.Addr) (netcheck.RouteHop, error) // new
    trace     func(ctx context.Context, a netip.Addr, o nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error)
}
type pathResolvedMsg struct {
    gen      int
    addr     netip.Addr
    route    netcheck.RouteHop // new
    routeErr error             // new; the raw text is logged by the producer, never shown
}

// internal/ui/netdiag/path_model.go (changed)
// table datatable.Model[pathStep]   (was datatable.Model[nettrace.Hop])
// route *pathRoute                  (nil until pathResolvedMsg of the current gen)
func (pm *PathModel) refreshSteps()  // pm.table.SetItems(pathSteps(pm.route, pm.hops))
func (pm *PathModel) hopCount() int  // rows of kind stepHop

// internal/ui/netdiag/path_view.go (changed)
func pathColumns() []datatable.Column[pathStep]
```

## Steps

Order: **1 and 2 in parallel** → 3 → 4 → 5. Each step ends green on its targeted command and
`go vet ./internal/ui/netdiag`; the last one on `mise run ci`. `-race` needs cgo (gcc on PATH); without
it run the same command without `-race` and say so. Unused-function lint findings between step 1 and
step 3 are expected (as in DEVDESK-328); lint must be clean at step 5.

### Step 1 — pure row model (`path_steps.go`) — parallel with 2 — S

Files: new `internal/ui/netdiag/path_steps.go`, `path_steps_test.go` (table-driven).
Depends on nothing in this plan (`netcheck.RouteHop` and `nettrace.Hop` exist).

`newPathRoute`:
- `{eth0, 192.168.1.21, 192.168.1.1}, nil` → `Known`, `eth0`, both addresses valid and `Is4()`.
- 16-byte IPv4-mapped `net.IP` (`net.ParseIP("192.168.1.1")`) → `Unmap`ped, `Is4()`.
- nil `Gateway` → `Gateway` invalid (directly connected); nil `Source` → `Source` invalid.
- any error → `Known == false`, other fields zero (whatever the hop holds).

`pathSteps` (returns a new slice, inputs untouched):
- `nil, nil` → empty (no row before the route is known, D-d).
- `route, nil` → kinds `[source, interface, gateway]` (resolved, no hop yet).
- `route, [ttl3, ttl1]` → `[source, interface, gateway, hop1, hop3]` (hops through
  `visiblePathHops`: sorted, unresolved dropped, cut after the terminal hop) — **criterion 1**.
- `nil, [ttl1]` (hop before route, only in tests or a lost message) → `[source, hop1]`: the source is
  first whenever anything is shown.
- `&pathRoute{Known:false}, hops` → `[source, interface, gateway, hops…]` with undetermined rows.

Cells (each returns plain text; assert `ansi.Strip(cell) == cell` for every kind, Rule 122):
- source: hop `""`, step `source`, address `this machine`, rtt `""` — for **every** route value,
  including a route whose `Source` equals a hop address (**criterion 5**).
- interface: `eth0 (192.168.1.21)`; `tun0` with no source → `tun0`; no name → `unnamed (192.168.1.21)`;
  not known → `undetermined`. A VPN route `{Interface: "wg0", Source: 10.2.0.2, Gateway: nil}` →
  `wg0 (10.2.0.2)` (**criterion 2**, unit level).
- gateway: `192.168.1.1`; invalid → `directly connected`; not known → `undetermined`.
- hop: `hopCell` = TTL, `stepCell` = `Kind.String()`, `addressCell` = addr or `-`, `rttCell` =
  `12.3 ms` or `-` (today's behaviour, moved from the closures in `pathColumns`).

Command: `go test -race -count=1 -run 'PathStep|PathRoute' ./internal/ui/netdiag`
Done when: tables green; no `os.Hostname` call anywhere under `internal/ui/netdiag/path_*`.

### Step 2 — route lookup in the producer — parallel with 1 — S

Files: `internal/ui/netdiag/path_run.go`, `path_run_test.go` (fake gains `routeCalls`, `routeAddr`,
`routeFn`; default answer `netcheck.RouteHop{Interface: "eth0", Source: 192.168.1.21, Gateway:
192.168.1.1}` so every existing producer test keeps passing), `path_flow_test.go` (compiles
unchanged through the fake).
Change: `pathEngine.route`; `systemPathEngine()` sets it to
`netcheck.SystemEnv(netcheck.DefaultSettings()).Route` with `net.IP(a.AsSlice())`. In
`producePath`, after `resolvePathTarget` succeeds: `hop, rerr := e.route(ctx, addr)`; on `rerr != nil`
and `ctx.Err() == nil`, `log.Printf("ERROR [netdiag/path] route %s: %v", tg.Host, rerr)`; then
`send(pathResolvedMsg{gen, addr, hop, rerr})`, then trace as today.
Tests (synchronous producer, nobody reading, as today):
- `"1.1.1.1"` → route called once with `1.1.1.1`; the resolved message carries the fake's `RouteHop`;
  message order still resolved, hops…, done.
- `"example.com"` resolving to `[93.184.216.34, 1.2.3.4]` → route called with `93.184.216.34` (the
  traced address, not another one).
- route returns an error → the resolved message carries it, **trace still called**, done sent.
- resolution error → route never called, trace never called (no lookup for a target that was not
  resolved).
- `pathChanCap + 5` hops → done still last (existing `TestProducePathDoneSurvivesAFullChannel`): the
  route adds no message.
Command: `go test -race -count=1 -run 'ProducePath|WaitForPath|PathFlow' ./internal/ui/netdiag`
Done when: green; `pathChanCap` unchanged; no network or routing table touched by a test.

### Step 3 — the table shows the column (`PathModel` + `pathColumns`) — after 1 and 2 — S

Files: `path_model.go`, `path_view.go`, `path_from_route.go` (`prefill` clears `route`),
`path_model_test.go`, `path_flow_test.go`, `netdiag_path_tab_test.go`.
Changes:
- `table datatable.Model[pathStep]`; `pathColumns()` over `pathStep`: `Hop` fixed 3, `Step` fixed 11
  (H1), `Address` content min 15 flex 1, `RTT` fixed 9 — the same widths, so Rule 116 thresholds do
  not move. `Cell` = the step-1 cell functions; `Style`: hop rows as today (`pathAnswerStyle` on the
  hop kind, dim `-`); local rows default, except `undetermined` and `directly connected` dim.
- field `route *pathRoute`; `handleResolved` sets `pm.addr` and `pm.route = &r` (`newPathRoute(msg.route,
  msg.routeErr)`), then `refreshSteps()`; `handleHop` and the success branch of `handleDone` call
  `refreshSteps()`; `launch`, the error branch of `handleDone` and `prefill` set `route = nil`
  (before their `SetItems(nil)`); `cancelRun` keeps it (rows kept).
- `statusLine` counts `pm.hopCount()` instead of `len(pm.table.Items())` (H5).
Test helpers to adapt: `pmHopTTLs` reads only `stepHop` rows; `len(pm.table.Items())` assertions that
mean "hops" become `pm.hopCount()` (`TestPathStaleGenerationHopIsDropped`,
`TestPathDoneEnterRelaunchesAndEscEdits`, `TestPathMessagesAreRoutedWhateverTabIsOnScreen`); the
header-column list in `TestPathNewModelIsAnEditableEmptyTable` reads `Step` (H1);
`TestPathUnresolvedNameIsAPersistentFooterError` keeps "0 rows".
New tests (input → expected):
- launch `1.1.1.1`, resolved with route `{eth0, 192.168.1.21, 192.168.1.1}`, hops TTL 2 then 1, done
  reached → visible Step column, top to bottom: `source, interface, gateway, router, target`; Address
  of rows 0..2 = `this machine`, `eth0 (192.168.1.21)`, `192.168.1.1` (**criterion 1**).
- resolved with route `{wg0, 10.2.0.2, nil}` → interface row `wg0 (10.2.0.2)`, gateway
  `directly connected` (**criterion 2**, model level).
- resolved with `routeErr` → interface and gateway `undetermined`, hops still appended, footer has no
  error and no raw error text (Rule 128).
- before the resolved message → 0 rows, footer `Resolving …`; after it and before any hop → 3 rows,
  footer `Tracing … — 0 hops answered` (H5).
- the first row is `source`/`this machine` in running, done and canceled states, and no other row's
  Address is `this machine` (**criterion 5**).
- a stale-gen resolved message → route unchanged, no row, Cmd nil.
- relaunch after done → rows emptied at launch, route nil until the new resolved message; `prefill`
  (`H`) → 0 rows, route nil.
- error done (`ErrNoRoute`) after a resolved message → 0 rows (H4), editing, persistent error.
Command: `go test -race -count=1 -run 'TestPath|ProducePath' ./internal/ui/netdiag`
Done when: green; every existing Path test still passes with only the helper changes listed.

### Step 4 — width, scrolling and help at the view level — after 3 — S

Files: new `internal/ui/netdiag/path_column_layout_test.go`; `header.go` (help text);
`internal/app/activity_guard_test.go` (`migratedSources[command.ViewNetdiag]` +=
`ui/netdiag/path_steps.go`), `internal/app/netdiag_path_guard_test.go` (want list +=
`path_steps.go`).
Help: in "Path Tab — How it works", one sentence after the first paragraph: "The column starts on
this machine: the interface the target's traffic leaves by, with its source address, and the
gateway, read from this machine's routing table, then the hops." Keep the rest.
Tests (full `netdiag.Model` on the Path tab, fake engine, `WindowSizeMsg{Width: 80, Height: 24}`):
- **Criterion 3.** Resolved with a 40-character interface name and a 60-character host name, 30 hops
  (mixed router/silent/target), done. Every line of `Render()` has `ansi.StringWidth == 78`
  (`== pathModel.table.RenderedWidth() == width-2`, Rule 116); every line of `RenderFooter(80)` has
  width ≤ 80; the stripped body contains `this machine`, the truncation mark on the interface row and
  no `\x1b`. A cut escape would eat visible cells, so the exact-width assertion is what detects it.
  Repeat at width 60.
- **Criterion 4.** Same model, `Height` small enough that 33 rows do not fit: initially the body shows
  `this machine` and not TTL 30; `end` → TTL 30's row visible, `this machine` not; `home` → back.
  `pgdown` moves by a page. Run the keys in editing, running and done states (the table takes them in
  all three).
- Guard: `TestActivityGuardScansThePathTabSources` lists `path_steps.go`; `path_run.go` still out.
Command: `go test -race -count=1 ./internal/ui/netdiag && go test -count=1 -run 'Activity|Guard|EveryColumn|IconColumn' ./internal/app ./internal/ui/datatable ./internal/ui/agentusageview`
Done when: green, including `TestEveryColumnDeclaresItsSizing` and the activity guard.

### Step 5 — documentation — after 4 — S

- `docs/architecture/network.md`:
  - "Network Diagnostics View", Path tab entry: columns `Hop, Step, Address, RTT`; the column starts
    with `source` (`this machine`), `interface` (name + source address), `gateway`, then the hops;
    file `path_steps.go`.
  - "The Path tab — streaming, cancellation, resolution": the route lookup (D-a) — `Env.Route` on the
    traced address, in the producer, carried by `pathResolvedMsg`, no extra message (capacity still
    32), non-fatal, raw error logged only; why it agrees with the probes (go-netroute asks the kernel
    `RTM_GETROUTE`, policy rules applied; the probe socket is unmarked and unbound).
  - Why the source is never a host name (D-c).
  - Activity guard: `path_steps.go` scanned.
- `docs-site/docs/explanation/network.md` § "The Path tab": the first three rows and what they mean
  (VPN case: the interface row names the tunnel).
- `.claude/CLAUDE.md` overview line on network diagnostics: "a Path tab (native path trace, Linux only;
  the column starts with this machine, its interface and gateway; `H` …)".
- `docs/backlog.md`: one D entry for D-a…D-d (archivist). No ADR: local, reversible, inside one view.
Command: `mise run ci`.

## Manual check (PR description, Linux)

1. `dk` → `:net` → Path → `1.1.1.1` → enter: rows `source / interface / gateway / 1…` and the
   interface matches `ip route get 1.1.1.1` (`dev`, `src`, `via`).
2. With a full-tunnel VPN up (wg-quick or OpenVPN): the interface row names the tunnel, as
   `ip route get 1.1.1.1` does.
3. Terminal resized to 80 columns: no wrapped line; long column scrolled with `end`/`home`.

## Risks

- **Route and trace taken a few milliseconds apart.** A VPN connecting in between makes the
  interface row describe the old route. Accepted: the window is the lookup (~2 ms) and the user
  relaunches; the Diagnostics route check has the same property.
- **go-netroute errors with `no source IP found`** on some routes (blackhole, unusual tables): shown
  as `undetermined`, trace unaffected.
- **Linux only in practice.** Off Linux the tab cannot launch (S-08), so `route` is never called there;
  every test uses the fake.
- **Test churn.** The item type change touches about six existing assertions (listed in step 3);
  none changes a behaviour of DEVDESK-328.
- **Irreversible:** none. No config key, no persisted format, no public API (`internal` packages; the
  `pathEngine` seam is unexported).

## Effort

S overall (hypothesis (b) holds: a `datatable` already, navigation already wired). Steps 1–5 are each
S; steps 1 and 2 can run in parallel.
