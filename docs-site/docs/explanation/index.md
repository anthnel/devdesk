# Architecture Overview

DevDesk is a multi-view terminal UI built on
[Bubble Tea](https://github.com/charmbracelet/bubbletea)'s Elm architecture:
one router, many self-contained views, and a small set of rules — no model
mutation outside `Update()`, one credential store per context, one command
per view — that keep those views from drifting apart as they grow. The
pages below explain *why* each area is built the way it is, not just what
the code does; read the [reference](../reference/commands.md) section
instead if you already know the shape and need a specific name or value.

| Page | Answers |
|---|---|
| [App Shell & Router](app-shell.md) | How view switching, command mode, and cross-view state work, and why the keyboard vocabulary is split into navigation, uppercase actions, and local lowercase toggles |
| [Forge (GitLab / GitHub)](forge.md) | Why a context targets exactly one forge, and how `internal/forge` keeps the rest of the app from caring which one |
| [Configuration & Contexts](configuration.md) | The context model, how settings and credentials are kept apart, and how old config shapes migrate forward |
| [Workspaces](workspaces.md) | How local repositories are discovered, scanned, and kept in sync with the forge |
| [Viewer](viewer.md) | The one document viewer every "open this file/log" action in the app shares, and how it adapts per file kind |
| [Security Scanning](scanning.md) | How Trivy, Gitleaks and the scan cache fit together, and why scans are background jobs rather than blocking waits |
| [Registries](registries.md) | The registry/group model behind OCI image discovery |
| [Network](network.md) | `internal/docker`, `internal/oci`, and the network diagnostics + live port monitor |
| [MCP Server](mcp.md) | Why DevDesk serves its data over MCP instead of calling a model itself |
| [UI Components](ui-components.md) | The shared building blocks — tables, forms, footer messages — that keep every view consistent |
