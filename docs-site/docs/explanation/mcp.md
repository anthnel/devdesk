# MCP Server

DevDesk can expose what it knows to AI coding agents over the [Model Context Protocol](https://modelcontextprotocol.io/), served directly from the running TUI process rather than as a separate tool. This page explains why it's shaped the way it is: HTTP rather than stdio, read-mostly rather than fully interactive, and scoped to exactly the session that's currently running.

## Serving from the TUI, over HTTP

The design direction matters: DevDesk doesn't assemble and send data anywhere. It exposes what it already knows, and an agent connects to read it — a plain server role rather than a client that pushes data out.

```yaml
mcp:
  enabled: false          # per context; nothing turns this on as a side effect
  listen: 127.0.0.1:7777  # loopback; an empty value means this default
  expose: []              # allow-list of tool names; empty means all
```

The server was originally a `dk mcp` subcommand speaking MCP over stdio, which made sense while Bubble Tea itself needed full, exclusive ownership of stdin/stdout — there was no room for a second protocol sharing the same process's standard streams. That approach broke down for one case that turned out to be the only one anyone actually had: an agent running inside a container can't execute the host binary at all. So the transport moved to HTTP, served by the already-running TUI process itself, and stdio support was removed entirely rather than kept alongside it — with it went every constraint that only existed because stdio treats stdout as *the* channel; over HTTP the two don't share a channel to begin with.

!!! note "Why loopback, and why it's still a setting"
    Binding to `127.0.0.1` is both the most closed option available and, in practice, the one that actually works: on Docker Desktop, a container reaching `host.docker.internal` lands on the host's own loopback interface, so a sandboxed agent can reach the server without the port ever being exposed to the LAN. `listen` is still a config field rather than a hardcoded literal for one specific reason: on native Linux Docker, loopback-only binding doesn't work the same way, and the Docker bridge gateway address needs to be bound instead. An empty string is treated as "use the default," never as "listen nowhere" — a config's zero value should never silently mean a real choice.

## Authentication

A bearer token is required, and this wasn't optional in the original stdio design: over stdio there's no real authentication question, since the process *is* the user. A loopback HTTP port that can trigger scans and clones is reachable by any process on the same machine — an npm `postinstall` script, a rogue editor extension — so `Authorize` checks the bearer token in constant time before the request body is even read, since on a loopback interface an attacker gets unlimited attempts with no network jitter obscuring a timing difference.

The token itself lives in the host secret store, never in a config file DevDesk owns — the same rule that keeps forge tokens and registry passwords out of `config.yaml`. It's deliberately not threaded through the shared `Env` value handed to every tool's registration closure, since anything reachable from there eventually finds its way into some tool's answer; authorization stays strictly a transport-layer concern.

If the secret store can't actually persist (falling back to an in-memory-only store), the server refuses to start rather than silently issuing a token that will vanish and change on every restart — an agent's saved configuration breaking silently once per session would be a much worse failure mode.

## One process, one writer

`~/.devdesk/` has no file lock, and the MCP server doesn't change that — it stays a *read-only* participant by living inside the same process whose `Update()` function is already the only thing in the application allowed to write anywhere. The guarantee shifts from "the server never writes" to "the server can only write through the same door the keyboard already uses," and `internal/mcp` simply never touches the router's model at all — reaching into it from a goroutine outside `Update()` would be exactly the kind of data race the application's Bubble Tea conventions exist to prevent.

Reads go through the same disk and cache paths every other read in the application already uses, exposed through a read-only wrapper that returns plain maps rather than cache objects — a read-only cache whose `Set` method silently does nothing is a trap for the next person who calls it; returning a plain map instead makes writing through this path a compile error rather than a bug waiting to happen.

## The tools

Tools are declared in a single table (`internal/mcp/tools.go`), each entry holding a small registration closure rather than a shared handler function — the underlying MCP SDK is generic over both argument and result types per tool, so a single uniform handler signature can't represent all of them.

| Tool | What it answers |
|---|---|
| `context_list` | the contexts on this machine, and which one this server instance is serving |
| `context_get` | the served context's configuration (no field can ever carry a credential) |
| `workspaces_list` | local repositories under `workspaces_dir`, their git state and scan state |
| `registries_list` | configured OCI registries and their last-discovered members |
| `containers_list` | what the Docker daemon holds, with ports parsed |
| `ports_list` | open TCP/UDP sockets on this machine and the process holding each |
| `images_list` | local images and whether each has ever been scanned |
| `scan_inventory` | every scan target this context has scanned |
| `scan_result` | one scan's findings, filtered by severity/category, paginated |
| `net_check` | the built-in network diagnostics pipeline |
| `jobs_list` | work started this session, and what each run is currently doing |
| `jobs_get` | one run's targets, with failure reasons |
| `workspace_scan_start` / `workspace_sync_start` | headless scan/sync of a workspace, returns a job id |
| `image_scan_start` / `image_pull_start` | headless image scan / pull |
| `jobs_cancel` | cancel a **run** — never a container directly |

`jobs_list` and `jobs_get` are the only tools that never touch disk at all, and that's exactly why the server has to live inside the TUI process rather than run standalone: a completed scan is written to the on-disk cache, but a scan that's *currently running* only exists in the in-memory jobs registry, for the life of the session. A standalone headless process would have no way to answer "is it still going."

These two tools cross into the router's own model through a small dispatcher interface implemented over `tea.Program.Send`:

```
tool handler  →  p.Send(mcpJobsRequestMsg{reply})
                   ↓
                 Update()  — reads the registry, writes the channel
                   ↓
tool handler  ←  reply
```

The reply channel is deliberately buffered to exactly one slot. An agent that disconnects while its request is still queued would otherwise leave nobody reading from an unbuffered channel — and sending from inside `Update()` blocking on that would freeze the entire TUI (every keypress, every spinner tick) waiting on a client that's already gone.

## What the user sees

The router itself has no footer — the active view always renders its own — so a small broadcast message type lets the router post a footer notice regardless of which view happens to be on screen; every view already forwards unhandled messages to its footer component, so this reaches the user wherever they currently are, with one gap: the sign-in screen renders a footer but doesn't wire up this broadcast type, so a notice posted at exactly that moment reaches nobody. That's tolerable specifically because the same underlying state is also visible, persistently, on the MCP tab of the configuration view.

A bind failure is reported as an error-level footer message, but **only if the current context actually asked for a server** — reporting the failure of a server nobody requested would read as a fault where there isn't one. The most common cause is simply a second `dk` instance already holding the port; the TUI keeps running without its server in that case, since losing the whole application over an already-used port would be wildly disproportionate.

The MCP configuration tab shows a `State` row distinguishing three outcomes that look similar from outside but mean different things — serving on a specific address, not started with a specific reason why, or simply not enabled for this context — plus a `Token` row that stays masked until explicitly revealed, matching how every other secret-bearing field in the configuration view behaves.

## Context scoping

The server is fixed to whichever context the process is currently running under, and switching contexts **restarts the server**, deliberately dropping any open client sessions rather than quietly answering for a different context underneath an in-progress conversation. This is treated as the actual guarantee, not an unfortunate side effect: a connected client should never silently start reading a different context's data — it should visibly lose its session and have to reinitialize. The shutdown used here is a hard close rather than a graceful drain, since a request still in flight belongs to the context being left, and letting it finish would mean answering for that context after the user has already moved on. Switching to a context with `mcp.enabled: false` simply stops the server outright, since the setting is per-context.

Every answer whose content actually depends on which context served it explicitly says which context that was, with a small set of declared exceptions for answers that are the same regardless of context (facts about the machine itself, for instance). `jobs_list` in particular reports both the context serving the answer *and* each individual run's own originating context, since those can legitimately differ — a run is kept alive for the life of the whole session rather than being tied to one context, so a job started before a context switch and still running afterward correctly reports where it actually came from.

## What's deliberately not exposed

Nothing destructive is registered as a tool at all: nothing deletes, prunes, kills, or stops a container; nothing creates or renames one either. The reasoning is that a confirmation dialog raised by an incoming tool call would block the agent on a screen the user isn't necessarily looking at — so rather than bringing back a confirmation flow for headless callers, the whole class of action that would need one is simply never registered as a tool. Every exposed action tool maps back to an existing keyboard shortcut in the interactive UI, which keeps the two vocabularies from drifting apart.

One notable absence: there's no `clone_start` tool. Cloning in the interactive UI is a two-step flow — walking the forge's tree to build a selection, then picking a destination — and an agent has no equivalent of "a tree somebody has been browsing." Exposing it headlessly would need a genuinely new resolution path (turning a bare group path directly into a target set, with no tree involved), which is a feature to design deliberately rather than something that falls out of the existing flow for free.

For the write-capable tools that *are* exposed (starting a scan or sync headlessly, for instance), the request is resolved against the same `Availability` checks that grey out the equivalent keyboard shortcut in the UI, and refused with the same reason if it doesn't apply — one calculation shared by the header, the footer, and the tool's own error response, rather than three separate opinions about whether an action is currently valid.

### Secrecy guarantees enforced by absence, not by filtering

Several sensitive categories of data are excluded from MCP answers by simply having no field to carry them, rather than by being filtered out after the fact — a distinction that matters because a filter can be forgotten on a new field, while a genuinely absent field can't leak by omission:

- context answers carry no credential field, because the underlying config type itself has none
- a secret-scan finding never includes the actual matched string — only that a match occurred and where — since the matched string *is* the secret
- `mcp.expose` is strictly an allow-list, never a deny-list: a tool that was never registered can't accidentally be "insufficiently excluded," and a typo in the allow-list simply exposes less than intended rather than silently exposing something unintended

## SDK and cost

The server uses `github.com/modelcontextprotocol/go-sdk`, chosen for its v1 compatibility guarantee and because the set of MCP spec revisions it supports is declared by the SDK itself rather than tracked by hand elsewhere. Moving from stdio to Streamable HTTP added no new dependency, since the SDK already shipped the HTTP handler needed. The addition costs roughly 3 MB in the compiled binary.
