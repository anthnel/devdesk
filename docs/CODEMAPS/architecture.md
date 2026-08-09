# DevDesk Architecture Codemap

**Last Updated:** 2026-08-01
**File Count:** 105 Go files (~33k LOC)
**Entry Points:** `main.go` → `internal/app/app.go` (router)

## System Overview

DevDesk is a **Bubble Tea TUI application** implementing multi-view navigation with shared state. The architecture follows the Elm pattern: `Update()` modifies state, `View()` renders, `Cmd` functions perform I/O and return messages.

```
main.go
  ↓
config.Load() → theme.ApplyTheme()
  ↓
app.New() → Router (currentView, views map, sharedState)
  ↓
Bubble Tea program.Run()
```

## View Routing Architecture

**App Router** (`internal/app/app.go`) orchestrates view switching and command mode:

| Component | Purpose |
|-----------|---------|
| `App.currentView` | Active view type (ViewType enum) |
| `App.views` | Lazy-loaded view instances (map[ViewType]tea.Model) |
| `App.sharedState` | Cross-view data (GitLab client, cache, stats) |
| `App.commandMode` | Command input (`:/` prefix) active state |
| `App.commandInput` | textinput.Model for `:cmd` mode |

**View Command Map** (`internal/command/parser.go`):

```
:d[ashboard]      → ViewDashboard
:s[tatus]         → ViewStatus
:gla[uth]         → ViewGitlabAuth
:gle[xplorer]     → ViewGitlabExplorer
:w[orkspaces]     → ViewWorkspaces
:sec[urity]       → ViewSecurity
:cont[ainers]     → ViewContainers
:oci[-resources]  → ViewOCIResources
:net[diag]        → ViewNet
:ctx[context]     → Context switch (reinit views)
:quit / :q        → Exit app
```

## View Models

| View | File | Purpose |
|------|------|---------|
| Dashboard | `internal/ui/dashboard/` | Stats aggregator, tool availability, service health |
| Status | `internal/ui/status/` | HTTP/HTTPS/ICMP/DNS monitors, CRUD operations |
| GitLab Auth | `internal/ui/gitlab/auth/` | Token entry, authentication state |
| GitLab Explorer | `internal/ui/gitlab/explorer/` | Group/project browser, multi-select clone, creation |
| Workspaces | `internal/ui/workspaces/` | Local git repos, CRUD, terminal launch, selection mode |
| Security | `internal/ui/security/` | Inventory of what has been scanned, rescan, result tabs |
| Containers | `internal/ui/containers/` | Docker container list, metrics, control |
| OCI Resources | `internal/ui/oci_resources/` | Registry images, scan, launch, network inspect |
| Network Diag | `internal/ui/netdiag/` | Diagnostics (ping, DNS, traceroute) + real-time port monitor |

## Data Flow

### Shared State (`internal/shared/state.go`)

Injected at view creation, holds mutable cross-view data:

```go
type State struct {
  GitLabClient    *gitlabclient.Client
  IsAuthenticated bool
  CurrentUser     *gitlabclient.User
  CachedGroups    []*gitlabclient.Group
  CachedProjects  []*gitlabclient.Project
  ServiceStatus   ServiceGlobalStatus  // all_ok|degraded|down|unknown
  ServiceComponents []status.ComponentStatus
  GitLabStats, DockerStats, OCIStats
  WorkspaceCount  int
  Tools           []ToolInfo
}
```

### Message Flow

**Inter-view messages** (`internal/app/messages.go`):
- `SwitchViewMsg{View}` — navigation
- `GitLabAuthSuccessMsg`, `GitLabLogoutMsg` — auth state
- `PullCompletedMsg`, `PullErrorMsg` — sync feedback
- `WorkspaceScanResultLoadedMsg`, `ImageScanResultLoadedMsg` — cache loading

**Per-view messages** defined in each view's `model.go`:
- `TickMsg`, `CheckCompleteMsg`, `FormSubmitMsg`, etc.

### Command Execution

Commands (I/O operations) return to `Update()`:

```
view.Update(msg) → (tea.Model, tea.Cmd)
                     ↓
                   Cmd func
                     ↓
              tea.Msg (sent back to Update)
```

## UI Layout (`internal/ui/theme/`)

**Fixed header** (7 lines):
- View name + shortcuts (top 2 lines)
- Keybindings (5 lines)

**Viewport** (dynamic):
- Bordered rectangle, content rendered via `lipgloss`
- Background propagation via `theme.Bg*()` helpers

**Footer** (2–3 lines):
- Tab bar (if present)
- Empty line separator
- Info line (status/error centered)

## Package Structure

| Package | Responsibility |
|---------|-----------------|
| `internal/app/` | Router, view switching, command dispatch (1540 LOC) |
| `internal/ui/{views}/` | Individual view models + Update/View |
| `internal/ui/components/` | Reusable: filter bar, modals, wrapped input |
| `internal/ui/theme/` | Colors, styles, icons, time formatting |
| `internal/command/` | Parser, completion engine |
| `internal/config/` | YAML loading, multi-context support |
| `internal/shared/` | Cross-view state struct |
| `internal/cache/` | Image/workspace scan caches (metadata + results) |
| `internal/scan/` | Trivy/Gitleaks orchestrator |
| `internal/docker/` | Docker CLI wrapper, network diag tools |
| `internal/oci/` | OCI registry HTTP client |
| `internal/gitlab/` | GitLab API client wrapper, auth, stats |
| `internal/git/` | The git binary: clone, sync, and the non-interactive environment both run under |
| `internal/status/` | Health checker (HTTP/ICMP/DNS) |
| `internal/credentials/` | Storage interface: File/Memory/GitCredential |

## Critical Bubble Tea Rules (Rule 110)

- **Never modify state in Cmd** — only in `Update()`
- **Never use `style.Render()` in table.Row{}** — causes ANSI corruption
- **View must implement FormView.InEditMode()** if active forms block command mode
- **Footer messages disappear after 3 seconds** via `clearFooterMsgCmd()`

## Context Switching

Context names stored in `~/.devdesk/current-context`. On switch:
1. Load new config from `~/.devdesk/contexts/<name>/config.yaml`
2. Reinitialize all views with new config
3. Update sharedState (GitLab client, etc.)
4. Emit `tea.WindowSizeMsg` to trigger resize

## Initialization Chain

```
main()
  → config.Load() [from ~/.devdesk/config.yaml or current context]
  → theme.LoadTheme() [if config.App.Theme set]
  → app.New(cfg)
     → new(views map)
     → dashboard.New() + status.New()  [eager]
     → other views lazy-loaded on first `:cmd`
     → sharedState initialized
  → tea.NewProgram().Run()
```
