package browser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommand(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "explorer.exe")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	const url = "https://example.com"
	tests := []struct {
		name   string
		goos   string
		distro string
		want   string
	}{
		{"linux under WSL", "linux", "archlinux", fake},
		{"linux outside WSL", "linux", "", "xdg-open"},
		{"darwin", "darwin", "archlinux", "open"},
		{"windows", "windows", "", "cmd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WSL_DISTRO_NAME", tt.distro)
			cmd := command(tt.goos, url)
			if cmd.Args[0] != tt.want && cmd.Path != tt.want {
				t.Errorf("command = %v (path %q), want %q", cmd.Args, cmd.Path, tt.want)
			}
			if got := cmd.Args[len(cmd.Args)-1]; got != url {
				t.Errorf("last arg = %q, want the URL", got)
			}
		})
	}
}
