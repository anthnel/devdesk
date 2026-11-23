# Network

DevDesk's network diagnostics — reachability checks, the local socket table,
the network interface list, and local port forwarding — all run *inside the DevDesk process itself*,
never inside a privileged container. This page explains why that constraint
exists, what it cost to satisfy, and how the diagnostics pipeline is built
around it.

## Container engine: Docker or Podman

DevDesk drives a single container engine at a time — Docker or Podman — for
image scanning, container management, and OCI resource browsing.
`app.container_engine` (see [configuration reference](../reference/configuration.md))
picks it: `auto` (the default) takes Docker if it's on `PATH` and falls back
to Podman, `docker`/`podman` pin one explicitly, and anything else is read as
a path to a binary. Picking one explicitly is worth doing if both are
installed and you want DevDesk to never guess — a *pinned* engine that isn't
installed is reported as an error rather than silently falling back to the
other one, because the two don't hold the same containers.

Everything DevDesk does through the engine — listing containers, pulling
images, inspecting networks, scanning — goes through the same `--format`
invocations for both, mostly with identical template strings; the binary
name, the credential helper prefix, and the registry auth file location
differ per engine, and a handful of templates (`info`, `network ls`) carry a
Podman-specific string where its field names or output shape differ from
Docker's. One case goes further: the detailed per-image disk usage breakdown
(`system df -v`) is Docker-only — it's read by scraping a fixed-width table
at column offsets, and Podman's output doesn't line up with them, so that
specific view is disabled rather than shown wrong.

Most views don't need to know which engine is active, but one place does: the
OCI resources registry browser (`B` from the Images tab) builds the reference
it hands to `pull` (`G`) itself, and under Podman that reference needs an
explicit `docker.io/` prefix — Podman, unlike Docker, doesn't assume Docker
Hub for a bare image name unless `unqualified-search-registries` is
configured, which it isn't on a default Debian/Ubuntu or Fedora install.

## The activity dot on the Images tab

The leftmost column of the Images tab shows the image icon at rest and a dot
while something runs on the image, as in `ws` and the security inventory:

| Glyph | Meaning |
|---|---|
| `●` / `○` | A scan, a pull or a removal is running (the dot blinks). The Scanned cell reads `scanning`, or the ID cell reads `pulling`. |
| `◴` | A scan or pull is queued. |
| `⊗` | The last scan or pull failed. It stays until the next attempt on that image. |

The color says the state, not the operation; the footer describes the selected
image beside the batch progress. A scan or pull started from another view
marks the same row. Rows match by the reference exactly as the image is listed,
so `nginx` and `docker.io/library/nginx` are tracked separately. On a narrow
terminal whole columns drop, optional ones first; the dot stays.
Press `?` for the legend.

The Networks, Volumes and Registries tabs and the registry browser (`B`) have
the same leftmost column and the same dots, for a removal, a pull or a tag scan
running on the row.

## Is there a newer image?

The Images tab, the containers list and a scan's Remediation tab each have an
**Update** column. An arrow there means the image's registry holds something
newer. When the update is a later patch tag on the same line
(`node:20.11.1` → `20.11.4`), the arrow is followed by that tag. An arrow on its
own means a new build: the same tag now points to other content, which is the
only kind of update a tag like `latest` or `bookworm-slim` ever gets.

The answer comes from comparing digests: the one the registry gives for the
tag today, and the one the engine recorded when the image was pulled. That has
two consequences worth knowing. An image built or loaded locally has no such
digest, so it never shows an arrow. And a container is compared through the
image it was *created* from: pulling the new image clears the arrow in the
Images tab, but the container keeps it until it is recreated — because until
then it still runs the old one.

When there is no arrow, a grey glyph says why:

| Glyph | Meaning |
|---|---|
| check mark | up to date |
| hourglass | the registry has not answered yet |
| `?` | the registry did not answer (logged) |
| hammer | built or loaded here — there is no registry digest to compare |
| cloud | a Dockerfile base your engine does not hold (Remediation tab) |
| pin | a reference fixed by digest: nothing can move |

Every state except a newer patch is a single glyph, because the words cost
eleven cells in the widest table there is. The reason is still spelled out by
`G`'s greyed shortcut and in the help (`?`), and the filter still matches the
words.

In the Images tab, `G` applies the update: it pulls the newer image — through
the [signature check](signatures.md), like every pull — and
removes the one it replaces. It refuses — and the footer names them — while any
container, running or stopped, was created from that image: remove or recreate
those first.

Registries are asked in the background when a list loads, with a `HEAD`
request that Docker Hub does not count against its pull limit, and each answer
is kept for six hours.

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

### The proxy variables

The Diagnostics measure the direct path, and the **Proxy** row says whether
`HTTPS_PROXY`, `HTTP_PROXY` or `NO_PROXY` applies to the target; nothing is sent
to a proxy. The monitors, the SSL check and the scanner containers follow the
variables. See [Work behind a proxy](../how-to/work-behind-a-proxy.md).

### The Trace Path button

The form has two buttons, **Run Diagnostics** and **Trace Path**. `↑` and `↓`
move from the fields to the buttons. `enter` on a text field still runs the
diagnostics, so nobody has to walk to a button first.

Trace Path opens a level of the Diagnostics tab, `Network › Path › <target>`,
with the table described under [The path trace](#the-path-trace--internalnettrace)
(Hop, Step, Address, RTT). The trace starts when you press the button, from
this machine, and nothing is sent before. `esc` cancels a running trace; `esc`
again returns to the form, with Target, Port and DNS Server as you left them.
In the level, `enter` traces again and `ctrl+o` opens **Trace from**. Every
other key except the table's navigation (and `Tab`, which switches tab) is
ignored there.

The trace probes the path to a host, not a service, so Port and DNS Server
play no part in it. If either is filled, the footer says so and the trace goes
ahead. The Target follows the trace's rules: a host name or an IPv4
address, no port, no IPv6. A refused target is underlined and the footer
gives the reason. Where the trace cannot run (macOS, Windows, WSL1) the button
is greyed out and the footer says why.

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

### The dot while `K` runs

The leftmost column of the Ports table is empty at rest. After you confirm `K`, every socket of that process shows a blinking dot (`●` / `○`) until the system answers, and the footer reads `Killing sshd (812)` for the selected row. The State cell keeps its text, dimmed. If the system refuses, the footer says so and the dot goes away: a failed kill leaves no mark on the row. The periodic refresh never makes a dot blink. Press `?` for the legend.

A network test that fails (TCP refused, invalid TLS) is a result, not a failed operation: it stays in the Verdict column of the Diagnostics tab.

On a narrow terminal whole columns drop, rightmost first: Process under 103 columns, then PID under 91, then Peer Address under 82, down to Proto, State and Local Address from 54.

### Killing a process

Signaling a process uses whatever rights the DevDesk process itself has —
which means killing another user's process, or a system service, now
correctly comes back as "refused by the operating system" instead of
silently succeeding against the wrong machine (as it effectively did when
run inside a privileged, host-networked container).

Because the operating system's refusal is only knowable after the attempt,
the UI treats these questions differently:

- **No PID means the action is unavailable before you even try** — a socket
  the OS won't attribute to a process has the kill shortcut visibly
  disabled on that row, rather than appearing available and failing with an
  explanation only after the keypress.
- **Some processes are protected, and the key is disabled on them too.**
  DevDesk refuses to stop itself, the process that started it (its direct
  parent), and PID 1. On Windows it also refuses PIDs 0 and 4. On such a row
  the kill shortcut is greyed out, and pressing `K` puts the reason in the
  footer: `This is DevDesk itself`, `This process started DevDesk`,
  `PID 1 is the system's init process` or `Windows system process`. This is
  DevDesk's own rule, not a guess about what the OS would allow. Only the
  direct parent is protected: a terminal or a multiplexer further up is not.
  If DevDesk cannot read when its parent was created, it protects the parent.
- **Some rows cannot be checked, and the key is disabled on them.** Before it
  stops anything, DevDesk wants to be sure that a PID still belongs to the
  process you saw. When it could not read what it needs to tell, the kill
  shortcut is greyed out and pressing `K` puts `Cannot verify which process
  this is` in the footer. The row stays visible. DevDesk's own rows keep their
  own reason (`This is DevDesk itself`).
- **Whether the OS actually accepts the signal is answered by the attempt,
  not predicted in advance** — a row with a real PID stays enabled even
  when the kill is likely to be refused, since guessing at permissions
  ahead of time can be wrong in both directions. The resulting error is
  classified rather than shown as a raw platform error string: a permission
  failure reads as "refused by the system," a process that already exited
  reads as "no longer running," and anything else falls back to a generic
  message.

### What the confirmation stops

The confirmation names a process and its PID, for example
`Send SIGKILL to node (PID 4242)?`. Confirming stops **that** process, even
if the table has refreshed and moved since you pressed `K`. The text does not
change while the confirmation is open, and answering No or pressing `esc` sends
nothing.

If the process is no longer in the table when you confirm, DevDesk stops
nothing and the footer says `node (PID 4242) is no longer listed — not stopped`.
The process may have exited, and its PID may now belong to another process.
Press `K` again on the row you want.

### What DevDesk checks before it stops anything

A PID is only a number. When a process exits, the system can give its number to
a new, unrelated process. So DevDesk remembers which process each row was when
the table was read, and checks, at the moment it sends the signal, that the PID
still belongs to that process. If it does not, nothing is sent. The footer says:

```text
node (PID 4242) is no longer the process that was listed — table refreshed
```

and the table is read again, even if you paused it. Press `K` on the row you
want from the new table. If the process has no name, the message starts with
`PID 4242`.

Two other messages come from the same check:

- `Cannot verify which process PID 4242 is — not stopped` means DevDesk lost the
  means to tell, so it did not guess.
- `Refused by the system — DevDesk cannot signal PID 4242` means the operating
  system itself said no. On Windows this can also appear as soon as you press `K`, on a
  row that was lit: DevDesk then checks that row at that moment instead of when
  the table was read, and the system may refuse.

On Linux the check and the signal go to the same process handle, so the PID
cannot change hands between them. On macOS, and on a Linux kernel or sandbox
without that support, a very short gap remains between the check and the
signal. On Windows the check compares the process's creation time with the
moment the table was read, which assumes the computer's clock did not jump
backwards in between.

On Windows there is no way to ask a process to close cleanly from here. The
confirmation reads `Terminate node.exe (PID 4412)?` and says that the process
is ended at once and cannot save anything. On Linux and macOS the process gets
a SIGKILL, which does not let it shut down cleanly either.

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

## The Forward tab — `internal/forward`

The Forward tab redirects a local port to a `host:port`, or gives a service a
name — **inside the DevDesk process and with no privilege**. A forward is a
`net.Listen` on `127.0.0.1` plus two `io.Copy`; nothing is installed and no
elevation is asked for. `N` opens one, `space` pauses or resumes it, `K` deletes
it after a confirmation.

**The router owns the forwards, not the tab.** A configuration save or a
context switch drops every view, so a listener held by the tab would stay
bound with nothing left to close it. The tab asks the router to open, close or
refresh, and is told about every change. A forward belongs to no context, so
forwards **survive a context switch**.

### Four decisions about a TCP forward

- **Loopback only.** A forward binds `127.0.0.1`; `0.0.0.0` would put on the
  LAN a service its owner kept off it.
- **Below 1024 is refused up front**, before the target is even probed. There
  is no unprivileged way around it on Unix, and `permission denied` reads like
  something a retry would fix.
- **The target is dialled once before the port opens.** Otherwise the bind
  succeeds, the row reads healthy, and the failure only surfaces at the first
  client. Refusals carry a fixed wording of DevDesk's own, never the operating
  system's.
- **A container's own address is not a target on Docker Desktop.** That
  address exists inside the Linux VM and routes nowhere outside it (the same
  mechanism as the Ports tab's problem above); it works on native Linux. The
  probe turns it into a named refusal rather than a healthy-looking dead row.

### Forwards survive a restart

The listener can't outlive the process, but the *intent* can: every forward is
written to `~/.devdesk/forwards.yaml` and reopened at the next launch. It's a
file of its own rather than a key of `config.yaml` because the config is per
context and reloaded on every switch, while a forward belongs to none.

A forward is in one of three states:

| State | Meaning | Saved as |
|---|---|---|
| Live | listener bound | an entry |
| Paused | you stopped it; the port is released, the row is kept | an entry with `paused: true` |
| Unbound | wanted but not bound — the port was taken or the target silent at the last attempt; the row says which | an entry, retried at launch |

- **A typo at creation deletes nothing; a failure at launch keeps the entry.**
  The form refuses a bad port or an unreachable target and writes nothing. At
  launch, though, a service that isn't up yet or a port another process holds
  *today* must not make DevDesk forget the route — the row reads *unbound* and
  `space` tries again.
- **Rows keep their creation order**, so a row doesn't move when paused and
  resumed.
- **The write is atomic** (temporary file, then rename). An **unreadable** file is
  refused rather than treated as empty, and the next save moves it aside to
  `forwards.yaml.unreadable` instead of overwriting it.
- **Two DevDesk instances share the file.** The last write wins and there is no
  lock: the second instance's reopening finds the first one's ports taken and
  reads *unbound* rather than failing.

### Named routes — `http://api.localhost:8080`

A forward given a **name** becomes an HTTP route instead of a TCP
redirection. One small HTTP server on `127.0.0.1:<network.proxy_port>` (8080
by default, see the [configuration reference](../reference/configuration.md))
serves every route, and the request's `Host` header picks the target:
`http://api.localhost:8080` and `http://app.localhost:8080` differ by name,
not by port. There is no DNS entry to add and no privilege to gain —
`*.localhost` is reserved for the loopback (RFC 6761) and the platforms
measured resolve it unaided.

In the form, `Type` is a `←`/`→` cycle field (`TCP` / `HTTP`), and only the
fields that apply to the chosen type are shown.

- **The proxy starts with the first served route and stops with the last.**
- **A name must be a valid host name ending in `.localhost`**, stored in lower
  case and unique across every route — a paused route keeps its name, or
  resuming it would find it gone.
- **Unknown host → 404 listing what *is* served; silent target → 502 naming it.**
  `Host` is matched ignoring its port, a trailing dot and case.
- **The original `Host` reaches the backend** rather than being replaced by the
  target's, because applications behind a named route often key on it.
  WebSocket upgrades pass, which is what hot reload rides on.
- **A route has no port of its own**: its row shows the proxy's port — the number
  that goes in the URL — and a TCP forward asking for that port is refused.
- **The port follows the configuration.** `network.proxy_port` is per context
  while forwards belong to none, so a context switch or a saved configuration
  that changes it moves the proxy. If the new port can't be bound, every live
  route reads *unbound* with the reason, and nothing leaves the file.

**Limits.** HTTP only — `https://app.localhost` would need a certificate the
browser trusts, which is one more elevation. The port stays in the URL (port 80
is privileged). A name outside `.localhost` doesn't resolve, and a client with
its own resolver isn't covered. The proxy adds no CORS header: it makes nothing
reachable that wasn't already on the loopback. The proxy binds IPv4 only, and
browsers on Windows and macOS haven't been measured yet.

## The path trace — `internal/nettrace`

The trace shows the hops between this machine and a target, in this
process: nothing runs in a container, there is no `traceroute` binary and no
privilege. It replaces the
container trace that was removed because it answered for the Docker VM and not
for this machine.

- **Native Linux and WSL2 only.** On macOS, native Windows and WSL1 the
  Trace Path button, `H`, `enter` and `ctrl+o` are greyed out, and the footer says why, for
  example `The path trace needs Linux — not available on macOS`. On Windows and
  WSL1 it adds `use WSL2`. Nothing is sent.
- **IPv4 only.** A host name is resolved to its IPv4 address (within
  `check_timeout`); the first one is traced (unless the Diagnostics results already read
  another address of the name successfully, see **From Diagnostics**) and shown next to the
  name in the header. An IPv6 literal is refused.
- **How it probes.** One UDP datagram per hop, to port 33435, with a growing
  time-to-live. The router that drops it answers, and the table fills in as the
  answers come.
- **No port.** The target takes a host name or an IPv4 address only. A target
  with a port (`host:80`) is refused with `Path ignores ports: enter a host or
  IP address only`, and nothing is sent. The trace reaches a host, not a
  service: the probes always go to UDP port 33435.
- **The list starts on this machine.** Before the hops, three rows say how
  your traffic leaves: `source` (`this machine`), `interface` (the interface
  the target's traffic uses, with its source address, for example
  `eth0 (192.168.1.21)`) and `gateway` (the next router, or `directly connected`
  when there is none). They come from this machine's routing table, so with a
  full-tunnel VPN the interface row names the tunnel. If the routing table
  can't answer, both rows read `undetermined` and the trace goes on. If it says
  there is no route to the target, the interface row reads `no route` and the
  gateway row `-`. `undetermined` says nothing either way; `no route` says this
  machine has none. In that case the footer says `No route to <host> from this
  machine — no probe was sent`, and the rows that describe this machine stay on
  screen. Any other error empties the list. The footer counts only the hops that
  answered, not these three rows.
- **Under WSL2.** The distro is a VM, so the `source` row reads
  `WSL2 (VM), not the Windows host · NAT mode` (or `· mirrored mode`, or
  `· mode unknown` when DevDesk can't tell; it asks `wslinfo`, which needs
  WSL 2.0.4 or later). In NAT mode, when the default route of the distro is the
  one being traced, the `gateway` row and hop 1 add `— Windows host (virtual
  switch)`. Never in mirrored or unknown mode, and not when a VPN inside the
  distro replaces the default route. The `trace` rows still say `from this
  machine`. Checked on WSL 2.7.13.0 only. On native Linux nothing is added.
- **Start from a container.** Press `ctrl+o` to open **Trace from**, a list of
  `this machine` and the running containers of a Docker Engine on this machine.
  Use `↑` `↓` (and `pgup`, `pgdown`, `home`, `end`) to move, `enter` to choose
  and `esc` to close. While the list is open, `tab` and pasting do nothing. The
  key works before the trace starts and once it is done. During a trace it is
  ignored, and where the trace is not supported it is greyed out. Choosing
  another source empties the list and puts you back to the ready level, with
  the target kept and nothing sent. The header then shows a `Source` field.
- **What the container rows say.** Before `source`, the list gains a
  `container` row (its name and address, for example `web-1 (172.18.0.5)`) and
  a `network` row (for example `app_net via 172.18.0.1`, the network and its
  gateway). After `gateway`, a `trace` row reads
  `from this machine, not from the container`.
- **The hops still come from this machine.** DevDesk sends nothing from inside
  the container: it only reads what the engine says about it. The hops after the
  `trace` row are the path from your machine, not the container's.
- **Which network.** A container on several networks has one default route,
  and DevDesk does not guess it. It names the network when only one has a
  gateway, or when the engine gives one network the strictly highest gateway
  priority. Otherwise the row reads `undetermined (3 networks)`. A container in
  host mode shares this machine's network stack, so the row says
  `host (shares this machine's stack)` and no bridge is added.
- **When only this machine is offered.** The footer says why: no running
  container, no Docker Engine reachable, Docker Desktop (not supported yet) or
  podman (not supported). DevDesk only reads the engine (`ps`, `info` and
  `inspect`); it starts, stops and enters nothing, and it never reads the
  container's environment variables.
- **Limit: a remote engine is not detected.** If `DOCKER_HOST` or a Docker
  context points at another machine or a VM, its containers are listed anyway
  and drawn before `this machine`, which their traffic never crosses. The
  `trace` row still says where the hops start.
- **A silent hop is not a failure.** Many routers do not answer these probes.
  The row reads `silent` (dim, never red) and the trace goes on. The number of
  hops that answer can vary from one run to the next.
- **When the trace ends without an answer.** If it stops on the hop limit or
  the time limit, the silent hops at the end are folded into one row: `Hop`
  `7+`, step `silent`, for example `no response beyond hop 6 (hops 7 to 30)`
  (`no response from any hop (hops 1 to 30)` when no hop answered). The last hop
  that answered ends with `· last to answer`. The rows only say what was
  observed, not why. Silent hops are most often a router or firewall that does
  not answer these probes; that is a likely explanation, not something the trace
  checks.
- **Nothing is sent before `enter`.** (`enter` in the trace level, or the Trace Path button.) `esc` cancels a running trace and keeps
  the hops already seen. The header says how a trace ended: `Reached at hop N`,
  `Reported unreachable at hop N`, `No answer from the target within N hops`,
  `Stopped after 30 s` or `Canceled`.
- **From Diagnostics.** On the results, `H` on the **Local route** row opens the
  trace level with the host filled in, without starting the trace. The interface
  and gateway rows then reuse the reading the **Local route** check already made
  for the same address, so both show the same answer and the routing table is
  asked once. When the name resolves to several addresses (a round-robin or CDN
  name) and the results already read the route of some of them successfully, the trace takes
  the first of those, so the reading is shared; an address whose reading failed
  (no route, undetermined) is not preferred. Otherwise it takes the first IPv4
  address, as always. `ctrl+r` on the results reads it again; so does a trace
  started from the form, or from a container.

There is no setting for the maximum number of hops (`traceroute_max_hops`
stays removed): the trace uses the engine's defaults.
