package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyContextFile writes a raw context file into a temp home. It bypasses
// config.SaveContext deliberately: the current schema can no longer express the
// plaintext fields this whole migration exists to remove.
func legacyContextFile(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	dir := filepath.Join(home, ".devdesk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing context file: %v", err)
	}
	return path
}

const legacyBoth = `gitlab:
    url: https://gitlab.example.com
    token: glpat-plaintext
registry:
    url: https://registry.example.com
    password: hunter2
`

func TestMigrateLegacySecretsMovesBothSecrets(t *testing.T) {
	path := legacyContextFile(t, legacyBoth)
	store := NewMemoryStorage()

	notes := MigrateLegacySecrets(store, "default")

	if len(notes) != 2 {
		t.Errorf("notes = %v, want one per secret moved", notes)
	}

	token, err := store.Load("https://gitlab.example.com")
	if err != nil || token != "glpat-plaintext" {
		t.Errorf("token in store = %q (err %v), want glpat-plaintext", token, err)
	}
	password, err := store.Load("https://registry.example.com")
	if err != nil || password != "hunter2" {
		t.Errorf("password in store = %q (err %v), want hunter2", password, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading rewritten file: %v", err)
	}
	if body := string(data); strings.Contains(body, "glpat-plaintext") || strings.Contains(body, "hunter2") {
		t.Errorf("the plaintext survived in the config file:\n%s", body)
	}
}

// The normal case, on every launch after the first: nothing to say and nothing
// written.
func TestMigrateLegacySecretsIsSilentWhenThereIsNothingToMove(t *testing.T) {
	legacyContextFile(t, "gitlab:\n    url: https://gitlab.example.com\n")

	if notes := MigrateLegacySecrets(NewMemoryStorage(), "default"); notes != nil {
		t.Errorf("notes = %v, want none", notes)
	}
}

// A store that refuses the write must leave the plaintext where it is: losing
// the secret would be worse than leaving it, and the user has to be told.
func TestMigrateLegacySecretsKeepsThePlaintextWhenTheStoreRefuses(t *testing.T) {
	path := legacyContextFile(t, legacyBoth)

	notes := MigrateLegacySecrets(failingStorage{}, "default")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if !strings.Contains(string(data), "glpat-plaintext") {
		t.Error("the token was deleted from the config file without being stored anywhere")
	}
	if len(notes) == 0 || !strings.Contains(strings.Join(notes, " "), "still in plaintext") {
		t.Errorf("notes = %v, want the failure said plainly", notes)
	}
}

// A secret with no URL has no key to be filed under and no host DevDesk could
// use it against. It is unusable plaintext, so it goes — and the user is told.
func TestMigrateLegacySecretsDropsASecretWithNoURL(t *testing.T) {
	path := legacyContextFile(t, "gitlab:\n    token: glpat-orphan\n")
	store := NewMemoryStorage()

	notes := MigrateLegacySecrets(store, "default")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if strings.Contains(string(data), "glpat-orphan") {
		t.Errorf("the unusable token survived:\n%s", data)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "no URL") {
		t.Errorf("notes = %v, want the reason spelled out", notes)
	}
}

func TestMigrateLegacySecretsOnAMissingContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if notes := MigrateLegacySecrets(NewMemoryStorage(), "default"); notes != nil {
		t.Errorf("notes = %v, want none for a context that does not exist", notes)
	}
}

func TestMigrateLegacySecretsOnAnInvalidContextName(t *testing.T) {
	if notes := MigrateLegacySecrets(NewMemoryStorage(), "Not A Context"); notes != nil {
		t.Errorf("notes = %v, want none", notes)
	}
}

type failingStorage struct{}

func (failingStorage) Save(string, string) error   { return errors.New("store unavailable") }
func (failingStorage) Load(string) (string, error) { return "", errors.New("store unavailable") }
func (failingStorage) Delete(string) error         { return errors.New("store unavailable") }
