# Implementation Plan - Move Form Title to Viewport Title

Move form titles from within the viewport to the viewport's border title, creating a breadcrumb effect (e.g., "Status Monitor > Edit Monitor").

## Proposed Changes

### [Theme]
#### [MODIFY] [icons.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/theme/icons.go)
- Ensure `IconChevronRight` is available for use as a breadcrumb separator.

### [Status View]
#### [MODIFY] [view.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/status/view.go)
- Update `GetTitle()` to check if `m.componentForm != nil`.
- If active, append ` theme.IconChevronRight + " " + m.componentForm.GetTitle()` to the base title.

#### [MODIFY] [component_form.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/status/components/component_form.go)
- Add `GetTitle() string` method returning "Add New Monitor" or "Edit Monitor".
- Remove title rendering from the `View()` method.

### [OCI Images View]
#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_images/model.go)
- Update `GetTitle()` to include `launchForm` or `resourceForm` titles if they are active.

#### [MODIFY] [launch_form.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_images/launch_form.go)
- Add `GetTitle() string` method returning "Launch Container".
- Remove title rendering from the `View()` method.

#### [MODIFY] [resource_form.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_images/resource_form.go)
- Add `GetTitle() string` method returning "Resource Usage".
- Remove title rendering from the `View()` method.

### [Security View]
#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/security/model.go)
- Update `GetTitle()` to append ` theme.IconChevronRight + " Scan Configuration"` when `m.state == StateInput`.

### [GitLab Explorer View]
#### [MODIFY] [view.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/gitlab/explorer/view.go)
- Update `GetTitle()` to check if `m.creationForm != nil`.
- If active, append ` theme.IconChevronRight + " " + m.creationForm.GetTitle()`.

#### [MODIFY] [creation_form.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/components/creation_form.go)
- Add `GetTitle() string` method returning "New Group" or "New Project".
- Remove title rendering from the `View()` method.

---

## Verification Plan

### Manual Verification
1.  **Status View**:
    *   Open Status view. Viewport title should be "Status Monitor".
    *   Press `n`. Viewport title should change to "Status Monitor > Add New Monitor". The "Add New Monitor" title should no longer be inside the viewport.
    *   Select a monitor and press `e`. Viewport title should change to "Status Monitor > Edit Monitor".
2.  **OCI Images View**:
    *   Open OCI Images view.
    *   Select an image and press `l`. Viewport title should change to "OCI Images > Launch Container".
    *   Select a container/image and view resource usage. Viewport title should show "OCI Images > Resource Usage".
3.  **Security View**:
    *   Open Security view. Viewport title should be "Security Scanner > Scan Configuration".
    *   Start a scan. Viewport title should change to "Security Scanner (target)".
4.  **GitLab Explorer**:
    *   Open GitLab Explorer.
    *   Press `g` or `n`. Viewport title should show the breadcrumb to "New Group" or "New Project".
