# DevDesk Dependencies Codemap

**Last Updated:** 2026-08-01
**Language:** Go 1.25.5
**Total Files:** 105 Go files (~33k LOC)

## Go Module Structure

**Module:** `gitlab.com/anthnell/devsecops/devdesk`
**Latest Commit:** Uses Go 1.25.5

## Direct Dependencies (go.mod)

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/charmbracelet/bubbles` | 0.21.0 | Pre-built TUI components (table, spinner, textinput) |
| `github.com/charmbracelet/bubbletea` | 1.3.10 | TUI framework (Elm architecture) |
| `github.com/charmbracelet/lipgloss` | 1.1.0 | Styling + layout (colors, borders, positioning) |
| `github.com/charmbracelet/x/ansi` | 0.10.1 | ANSI sequence parsing |
| `github.com/charmbracelet/x/term` | 0.2.1 | Terminal size detection, primitives |
| `github.com/prometheus-community/pro-bing` | 0.5.0 | ICMP ping (maintained fork of go-ping/ping) |
| `gitlab.com/gitlab-org/api/client-go` | 1.10.0 | GitLab REST API client |
| `golang.org/x/sync` | 0.19.0 | errgroup for concurrent operations |
| `gopkg.in/yaml.v3` | 3.0.1 | YAML config parsing |

## Indirect Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/atotto/clipboard` | OS clipboard access (bubbles) |
| `github.com/aymanbagabas/go-osc52/v2` | OSC-52 clipboard (terminal sequences) |
| `github.com/charmbracelet/colorprofile` | Terminal color capability detection |
| `github.com/charmbracelet/x/cellbuf` | Character cell buffer |
| `github.com/erikgeiser/coninput` | Console input handling (Windows) |
| `github.com/google/go-querystring` | URL query encoding (GitLab client) |
| `github.com/google/uuid` | UUID generation |
| `github.com/hashicorp/go-cleanhttp` | HTTP client cleanup |
| `github.com/hashicorp/go-retryablehttp` | Retry logic (GitLab) |
| `github.com/lucasb-eyer/go-colorful` | Color manipulation |
| `github.com/mattn/go-isatty` | TTY detection |
| `github.com/mattn/go-runewidth` | Unicode width calculation |
| `github.com/muesli/ansi` | ANSI parsing utilities |
| `github.com/muesli/cancelreader` | Context-aware input reading |
| `github.com/muesli/termenv` | Terminal capabilities |
| `golang.org/x/net` | HTTP/2, IPv6 support |
| `golang.org/x/oauth2` | OAuth2 flow (GitLab) |
| `golang.org/x/sys` | OS-specific syscalls |
| `golang.org/x/text` | Unicode normalization |
| `golang.org/x/time` | Rate limiting |

## External Tools (Docker + System)

| Tool | Image/Binary | Used For |
|------|--------------|----------|
| Docker | `docker` binary or Docker daemon | Container management, network diagnostics, tool isolation |
| Trivy | `aquasec/trivy:latest` | CVE scanning, SBOM, misconfig detection |
| Gitleaks | `zricethezav/gitleaks:latest` | Secrets scanning (git history, staged) |
| git | System `git` binary | Cloning, pulling, fetching git metadata |
| Network tools | `alpine` image (ping, curl, nc, traceroute, ss) | Network diagnostics |

### Configurable Tool Images

```yaml
# From config.yaml
docker:
  network_tool_image: "alpine:latest"  # Must have: ping, curl, nc, traceroute, ss, dig

scan:
  trivy_image: "aquasec/trivy:0.52.1"
  gitleaks_image: "zricethezav/gitleaks:v13.3.0"
```

## System Dependencies

| Dependency | Optional | Used By | Note |
|------------|----------|---------|------|
| `docker` / `podman` | Yes | Containers, OCI, security scanning, network diag | Can use path or Docker daemon |
| `git` | No | GitLab clone/pull, workspace discovery | System binary |
| `ssh` (OpenSSH) | Conditional | GitLab SSH clone | Only if `clone_method: ssh` |
| Credential helper | Recommended | Git credentials storage | `git-credential-manager`, `osxkeychain`, `pass` |

## API Integrations

### GitLab REST API (v4)

**Endpoint:** `https://gitlab.example.com/api/v4`

**Authentication:** Personal access token (scope: `api`, `read_repository`)

**Key Resources:**
- `/users` → User info
- `/groups` → List/create groups
- `/projects` → List/create projects
- `/merge_requests` → List assigned MRs
- `/issues` → List assigned issues
- `/user/events` → Activity feed

**Client Library:** `gitlab.com/gitlab-org/api/client-go` (idiomatic wrapper)

### Docker API

**Access:** Via Docker CLI (`docker` command)

**Commands Used:**
- `docker ps` — List containers
- `docker stats` — Live metrics
- `docker exec` — Run commands in containers
- `docker network` — Network info
- `docker system df` — Disk usage

### OCI Registry HTTP API

**Endpoints:**
- `GET /v2/` → Check auth
- `GET /v2/<name>/tags/list` → Fetch tags
- `GET /v2/<name>/manifests/<reference>` → Image manifest
- `GET /v2/<name>/blobs/<digest>` → Download layer

**Authentication:** Basic auth or bearer token (docker login)

## Build & Test Dependencies

| Tool | Purpose |
|------|---------|
| `go` (1.25.5) | Compiler, test runner |
| `golangci-lint` | Linter (Rule 301) |
| `go vet` | Static analysis |
| Standard library | testing, log, os, path/filepath, time, sync, context, encoding/json |

## Data Flow Dependencies

```
config.yaml (YAML 3.0)
    ↓
internal/config/config.go (gopkg.in/yaml.v3)
    ↓
internal/app/app.go
    ├── internal/ui/theme/ (lipgloss 1.1.0)
    ├── internal/ui/components/ (bubbles, lipgloss)
    └── internal/shared/state.go (gitlabclient.Client)

internal/gitlab/client.go
    ├── gitlab.com/gitlab-org/api/client-go (1.10.0)
    └── internal/credentials/ (git credential helper)

internal/scan/scanner.go
    ├── Trivy (Docker image)
    └── Gitleaks (Docker image)

internal/docker/client.go
    └── docker binary (exec)

internal/status/checker.go
    ├── github.com/prometheus-community/pro-bing (ICMP)
    └── net (stdlib, DNS)

internal/cache/
    ├── Image scan cache (.devdesk/cache/image-scans.json)
    └── Workspace scan cache (.devdesk/cache/workspace-scans.json)
```

## Version Constraints

**Bubble Tea Ecosystem:**
- `bubbletea 1.3.10` — Core TUI framework
- `bubbles 0.21.0` — Components (table, spinner, input)
- `lipgloss 1.1.0` — Styling (must match bubbletea for color profiles)

**Network:**
- `pro-bing 0.5.0` — Maintained ICMP fork (replaces deprecated go-ping/ping)

**Concurrency:**
- `golang.org/x/sync 0.19.0` — errgroup (no context.CancelFunc leak)

**Configuration:**
- `gopkg.in/yaml.v3 3.0.1` — Stable YAML 1.2 spec

## Security Considerations

### No Direct Secrets in Dependencies

- Credentials stored via `git-credential` (external secure storage)
- No hardcoded API keys or passwords
- Config uses `${VAR}` placeholder syntax (env var injection)

### Dependency Audit

```bash
go list -m all | sort
go mod verify
# Optional: gosec ./... (security scanning)
```

### Known Issues / CVEs

- None documented as of 2026-03-28
- Regular `go get -u` recommended for patch updates
- No EOL dependencies in active use

## Network Isolation

**Docker containers used for isolation:**
- Trivy/Gitleaks run with `--rm` (ephemeral)
- Network diagnostics in Alpine containers (host network)
- No container persistence

**Terminal commands:**
- SSH key passphrase prompts handled via `git` native flow
- No credential passing via command line args

## Performance Dependencies

| Component | Impact | Optimization |
|-----------|--------|--------------|
| Bubbles table rendering | O(n) cells | Virtual scrolling via viewport |
| Trivy scanning | O(layers) | Parallel via `errgroup` |
| Gitleaks secrets scan | O(commits) | Optional `--log-level error` to reduce output |
| Docker stats | Real-time | Polling interval configurable |
| Git clone/pull | Network I/O | Parallel jobs configurable (gitlab.pull.parallel_jobs) |

## Compiler Directives

**Go build tags:** None currently used

**CGO:** Not required (pure Go + CLI tools)

**Platform support:** Linux, macOS, Windows (WSL2)
