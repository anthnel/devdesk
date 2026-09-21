# Connect an AI client to the MCP server

DevDesk serves MCP over Streamable HTTP from the running TUI. Any client that speaks that transport connects the same way: an **URL** and an **`Authorization: Bearer` header**. This page shows the exact form for each client.

Before you start, [enable the server](enable-the-mcp-server.md) and keep DevDesk (`dk`) running — the server lives in that process and stops with it.

## What every client needs

| | Value |
|---|---|
| Transport | Streamable HTTP |
| URL | `http://127.0.0.1:7777` (your `mcp.listen`, no sub-path) |
| Header | `Authorization: Bearer <token>` |

The token is not optional: a request without it gets a `401` before the body is read.

### Read the token

DevDesk generates it the first time the server starts and keeps it in your host secret manager (Credential Manager, Keychain or Secret Service), never in a file. To read it, open `:config`, go to the `mcp` tab and press `space` on the `Token` row. The `State` row above it must show the address the server is listening on.

The token is per context, and it stays the same across launches.

### Keep it out of your repositories

Most clients below can read the token from an environment variable. Prefer that over pasting it into a config file you might commit:

```bash
export DEVDESK_MCP_TOKEN='<token>'
```

## Claude Code

```bash
claude mcp add --transport http devdesk http://127.0.0.1:7777 \
  --header "Authorization: Bearer $DEVDESK_MCP_TOKEN"
```

The command stores the header value it expands, in your local config. Add `--scope user` to make the server available in every project, or `--scope project` to write it to a shared `.mcp.json`.

To write the file by hand instead, `.mcp.json` at the project root expands `${VAR}`, so the token never has to be in it:

```json
{
  "mcpServers": {
    "devdesk": {
      "type": "http",
      "url": "http://127.0.0.1:7777",
      "headers": {
        "Authorization": "Bearer ${DEVDESK_MCP_TOKEN}"
      }
    }
  }
}
```

Check it with `claude mcp list`, or `/mcp` inside a session. If the server already exists, `claude mcp remove devdesk` first.

## Codex

Codex reads `~/.codex/config.toml`. Name the environment variable and Codex builds the bearer header from it:

```toml
[mcp_servers.devdesk]
url = "http://127.0.0.1:7777"
bearer_token_env_var = "DEVDESK_MCP_TOKEN"
```

`codex mcp add` only covers stdio servers, so an HTTP server is added through this file (or the client's settings screen, choosing **Streamable HTTP**).

## Gemini CLI

```bash
gemini mcp add --transport http \
  --header "Authorization: Bearer $DEVDESK_MCP_TOKEN" \
  devdesk http://127.0.0.1:7777
```

Or in `settings.json`, where the token is written into the file:

```json
{
  "mcpServers": {
    "devdesk": {
      "httpUrl": "http://127.0.0.1:7777",
      "headers": {
        "Authorization": "Bearer <token>"
      }
    }
  }
}
```

Gemini uses `httpUrl` for Streamable HTTP; a plain `url` selects the older SSE transport, which DevDesk does not serve.

## Cursor

`.cursor/mcp.json` in the project, or `~/.cursor/mcp.json` for every project. Cursor interpolates `${env:NAME}` in `url` and `headers`:

```json
{
  "mcpServers": {
    "devdesk": {
      "url": "http://127.0.0.1:7777",
      "headers": {
        "Authorization": "Bearer ${env:DEVDESK_MCP_TOKEN}"
      }
    }
  }
}
```

## VS Code (GitHub Copilot)

`.vscode/mcp.json` in the workspace, or **MCP: Open User Configuration** for your profile. VS Code's key is `servers`, not `mcpServers`, and it can prompt for the token instead of storing it:

```json
{
  "inputs": [
    {
      "type": "promptString",
      "id": "devdesk-token",
      "description": "DevDesk MCP token",
      "password": true
    }
  ],
  "servers": {
    "devdesk": {
      "type": "http",
      "url": "http://127.0.0.1:7777",
      "headers": {
        "Authorization": "Bearer ${input:devdesk-token}"
      }
    }
  }
}
```

## Claude Desktop

Claude Desktop cannot send a custom header to a remote server from its config file, so it goes through the [`mcp-remote`](https://github.com/geelen/mcp-remote) bridge, which needs Node.js. Put the whole header value in an environment variable and leave no space in the argument — on Windows, a space in `args` is mangled when the bridge starts:

```json
{
  "mcpServers": {
    "devdesk": {
      "command": "npx",
      "args": [
        "mcp-remote",
        "http://127.0.0.1:7777",
        "--allow-http",
        "--header",
        "Authorization:${AUTH_HEADER}"
      ],
      "env": {
        "AUTH_HEADER": "Bearer <token>"
      }
    }
  }
}
```

`--allow-http` is needed because the URL is not HTTPS. On a loopback address that is safe; do not use it for a `listen` address that leaves the machine.

## From a container or a sandbox

`127.0.0.1` inside a container is the container itself, not the machine running DevDesk. Use `host.docker.internal` instead:

```
http://host.docker.internal:7777
```

DevDesk listens on the host's loopback, which Docker Desktop reaches through that name. A Docker Sandbox (`sbx`) also has its own network policy, and the port has to be allowed there by name:

```bash
sbx policy allow network "localhost:7777"
```

On native Linux Docker, `host.docker.internal` does not reach the host's loopback. Set `mcp.listen` to the bridge gateway address instead, and read [Why it's loopback by default](enable-the-mcp-server.md#why-its-loopback-by-default) first.

## When it does not connect

| Symptom | Likely cause |
|---|---|
| `401 unauthorized` | Wrong or missing token. Copy it again from the `mcp` tab, with no `<` `>` around it and no trailing space |
| Connection refused or timed out | DevDesk is not running, `mcp.enabled` is `false` for the current context, or another `dk` already holds the port. The `State` row on the `mcp` tab says which |
| Connected, then dropped | You switched context in DevDesk. The server restarts for the new context and closes open sessions on purpose — reconnect (`/mcp` in Claude Code) |
| `already exists` when adding | A previous attempt saved it. Remove the server, then add it again |
| Tools missing | `mcp.expose` is an allow-list and names only some of them |
| Works on the host, not in a sandbox | See [From a container or a sandbox](#from-a-container-or-a-sandbox) |

A client added with the wrong token is saved anyway. The mistake shows up at connection time, not when you add it.
