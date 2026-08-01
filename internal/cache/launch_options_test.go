package cache

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func newTestLaunchOptionsCache(t *testing.T) *LaunchOptionsCache {
	t.Helper()
	dir := t.TempDir()
	c := &LaunchOptionsCache{
		path:    filepath.Join(dir, "launch-options.json"),
		entries: make(map[string]LaunchOptionsEntry),
	}
	return c
}

func TestLaunchOptionsCache_GetUnknownKey(t *testing.T) {
	c := newTestLaunchOptionsCache(t)
	if got := c.Get("nginx:latest"); got != nil {
		t.Errorf("expected nil for unknown key, got %+v", got)
	}
}

func TestLaunchOptionsCache_SetAndGet(t *testing.T) {
	c := newTestLaunchOptionsCache(t)
	entry := LaunchOptionsEntry{
		Entrypoint:  "/bin/sh",
		ExtraPorts:  "8080:8080",
		Env:         "APP_ENV=prod",
		Volumes:     "data:/data",
		User:        "1000:1000",
		Network:     "bridge",
		Remove:      true,
		Detach:      false,
		Interactive: true,
		PortMappings: map[string]PortMappingEntry{
			"80/tcp": {HostPort: "80", Enabled: true},
		},
	}

	if err := c.Set("nginx:latest", entry); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got := c.Get("nginx:latest")
	if got == nil {
		t.Fatal("expected entry, got nil")
	}
	if got.Entrypoint != entry.Entrypoint {
		t.Errorf("Entrypoint: got %q, want %q", got.Entrypoint, entry.Entrypoint)
	}
	if got.Network != entry.Network {
		t.Errorf("Network: got %q, want %q", got.Network, entry.Network)
	}
	if got.Interactive != entry.Interactive {
		t.Errorf("Interactive: got %v, want %v", got.Interactive, entry.Interactive)
	}
	pm, ok := got.PortMappings["80/tcp"]
	if !ok {
		t.Fatal("expected port mapping for 80/tcp")
	}
	if pm.HostPort != "80" || !pm.Enabled {
		t.Errorf("port mapping: got %+v", pm)
	}
}

func TestLaunchOptionsCache_Persistence(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "launch-options.json")

	c1 := &LaunchOptionsCache{path: cachePath, entries: make(map[string]LaunchOptionsEntry)}
	entry := LaunchOptionsEntry{
		Entrypoint: "/bin/bash",
		Detach:     true,
	}
	if err := c1.Set("alpine:3.18", entry); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// A new cache instance loading the same file must return the persisted entry.
	c2 := &LaunchOptionsCache{path: cachePath, entries: make(map[string]LaunchOptionsEntry)}
	c2.load()

	got := c2.Get("alpine:3.18")
	if got == nil {
		t.Fatal("expected persisted entry, got nil")
	}
	if got.Entrypoint != "/bin/bash" {
		t.Errorf("Entrypoint: got %q, want %q", got.Entrypoint, "/bin/bash")
	}
}

func TestLaunchOptionsCache_Overwrite(t *testing.T) {
	c := newTestLaunchOptionsCache(t)

	_ = c.Set("redis:7", LaunchOptionsEntry{Entrypoint: "/first"})
	_ = c.Set("redis:7", LaunchOptionsEntry{Entrypoint: "/second"})

	got := c.Get("redis:7")
	if got == nil {
		t.Fatal("expected entry, got nil")
	}
	if got.Entrypoint != "/second" {
		t.Errorf("expected /second after overwrite, got %q", got.Entrypoint)
	}
}

func TestLaunchOptionsCache_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions not applicable on Windows")
	}
	c := newTestLaunchOptionsCache(t)
	if err := c.Set("test:latest", LaunchOptionsEntry{Env: "X=1"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	info, err := os.Stat(c.path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected file permissions 0600, got %o", perm)
	}
}
