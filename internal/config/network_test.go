package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The `docker:` section became `network:`, and the one key it held has to come
// across.
//
// This is the migration that could not be a comment: Load unmarshals without
// KnownFields, so an un-migrated `docker:` block is dropped in silence — the
// image would revert to nicolaka/netshoot with nothing on screen saying it had
// moved, and a user pointing at their own mirror would find their traces
// pulling from Docker Hub.
func TestANetworkToolImageSurvivesTheRename(t *testing.T) {
	cfg := writeAndLoad(t, "docker:\n  network_tool_image: mirror.local/netshoot:1.2\n")

	if cfg.Network.ToolImage != "mirror.local/netshoot:1.2" {
		t.Errorf("Network.ToolImage = %q, want the image carried over from docker:", cfg.Network.ToolImage)
	}
	if cfg.Docker.NetworkToolImage != "" {
		t.Errorf("docker.network_tool_image = %q; it must be cleared so the key leaves the file on the next save",
			cfg.Docker.NetworkToolImage)
	}
}

// A file already carrying both keeps what `network:` says. The old key is a
// migration source, not a second writer: letting it win would make the setting
// the configuration view edits the one that loses.
func TestTheNewKeyWinsOverTheOldOne(t *testing.T) {
	cfg := writeAndLoad(t,
		"docker:\n  network_tool_image: old.example.com/netshoot\n"+
			"network:\n  tool_image: new.example.com/netshoot\n")

	if cfg.Network.ToolImage != "new.example.com/netshoot" {
		t.Errorf("Network.ToolImage = %q, want network: to win", cfg.Network.ToolImage)
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
		{"traceroute_max_hops", cfg.Network.TracerouteMaxHops, DefaultTracerouteMaxHops},
		{"ports_refresh_interval", cfg.Network.PortsRefreshInterval, DefaultPortsRefreshInterval},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
	if cfg.Network.ToolImage != DefaultNetworkToolImage {
		t.Errorf("tool_image = %q, want %q", cfg.Network.ToolImage, DefaultNetworkToolImage)
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
