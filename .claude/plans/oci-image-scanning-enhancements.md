# OCI Image Scanning Enhancements

Improve the OCI image scanning experience by adding a better workflow for batch scanning, visual feedback during scans, and quick access to scan details.

## Proposed Changes

### [OCI Images Component]

#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_images/model.go)
- Add `lastScanOptions scan.ScanOptions` to use for batch/repeat scans.

#### [MODIFY] [update.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_images/update.go)
- `handleNormalKeyMsg`:
    - `ctrl+a`: Send `ScanRequestMsg{ImageName: "all"}`.
    - `ctrl+s`: Send `ScanRequestMsg{ImageName: name}`.
    - `enter`: If image is already scanned, send `ScanDetailsRequestMsg{ImageName: name}`.
- Handle `LaunchBatchScanMsg` and `LaunchSingleImageScanMsg`:
    - Update `lastScanOptions` with provided options.
    - Initiate `batchScanCmd` or `scanOneImageCmd` with these options.

#### [MODIFY] [commands.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_images/commands.go)
- Update `scanOneImageCmd` and `batchScanCmd` to accept `scan.ScanOptions` instead of using hardcoded values.

#### [MODIFY] [view.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_images/view.go)
- Update `updateTable` to render `theme.IconRefresh` (🔄) in the "Scanned" column when an image is scanning.
- Update shortcuts and help text to include `enter` for scan details.

---

### [Security Component]

#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/security/model.go)
- Add `returnToView command.ViewType`.
- Add `isImageScan bool`.
- Update `NewWithImageTarget`:
    - Set `isImageScan = true`.
    - If `imageName == "all"`, set target input value to "all" and ensure it's handled as "All OCI Images" in display.
- Update `startScan`:
    - If `isImageScan` and `returnToView` is set:
        - Send `LaunchBatchScanMsg` or `LaunchSingleImageScanMsg` with `opts`.
        - Router will handle returning to the previous view.

---

### [App Router Component]

#### [MODIFY] [app.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/app/app.go)
- Update `handleImageScanRequest`:
    - Set the `returnToView` in the security model to the current view.
- Add handlers for `LaunchBatchScanMsg` and `LaunchSingleImageScanMsg`:
    - Switch view back to `command.ViewOCIImages`.
    - Forward the message to the model.
- Add handler for `ScanDetailsRequestMsg`:
    - Switch to Security view.
    - Initialize with target image and trigger scan (it will be fast if cached).

## Verification Plan

### Manual Verification
1.  **Scan All**:
    - Go to Images view `:images`.
    - Press `ctrl+a`.
    - Verify it switches to Security view with target "all".
    - Adjust scan options (e.g. enable License scan).
    - Press "Launch" (`ctrl+s` or enter on button).
    - Verify it returns to Images view and all images show the 🔄 icon.
    - Wait for completion and verify results reflect License scan.
2.  **Scan Single Image**:
    - Select an image in Images view.
    - Press `ctrl+s`.
    - Verify it switches to Security view with the image name.
    - Press "Launch".
    - Verify it returns to Images view, selection preserved, image shows 🔄.
3.  **View Details**:
    - Select a scanned image.
    - Press `enter`.
    - Verify it switches to Security view and shows findings for that image.
4.  **Browse from Security**:
    - Go to Security view `:sec`.
    - Navigate to "Target", press `b`.
    - Verify it switches to Images view in selection mode.
    - Select an image, press `enter`.
    - Verify it returns to Security view with the selected image.
