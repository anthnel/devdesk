package setup

import (
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

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

// DEVDESK-663 (S-01) criterion 5, DEVDESK-664 (S-02) criterion 3: the entry of
// a context the wizard builds is the one :context new writes, and it is there
// exactly once. buildConfig is S-02's scope, so the test is written now (it
// compiles against the shared constructor) and skipped until S-02 injects the
// entry; the S-02 task removes the Skip.
func TestBuildConfigAndCreateContextWriteTheSameHubEntry(t *testing.T) {
	t.Skip("S-02 (DEVDESK-664): buildConfig does not inject config.DefaultHubRegistry() yet")

	isolateHome(t)
	if err := config.CreateContext("fresh"); err != nil {
		t.Fatal(err)
	}
	created, err := config.LoadContext("fresh")
	if err != nil {
		t.Fatal(err)
	}
	built := buildConfig(New()).Registry.Registries

	if len(built) != 1 || built[0] != config.DefaultHubRegistry() {
		t.Fatalf("buildConfig registries = %+v, want exactly [%+v]", built, config.DefaultHubRegistry())
	}
	if got := created.Registry.Registries; len(got) != 1 || got[0] != built[0] {
		t.Errorf("CreateContext registries = %+v, buildConfig = %+v: the two paths diverge", got, built)
	}
	if got := config.Default().Registry.Registries; len(got) != 0 {
		t.Errorf("config.Default() registries = %+v, want none", got)
	}
}
