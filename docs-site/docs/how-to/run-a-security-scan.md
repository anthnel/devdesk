# Run a security scan

DevDesk runs [Trivy](https://trivy.dev/) (CVEs, secrets, licenses, misconfiguration) and [Gitleaks](https://github.com/gitleaks/gitleaks) (secrets) against local repositories and OCI images. Both are optional — a scan silently skips whichever tool isn't installed.

## Scan a local repository

From **Workspaces** (`:ws`), select a repository and press:

| Key | Effect |
|---|---|
| `S` / `ctrl+s` | Scan this repository. If it was already scanned, this rescans it and overwrites the cached result. |
| `A` | Scan every repository that has never been scanned. Already-scanned ones are left alone. |
| `ctrl+a` | Purge the scan cache entirely and rescan everything, including repositories scanned earlier. |

A scan runs as a background job — check `:jobs` to see it in flight, or just keep navigating.

## Scan an OCI image

The same three keys work from **OCI Resources** (`:oci`): `S`/`ctrl+s` to (re)scan the selected image, `A` for every unscanned image, `ctrl+a` to purge and rescan all.

## Read the results

Open **Security** (`:sec`) once a scan finishes. Findings are grouped by severity (CRITICAL, HIGH, MEDIUM, LOW) with cumulative filters — press `c`, `h`, `m`, `l` to toggle a severity on or off; several can be active at once (`c`+`h` means "CRITICAL or HIGH"). Trivy findings include remediation details (fixed version, advisory link) where the scanner provides them; Gitleaks findings show the file and line the secret was found at.

## Why results might be stale

The cache is keyed by repository/image, not by commit — a scan result stays valid until you rescan, even if the underlying code has changed since. If you want to be sure you're looking at current findings, `S`/`ctrl+s` before reading `:sec`.

Deleted repositories and images drop out of the Security view and the dashboard's counts automatically; a cached result for something that no longer exists is never shown as current.

## Configure which checks run

```yaml
scan:
  enable_vuln: true        # Trivy: CVEs
  enable_secret: true      # Trivy: secrets
  enable_misconfig: true   # Trivy: IaC misconfiguration
  enable_license: false    # Trivy: license issues
  ignore_unfixed: false    # drop CVEs with no available fix
  gitleaks_history: false  # scan full git history, not just the working tree
```

These same toggles persist from the **Security** view itself — no need to
hand-edit YAML for a one-off change. There is no per-tool `enabled:` switch;
`enable_vuln`/`enable_secret`/`enable_misconfig`/`enable_license` are Trivy's
four checks, individually. See the [configuration
reference](../reference/configuration.md) for the full `scan:` schema,
including Trivy server mode (`use_trivy_server`, `trivy_server`).
