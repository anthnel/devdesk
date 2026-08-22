package gitlab

import (
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// TestTheForgeVocabularyMatchesTheConfig keeps `forge.type` and the backends'
// Shape().Name in step. The two are stated in different packages on purpose —
// config must not import a backend, and a backend must not be the authority on
// what a config file may say — so the only thing that can hold them together is
// a test. The precedent is TestTheProviderVocabularyMatchesTheConfig, which the
// registry `provider` names have for the same reason (§3.8).
//
// A config value no backend answers to resolves to nothing at load, which is a
// failure mode worth checking rather than commenting.
func TestTheForgeVocabularyMatchesTheConfig(t *testing.T) {
	shape := NewWithClient(nil, "https://gitlab.com").Shape()

	if shape.Name != config.ForgeGitLab {
		t.Errorf("this backend calls itself %q where config says %q", shape.Name, config.ForgeGitLab)
	}

	// Every value the configuration view will offer has to be answerable. The
	// GitHub one is not implemented yet (§3.6 step 7), so it is named here
	// rather than resolved — what this checks is that the *list* is the one this
	// package's vocabulary comes from.
	known := map[string]bool{config.ForgeGitLab: true, config.ForgeGitHub: true}
	for _, forgeType := range config.ForgeTypes() {
		if !known[forgeType] {
			t.Errorf("config offers %q, which is not in the declared vocabulary", forgeType)
		}
	}
	if len(config.ForgeTypes()) != len(known) {
		t.Errorf("config offers %d forges, the vocabulary knows %d", len(config.ForgeTypes()), len(known))
	}
}

// TestTheDefaultForgeIsTheOneThatExists — an empty `type:` means GitLab, which
// is what every file written before the key existed meant, and the only backend
// there is.
func TestTheDefaultForgeIsTheOneThatExists(t *testing.T) {
	if config.ForgeTypes()[0] != config.ForgeGitLab {
		t.Errorf("the first forge offered is %q, want %q — it is the one a new context lands on",
			config.ForgeTypes()[0], config.ForgeGitLab)
	}
}
