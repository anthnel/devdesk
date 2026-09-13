package setup

import "testing"

func TestBuildConfigUsesTheWizardsAnswers(t *testing.T) {
	m := New()
	m.secretBackendIdx = 1   // keyring
	m.containerEngineIdx = 2 // podman
	m.themeIdx = 0
	m.forgeTypeIdx = 1 // github
	m.forgeURLInput.SetValue("https://git.example.com")
	m.forgeNamespaceInput.SetValue("my-org")
	m.visibilityValue = "public"
	m.cloneMethodIdx = 1 // ssh

	cfg := buildConfig(m)

	if cfg.App.SecretBackend != "keyring" {
		t.Errorf("SecretBackend = %q, want keyring", cfg.App.SecretBackend)
	}
	if cfg.App.ContainerEngine != "podman" {
		t.Errorf("ContainerEngine = %q, want podman", cfg.App.ContainerEngine)
	}
	if cfg.Forge.Type != "github" {
		t.Errorf("Forge.Type = %q, want github", cfg.Forge.Type)
	}
	if cfg.Forge.URL != "https://git.example.com" {
		t.Errorf("Forge.URL = %q, want https://git.example.com", cfg.Forge.URL)
	}
	if cfg.Forge.DefaultParentGroup != "my-org" {
		t.Errorf("Forge.DefaultParentGroup = %q, want my-org", cfg.Forge.DefaultParentGroup)
	}
	if cfg.Forge.DefaultVisibility != "public" {
		t.Errorf("Forge.DefaultVisibility = %q, want public", cfg.Forge.DefaultVisibility)
	}
	if cfg.Forge.CloneMethod != "ssh" {
		t.Errorf("Forge.CloneMethod = %q, want ssh", cfg.Forge.CloneMethod)
	}
	if len(cfg.Status.Components) != 0 {
		t.Errorf("Status.Components = %v, want empty (matches config.CreateContext)", cfg.Status.Components)
	}
}
