# Fix Navigation Flow: Escape from Scan Details

This plan addresses the issue where pressing `Esc` in the scan results view (security view) does not return the user to the original view (`workspaces` or `images`).

## Proposed Changes

### [security] Component

Update the security view to track its "origin" and handle the `Esc` key at the results level.

#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/security/model.go)

- Add `OriginView command.ViewType` to the `Model` struct.
- Add `BackToOriginMsg` struct to the messages section.
- Update `InEditMode()` to return `true` when in `StateResults`.
- Update `handleKeyMsg` to process `tea.KeyEsc`:
    - In `StateResults`: send `BackToOriginMsg{Origin: m.OriginView}`.
- Update constructors (`NewWithTarget`, `NewWithImageTarget`, `NewWithPreloadedResult`) to accept `OriginView`.

### [app] Component

Update the main app router to pass the origin view and handle the back navigation.

#### [MODIFY] [app.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/app/app.go)

- In `handleWorkspaceScanResultLoaded`, pass `command.ViewWorkspaces` as the `OriginView`.
- In `handleImageScanResultLoaded`, pass `command.ViewOCIImages` as the `OriginView`.
- Handle `security.BackToOriginMsg` in the main `Update` loop to switch the view back to the specified origin.

## Verification Plan

### Manual Verification

1.  **OCI Images Flow**:
    *   Navigate to the `images` view.
    *   Perform a scan on an image (or select a scanned one).
    *   Press `Enter` to view scan details (`security` view, `StateResults`).
    *   Press `Enter` on a CVE to see details (`StateDetails`).
    *   Press `Esc` to return to scan details (Expected: back to `StateResults`).
    *   Press `Esc` again (Expected: back to `images` view).

2.  **Workspaces Flow**:
    *   Navigate to the `ws` view.
    *   Select a scanned git repository.
    *   Press `Enter` to view scan details (`security` view, `StateResults`).
    *   Press `Esc` (Expected: back to `ws` view).

3.  **Command Mode Interaction**:
    *   In `StateResults` of security view, press `:` to enter command mode.
    *   Press `Esc` (Expected: quit command mode, stay in `security` view).
    *   Press `Esc` again (Expected: return to origin view).
