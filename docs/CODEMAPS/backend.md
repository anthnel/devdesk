# DevDesk Backend Codemap

**Last Updated:** 2026-08-01
**Core Packages:** 11 (config, shared, app, command, cache, scan, docker, oci, gitlab, status, credentials)

## Command Parser (`internal/command/`)

**Key Exports:**
- `ParseCommand(input string) Command` — Parse `:view`, `:context`, `:theme`, `:quit`
- `CompletionEngine` — Tab completion for commands
- `ViewType` enum — 9 view types (dashboard, status, gitlab-*, workspaces, security, containers, oci-resources, net)

**ViewType to View Mapping:**
```go
ViewDashboard, ViewStatus, ViewGitlabAuth, ViewGitlabExplorer,
ViewWorkspaces, ViewSecurity, ViewContainers, ViewOCIResources, ViewNet
```

## Configuration (`internal/config/`)

**Structs:**
- `Config` — Top-level (App, Status, GitLab, Registry, Scan, Docker)
- `AppConfig` — Theme, LogFile, DefaultView, WorkspacesDir, IDECommand, TerminalCommand
- `GitLabConfig` — URL, Token, DefaultParentGroup, DefaultVisibility, CloneMethod, Pull
- `ScanConfig` — Trivy/Gitleaks paths, images, options, timeouts
- `RegistryConfig` — OCI registry URL, creds, templates, cache

**Key Functions:**
- `Load() *Config` — Load from `~/.devdesk/config.yaml`
- `Save() error` — Persist config changes
- `GetCurrentContext() string` — Read from `~/.devdesk/current-context`
- `SetCurrentContext(name string) error` — Write + save

**Context Structure:**
```
~/.devdesk/
├── config.yaml           (global/default)
├── current-context       (active context name)
└── contexts/
    ├── default/config.yaml
    ├── work/config.yaml
    └── client-a/config.yaml
```

## Shared State (`internal/shared/`)

**State Struct:**
```go
type State struct {
  // GitLab
  GitLabClient    *gitlabclient.Client
  IsAuthenticated bool
  CurrentUser     *gitlabclient.User

  // Cache
  CachedGroups   []*gitlabclient.Group
  CachedProjects []*gitlabclient.Project

  // Dashboard metrics
  ServiceStatus     ServiceGlobalStatus
  ServiceComponents []status.ComponentStatus
  GitLabStats       *GitLabStats
  DockerStats       *DockerStats
  OCIStats          *OCIStats
  WorkspaceCount    int
  Tools             []ToolInfo  // Trivy, Gitleaks, Docker, etc.
}
```

## Cache System (`internal/cache/`)

**Two independent caches** (metadata + full results on disk):

### ImageScanCache
```go
type ImageScanEntry struct {
  ImageID   string
  Critical, High, Medium, Low int
  ScannedAt time.Time
}
// Metadata: ~/.devdesk/cache/image-scans.json
// Results:  ~/.devdesk/cache/image-results/<sha256>.json
```

**Key Functions:**
- `NewImageScanCache()` — Load/create cache
- `Get(imageID string) (*scan.Result, error)` — Load result from disk
- `Set(imageID string, result *scan.Result) error` — Cache result
- `Delete(imageID string)` — Remove entry

### WorkspaceScanCache
```go
type WorkspaceScanEntry struct {
  RepoPath  string
  Critical, High, Medium, Low int
  ScannedAt time.Time
}
// Metadata: ~/.devdesk/cache/workspace-scans.json
// Results:  ~/.devdesk/cache/workspace-results/<sha256>.json
```

## Security Scanner (`internal/scan/`)

**Orchestrator:**
- `Scanner.Run(ctx, target, opts) → (Result, progress chan)` — Execute Trivy + Gitleaks
- Runs both tools concurrently via `errgroup`
- Emits `ProgressUpdate` structs per stage (vuln, secret, misconfig, license, sbom)

**ScanOptions:**
```go
type ScanOptions struct {
  EnableVuln, EnableSecret, EnableMisconfig, EnableLicense bool
  GenerateSBOM bool
  SBOMOutputDir string
  TrivyImage, GitleaksImage string  // custom Docker images
  IgnoreUnfixed, IgnoreEOL bool
  GitleaksHistory, GitleaksConfig string
}
```

**Result Struct:**
```go
type Result struct {
  Target        string
  Vulnerabilities []Vulnerability  // Trivy CVE results
  Secrets         []Secret          // Gitleaks findings
  Misconfigs      []Misconfig       // Config issues
  SBOM           []SBOMComponent   // CycloneDX
}
```

**Key Functions:**
- `trivy.Scan()` — Run Trivy (CVE, SBOM, misconfig)
- `gitleaks.Scan()` — Run Gitleaks (secrets)

## Docker Integration (`internal/docker/`)

**Client Functions:**
- `ListContainers(all bool) []Container` — List containers with metrics
- `GetContainerStats(containerID) *Container` — Live CPU/memory/network
- `StopContainer(containerID) error`
- `RestartContainer(containerID) error`
- `PauseContainer(containerID) error`
- `RemoveContainer(containerID) error`

**Network Diagnostics (ephemeral containers):**
- `RunPing(host string) DiagResult` — ICMP
- `RunDNS(host string) DiagResult` — DNS resolution
- `RunTraceroute(host string) DiagResult` — ICMP traceroute
- `RunTCPTraceroute(host string, port int) DiagResult`
- `RunNetcat(host string, port int) DiagResult`
- `RunCurl(url string) DiagResult`
- `RunSSLCert(host string, port int) DiagResult`

**Port Monitor:**
- `RunSS(image, numeric bool) []PortInfo` — Real-time port table via host network
- `KillProcess(image string, pid int) error` — Terminate process by PID

## OCI Registry (`internal/oci/`)

**Client:**
- `NewClient(registryURL, username, password) *Client`
- `ListTags(repo string) []string` — Fetch available tags
- `GetTemplates(repo string) []Template` — List OCI templates
- `PullAndExtract(repo, tag, outDir) error` — Download + extract tar.gz

## GitLab Integration (`internal/gitlab/`)

**Auth:**
- `NewAuth(storage credentials.Storage) *Auth`
- `Login(url, token) error` — Validate + store credentials
- `Logout(url) error`
- `IsAuthenticated(url) bool`

**Client Wrapper:**
- `NewClient(url, token) *gitlabclient.Client` — API client
- `GetUser() *gitlabclient.User`
- `ListGroups() []*gitlabclient.Group`
- `ListProjects(groupID) []*gitlabclient.Project`

**Git Operations:**
- `CloneRepository(projectID, path, method) error` — Clone via HTTPS/SSH
- `PullRepository(projectID, path) error` — Fetch + merge
- `SyncToTarget(groups []int, targetDir, method string) PullStats` — Parallel clone/pull

**Stats:**
- `GetGitLabStats(client) GitLabStats` — Assigned MRs/issues, project/group counts

## Credentials Storage (`internal/credentials/`)

**Storage Interface:**
```go
type Storage interface {
  Get(key string) (string, error)
  Set(key string, value string) error
  Delete(key string) error
}
```

**Implementations:**
- `KeyringStorage` — host secret manager (Credential Manager / Keychain / Secret Service), context-aware, the default
- `GitCredentialStorage` — git credential helper (context-aware); refused when the helper is `store`
- `MemoryStorage` — session-only, the last resort, surfaced to the user as a warning

`Select(context, preference)` picks exactly one of them. No secret is written to
a file DevDesk owns.

## Status Monitoring (`internal/status/`)

**Checker Interface:**
```go
type CheckerInterface interface {
  Check(component ComponentStatus) ComponentStatus
}
```

**Concrete Checkers:**
- `HTTPChecker` — HTTP/HTTPS GET + TLS verification
- `ICMPChecker` — Ping via pro-bing
- `DNSChecker` — A/AAAA resolution

**Orchestrator:**
```go
type Checker struct {
  // Factory pattern — selects checker by component.Type
}
func (c *Checker) CheckAll(components []ComponentStatus) []ComponentStatus  // concurrent
func (c *Checker) CheckOne(component ComponentStatus) ComponentStatus
```

**ComponentStatus:**
```go
type ComponentStatus struct {
  Name       string
  Type       string  // "http", "icmp", "dns"
  Status     string  // "up", "down", "unknown"
  LastCheck  time.Time
  ResponseTime int64  // milliseconds
  Error      string
}
```

## App Router (`internal/app/app.go` – 1540 LOC)

**Core Methods:**
- `Init() tea.Cmd` — Initialize Bubble Tea
- `Update(msg tea.Msg) (tea.Model, tea.Cmd)` — Main message dispatcher
- `View() string` — Render layout (header + viewport + footer)

**View Management:**
- `initView(viewType)` — Lazy-load view instance
- `switchView(viewType)` — Dispatch `SwitchViewMsg`
- `GetViewShortcuts()` — Dynamic shortcut list from current view

**Command Mode:**
- `handleCommand(input string)` — Parse + execute `:cmd`
- `completionEngine.Suggest(input)` — Tab completion

**Selection Mode:**
- `startSelection(returnView)` — Enter cross-view picker
- `HandleSelectionResult(entry)` — Return to origin view with selection

**Overlay Handling:**
- Context list, theme list, help viewport — position via `lipgloss.Place()`

## Helper Functions (Rule 201)

**App.Update() handlers (> 5 lines extracted):**
- `handleContextSwitch(msg)` — Load config, reinit views
- `handleThemeSwitch(msg)` — Apply theme, persist
- `handleGitLabAuth(msg)` — Update sharedState, broadcast
- `handleViewSwitch(msg)` — Initialize + resize view

**Exports preserved in module scope** for testability.
