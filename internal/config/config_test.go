package config

import (
	"os"
	"path/filepath"
	"testing"
)

// slash normalizes a path to forward slashes for cross-platform assertions.
func slash(p string) string { return filepath.ToSlash(p) }

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg == nil {
		t.Fatal("Default() returned nil")
	}

	// Test App defaults
	if cfg.App.Theme != "default" {
		t.Errorf("Expected theme 'default', got '%s'", cfg.App.Theme)
	}
	if cfg.App.DefaultView != "dashboard" {
		t.Errorf("Expected default view 'dashboard', got '%s'", cfg.App.DefaultView)
	}

	// Test Status defaults
	if cfg.Status.RefreshInterval != 10 {
		t.Errorf("Expected refresh interval 10, got %d", cfg.Status.RefreshInterval)
	}
	if cfg.Status.Timeout != 5 {
		t.Errorf("Expected timeout 5, got %d", cfg.Status.Timeout)
	}
	if !cfg.Status.AutoRefresh {
		t.Error("Expected AutoRefresh to be true")
	}

	// Test GitLab defaults
	if cfg.GitLab.DefaultVisibility != "private" {
		t.Errorf("Expected visibility 'private', got '%s'", cfg.GitLab.DefaultVisibility)
	}
	if cfg.GitLab.CloneMethod != "https" {
		t.Errorf("Expected clone method 'https', got '%s'", cfg.GitLab.CloneMethod)
	}
	if cfg.GitLab.Pull.ParallelJobs != 4 {
		t.Errorf("Expected parallel jobs 4, got %d", cfg.GitLab.Pull.ParallelJobs)
	}
	if cfg.GitLab.Pull.MaxDepth != 5 {
		t.Errorf("Expected max depth 5, got %d", cfg.GitLab.Pull.MaxDepth)
	}

	// Test Scan defaults
	if cfg.Scan.MaxCachedReports != 50 {
		t.Errorf("Expected max cached reports 50, got %d", cfg.Scan.MaxCachedReports)
	}
	if cfg.Scan.Timeout != 300 {
		t.Errorf("Expected timeout 300, got %d", cfg.Scan.Timeout)
	}
	if cfg.Scan.MaxConcurrentScans != 3 {
		t.Errorf("Expected max concurrent scans 3, got %d", cfg.Scan.MaxConcurrentScans)
	}
}

func TestConfigDir(t *testing.T) {
	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error: %v", err)
	}

	homeDir, _ := os.UserHomeDir()
	expected := filepath.Join(homeDir, ".devdesk")

	if slash(dir) != slash(expected) {
		t.Errorf("Expected config dir '%s', got '%s'", expected, dir)
	}
}

func TestEnsureConfigDir(t *testing.T) {
	tmpDir := setupTmpHome(t)

	err := EnsureConfigDir()
	if err != nil {
		t.Fatalf("EnsureConfigDir() error: %v", err)
	}

	// Verify directory was created
	configDir := filepath.Join(tmpDir, ".devdesk")
	info, err := os.Stat(configDir)
	if err != nil {
		t.Fatalf("Config directory not created: %v", err)
	}

	if !info.IsDir() {
		t.Error("Expected config path to be a directory")
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := setupTmpHome(t)

	// Create a test config
	testCfg := &Config{
		App: AppConfig{
			Theme:         "light",
			DefaultView:   "gitlab-auth",
			WorkspacesDir: "/test/workspaces",
		},
		Status: StatusConfig{
			RefreshInterval: 20,
			Timeout:         10,
			AutoRefresh:     false,
			Components: []ComponentConfig{
				{
					Name:   "test-component",
					Type:   "https",
					Target: "https://example.com",
				},
			},
		},
		GitLab: GitLabConfig{
			URL:               "https://gitlab.example.com",
			DefaultVisibility: "public",
			CloneMethod:       "ssh",
		},
	}

	// Save config
	err := Save(testCfg)
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify file was created
	configPath := filepath.Join(tmpDir, ".devdesk", "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("Config file was not created")
	}

	// Load config
	loadedCfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Verify loaded values match saved values
	if loadedCfg.App.Theme != "light" {
		t.Errorf("Expected theme 'light', got '%s'", loadedCfg.App.Theme)
	}
	if loadedCfg.App.DefaultView != "gitlab-auth" {
		t.Errorf("Expected default view 'gitlab-auth', got '%s'", loadedCfg.App.DefaultView)
	}
	if loadedCfg.Status.RefreshInterval != 20 {
		t.Errorf("Expected refresh interval 20, got %d", loadedCfg.Status.RefreshInterval)
	}
	if loadedCfg.Status.AutoRefresh {
		t.Error("Expected AutoRefresh to be false")
	}
	if len(loadedCfg.Status.Components) != 1 {
		t.Errorf("Expected 1 component, got %d", len(loadedCfg.Status.Components))
	}
	if loadedCfg.Status.Components[0].Name != "test-component" {
		t.Errorf("Expected component name 'test-component', got '%s'", loadedCfg.Status.Components[0].Name)
	}
	if loadedCfg.GitLab.URL != "https://gitlab.example.com" {
		t.Errorf("Expected GitLab URL 'https://gitlab.example.com', got '%s'", loadedCfg.GitLab.URL)
	}
}

func TestLoadNonExistent(t *testing.T) {
	setupTmpHome(t)

	// Load when config doesn't exist should return default
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error on non-existent config: %v", err)
	}

	if cfg == nil {
		t.Fatal("Load() returned nil for non-existent config")
	}

	// Should have default values
	if cfg.App.Theme != "default" {
		t.Errorf("Expected default theme 'default', got '%s'", cfg.App.Theme)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	tmpDir := setupTmpHome(t)

	// Create config directory
	configDir := filepath.Join(tmpDir, ".devdesk")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	// Write invalid YAML
	configPath := filepath.Join(configDir, "config.yaml")
	invalidYAML := "this is: not: valid: yaml:\n  - broken"
	if err := os.WriteFile(configPath, []byte(invalidYAML), 0600); err != nil {
		t.Fatalf("Failed to write invalid YAML: %v", err)
	}

	// Load should return error
	_, err := Load()
	if err == nil {
		t.Error("Expected error when loading invalid YAML, got nil")
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	setupTmpHome(t)

	// Create minimal config with missing fields
	minimalCfg := &Config{
		App: AppConfig{
			// Theme missing - should default to "dark"
		},
		Status: StatusConfig{
			// RefreshInterval missing - should default to 10
		},
	}

	// Save minimal config
	if err := Save(minimalCfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Load and verify defaults were applied
	loadedCfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if loadedCfg.App.Theme != "default" {
		t.Errorf("Expected default theme 'default', got '%s'", loadedCfg.App.Theme)
	}
	if loadedCfg.Status.RefreshInterval != 10 {
		t.Errorf("Expected default refresh interval 10, got %d", loadedCfg.Status.RefreshInterval)
	}
	if loadedCfg.GitLab.DefaultVisibility != "private" {
		t.Errorf("Expected default visibility 'private', got '%s'", loadedCfg.GitLab.DefaultVisibility)
	}
	if loadedCfg.GitLab.Pull.ParallelJobs != 4 {
		t.Errorf("Expected default parallel jobs 4, got %d", loadedCfg.GitLab.Pull.ParallelJobs)
	}
	// Backward compat: a config with no scan option booleans (all false) must get vuln+secret enabled
	if !loadedCfg.Scan.EnableVuln {
		t.Error("Expected EnableVuln=true for legacy config with no scan options set")
	}
	if !loadedCfg.Scan.EnableSecret {
		t.Error("Expected EnableSecret=true for legacy config with no scan options set")
	}
}

// "dark" was a third name for the built-in theme: LoadTheme accepted "", "dark"
// and "default" alike, but ListThemes only ever offered "default". A config
// saying "dark" therefore named a theme no picker could show — invisible until
// the configuration view bound a cycle field straight to the setting.
func TestTheLegacyDarkThemeNameIsNormalised(t *testing.T) {
	cfg := &Config{App: AppConfig{Theme: "dark"}}

	if err := applyDefaults(cfg); err != nil {
		t.Fatalf("applyDefaults: %v", err)
	}

	if cfg.App.Theme != "default" {
		t.Errorf("Theme = %q, want it normalised to \"default\"", cfg.App.Theme)
	}
}

// A theme the user actually installed is left alone.
func TestANamedThemeSurvivesNormalisation(t *testing.T) {
	cfg := &Config{App: AppConfig{Theme: "mocha"}}

	if err := applyDefaults(cfg); err != nil {
		t.Fatalf("applyDefaults: %v", err)
	}

	if cfg.App.Theme != "mocha" {
		t.Errorf("Theme = %q, want it untouched", cfg.App.Theme)
	}
}
