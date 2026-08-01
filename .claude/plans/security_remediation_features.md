# Security Scan Remediation Plan

Implement guided remediation to help users fix security findings (CVEs, Secrets, Misconfigurations) directly from the application.

## User Review Required

> [!IMPORTANT]
> This change introduces a "Fix" action in the UI. For vulnerabilities, it will primarily provide *instructions* (commands) rather than automatic execution to ensure user control over build dependencies.

## Proposed Changes

### [Scan Engine]

Extend the finding data model to capture remediation metadata.

#### [MODIFY] [scanner.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/scan/scanner.go)
- Add fields to `Finding` struct:
  - `Resolution` (string): Recommended fix steps.
  - `References` ([]string): Links to advisories or documentation.
  - `FixCommand` (string): Suggested command to run (e.g., `npm update`).

#### [MODIFY] [trivy.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/scan/trivy.go)
- **Vulnerabilities**: Populate `References` from `PrimaryURL` and advisory links.
- **Misconfigurations**: Map `Resolution` from Trivy's `Resolution` field.

#### [MODIFY] [gitleaks.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/scan/gitleaks.go)
- Ensure `Fingerprint` is always available for reliable "Ignore" actions.

---

### [Security UI]

Enhance the results and detail views to support remediation actions.

#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/security/model.go)
- **Detail View Enhancement**:
  - Add a "Remediation" section showing `Resolution` and `FixCommand`.
  - Add a "References" section for external links.
- **Interactive Actions**:
  - Implement `f` (Fix) keybinding in `StateDetails`:
    - **Vulnerabilities**: Show a modal with the suggested update command.
    - **Misconfigurations**: Show detailed resolution steps.
    - **Secrets**: Trigger the ignore flow (already exists in list view).
  - Implement `o` (Open) to open the first reference URL in the default browser.

## Verification Plan

### Automated Tests
- Update `internal/scan/scanner_test.go` to verify the new `Finding` fields are correctly populated from mock Trivy/Gitleaks output.

### Manual Verification
1. Conduct a scan on a repository with known issues.
2. Navigate to a High/Critical finding detail view.
3. Verify that the "Remediation" section appears.
4. Press `f` and verify the action appropriate for the finding type.
5. Press `o` on a finding with a URL to verify it opens the browser.
