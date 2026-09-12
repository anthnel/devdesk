package engine

import (
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// internal/config must not import a backend, and a backend must not be the
// authority on what a config file may say — so a test is the only thing that
// can hold the two spellings together. Same arrangement as
// TestTheForgeVocabularyMatchesTheBackends (§3.6) and the registry `provider`
// names (§3.8).
//
// The failure this catches: a config value that no shape answers to resolves
// to docker at load, silently, and the setting reads as ignored.
func TestEveryConfiguredEngineHasAShape(t *testing.T) {
	declared := Names()

	for _, name := range config.ContainerEngines() {
		if name == config.EngineAuto {
			// auto is a resolution strategy, not an engine; Resolve turns it
			// into one of the declared names.
			continue
		}
		if !slices.Contains(declared, name) {
			t.Errorf("config offers %q but internal/engine declares no shape for it", name)
		}
	}

	for _, name := range declared {
		if !slices.Contains(config.ContainerEngines(), name) {
			t.Errorf("internal/engine declares %q but the configuration never offers it", name)
		}
	}
}

// The constants are spelled twice on purpose; this is the only thing that
// stops one of them drifting.
func TestTheEngineNamesAgreeWithTheConfigConstants(t *testing.T) {
	if config.EngineDocker != Docker {
		t.Errorf("config.EngineDocker = %q, engine.Docker = %q", config.EngineDocker, Docker)
	}
	if config.EnginePodman != Podman {
		t.Errorf("config.EnginePodman = %q, engine.Podman = %q", config.EnginePodman, Podman)
	}
	if config.EngineAuto != Auto {
		t.Errorf("config.EngineAuto = %q, engine.Auto = %q", config.EngineAuto, Auto)
	}
}

// The default written on load has to be one of the values the configuration
// view cycles, or the first arrow press jumps somewhere arbitrary. This is the
// same invariant TestEveryCycleFieldDefaultsToOneOfItsOptions checks from the
// view's side; stated here it fails in the package that owns the default.
func TestTheDefaultEngineIsOneOfTheCycledValues(t *testing.T) {
	cfg := config.Default()
	if !slices.Contains(config.ContainerEngines(), cfg.App.ContainerEngine) {
		t.Errorf("Default().App.ContainerEngine = %q, which is not in %v",
			cfg.App.ContainerEngine, config.ContainerEngines())
	}
}
