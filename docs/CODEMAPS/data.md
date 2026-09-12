# DevDesk Data Structures Codemap

**Last Updated:** 2026-08-01
**Schema Version:** 1.0
**Storage:** YAML config + JSON metadata + JSON results

## Configuration Schema

### ~/.devdesk/config.yaml (Global + Context-specific)

```yaml
app:
  theme: "dark"                    # Theme name
  log_file: "~/.devdesk/debug.log"
  default_view: "dashboard"        # :d shortcut
  workspaces_dir: "~/code"         # For workspace discovery
  ide_command: "code"              # VS Code, IntelliJ, etc.
  terminal_command: ""             # Auto-detect kitty/tmux/etc
  secret_backend: "auto"           # auto|keyring|git-credential

status:
  refresh_interval: 30             # seconds
  components:
    - name: "API Server"
      type: "http"
      url: "https://api.example.com"
    - name: "DNS"
      type: "dns"
      target: "8.8.8.8"

gitlab:
  url: "https://gitlab.example.com"
  # No token here. It lives in the host secret manager — see the credentials
  # section below. A `token:` left by an older DevDesk is moved into the store
  # and removed from this file on load.
  default_parent_group: "my-org"
  default_visibility: "private"    # private|internal|public
  clone_method: "https"            # https|ssh
  pull:
    parallel_jobs: 4
    include_archived: false

registry:
  url: "registry.example.com"      # Default OCI registry
  username: "user"
  templates_repository: "templates" # Default template repo
  cache_dir: "~/.devdesk/cache"
  registries:                       # Additional registries
    - url: "gcr.io"
      username: "user@example.com"
      alias: "gcr"
      auth_enabled: true

scan:
  trivy_image: "aquasec/trivy:0.52.1"
  use_trivy_server: false          # The checkbox that actually turns on server mode
  trivy_server: ""                 # Address used only when use_trivy_server is true
  gitleaks_image: "zricethezav/gitleaks:v13.3.0"
  cache_dir: "~/.devdesk/cache"
  max_cached_reports: 50
  timeout: 300                     # seconds
  max_concurrent_scans: 3
  enable_vuln: true
  enable_secret: true
  enable_misconfig: true
  enable_license: false
  ignore_unfixed: false
  ignore_eol: false
  gitleaks_history: false
  gitleaks_config: ""              # Custom Gitleaks config path

docker:
  network_tool_image: "alpine:latest"  # For network diagnostics
```

### ~/.devdesk/contexts/ (Multi-context support)

```
~/.devdesk/
├── current-context                    (text file: "work")
├── contexts/
│   ├── default/
│   │   └── config.yaml               (same schema as above)
│   ├── work/
│   │   └── config.yaml               (work-specific config)
│   └── client-a/
│       └── config.yaml               (client-specific config)
```

## Shared State (`internal/shared/State`)

```go
type State struct {
  // GitLab session
  GitLabClient    *gitlabclient.Client // API client (thread-safe)
  IsAuthenticated bool                  // Login status
  CurrentUser     *gitlabclient.User    // User profile + username

  // Caching
  CachedGroups   []*gitlabclient.Group  // Groups loaded from API
  CachedProjects []*gitlabclient.Project // Projects loaded from API

  // Dashboard metrics
  ServiceStatus     ServiceGlobalStatus  // all_ok | degraded | down | unknown
  ServiceComponents []status.ComponentStatus  // Health check results
  GitLabStats       *GitLabStats        // Assigned MRs/issues, counts
  DockerStats       *DockerStats        // Running/stopped/paused counts
  OCIStats          *OCIStats           // Image/container/volume counts + size
  WorkspaceCount    int                 // Local git repos discovered
  Tools             []ToolInfo          // Trivy, Gitleaks, Docker versions
}

type GitLabStats struct {
  AssignedMRs    int   // Merge requests assigned to user
  ReviewMRs      int   // MRs awaiting user review
  AssignedIssues int   // Issues assigned
  TotalProjects  int   // Projects in account
  TotalGroups    int   // Groups in account
}

type DockerStats struct {
  Available bool
  Running   int   // Container count
  Stopped   int
  Paused    int
}

type OCIStats struct {
  Available       bool
  ImagesCount     int    // Docker images
  ImagesSize      string // e.g., "2.3 GiB"
  ContainersCount int
  ContainersSize  string
  VolumesCount    int
  VolumesSize     string
}

type ToolInfo struct {
  Name      string  // "Trivy", "Gitleaks", "Plumber", "Git", and the engine in use ("Docker" | "Podman")
  Available bool    // Found on PATH, or held as an image by the engine
  Version   string  // e.g., "0.52.1"
  Source    string  // "binary" | "container" | ""  — see scan.ToolSource
}

type ServiceGlobalStatus string
const (
  ServiceStatusAllOK   ServiceGlobalStatus = "all_ok"
  ServiceStatusDegraded ServiceGlobalStatus = "degraded"  // some down
  ServiceStatusDown    ServiceGlobalStatus = "down"       // all down
  ServiceStatusUnknown ServiceGlobalStatus = "unknown"
)
```

## Scan Results (`internal/scan/Result`)

```go
type Result struct {
  Target         string
  TargetType     TargetType     // image | directory
  StartTime, EndTime time.Time
  Duration       time.Duration
  Counts         SeverityCounts // vulnerabilities, by severity
  SecretCount    int            // gitleaks + trivy secrets
  LicenseCount   int
  MisconfigCount int
  Findings       []Finding      // one flat list; scan.Categorize sorts them
  Errors         []string
}
```

All findings share one `Finding` type — there is no per-family struct. Which
family a finding belongs to comes from `scan.Categorize(f)` reading `f.Source`,
and nothing else (see `internal/scan/category.go`).

```go
type Finding struct {
  ID          string        // CVE-XXXX-XXXXX, a rule id, a license name
  Title       string
  Description string
  Severity    SeverityLevel
  Source      string        // trivy | trivy-secret | trivy-license | trivy-misconfig | gitleaks
  File        string
  Line        int
  Match       string        // secrets: the masked value
  Fingerprint string        // gitleaks only — what .gitleaksignore matches on
  PkgName     string        // vulnerabilities
  Version     string
  FixedIn     string
  Resolution  string
  References  []string
  FixCommand  string
}
```

`Source` is the whole of the classification, which is why Trivy's secrets carry
`trivy-secret` rather than being recognised by the presence of `Match`.
`Fingerprint` is Gitleaks-only, and it is why `i` (add to `.gitleaksignore`) is
offered for Gitleaks findings alone.

```go
type SeverityLevel string
const (
  SeverityCritical SeverityLevel = "CRITICAL"
  SeverityHigh     SeverityLevel = "HIGH"
  SeverityMedium   SeverityLevel = "MEDIUM"
  SeverityLow      SeverityLevel = "LOW"
  SeverityUnknown  SeverityLevel = "UNKNOWN"
)
```

## Image Scan Cache

### Metadata: ~/.devdesk/cache/image-scans.json

```json
{
  "alpine:latest": {
    "image_id": "sha256:...",
    "critical": 2,
    "high": 5,
    "medium": 12,
    "low": 30,
    "scanned_at": "2026-03-28T10:15:00Z"
  },
  "nginx:1.25": {
    "image_id": "sha256:...",
    "critical": 0,
    "high": 1,
    "medium": 3,
    "low": 10,
    "scanned_at": "2026-03-28T09:00:00Z"
  }
}
```

### Results: ~/.devdesk/cache/image-results/{sha256}.json

Full `Result` struct with vulnerabilities, secrets, etc.

## Workspace Scan Cache

### Metadata: ~/.devdesk/cache/workspace-scans.json

```json
{
  "/home/user/code/myrepo": {
    "repo_path": "/home/user/code/myrepo",
    "critical": 0,
    "high": 2,
    "medium": 4,
    "low": 15,
    "scanned_at": "2026-03-27T14:30:00Z"
  }
}
```

### Results: ~/.devdesk/cache/workspace-results/{sha256}.json

Full `Result` struct.

## Status Component Schema

```go
type ComponentStatus struct {
  Name         string        // Display name
  Type         string        // "http" | "icmp" | "dns"
  URL          string        // For HTTP
  Target       string        // For ICMP/DNS
  Status       string        // "up" | "down" | "unknown"
  LastCheck    time.Time
  ResponseTime int64         // milliseconds
  Error        string        // Error message if down
}
```

### Persisted in Config.Status.Components

## Docker Container Structure

```go
type Container struct {
  ID          string   // Full SHA
  Name        string   // Short name
  Image       string   // Image:tag
  State       string   // running | exited | paused | created | restarting | dead
  Status      string   // "Up 2 hours" | "Exited (0) 5 minutes ago"
  CreatedAt   string   // Human-readable
  Ports       string   // "8080:8080/tcp"
  CPUPercent  float64  // 0.5 | 1.2 (%)
  MemUsage    string   // "150MiB / 8GiB"
  MemPercent  float64
  NetIO       string   // "1.2kB / 3.4kB"
  NetRX, NetTX int64  // Bytes
  BlockIO     string   // "1.2MB / 3.4MB"
  BlockRX, BlockTX int64  // Bytes
}
```

## Port Monitor Structure

```go
type PortInfo struct {
  LocalAddr   string  // 0.0.0.0 | 127.0.0.1
  LocalPort   int
  RemoteAddr  string
  RemotePort  int
  State       string  // LISTEN | ESTAB | TIME_WAIT
  Protocol    string  // TCP | UDP
  PID         int
  ProcessName string
}
```

## GitLab API Types (Imported)

From `gitlab.com/gitlab-org/api/client-go`:

```go
type Group struct {
  ID          int
  Name        string
  Path        string
  Description string
  Visibility  string
  ParentID    *int
}

type Project struct {
  ID          int
  Name        string
  Path        string
  Description string
  Visibility  string
  GroupID     *int
  PathWithNamespace string  // e.g., "my-org/my-project"
  HTTPURLToRepo string
  SSHURLToRepo  string
}

type User struct {
  ID       int
  Username string
  Name     string
  Email    string
}

type MergeRequest struct {
  ID        int
  IID       int
  Title     string
  State     string  // opened | merged | closed
  Assignee  *User
}

type Issue struct {
  ID        int
  IID       int
  Title     string
  State     string  // opened | closed
  Assignee  *User
}
```

## Message Flow

**Inter-view** (`internal/app/messages.go`):
- `SwitchViewMsg` → Router
- `GitLabAuthSuccessMsg` → Update sharedState
- `PullCompletedMsg` → Dashboard refresh
- `WorkspaceScanResultLoadedMsg`, `ImageScanResultLoadedMsg` → Cache loaded from disk

**Intra-view**: Each view defines custom messages in its model.

## Credentials Storage

No secret is written to a file DevDesk owns. `credentials.Select(context,
app.secret_backend)` resolves **one** destination per context and returns a
`Selection{Storage, Backend, Detail}`; the router keeps it in
`shared.State.Secrets` for the lifetime of that context.

**KeyringStorage** (default):
- The host's own secret manager, via `zalando/go-keyring` — no cgo
- Windows Credential Manager / macOS Keychain / Secret Service (Linux, BSD)
- Service `devdesk`, account `<context>/<url>`, so contexts stay isolated
- Visible and revocable in the host's own UI

**GitCredentialStorage** (alternative, when no host store answers):
- Uses git's configured credential helper, scoped per context via the path field
- Refused when the helper is `store`: that one writes `~/.git-credentials` in
  plaintext, which is the failure being removed

**MemoryStorage** (last resort):
- Lost on app exit, and the auth view says so in a warning
- Deliberately worse than a file: a fallback that silently persists is how the
  old "secure" option came to write plaintext

Migration: `credentials.MigrateLegacySecrets` runs on load and on every context
switch. A `gitlab.token` or `registry.password` left in a context file by an
older build is moved into the store and deleted from the YAML.
