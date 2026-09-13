package setup

import (
	"strings"

	"github.com/anthnel/devdesk/internal/config"
)

// buildConfig turns the wizard's answers into a full context configuration,
// starting from config.Default() the same way config.CreateContext does. It
// is a pure function on purpose: everything it needs is already collected on
// Model, so this is testable without driving the Bubble Tea loop.
//
// The forge token is deliberately absent here: it never lives in the config
// struct (internal/config/config.go's ForgeConfig doc comment), and
// validateTokenCmd already persists it via the resolved credentials.Storage.
func buildConfig(m Model) *config.Config {
	cfg := config.Default()

	cfg.App.SecretBackend = secretBackendOptions[m.secretBackendIdx]
	cfg.App.ContainerEngine = config.ContainerEngines()[m.containerEngineIdx]
	cfg.App.Theme = m.themeOptions[m.themeIdx]

	cfg.Forge.Type = config.ForgeTypes()[m.forgeTypeIdx]
	cfg.Forge.URL = strings.TrimSpace(m.forgeURLInput.Value())
	cfg.Forge.DefaultParentGroup = strings.TrimSpace(m.forgeNamespaceInput.Value())
	cfg.Forge.DefaultVisibility = m.visibilityValue
	cfg.Forge.CloneMethod = cloneMethodOptions[m.cloneMethodIdx]

	// Matches config.CreateContext: a freshly wizard-built context starts with
	// no monitors to manage.
	cfg.Status.Components = []config.ComponentConfig{}

	return cfg
}
