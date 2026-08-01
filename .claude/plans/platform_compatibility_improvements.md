# Platform Compatibility and Windows Test Improvements

Fix hardcoded Linux commands (`xdg-open`, `sh -c`) and shell fallbacks, and update test assertions to support Windows/macOS fully.

## Proposed Changes

### Centralized URL/Browser Utilities

#### [MODIFY] [model.go (dashboard)](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/ui/dashboard/model.go)
Replace Linux-only `xdg-open` browser invocation with a cross-platform command using `runtime.GOOS`.
```go
func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		_ = cmd.Start()
		return nil
	}
}
```

#### [MODIFY] [model.go (security)](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/ui/security/model.go)
Replace `xdg-open` in `handleDetailsOpenReference()` with the same `runtime.GOOS` switch.

---

### Terminal and Shell Detection

#### [MODIFY] [terminal.go](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/ui/terminal/terminal.go)
Update `Shell()` to return the correct shell on Windows.
```go
func Shell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if runtime.GOOS == "windows" {
		if comspec := os.Getenv("COMSPEC"); comspec != "" {
			return comspec
		}
		return "cmd.exe"
	}
	return "/bin/sh"
}
```

#### [MODIFY] [update.go (containers)](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/ui/containers/update.go)
In `openExternalPager()` and `inspectSelectedContainer()`, avoid using `sh -c` on Windows.
- On Windows: Run `cmd /c` or invoke `docker logs` / `docker inspect` directly, piping the output to a temporary file, then starting `notepad` or the configured pager on it.
- On other OS: keep the `sh -c` pipeline.

---

### Unit Test Fixes for Windows

#### [MODIFY] [launch_options_test.go](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/cache/launch_options_test.go)
#### [MODIFY] [storage_test.go](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/credentials/storage_test.go)
Skip Unix permission tests checking for exact `0600` permissions on Windows:
```go
if runtime.GOOS == "windows" {
    t.Skip("skipping file permission assertion on Windows")
}
```

#### [MODIFY] [config_test.go](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/config/config_test.go)
#### [MODIFY] [context_test.go](file:///C:/Users/anthoni/projects/gitlab/devdesk/internal/config/context_test.go)
- Normalize paths with `filepath.ToSlash()` before matching.
- Avoid mixing hardcoded Unix slashes in path assertions.
- Use `t.TempDir()` or correct config overrides to ensure tests do not write to or check the default global `~/.devdesk` directory.

---

## Verification Plan

### Automated Tests
- Run tests on the modified packages:
  `mise x -- go test ./internal/cache/...`
  `mise x -- go test ./internal/credentials/...`
  `mise x -- go test ./internal/config/...`

### Manual Verification
- Test URL opening shortcut `o` in `security` and dashboard shortcuts `m` and `i`.
- Verify shell opening in a container works on Windows.
