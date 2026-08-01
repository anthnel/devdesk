# Informative Selection Mode Messages in Footer

This plan describes how to display succinct, context-aware messages in the footer's information line when the application is in selection mode (e.g., picking a directory for scanning or for pulling a GitLab project).

## Proposed Changes

### [Component] UI Inter-communication

#### [MODIFY] [security/model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/security/model.go)
- Enhance `SelectionRequestMsg` to include a context/reason for the selection.

#### [MODIFY] [app/app.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/app/app.go)
- Update `handleSelectionRequest` and `handleExplorerPullRequest` to pass the selection context to the target views.

---

### [Component] Workspaces View

#### [MODIFY] [workspaces/model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/model.go)
- Add `selectionMessage` string field to the `Model`.
- Update `NewForSelection` to accept and store the message.

#### [MODIFY] [workspaces/view.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/view.go)
- Update `RenderFooter` to display the `selectionMessage` in the information line when in `ModeSelecting`.

---

### [Component] OCI Images View

#### [MODIFY] [oci_images/model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_images/model.go)
- Add `selectionMessage` string field to the `Model`.
- Update `NewForSelection` to accept and store the message.

#### [MODIFY] [oci_images/view.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_images/view.go)
- Update `RenderFooter` to display the `selectionMessage` in the information line when `selectionMode` is true.

## Verification Plan

### Automated Tests
- This change mostly affects TUI rendering. Basic verification will be done via manual testing.

### Manual Verification
1.  **Security -> Directory Selection**:
    - Go to Security view (`:sec`).
    - Focus "Target" field, change type to "directory".
    - Press `b`.
    - Verify footer reflects "select a directory to scan".
2.  **Security -> Image Selection**:
    - Go to Security view (`:sec`).
    - Focus "Target" field, change type to "image".
    - Press `b`.
    - Verify footer reflects "select an image to scan".
3.  **Explorer -> Pull Destination Selection**:
    - Go to GitLab Explorer (`:explorer`).
    - Select a project/group and press `p`.
    - Verify footer in workspaces view reflects "select a destination for pulling".
