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
| `templates_repository` | string | | |
| `cache_dir` | string | | Discovered-members cache |
| `registries` | list | | Additional named registries (`slug`, `provider`, `url`, `username`, `alias`) |

## `scan:`

| Key | Type | Default | Meaning |
|---|---|---|---|
| `trivy_source` | string | `auto` | `auto` \| `binary` \| `image` |
| `trivy_path` | string | | Custom binary path (with `trivy_source: binary`) |
| `trivy_image` | string | `aquasec/trivy` | Custom image (with `trivy_source: image`) |
| `use_trivy_server` | bool | `false` | Client/server mode |
| `trivy_server` | string | | Server URL — read only when `use_trivy_server` is true |
| `gitleaks_source` / `_path` / `_image` | | `auto` | Same shape as Trivy's |
| `plumber_source` / `_path` / `_image` | | `auto` | CI-score scanner |
| `cache_dir` | string | | Scan report cache |
| `max_cached_reports` | int | | |
| `timeout` | int (seconds) | | Per-scan timeout |
| `max_concurrent_scans` | int | | |
| `enable_vuln` | bool | `true` | Trivy: CVEs |
| `enable_secret` | bool | `true` | Trivy: secrets |
| `enable_misconfig` | bool | `true` | Trivy: IaC misconfiguration |
| `enable_license` | bool | `false` | Trivy: license issues |
| `ignore_unfixed` | bool | `false` | Drop CVEs with no available fix |
| `ignore_eol` | bool | `false` | Drop findings for end-of-life packages |
| `gitleaks_history` | bool | `false` | Scan full git history, not just the working tree |
| `gitleaks_config` | string | | Custom Gitleaks config path |
| `enable_ci_score` | bool | `false` | Run plumber over CI configuration |

There is no per-tool `enabled:` switch — Trivy's four checks
(`enable_vuln`/`enable_secret`/`enable_misconfig`/`enable_license`) toggle
independently, and Gitleaks runs whenever `enable_secret` is on.

## `network:`

| Key | Type | Default | Meaning |
|---|---|---|---|
| `check_timeout` | int (seconds) | | Per-check timeout in Network Diagnostics |
| `ping_count` | int | | ICMP echo requests per reachability probe |
| `cert_expiry_warn_days` | int | `30` | How close to expiry a TLS check warns |
| `ports_refresh_interval` | int (seconds) | `2` | Ports tab refresh rate |

## `mcp:`

| Key | Type | Default | Meaning |
|---|---|---|---|
| `enabled` | bool | `false` | Whether the TUI serves MCP at all |
| `listen` | string | `127.0.0.1:7777` | Bind address — loopback by design |
| `expose` | list of strings | `[]` (all) | Allow-list of tool names |

See [Enable the MCP server](../how-to/enable-the-mcp-server.md) and
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
