# Implementation Plan - Container Launch Enhancements

This plan outlines the enhancements to the container launch form in `devdesk`. The goal is to provide more control over the container lifecycle while simplifying the user experience through intelligent defaults and real-time verification.

## Proposed Changes

### [Component] Docker Internal Client
#### [MODIFY] [internal/docker/client.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/docker/client.go)
- Update `ContainerLaunchOptions` struct:
    - Add `Remove bool` (`--rm`)
    - Add `Interactive bool` (`-i`)
    - Add `TTY bool` (`-t`)
- Update `LaunchContainer` function to include these new flags in the `docker run` command.

---

### [Component] OCI Images UI
#### [MODIFY] [internal/ui/oci_images/launch_form.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_images/launch_form.go)
- **UI Updates**:
    - Add checkboxes for `Remove`, `Detach`, and `Interactive/TTY`.
    - Update the layout to include these options in a new "Options" section.
    - Update `buildCommandLines` to reflect the new flags in the live preview.
- **Auto-selection Logic**:
    - In the `Update` loop, watch for changes in the `entrypointInput`.
    - If the entrypoint matches common shells (`sh`, `bash`, `zsh`, `python`, etc.), automatically:
        - Set `Interactive/TTY` to `true`.
        - Set `Detach` to `false`.
- **Entrypoint Verification**:
    - Implement a debounced check that triggers a background command.
    - The command will be: `docker run --rm --entrypoint /bin/sh <image> -c "command -v <entrypoint>"`
    - Store the verification state (`Success`, `Warning`, `Checking`) in the `LaunchForm` struct.
    - Display a visual indicator (icon) next to the Entrypoint field based on this state.

#### [MODIFY] [internal/ui/oci_images/update.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_images/update.go)
- Handle new message types for "Verification Started" and "Verification Finished" to update the form state without blocking the main UI thread.

## Verification Plan

### Automated Tests
- No existing automated tests for the UI forms were found.
- Verification will rely on manual testing of the `docker run` command generation logic.

### Manual Verification
1. **Option Toggles**:
    - Open the Launch Form for an image.
    - Toggle the new checkboxes (`--rm`, `-it`, `-d`).
    - Verify that the command preview on the right updates correctly.
2. **Auto-selection**:
    - Type `/bin/bash` in the Entrypoint field.
    - Verify that `-it` is checked and `-d` is unchecked automatically.
3. **Verification UI**:
    - Type a known valid entrypoint (e.g., `ls` or `sh`).
    - Observe the "Checking" spinner (if implemented) followed by a success icon.
    - Type a non-existent entrypoint.
    - Observe the warning icon.
4. **Final Launch**:
    - Launch a container with `--rm` enabled.
    - Stop the container and verify it is automatically removed from the list.
