# The MCP server — `internal/mcp` + `dk mcp`

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## The MCP server — `internal/mcp` + `dk mcp`

What DevDesk knows, served over the Model Context Protocol, on stdio, **read
only** (§3.38). The direction is the point: §3.10 had DevDesk assemble a payload,
pseudonymise it and send it to a model; here DevDesk exposes what it knows and
the agent comes to read it. That deleted half a feature — the client, the
pseudonymiser, the confirmation panel, the streaming — because a protocol exists
for it.

```bash
dk mcp                      # serves the current context
dk mcp --context work       # serves that one, for the life of the process
```

`main()` dispatches on the first argument **before anything else**, and the
ordering is load-bearing: everything on the TUI path writes to stdout, and in
stdio MCP **stdout is the protocol channel**. One warning printed before the
server starts makes the client report a JSON parse error that names nothing.
Diagnostics go to stderr, the refusal included.
`TestNothingInThisPackageWritesToStdout` parses the package and
`TestTheMCPBranchIsTakenBeforeAnythingPrints` reads `main.go`, so neither is a
convention.

**Nothing is written, anywhere.** `~/.devdesk/` has no lock, so a server that
writes nothing can run while the TUI runs without anyone having to think about
it. That took more than the entry anticipated: three write paths sit on what
looks like a read, and `internal/cache/readonly.go` avoids all three —

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

**`io.EOF` is not a failure**: it is how a stdio server ends. The sentinel is
compared rather than the message, because the SDK's own `ErrServerClosing` lives
in an `internal` package.

SDK: `github.com/modelcontextprotocol/go-sdk` v1.7.0, chosen for its v1
compatibility guarantee and because the table of supported spec revisions is
declared by the SDK rather than tracked by hand. Cost: **+2,99 MB** on the binary
(24.98 → 27.97).

