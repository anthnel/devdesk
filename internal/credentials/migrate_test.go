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

// The migration target is the host store (go-keyring's mock, installed by
// TestMain): until DEVDESK-745 this test migrated into a MemoryStorage, which
// the migration now refuses because it does not persist.
func TestMigrateLegacySecretsMovesBothSecrets(t *testing.T) {
	mockKeyring(t)
	path := legacyContextFile(t, legacyBoth)
	sel := keyringSelection("default")
	store := sel.Storage

	notes := MigrateLegacySecrets(sel, "default")

	if len(notes) != 2 {
		t.Errorf("notes = %v, want one per secret moved", notes)
	}
	for _, n := range notes {
		if !strings.Contains(n, "into the "+KeyringName()) {
			t.Errorf("note %q does not name the store the secret went to", n)
		}
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

	if notes := MigrateLegacySecrets(persistentMemory(), "default"); notes != nil {
		t.Errorf("notes = %v, want none", notes)
	}
}

// A store that refuses the write must leave the plaintext where it is: losing
// the secret would be worse than leaving it, and the user has to be told.
func TestMigrateLegacySecretsKeepsThePlaintextWhenTheStoreRefuses(t *testing.T) {
	path := legacyContextFile(t, legacyBoth)

	notes := MigrateLegacySecrets(Selection{Storage: failingStorage{}, Backend: BackendKeyring}, "default")

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

	notes := MigrateLegacySecrets(persistentMemory(), "default")

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

	if notes := MigrateLegacySecrets(persistentMemory(), "default"); notes != nil {
		t.Errorf("notes = %v, want none for a context that does not exist", notes)
	}
}

func TestMigrateLegacySecretsOnAnInvalidContextName(t *testing.T) {
	if notes := MigrateLegacySecrets(persistentMemory(), "Not A Context"); notes != nil {
		t.Errorf("notes = %v, want none", notes)
	}
}

type failingStorage struct{}

func (failingStorage) Save(string, string) error   { return errors.New("store unavailable") }
func (failingStorage) Load(string) (string, error) { return "", errors.New("store unavailable") }
func (failingStorage) Delete(string) error         { return errors.New("store unavailable") }

// A host store that does not answer, with no git helper to fall back on,
// leaves Select on memory. Migrating there would delete the plaintext from the
// file and keep the secret only until the process exits (DEVDESK-745): the
// file must stay as it is, and the note must say nothing moved.
func TestMigrateLegacySecretsKeepsTheFileWhenNoStorePersists(t *testing.T) {
	for _, pref := range []string{PreferenceAuto, PreferenceKeyring} {
		t.Run(pref, func(t *testing.T) {
			shortKeyringTimeout(t)
			useHangingHost(t)
			hermeticGitWithHelper(t, "store")
			path := legacyContextFile(t, legacyBoth)

			sel := Select("default", pref)
			if sel.Persists() {
				t.Fatalf("Select(%q) = %q, want a selection that does not persist", pref, sel.Backend)
			}
			notes := MigrateLegacySecrets(sel, "default")

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading file: %v", err)
			}
			if string(data) != legacyBoth {
				t.Errorf("the configuration file changed although no store persists:\n%s", data)
			}
			joined := strings.Join(notes, " ")
			if strings.Contains(joined, "moved") || !strings.Contains(joined, "no persistent secret store answered") {
				t.Errorf("notes = %v, want them to say the secrets were kept in the file", notes)
			}
		})
	}
}

// persistentMemory is a selection that claims to persist, backed by memory: for
// the tests whose subject is not the destination (nothing to move, no URL).
func persistentMemory() Selection {
	return Selection{Storage: NewMemoryStorage(), Backend: BackendKeyring}
}
