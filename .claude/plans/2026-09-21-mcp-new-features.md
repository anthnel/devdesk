# MCP: exposing the features added since §3.61

Status: batches 1 to 4 decided and implemented on `feat/mcp-forward-templates-status`. Baseline: 17 tools in `internal/mcp/tools.go`.

## Constraints that decide everything

Taken from `docs/architecture/mcp.md`; not repeated in each item.

- Nothing in `internal/mcp` touches the router's model (Rule 110). State held by
  the router is reached through `mcp.Dispatcher`, buffered reply channel of one.
- No credential leaves: the absence of a field, not a filter. A test walks the
  types.
- An action is registered only if it is the headless form of a declared key in
  `internal/ui/keymap` (`mcpActionKeys`), and its inverse is not removed.
- Every context-dependent answer says which context served it.
- Only `net_check` touches the network today. Any second one is declared as such.
- `mcp.expose` stays the only authorization.

## Batch 1 — text fix for Podman (done)

`containers_list`, `images_list`, and the `jsonschema` tags in `docker_tools.go`
and `scan_tools.go` said "Docker" / "what docker prints" although
`app.container_engine` can be Podman. Agent-visible strings only; identifiers and
comments untouched. `go test ./internal/mcp` passes.

## Batch 2 — two read tools (done)

### `forwards_list`

- Source: `forward.Registry.List()`. The registry belongs to the router
  (`shared.State.Forwards`), so this goes through the dispatcher like
  `jobs_list`, with a new typed request (no generic `Invoke`).
- Not from `store.go`: it holds what is wanted, not what is bound.
- Answer per forward: id, name (empty for a plain port), local port, target,
  state (bound / paused / error and why), URL (`Forward.URL()`).
- Context stamp: decided — no stamp. The forwards file is global
  (`config.ConfigDir()`) and a switch leaves forwards open, so the answer is
  the session's, not a context's. Declared in `machineWide` with that reason.
- Tests: extend `TestTheServerRegistersExactlyTheDeclaredTools`, the HTTP
  transport test, and `TestUpdateNeverBlocksOnAnAbandonedReply` for the new
  request.

### `templates_list`

- Source: `template.Store.List()` and `Cache.FetchedAtAll`. Disk only, no
  dispatcher.
- Answer: slug, name, source kind and location, tags, age of the cached copy.
- No field of `template.Credentials`, no file content. Add the type to
  `TestNothingInAContextAnswerCanCarryASecret`. A source URL may embed a token:
  check `Source` and strip userinfo, or refuse the field.
- No context stamp: the catalog is global (`template/store.go`), declared in `machineWide`. The rejected entries are returned as a count only, since their reasons quote the entry.

## Batch 3 — `monitors_status` (done)

- Source: `status.Checker.CheckAll` over the context's `components`.
- Second tool that touches the network. Targets come from `config`, never from
  an argument, same argument as `net_check`'s dials. Update the "`net_check` is
  the one tool that touches the network" paragraph in `mcp.md`.
- Context is the client's, so cancelling the call stops the probes.
- Answer: per monitor, type, verdict, latency, and for SSL the `CertState`.
  `certificates_list` is not a separate tool unless a real need appears; a
  `type` filter on this one covers it.
- Timeout: each monitor's own, plus a 30 s cap on the call (`monitorsDeadline`),
  because the SSL probe takes no context.
- Context stamped: monitors come from the context's configuration.
- Found on the way: `ui/status/commands.go` builds the checker with
  `time.Duration(cfg.Status.Timeout)`, which is seconds read as nanoseconds. Not
  touched here; see the note in the commit.

## Batch 4 — actions (decided 2026-09-21)

- `template_sync_start`: **built**, one slug. The credential worry was smaller
  than written: `CredentialsFor` reads the secret store inside the process and
  hands the token to the fetch, only for the forge's own host and never over
  http; decision 6 forbids a tool exposing a secret, not triggering an action
  that uses one. The reply is a job id.
- `forward_open` / `forward_close`: **not built** (option A). The free target is
  a bridge to any host the machine can reach. Way back if wanted: restrict
  targets to loopback and to what `containers_list` reports, and limit
  `forward_close` to forwards opened over MCP. Recorded in `mcp.md`.

The original analysis follows.

- `template_sync_start` — headless `F` on `:templates`. Goes through the jobs
  registry like `workspace_sync_start`. Blocker: `template.CredentialsFor` reads
  the secret store, and decision 6 says no tool reads it. Acceptable only if the
  read stays inside the process and no value reaches an answer; otherwise
  refuse.
- `forward_open` / `forward_close` — headless `N` / `K` on the Forward tab. The
  inverse exists, which the action-tier rule requires. The open question is the
  free target: an agent could bind a port toward any host. Options: restrict to
  targets already on loopback or in the context's config, or leave unbuilt like
  `clone_start`. `forward.Open` returns a `tea.Cmd`, so it needs the
  `pendingInvocations` path in `invoke.go`.

## Not exposed

- Repository creation from a template: creation, and its undo is a delete that
  is not exposed.
- Template file contents: can carry secrets.
- Podman: no new tool; batch 1 covers it.
- Dashboard: an aggregate of data already served. Host metrics
  (`internal/metrics`) possible but low value.

## Docs to update with each batch

- `docs/architecture/mcp.md` (tool table, network paragraph, secrecy list)
- `docs/backlog.md` (new entry at the top of §1.1)
- `docs-site` MCP reference page, if one lists the tools
- Help content (`GetHelpContent`) of the configuration view's `mcp` tab if it
  lists the tools
- `mcpActionKeys` and `TestEveryActionToolMapsToADeclaredKey` for any action

## Order

1. Batch 1 (done), 2. `forwards_list` + `templates_list`, 3. `monitors_status`,
4. decide batch 4.
