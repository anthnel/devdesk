# Docker/OCI, network diagnostics, sockets and interfaces

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## Docker / OCI Integration

- `internal/docker/client.go` — wraps Docker CLI (exec-based): list, metrics, stop, restart, pause, remove, prune
- `internal/docker/netdiag.go` — **gone**. §3.33 took the five probes that did not need a container, §3.43 the ports table, §3.44 the topology (and with it `docker/topology.go` and the last `--privileged`), §3.47 the route trace and the file itself. `RunDiagnosticContainer` in `networks.go` is the one container runner left, and it attaches to a Docker network rather than to the host
- `internal/oci/oci.go` — OCI registry HTTP client: list tags/templates, download + extract tar.gz

## Network Diagnostics View

`internal/ui/netdiag/` — three tabs:
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

