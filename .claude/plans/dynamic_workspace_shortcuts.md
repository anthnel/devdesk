# Dynamic Workspace Shortcuts

Restrict shortcut visibility and update descriptions in the Workspaces view based on the selected node's type and state (Git repository, scan status).

## User Review Required

> [!IMPORTANT]
> - `<n>` will be hidden if a Git repository is selected.
> - `<enter>` will only show "scan details" if a Git repository with cached scan results is selected.
> - `<ctrl+w>` will be hidden if the selected node is not a Git repository.

## Proposed Changes

### TUI Rules

#### [MODIFY] [tui-layout.md](file:///home/anthoni/projects/gitlab/devsecops/devdesk/.claude/rules/tui-layout.md)
Add **Rule 130: Shortcuts must be dynamic**.

### Workspaces Component

#### [MODIFY] [view.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/view.go)
Update `GetShortcuts()` to:
1.  Identify the selected entry (if any).
2.  Filter/modify shortcuts based on the entry's state:
    - `<n>`: Only show if NO entry is selected or the selected entry is NOT a Git repository. Update description to "new directory".
    - `<enter>`: Only show if the selected entry IS a Git repository AND has cached scan results. Update description to "scan details".
    - `<ctrl+w>`: Only show if the selected entry IS a Git repository.
    - `<ctrl+s>`: Only show if the selected entry IS a Git repository (or a directory containing sub-repos, which is already handled by `startSecurityScan` but we should hide the shortcut if no action is possible).

## Verification Plan

### Manual Verification
1.  Navigate to the Workspaces view.
2.  Select a non-git directory:
    - Verify `<n>` is shown as "new directory".
    - Verify `ctrl+w` and `enter` (for scan details) are NOT shown.
3.  Select a Git repository that has NOT been scanned:
    - Verify `<n>` is NOT shown.
    - Verify `<ctrl+w>` is shown as "browser".
    - Verify `enter` is NOT shown (or doesn't say "scan details").
4.  Scan the Git repository.
5.  After scan completes, select the Git repository:
    - Verify `enter` is shown as "scan details".
6.  Navigate into a directory where no items exist:
    - Verify `<n>` is shown as "new directory".
