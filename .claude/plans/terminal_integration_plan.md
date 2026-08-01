# Implementation Plan - Terminal Integration in Workspace View

This plan describes how to add a "Terminal" feature to the workspace view, allowing users to quickly open an interactive shell in the selected directory. This implementation is designed to be cross-platform (Linux, macOS, and Windows).

## Proposed Changes

### Workspaces View

#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/model.go)

- Add `TerminalExitMsg` to handle the resume of the TUI after the shell closes.
- In `handleKeyMsg`, add a case for the `t` key to call `m.openTerminal()`.
- Implement `openTerminal()`:
    - Determine the `targetPath`:
        - If a row is selected, use the entry's `Path`.
        - If no row is selected (empty directory), use `m.currentPath` (or the default workspaces directory if at root).
    - Handle cross-platform shell detection (Rule 108):
        - **Linux/macOS**: Try `$SHELL` environment variable, fallback to `/bin/bash`, then `/bin/sh`.
        - **Windows**: Try `%COMSPEC%` environment variable, fallback to `powershell.exe`, then `cmd.exe`.
    - Use `tea.ExecProcess` to start the shell in the `targetPath`.
    - On exit, return a `TerminalExitMsg`.
- In `Update`, handle `TerminalExitMsg` by triggering a reload of the directory entries (`m.loadEntries()`) to sync any changes made in the terminal (e.g., new/deleted files).

#### [MODIFY] [view.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/view.go)

- Update `GetShortcuts()` to include the `t` shortcut:
    - Key: `t`, Description: `terminal`.
    - Always visible in normal mode.
- Update `GetHelpContent()`:
    - Add `t` to the keybindings list.
    - Add a section explaining that it opens the default system shell in the selected folder.

## Verification Plan

### Automated Tests
- Verify path resolution logic (root, subdirectory, selected entry).
- Verify shell detection logic by mocking environment variables.

### Manual Verification
1.  **Linux/macOS**:
    - Press `t` on a folder.
    - Verify shell opens in that folder (`pwd`).
    - `exit` and verify TUI resumes and refreshes.
2.  **Windows (if applicable)**:
    - Press `t` on a folder.
    - Verify PowerShell or CMD opens in that folder.
    - `exit` and verify TUI resumes.
