# Implementation Plan - Refactor Security Header and Separate Finding Counts

This plan outlines the changes required to separate vulnerability (CVE), secret, and license counts in the security header and remove the security score from the UI.

## Proposed Changes

### [Component] Scan Logic (`internal/scan`)

#### [MODIFY] [scanner.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/scan/scanner.go)
- Update `Result` struct:
    - Add `SecretCount`, `LicenseCount`, and `MisconfigCount` (all `int`).
    - Remove `Score` (`int`) and `Passed` (`bool`) fields.
    - Ensure `Counts` (`SeverityCounts`) represents only vulnerabilities (CVEs).
- Update `CountFindings()` method:
    - Implement logic to separate findings by source:
        - `trivy` with package info -> Increment `Counts` (CVEs) by severity.
        - `gitleaks` or `trivy` with match -> Increment `SecretCount`.
        - `trivy-license` -> Increment `LicenseCount`.
        - `trivy-misconfig` -> Increment `MisconfigCount`.
- [DELETE] Remove `CalculateScore()` method.

### [Component] Cache Layer (`internal/cache`)

#### [MODIFY] [image_scan.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/cache/image_scan.go)
- Update `ImageScanEntry` struct:
    - Remove `Score`.
    - (Optional) Add `SecretCount`, `LicenseCount` if needed for quick viewing in listings, though primarily needed for the header.

#### [MODIFY] [workspace_scan.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/cache/workspace_scan.go)
- Update `WorkspaceScanEntry` struct:
    - Remove `Score`.

### [Component] UI Layer (`internal/ui`)

#### [MODIFY] [model.go (security)](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/security/model.go)
- Update `GetHeaderInfo()`:
    - Change "CVEs" label or ensure it only uses the new CVE-only `Counts`.
    - Add "Secrets" and "Licenses" lines.
    - Remove "Score" line.
- Update `handleScanComplete()`:
    - Adjust how it sends `WorkspaceScanCompleteMsg` to exclude `Score`.
    - Ensure it updates the new count fields.
- Update `saveImageScanToCache()`:
    - Adjust to remove `Score`.

#### [MODIFY] [model.go (workspaces)](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/model.go)
- Update `WorkspaceScanCompleteMsg` struct to remove `Score`.
- Update `handleWorkspaceScanComplete()` handler accordingly.

## Verification Plan

### Automated Tests
- Create `internal/scan/scanner_test.go` to verify the `CountFindings` logic correctly separates vulnerabilities, secrets, and licenses into their respective counters.
```bash
go test ./internal/scan/...
```

### Manual Verification
1. Launch the application with `go run main.go`.
2. Navigate to the Security view.
3. Perform a scan on a directory known to have mixed findings (e.g., vulnerabilities, a secret, and a license issue).
4. Verify that the header displays:
    - The CVE pill bar (5 numbers) reflecting ONLY vulnerabilities.
    - A "Secrets" line with the count of secrets.
    - A "Licenses" line with the count of licenses.
    - NO "Score" line.
5. Verify that navigating back to workspaces/images view still shows the CVE counts correctly (now filtered to exclude secrets/licenses).
