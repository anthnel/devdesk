# DevDesk

A terminal-based DevSecOps workstation built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea). DevDesk centralizes the tools a developer needs daily — GitLab or GitHub, Docker, security scanning, and system monitoring — in a single keyboard-driven TUI.

## Features

| View                    | Description                                                                               |
| ----------------------- | ----------------------------------------------------------------------------------------- |
| **Dashboard**           | Overview of forge stats, Docker usage, OCI images, tool availability, and service health  |
| **Status**              | Real-time system monitoring (HTTP/HTTPS, ICMP ping, DNS, SSL) with CRUD for monitors      |
| **Forge Auth**          | Authenticate against the context's forge — GitLab or GitHub, one at a time                |
| **Forge Explorer**      | Browse groups/namespaces and projects/repositories, clone/pull, create and delete resources |
| **Workspaces**          | Navigate local git repositories with metadata (branch, status, last scan)                 |
| **Security**            | Run Trivy (CVE, secrets, licenses, misconfig) and Gitleaks (secrets) scans with remediation details |
| **Containers**          | List and manage Docker containers with live CPU/memory/network metrics                    |
| **OCI Resources**       | Browse OCI images, scan them, launch containers, and inspect container networks           |
| **Network Diagnostics** | ICMP, DNS, Traceroute, TCP Traceroute, Netcat, HTTP, SSL checks + real-time port monitor  |
| **Configuration**       | Edit the active context's settings, switch theme, manage contexts                         |
| **Jobs**                | What is running and what has run this session — scans and image pulls are background jobs, not blocking waits |
| **About**               | Which build is running: version, commit, build date, and where config/cache/log live      |

**Multi-context support** — maintain isolated configurations (forge credentials, registries, scan settings) for multiple environments (work, personal, client-A, …). Each context targets exactly one forge, GitLab or GitHub.

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

The built binary reports its own version, commit and build date (`:about`,
also `:version`) — it is stamped at build time via `-ldflags`, not read from a
committed file. A `go run .` / `mise run dev` build reports `dev`.

## Usage

```bash
# Run (development)
mise run dev

# Run (built binary)
./bin/dk
```

Press `ctrl+p` (or `:`, outside a text field) to open the command palette:

| Command           | Alias        | Description                    |
| ------------------ | ------------ | ------------------------------ |
| `dashboard`       | `d`, `dash`  | Dashboard overview              |
| `status`          | `s`          | System status monitors          |
| `git-auth`        | `ga`         | Forge authentication (GitLab/GitHub) |
| `git-explorer`    | `ge`         | Forge project/repository browser |
| `workspaces`      | `ws`, `w`    | Local workspace manager         |
| `security`        | `sec`        | Security scanner                |
| `containers`      | `cont`, `ct` | Docker container manager        |
| `oci-resources`   | `oci`        | OCI resource manager            |
| `netdiag`         | `net`        | Network diagnostics             |
| `configuration`   | `config`, `cfg` | Configuration view           |
| `jobs`            | `j`          | Background jobs (scans, pulls)  |
| `about`           | `version`    | Build info                      |
| `ctx <name>`      |              | Switch context                  |
| `context list`    |              | List available contexts         |
| `quit`            | `q`          | Exit                             |

There is one spelling per command — see `docs/architecture/app-shell.md` if
you are looking for the exact parsing rules.

### Key bindings (global)

| Key                | Action                     |
| ------------------ | -------------------------- |
| `↑ / ↓`            | Navigate list               |
| `Tab / Shift+Tab`  | Switch tabs / cycle fields  |
| `← / →`            | Go back / drill down        |
| `Enter`            | Select / confirm            |
| `Esc`              | Cancel / go back            |
| `ctrl+p`           | Open the command palette    |
| `ctrl+r`           | Refresh — nothing else      |
| `.`                | Cycle sort column           |
| `/`                | Filter                      |
| `?`                | In-app help                 |
| `q`                | Quit                        |

Resource actions (`N` create, `S` scan, `D` delete, `E` edit, `T` terminal,
`L` logs, …) are a single **uppercase** letter, identical across every
view — see `.claude/rules/tui-layout.md` (Rule 111) for the complete table.
There is no `ctrl+`-prefixed alternative for any of them: only three `Ctrl`
combinations exist in the whole app (`ctrl+c`, `ctrl+r`, `ctrl+p`).
Lowercase letters are always local filters/toggles, never actions, and
vim-style `hjkl` navigation aliases do not exist.

## Configuration

Config is stored per context: the `default` context lives at
`~/.devdesk/config.yaml`, any other context `<name>` at
`~/.devdesk/config-<name>.yaml`. The active context is tracked in
`~/.devdesk/.current-context`.

```yaml
app:
  theme: catppuccin-mocha   # see Themes below
  default_view: dashboard
  workspaces_dir: ~/workspaces   # scanned by Workspaces, and where the explorer clones into

forge:
  type: gitlab               # gitlab | github — one per context, empty means gitlab
  url: https://gitlab.com
  default_parent_group: my-group   # GitLab only

registry:
  url: registry.example.com
  username: myuser

scan:
  trivy_source: auto        # auto | binary | image
  enable_vuln: true
  enable_secret: true
  gitleaks_source: auto

mcp:
  enabled: false             # exposes DevDesk's data over MCP, off by default
  listen: 127.0.0.1:7777
```

Credentials (forge tokens, registry passwords) are stored via the host's
keyring or git credential helper — never in the config file. See
`docs/architecture/configuration.md` for the full schema and
`.claude/CLAUDE.md` (`## Credentials Management`) for the storage backends.

### Themes

The built-in `default` theme needs no setup. Twelve more ship as JSON files
in `themes/` in this repository — `catppuccin-mocha`, `catppuccin-latte`,
`catppuccin-frappe`, `catppuccin-macchiato`, `tokyo-night`, `one-dark`,
`dark-plus` (Windows Terminal Dark+), `nord`, `solarized-dark`,
`solarized-light`, `github-dark`, `github-light` — but DevDesk only looks
for theme files in `~/.devdesk/themes/`, so copy the ones you want there
first:

```bash
mkdir -p ~/.devdesk/themes
cp themes/*.json ~/.devdesk/themes/
```

Once copied, they appear in the `:config` view's theme cycle (`←/→`), or
set `app.theme` directly to a theme's file name without `.json`.

### MCP server

DevDesk can serve what it knows — status, workspaces, scan results, forge
stats — over the Model Context Protocol, in Streamable HTTP, from the
running TUI's own process. It is off by default (`mcp.enabled: false`); once
on, it listens on `mcp.listen` (loopback by default) and `mcp.expose` lets
you allow-list which tools are exposed. See `docs/architecture/mcp.md`.

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

Any change that will produce a commit starts in its own worktree under
`.worktrees/<branch>/` inside the repository — not a sibling directory — and
releases go through `release-please` + `goreleaser`, triggered by merging the
release PR it keeps open. The full workflow, including sandbox/host-relay
development, is documented in `.claude/CLAUDE.md`.

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
    ├── internal/shared/state.go ← Cross-view state (forge session, stats, tools)
    ├── internal/config/         ← YAML config, multi-context loading/saving
    ├── internal/cache/          ← Disk+memory scan result cache
    ├── internal/scan/           ← Trivy + Gitleaks orchestration
    ├── internal/status/         ← HTTP/ICMP/DNS/SSL checker factory
    ├── internal/forge/          ← GitLab/GitHub abstraction (session, namespaces, repositories)
    ├── internal/docker/         ← Docker CLI wrapper, netdiag ephemeral runners, port monitor (ss)
    ├── internal/oci/            ← OCI registry HTTP client
    ├── internal/jobs/           ← Background job registry (scans, pulls) + the :jobs view's spinner chain
    ├── internal/mcp/            ← MCP server exposing DevDesk's data over Streamable HTTP
    └── internal/ui/             ← All views + shared theme system
        └── theme/               ← Centralized colors, styles, icons (Catppuccin-based palette + user themes)
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

**A `datatable`'s body is always the table**, loading, empty or filtered included — never a spinner or a message swapped in for it (Rule 139).

The full set of TUI rules lives in `.claude/rules/`.

### Code conventions

- Message naming: `[ComponentName][Action]Msg` (e.g., `ScanCompleteMsg`)
- `Update()` cases exceeding 5 lines are extracted to `handle[MessageType]()` returning `(tea.Model, tea.Cmd)`
- All colors defined in `theme/colors.go` — no hex literals elsewhere
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
│       ├── help/            # In-app help overlay
│       ├── jobsview/        # Background jobs view
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

For the "why" behind each area — not just the "what" — see
`docs/architecture/*.md`, one file per area, referenced from `.claude/CLAUDE.md`.

## License

See [LICENCE](./LICENCE).
