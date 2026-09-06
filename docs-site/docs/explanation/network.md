# Network

DevDesk's network diagnostics — reachability checks, the local socket table,
and the network interface list — all run *inside the DevDesk process itself*,
never inside a privileged container. This page explains why that constraint
exists, what it cost to satisfy, and how the diagnostics pipeline is built
around it.

## Why nothing runs in a privileged container anymore

Earlier versions of these views ran diagnostic commands (`ss`, `ip addr`,
`iptables`, and similar) inside containers started with
`--network host --pid host --privileged`. On Docker Desktop, `--network
host` is the namespace of the Linux VM that backs Docker, not the host
machine — so these tools were answering for the *wrong machine* entirely.
Concretely, the socket table listed the VM's own NFS daemons with low PIDs
while none of the actual host's dozens of listening sockets appeared, and
sending a "kill" signal to one of those PIDs killed a process inside the VM
under the impression it was freeing a port on the host. The interfaces view
had the same problem: it showed the VM's `eth0` and `docker0` while the real
machine had a completely different set of adapters (a VPN, a mesh network
interface, two competing default routes) with not one name or route in
common.

Under native Linux this defect didn't exist — the container's host network
namespace *is* the real one — which is exactly why it went unnoticed for so
long: it was correct on the platform it was developed on and wrong
everywhere Docker Desktop is used.

The fix, applied one diagnostic at a time, was to read everything directly
from the DevDesk process using OS-level APIs instead of shelling out to
containerized CLI tools. Nothing in the application requests `--network
host` anymore, which is a checkable way of stating the underlying principle:
**DevDesk never claims to answer for a machine that isn't the one it's
running on.**

## The Diagnostics tab

`internal/netcheck` runs a pipeline against a target: resolve, route, reach,
connect, TLS, HTTP — one stage per step, chained so the UI can name exactly
which question is currently being asked. Earlier versions exposed a set of
checkboxes letting the user pick which tools to run; those are gone, because
which checks make sense follows automatically from the target and from
whatever has already failed.

### One configurable timeout instead of five

The pipeline used to hardcode five different per-stage timeouts (5s for DNS
and the initial dial, 8s for TLS and HTTP, 4s for ping) — a set of values
that was never really justified, just five separate guesses. A single
`check_timeout` setting replaces all five, defaulting to the old maximum so
nothing that used to succeed starts failing. The cost is explicit rather
than hidden: an unreachable host now spends up to 8 seconds on a DNS lookup
instead of 5, but the staged progress display makes that legible as "still
waiting on DNS" instead of an unexplained pause.

Two values stay hardcoded rather than becoming settings, each for a reason
tied to what the check is *for*:

- **`MinVersion: TLS 1.0`** in the handshake — the point of this probe is to
  detect an old TLS version, so making it configurable would let the tool
  become blind to exactly the thing it exists to report.
- **`InsecureSkipVerify`** — the TLS stage verifies the certificate chain
  itself, in order to say specifically *which* part of the chain failed. A
  single pass/fail from the Go standard library would collapse four
  distinguishable failure modes into one opaque error string.

### The route stage

Reachability failures usually look the same from the outside — no ping
reply, no TCP connect — regardless of cause. The route stage answers a
question none of the other checks can: *which interface will traffic
actually leave through?* When a VPN has captured the default route, a host
that's reachable from elsewhere becomes unreachable locally, and every other
check reports only the symptom. The route stage's summary names the
interface itself ("traffic leaves through the VPN adapter") rather than a
gateway IP address, because that's the form of the answer a person
recognizes without a second lookup.

This is implemented on top of `github.com/libp2p/go-netroute`, which
abstracts over three completely different underlying mechanisms — a
Windows API call, a Linux netlink query, and a BSD routing socket — behind
one interface, needs no elevated privileges, and resolves a route in about
2 milliseconds.

A few points shape how the route stage behaves:

- It never blocks anything else. A machine whose routing table can't be
  read still has a perfectly answerable question about whether a given port
  is open.
- A lookup that fails is reported as *unknown*, never as *no route exists*
  — those are different facts, and conflating them would misreport a local
  permissions or platform quirk as "this host is unreachable."
- If one address family (typically IPv6) has no route while the other does,
  that's reported as a warning with a count — worth surfacing, since a
  client that prefers IPv6 will hang before it falls back to IPv4.
- It reuses the addresses the `resolve` stage already found rather than
  resolving the target a second time, so the route it reports is guaranteed
  to describe an address some other check in the same run actually touched
  (important for round-robin DNS names, where two independent lookups can
  return different addresses).

## The Ports tab — `internal/ports`

The socket table lists this machine's TCP and UDP sockets and the process
holding each, read directly in-process via `gopsutil`. No new dependency was
needed — `gopsutil/v4/net` was already used elsewhere for host metrics.
Windows reads through `iphlpapi`, Linux through `/proc`, and macOS/BSD
shell out to `lsof` — a subprocess, but one the operating system itself
supplies, replacing what used to be a privileged container.

A few implementation choices matter beyond the basic "run in-process" fix:

- **Process names come from one bulk enumeration, not one lookup per
  socket.** The obvious approach — asking the OS for one process's name at
  a time — requires elevated rights over each target process on Windows;
  measured against a real machine, roughly half of all sockets came back
  "access denied" that way, which would have left the process column empty
  for most system services. Reading a full process snapshot once and
  matching by PID instead named essentially every process regardless of
  privilege level, in a fraction of the time. This isn't a minor
  optimization — it's the difference between a column that's populated and
  one that mostly isn't.
- **A PID of zero means "no attributable process," not "process ID zero."**
  The OS declining to attribute a socket to any process is different from
  actually knowing the PID, and the kill action is disabled specifically on
  that field being empty — process ID 0 has special meaning on Unix (a
  whole process group), so treating it as a normal killable PID would be
  actively wrong, not just imprecise.
- **Socket states are normalized across platforms**, because the same kind
  of socket (an unconnected UDP socket, say) reports a different raw state
  string — or none at all — depending on the OS, which would otherwise make
  filtering behave inconsistently depending on where DevDesk happens to be
  running.
- **Reverse DNS resolves only the host half of an address**, not port
  numbers to service names — turning `22` into `ssh` would mean shipping
  and maintaining a private copy of the system's service-name database and
  presenting it as though it came from the OS. Saying less is preferred
  over saying something that isn't actually the system's own answer.

The reverse-DNS lookup cache lives at package scope (outside the UI model),
because the lookups happen inside a background command and background work
is not allowed to touch UI state directly (see the app-wide "never mutate
model state outside `Update`" rule). It caches *failures* too, since a
machine that talks mostly to hosts without reverse DNS records would
otherwise re-attempt every one of those lookups on every refresh tick — the
exact kind of request storm the cache exists to prevent, just arriving
through failed lookups instead of successful ones.

### Killing a process

Signaling a process uses whatever rights the DevDesk process itself has —
which means killing another user's process, or a system service, now
correctly comes back as "refused by the operating system" instead of
silently succeeding against the wrong machine (as it effectively did when
run inside a privileged, host-networked container).

Because that refusal is only knowable after the attempt, the UI treats two
different questions differently:

- **No PID means the action is unavailable before you even try** — a socket
  the OS won't attribute to a process has the kill shortcut visibly
  disabled on that row, rather than appearing available and failing with an
  explanation only after the keypress.
- **Whether the OS actually accepts the signal is answered by the attempt,
  not predicted in advance** — a row with a real PID stays enabled even
  when the kill is likely to be refused, since guessing at permissions
  ahead of time can be wrong in both directions. The resulting error is
  classified rather than shown as a raw platform error string: a permission
  failure reads as "refused by the system," a process that already exited
  reads as "no longer running," and anything else falls back to a generic
  message.

## The Interfaces tab — `internal/netiface`

The interfaces list reads this machine's network adapters directly via the
standard library's `net.Interfaces()` plus `gopsutil` for counters — again,
no privileged container involved, and again motivated by the exact same
"answered for the wrong machine" problem described above.

A couple of representational choices are worth calling out:

- **IPv4 and IPv6 addresses are kept as separate typed lists**, split at the
  point where the address is still a `net.IP` and its family is an
  unambiguous fact — rather than joined into one list of strings that a
  later layer would have to re-parse (and guess about) to tell them apart.
- **Error counters are nullable (`*uint64`), not defaulted to zero.** These
  counters come from a second data source that can fail independently of
  the interface list itself, and a zero written because nobody could
  actually read the counter is indistinguishable from an interface that
  truly hasn't dropped a single packet — so a counter read failure renders
  as a dash, never a `0`, and a counter failure never fails the whole
  listing (an interface list without its error counts is still a useful
  answer to "what adapters does this machine have").
- **An MTU the platform doesn't meaningfully report is withheld** rather
  than shown — some platforms report implausible values (like `-1`) for
  virtual/loopback interfaces, and printing those as if they were real MTU
  values would just be wrong in a way a user has no way to detect.

Three sections that used to exist in an older "Topology" tab — the ARP
cache, firewall rules, and a full routing table — were removed rather than
reimplemented on top of in-process APIs, because each turned out not to
translate cleanly: the ARP cache loses IPv6 support and most of its state
information outside a full netlink implementation, firewall rules are
shaped completely differently between platforms (`iptables` versus Windows
Firewall profiles versus `pf`), and a full routing table is arguably the
wrong shape for the underlying question anyway — the route stage described
above answers a *specific* routing question ("which interface will this
traffic use?") far more directly than a static table would.

## What's deliberately still out of scope

A full traceroute-style hop-by-hop trace was considered and deliberately
not rebuilt after being removed. Beyond having previously answered for the
wrong machine (the same VM-namespace problem as everything else on this
page), reimplementing it properly would have meant solving three genuinely
unmeasured problems at once: reading ICMP `TIME_EXCEEDED` responses without
a raw socket across three different operating systems, working around the
lack of per-packet TTL control in the ICMP library already in use, and
handling a TCP-based trace mode that needs to observe the ICMP error
triggered by an outgoing connection attempt. The route stage's "which
interface, which gateway" answer covers the practical case — diagnosing a
split-tunnel VPN — that people actually reached for a trace to answer, at a
tiny fraction of the implementation cost.
