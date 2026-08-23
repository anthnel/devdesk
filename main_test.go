package main

import (
	"os"
	"path/filepath"
	"testing"
)

// `mcp.enabled` is false by default and that is the whole safety story: turning
// it on is the moment the user decides an agent may read this context. A config
// written before the key existed must therefore refuse, and the zero value of a
// bool is what makes that true without a migration.
func TestAContextThatHasNotEnabledTheServerIsRefused(t *testing.T) {
	fakeHome(t, "app:\n  theme: default\n")

	if code := runMCP(nil); code != 1 {
		t.Errorf("runMCP returned %d for a context with mcp.enabled unset, want 1", code)
	}
}

func TestAContextThatEnabledTheServerIsServed(t *testing.T) {
	fakeHome(t, "app:\n  theme: default\nmcp:\n  enabled: true\n")

	// stdin is closed under `go test`, so the transport reaches EOF at once —
	// which is the ordinary end of a stdio session, not a failure.
	if code := runMCP(nil); code != 0 {
		t.Errorf("runMCP returned %d for an enabled context, want 0", code)
	}
}

// An allow-list naming a tool that does not exist is refused rather than
// ignored: silently exposing less than was asked for is the failure nobody
// notices.
func TestAnUnknownToolInTheAllowListIsRefused(t *testing.T) {
	fakeHome(t, "app:\n  theme: default\nmcp:\n  enabled: true\n  expose:\n    - scan_everything\n")

	if code := runMCP(nil); code != 1 {
		t.Errorf("runMCP returned %d for an allow-list naming no tool, want 1", code)
	}
}

// fakeHome points os.UserHomeDir at a temp directory holding one config, so no
// test reads or writes the developer's own ~/.devdesk — which every worktree
// shares.
func fakeHome(t *testing.T, configYAML string) {
	t.Helper()

	home := t.TempDir()
	dir := filepath.Join(home, ".devdesk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create fake .devdesk: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write fake config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".current-context"), []byte("default"), 0o600); err != nil {
		t.Fatalf("write fake current-context: %v", err)
	}

	// USERPROFILE on Windows, HOME elsewhere — CI runs on ubuntu-latest and the
	// development machine on Windows, so both have to be set.
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
}
