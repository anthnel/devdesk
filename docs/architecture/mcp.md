# The MCP server — `internal/mcp`, served by the TUI

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## The MCP server — `internal/mcp`, served by the TUI

What DevDesk knows, served over the Model Context Protocol, in **Streamable
HTTP, from the running TUI's own process** (§3.61). The direction is the point:
§3.10 had DevDesk assemble a payload, pseudonymise it and send it to a model;
here DevDesk exposes what it knows and the agent comes to read it. That deleted
half a feature — the client, the pseudonymiser, the confirmation panel, the
streaming — because a protocol exists for it.

```yaml
mcp:
  enabled: false          # per context; nothing turns it on as a side effect
  listen: 127.0.0.1:7777  # loopback, and empty means this default
  expose: []              # allow-list of tool names; empty means all
```

**It was a subcommand, `dk mcp`, on stdio** (§3.38), because Bubble Tea owns
stdin and stdout in full and there is no room for a second protocol in one
process. That was right, and it left one thing out: **an agent running in a
container cannot execute the host binary at all.** It was §3.38's own open
question 1 and it became the only case anyone had. So the transport is HTTP, the
TUI is what serves it, and stdio is gone rather than kept beside it — with the
whole ordering constraint that used to head `main()`, and the two tests that
guarded it (`TestNothingInThisPackageWritesToStdout`,
`TestTheMCPBranchIsTakenBeforeAnythingPrints`). Their entire reason was that in
stdio *stdout is the channel*; over HTTP the two do not share one, so keeping
them would leave tests that read as constraints still in force.

**Loopback is both the most closed bind available and the one that works.** On
Docker Desktop a container reaching `host.docker.internal` arrives at the host's
loopback, so a sandboxed agent is served without the LAN ever being offered the
port — and the sandbox's own network policy is a second lock, since the port has
to be allowed there by name (`sbx policy allow network "localhost:7777"`).
`listen` is a setting rather than a literal for one reason, and it is not
configurability: on native Linux Docker that path does not work and the bridge
gateway would have to be bound instead. An empty value is the default, never
"listen nowhere" — the zero value of a string must not read as a choice.

**A bearer token is not optional here.** §3.38 needed none: stdio has no
authentication because the process *is* the user. A loopback port that clones
and scans is reachable by every process on the machine — an npm `postinstall`,
an editor extension — and the sandbox's network policy protects the sandbox, not
the host from itself. So `Authorize` refuses anything without the right bearer
before the body is read, in constant time: on the loopback an attacker gets
unlimited attempts with no network jitter to hide a timing difference, which is
the case where that actually matters.

The token lives in the secret store (`internal/credentials`) under
`devdesk://mcp`, never in a file DevDesk owns — §3.9's rule, and a server token
is exactly the secret that would otherwise land in `config.yaml` "because it is
only a local one". The keyring namespaces the account by context, so two
contexts do not share one.

**It is not carried on `Env`**, and that is deliberate: `Env` is handed to every
tool's register closure, and a token reachable from there finds its way into an
answer eventually. Authorization is the transport's business and stays at the
transport.

**A store that does not persist stops the server from starting**, and says so.
`credentials.Select` falls back to memory when no host store answers, so it is a
real path — and a token regenerated every launch would break the agent's
configuration once per session, silently, with the failure read as the agent's
rather than as the store's.

**The server is the router's**, started from `Init()` by a `Cmd` (opening a
listener is I/O) and reported back on `MCPServerStartedMsg` — a running server,
the address it *actually* bound, or the reason there is none. `mcp.enabled:
false` is one of those reasons and not the absence of one, because "off" and
"could not bind" look identical from outside and only one of them is a problem.
`main()` hands the router the `*tea.Program` between `tea.NewProgram` and
`p.Run()`: the one window where writing a field of the model races with nothing.

**There is still no lock, for a different reason.** `~/.devdesk/` has none, and
§3.38 bought the absence of the question with read-only tools. Serving from the
TUI keeps it a different way — the server is not a second writer. It lives in the
one process whose `Update()` is already the only thing allowed to write, so the
guarantee moves from *the server does not write* to *the server writes through
the same door as the keyboard*. What follows is a hard rule for the package:
**nothing in `internal/mcp` ever touches the router's model.** The model is in
the same process, which is exactly why reading it would be the data race
Rule 110 exists to forbid. Reads go to disk and to the daemon as they always
did, and `internal/cache/readonly.go` is unchanged — three write paths sit on
what looks like a read, and it avoids all three —

| Write | Where it hid |
|---|---|
| attributing legacy entries to the opening context | `readScanCacheFile`'s upgrade. The only one that *decides* something |
| the §3.39 context fold | written back on the first open of an image cache |
| `MkdirAll` | both cache constructors, **and** both `Load*ScanResult` — reading a result that is not there created the directory it is not in |

It returns **maps, not caches**. A read-only cache whose `Set` does nothing is a
trap laid for the next caller; with no cache there is no `Set`, and the write is
unexpressible rather than forbidden by review — Rule 122's shape, one layer up.

**The tools are a declared table**, `internal/mcp/tools.go`, in the spirit of
`internal/ui/keymap` and `command.AllViewNames()`. Each entry carries a
`register` closure rather than a handler, because `sdk.AddTool` is generic over
both the argument and the result type and a homogeneous table cannot hold
handlers that differ in both. `TestTheServerRegistersExactlyTheDeclaredTools`
therefore drives a real client over the SDK's in-memory transport: only an
execution sees a closure that registers under another name, twice, or not at all.

| Tool | What it answers |
|---|---|
| `context_list` | the contexts on this machine, and which one this process serves |
| `context_get` | the served context's configuration — no field can carry a credential |
| `workspaces_list` | the repositories under `workspaces_dir`, their git state and their scan state. It carried a `project_type` guessed from a signature file, with its own copy of the table; §3.46 removed the column that motivated it and this with it |
| `registries_list` | the configured registries, and the members discovery last found |
| `containers_list` | what the daemon holds, with the ports parsed |
| `ports_list` | the TCP and UDP sockets open on this machine, and the process holding each |
| `images_list` | the local images, and whether each has ever been scanned |
| `scan_inventory` | every target this context has scanned, reconciled against what still exists |
| `scan_result` | one scan's findings, filtered by severity and category, paginated |
| `net_check` | the `internal/netcheck` pipeline: eleven checks, each with a verdict and what to do |
| `jobs_list` | the work this session has started, and what each run is doing right now |
| `jobs_get` | one run target by target, with the reason any of them failed |
| `workspace_scan_start` | `S`/`A` on `ws`, headless — returns a job id |
| `workspace_sync_start` | `F` on `ws`, headless |
| `image_scan_start` | `S`/`A` on the Images tab, headless |
| `image_pull_start` | `G`, headless |
| `jobs_cancel` | `K` on a **run**, never on a container |

**`jobs_list` and `jobs_get` are the only tools that do not read the disk**, and
they are the reason the server is inside the TUI rather than beside it. A scan
that has finished is in the scan cache; a scan that is *running* exists only in
`jobs.Registry`, in the router's model, for the life of the session (D8 of
§3.58). A headless process cannot answer "is it still going".

They cross through `mcp.Dispatcher`, which `internal/app` implements over
`tea.Program.Send`:

```
tool handler  →  p.Send(mcpJobsRequestMsg{reply})
                   ↓
                 Update()  — reads the registry, writes the channel
                   ↓
tool handler  ←  reply
```

**The reply channel is buffered to one, and that is load-bearing.** An agent
that hangs up while its message is still queued leaves nobody reading, and an
unbuffered send from `Update()` would stop the whole TUI — every keypress, every
spinner frame — on a client that has gone. With a buffer of one the send always
completes and the channel is collected when both sides let go, so there is no
registry of pending calls to keep and nothing to leak.
`TestUpdateNeverBlocksOnAnAbandonedReply` is what holds that.

The dispatcher is a value holding the program, not a method on `*App`:
it is called from a goroutine that is not `Update`'s, so it must be unable to
reach a field of the model even by accident. And the interface is **typed per
question** rather than generic over a payload — an `Invoke(name, args any)`
would put a type assertion at both ends of every call and buy nothing, with one
implementation on each side.

`Snapshot` clears the cancel functions on the copies it hands out, so what
leaves cannot stop a job behind the router's back — the same asymmetry a view
gets (D1 of §3.58), and the reason nothing has to be filtered on top.

## The served context is the session's, and what pays for it

§3.38 fixed the context at process start, for a reason that has not stopped
being true: a server following the current context changes what it answers
underneath an agent mid-conversation. §3.61 gives that up — there is no separate
process to fix it in — and two things pay for it.

**A switch restarts the server, which drops the open sessions.** That is the
guarantee, not a side effect: a client does not silently begin reading another
context, it loses its session and re-initialises. `Close` rather than
`Shutdown`, deliberately — a request in flight belongs to the context being
left, and letting it finish would answer for that context after the user has
moved on. The rebuild is also not optional: the server is built around one `Env`
captured at start, so one left running would keep answering for a context nobody
is in. Switching to a context with `mcp.enabled: false` therefore stops the
server, the setting being per context.

The alternative on offer was a notification, and the SDK delivers
`ServerSession.Log` only once the client has set a log level — a guarantee that
holds when it feels like it.

**Every answer whose content depends on the context says which one served it.**
`TestEveryContextDependentAnswerSaysWhichContextServedIt` finds the answers
where they are returned — the second result of a handler whose first is
`*sdk.CallToolResult` — rather than by their name, so a row type inside an
answer is not asked to repeat it. The exceptions are declared with their reason,
in the spirit of `keymap.DeclaredExceptions()`: an answer about the machine is
the same answer whatever context is on screen, and stamping it would say the
opposite.

`jobs_list` carries both — the context that served the answer, and each run's
own. They differ, and the difference matters: runs are kept for the session
rather than per context (D8 of §3.58), so a job started in one and still going
after a switch says so, and an agent does not read its result against the
context now on screen.

## The action tier

§3.38 refused one outright: an agent that picks the wrong row meets no modal,
and there was nobody to raise one. §3.61 reverses that, **and not by bringing
the modal back** — a confirmation raised by a tool call would block the agent on
an event the user is not looking at, in a window they may not have open. The
class of action that would have needed one is simply not registered, which is
the shape of every other guarantee here. An action never registered cannot be
wrongly confirmed.

So nothing deletes, prunes, kills or stops a container. Nothing creates or
renames either — **their only undo is a delete that is not exposed**, and an
action made irreversible by removing its inverse is worse than a destructive one
owned up to.

**The selection rule is the keyboard vocabulary**: an action tool is the
headless form of an entry in `internal/ui/keymap` that keeps a meaning without a
screen. `mcpActionKeys` names the key beside each tool and
`TestEveryActionToolMapsToADeclaredKey` opposes the two, so a tool invented with
no key behind it fails rather than accumulates;
`TestNoDestructiveActionIsRouted` holds the excluded list.

**`clone_start` is missing, and it is not an oversight.** `C` opens a selection
the user builds by walking the forge tree, then a second screen for the
destination; `handleCloneDestinationSelected` resolves it through `rootNodes()`
and `m.selection`, both of which are the state of a tree somebody browsed. An
agent has none of that. Exposing the tool needs a headless resolution path — a
group path to a node set, without the tree — which is a feature to specify, not
plumbing. The vocabulary rule stands; what it maps to does not exist yet.

### How an action gets across

A reading call is answered inside the Update that receives it. An action is not:
the router hands the request to a view, the view returns a `Cmd`, and the run is
registered one Update later in `handleStartJobs`. Something has to hold the
caller's channel in between — `pendingInvocations`, mutated from Update alone.

**The correlation is carried by the run, not matched by time.** `jobs.StartMsg`
gained an `Invocation`, and `jobs.WithInvocation` stamps it onto the command a
view was already returning — a decorator rather than a fourth constructor, since
`Start`, `StartInContext` and `StartCancellable` already differ along two axes.
A router that instead remembered "an invocation is in flight" and gave its id to
the next `StartMsg` would have a window one Update cycle wide, in which a
keypress fits: the agent would be handed the identifier of the scan the user
just started by hand. `TestAKeyboardLaunchAnswersNobody` is that bug, written
down.

**The view does the work, not the router.** It resolves the targets against what
it lists, consults the same `Availability` the header greys the shortcut with,
and either starts or refuses with that same sentence (`jobs.RefusedMsg`). One
calculation, three readers now: the header, the footer, and the tool's error. A
path the view does not list is refused rather than passed to git — an agent typo
should not become a scan of somewhere else on the disk.

The target view is built on demand: views are lazy, and an agent asking for a
scan before anyone has opened `ws` is the ordinary case.

**Neither `A` variant purges.** The purging one is behind a modal with a
checkbox because it is the closest this application comes to losing data by
accident (§3.26); a tool call has no modal, so it gets the non-destructive half.

**`jobs_cancel` decides nothing of its own.** `Registry.Cancel` answers, and
what stopping *means* is D7 of §3.58: the queue always stops, and work in flight
is cut only where cutting leaves nothing behind. The one refusal here is a run
that has already settled — reporting a cancellation that did nothing would be
worse than saying so.

**Three secrecy guarantees, and each is the absence of a field rather than a
filter.**

- `contextGetOut` has no credential field because `config.Config` has none
  (§3.9). `TestNothingInAContextAnswerCanCarryASecret` walks the *types* — a
  field empty in a fixture would pass a value check — and its rule has a kind
  criterion: a secret is a **string**, so a `bool` named `secret_scanning` cannot
  be one.
- `finding` has **no `Match`**. The string a secret scanner matched is the
  secret, so a field able to carry it gets filled in by accident one day.
  `TestTheMatchedStringOfASecretNeverLeaves` looks for the string in the
  **serialised** answer rather than for a field name, since a `Match` copied into
  a `Title` would pass a field-shaped check.
- `mcp.expose` is an **allow-list**, never a deny-list: a tool never registered
  cannot fail to be excluded. A name matching nothing is refused rather than
  ignored — a typo would otherwise expose less than asked, silently.

**`redact_secret_matches` does not exist**, though §3.38 specified it. The same
entry classed the matched string as never exposed, so its other value was
refused: a parameter that has to be ignored is worse than none (§3.39's
argument). Worse, `false` is a `bool`'s zero value, so every file written before
the key existed would have decoded to "do not redact" — D12 exactly.

**`registry_tags` was not built, and the reason is in the source.** There is no
tag cache. The group cache holds discovered *members*; tags are fetched over HTTP
when the browser searches. Fetching them here would need a credential for every
registry anyone actually runs, and decision 6 is that no tool reads the §3.9
store. An anonymous-only listing would answer "no tags" for a private registry:
an absence read as an emptiness, which is D20.

**`ports_list` was refused and then built**, and the reversal is the point.
§3.38 kept it out because `docker.RunSS` was
`docker run --rm --net=host --pid=host --privileged` — the same call as
`KillProcess` bar the command — and starting a privileged container is acting on
the machine, which is the one thing this server promises not to do. D55 then
established that the same call was reading the *wrong* machine. `internal/ports`
reads the socket table in this process, so there is no container to start and the
argument left with it. It never resolves an address: `net_check` is the one tool
that touches the network, and a listing that quietly asked reverse DNS for every
peer it found would be a second.

**`net_check` is the one tool that touches the network, and it runs no
container** — the pipeline is pure Go, and the route trace, DevDesk's one probe
that shells out, is not part of it. Its dials come from `network:` rather than
from a tool argument: a caller that could override them could make the server
hammer a host or hang on one. The context passed to the probes is the client's,
so cancelling the call stops them.

**`http.ErrServerClosed` is not a failure**: it is what `Shutdown` produces, so
it is the ordinary end of a session. Anything else happened to a server the user
believed was up, and the log is the only place left to say so — a footer message
lasts three seconds and this can arrive an hour in.

SDK: `github.com/modelcontextprotocol/go-sdk` v1.7.0, chosen for its v1
compatibility guarantee and because the table of supported spec revisions is
declared by the SDK rather than tracked by hand. Cost: **+2,99 MB** on the binary
(24.98 → 27.97). The move to HTTP added no dependency: `NewStreamableHTTPHandler`
was already in that version.

**The in-memory transport is not the transport.** `TestTheServerRegistersExactlyTheDeclaredTools`
drives a real client over `NewInMemoryTransports`, which proves what the server
registers and says nothing about what survives a socket.
`TestTheDeclaredToolsSurviveTheHTTPTransport` makes the same assertion over
`httptest` — deliberately the same, so that when one of them fails the pair says
which half broke.

