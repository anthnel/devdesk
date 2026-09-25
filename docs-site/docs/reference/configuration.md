# Configuration reference

Full `config.yaml` schema, grouped by top-level key. Every field has a
default — an absent key is not an error, `config.Default()` fills it in.

## `app:`

| Key | Type | Default | Meaning |
|---|---|---|---|
| `theme` | string | `default` | `""`, `default`, or `dark` all mean the built-in theme; any other name loads `~/.devdesk/themes/<name>.json` — copy the bundled files from `themes/` in the repository there first, DevDesk does not install them automatically |
| `log_file` | string | | Path to the log file |
| `default_view` | string | `dashboard` | View opened at startup |
| `workspaces_dir` | string | `~/workspaces` | Scanned by Workspaces; default clone target for the forge explorer |
| `ide_command` | string | | Command run by `O` (open in IDE) |
| `terminal_command` | string | | e.g. `kitty --directory` — empty auto-detects |
| `terminal_new_window` | bool | `false` | `T` opens a separate terminal window instead of suspending the TUI in place. Off by default: no window can open under WSL, and none exists to open over SSH |
| `show_hidden_files` | bool | `false` | Whether Workspaces lists dot-prefixed entries — governs both the listing and what `S`/`F`/`A` act on |
| `secret_backend` | string | `auto` | `auto` \| `keyring` \| `git-credential` — pins the credential storage backend |
| `container_engine` | string | `auto` | `auto` \| `docker` \| `podman`, or an explicit path to a binary. `auto` picks Docker if it's on PATH, Podman otherwise. A pinned engine that isn't installed is an error, not a silent fall back to the other one — see [Container engine](../explanation/network.md#container-engine-docker-or-podman) |

## `forge:`

| Key | Type | Default | Meaning |
|---|---|---|---|
| `type` | string | `gitlab` (empty means this) | `gitlab` \| `github` — exactly one per context |
| `url` | string | | Forge instance URL |
| `default_parent_group` | string | | GitLab only |
| `default_visibility` | string | | Default visibility for created resources |
| `clone_method` | string | | `ssh` or `https` |
| `pull.parallel_jobs` | int | | Concurrent clones during a bulk pull |
| `pull.include_archived` | bool | `false` | Include archived projects/repositories |

## `registry:`

| Key | Type | Default | Meaning |
|---|---|---|---|
| `url` | string | | Default OCI registry |
| `username` | string | | |
| `templates_repository` | string | | Unused. Repository templates are declared in `~/.devdesk/templates.yaml` and managed with `:templates`; the key is kept so older files still load |
| `cache_dir` | string | | Discovered-members cache |
| `registries` | list | | Additional named registries (`slug`, `provider`, `url`, `username`, `alias`) |

## `scan:`

What a scan looks for and what runs it are two questions: `categories:` turns
each kind of check on or off and ticks the tools that serve it, and `tools:`
holds each tool's own settings.

```yaml
scan:
  categories:
    vuln:      { enabled: true,  tools: [trivy] }
    secret:    { enabled: true,  tools: [trivy, gitleaks] }
    misconfig: { enabled: false, tools: [trivy] }
    license:   { enabled: false, tools: [trivy] }
    ci:        { enabled: false, tools: [plumber] }
  tools:
    trivy:
      source: auto            # auto | binary | image
      binary: ""
      image: ""
      config: ""              # trivy.yaml
      args: []                # e.g. [--skip-dirs, vendor]
      server: { enabled: false, url: "" }
      ignore_unfixed: false
      ignore_eol: false
    gitleaks:    { source: auto, binary: "", image: "", config: "", history: false }
    plumber:     { source: auto, binary: "", image: "", config: "" }
    kubeconform: { source: auto, binary: "", image: "", kubernetes_version: 1.36.0 }
    helm:        { source: auto, binary: "", image: "" }
    kustomize:   { source: auto, binary: "", image: "" }
```

### `scan.categories.<category>`

| Key | Type | Meaning |
|---|---|---|
| `enabled` | bool | Whether the scan looks for this at all |
| `tools` | list | The tools that run it. A category that is off keeps its list |

| Category | Tools it accepts | Default |
|---|---|---|
| `vuln` | `trivy` | on, `[trivy]` |
| `secret` | `trivy` (files, image layers), `gitleaks` (git history, repositories only) | on, `[trivy, gitleaks]` |
| `misconfig` | `trivy` (security rules), `kubeconform` (API schema, repositories only), `helm` and `kustomize` (render charts and overlays for kubeconform) | off, `[trivy]` |
| `license` | `trivy` (repositories only) | off, `[trivy]` |
| `ci` | `plumber` (this context's forge only) | off, `[plumber]` |

A tool is **required** when it is ticked in a category that is on — and, for
`helm` and `kustomize`, when `kubeconform` is ticked beside them. A required tool
that cannot run is reported by every scan that needed it; a tool nobody ticked
is never required, installed or not.

### `scan.tools.<tool>`

| Key | Type | Default | Meaning |
|---|---|---|---|
| `source` | string | `auto` | `auto` \| `binary` \| `image`. `binary` fails rather than falling back to an image |
| `binary` | string | | Custom executable; empty resolves the name on `PATH` |
| `image` | string | the tool's | Custom image |
| `config` | string | | `trivy`, `gitleaks` and `plumber`: a rules file (`trivy.yaml`, `.gitleaks.toml`, `.plumber.yaml`), made absolute at load and mounted when the tool runs from an image |
| `args` | list | | Extra arguments, placed after the subcommand and before DevDesk's own flags and the target. The flags DevDesk sets itself — output format and path, scanners, server, config — are refused. For `helm` they apply to `lint` and `template` alike, so only the flags the two share make sense (`--values`, `--set`) |

| Tool-specific key | Type | Default | Meaning |
|---|---|---|---|
| `trivy.server.enabled` | bool | `false` | Client/server mode |
| `trivy.server.url` | string | | Server URL — read only when `enabled` is true |
| `trivy.ignore_unfixed` | bool | `false` | Drop CVEs with no available fix |
| `trivy.ignore_eol` | bool | `false` | Drop findings for end-of-life packages |
| `gitleaks.history` | bool | `false` | Scan full git history, not just the working tree |
| `kubeconform.kubernetes_version` | string | `1.36.0` | Release to validate against: `x.y.z` or `master` |

Default images: `aquasec/trivy`, `zricethezav/gitleaks`, `getplumber/plumber`,
`ghcr.io/yannh/kubeconform`, `alpine/helm`,
`registry.k8s.io/kustomize/kustomize:v5.8.1`.

### Other keys

| Key | Type | Default | Meaning |
|---|---|---|---|
| `base_image_track` | string | `same-line` | How far remediation may move a base image: `same-line` \| `next-major` |
| `image_verification` | string | `on` | `on` \| `off` — check image signatures before every pull, in the `Sig` columns and in scans. Only `off` disables it, and it overrides `~/.devdesk/trust.yaml`. See [Image signatures](../explanation/signatures.md) |
| `cache_dir` | string | | Scan report cache |
| `max_cached_reports` | int | `50` | |
| `timeout` | int (seconds) | `300` | Per-scan timeout |
| `max_concurrent_scans` | int | `3` | |

### Older files

A file written before `categories:` and `tools:` — flat keys such as
`trivy_source`, `gitleaks_config` or `enable_vuln` — still loads: each key is
carried to its new place and leaves the file on the next save. The one change of
meaning: `enable_k8s_schema` ticks `kubeconform`, `helm` and `kustomize` under
`misconfig`, so the two renderers become required where they used to be used
only when installed. Untick them to go back.

## `network:`

| Key | Type | Default | Meaning |
|---|---|---|---|
| `check_timeout` | int (seconds) | | Per-check timeout in Network Diagnostics |
| `ping_count` | int | | ICMP echo requests per reachability probe |
| `cert_expiry_warn_days` | int | `30` | How close to expiry a TLS check warns |
| `ports_refresh_interval` | int (seconds) | `2` | Ports tab refresh rate |
| `proxy_port` | int | `8080` | The one port every named Forward route is served on (`http://api.localhost:8080`). Loopback only, 1024–65535 — a value outside that range is logged and replaced by the default |

Forwards themselves are not in `config.yaml`: they live in `~/.devdesk/forwards.yaml`, shared by every context (see [Network](../explanation/network.md#the-forward-tab-internalforward)).

## Files beside `config.yaml`

| File | Scope | Holds |
|---|---|---|
| `~/.devdesk/trust.yaml` | global | whom to expect image signatures from — [Trust image signatures](../how-to/trust-image-signatures.md) |
| `~/.devdesk/forwards.yaml` | global | port forwards and named routes |
| `~/.devdesk/templates.yaml` | global | the repository template catalog |

## `mcp:`

| Key | Type | Default | Meaning |
|---|---|---|---|
| `enabled` | bool | `false` | Whether the TUI serves MCP at all |
| `listen` | string | `127.0.0.1:7777` | Bind address — loopback by design |
| `expose` | list of strings | `[]` (all) | Allow-list of tool names |

See [Enable the MCP server](../how-to/enable-the-mcp-server.md),
[Connect an AI client](../how-to/connect-an-ai-client.md) and
[MCP Server](../explanation/mcp.md).

## `status:`

| Key | Type | Meaning |
|---|---|---|
| `refresh_interval` | int (seconds) | Auto-refresh interval |
| `timeout` | int (seconds) | Per-check timeout |
| `auto_refresh` | bool | Whether checks re-run on their own |
| `components` | list | `{name, type, target, timeout}` — `type` is `http`, `https`, `icmp`, or `dns` |

## Legacy keys, migrated automatically

`gitlab:` (the shape `forge:` replaced) and a bare `docker:` section are read
once at load, migrated into `forge:`/`network:`, and not written back in
their old shape. A config file from before either rename keeps working
without manual edits.
