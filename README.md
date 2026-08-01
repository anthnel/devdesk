# DevDesk

A terminal-based DevSecOps workstation built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea). DevDesk centralizes the tools a developer needs daily — GitLab, Docker, security scanning, and system monitoring — in a single keyboard-driven TUI.

## Features

| View                    | Description                                                                               |
| ----------------------- | ----------------------------------------------------------------------------------------- |
| **Dashboard**           | Overview of GitLab stats, Docker usage, OCI images, tool availability, and service health |
| **Status**              | Real-time system monitoring (HTTP/HTTPS, ICMP ping, DNS, SSL) with CRUD for monitors      |
| **GitLab Explorer**     | Browse groups and projects, clone/pull repositories, create and delete resources          |
| **Workspaces**          | Navigate local git repositories with metadata (branch, status, last scan)                 |
| **Security**            | Run Trivy (CVE, misconfig, SBOM) and Gitleaks (secrets) scans with remediation details    |
| **Containers**          | List and manage Docker containers with live CPU/memory/network metrics                    |
| **OCI Resources**       | Browse OCI images, scan them, launch containers, and inspect container networks           |
| **Network Diagnostics** | ICMP, DNS, Traceroute, TCP Traceroute, Netcat, HTTP, SSL checks + real-time port monitor  |

**Multi-context support** — maintain isolated configurations (GitLab credentials, registries, scan settings) for multiple environments (work, personal, client-A, …).

## Requirements

- Go 1.25.5+
- A terminal with [Nerd Font](https://www.nerdfonts.com/) support (icons are used throughout the UI)
- Optional external tools (features degrade gracefully if absent):
  - [Trivy](https://trivy.dev/) — CVE and misconfiguration scanning
  - [Gitleaks](https://github.com/gitleaks/gitleaks) — secret detection
  - Docker — container management and image scanning

## Installation

```bash
# Clone the repository
git clone https://github.com/anthnel/devdesk.git
cd devdesk

# Build
mise run build      # → bin/dk

# Or install directly to $GOPATH/bin
mise run install    # → dk
```

## Usage

```bash
# Run (development)
mise run dev

# Run (built binary)
./bin/dk
```

Press `:` to open the command palette:

| Command           | Alias        | Description              |
| ----------------- | ------------ | ------------------------ |
| `dashboard`       | `d`, `dash`  | Dashboard overview       |
| `status`          | `s`          | System status monitors   |
| `gitlab-auth`     | `gla`        | GitLab authentication    |
| `gitlab-explorer` | `exp`        | GitLab project browser   |
| `workspaces`      | `ws`         | Local workspace manager  |
| `security`        | `sec`        | Security scanner         |
| `containers`      | `cont`, `ct` | Docker container manager |
| `oci-resources`   | `oci`        | OCI resource manager     |
| `net`             |              | Network diagnostics      |
| `ctx <name>`      |              | Switch context           |
| `context list`    |              | List available contexts  |
| `quit`            | `q`          | Exit                     |

### Key bindings (global)

| Key                | Action                     |
| ------------------ | -------------------------- |
| `↑ / ↓` or `k / j` | Navigate list              |
| `Tab / Shift+Tab`  | Switch tabs / cycle fields |
| `← / →` or `h / l` | Go back / drill down       |
| `Enter`            | Select / confirm           |
| `Esc`              | Cancel / go back           |
| `ctrl+s`           | Scan selected item         |
| `ctrl+d`           | Delete selected item       |
| `ctrl+r`           | Reload / refresh           |
| `ctrl+n`           | Create new resource        |
| `e`                | Edit selected resource     |
| `.`                | Cycle sort column          |
| `/`                | Filter                     |
| `?`                | In-app help                |
| `q`                | Quit                       |

## Configuration

Config is stored per context in `~/.devdesk/contexts/<name>/config.yaml`. The active context is tracked in `~/.devdesk/current-context`.

```yaml
app:
  theme: dark           # dark | light
  default_view: dashboard
  workspaces_dir: ~/projects

gitlab:
  url: https://gitlab.com
  clone_dir: ~/projects

registry:
  url: registry.example.com
  username: myuser

scan:
  trivy:
    enabled: true
  gitleaks:
    enabled: true
```

GitLab tokens are stored securely via the system's git credential helper (not in the config file).

## Development

### Setup

```bash
git clone https://github.com/anthnel/devdesk.git
cd devdesk
go mod download
```

The repository is mirrored to [Entire](https://entire.io) in `aws-eu-central-1`.
Cloning from the mirror gives faster regional access and is the preferred remote
for coding agents:

```bash
git clone entire://aws-eu-central-1.entire.io/gh/anthnel/devdesk
```

### Contributing — pull requests only

`main` cannot be pushed to directly: the Entire mirror rejects it with
`remote rejected: main -> main (protected branch)`. Every other branch pushes
through the mirror and is forwarded to GitHub.

```bash
git switch -c my-feature
git push origin my-feature
gh pr create --base main --head my-feature
gh pr merge <n> --squash --delete-branch
git fetch origin && git merge --ff-only origin/main
```

### Commands

Tasks are defined in `mise.toml`. Run `mise tasks` to list them with descriptions.

```bash
mise run dev        # Run with go run (fastest iteration loop)
mise run build      # Build binary to bin/dk
mise run test       # Run all tests
mise run cover      # Test coverage per package
mise run fmt        # Format code (go fmt)
mise run vet        # Static analysis (go vet)
mise run lint       # Full linter (golangci-lint, mandatory before committing)
mise run tidy       # Tidy go.mod / go.sum
mise run check      # fmt + vet + lint + test (pre-commit checklist)
mise run test-race  # Race condition detection (needs a C toolchain, see below)
```

`test-race` requires cgo, so a C compiler (`gcc` or `clang`) must be on `PATH`.
Without one, `go test -race` fails with `cgo: C compiler "gcc" not found`.

On Windows, tasks that use POSIX syntax declare `shell = "bash -c"` and therefore
need Git Bash, which ships with Git for Windows.

### Architecture

DevDesk is a multi-view TUI following the [Elm Architecture](https://guide.elm-lang.org/architecture/) via Bubble Tea:

```
main.go
└── internal/app/app.go          ← Router: manages view switching, command mode, shared state
    ├── internal/command/        ← Command parser + tab-completion
    ├── internal/shared/state.go ← Cross-view state (GitLab client, stats, tools)
    ├── internal/config/         ← YAML config, multi-context loading/saving
    ├── internal/cache/          ← Disk+memory scan result cache
    ├── internal/scan/           ← Trivy + Gitleaks orchestration
    ├── internal/status/         ← HTTP/ICMP/DNS/SSL checker factory
    ├── internal/gitlab/         ← GitLab API client + git operations
    ├── internal/docker/         ← Docker CLI wrapper, netdiag ephemeral runners, port monitor (ss)
    ├── internal/oci/            ← OCI registry HTTP client
    └── internal/ui/             ← All views + shared theme system
        └── theme/               ← Centralized colors, styles, icons (Catppuccin palette)
```

Views are lazy-loaded. Each view is a self-contained Bubble Tea model with its own `Init / Update / View` cycle.

### Adding a new view

1. Create `internal/ui/<viewname>/model.go` with a `Model` struct implementing `tea.Model`
2. Add a `ViewType` constant in `internal/command/parser.go`
3. Register the view in `internal/app/app.go` (lazy-load pattern, same as existing views)
4. Implement `GetHelpContent()` (satisfies `help.Provider` — required for `?` key)
5. Implement `InEditMode() bool` if the view has forms (blocks command mode while editing)
6. Follow the keybinding standards in `.claude/rules/tui-layout.md` (Rule 111)

### Critical rules for contributors

**Never modify model state inside a `Cmd`** — Cmds run concurrently. Only `Update()` is allowed to mutate the model (Rule 110).

**Never use `style.Render()` inside `table.Row{}`** — ANSI escape sequences break `runewidth.Truncate()` and corrupt all subsequent rows. Use plain text in rows; apply styling via `table.SetStyles()` (Rule 122).

**All UI text and log messages must be in English US** (Rule 129).

**Closed-set fields (e.g., enum selects) use the `←/→` cycle pattern** — no dropdowns, no `Enter` to cycle (Rule 132).

**Background color is not inherited** by `lipgloss.JoinHorizontal/Vertical` — use `theme.PadWithBg()`, `theme.BgLine()`, and `theme.EmptyLineBg()` helpers instead (Rule 115).

The full set of TUI rules lives in `.claude/rules/`.

### Code conventions

- Message naming: `[ComponentName][Action]Msg` (e.g., `ScanCompleteMsg`)
- `Update()` cases exceeding 5 lines are extracted to `handle[MessageType]()` returning `(tea.Model, tea.Cmd)`
- All colors defined in `theme/colors.go` using the Catppuccin palette — no hex literals elsewhere
- Errors are logged via `log.Printf("ERROR [package] action: %v", err)` and shown as a short footer message, never in the viewport
- Comments may be written in French or English

### Testing

```bash
go test ./...                        # All packages
go test -v ./internal/status/...     # Specific package, verbose
go test -race ./...                  # Race condition check
go test -run TestParseCommand ./...  # Single test by name
```

Test files follow Go conventions (`*_test.go`). Use table-driven tests.

## Project structure

```
devdesk/
├── main.go
├── go.mod
├── mise.toml
├── internal/
│   ├── app/          # Router, header, cross-view messages
│   ├── cache/        # Scan result caching (images + workspaces)
│   ├── command/      # Command parser + completion
│   ├── config/       # YAML config + multi-context
│   ├── credentials/  # Credential storage (file, memory, git-credential)
│   ├── docker/       # Docker CLI wrapper, netdiag runners, port monitor (ss)
│   ├── gitlab/       # GitLab API + git operations
│   ├── oci/          # OCI registry client
│   ├── scan/         # Trivy + Gitleaks integration
│   ├── shared/       # Cross-view shared state
│   ├── status/       # Status checker factory (HTTP/ICMP/DNS/SSL)
│   └── ui/
│       ├── components/      # Reusable modals (confirm, report, selector, filter bar)
│       ├── containers/      # Docker containers view
│       ├── dashboard/       # Overview dashboard
│       ├── gitlab/auth/     # GitLab login form
│       ├── gitlab/explorer/ # GitLab project tree browser
│       ├── help/            # In-app help overlay
│       ├── netdiag/         # Network diagnostics + real-time port monitor
│       ├── oci_resources/   # OCI resource manager (images, launch, network inspect)
│       ├── security/        # Security scanner view
│       ├── shortcut/        # Keybinding display
│       ├── status/          # System status view
│       ├── terminal/        # Embedded terminal
│       ├── theme/           # Colors, styles, icons, theme manager
│       └── workspaces/      # Local workspace manager
└── .claude/
    ├── CLAUDE.md     # AI implementation guidance
    └── rules/        # Detailed TUI coding rules
```

## License

See [LICENCE](./LICENCE).
