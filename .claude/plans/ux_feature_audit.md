# UX and Feature Audit Roadmap

This document outlines the roadmap for UX improvements and new features in DevDesk, structured as an implementation plan for Claude Code to execute.

## User Review Required

> [!NOTE]
> This plan describes the strategic roadmap for UX changes and new feature modules. Individual features should be broken down into detailed sub-plans as needed.

## Proposed Changes

### Platform-Specific UX Fixes

#### [MODIFY] [model.go (dashboard)](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/ui/dashboard/model.go)
#### [MODIFY] [model.go (security)](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/ui/security/model.go)
Remove hardcoded `xdg-open` commands. Use `runtime.GOOS` switches for cross-platform browser launching, similar to `explorer/model.go`.

#### [MODIFY] [terminal.go](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/ui/terminal/terminal.go)
Replace fallback `/bin/sh` shell with `%COMSPEC%` on Windows systems.

#### [MODIFY] [update.go (containers)](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/ui/containers/update.go)
Modify `openExternalPager()` and `inspectSelectedContainer()` to avoid using `sh -c` on Windows. Use `cmd /c` or invoke processes directly.

---

### Windows Unit Test Fixes

#### [MODIFY] [launch_options_test.go](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/cache/launch_options_test.go)
#### [MODIFY] [storage_test.go](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/credentials/storage_test.go)
Skip Unix permission tests checking for exact `0600` permissions on Windows using `runtime.GOOS == "windows"`.

#### [MODIFY] [config_test.go](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/config/config_test.go)
#### [MODIFY] [context_test.go](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/config/context_test.go)
Normalize path assertions with `filepath.ToSlash()` to support backslashes on Windows.

---

### Feature Roadmap (New Modules)

#### [NEW] [braille_chart.go](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/ui/components/braille_chart.go)
A reusable braille chart component to visualize CPU and Memory usage history in real-time.

#### [NEW] [ports.go (containers)](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/ui/containers/ports.go)
 A Port Forwarding Manager dashboard to inspect and manage Docker container port mappings, with actions to copy local URLs.

#### [NEW] [layers.go (oci)](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/ui/oci_resources/layers.go)
An OCI Image Layer Visualizer (similar to `dive`) using daemonless registry inspection via `google/go-containerregistry`.

#### [NEW] [auto_patch.go (security)](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/ui/security/auto_patch.go)
Interactive security patches to suggest updating vulnerable base images in Dockerfiles.

---

## Verification Plan

### Manual Verification
- Test browser opening shortcut `o` in `security` and dashboard shortcuts `m` and `i` on Windows/macOS/Linux.
- Run all tests on Windows and verify clean completion.
- Implement and verify each new roadmap module individually.
