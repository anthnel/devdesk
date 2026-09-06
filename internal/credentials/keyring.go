package credentials

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/zalando/go-keyring"
)

const (
	// keyringService is the service name every DevDesk secret is filed under.
	// It is what the entry is called in the host's store: `devdesk:<account>`
	// in the Windows Credential Manager, service `devdesk` in the macOS
	// Keychain, attribute `service=devdesk` in the Secret Service.
	keyringService = "devdesk"

	// keyringProbeAccount is read — never written — to find out whether a store
	// is reachable at all. A miss proves the backend answered; an error of any
	// other kind proves it did not.
	keyringProbeAccount = "devdesk-availability-probe"
)

// KeyringStorage keeps secrets in the host's own secret manager: the Windows
// Credential Manager, the macOS Keychain, or a Secret Service implementation on
// Linux and the BSDs. None of the three needs cgo and none of them writes the
// secret to a file DevDesk owns.
//
// This is the only storage that can honour "no secret is written to disk in
// plaintext" (§3.9). GitCredentialStorage delegates to whatever helper git is
// configured with, which may well be one that writes plaintext.
type KeyringStorage struct {
	context string
}

// NewKeyringStorage returns a store scoped to a DevDesk context. Two contexts
// pointing at the same host keep separate secrets, which is the property
// GitCredentialStorage has to work for (see its describe method).
func NewKeyringStorage(contextName string) *KeyringStorage {
	if contextName == "" {
		contextName = "default"
	}
	return &KeyringStorage{context: contextName}
}

// account builds the key a secret is filed under. The context comes first so
// that the entries of one context sort together when a user browses the store
// by hand.
func (k *KeyringStorage) account(url string) string {
	return k.context + "/" + url
}

// Save writes the secret to the host store, replacing any previous value.
func (k *KeyringStorage) Save(url, secret string) error {
	if err := keyring.Set(keyringService, k.account(url), secret); err != nil {
		return fmt.Errorf("%s: %w", KeyringName(), err)
	}
	return nil
}

// Load reads the secret back. A missing entry is an error, matching the rest of
// the Storage implementations.
func (k *KeyringStorage) Load(url string) (string, error) {
	secret, err := keyring.Get(keyringService, k.account(url))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", fmt.Errorf("%w for %s (context: %s)", ErrNotFound, url, k.context)
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", KeyringName(), err)
	}
	return secret, nil
}

// Delete removes the secret. Deleting one that is not there succeeds: logout
// must not fail because the token was already gone.
func (k *KeyringStorage) Delete(url string) error {
	err := keyring.Delete(keyringService, k.account(url))
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return fmt.Errorf("%s: %w", KeyringName(), err)
}

// KeyringAvailable reports whether the host store answers at all.
//
// The probe is a read of an account that is never written. A miss means the
// backend replied and simply holds nothing — the store works. Anything else is
// the backend being absent: no D-Bus session on a headless Linux box, no
// `security` binary, an OS with no provider compiled in.
//
// It is a read, so it can never leave anything behind if it is called on a
// machine where the store turns out to be unusable.
func KeyringAvailable() bool {
	_, err := keyring.Get(keyringService, keyringProbeAccount)
	return err == nil || errors.Is(err, keyring.ErrNotFound)
}

// KeyringName is what the host calls its secret store, for messages the user
// reads. Naming the actual store beats "the system keyring" when the point is
// to tell someone where their token went.
func KeyringName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows Credential Manager"
	case "darwin":
		return "macOS Keychain"
	default:
		return "Secret Service"
	}
}
