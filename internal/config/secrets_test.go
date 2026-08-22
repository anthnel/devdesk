package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeContextFile drops a raw YAML context file into the temp home, bypassing
// SaveContext — which is the point: these tests are about what an *older*
// DevDesk wrote, and the current schema can no longer express it.
func writeContextFile(t *testing.T, contextName, body string) string {
	t.Helper()
	home := setupTmpHome(t)
	dir := filepath.Join(home, ".devdesk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	path, err := GetContextPath(contextName)
	if err != nil {
		t.Fatalf("GetContextPath: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing context file: %v", err)
	}
	return path
}

const legacyContext = `app:
    theme: mocha
gitlab:
    url: https://gitlab.example.com
    token: glpat-plaintext
    clone_method: https
registry:
    url: https://registry.example.com
    username: builder
    password: hunter2
`

func TestReadLegacySecrets(t *testing.T) {
	writeContextFile(t, "default", legacyContext)

	got, err := ReadLegacySecrets("default")
	if err != nil {
		t.Fatalf("ReadLegacySecrets() error = %v", err)
	}

	want := LegacySecrets{
		GitLabURL:        "https://gitlab.example.com",
		GitLabToken:      "glpat-plaintext",
		RegistryURL:      "https://registry.example.com",
		RegistryPassword: "hunter2",
	}
	if got != want {
		t.Errorf("ReadLegacySecrets() = %+v, want %+v", got, want)
	}
	if got.Empty() {
		t.Error("Empty() = true for a file holding two secrets")
	}
}

func TestReadLegacySecretsOnACleanFile(t *testing.T) {
	writeContextFile(t, "default", "gitlab:\n    url: https://gitlab.example.com\n")

	got, err := ReadLegacySecrets("default")
	if err != nil {
		t.Fatalf("ReadLegacySecrets() error = %v", err)
	}
	if !got.Empty() {
		t.Errorf("Empty() = false for %+v, want true — nothing to migrate", got)
	}
}

// A context that does not exist yet is the normal case on first launch, not an
// error condition.
func TestReadLegacySecretsOnAMissingFile(t *testing.T) {
	setupTmpHome(t)

	got, err := ReadLegacySecrets("default")
	if err != nil {
		t.Fatalf("ReadLegacySecrets() error = %v, want nil for a missing file", err)
	}
	if !got.Empty() {
		t.Errorf("ReadLegacySecrets() = %+v, want empty", got)
	}
}

func TestRemoveLegacySecrets(t *testing.T) {
	path := writeContextFile(t, "default", legacyContext)

	if err := RemoveLegacySecrets("default", true, true); err != nil {
		t.Fatalf("RemoveLegacySecrets() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading rewritten file: %v", err)
	}
	body := string(data)

	for _, secret := range []string{"glpat-plaintext", "hunter2", "token:", "password:"} {
		if strings.Contains(body, secret) {
			t.Errorf("rewritten file still contains %q:\n%s", secret, body)
		}
	}

	// Everything else has to survive: this is somebody's configuration file,
	// not a scratch buffer.
	for _, kept := range []string{"theme: mocha", "url: https://gitlab.example.com", "clone_method: https", "username: builder"} {
		if !strings.Contains(body, kept) {
			t.Errorf("rewritten file lost %q:\n%s", kept, body)
		}
	}
}

// The caller passes true only for a secret it has already stored elsewhere. A
// store that refused the write must leave the file alone rather than losing it.
func TestRemoveLegacySecretsOnlyRemovesWhatItIsAskedTo(t *testing.T) {
	path := writeContextFile(t, "default", legacyContext)

	if err := RemoveLegacySecrets("default", true, false); err != nil {
		t.Fatalf("RemoveLegacySecrets() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading rewritten file: %v", err)
	}
	body := string(data)

	if strings.Contains(body, "glpat-plaintext") {
		t.Errorf("the token was not removed:\n%s", body)
	}
	if !strings.Contains(body, "hunter2") {
		t.Errorf("the registry password was removed without being asked for:\n%s", body)
	}
}

func TestRemoveLegacySecretsOnACleanFileChangesNothing(t *testing.T) {
	const clean = "gitlab:\n    url: https://gitlab.example.com\n"
	path := writeContextFile(t, "default", clean)

	if err := RemoveLegacySecrets("default", true, true); err != nil {
		t.Fatalf("RemoveLegacySecrets() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if string(data) != clean {
		t.Errorf("file was rewritten with nothing to remove:\ngot  %q\nwant %q", data, clean)
	}
}

// The rewritten file still has to load.
func TestRemoveLegacySecretsLeavesALoadableFile(t *testing.T) {
	writeContextFile(t, "default", legacyContext)

	if err := RemoveLegacySecrets("default", true, true); err != nil {
		t.Fatalf("RemoveLegacySecrets() error = %v", err)
	}

	cfg, err := LoadContext("default")
	if err != nil {
		t.Fatalf("LoadContext() after the rewrite: %v", err)
	}
	if cfg.Forge.URL != "https://gitlab.example.com" {
		t.Errorf("GitLab.URL = %q, want it preserved", cfg.Forge.URL)
	}
	if cfg.App.Theme != "mocha" {
		t.Errorf("App.Theme = %q, want mocha", cfg.App.Theme)
	}
}

func TestRemoveLegacySecretsOnAMissingFile(t *testing.T) {
	setupTmpHome(t)

	if err := RemoveLegacySecrets("default", true, true); err != nil {
		t.Errorf("RemoveLegacySecrets() on a missing file = %v, want nil", err)
	}
}

func TestLegacySecretsReadRejectsAnInvalidContextName(t *testing.T) {
	setupTmpHome(t)

	if _, err := ReadLegacySecrets("Not A Context"); err == nil {
		t.Error("ReadLegacySecrets() with an invalid context name returned no error")
	}
	if err := RemoveLegacySecrets("Not A Context", true, true); err == nil {
		t.Error("RemoveLegacySecrets() with an invalid context name returned no error")
	}
}
