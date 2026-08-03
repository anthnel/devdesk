package credentials

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// Backend names where a secret actually ends up.
type Backend string

const (
	// BackendKeyring is the host's own secret manager. The only backend that
	// can promise the secret is not sitting in a plaintext file somewhere.
	BackendKeyring Backend = "keyring"

	// BackendGitCredential delegates to git's configured credential helper.
	// Honest only as far as that helper is: `store` writes plaintext, which is
	// why gitHelperUsable refuses it.
	BackendGitCredential Backend = "git-credential"

	// BackendMemory holds the secret for this session and nothing longer. It is
	// the fallback, and it is deliberately the worst one — see Selection.Detail.
	BackendMemory Backend = "memory"
)

// Preference values accepted from app.secret_backend in the configuration.
const (
	PreferenceAuto          = "auto"
	PreferenceKeyring       = "keyring"
	PreferenceGitCredential = "git-credential"
)

// gitConfigTimeout bounds the `git config` read that inspects the helper. Same
// reasoning as credentialTimeout: a blocked git call must not freeze the TUI.
const gitConfigTimeout = 2 * time.Second

// Selection is a storage together with the answer to "where does the secret go".
// The UI needs both: it has to tell the user which store a token was written
// to, and warn them when the answer is "nowhere that survives this session".
type Selection struct {
	Storage Storage
	Backend Backend

	// Detail is a sentence for the user, naming the store or explaining why
	// there is none. Always populated.
	Detail string
}

// Persists reports whether a secret saved here survives the process.
func (s Selection) Persists() bool {
	return s.Backend != BackendMemory
}

// Select resolves the storage for a context.
//
// The order is deliberate and there is exactly one destination — writing to
// several at once is what made the old "secure" option store the token in the
// credential manager *and* in a plaintext file (§3.9):
//
//  1. the host secret store, when one answers;
//  2. git's credential helper, when git is configured with one that does not
//     itself write plaintext;
//  3. memory, for this session only.
//
// preference comes from app.secret_backend and pins the head of that list.
// A pinned backend that turns out to be unreachable falls through to memory
// rather than silently to the other one: someone who asked for the keyring
// should not get a git helper without being told.
//
// The returned Selection must be kept for the lifetime of the context. Calling
// Select again yields a fresh MemoryStorage, and a token saved into the old one
// would be invisible to it.
func Select(contextName, preference string) Selection {
	switch preference {
	case PreferenceKeyring:
		if KeyringAvailable() {
			return keyringSelection(contextName)
		}
		return memorySelection(KeyringName() + " is configured but not reachable")

	case PreferenceGitCredential:
		if helper, ok := gitHelperUsable(); ok {
			return gitSelection(contextName, helper)
		}
		return memorySelection("git is not configured with a credential helper that stores secrets safely")
	}

	if KeyringAvailable() {
		return keyringSelection(contextName)
	}
	if helper, ok := gitHelperUsable(); ok {
		return gitSelection(contextName, helper)
	}
	return memorySelection("no " + KeyringName() + " and no usable git credential helper")
}

func keyringSelection(contextName string) Selection {
	return Selection{
		Storage: NewKeyringStorage(contextName),
		Backend: BackendKeyring,
		Detail:  "Secrets are stored in the " + KeyringName() + ".",
	}
}

func gitSelection(contextName, helper string) Selection {
	return Selection{
		Storage: NewGitCredentialStorageWithContext(contextName),
		Backend: BackendGitCredential,
		Detail:  "Secrets are stored by the git credential helper (" + helper + ").",
	}
}

// SessionOnly returns a Selection that keeps secrets in this process and
// nowhere else. Select falls back to it, and the router starts on it before it
// has resolved a real backend — a Selection is never nil, so nothing has to
// guard against a missing store.
func SessionOnly(why string) Selection {
	return Selection{
		Storage: NewMemoryStorage(),
		Backend: BackendMemory,
		Detail:  "Nothing is saved — " + why + ". You will have to authenticate again next launch.",
	}
}

func memorySelection(why string) Selection {
	return SessionOnly(why)
}

// gitHelperUsable reports whether git has a credential helper configured that
// is worth delegating to, and names it.
//
// `store` is refused by name: it keeps credentials in ~/.git-credentials in
// plaintext, which is the failure this whole change exists to remove — moving
// the token from one plaintext file to another is not a fix. An unset helper is
// refused too, since git would then have nowhere to put the secret and would
// quietly succeed at storing nothing.
//
// Every other helper is accepted. There is no list of the good ones to check
// against: `manager`, `osxkeychain`, `libsecret`, `wincred` and any number of
// third-party helpers all reach a real store, and enumerating them would only
// mean rejecting the next one someone installs.
func gitHelperUsable() (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitConfigTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "config", "--get-all", "credential.helper")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.WaitDelay = credentialWaitDelay
	if err := cmd.Run(); err != nil {
		return "", false
	}

	// git resolves the last helper that answers, so read them in that order and
	// take the first usable one from the end.
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if name, ok := usableHelperName(lines[i]); ok {
			return name, true
		}
	}
	return "", false
}

// usableHelperName extracts the helper name from one credential.helper value
// and reports whether it stores secrets somewhere other than a plaintext file.
func usableHelperName(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}

	// A value may carry arguments (`store --file=/path`), and a leading `!`
	// marks a shell command. Only the first word names the helper.
	name := strings.TrimPrefix(strings.Fields(value)[0], "!")
	if name == "store" {
		return "", false
	}
	return name, true
}
