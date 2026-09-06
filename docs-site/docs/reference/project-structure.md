# Project structure

```
devdesk/
├── main.go
├── go.mod
├── mise.toml
├── themes/            # Bundled theme JSON files
├── internal/
│   ├── app/           # Router, header, cross-view messages
│   ├── cache/         # Scan result caching (images + workspaces)
│   ├── command/       # Command parser + completion
│   ├── config/        # YAML config + multi-context
│   ├── credentials/   # Credential storage (keyring, git-credential, memory)
│   ├── docker/        # Docker CLI wrapper, netdiag runners, port monitor (ss)
│   ├── forge/         # GitLab/GitHub abstraction — session, namespaces, repositories
│   ├── jobs/          # Background job registry backing the :jobs view
│   ├── mcp/           # MCP server, served over HTTP by the running TUI
│   ├── oci/           # OCI registry client
│   ├── scan/          # Trivy + Gitleaks integration
│   ├── shared/        # Cross-view shared state
│   ├── status/        # Status checker factory (HTTP/ICMP/DNS/SSL)
│   └── ui/
│       ├── components/      # Reusable modals (confirm, report, selector, filter bar)
│       ├── containers/      # Docker containers view
│       ├── dashboard/       # Overview dashboard
│       ├── forge/           # Forge auth + explorer views (GitLab/GitHub)
│       ├── help/             # In-app help overlay
│       ├── jobsview/         # Background jobs view
│       ├── netdiag/          # Network diagnostics + real-time port monitor
│       ├── oci_resources/    # OCI resource manager (images, launch, network inspect)
│       ├── security/         # Security scanner view
│       ├── shortcut/         # Keybinding display
│       ├── status/           # System status view
│       ├── terminal/         # Embedded terminal
│       ├── theme/            # Colors, styles, icons, theme manager
│       └── workspaces/       # Local workspace manager
└── .claude/
    ├── CLAUDE.md     # AI implementation guidance
    └── rules/        # Detailed TUI coding rules
```

## Entry point and router

```
main.go
└── internal/app/app.go
```

`app.go` is the router: it owns view switching, command mode, and
`shared.State` — everything more than one view needs to read (the forge
session, tool availability, dashboard stats). Views are lazy-loaded; each is
a self-contained [Bubble Tea](https://github.com/charmbracelet/bubbletea)
model with its own `Init`/`Update`/`View` cycle.

## Where the "why" for each area lives

`docs/architecture/` in the repository holds one file per subsystem — read
alongside this reference, or as the source for the
[Explanation](../explanation/index.md) section of this site, which adapts
them for an external reader.
