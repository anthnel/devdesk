# Implementation Plan: TUI Rules 122 & 123 Compliance

Align the application with Rule 122 (Plain text in tables) and Rule 123 (Persistent tabs under tables) to prevent rendering artifacts and improve layout consistency.

## Goals

- **Rule 122**: Ensure all `table.Row` values are plain text/icons to avoid `go-runewidth` truncation issues and ANSI bleed.
- **Rule 123**: Implement a fixed height layout for tables to ensure associated tab bars (breadcrumbs or navigation) stay fixed and visible below the table viewport.

## Proposed Changes

### [Theme Component]

#### [MODIFY] [styles.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/theme/styles.go)
- Export `ActiveTabStyle` and `InactiveTabStyle` as global variables to align with Rule 123 implementation patterns.
- Update `RenderTabs` to use these exported styles.

---

### [Workspaces View]

#### [MODIFY] [view.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/view.go)
- **Fix Rule 122 violation**: Remove `lipgloss.Style.Render` from `formatSensitive`. Return plain icons (`theme.IconWarning` or `theme.IconSecurity`) without styling.
- **Fix Rule 123 violation**: Update `renderTable` to use a layout that ensures the tab bar is always fixed at the bottom, separate from the `table.View()`.
- Ensure no empty lines are manually appended to the table view; instead, use height constraints.

#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/model.go)
- Adjust `updateTableSize` to reserve the correct amount of space for the fixed tab bar (accounting for empty lines and margins as per Rule 123).

---

### [Security View]

#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/security/model.go)
- **Fix Rule 123 violation**: Update `renderResultsView` to render the table and tabs separately, ensuring the table height is constrained so tabs remain visible and non-scrolling.
- Adjust `updateFindingsTable` to ensure the table height matches the calculated available space.

---

### [Status View]

#### [MODIFY] [view.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/status/view.go)
- **Fix Rule 123 violation**: Update `renderTable` to prevent concatenation of table and tabs. Implement the fixed layout pattern.
- Ensure `updateTable` handles table height correctly after layout change.

## Verification Plan

### Automated Tests
- Run existing TUI tests to ensure no regressions:
  ```bash
  go test ./internal/ui/...
  ```

### Manual Verification
- **Rule 122**: Run the app (`go run main.go`), navigate to Workspaces. Verify that the sensitivity column ("!") displays correctly without artifacts, even when the row is truncated or scrolled.
- **Rule 123**:
  1. Open the Security view with many results. Scroll the table and verify that the tab bar remains fixed at the bottom.
  2. Open the Workspaces view and navigate into a deep directory structure. Verify the breadcrumb tabs at the bottom are always visible.
  3. Open the Status view, switch tabs, and verify the tab bar stays fixed.
