# Multi-Registry Image Search Workflow

Implementation plan for adding a concurrent multi-registry search/browse capability triggered directly from the OCI Images tab (`tabImages`), replacing the old single-registry browser entirely.

## User Review Required

> [!IMPORTANT]
> - **Removal of Registries Tab Browse**:
>   - The "Browse" feature will be removed from the Registries tab. Pressing `Enter` on the Registries tab will no longer open a browser.
>   - The OCI registry browser is now opened from the **Images tab** using the `b` shortcut.
> - **Authentication Integration**:
>   - For each registry selected in the search checklist, the browser will retrieve and use stored Docker credentials (from `docker login`, managed by `docker.GetStoredCreds()`) if `AuthEnabled` is true.
> - **Registry Filtering in Results**:
>   - Pressing `r` in the results table cycles the active registry filter (e.g. showing only `docker.io` tags, only a private registry's tags, or `All`).

## Open Questions

> [!NOTE]
> None. All design choices have been aligned with user feedback.

## Proposed Changes

### OCI Resources Components

We will repurpose the existing `registry_browser.go` and clean up the coordinator files to completely replace the single-registry browse workflow.

---

#### [MODIFY] [registry_browser.go](file:///c:/Users/anthoni/projects/gitlab/devdesk/internal/ui/oci_resources/registry_browser.go)
- Redefine `RegistryBrowser` to support multi-registry search and browse.
- **Form State**:
  - `repoInput textinput.Model`: text input for the image/repository name (e.g. `nginx`).
  - `registries []config.RegistryItem`: configured registries retrieved from context.
  - `selectedRegs map[string]bool`: checklist tracking which registries to query (default: all checked).
  - `focusedField int`: cursor index (0 = repo name, 1..N = registry checkboxes, N+1 = Search button).
- **Results State**:
  - `tags []MultiRegistryTag`: combined list of tag entries.
  - `registryFilter string`: active registry filter (empty = all registries).
  - `tagTable table.Model`: table display showing columns: `Registry`, `Tag`, `Updated`, `C`, `H`, `M`, `L`.
- Implement navigation key handlers:
  - `Up`/`Down`/`j`/`k` to navigate inputs, checklist checkboxes, and search button.
  - `Space` to toggle checkbox selection status.
  - `r` to cycle through the searched registries in the tags result table.
- Implement view rendering:
  - Form layout with checkbox indicators (`[x]` / `[ ]`) and submit button.
  - Results table with the new `Registry` column.

#### [MODIFY] [model.go](file:///c:/Users/anthoni/projects/gitlab/devdesk/internal/ui/oci_resources/model.go)
- Add new message definitions:
  - `MultiRegistryTagsLoadedMsg`: sent as each selected registry finishes querying tags.
  - `MultiRegistryTagsMetaMsg`: sent as tag metadata (like Docker Hub updated times) finishes loading.
- Delete unused single-registry message definitions if any.

#### [MODIFY] [commands.go](file:///c:/Users/anthoni/projects/gitlab/devdesk/internal/ui/oci_resources/commands.go)
- Add background command functions to perform OCI registry tag queries:
  - `searchRegistryTagsCmd`: calls `/v2/{repo}/tags/list` on a registry URL using stored credentials if `AuthEnabled` is true.
  - `loadMultiRegistryTagsMetaCmd`: fetches background metadata (like last updated timestamps) for registries that support it.
- Remove or deprecate single-registry fetch command functions.

#### [MODIFY] [update.go](file:///c:/Users/anthoni/projects/gitlab/devdesk/internal/ui/oci_resources/update.go)
- **Registries Tab**:
  - Remove key `enter` mapping to `openRegistryBrowser` from `handleRegistriesKeyMsg`.
- **Images Tab**:
  - Map key `b` in `handleImagesKeyMsg` to call `openMultiRegistryBrowser`.
- **Coordinator Logic**:
  - Update `openMultiRegistryBrowser` to initialize `RegistryBrowser` with all configured registries.
  - Update tag loading handlers to process concurrent `MultiRegistryTagsLoadedMsg` results.
  - Handle image pulling (`p`) and scanning (`ctrl+s`) actions using the registry URL selected on the target table row.

#### [MODIFY] [view.go](file:///c:/Users/anthoni/projects/gitlab/devdesk/internal/ui/oci_resources/view.go)
- Update Help Content (`GetHelpContent()`) and Shortcuts (`GetShortcuts()`):
  - Remove "Browse Registry" from the Registries tab help and add the new "Browse/Search Registry" shortcut (`b`) to the Images tab.
  - Update results table shortcuts to document key `r` for cycling registry filters.
- Update view title mapping:
  - If the registry browser is active in tag state, display active query and active registry filter (e.g. `OCI Resources > Search: nginx (Filter: docker.io)`).

---

## Verification Plan

### Automated Tests
- Run `go test ./internal/oci/...` and verify all tests pass.
- Verify building compiles successfully via `go build`.

### Manual Verification
1. Launch DevDesk (`go run main.go`).
2. Go to the `OCI Resources` tab.
3. Select the `Registries` tab and verify pressing `Enter` has no effect.
4. Go to the `Images` tab.
5. Press `b` to open the multi-registry search view.
6. Verify the search form displays with configured registries checked by default.
7. Enter a search term (e.g. `nginx`) and press Enter to search.
8. Verify that results display with a `Registry` column.
9. Press `/` and type to filter tags by name.
10. Press `r` to cycle the registry filter, verifying that the list updates to show only tags from the selected registry.
11. Press `p` to pull a selected tag, or `ctrl+s` to scan.
12. Press `esc` to go back to the search form, and `esc` again to exit back to the Images tab.
