# Goal: Browse OCI Registry Images & Tags, Pull, and Scan

Provide a premium, robust feature in the OCI Registries view to browse images/tags from a configured registry, pull them to local docker store, and run security scans.

## User Review Required

> [!NOTE]
> All UI text, logs, and messages will be written in **English US** as per **Rule 129**.
> The workflow uses the Elm Architecture (TEA) and strictly adheres to the keybinding rules (e.g. no Tab for forms, ↑/↓ for focus, space for toggles, clear footer after 3s).

> [!IMPORTANT]
> Since passwords/tokens are not persisted in the config, private registries require the user to have performed a `docker login` (which stores credentials in `~/.docker/config.json`). The pull/scan command runs via the local docker CLI, which automatically picks up these credentials.
> To list tags, we call the OCI HTTP API. If it requires authentication, we'll gracefully fall back and allow the user to provide an optional token/password for the browsing session, or show a clear warning if unauthorized.

---

## Proposed Changes

### [Docker Core Client]

#### [MODIFY] [client.go](file:///c:/Users/anthoni/projects/gitlab/devdesk/internal/docker/client.go)
- Add a new helper `PullImage(imageName string) error` to execute `docker pull <imageName>`.

---

### [OCI Registry UI Components]

#### [NEW] [registry_browser.go](file:///c:/Users/anthoni/projects/gitlab/devdesk/internal/ui/oci_resources/registry_browser.go)
- Implement `RegistryBrowser` struct and state:
  - Text input for repository name.
  - Text input for optional credentials (password/token) to query private registries' HTTP API.
  - Async catalog listing where available (`/v2/_catalog`), with suggestion list (similar to connectivity test form).
  - Search filter input to filter/locate repositories.
  - Tag list table with details (using standard Charm `table.Model`).
  - Actions: `p` (Pull image), `ctrl+s` (Scan image).
  - States: `browserStateInput` (Entering repository name), `browserStateTags` (Listing repository tags), `browserStateStatus` (Pulling/Scanning status screen).

#### [MODIFY] [commands.go](file:///c:/Users/anthoni/projects/gitlab/devdesk/internal/ui/oci_resources/commands.go)
- Add `pullRegistryImageCmd(imageName string) tea.Cmd` to execute `docker.PullImage`.
- Add `pullAndScanRegistryImageCmd(imageName string, opts scan.ScanOptions) tea.Cmd` to pull first, then call Trivy scanner, update the scan cache, and return the result.

#### [MODIFY] [model.go](file:///c:/Users/anthoni/projects/gitlab/devdesk/internal/ui/oci_resources/model.go)
- Add `registryBrowser *RegistryBrowser` to the OCI model.
- Add message structs:
  - `RegistryCatalogLoadedMsg`
  - `RegistryTagsLoadedMsg`
  - `RegistryPullCompleteMsg`
  - `RegistryScanCompleteMsg`

#### [MODIFY] [update.go](file:///c:/Users/anthoni/projects/gitlab/devdesk/internal/ui/oci_resources/update.go)
- Handle opening the browser when pressing `enter` on the selected registry in `tabRegistries`.
- Delegate update calls to the `registryBrowser` when non-nil.
- Handle state updates for tags loading, pulling progress, scan completion, and error notifications.
- Trigger `ScanDetailsRequestMsg` upon successful scan completion to open the vulnerability report.
- Ensure footer messages use `clearInfoMsgCmd()` to disappear after 3 seconds.

#### [MODIFY] [view.go](file:///c:/Users/anthoni/projects/gitlab/devdesk/internal/ui/oci_resources/view.go)
- Render the `registryBrowser` as a priority overlay in `View()`.
- Add help contents and dynamic keybindings to `GetShortcuts()` and `GetHelpContent()`.

---

## Verification Plan

### Automated Tests
- Run `go test ./internal/...` to ensure all existing tests pass and verify no compile-time or logic breakages.

### Manual Verification
- Launch DevDesk (`go run main.go`).
- Go to `OCI Resources` -> `Registries` tab.
- Select a registry (e.g. `docker.io` or configured custom registry) and press `enter`.
- Verify the Registry Browser view opens with top-padding (Rule 131) and instructions.
- Search for a public image (e.g., `library/nginx`) and press `enter`.
- Verify tags list is fetched and displayed.
- Select a tag and press `p` (pull). Check that spinner/status shows, and pull succeeds. Verify the image appears in the local Images tab.
- Select a tag and press `ctrl+s` (scan). Check that it pulls and scans, and opens the vulnerability details view in the Security view when completed.
- Test with an invalid repository to verify the error message is shown in the footer and auto-cleared in 3 seconds.
