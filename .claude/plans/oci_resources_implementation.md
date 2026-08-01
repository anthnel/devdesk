# Implementation Plan: OCI Resources View

Evolve the current "OCI Images" view into a comprehensive "OCI Resources" view with support for Images, Networks, and Volumes, and the ability to launch containers directly from images.

## Proposed Changes

### [Component] UI/UX & Navigation

#### [MODIFY] internal/ui/oci_images/model.go
- Add `activeTab` field (enum: Images, Networks, Volumes).
- Add `formMode` boolean and `launchForm` (a new component or struct) to handle view-port forms.
- Data structures for `networks` and `volumes`.

#### [MODIFY] internal/ui/oci_images/view.go
- Update `View()` to render either the active tab's table or the current form.
- Implement the tab line at the bottom (above the status line).
- Update `GetShortcuts()` to return context-aware shortcuts based on `activeTab` and `formMode`.
- Implement `renderTabs()` helper.

#### [MODIFY] internal/ui/oci_images/update.go
- Handle `Tab` and `Shift+Tab` to cycle between tabs.
- Handle `n` and `v` to trigger network/volume creation forms.
- Handle `Enter` on images to trigger the launch form.
- Switch between list and form states.

---

### [Component] Docker Integration

#### [MODIFY] internal/docker/docker.go (or similar)
- Add functions to list/create/delete/prune Networks.
- Add functions to list/create/delete/prune Volumes.
- Add `LaunchContainer(opts ContainerLaunchOptions)` function.
- Add `GetImageExposePorts(imageName string)` to extract `EXPOSE` metadata.

---

### [Component] Form Components

#### [NEW] internal/ui/oci_images/launch_form.go
- Implement a viewport-based form for container launch.
- Fields: Name, Ports (pre-filled), Env, Volumes, Network.
- Keybindings: `Up/Down` to move, `Enter` to submit, `Esc` to cancel.

---

## Verification Plan

### Automated Tests
- Unit tests for `GetImageExposePorts` parsing logic.
- Unit tests for tab switching logic in the model.

### Manual Verification
1. **Navigation**: Verify `Tab` cycles through Images, Networks, and Volumes.
2. **Tab UI**: Verify the tab line is consistently below the viewport.
3. **Launch Form**:
   - Press `Enter` on an image.
   - Verify the viewport is replaced by the form.
   - Verify `Esc` returns to the image list.
   - Verify ports are pre-filled based on image metadata.
4. **Resources Management**:
   - Create a network with `n`.
   - Create a volume with `v`.
   - Delete/Prune and verify Docker state via CLI.
