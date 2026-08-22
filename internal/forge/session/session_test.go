package session

import (
	"context"
	"net/http"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// TestEveryConfiguredForgeGetsABackend is the guard against a half-added
// platform: a `type:` the configuration view offers that this switch does not
// answer would silently open a GitLab session against a GitHub host, and the
// failure would surface as an authentication error naming the wrong thing.
func TestEveryConfiguredForgeGetsABackend(t *testing.T) {
	seen := map[string]bool{}
	for _, forgeType := range config.ForgeTypes() {
		backend, err := Backend(forgeType, "https://example.test", "token")
		if err != nil {
			t.Errorf("Backend(%q) error = %v", forgeType, err)
			continue
		}
		name := backend.Shape().Name
		if name != forgeType {
			t.Errorf("Backend(%q) built a %q backend", forgeType, name)
		}
		seen[name] = true
	}
	if len(seen) != len(config.ForgeTypes()) {
		t.Errorf("%d forges collapsed to %d backends", len(config.ForgeTypes()), len(seen))
	}
}

// TestAnUnknownForgeFallsBackToGitLab — applyDefaults has already normalised
// the value by the time a context is loaded, and falling through to the one
// backend that has always existed beats an error nothing can act on.
func TestAnUnknownForgeFallsBackToGitLab(t *testing.T) {
	backend, err := Backend("bitbucket", "https://example.test", "token")
	if err != nil {
		t.Fatalf("Backend() error = %v", err)
	}
	if got := backend.Shape().Name; got != config.ForgeGitLab {
		t.Errorf("an unknown forge built a %q backend, want the GitLab fallback", got)
	}
}

// TestASessionOpensAgainstTheDeclaredPlatform — the same host answering the
// same way yields two different backends, which is the whole point of the
// switch. The fake answers both SDKs' "who am I" call.
func TestASessionOpensAgainstTheDeclaredPlatform(t *testing.T) {
	fake := newFakeForge(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":9,"username":"ada","login":"ada","name":"Ada"}`))
	})

	for _, forgeType := range config.ForgeTypes() {
		result, err := NewAuth(nil).AuthenticateOnly(context.Background(), forgeType, fake.server.URL, "token")
		if err != nil {
			t.Errorf("AuthenticateOnly(%q) error = %v", forgeType, err)
			continue
		}
		if got := result.Forge.Shape().Name; got != forgeType {
			t.Errorf("a %q context opened a %q session", forgeType, got)
		}
		if result.User.Username != "ada" {
			t.Errorf("%q: user = %+v", forgeType, result.User)
		}
	}
}
