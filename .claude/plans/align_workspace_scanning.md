# Plan: Align Workspace Scanning with OCI Image Scanning

## Goal
Align the scanning functionality of the Workspace view with the OCI Images view. This includes implementing parallel scanning (utilizing 50% of available CPUs), recursive folder scanning, "Scan All" capabilities, and improving data aggregation for non-git directories.

## Proposed Changes

### Workspaces View

#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/model.go)
- Add state to track multiple scanning paths and batch scan status.
- Update `handleKeyMsg` to support new shortcuts:
    - `A`: Scan all unscanned git repos in the current view (recursive).
    - `ctrl+a`: Scan all git repos in the current view (recursive, purges cache first).
- Update `startSecurityScan` to handle recursive scanning of sub-folders (triggering batch scan of `SubRepoPaths`).
- Integrate `batchScanCmd` logic (moved or shared).

#### [NEW] [commands.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/commands.go)
- Move existing workspace commands and add new ones:
    - `scanOneRepoCmd`: Scans a single git repo with Image-style semaphore for parallelism.
    - `batchScanCmd`: Worker pool to scan multiple repos in parallel.
    - `deleteScanCacheCmd`: Purge cache for a list of repos (Rule 126).

#### [MODIFY] [view.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/view.go)
- Update `formatScanColumns` to better reflect scanning state (spinner/refresh icon).
- Ensure `Scanned` column uses relative time (e.g., "5m ago") via `theme.TimeAgo`.
- Update help content to reflect new shortcuts.

## Verification Plan

### Automated Tests
- None currently exist for this component. Verification will be manual.

### Manual Verification
1.  **Navigate** to a workspace folder containing several git repos.
2.  **Verify Recursive Scan**: Press `ctrl+s` on a parent folder. All nested git repos should start scanning. The parent folder should show an aggregate "scanning" indicator.
3.  **Verify Batch Scan**: Press `A` or `ctrl+a` at the workspaces list level. Multiple repos should scan in parallel.
4.  **Verify Data Aggregation**: After scanning, check that non-git directories correctly sum up the vulnerability counts of their children.
5.  **Verify Relative Time**: Check that the `Scanned` column updates with relative time.
6.  **Verify "Secrets" logic**: Ensure icons (`IconWorkspaceTrusted`/`IconWorkspaceUntrusted`/`IconWorkspaceUnknown`) are correctly displayed based on scan results.
