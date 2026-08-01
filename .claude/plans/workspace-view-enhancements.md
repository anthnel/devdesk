# Workspace View Enhancements Implementation Plan

This plan outlines the changes required to improve the workspace view by renaming columns, adding new columns, updating icons based on scan results, and enhancing navigation shortcuts.

## Proposed Changes

### [Component] Workspaces UI

#### [MODIFY] [view.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/view.go)

- Update `numColumns` from 9 to 10.
- In `defaultColumns()`:
    - Rename "!" to "Secrets".
    - Add a new column "Modified" at the end.
- In `calculateColumns()`:
    - Update fixed columns total and flex calculations for 10 columns.
    - Rename header "!" to "Secrets".
    - Set the title for the new "Modified" column.
- In `formatScanColumns()`:
    - Implement the conditional icon logic:
        - No scan: `theme.IconWorkspaceUnknown`.
        - Scan done, no secrets: `theme.IconWorkspaceTrusted`.
        - Secrets detected: `theme.IconWorkspaceUntrusted`.
- Add a new helper `formatModTime(t time.Time) string` to return a relative time string (e.g., "1 hour ago").

#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/model.go)

- In `updateTableData()`:
    - Include the "Modified" column value by using `formatModTime(entry.ModTime)`.
- In `handleKeyMsg()`:
    - Allow the "n" shortcut even if `m.currentPath != ""` (currently restricted to root).
- In `startAdd()`:
    - Remove the restriction `if m.currentPath != "" { return m, nil }`.
- In `createWorkspace()`:
    - Use `m.currentPath` as the base directory if it's set, otherwise use the default workspaces directory.

#### [MODIFY] [icons.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/theme/icons.go)

- Define `IconSecurity` if it's missing (to avoid build errors or replace its usages). It seems to be used but not defined in the `theme` package.

## Verification Plan

### Automated Tests
- No automated tests currently exist for the UI core logic in `workspaces`. I will verify the changes manually.

### Manual Verification
1.  **Column Names & Alignment**:
    - Launch the application and navigate to the Workspaces view.
    - Verify that the column previously named "!" is now "Secrets".
    - Verify that the "TYPE" column is left-aligned.
    - Verify that a new "Modified" column is present at the end of the table.
2.  **Relative Date**:
    - Check that the "Modified" column shows relative dates like "1 hour ago", "now", etc.
3.  **Security Icons**:
    - View a workspace that has NOT been scanned; verify it shows `IconWorkspaceUnknown` (󱈃).
    - Run a scan on a workspace with no secrets; verify it shows `IconWorkspaceTrusted` (󱮁).
    - Run a scan on a workspace with secrets; verify it shows `IconWorkspaceUntrusted` (󱮂).
4.  **New Folder Shortcut**:
    - Navigate into a subdirectory.
    - Press `n` and create a new directory.
    - Verify that the directory is created *within* the current subdirectory and not at the root level.
