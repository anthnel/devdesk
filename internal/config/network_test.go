package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The image key has been renamed twice: `docker.network_tool_image` became
// `network.tool_image` when the section was renamed, and `network.tool_image`
// became `network.connectivity_image` when the route trace was removed and the
// OCI connectivity test was left as its only reader (§3.47). A config may sit
// at any point on that chain.
//
// This is the migration that could not be a comment: Load unmarshals without
// KnownFields, so an un-migrated block is dropped in silence — the image would
// revert to the default with nothing on screen saying it had moved, and a user
// pointing at their own mirror would find the probe pulling from Docker Hub.
func TestTheImageSurvivesBothRenames(t *testing.T) {
	for _, tt := range []struct{ name, yaml string }{
		{"the oldest spelling", "docker:\n  network_tool_image: mirror.local/probe:1.2\n"},
		{"the middle spelling", "network:\n  tool_image: mirror.local/probe:1.2\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := writeAndLoad(t, tt.yaml)

			if cfg.Network.ConnectivityImage != "mirror.local/probe:1.2" {
				t.Errorf("ConnectivityImage = %q, want the image carried over", cfg.Network.ConnectivityImage)
			}
			if cfg.Docker.NetworkToolImage != "" || cfg.Network.ToolImage != "" {
				t.Error("a retired key was kept; it must be cleared so it leaves the file on the next save")
			}
		})
	}
}

// A file carrying several spellings keeps the newest. An old key is a migration
// source, not a second writer: letting it win would make the setting the
// configuration view edits the one that loses.
func TestTheNewestKeyWins(t *testing.T) {
	cfg := writeAndLoad(t,
		"docker:\n  network_tool_image: oldest.example.com/probe\n"+
			"network:\n  tool_image: middle.example.com/probe\n"+
			"  connectivity_image: newest.example.com/probe\n")

	if cfg.Network.ConnectivityImage != "newest.example.com/probe" {
		t.Errorf("ConnectivityImage = %q, want connectivity_image to win", cfg.Network.ConnectivityImage)
	}
}

// Every network setting has a default, so a config written before they existed
// behaves exactly as it did.
func TestTheNetworkSettingsDefault(t *testing.T) {
	cfg := writeAndLoad(t, "app:\n  theme: default\n")

	tests := []struct {
		name string
		got  int
		want int
	}{
		{"check_timeout", cfg.Network.CheckTimeout, DefaultCheckTimeout},
		{"ping_count", cfg.Network.PingCount, DefaultPingCount},
		{"cert_expiry_warn_days", cfg.Network.CertExpiryWarnDays, DefaultCertExpiryWarnDays},
		{"ports_refresh_interval", cfg.Network.PortsRefreshInterval, DefaultPortsRefreshInterval},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
	if cfg.Network.ConnectivityImage != DefaultConnectivityImage {
		t.Errorf("connectivity_image = %q, want %q", cfg.Network.ConnectivityImage, DefaultConnectivityImage)
	}
}

// A value the file states is never replaced by its default.
func TestAStatedNetworkSettingIsKept(t *testing.T) {
	cfg := writeAndLoad(t, "network:\n  check_timeout: 30\n  ping_count: 10\n  ports_refresh_interval: 5\n")

	if cfg.Network.CheckTimeout != 30 {
		t.Errorf("check_timeout = %d, want 30", cfg.Network.CheckTimeout)
	}
	if cfg.Network.PingCount != 10 {
		t.Errorf("ping_count = %d, want 10", cfg.Network.PingCount)
	}
	if cfg.Network.PortsRefreshInterval != 5 {
		t.Errorf("ports_refresh_interval = %d, want 5", cfg.Network.PortsRefreshInterval)
	}
}

func writeAndLoad(t *testing.T, yaml string) *Config {
	t.Helper()

	tmpDir := setupTmpHome(t)
	configDir := filepath.Join(tmpDir, ".devdesk")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("creating the config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yaml), 0600); err != nil {
		t.Fatalf("writing the config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}
