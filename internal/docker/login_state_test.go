package docker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// writeAuthFile points the engine's auth file at a temporary home and writes
// body there, for the engine the test pinned.
func writeAuthFile(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(home, "run"))
	path := engine.Current().AuthPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Every row is what a real `login` wrote, measured on 2026-09-25 against
// docker 29.7.2 and podman 5.7.0 (§3.68) — not what the format allows.
func TestRegistryLoginStateReadsWhereTheSecretIs(t *testing.T) {
	const host = "localhost:5055"
	tests := []struct {
		name   string
		engine string
		file   string
		helper string // what the helper's `get` answers
		want   LoginState
	}{
		{"docker, no helper", engine.Docker,
			`{"auths":{"localhost:5055":{"auth":"dGVzdGVyOnMzY3JldHB3"}}}`, "", LoginInline},
		{"docker, credsStore", engine.Docker,
			`{"auths":{"localhost:5055":{}},"credsStore":"desktop"}`, "", LoginHelper},
		{"docker, credHelpers", engine.Docker,
			`{"auths":{"localhost:5055":{}},"credHelpers":{"localhost:5055":"pass"}}`, "", LoginHelper},
		// Logged in before the helper was configured: docker ignores the
		// base64, but it is still on disk.
		{"docker, stale inline next to a store", engine.Docker,
			`{"auths":{"localhost:5055":{"auth":"dGVzdGVyOnMzY3JldHB3"}},"credsStore":"desktop"}`, "", LoginInline},
		{"docker, identity token", engine.Docker,
			`{"auths":{"localhost:5055":{"identitytoken":"tok"}}}`, "", LoginInline},
		{"docker, empty entry and no helper", engine.Docker,
			`{"auths":{"localhost:5055":{}}}`, "", LoginNone},
		{"docker, never logged in", engine.Docker,
			`{"auths":{}}`, "", LoginNone},
		{"podman, no helper", engine.Podman,
			`{"auths":{"localhost:5055":{"auth":"dGVzdGVyOnMzY3JldHB3"}}}`, "", LoginInline},
		// podman leaves no auths entry when a helper took the secret.
		{"podman, credHelpers, logged in", engine.Podman,
			`{"credHelpers":{"localhost:5055":"pass"}}`, `{"Username":"tester","Secret":"s3cretpw"}`, LoginHelper},
		{"podman, credHelpers, logged out", engine.Podman,
			`{"credHelpers":{"localhost:5055":"pass"}}`, `credentials not found in native keychain`, LoginNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub(t, &stubRunner{output: map[string][]byte{"get": []byte(tt.helper)}})
			useEngine(t, tt.engine)
			writeAuthFile(t, tt.file)

			if got := RegistryLoginState(host); got != tt.want {
				t.Errorf("RegistryLoginState() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Docker Hub's credentials land under https://index.docker.io/v1/ whatever
// alias was typed, so the state must be found through the alias set.
func TestRegistryLoginStateFollowsDockerHubAliases(t *testing.T) {
	stub(t, &stubRunner{})
	useEngine(t, engine.Docker)
	writeAuthFile(t, `{"auths":{"https://index.docker.io/v1/":{"auth":"dTpw"}}}`)

	if got := RegistryLoginState("docker.io"); got != LoginInline {
		t.Errorf("RegistryLoginState(docker.io) = %v, want LoginInline", got)
	}
}

// No file is the ordinary state of a machine that never logged in anywhere.
func TestRegistryLoginStateWithoutAnAuthFile(t *testing.T) {
	stub(t, &stubRunner{})
	useEngine(t, engine.Docker)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if got := RegistryLoginState("registry.example.com"); got.LoggedIn() {
		t.Errorf("RegistryLoginState() = %v, want logged out", got)
	}
}
