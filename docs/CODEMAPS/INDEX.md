# DevDesk Codemaps Index

**Last Updated:** 2026-08-01
**Project Type:** Go Bubble Tea TUI Application
**Total LOC:** ~33,300 across 105 Go files
**Architecture:** Multi-view Elm pattern with router orchestration

## What This Is

This directory contains **architectural maps** of the DevDesk codebase. Each file is a quick reference for understanding how the system is organized, what it does, and how data flows through it.

## Quick Navigation

| Document | Focus | Readers |
|----------|-------|---------|
| **[architecture.md](architecture.md)** | System design, view routing, message flow, Bubble Tea lifecycle | Full-stack developers, architecture reviewers |
| **[backend.md](backend.md)** | Package exports, function signatures, interfaces, orchestrators | Backend devs, API consumers, integrators |
| **[data.md](data.md)** | Config schemas, shared state, cache structures, scan results | Data modelers, testers, debuggers |
| **[dependencies.md](dependencies.md)** | External tools, API integrations, version constraints, build tools | DevOps, release managers, dependency auditors |

## Core Concepts at a Glance

### Entry Point

```
main.go
  → config.Load() + theme.ApplyTheme()
  → app.New(cfg)          [Router with lazy-loaded views]
  → tea.NewProgram().Run()
```

### View Types (9 Total)

Press `:` to enter command mode, then type view name:

```
:d[ashboard]           Dashboard (stats aggregator)
:s[tatus]              System monitoring (HTTP/ICMP/DNS)
:gla[uth]              GitLab login
:gle[xplorer]          GitLab project browser
:w[orkspaces]          Local git repositories
:sec[urity]            Trivy + Gitleaks scanner
:cont[ainers]          Docker container list
:oci[-resources]       OCI registry images
:net[diag]             Network diagnostics + port monitor
```

### Message Flow

**All views receive messages via Bubble Tea Update():**

1. User presses key → `tea.KeyMsg`
2. View handler processes → returns `(tea.Model, tea.Cmd)`
3. Cmd performs I/O (network, disk, Docker, etc.)
4. Result → custom `tea.Msg` → back to `Update()`
5. View state modified only in `Update()` (**Rule 110**)

**Cross-view communication:**
- `SwitchViewMsg` — navigate to another view
- `GitLabAuthSuccessMsg` — update shared GitLab state
- `PullCompletedMsg` — sync feedback
- `WorkspaceScanResultLoadedMsg` — cache loading

### Shared State

All views have read-access to `internal/shared/State`:
- GitLab client, auth status, user profile
- Cached groups/projects
- Dashboard metrics (service status, container counts, tool versions)

### Config System

- **Global:** `~/.devdesk/config.yaml` (YAML format)
- **Contexts:** `~/.devdesk/contexts/<name>/config.yaml` (multi-workspace support)
- **Current:** `~/.devdesk/current-context` (text file: context name)
- **Credentials:** Git credential helper (secure, context-aware)
- **Cache:** `~/.devdesk/cache/` (image/workspace scan metadata + results)

### Critical Rules

| Rule | Impact | Consequence |
|------|--------|-------------|
| **Never modify state in Cmd** | Race condition | Use `go test -race ./...` to verify |
| **No `style.Render()` in table.Row{}** | ANSI corruption spreads to all rows | Always use plain text + `SetStyles()` |
| **View.InEditMode() blocks command mode** | Prevent `:` during form input | Return `true` if active form exists |
| **Footer messages auto-clear after 3s** | Transient feedback | Emit `clearFooterMsgCmd()` when setting `footerError`/`footerInfo` |

## Directory Structure

```
devdesk/
├── main.go                      Entry point
├── internal/
│   ├── app/                     Router + view dispatcher (1540 LOC)
│   ├── ui/
│   │   ├── dashboard/           Stats display
│   │   ├── status/              Monitor CRUD
│   │   ├── gitlab/auth/         Login form
│   │   ├── gitlab/explorer/     Project browser
│   │   ├── workspaces/          Local repos
│   │   ├── security/            Scan orchestrator
│   │   ├── containers/          Docker list
│   │   ├── oci_resources/       Registry browser
│   │   ├── netdiag/             Diagnostics + ports
│   │   ├── components/          Shared: filter bar, modals, input
│   │   ├── theme/               Colors, styles, icons
│   │   ├── help/                Help system
│   │   └── shortcut/            Keybinding registry
│   ├── command/                 Parser + completion
│   ├── config/                  YAML loader, multi-context
│   ├── shared/                  Cross-view State
│   ├── cache/                   Image + workspace scan caches
│   ├── scan/                    Trivy + Gitleaks orchestrator
│   ├── docker/                  Docker CLI wrapper
│   ├── oci/                     OCI registry client
│   ├── gitlab/                  GitLab API wrapper
│   ├── status/                  Health checker
│   └── credentials/             Storage interface (File/Memory/Git)
├── docs/CODEMAPS/               This directory
│   ├── INDEX.md                 Navigation guide
│   ├── architecture.md          System design
│   ├── backend.md               Package exports
│   ├── data.md                  Config + state schemas
│   └── dependencies.md          External integrations
└── go.mod / go.sum              Dependencies
```

## Key Integration Points

### Configuration Loading

```go
// From main.go
cfg, _ := config.Load()           // ~/.devdesk/config.yaml
cfg.App.Theme → theme.ApplyTheme()
cfg.Status.Components → status.Checker
cfg.GitLab.URL → gitlabclient.NewClient()
cfg.Scan.* → scan.Scanner options
cfg.Docker.NetworkToolImage → docker.RunPing()
```

### View Initialization (Lazy Loading)

```go
// In app.Update()
case command.ViewSecurity:
  if m.views[command.ViewSecurity] == nil {
    m.views[command.ViewSecurity] = security.New(m.config, m.sharedState)
  }
  return m.switchView(command.ViewSecurity)
```

### Shared State Updates

```go
// In app.Update()
case GitLabAuthSuccessMsg:
  m.sharedState.GitLabClient = msg.Client
  m.sharedState.CurrentUser = msg.User
  m.sharedState.IsAuthenticated = true
  // All views now see updated state
```

### Caching Scan Results

```
User presses Ctrl+S (scan)
  ↓
security.Update() → scan.Scanner.Run()
  ↓
Result written to disk: ~/.devdesk/cache/image-results/<sha256>.json
Metadata updated: ~/.devdesk/cache/image-scans.json
  ↓
Next time: Load from cache, display cached counts
Ctrl+A purges cache, rescans all
```

## Development Workflow

### Making Changes

1. **Understand the message flow:** Read [architecture.md](architecture.md)
2. **Find the right package:** See [backend.md](backend.md) exports
3. **Check data structures:** Review [data.md](data.md) schemas
4. **Update docs:** Keep codemaps in sync with code changes

### Testing

```bash
go test -race ./...           # Detect race conditions (Rule 110)
golangci-lint run             # Lint (Rule 301)
go run . --help               # Manual verification
```

### Before Commit

- [ ] `golangci-lint run` passes
- [ ] `go test -race ./...` all pass
- [ ] Code review (Rule 24)
- [ ] UPDATE this codemap if architecture changed

## Dependency Versions

| Dependency | Version | Reason |
|------------|---------|--------|
| Go | 1.25.5 | Latest stable |
| Bubble Tea | 1.3.10 | TUI framework (must match lipgloss) |
| pro-bing | 0.5.0 | Maintained ping fork (replaced go-ping/ping) |
| Trivy | Latest | CVE database freshness |
| Gitleaks | Latest | Security rules updates |

See [dependencies.md](dependencies.md) for full list + security audit.

## Debugging Tips

### View Rendering Issues
- Check `internal/ui/theme/colors.go` for palette
- Review `internal/ui/theme/styles.go` for style definitions
- Never use `lipgloss.JoinHorizontal()` without background wrapping (Rule 115)

### State Not Updating
- Verify view calls `m.sharedState.` not local copy
- Check `Update()` is the only place modifying model (Rule 110)
- Cmd functions must return messages, not modify state

### Cache Not Loading
- Check metadata exists: `~/.devdesk/cache/image-scans.json`
- Check results file: `~/.devdesk/cache/image-results/<sha256>.json`
- Check write permissions on `~/.devdesk/cache/`

### Command Mode Not Activating
- Check if view implements `FormView.InEditMode()` returning `true`
- If form is active, commands won't work (intentional, Rule 112)
- View can override via `CommandModeView.AllowCommandMode()`

## Contributing

When adding new features:

1. **Update [architecture.md](architecture.md)** if adding a new view or message type
2. **Update [backend.md](backend.md)** if adding new exports or interfaces
3. **Update [data.md](data.md)** if adding config options or data structures
4. **Update [dependencies.md](dependencies.md)** if adding Go dependencies or external tools

Keep each codemap **under 1000 tokens** for scanability. Use refs to files for full details.

---

**Generated:** 2026-03-28 | **Next Review:** After major feature addition or refactor
