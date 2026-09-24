# Container engine, OCI registries, network diagnostics, sockets and interfaces

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## The container engine — `internal/engine` and `internal/docker`

**`internal/engine`** names the engine in use — docker or podman — and carries
everything that is not the same between the two: the binary, the credential
helper prefix, the auth file, the host socket, and the `--format` templates. It
is its own package rather than a field of `internal/docker` because
`internal/scan` needs the same answer and does not import `internal/docker` —
`internal/forge`'s arrangement read one level down (§3.67).

`engine.Resolve(preference)` turns `app.container_engine` into a `Shape`:
`auto` takes docker if it is on PATH and podman otherwise; a pinned engine that
is absent is an **error**, never the other one, because the two do not hold the
same containers. Anything else is taken as a path to a binary. The router
resolves once at startup and once per context switch, into `engine.SetCurrent`.

A `Shape` carries **no resolved filesystem path**: `AuthPaths()`, `AuthPath()`
and `HostSocket()` are methods. The shape is built at package initialisation, so
a `$HOME` read there would be frozen before anything else runs — and whether the
socket exists is the whole question, since `podman system service` can start or
stop while the application is up.

**`internal/docker`** drives that engine. Every invocation goes through one
seam — `dockerRunner`, implemented by `cliRunner` (`exec.go`) — which is what
made podman cheap: the binary name lived in exactly one place. The package is
split by resource: `containers.go`, `images.go`, `networks.go`, `volumes.go`,
`registry.go`, `system.go`, `launch.go`, plus `exec.go` (the seam), `parse.go`,
`container_ports.go`, `identifier.go` and `names.go`. It is **not** called
`internal/container`: renaming it would touch 40 files for a vocabulary gain.

Two things about the engine are worth knowing before changing this code:

- **The `--format` templates are per engine and identical today.** Podman
  implements `--format`, but equality of the *fields* is guaranteed nowhere, and
  a divergence raises no error — it produces wrong rows. No measurement has been
  taken against a real podman; `TestTheTwoTemplateSetsAreStillUnmeasured` fails
  the day the two sets stop matching, so a divergence is recorded deliberately.
- **`system df -v` is scraped, not templated**, and is therefore disabled under
  podman (`Shape.ParsesSystemDFVerbose`). It is a fixed-width table read at
  offsets taken from its header line, and there is no `--format` that exposes
  per-image unique size.

- `internal/docker/netdiag.go` — **gone**. §3.33 took the five probes that did not need a container, §3.43 the ports table, §3.44 the topology (and with it `docker/topology.go` and the last `--privileged`), §3.47 the route trace and the file itself. §3.61 removed the OCI connectivity test, and with it the last container this application runs for a diagnostic: `networks.go` now holds only `ListNetworks`, `CreateNetwork`, `RemoveNetwork`, `PruneNetworks` and `InspectNetwork`.

## OCI registries — `internal/oci`

`internal/oci/oci.go` — an OCI **registry** HTTP client: list tags,
download and extract a tar.gz (a repository template is one — `templates.md`). It speaks the distribution spec over `net/http`
and never shells out, so it is engine-agnostic and podman changed nothing in it.

## Image updates — `internal/imageupdate` (§3.88)

Three tables carry an **Update** column (`internal/ui/updatecol`): oci's Images
tab, the containers list, and the Remediation tab of a scan result. It shows an
arrow when the image's registry holds something newer, and says what:

| Kind | When | Label |
|---|---|---|
| `NewPatch` | the tag has ≥ 3 version components and a tag differing only by the last, same variant, is higher (`20.11.1-alpine` → `20.11.4-alpine`) | the tag |
| `NewBuild` | the digest the tag points to now is none of the local image's digests | `new build` |

A newer patch wins over a new build. Every other state says why there is no
arrow, in grey — a blank used to stand for all of them, which in the
Remediation tab (bases rarely pinned, often not held locally) meant a column
that was silent without saying why:

| Kind | Cell | When |
|---|---|---|
| `UpToDate` | the check mark | the local digest is the tag's |
| `Pending` | `checking` | no answer yet |
| `Failed` | `?` | the registry did not answer (logged) |
| `LocalBuild` | `local build` | images, containers: no registry digest |
| `NotLocal` | `not local` | Remediation: unpinned, and not held by the engine |
| `Pinned` | `pinned` | a digest with no tag: nothing can move |
| `None` | blank | the question does not apply (untagged image, unresolved `FROM`) |

`Evaluate` takes what "no local digest" means to its caller (`LocalBuild` or
`NotLocal`). The column's filter only matches an update's label, or `/ca`
would find every `local build`. A tag with fewer components (`3.20`,
`20`) has no patches of its own — it floats over them, so a new patch moves the
tag and the digest says it. That is also the only answer a floating tag
(§3.79) ever gets.

**The registry's side is cached, the comparison is not.** `imageupdate.Check`
keeps, per reference, the tag's digest and the newer patch tag in
`~/.devdesk/cache/image-updates.json` — 6 hours for an answer, 30 minutes for a
failure (unknown repository, no credentials) so it is not asked on every
refresh. `Evaluate` compares with the local digests each time a row is built,
so a pull clears the arrow without asking the registry again.

**Two requests at most per reference.** The digest is a `HEAD` on the manifest
(`oci.ManifestDigest`) accepting an OCI index and a Docker manifest list — a
multi-platform tag's digest is the index's, the one `docker pull` records in
`RepoDigests`. Docker Hub does not count a `HEAD` against its pull rate limit.
The tag list (`oci.ListRegistryTags`) is only read for a tag with a patch
component. At most four references are asked at once; credentials are the
engine's (`docker.GetStoredCreds`), as for the Remediation tab.

**What each view compares with:**

- *Images* — the image's own `RepoDigests`, now read by the same `image
  inspect` that reads its size (`templates().ImageInspect`). Only a tagged image
  **with** a digest is asked about: one built or loaded here has none, and its
  name would send the question to a registry that never heard of it.
- *Containers* — the digests of the image the container was **created from**
  (`docker.ContainerImageDigests`: `container inspect` for the image ID, then
  `image inspect`). After a pull the container still runs the old image, so the
  arrow stays until it is recreated — which is what it is there to say. The
  digests are read again only when the set of containers changes, not on every
  two-second refresh.
- *Remediation* — the digest the Dockerfile pins, or else the image the engine
  holds under that name (`docker.ImageRepoDigests`). A base the engine does not
  hold only gets the patch side.

`imageupdate.Tracker` is what each view keeps between checks: the facts it has
and the references a check is out for, so a reload asks only what is new or
stale. Every view reaches the network through a package variable its tests
replace.

**On a narrow terminal**, the containers table drops the two gauges before
Update (they are `DropFirst`, §3.71), then the I/O counters, then Update. Both
gauges now need 180 columns instead of 160.

**Not done**: a digest for a tag the engine holds under another name, and the
`podman` side of `RepoDigests` has not been measured against a real `podman
system service` — the field exists there under the same name.

## Network Diagnostics View

`internal/ui/netdiag/` — four tabs:
- **Diagnostics tab** (`model.go`): target, port and resolver, then
  `internal/netcheck`'s pipeline — resolve, route, reach, connect, TLS, HTTP —
  chained one stage per message so the footer can name the question being asked.
  The seven tool checkboxes are gone (§3.33): the checks follow from the target
  and from what has already failed.
- **Ports tab** (`ports_model.go`): the machine's own socket table, re-read
  every `ports_refresh_interval`, filtered by protocol, state and text. `K`
  terminates a process, after a confirmation. It runs **no container** — see
  `internal/ports` below.
- **Interfaces tab** (`interfaces_model.go`): this machine's network interfaces
  — name, state, MTU, MAC, RX/TX error counters, addresses — in a `datatable`.
  It runs **no container**; see `internal/netiface` below. It was the Topology
  tab, and §3.44 is why three of its four sections are gone rather than
  translated.
- **Forward tab** (`forward_model.go`, `forward_form.go`): the port
  redirections, `N` to open one, `space` to pause or resume it and `K` to delete
  it. It holds **no listener** — see `internal/forward` below.

**Timing (§3.66).** `netcheck.Check` carries a `Duration time.Duration`
alongside `Summary`/`Facts` — zero means untimed. `stage_connect.go` and
`stage_tls.go` fill it from the dial and the handshake respectively;
`stage_http.go` fills it from an `httptrace.ClientTrace` attached in
`env.go`'s `Head`, which also gives `HTTPResult` a DNS/connect/TLS/TTFB
breakdown surfaced as `Facts` on the `HTTP` row. Nothing keeps a history
across runs — that stays an open question.

That breakdown also drives a waterfall: `Check.Phases` is a set of named,
non-overlapping spans that sum exactly to `Duration` (nil on checks with
nothing to break down — TCP, TLS). `stage_http.go`'s `httpPhases` derives
**Wait** and **Content** rather than charting TTFB directly, because TTFB is
measured from the start of the request and already contains DNS+connect+TLS —
charting it as a fifth independent share would double-count that overlap.
`internal/ui/netdiag/view.go`'s `phaseLines` renders one proportional bar per
phase, reusing the load-gauge machinery (`theme.Gauge`, `GaugeFillWidth`,
`GaugeTrackStyle`) with a new neutral fill colour, `theme.TimingFillStyle` —
a phase taking most of the time is a fact to read, not a severity to spot.

**Opened prefilled, from elsewhere (§3.66).** `netdiag.NewWithTarget` builds a
view with the form filled in but not yet run; `netdiag.OpenRequestMsg` is
what a producer elsewhere sends to ask the router for one (mirroring
`uiviewer.OpenRequestMsg`), and `netdiag.BackToOriginMsg` is how `esc` gets
back out, via `Model.OriginView` — empty when netdiag was opened directly
from the command line, in which case `esc` behaves exactly as it always has.
`status`'s `H` (`keymap.Diagnose`, `internal/ui/status/diagnostics.go`) is
the first and only producer: it resolves the selected monitor's target and
auto-runs the pipeline when the port is certain (http/https/ssl), or lands on
the prefilled form when it is a guess (icmp/dns).

**What is configurable, and what is not.** `internal/netcheck` held its timeouts
as constants with a comment saying they would become settings when somebody
asked; `network:` is what they became.

| Setting | Replaces |
|---|---|
| `check_timeout` | five per-stage constants — 5 s for DNS and the dial, 8 s for TLS and HTTP, 4 s for the ping |
| `ping_count` | `pingCount` |
| `cert_expiry_warn_days` | `expiryWarnWindow` |
| `ports_refresh_interval` | the 2 s tick |

**One timeout replaces five**, and the default is the old *maximum* so nothing
that answers today starts failing. The 5 / 5 / 8 / 8 / 4 split was never argued
anywhere — five rows for one idea, and each of the five a separate guess. The
cost is stated rather than discovered: an unreachable host now spends 8 s on DNS
instead of 5, which the staged progress line makes legible rather than a hang.

`traceroute_max_hops` was a sixth setting and went with the trace it bounded
(§3.47).

Two things stay hardcoded, each for a reason:

- **`MinVersion: VersionTLS10`** — the handshake is *probing*, not securing.
  Reporting an old version is the point; a setting could only make the tool
  blind to what it exists to find.
- **`InsecureSkipVerify`** — the TLS stage verifies the chain itself so it can
  say *which* part failed. A setting here would collapse four checks into one
  error string.

`netcheck.Settings` travels **beside** `Env`, not on it: `Env` is the seam to
the network, while `PingCount` and `ExpiryWarnWindow` are read by stages and
never by a network call — putting a preference behind the seam would make every
fake answer for one. `Settings.Normalized()` fills in anything non-positive at
every entry point, so a hand-edited `0` becomes the default rather than a dial
with no deadline. The view calls `checkSettings()` per run rather than capturing
at construction, so a run already in flight and the config cannot disagree
halfway down the pipeline.

Every probe here answers from the DevDesk process. The route trace was the one
exception and §3.47 removed it: `--network host` on Docker Desktop is the VM's
namespace, so it traced a path from somewhere else. Nothing shells out.

**The `route` stage answers the question no other check can** (§3.44): when a
VPN captures the default route, a host that is plainly reachable elsewhere is
unreachable here, and every other row reports the *symptom* — no ICMP reply, no
TCP connect — while none reports the cause. `Env.Route(ctx, ip)` returns a
`RouteHop{Interface, Source, Gateway}`, and the summary names the **interface**
because that is what the user recognises: "Traffic leaves through ProtonVPN"
answers the question, `10.2.0.1` needs a second lookup to mean anything.

`github.com/libp2p/go-netroute` is what makes it one implementation instead of
three — `GetBestRoute2` on Windows, an `RTM_GETROUTE` netlink query on Linux, the
routing socket on the BSDs — with no privilege anywhere and ~2 ms per lookup,
measured. It depends only on `x/net` and `x/sys`, both already in the graph.

Four decisions, each with a test:

- **It gates nothing**, for `reach`'s reason: a machine whose routing table
  cannot be read still has a perfectly answerable question about the port.
- **A lookup that fails is `Unknown`, never `Fail`.** "Could not determine the
  route" is not "there is no route", and rendering the first as the second is
  D20. The platform error is also **localised** — Windows returns
  `ERROR_NETWORK_UNREACHABLE` in the machine's own language — so it goes in a
  fact and never in the summary (Rule 129).
- **One family without a route is a `Warn` with its count.** That is the IPv6
  case, and it is worth reporting: a client that prefers IPv6 hangs before
  falling back.
- **It takes its addresses from the `resolve` check's facts**, capped at
  `maxRoutedAddresses`, rather than resolving again — on a round-robin name the
  two could diverge and the route would describe an address no other check in the
  run ever touched. `TestTheRouteStageReadsWhatTheResolveStageWrote` pins the
  coupling, which is otherwise the kind that breaks in silence.

## The socket table — `internal/ports`

The machine's TCP and UDP sockets, and the process holding each, read **in this
process**. `List(ctx, resolve)` and `Kill(pid)` are the whole interface.

**It exists because the old one answered for the wrong machine** (D55). The
Ports tab ran `ss -tupan` inside
`docker run --rm --net=host --pid=host --privileged`, and on Docker Desktop
`--net=host` is the namespace of the Linux VM: the tab listed the VM's NFS
daemons with two-digit PIDs while not one of the host's 38 listening sockets
appeared, and `K` sent SIGKILL to a process of the VM under the impression it was
freeing a port on the machine. The sentence explaining this has sat at the top of
`docker.runDiagHost` since §3.33 rapatriated DNS, ICMP, TCP, TLS and HTTP for
exactly the same reason; `RunSS` and `KillProcess` were the two nobody pulled it
for. **Under Linux the defect did not exist**, which is why it went unnoticed:
the tab was right on the platform it was written on.

No new dependency: `gopsutil/v4/net` was already imported by
`internal/metrics/host.go`. Windows reads iphlpapi, Linux `/proc`, macOS and the
BSDs `lsof` — the last is a subprocess, but one the system supplies, and it
replaces a privileged container.

Four decisions, each with a test:

- **The process names come from one enumeration, not one call per socket.** The
  obvious `process.NewProcess(pid).Name()` goes through `OpenProcess` on
  Windows and needs rights over the target: measured here, **98 of 189** sockets
  came back *Access denied*, so the Process column would have been empty for
  every service on the box. `Processes()` reads a Toolhelp32 snapshot instead
  and named **294 of 294** in 11 ms, elevated or not. The bulk call is not an
  optimisation over the per-PID one — it is the difference between a column that
  is filled in and one that is not.
- **A PID of zero carries no PID.** It is the system declining to attribute the
  socket, and `K` keys on that field: left as `"0"` the row would look killable,
  and process 0 is a whole process group on Unix. `Kill` refuses it a second
  time, on the model of §3.23's double guard. The name is a *separate* question:
  a row may know which process holds the socket and not what it is called, and
  it must stay killable — that is the row the user is acting on.
- **Two states are normalised, and neither is cosmetic.** An unconnected
  datagram socket is `NONE` on Linux and nothing at all on Windows, so the same
  socket read differently depending on where DevDesk ran — and the `l`/`e`
  filters with it. `ESTABLISHED` is shortened to `ESTAB` because the State
  column is ten cells wide, so the long spelling renders as `ESTABLISHE`.
- **`n` resolves the host half only.** `ss` without `-n` also turned 22 into
  `ssh`; Go resolves a name to a port and not the other way round, so honouring
  that would mean shipping a copy of `/etc/services` and calling the result the
  system's answer. Saying less beats saying something the system did not.

The reverse-DNS cache is **package-level**, because the lookups run inside a
`Cmd` and a `Cmd` may not touch the model (Rule 110). It caches the failures
too: a machine talking to hosts with no PTR record would otherwise re-ask for
every one of them on every two-second tick — the storm the cache exists to
prevent, arriving through the failures instead of the successes. Wildcard,
loopback and unspecified addresses are never asked at all.

**`Kill` signals with the rights DevDesk has**, which is the visible change:
another user's process, or a service, now comes back refused by the operating
system instead of succeeding against the wrong machine.

**So the refusal has to be legible, and `K` has to say when it cannot even
try** (§3.49). Two different questions, and only one of them is answerable
before the keypress:

- **No PID, no key.** A socket the system declines to attribute carries an empty
  `PID`, and `K` is greyed on that row (Rule 130) — it used to be advertised
  everywhere and warn only once pressed.
- **Whether the OS will accept the signal is the attempt's answer**, never the
  header's, so a row with a PID stays lit even when the kill is certain to be
  refused. `killFailureMessage` classifies the failure with `errors.Is` through
  the `%w` wrapping `Kill` applies: `os.ErrPermission` reads *Refused by the
  system*, `os.ErrProcessDone` reads *no longer running*, anything else keeps
  the generic line. `Failed to kill PID N` made those the same sentence.

The platform error never reaches the screen — measured, PID 4 on this machine
answers `OpenProcess: Accès refusé.`, in the machine's own language, which is
the route stage's rule (§3.44). Also measured, and written down rather than
discovered: **the "already gone" branch does not fire on Windows**, where a
nonexistent PID fails `OpenProcess` with `ERROR_INVALID_PARAMETER` and maps to
neither sentinel. Mapping that code would be a guess, and the row disappears on
the next two-second refresh anyway.

**Nothing in the application runs `--network host` any more** (§3.47). §3.33
brought DNS, ICMP, TCP, TLS and HTTP into the process, §3.43 the socket table,
§3.44 the interfaces, and §3.47 removed the route trace — the last one. That is
the checkable form of "DevDesk never claims to answer for a machine that is not
yours", and it closes **D57**.

**The route trace was deleted rather than rewritten**, and the reasons are worth
keeping because they are the shape of the decision, not of this feature:

- it answered for the **wrong machine** — `--network host` traced from the Docker
  Desktop VM, so the hops were the VM's;
- what people came for has a **better answer**: `netcheck`'s Local route check
  names the interface and source address from *this* machine, in 2 ms, which is
  the split-tunnel question;
- rewriting it was **three unmeasured unknowns** — reading ICMP `TIME_EXCEEDED`
  without a raw socket on three platforms, a per-packet TTL `pro-bing` does not
  expose, and a TCP mode needing the ICMP error of an outgoing connection.

`H` went back into `keymap.Free()` beside `J`, `Q` and `Z`: a letter an action
has just released is redeclared free, or it stays reserved for something that no
longer exists.

The OCI connectivity test — the last container DevDesk started from a
network-inspect overlay, and with it `network.connectivity_image` and its
three-link migration chain (`docker.network_tool_image` →
`network.tool_image` → `network.connectivity_image`) — has been removed. The
network-inspect overlay itself (`:oci` → Networks → `enter`) stays; only the
`c` key it used to offer, and everything behind it, is gone.

## The port forwarder — `internal/forward`

A local TCP port redirected to a `host:port`, **in this process** and with no
privilege: a `net.Listen` and two `io.Copy`. `Registry.Open(port, target)`,
`Toggle(id)`, `Close(id)`, `CloseAll()`, `List()`, `Restore(entries)` and
`Entries()` are the whole interface; the messages a view exchanges with the router live in the same package so a view
never imports `internal/app`.

**The router owns the registry** (`shared.State.Forwards`, built once in
`newWithSize`). `reinitializeViews` drops every view on a config save and on a
context switch, so a listener held by the Forward tab would stay bound with
nothing left to close it. The tab asks (`forward.Open` / `Close` / `Refresh`) and
is told (`forward.ChangedMsg`, broadcast to every held view, on screen or not).
Forwards **survive a context switch** — one belongs to no context.

### Persistence (§3.75)

The **listener** cannot outlive the process, but the **intent** can: every
forward is written to `~/.devdesk/forwards.yaml` and reopened at the next launch.
It is a file of its own rather than a key of `config.yaml`, because the config
is per context and is reloaded on every switch, and a forward belongs to no
context.

| Piece | Where | Does |
|---|---|---|
| `Store` (`store.go`) | `internal/forward` | `Load` / `Save` the `[]Entry` (`local_port`, `target`, `paused`), `version: 1` |
| `Registry.Entries()` | `internal/forward` | describes what is *wanted*, in creation order, live or not |
| `Registry.Restore()` | `internal/forward` | reopens the entries at startup, binds in parallel |
| `restoreForwardsCmd` / `saveForwardsCmd` | `internal/app/forward.go` | the router's two Cmds; the list is copied in `Update` (Rule 110) |

A forward has a `State`:

| State | Meaning | In the file |
|---|---|---|
| `StateLive` | listener bound | an entry |
| `StatePaused` | the user stopped it; port released, row kept | `paused: true` |
| `StateUnbound` | wanted, not bound — port taken or target silent at the last attempt; `LastErr` says which | an entry (retried at launch) |

- **A refusal at creation deletes nothing; a failure at launch keeps the entry.**
  `Open` still refuses a typo, opens no row and writes nothing — the form's
  business. `Restore` answers a service that is not up yet, or a port some other
  process holds today, and forgetting the route for that would make the file
  lose things by being started at the wrong moment. The row reads unbound and
  `space` tries again.
- **`space` is Pause/Resume** (Rule 111's *Control* key), not a new capital. A
  resume is a bind like any other: probe, then listen. A failure is returned and
  recorded, so the row and the footer say the same thing.
- **`K` deletes**: the row and its entry, after a confirmation that says it will
  not come back.
- **Order is the creation order** (`entry.seq`), not the last bind and not the ID
  as text — `"10"` sorts before `"2"`, and a restored file creates its rows in
  the same instant. A row does not move when it is paused and resumed.
- **`serve` is handed its listener.** A pause clears `entry.ln`; an accept loop
  that read the field would be the goroutine dereferencing it.
- **The write is atomic** (temporary file, `Sync`, `Rename`) and serialised by a
  mutex: two `Cmd`s can be in flight, and the older list must not land last.
- **An unreadable file is refused, not treated as empty.** `Load` returns
  `ErrUnreadable` naming the file and touches nothing; the first save afterwards
  moves it to `forwards.yaml.unreadable` instead of overwriting it.
- **A test never writes the developer's home.** `newWithSize` sets no store;
  `New` calls `useForwardStore`, the way it calls `useSecrets`.

**Two instances share the file.** `~/.devdesk/` is shared, so two DevDesks write
`forwards.yaml` and the last write wins; the second one's reopening then finds
the first one's ports taken and reads unbound rather than failing. There is no
file lock — the atomic write keeps the file whole, not merged.

### Named routes (§3.74)

A forward with a **name** is an HTTP route instead of a TCP redirection. One
`http.Server` on `127.0.0.1:network.proxy_port` serves every route, and the
request's `Host` header picks the target: `http://api.localhost:8080` and
`http://app.localhost:8080` differ by name, not by port. No DNS entry, no
privilege — `*.localhost` is reserved for the loopback (RFC 6761), and the
platforms measured resolve it unaided.

| Piece | Where | Does |
|---|---|---|
| `proxy.go` | `internal/forward` | `OpenRoute`, `SetProxyPort`, the proxy's lifecycle, `serveHTTP` |
| `Entry.Name` | `store.go` | a route is an entry with a name and no `local_port`; the type is *deduced from the name*, so no state has a route without one |
| `network.proxy_port` | `internal/config` | the port, 8080 by default, per context (see below) |
| the form's `Type` field | `forward_form.go` | `TCP` / `HTTP`, a cycle field (Rule 132); only the fields that apply are shown |

- **The proxy binds with the first served route and closes with the last.** Its
  lifecycle is serialised by `proxyMu`; lock order is `proxyMu` then `mu`, and the
  request handler takes `mu` only, so serving never waits on a bind.
- **`Host` is matched without its port, trailing dot or case.** An unknown host,
  or bare `localhost`, gets a 404 that lists what *is* served. A target that does
  not answer gets a 502 naming it, and the route's `LastErr` records why.
- **The inbound `Host` is forwarded to the backend**, not replaced by the
  target's: an application behind a named route often keys on it. `WebSocket`
  upgrades pass (`httputil.ReverseProxy` handles them), which is what hot reload
  rides on.
- **A name must be a valid host name ending in `.localhost`**, unique whatever
  the state of the route that owns it — a paused route keeps its name, or
  resuming it would find it gone. Stored in lower case. No suffix completion in
  the form: what is typed is what is stored, and a name that does not end in
  `.localhost` is refused there before it reaches the registry.
- **A route has no port of its own.** `List` fills `LocalPort` with the proxy's,
  which is the number that goes in the URL. A TCP forward asking for that port is
  refused (`ErrProxyPortTaken`), and the proxy's own bind failing is
  `ErrProxyPortInUse` — not `ErrPortInUse`, whose sentence says to pick another
  port in a field a route does not have.
- **The proxy's `Server` and its listener are closed separately.** `Server.Close`
  reaches the listener only once `Serve` has registered it, on a goroutine; the
  registry closes the listener itself so that the port is free when the stop
  returns and a pause-then-resume can rebind at once.
- **The port follows the configuration.** It is per context, and a forward
  belongs to none, so a switch that changes it moves the proxy: `syncProxyPortCmd`
  runs on a context switch and on a saved configuration. It records the wanted
  port in `App.wantedProxyPort` in `Update` and the `Cmd` applies the value as it
  is *when it runs* — two moves in flight leave the proxy on the latest port
  whatever order they finish in. If the new port cannot be bound, every live
  route becomes unbound with the reason and nothing leaves the file.
- **Restore probes the targets, then binds the proxy once**, and only if a route
  passed: a file whose routes are all silent binds nothing.
- **Restore holds the file to the rules of `OpenRoute`.** A name is normalized
  (lower case) and validated, and a second entry for a name is refused — the
  file can be edited by hand, and a name that was never normalized would read
  live and never match a request. A refused entry is kept, unbound, with the
  reason, and is not rewritten. Resuming a paused route also refuses a name a
  live route already answers to.
- **A new proxy port retries the routes a bind failure left unbound.** Without
  it, correcting a taken `network.proxy_port` would report success and leave
  every route unbound with an error naming the port just replaced.
- **A resume marks the route live under `proxyMu`**, the lock a pause takes to
  decide the proxy is idle. Released in between, a pause of the last live route
  could close the proxy under a route that then read live with no listener. A
  test holds a resume in that window through an unexported hook and fails when
  the lock is released early.
- **A client that goes away is not the route failing.** A cancelled request does
  not set `LastErr`.

**A known race, left as it is.** `bindTCP` checks the proxy's port under `mu`,
while the proxy binds under `proxyMu`, and neither holds a lock across the other.
A TCP forward asked for the proxy's port at the very moment the first route binds
it can therefore lose the race and be refused with a generic port-in-use
sentence instead of the one that names `network.proxy_port`. Nothing is lost and
the window is a single `net.Listen`; closing it would mean holding `proxyMu`
across every TCP bind, which is worse than the message.

**Limits, stated rather than discovered.** HTTP only — `https://app.localhost`
needs a certificate the browser trusts, which is one more elevation. The port
stays in the URL (80 is privileged). A name outside `.localhost` does not
resolve. A client with its own resolver is not covered. The proxy adds no CORS
header: it makes nothing reachable that was not already on the loopback.

**Not yet measured:** browsers on Windows and macOS, and the `::1`-then-IPv4
fallback (the proxy binds IPv4 only). See §3.74.

Four decisions, each with a test:

- **Loopback only.** A forward binds `127.0.0.1`. `0.0.0.0` would put on the LAN a
  service its owner kept off it. The test asserts the listener's own address: a
  second bind cannot prove it, since `0.0.0.0` and `127.0.0.1` collide on a port
  in both directions.
- **Below 1024 is refused before the syscall**, and before the target is probed.
  There is no unprivileged way around it on Unix, and `permission denied` reads
  like something a retry would fix.
- **The target is dialled once before the port opens.** Otherwise the bind
  succeeds, the row reads healthy, and the failure only surfaces at the first
  client. Refusals are sentinels (`ErrPrivilegedPort`, `ErrPortInUse`,
  `ErrTargetUnreachable`), and `app.forwardRefusal` maps each to a sentence —
  the wording never comes from the operating system.
- **The registry has a mutex; `jobs.Registry` deliberately does not.** `jobs` is
  only written from `Update`. Here the accept and connection goroutines write the
  counters `Update` reads. The lock is never held across I/O, and a test closes a
  forward while a connection is live.

**A container's own address is not a target on Docker Desktop.** Measured on
Windows: `172.17.0.3:80` for an unpublished `nginx` gives `i/o timeout`. The
address is real inside the Linux VM and routes nowhere outside it — the same
mechanism as D55 for the Ports tab. It works on native Linux. The probe is what
turns that into a named refusal instead of a healthy-looking dead row, and it is
why the container pre-fill was not built (§3.1).

A forward records its local port and its target, and nothing about where the
target came from. It briefly carried a `Label` for a container name, with a
`Source` column to show it; both were removed because the pre-fill that would
have filled them was not built, so the column only ever read `-`.

## The interfaces — `internal/netiface`

This machine's network interfaces, read **in this process**. `List(ctx)` is the
whole interface.

**It exists because the old one answered for the wrong machine** (D57), and it
is `internal/ports`' story exactly one screen over. The Topology tab ran
`ip addr`, `ip -s link`, `ip route`, `ip neigh` and `iptables` in containers
started with `--network host`, which on Docker Desktop is the Linux VM's
namespace: the tab showed `eth0 10.254.254.3`, `docker0` and the `br-*` while
the machine had `Ethernet 2`, ProtonVPN, Tailscale and **two competing default
routes**. Not one interface and not one route in common. Under Linux the defect
did not exist, which is why it went unnoticed.

No new dependency: `net.Interfaces()` is the standard library and
`gopsutil/v4/net` was already imported. Measured here: 10 interfaces in 4,6 ms,
10 counter rows in 2,6 ms, and **0 of 10 interfaces without a matching counter
row** — the names agree character for character, so there is no correspondence
table to keep.

**The addresses are split by family, one column each** (§3.49). The split
happens in `List`, where each address is still a `net.IP` and the family is a
fact — `To4()` answers for an IPv4-mapped address as well as for a plain one,
which is right, it *is* an IPv4 address. A view splitting `AddressList()` again
would be parsing text this package produced, and would have to decide what an
unparseable entry means: a question that only exists once the type has been
thrown away. Hence `IPv4 []string` and `IPv6 []string` rather than one
`Addresses`.

Two consequences in the view:

- **Both columns follow their content and both carry a flex weight.** The flex
  is what levels them against each other when there is a shortfall — a table
  reclaims from the widest content column first — and with the flex on IPv6
  alone it absorbed the whole shortfall and rendered at **zero width, header
  included, from about 100 columns down**.
- **The cliff below 88 columns is gone** (§3.45). It was written down here
  rather than fixed, because `MinWidth` was an ask and giving the solver a floor
  would have changed every table in the application; §3.45 changed every table
  in the application. What gives way now is MTU, then MAC, then the two
  counters — the columns that declare `Optional` — and each is removed whole, so
  it hands back its padding as well as its width. At 80 columns the addresses
  get 18 and 29 cells instead of nothing.

Three decisions, each with a test:

- **`RxErrors` and `TxErrors` are `*uint64`.** The counters come from a second
  source that fails on its own, and a zero written because nobody looked is
  indistinguishable from an interface that has dropped nothing. `nil` renders as
  `-`, never `0` — D58 at the scale of a column, and `SecretVerdict`'s `*bool`
  under another name.
- **A counter failure does not fail the listing.** `List` returns an error only
  when the interfaces themselves could not be read: a list without its error
  counts is still the answer to "which adapters does this machine have".
- **An MTU the platform does not report is withheld.** Windows returns `-1` for
  its loopback pseudo-interface where `ip` returns 65536; `HasMTU` is what keeps
  a cell from printing a number the machine never meant.

**Three sections were removed rather than translated**, and §3.44 measures why
for each: the ARP cache loses IPv6 and four of its six states outside netlink,
the firewall's `{Name, Policy, Rules}` is an iptables shape that Windows profiles
and `pf` do not fit, and the routing *table* became a routing *question* — see
the `route` stage below.

**The tab is named after what it shows.** At one section "Topology" described
nothing, and deleting it would have meant rehousing the interfaces in Diagnostics
or Ports, where neither has room. One consequence has a test: the table has a
search box where the old tab had no input at all, so `InEditMode()` had to stop
returning a hardcoded `false` — otherwise a `:` typed into the query opens the
command line (Rule 111).

