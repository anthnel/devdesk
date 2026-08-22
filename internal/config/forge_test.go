package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeContextFile drops a raw config into a temporary context and returns what
// LoadContext makes of it.
func loadRaw(t *testing.T, yaml string) *Config {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	dir := filepath.Join(home, ".devdesk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config-work.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := LoadContext("work")
	if err != nil {
		t.Fatalf("LoadContext() error = %v", err)
	}
	return cfg
}

// TestAGitLabSectionSurvivesTheRename is the whole point of the migration.
// yaml.Unmarshal is not strict here, so without it the block is dropped in
// silence and the context points at no host at all — the user's own settings
// gone, with nothing on screen saying so.
func TestAGitLabSectionSurvivesTheRename(t *testing.T) {
	cfg := loadRaw(t, `
gitlab:
  url: https://gitlab.acme.test
  default_parent_group: platform
  default_visibility: internal
  clone_method: ssh
  pull:
    parallel_jobs: 7
    include_archived: true
`)

	if cfg.Forge.URL != "https://gitlab.acme.test" {
		t.Errorf("URL = %q, want the legacy one", cfg.Forge.URL)
	}
	if cfg.Forge.DefaultParentGroup != "platform" {
		t.Errorf("DefaultParentGroup = %q", cfg.Forge.DefaultParentGroup)
	}
	if cfg.Forge.DefaultVisibility != "internal" {
		t.Errorf("DefaultVisibility = %q, want the legacy one rather than the default", cfg.Forge.DefaultVisibility)
	}
	if cfg.Forge.CloneMethod != "ssh" {
		t.Errorf("CloneMethod = %q, want the legacy one rather than the default", cfg.Forge.CloneMethod)
	}
	if cfg.Forge.Pull.ParallelJobs != 7 {
		t.Errorf("ParallelJobs = %d, want the legacy 7 rather than the default 4", cfg.Forge.Pull.ParallelJobs)
	}
	if !cfg.Forge.Pull.IncludeArchived {
		t.Error("IncludeArchived was lost — it is a bool, so no per-field guard can carry it")
	}
}

// A file that predates `type:` can only mean GitLab.
func TestALegacySectionIsAGitLabForge(t *testing.T) {
	cfg := loadRaw(t, "gitlab:\n  url: https://gitlab.acme.test\n")
	if cfg.Forge.Type != ForgeGitLab {
		t.Errorf("Type = %q, want %q", cfg.Forge.Type, ForgeGitLab)
	}
}

// TestTheLegacySectionLeavesTheFileOnTheNextSave — the key is cleared once
// read, so it does not sit there contradicting the block that replaced it. The
// precedent is RegistryItem.AuthEnabled and `docker.network_tool_image`.
func TestTheLegacySectionLeavesTheFileOnTheNextSave(t *testing.T) {
	cfg := loadRaw(t, "gitlab:\n  url: https://gitlab.acme.test\n")
	if cfg.GitLab != (GitLabConfig{}) {
		t.Errorf("the legacy block survived the load: %+v", cfg.GitLab)
	}

	if err := SaveContext(cfg, "work"); err != nil {
		t.Fatalf("SaveContext() error = %v", err)
	}
	home, _ := os.UserHomeDir()
	raw, err := os.ReadFile(filepath.Join(home, ".devdesk", "config-work.yaml"))
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if got := string(raw); contains(got, "\ngitlab:") {
		t.Errorf("the saved file still carries a gitlab: block:\n%s", got)
	}
	if !contains(string(raw), "https://gitlab.acme.test") {
		t.Errorf("the saved file lost the URL:\n%s", raw)
	}
}

// TestAForgeSectionWinsOverALegacyOne — a hand-edited file can carry both, and
// the new values were written later. Overwriting them with the old ones would
// undo the edit that created the situation.
func TestAForgeSectionWinsOverALegacyOne(t *testing.T) {
	cfg := loadRaw(t, `
forge:
  type: github
  url: https://github.com
gitlab:
  url: https://gitlab.acme.test
  default_parent_group: platform
`)

	if cfg.Forge.URL != "https://github.com" {
		t.Errorf("URL = %q, want the new block's", cfg.Forge.URL)
	}
	if cfg.Forge.Type != ForgeGitHub {
		t.Errorf("Type = %q, want it kept", cfg.Forge.Type)
	}
	// What the new block says nothing about is still worth carrying over rather
	// than losing.
	if cfg.Forge.DefaultParentGroup != "platform" {
		t.Errorf("DefaultParentGroup = %q, want the legacy value the new block is silent on", cfg.Forge.DefaultParentGroup)
	}
}

// A file with neither block gets the defaults, and `type` is one of them.
func TestAnEmptyConfigIsAGitLabForge(t *testing.T) {
	cfg := loadRaw(t, "app:\n  theme: default\n")
	if cfg.Forge.Type != ForgeGitLab {
		t.Errorf("Type = %q, want %q", cfg.Forge.Type, ForgeGitLab)
	}
	if cfg.Forge.CloneMethod != "https" || cfg.Forge.Pull.ParallelJobs != 4 {
		t.Errorf("defaults not applied: %+v", cfg.Forge)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
