# Persistent Security Options to Config

The goal is to allow users to save their security scan preferences (Scan Options, Trivy Options, Gitleaks Options) in the application configuration. This enables launching scans directly from the Workspaces and OCI Images views without having to re-configure them in the Security view every time.

## Proposed Changes

### Configuration

#### [MODIFY] [config.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/config/config.go)
- Add new fields to `ScanConfig` struct:
    - `EnableVuln`, `EnableSecret`, `EnableMisconfig`, `EnableLicense`, `GenerateSBOM` (bool)
    - `IgnoreUnfixed`, `GitleaksHistory` (bool)
    - `GitleaksConfig` (string)
- Update `applyDefaults` and `Default` to set sensible defaults.
- Update `ExpandPaths` to handle `GitleaksConfig`.

---

### Security View

#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/security/model.go)
- Update `New` to initialize the form fields from the configuration.
- Update `startScan` to save the current form options back to the configuration if they were changed.

---

### Workspaces View

#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/model.go)
- Add `getScanOptions` method to build scan options from config.
- Update `startSecurityScan` to launch the scan directly using the saved options, instead of switching to the Security view.
- Add necessary command and message handling to process the direct scan results.
- Keep `ctrl+s` for direct scan. Users can still configure options by switching to the Security view manually via `:sec`.

---

### OCI Images View

#### [MODIFY] [update.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_images/update.go)
- Update `defaultScanOpts` to use all the new fields from `config.ScanConfig`.
- Update `scanSelectedImage` to use `batchScanCmd` directly (similar to how `scanAllUnscanned` works) instead of sending `ScanRequestMsg`.

---

### Scan Engine

#### [MODIFY] [scanner.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/scan/scanner.go)
- Update `DefaultOptions` to consume settings from a global config if needed, or just keep it as is and let callers pass the config-based options. (Callers approach is cleaner).

## Verification Plan

### Automated Tests
- Run `go test ./internal/config/...` to ensure configuration loading/saving works with new fields.

### Manual Verification
1. Open the **Security** view (`:sec`).
2. Change some options (e.g., disable Vulnerabilities, enable Licenses).
3. Start a scan (or just switch back).
4. Verify in `~/.devdesk/config.yaml` that the options are saved.
5. Go to **Workspaces** view.
6. Select a git repo and hit `ctrl+s`.
7. Verify that the scan starts immediately (spinner in SCANNED column) without switching to the Security view.
8. Verify that the scan uses the options saved in step 2 (can be verified by checking the findings later).
9. Go to **OCI Images** view.
10. Select an image and hit `ctrl+s`.
11. Verify that the scan starts immediately without switching view.
