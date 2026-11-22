# Run a security scan

DevDesk runs [Trivy](https://trivy.dev/) (CVEs, secrets, licenses, misconfiguration) and [Gitleaks](https://github.com/gitleaks/gitleaks) (secrets) against local repositories and OCI images, plus kubeconform for Kubernetes manifests and plumber for CI grading when their categories are on. A tool is only required when it is ticked in a category that is on; one that cannot run is reported by the scan that needed it.

## Scan a local repository

From **Workspaces** (`:ws`), select a repository and press:

| Key | Effect |
|---|---|
| `S` | Scan this repository. If it was already scanned, this rescans it and overwrites the cached result. |
| `A` | Scan every repository below the cursor. Opens a confirmation with a **"Purge cached results first"** checkbox — unchecked, only what has never been scanned runs; checked, everything is purged and rescanned, including repositories scanned earlier. |

The checkbox exists because a bare `A` and a modifier-only variant used to
differ silently in whether they purged the cache — the closest this
application came to losing scan history by accident. The destructive half
is now a deliberate, visible choice in the confirmation itself, not a
different key.

A scan runs as a background job — check `:jobs` to see it in flight, or just keep navigating.

## Scan an OCI image

The same two keys work from **OCI Resources** (`:oci`): `S` to (re)scan the selected image, `A` for the same all/unscanned choice via its confirmation.

## Scan a template

From **Templates** (`:tpl`), `S` scans what a template would put in a new repository — worth doing before it is copied into every project made from it. See [Repository templates](../explanation/templates.md#scanning-a-template).

## Read the results

Open **Security** (`:sec`) once a scan finishes. Findings are grouped by severity (CRITICAL, HIGH, MEDIUM, LOW) with cumulative filters — press `c`, `h`, `m`, `l` to toggle a severity on or off; several can be active at once (`c`+`h` means "CRITICAL or HIGH"). Trivy findings include remediation details (fixed version, advisory link) where the scanner provides them; Gitleaks findings show the file and line the secret was found at.

On the Misconfigurations tab, each Kubernetes finding that a Pod Security profile requires starts with `baseline` or `restricted`. To hide the ones your target profile does not ask for, set `scan.pss_profile` to `baseline` or `restricted` in `:cfg` and press `p`; the filter is off each time the tab opens. See [Pod Security profile of a finding](../explanation/scanning.md#pod-security-profile-of-a-finding).

With the Misconfiguration category on, the lists gain a `CFG` column: the number of misconfigurations, coloured by the worst one. A `?` after the count means part of the target was not read — a Helm chart nothing rendered, say — so `0?` is not the same as `0`. See [The CFG column](../explanation/scanning.md#the-cfg-column).

## Fix what the scan found

The results of a repository scan offer two fixes that DevDesk writes itself, both behind a confirmation that defaults to No:

- **Remediation tab** — newer tags for each Dockerfile base image. `S` scans the candidates, `space` chooses one per stage, `enter` shows the diff, `ctrl+o` writes it.
- **Misconfigurations tab** — `ctrl+o` on a finding the built-in catalog covers (a missing `USER`, `ADD` instead of `COPY`, a missing `--no-cache`…) writes the fix, then re-scans to check that the rule is gone.

A missing seccomp profile (KSV-0030, and KSV-0104 when no profile is set) has a built-in fix too: `ctrl+o` adds `seccompProfile.type: RuntimeDefault` to the pod spec. The confirmation warns that the workload may fail at run time, and the re-scan cannot detect that. See [Seccomp: one setting for the whole pod](../explanation/scanning.md#seccomp-one-setting-for-the-whole-pod).

On a Pod Security finding with no built-in fix, `ctrl+o` writes nothing. The footer says which field to set and to what value for the profile of the finding. See [When there is no fix](../explanation/scanning.md#when-there-is-no-fix).

For anything else, an AI client connected over [MCP](connect-an-ai-client.md) can read the finding, edit the file and re-scan. See [Remediation](../explanation/scanning.md#remediation).

## Why results might be stale

The cache is keyed by repository/image, not by commit — a scan result stays valid until you rescan, even if the underlying code has changed since. If you want to be sure you're looking at current findings, press `S` before reading `:sec`.

Deleted repositories and images drop out of the Security view and the dashboard's counts automatically; a cached result for something that no longer exists is never shown as current.

## Scan behind a proxy

Scanner containers receive your `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY`. A
proxy on `127.0.0.1` cannot be reached from a container, and the footer warns you.
See [Work behind a proxy](work-behind-a-proxy.md#scanner-containers).

## Configure which checks run

```yaml
scan:
  categories:
    vuln:      { enabled: true,  tools: [trivy] }            # CVEs
    secret:    { enabled: true,  tools: [trivy, gitleaks] }  # secrets
    misconfig: { enabled: true,  tools: [trivy] }            # IaC misconfiguration
    license:   { enabled: false, tools: [trivy] }            # license issues
  tools:
    trivy:    { ignore_unfixed: false }  # drop CVEs with no available fix
    gitleaks: { history: false }         # scan full git history, not just the working tree
```

These same settings are editable in the configuration view (`:config`, the
`scan` tab) — no need to hand-edit YAML for a one-off change. A category runs
the tools ticked in it; a ticked tool that cannot run is reported by the scan
that needed it. See the [configuration reference](../reference/configuration.md)
for the full `scan:` schema, including Trivy server mode
(`tools.trivy.server`).
