# Implementation Plan: Improve Workspace Scan Column

The goal is to enhance the consistency and visual quality of the `Scanned` column in the `workspaces` view. This includes adopting NerdFont Material Design icons, a braille scanning indicator, and ensuring the cache is purged when restarting a scan.

## Proposed Changes

### [Component] Workspaces TUI

#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/model.go)
- Add `spinner spinner.Model` and `spinnerFrameIdx int` to the `Model` struct.
- Initialize `spinner` in `New` with `spinner.Braille` (or `spinner.Dot` as used in `oci_images`).
- Update `Update` to handle `spinner.TickMsg`, updating the model's `spinner` and `spinnerFrameIdx`.
- Update `startSecurityScan` to purge the cache for the target repo(s) before launching the scan.
- Update `requestScanAll` to ensure consistent purging.

#### [MODIFY] [view.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/view.go)
- Update `formatScanColumns` to:
    - Return `"-"` for unscanned entries.
    - Use `m.spinner.Frames[m.spinnerFrameIdx]` during scanning.
    - Add a NerdFont Material Design clock icon prefix for relative time (e.g., `󰅐 1d ago`).
    - Use a NerdFont Material Design folder icon prefix for directory coverage (e.g., `󰉋 5/6`).
    - Standardize coverage format to `X/Y` always (removing the "X repos" format).

## Verification Plan

### Manual Verification
1. **Unscanned State**: Verify that repos without scan results show `-`.
2. **Scanning Indicator**: Launch a scan (`ctrl+s`) and verify the braille character animation is shown.
3. **Cache Purging**: Re-launch a scan on a previously scanned repo. Verify that the previous CVE counts are cleared (return to `-` or 0) during the scan.
4. **Icons & Formatting**:
    - Verify that a scanned repo shows the clock icon with the relative time.
    - Verify that a directory shows the folder icon with the `X/Y` coverage ratio.
5. **Batch Scan**: Launch a "scan all" (`ctrl+a`) and verify all entries update correctly with the braille spinner.
