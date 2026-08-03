package credentials

import (
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestUsableHelperName(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		wantName string
		wantOK   bool
	}{
		{"a real helper", "manager", "manager", true},
		{"platform helper", "osxkeychain", "osxkeychain", true},
		{"a helper with arguments", "libsecret --timeout 5", "libsecret", true},
		{"a shell command helper", "!/usr/local/bin/my-helper", "/usr/local/bin/my-helper", true},
		{"surrounding whitespace", "  wincred  ", "wincred", true},

		// `store` keeps credentials in ~/.git-credentials in plaintext. Moving
		// the token from one plaintext file to another is not a fix.
		{"store is refused", "store", "", false},
		{"store with a file argument is refused", "store --file=/tmp/creds", "", false},

		// An unset helper means git has nowhere to put the secret, and would
		// quietly succeed at storing nothing.
		{"empty is refused", "", "", false},
		{"whitespace only is refused", "   ", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotOK := usableHelperName(tt.value)
			if gotOK != tt.wantOK {
				t.Fatalf("usableHelperName(%q) ok = %v, want %v", tt.value, gotOK, tt.wantOK)
			}
			if gotName != tt.wantName {
				t.Errorf("usableHelperName(%q) name = %q, want %q", tt.value, gotName, tt.wantName)
			}
		})
	}
}

func TestGitHelperUsable(t *testing.T) {
	tests := []struct {
		name     string
		helper   string
		wantName string
		wantOK   bool
	}{
		{"a helper that reaches a real store", "manager", "manager", true},
		{"the plaintext store helper", "store", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hermeticGitWithHelper(t, tt.helper)

			gotName, gotOK := gitHelperUsable()
			if gotOK != tt.wantOK {
				t.Fatalf("gitHelperUsable() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotName != tt.wantName {
				t.Errorf("gitHelperUsable() = %q, want %q", gotName, tt.wantName)
			}
		})
	}
}

// git resolves the last helper that answers, so the last usable one in the list
// is the one whose name should be reported.
func TestGitHelperUsablePrefersTheLastConfiguredHelper(t *testing.T) {
	hermeticGitWithHelper(t, "store\n\thelper = manager")

	got, ok := gitHelperUsable()
	if !ok {
		t.Fatal("gitHelperUsable() = false, want the trailing manager entry to win")
	}
	if got != "manager" {
		t.Errorf("gitHelperUsable() = %q, want manager", got)
	}
}

func TestSelectPrefersTheHostStore(t *testing.T) {
	mockKeyring(t)

	sel := Select("work", PreferenceAuto)

	if sel.Backend != BackendKeyring {
		t.Fatalf("Backend = %q, want %q", sel.Backend, BackendKeyring)
	}
	if !sel.Persists() {
		t.Error("Persists() = false for the host store")
	}
	if !strings.Contains(sel.Detail, KeyringName()) {
		t.Errorf("Detail = %q, want it to name %q", sel.Detail, KeyringName())
	}
	// The storage has to be scoped to the context it was asked for.
	if ks, ok := sel.Storage.(*KeyringStorage); !ok || ks.context != "work" {
		t.Errorf("Storage = %T scoped to %v, want a *KeyringStorage on work", sel.Storage, sel.Storage)
	}
}

func TestSelectFallsBackToGitWhenNoHostStoreAnswers(t *testing.T) {
	unreachableKeyring(t)
	hermeticGitWithHelper(t, "manager")

	sel := Select("work", PreferenceAuto)

	if sel.Backend != BackendGitCredential {
		t.Fatalf("Backend = %q, want %q", sel.Backend, BackendGitCredential)
	}
	if !strings.Contains(sel.Detail, "manager") {
		t.Errorf("Detail = %q, want it to name the helper", sel.Detail)
	}
}

// The last resort is deliberately the worst option: nothing is written, and the
// user has to be able to tell without reading the source.
func TestSelectFallsBackToMemoryWhenNothingCanStoreASecret(t *testing.T) {
	unreachableKeyring(t)
	hermeticGitWithHelper(t, "store")

	sel := Select("work", PreferenceAuto)

	if sel.Backend != BackendMemory {
		t.Fatalf("Backend = %q, want %q", sel.Backend, BackendMemory)
	}
	if sel.Persists() {
		t.Error("Persists() = true for the memory fallback")
	}
	if !strings.Contains(sel.Detail, "Nothing is saved") {
		t.Errorf("Detail = %q, want it to say plainly that nothing is saved", sel.Detail)
	}
}

func TestSelectHonoursThePreference(t *testing.T) {
	tests := []struct {
		name       string
		preference string
		want       Backend
	}{
		{"pinned to the host store", PreferenceKeyring, BackendKeyring},
		{"pinned to git", PreferenceGitCredential, BackendGitCredential},
		{"unset means auto", "", BackendKeyring},
		{"an unknown value means auto", "nonsense", BackendKeyring},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockKeyring(t)
			hermeticGitWithHelper(t, "manager")

			if got := Select("work", tt.preference).Backend; got != tt.want {
				t.Errorf("Backend = %q, want %q", got, tt.want)
			}
		})
	}
}

// A pinned backend that turns out to be unreachable falls through to memory,
// not to the other backend: someone who asked for the host store should not be
// handed a git helper without being told.
func TestSelectDoesNotSubstituteAPinnedBackend(t *testing.T) {
	tests := []struct {
		name       string
		preference string
		setup      func(t *testing.T)
	}{
		{
			"host store pinned but unreachable, with git available",
			PreferenceKeyring,
			func(t *testing.T) {
				unreachableKeyring(t)
				hermeticGitWithHelper(t, "manager")
			},
		},
		{
			"git pinned but unusable, with the host store available",
			PreferenceGitCredential,
			func(t *testing.T) {
				mockKeyring(t)
				hermeticGitWithHelper(t, "store")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)

			if got := Select("work", tt.preference).Backend; got != BackendMemory {
				t.Errorf("Backend = %q, want %q", got, BackendMemory)
			}
		})
	}
}

func TestSessionOnlyAlwaysHasAStorage(t *testing.T) {
	sel := SessionOnly("under test")

	if sel.Storage == nil {
		t.Fatal("Storage is nil — views would have to guard against it")
	}
	if err := sel.Storage.Save("https://gitlab.example.com", "tok"); err != nil {
		t.Errorf("Save() error = %v", err)
	}
}

// unreachableKeyring makes the host store answer with a backend error, which is
// what a headless Linux box with no D-Bus session looks like.
func unreachableKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInitWithError(errors.New("no D-Bus session"))
	t.Cleanup(keyring.MockInit)
}
