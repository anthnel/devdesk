package workspaces

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLinuxBrowserCommandUsesExplorerUnderWSL pins the WSL branch: xdg-open
// blocks on a dead X server there, so the URL goes to explorer.exe.
func TestLinuxBrowserCommandUsesExplorerUnderWSL(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "explorer.exe")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	tests := []struct {
		name   string
		distro string
		want   string
	}{
		{"wsl", "archlinux", fake},
		{"not wsl", "", "xdg-open"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WSL_DISTRO_NAME", tt.distro)
			cmd := linuxBrowserCommand("https://example.com")
			if cmd.Args[0] != tt.want && cmd.Path != tt.want {
				t.Errorf("command = %v (path %q), want %q", cmd.Args, cmd.Path, tt.want)
			}
			if got := cmd.Args[len(cmd.Args)-1]; got != "https://example.com" {
				t.Errorf("last arg = %q, want the URL", got)
			}
		})
	}
}
