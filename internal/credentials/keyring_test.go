package credentials

import (
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// mockKeyring swaps go-keyring's provider for an in-memory one. The provider is
// a package-level global in that library, so every test that touches the store
// has to install its own — and none of them may run in parallel.
func mockKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInit()
}

func TestKeyringStorageRoundTrip(t *testing.T) {
	mockKeyring(t)
	storage := NewKeyringStorage("default")

	if err := storage.Save("https://gitlab.example.com", "glpat-secret"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := storage.Load("https://gitlab.example.com")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != "glpat-secret" {
		t.Errorf("Load() = %q, want glpat-secret", got)
	}

	if err := storage.Delete("https://gitlab.example.com"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := storage.Load("https://gitlab.example.com"); err == nil {
		t.Error("Load() after Delete returned no error")
	}
}

// Two contexts pointing at the same host are the case the whole context-scoping
// exists for: without it the last one to authenticate wins for both.
func TestKeyringStorageIsolatesContexts(t *testing.T) {
	mockKeyring(t)
	const url = "https://gitlab.example.com"

	work := NewKeyringStorage("work")
	personal := NewKeyringStorage("personal")

	if err := work.Save(url, "work-token"); err != nil {
		t.Fatalf("work Save() error = %v", err)
	}
	if err := personal.Save(url, "personal-token"); err != nil {
		t.Fatalf("personal Save() error = %v", err)
	}

	got, err := work.Load(url)
	if err != nil {
		t.Fatalf("work Load() error = %v", err)
	}
	if got != "work-token" {
		t.Errorf("work Load() = %q, want work-token — the personal context overwrote it", got)
	}
}

func TestNewKeyringStorageDefaultsTheContext(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty context falls back to default", "", "default"},
		{"named context is kept", "prod", "prod"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewKeyringStorage(tt.in).context; got != tt.want {
				t.Errorf("context = %q, want %q", got, tt.want)
			}
		})
	}
}

// Deleting a secret that is not there succeeds: logout must not fail because
// the token was already gone.
func TestKeyringStorageDeleteIsIdempotent(t *testing.T) {
	mockKeyring(t)

	if err := NewKeyringStorage("default").Delete("https://never-saved.example.com"); err != nil {
		t.Errorf("Delete() on a missing entry = %v, want nil", err)
	}
}

func TestKeyringStorageReportsBackendFailures(t *testing.T) {
	keyring.MockInitWithError(errors.New("no D-Bus session"))
	t.Cleanup(keyring.MockInit)
	storage := NewKeyringStorage("default")

	for _, tt := range []struct {
		name string
		op   func() error
	}{
		{"Save", func() error { return storage.Save("https://gitlab.example.com", "tok") }},
		{"Load", func() error { _, err := storage.Load("https://gitlab.example.com"); return err }},
		{"Delete", func() error { return storage.Delete("https://gitlab.example.com") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op()
			if err == nil {
				t.Fatalf("%s() with an unreachable store returned no error", tt.name)
			}
			// The message has to name the store: "keyring error" tells a user
			// nothing about where to go looking.
			if !strings.Contains(err.Error(), KeyringName()) {
				t.Errorf("%s() error = %q, want it to name %q", tt.name, err, KeyringName())
			}
		})
	}
}

func TestKeyringAvailable(t *testing.T) {
	tests := []struct {
		name  string
		setup func()
		want  bool
	}{
		{"a store that answers", keyring.MockInit, true},
		{
			"a store that is not there",
			func() { keyring.MockInitWithError(errors.New("no D-Bus session")) },
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			t.Cleanup(keyring.MockInit)

			if got := KeyringAvailable(); got != tt.want {
				t.Errorf("KeyringAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The probe must never leave an entry behind: it runs on every launch, on
// machines where the store may turn out to be unusable.
func TestKeyringAvailableWritesNothing(t *testing.T) {
	mockKeyring(t)

	KeyringAvailable()

	if _, err := keyring.Get(keyringService, keyringProbeAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Errorf("probe account exists after KeyringAvailable(): err = %v, want ErrNotFound", err)
	}
}

func TestKeyringNameIsNotEmpty(t *testing.T) {
	if KeyringName() == "" {
		t.Error("KeyringName() is empty — it is shown to the user on every platform")
	}
}
