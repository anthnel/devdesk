# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> [!IMPORTANT]
> **AI Collaboration Rule**: 
> - Support for **Implementation** and **Bug fixes** only.
> - **Planning** and **Brainstorming** are handled by Gemini (Antigravity).
> - Always refer to [project-context.md](file:///home/anthoni/projects/gitlab/devsecops/devdesk/project-context.md) for global project rules.

## Project Overview

DevDesk is a terminal-based TUI (Text User Interface) application built with Go and Bubble Tea framework. It provides DevSecOps functionality including:
- System status monitoring (HTTP/HTTPS, ICMP, DNS, SSL checks)
- GitLab integration (authentication, project explorer, clone/pull)
- Security scanning (Trivy CVE/misconfig/SBOM + Gitleaks secrets)
- Docker container management with real-time metrics
- OCI resource management (image scanning, container launching, network inspection)
- Network diagnostics (ICMP, DNS, TCP traceroute, Netcat, HTTP, SSL) + real-time port monitoring
- Multi-context configuration management via YAML
- Local workspaces management with git metadata

## Build and Development Commands

Tasks live in `mise.toml` — there is no Makefile. `mise tasks` lists them all.

```bash
# Development (fastest)
mise run dev          # Run directly with go run
go run .

# Build
mise run build        # Build binary to bin/dk
mise run run          # Build and run

# Testing
mise run test         # Run all tests
mise run cover        # Coverage per package
go test -v ./...      # Run all tests with verbose output
go test ./internal/command  # Run tests for specific package
mise run test-race    # Detect race conditions (critical for Bubble Tea Cmds)

# Code Quality
mise run fmt          # Format code
mise run vet          # Run go vet
mise run lint         # MANDATORY before commits - golangci-lint
mise run tidy         # Tidy dependencies
mise run check        # fmt + vet + lint + test in one go

# Installation
mise run install      # Install to $GOPATH/bin
```

`test-race` needs cgo and therefore a C compiler (`gcc`/`clang`) on `PATH`.

## Git workflow — pull requests only

**Never push directly to `main`.** The Entire mirror rejects it:

```
! [remote rejected] main -> main (protected branch)
```

This is enforced by the mirror, not by GitHub (`gh api repos/anthnel/devdesk/branches/main`
reports `"protected": false`). Every other branch, including
`entire/checkpoints/v1`, pushes through the mirror and is forwarded to GitHub.

```bash
git switch -c <branch>                          # work
git push origin <branch>                        # via the mirror — forwarded to GitHub
gh pr create --base main --head <branch>
gh pr merge <n> --squash --delete-branch
git fetch origin && git merge --ff-only origin/main
```

### Remotes

| Remote | URL | Use |
|--------|-----|-----|
| `origin` | `entire://aws-eu-central-1.entire.io/gh/anthnel/devdesk` | Entire mirror — clone, fetch, push branches |
| `github` | `https://github.com/anthnel/devdesk.git` | source of truth; fallback for direct pushes |

The repository is hosted on GitHub and mirrored to EntireDB in `aws-eu-central-1`
(a second placement exists in `aws-us-east-2`). Prefer `origin` for day-to-day
work: it is the regional path and is what keeps agent reads fast.

Semantic search (`entire search`, and the `entire:*` skills that depend on it) is
**not available in the EU region** — the server answers `semantic search is not
yet available in the region(s) hosting this search`. Nothing in the repo config
fixes this.

## Architecture

### Bubble Tea Application Structure

This is a **multi-view TUI application** using the Elm Architecture (TEA) pattern via Bubble Tea. Understanding the routing and view lifecycle is critical:

**Main Components:**
- `main.go` - Entry point, initializes config and Bubble Tea program
- `internal/app/app.go` - **Router/orchestrator** that manages view switching and command mode
- `internal/app/messages.go` - Cross-view messages (GitLab, scan results, pull, etc.)
- `internal/shared/state.go` - SharedState for cross-view data (GitLab client, user, stats)
- `internal/ui/*/` - Individual views

**Key Architecture Pattern:**
```
App (Router)
├── Manages: currentView, commandMode, viewport, sharedState
├── Routes messages to active view
├── Handles view switching via command parser
├── Manages multi-context configuration
└── Views (lazy-loaded):
    ├── dashboard       - Overview (stats, tools, service status)
    ├── status          - System monitoring (CRUD monitors)
    ├── gitlab-auth     - GitLab authentication form
    ├── gitlab-explorer - GitLab project/group browser + clone
    ├── workspaces      - Local workspace management + git metadata
    ├── security        - Trivy + Gitleaks scanner with multi-tab results
    ├── containers      - Docker container list + live metrics
    ├── oci-resources   - OCI resource list, scan, launch containers, network inspection
    └── netdiag         - Network diagnostics (Docker-based tools) + real-time port monitor
```

### View Switching & Command Mode

Press `:` to enter command mode, then type:
- `dashboard` or `d` - Switch to dashboard view
- `status` or `s` - Switch to status view
- `gitlab-auth` or `gla` - Switch to GitLab auth view
- `gitlab-explorer` or `gle` - Switch to GitLab explorer view
- `workspaces` or `w` - Switch to workspaces view
- `security` or `sec` - Switch to security scanner view
- `containers` or `c` - Switch to containers view
- `oci-resources` or `oci` - Switch to OCI resources view
- `netdiag` or `net` - Switch to network diagnostics view
- `context <name>` or `ctx <name>` - Switch configuration context
- `context list` - Show available contexts
- `theme <name>` - Switch UI theme
- `quit` - Exit application

Command parsing and tab-completion live in `internal/command/`. `ParseCommand()` returns a structured `Command{Type, View, Args}` supporting `CommandView`, `CommandContext`, `CommandTheme`, `CommandQuit`, `CommandUnknown`.

**Important:** The `FormView` interface (`InEditMode()`) prevents command mode activation when forms are active. Views with active forms must implement this interface.

### Multi-Context Configuration

The app supports multiple configuration contexts (e.g., work, personal, client-A):
- Contexts are stored in `~/.devdesk/contexts/<name>/config.yaml`
- Current context is tracked in `~/.devdesk/current-context`
- Each context has isolated GitLab credentials via Git Credential Manager
- Context switching reinitializes all views with new config

### Configuration System

Config loaded from `~/.devdesk/config.yaml` with schema defined in `internal/config/config.go`:
- `App` - Global settings (theme, default view, workspaces dir, `secret_backend`)
- `Status` - Monitoring settings (refresh interval, components)
- `GitLab` - GitLab URL and clone settings
- `Registry` - OCI registry configuration (see Registry model below)
- `Scan` - Security scanning (Trivy, Gitleaks)

**No secret goes in this file.** `GitLabConfig` has no `Token` and
`RegistryConfig` has no `Password`; both live in the host secret store (see
Credentials Management). Do not add a secret-bearing field back — the schema is
what makes the guarantee checkable.

Config is injected into views at creation. Use `config.Save()` to persist changes.

### Registry model

`RegistryConfig.Registries` is one flat list holding both plain registries and
repository-manager groups, told apart by `kind` (§3.8). A group fronts several
registries and is pullable itself, which is why they share a list.

| Field | Meaning |
|---|---|
| `slug` | DevDesk's own identifier: what `parent` points at and what the group cache is keyed on. Everything Docker-facing stays keyed on `url`, because Docker is. |
| `kind` | `registry` or `group` |
| `parent` | slug of the owning group — carried by discovered members, not normally by config entries |
| `provider` | `generic`, `nexus`, `harbor`, `artifactory`, `gitlab`; declared, never sniffed from the URL |
| `auth_mode` | `credentials`, `anonymous`, or `inherit` for a member. Replaces `auth_enabled`, which is migrated at load and then dropped. |

**`auth_mode` is read before any credential lookup**, on both paths that talk to
a registry: `detectRegistryGroupCmd` and the browser's `credsFor`. `anonymous`
sends nothing, not even a configured username. This is what `auth_enabled` never
did (D12): `docker login` is keyed on host, so one login against a Nexus
instance used to authenticate every repository it serves.

That same host-keying is why a **member cannot declare `credentials` of its
own** — it shares its group's single credential entry, so a password of its own
has nowhere to go — and why `inherit` on an entry with no group is refused.
Both are load-time errors.

`internal/config/registries.go` normalizes the list at load and is the only
place that decides a slug. Two rules hold it together:

- A slug **DevDesk derives** (from the alias, else the URL host) is made unique
  by stepping aside — `prod`, `prod-2`. A slug **the file declares** is never
  rewritten, because it is a link target; a duplicate, or a `parent` naming no
  configured group, makes `LoadContext` fail rather than load a config the
  application cannot honour.
- An entry from before `kind` existed that carries a `management_url`, **or**
  whose URL contains `/repository/`, migrates to `kind: group`,
  `provider: nexus` — those were the two things `NexusDetector.CanHandle` used
  to accept. A kind the file states is never second-guessed.

`provider` is what picks the detector in `internal/registrymgr`: `DetectGroup`
matches `Detector.Provider()` against the declared value and falls back to
`GenericDetector`, which discovers nothing. A registry is probed because it was
declared as something, never because its URL looked like it — so registration
order decides nothing, and a plain registry costs no HTTP call. The provider
names are stated in both packages on purpose; `TestTheProviderVocabularyMatchesTheConfig`
keeps them in step.

`RegistryForm` is what keeps the file loadable: it refuses a duplicate or
badly-formed slug instead of correcting it, and drops the group-only fields when
the kind is not a group.

### Shared State

`internal/shared/state.go` holds cross-view data injected at view creation:
- `Secrets`, `SecretNotices` — the context's secret store and what the migration off plaintext reported
- `GitLabClient`, `IsAuthenticated`, `CurrentUser` — GitLab session
- `CachedGroups`, `CachedProjects` — GitLab data cache
- `GitLabStats`, `DockerStats`, `OCIStats` — Dashboard counters
- `ServiceStatus`, `ServiceComponents` — Status monitoring results
- `WorkspaceCount`, `Tools []ToolInfo` — Tool availability (Trivy, Gitleaks, Docker)

### Cross-View Communication

Key messages in `internal/app/messages.go`:
- `SwitchViewMsg` — navigate to another view
- `SelectionRequestMsg` / `SelectionResultMsg` — selection mode (e.g., workspaces opened from security view to pick a repo)
- `ImageScanResultLoadedMsg` / `WorkspaceScanResultLoadedMsg` — cached results ready

### Security Scanning

`internal/scan/` orchestrates Trivy + Gitleaks:
- `scanner.go` — runs both tools concurrently, streams progress via `ProgressUpdate` channel
- `trivy.go` — CVE, SBOM, misconfiguration detection
- `gitleaks.go` — secrets detection with custom config support

**Security view** (`internal/ui/security/model.go`) has four states: `StateInput` → `StateScanning` → `StateResults` → `StateDetails` (with remediation info).

### Registry group cache

`internal/cache/registry_groups.go` — `RegistryGroupCache`, keyed by **group
slug**, metadata at `~/.devdesk/cache/registry-groups.json`. It holds the members
one discovery found and when: discovered members are derived data with a server
as their source of truth, and `config.yaml` is what the user declares.

- Discovery runs **only for `kind: group`**, and only when asked: `ctrl+r` on a
  group row in the Registries tab. `detectRegistryGroupCmd` writes through.
- An empty result **is** stored — "asked, and it is not a group" is an answer.
  A *failed* one is not: an unreachable manager must not erase what was last
  known, which is why `registrymgr` distinguishes the two (D23).
- The `Members` column shows `count · TimeAgo(discovered_at)`, or `never`. It is
  not decoration: a cache with no visible age looks current whatever it holds.
- The **registry browser reads this cache**, never the network: it opens on the
  first frame and works offline. `→` on a group row in the Registries tab drills
  into its members, `←`/`esc` go back.
- `internal/cache/browser_selection.go` remembers what the browser had
  **un**checked, per context. Storing the exceptions is what makes a
  newly-discovered member arrive checked rather than silently excluded.

### Scan Cache

Two independent disk+memory caches in `internal/cache/`:
- `ImageScanCache` — keyed by `"repo:tag"`, metadata at `~/.devdesk/cache/image-scans.json`, full results in `image-results/<sha256>.json`
- `WorkspaceScanCache` — keyed by absolute repo path, metadata at `~/.devdesk/cache/workspace-scans.json`, full results in `workspace-results/<sha256>.json`

Cache invalidation: `ctrl+s` (single) overwrites; `ctrl+a` (all) purges cache then rescans.

### Docker / OCI Integration

- `internal/docker/client.go` — wraps Docker CLI (exec-based): list, metrics, stop, restart, pause, remove, prune
- `internal/docker/netdiag.go` — ephemeral container runners with `--network host`: `RunPing`, `RunDNS`, `RunTraceroute`, `RunTCPTraceroute`, `RunNetcat`, `RunCurl`, `RunSSLCert` → returns `DiagResult{Success, Output}`
- `internal/docker/network.go` — `RunSS(image, numeric)` for real-time port table (mounts host DNS files, uses `--privileged --net=host --pid=host`), `KillProcess(image, pid)`, `PortInfo` struct, `parseSSOutput()` multi-format parser
- `internal/oci/oci.go` — OCI registry HTTP client: list tags/templates, download + extract tar.gz

### Network Diagnostics View

`internal/ui/netdiag/` — two-tab interface:
- **Diagnostics tab** (`model.go`): Interactive form with target/port inputs and checkboxes to select tests (ICMP, DNS, Traceroute, TCP Traceroute, Netcat, HTTP/HTTPS, SSL). Runs selected tests in parallel via Docker ephemeral containers. Results table uses Nerd Font icons.
- **Ports tab** (`ports_model.go`): Live `ss` monitoring with real-time filtering by protocol (TCP/UDP), state (LISTEN/ESTAB), and text search. `ctrl+k` kills a process (requires privileged container). Active filter shown in status line.

Both tabs use Docker with host network/PID namespaces. DNS hostname resolution uses mounted host DNS files (`/etc/resolv.conf`, `/etc/hosts`, `/etc/nsswitch.conf`).

### Status Monitoring System

**Factory Pattern for Checkers:**
- `internal/status/checker.go` - Main orchestrator
- `internal/status/http_checker.go` - HTTP/HTTPS checks
- `internal/status/icmp_checker.go` - ICMP ping checks
- `internal/status/dns_checker.go` - DNS resolution checks

Each checker implements `CheckerInterface`. The main `Checker.CheckOne()` uses a factory pattern to instantiate the right checker based on `component.Type`.

**Parallel Execution:**
- `CheckAll()` runs all component checks concurrently using goroutines and sync.WaitGroup
- Results are collected and returned as `[]ComponentStatus`

### Credentials Management

No secret DevDesk holds is written to a file DevDesk owns. `internal/credentials`
implements the `Storage` interface three ways and `Select()` picks exactly one —
writing to several at once is what let the old "secure" option store a token in
the credential manager *and* in plaintext:

| Storage | Backend | Notes |
|---------|---------|-------|
| `KeyringStorage` | Windows Credential Manager / macOS Keychain / Secret Service | via `zalando/go-keyring`, no cgo. The default. |
| `GitCredentialStorage` | git's configured credential helper | Only when the helper is not `store`, which writes plaintext. Context-aware. |
| `MemoryStorage` | this process | Last resort, and deliberately worse: the auth view says nothing was saved. |

`Select(context, preference)` returns a `Selection{Storage, Backend, Detail}`.
The order is keyring → git credential → memory; `app.secret_backend` in the
context config pins the head of it (`auto`, `keyring`, `git-credential`). A
pinned backend that is unreachable falls through to memory rather than silently
to the other one.

The router resolves one `Selection` per context and keeps it in
`shared.State.Secrets`. It must be reused for the lifetime of the context — a
second `Select()` call yields a fresh `MemoryStorage` that cannot see what the
first one holds.

`MigrateLegacySecrets` runs at startup and on every context switch: a
`gitlab.token` or `registry.password` left in a context file by an older build
is moved into the store and deleted from the YAML, with the result reported in
the auth view.

### Bubble Tea Message Flow

Custom messages defined in view models (e.g., `internal/ui/status/model.go`):
```go
type TickMsg time.Time           // Countdown timer
type CheckStartedMsg struct{}    // Check initiated
type CheckCompleteMsg struct{}   // Check results ready
```

Commands return these messages to trigger async operations. The Bubble Tea `Update()` method handles them.

### Component CRUD Operations

Status view supports adding/editing/deleting monitors:
- `internal/ui/status/components/component_form.go` - Form component
- `internal/ui/status/components/confirm_modal.go` - Confirmation dialog
- Changes persist to `~/.devdesk/config.yaml` via `config.Save()`

State flags in Model: `creating`, `editing`, `confirming`, `selectedIdx`

## Tables — `internal/ui/datatable`

The shared mechanism behind the application's tables: column widths, sorting,
sort arrows, filter matching, cursor clamping and cursor-to-object resolution.
`theme.DefaultTableStyles()` and friends still own the *look*.

```go
datatable.New(datatable.Config[T]{
    Columns: []datatable.Column[T]{{
        Title: "Name", MinWidth: 20, Flex: 1,
        Cell:   func(x T) string { … },  // plain text — Rule 122 by construction
        Less:   func(a, b T) bool { … }, // nil = not sortable
        Search: func(x T) string { … },  // nil = not searchable
    }},
    SortColumn:     0,
    SelectedStyles: func(x T) table.Styles { … }, // e.g. TableStylesForSeverity
})
```

Three things it guarantees that hand-wired tables did not:

- **`Selected()` cannot disagree with the screen.** The filtered, sorted slice is
  built once and kept; nothing replays the pipeline to resolve a cursor.
- **Rule 116 holds at every width.** The solver distributes the shortfall across
  columns rather than clamping each one after the remainder is computed, which is
  how several views overflowed on narrow terminals.
- **The cursor is clamped in one place** — `SetItems`, both ends — and otherwise
  left where it was, so a periodic refresh keeps the scroll position. `GotoTop()`
  is explicit for views that do want a reset.

`SelectedStyles` returns styles rather than a state keyword so the component
never learns what a severity is. `containers` is its one client: it is the only
table whose selection colour depends on the row (exited or dead reads as an
error).

When a row shows something that is not on the domain object — the images tab
shows the scan cache, whether a scan is running, and the alias-substituted name
— the view defines a **row type** carrying that decoration (`imageRow`) rather
than closing over the model. The columns are built once, in `New`, so they
cannot reach it; and carrying it means a column sorts by the same value it
prints.

`SetCursor` is clamped and exists for the views that remember a position across
a reload — workspaces and the explorer each restore one per drill-down level.
The selected row is pinned to the content width: column widths count cells, and
a Nerd Font icon does not always render as wide as it counts, so the highlight
would otherwise stop short of the right border.

Migration of the fifteen existing tables is step-by-step: `oci_resources`
networks, volumes and images, `netdiag` ports, `containers`, `workspaces` and
the GitLab `explorer` are done — `security` and `status` are what is left. See
the backlog.

## Testing

Test files follow Go conventions (`*_test.go`):
- `internal/command/parser_test.go` - Command parser tests
- `internal/app/app_test.go` - App router tests

Use table-driven tests where appropriate.

## Dependencies

Key libraries (see `go.mod`):
- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/bubbles` - Pre-built TUI components (table, textinput, spinner)
- `github.com/charmbracelet/lipgloss` - Styling
- `gitlab.com/gitlab-org/api/client-go` - GitLab API client
- `github.com/prometheus-community/pro-bing` - ICMP ping functionality (maintained fork of go-ping/ping)
- `github.com/zalando/go-keyring` - host secret manager (wincred / Keychain / Secret Service), no cgo
- `gopkg.in/yaml.v3` - YAML configuration

## Code Conventions

- Comments: French or English both accepted
- UI text and logs: **English US only** (Rule 129)
- Bubbletea models define their own message types
- Use `theme` package for consistent styling (`internal/ui/theme/`)
- Config changes must call `config.Save()` to persist
- All colors defined in `theme/colors.go`, all styles in `theme/styles.go`
- Message naming: `[ComponentName][Action]Msg` (e.g., `ComponentFormSubmitMsg`)

## Critical Bubble Tea Rules

**NEVER modify model state inside a Cmd** (Rule 110) - Race condition:
- `Update()` is the ONLY place to modify model state
- `View()` is read-only
- `Cmd` functions do I/O and return messages

**NEVER use `style.Render()` inside `table.Row{}`** (Rule 122) - ANSI sequences corrupt all subsequent rows:
- Use plain text: `theme.IconError + " error"` not `theme.StatusErrorStyle.Render(...)`
- Apply row styling via `table.SetStyles(theme.TableStylesForState("error"))`

**Update() case extraction**: If a case block exceeds 5 lines, extract to `handle[MessageType]()` method returning `(tea.Model, tea.Cmd)`
