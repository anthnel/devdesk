# Enable the MCP server

DevDesk can expose what it knows — status checks, workspace metadata, scan results, forge stats — to an AI agent over the [Model Context Protocol](https://modelcontextprotocol.io/), served in Streamable HTTP directly from the running TUI process. It is off by default.

## Turn it on

In `config.yaml` (edit directly, or via `:config`):

```yaml
mcp:
  enabled: true
  listen: 127.0.0.1:7777   # loopback by default
  expose: []                 # empty = every tool; or list names to allow-list
```

`enabled: false` is the default and nothing turns it on as a side effect — you have to opt in per context.

## Restrict what's exposed

Leave `expose: []` to make every tool available, or list specific tool names to allow-list only those:

```yaml
mcp:
  enabled: true
  expose:
    - workspaces_list
    - scan_inventory
```

(Tool names come from `internal/mcp` — `workspaces_list`, `scan_inventory`,
`scan_result`, `containers_list`, `images_list`, `jobs_list`, `net_check`,
and the rest declared in `internal/mcp/tools.go`.)

This matters most on a context whose scan results or forge data you don't want a connected agent to read in full.

## Point an MCP client at it

With DevDesk running and `mcp.enabled: true`, add it as a Streamable HTTP MCP server in your client (Claude Code, Claude Desktop, or any MCP-compatible tool), pointing at `http://<mcp.listen>` — `http://127.0.0.1:7777` for the default.

## Why it's loopback by default

`mcp.listen` defaults to `127.0.0.1`, not `0.0.0.0` — the server is meant for an agent running on the same machine as DevDesk. Binding it to a wider interface is your call to make explicitly by changing `listen`, not something the default does for you.

## Why this exists instead of a chat feature

DevDesk does not send your data anywhere on its own. The MCP server inverts that: DevDesk exposes what it knows, and the agent — running under whatever access and review process you already trust it with — comes to read it. See [MCP Server](../explanation/mcp.md) for the reasoning behind that direction, including why it replaced an earlier design that had DevDesk assemble and send a payload itself.
