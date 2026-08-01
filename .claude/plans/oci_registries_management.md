# Add Registries Management to OCI View

Add a new tab in the OCI view to manage Docker/OCI registries, handle authentication, and define aliases for shorter image names.

## Proposed Changes

### Configuration

#### [MODIFY] [config.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/config/config.go)
- Define `RegistryItem` struct:
  ```go
  type RegistryItem struct {
      URL         string `yaml:"url"`
      Username    string `yaml:"username"`
      // Password is NOT stored in YAML. It is stored via the system credential helper.
      Alias       string `yaml:"alias"`
      AuthEnabled bool `yaml:"auth_enabled"`
  }
  ```
- Change `RegistryConfig.URL/Username/Password` to `Registries []RegistryItem`.
- Implement migration in `Load()` or `applyDefaults()` to move old single-registry config into the new `Registries` list if present.

---

### Credentials & Docker Integration

#### [MODIFY] [client.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/docker/client.go)
- Add `RegistryLogin(url, user, pass string) error` using `docker login <url> -u <user> -p <pass>`.
- Add a helper `ApplyAliases(imageName string, registries []config.RegistryItem) string` to replace registry prefixes with aliases using the format `<alias>/rest/of/image:tag`.
    - Example: `gitlab.com/my-group/my-image:1.0` -> `gl/my-image:1.0` if `gl` stays for `gitlab.com/my-group`.

#### [MODIFY] [credentials/helper.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/credentials/helper.go) or [credentials/git_credential.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/credentials/git_credential.go)
- Ensure the credential storage can handle `docker://` or generic registry URLs (currently seems to assume `https://` for GitLab).
- Use `git-credential` or system keyring to store registry passwords.

---

### OCI Resources UI

#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_resources/model.go)
- Add `tabRegistries` to `ociTab`.
- Add `registryTable table.Model` and `loadingRegs bool`.
- Add state for a new registry creation/edit form.

#### [MODIFY] [view.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_resources/view.go)
- Update `renderTabBar` to include the "Registries" tab.
- Implement `renderRegistriesView` (Table with columns: Registry URL, Username, Alias, Auth Status).
- Add shortcuts for the Registries tab: `n` (new), `e` (edit), `ctrl+d` (remove), `l` (login).

#### [MODIFY] [update.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_resources/update.go)
- Handle tab switching for `tabRegistries`.
- Implement `updateRegistryTable` to populate rows from config.
- Update `updateImageTable` to call `ApplyAliases` for each image name.
- Handle Registry CRUD messages and "login" requests.

#### [NEW] [registry_form.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_resources/registry_form.go)
- Create a form component for adding and editing registries (URL, Username, Password, Alias, Auth toggle).

---

## Verification Plan

### Automated Tests
- **Config Migration**: Add a test in `config_test.go` to verify that old `RegistryConfig` fields are correctly migrated to the first item in the `Registries` list.
- **Aliasing Logic**: Add a unit test for `ApplyAliases` in a new `internal/docker/alias_test.go` or within `client_test.go` if it exists.

### Manual Verification
1. **Manage Registries**:
   - Open OCI view, switch to "Registries" tab.
   - Add a new registry with an alias (e.g. `gitlab.com` -> `gl`).
   - Edit the registry.
   - Delete a registry.
2. **Verify Aliasing**:
   - Go to "Images" tab.
   - Verify that images with the `gitlab.com` prefix now show the `gl/image:tag` alias.
3. **Verify Auth & Persistence**:
   - Select a registry, press `l`.
   - Verify that `docker login` is successful.
   - Close and relaunch the application.
   - Verify that the registry remains "logged in" (the TUI should show a status, and Docker commands should work without re-authenticating).
   - *Note: `docker login` persists credentials in `~/.docker/config.json` and the system keyring, so sessions survive application restarts.*
