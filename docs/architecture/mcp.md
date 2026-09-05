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

