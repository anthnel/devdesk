package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
		{"proxy_port", cfg.Network.ProxyPort, DefaultProxyPort},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
		}
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

func TestTheProxyPortIsKeptWhenBindable(t *testing.T) {
	if got := writeAndLoad(t, "network:\n  proxy_port: 9000\n").Network.ProxyPort; got != 9000 {
		t.Errorf("proxy_port = %d, want the stated 9000", got)
	}
}

// A port the proxy cannot bind would make every named route unbindable, so an
// out-of-range value is replaced rather than carried into the listener.
func TestAProxyPortOutsideTheBoundsFallsBackToTheDefault(t *testing.T) {
	for name, port := range map[string]string{
		"privileged": "80",
		"reserved":   "1023",
		"too high":   "70000",
		"negative":   "-1",
	} {
		t.Run(name, func(t *testing.T) {
			got := writeAndLoad(t, "network:\n  proxy_port: "+port+"\n").Network.ProxyPort
			if got != DefaultProxyPort {
				t.Errorf("proxy_port %s loaded as %d, want the default %d", port, got, DefaultProxyPort)
			}
		})
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
