package credentials

import (
	"bytes"
	"context"
	"errors"
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

// StoreName names where a secret saved here goes, for a sentence the user
// reads: "moved ... into <StoreName>".
func (s Selection) StoreName() string {
	switch s.Backend {
	case BackendKeyring:
		return "the " + KeyringName()
	case BackendGitCredential:
		return "the git credential helper"
	default:
		return "memory, for this session only"
	}
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
//
// The probe of the host store is bounded by keyringTimeout (DEVDESK-745): a
// store that does not answer is skipped like an absent one, and Detail says it
// did not answer, since the user can fix that (unlock it) where they cannot
// fix an absent one.
func Select(contextName, preference string) Selection {
	switch preference {
	case PreferenceKeyring:
		probe := keyringProbe()
		if probe == nil {
			return keyringSelection(contextName)
		}
		if errors.Is(probe, ErrKeyringUnresponsive) {
			return memorySelection(KeyringName() + " is configured but " + keyringSilence())
		}
		return memorySelection(KeyringName() + " is configured but not reachable")

	case PreferenceGitCredential:
		if helper, ok := gitHelperUsable(); ok {
			return gitSelection(contextName, helper)
		}
		return memorySelection("git is not configured with a credential helper that stores secrets safely")
	}

	probe := keyringProbe()
	if probe == nil {
		return keyringSelection(contextName)
	}
	silent := errors.Is(probe, ErrKeyringUnresponsive)
	if helper, ok := gitHelperUsable(); ok {
		sel := gitSelection(contextName, helper)
		if silent {
			sel.Detail += " The " + KeyringName() + " was skipped: it " + keyringSilence() + "."
		}
		return sel
	}
	if silent {
		return memorySelection("the " + KeyringName() + " " + keyringSilence())
	}
	return memorySelection("no " + KeyringName() + " and no usable git credential helper")
}

// keyringSilence says that the host store did not answer and what to do about
// it, as the end of a sentence whose subject is the store.
func keyringSilence() string {
	return "did not answer within " + keyringTimeout.String() +
		" (probably locked): unlock it and restart DevDesk, or set app.secret_backend"
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
// `store` is refused: it keeps credentials in ~/.git-credentials in plaintext,
// which is the failure this whole change exists to remove — moving the token
// from one plaintext file to another is not a fix. It is refused anywhere in
// the list, not only as the last helper: `git credential approve` hands the
// secret to every configured helper, so a `store` before or after a safe one
// still writes it in plaintext (DEVDESK-745). An unset helper is refused too,
// since git would then have nowhere to put the secret and would quietly
// succeed at storing nothing.
//
// Every other helper is accepted. There is no list of the good ones to check
// against: `manager`, `osxkeychain`, `libsecret`, `wincred` and any number of
// third-party helpers all reach a real store, and enumerating them would only
// mean rejecting the next one someone installs.
func gitHelperUsable() (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitConfigTimeout)
	defer cancel()

	cmd, cleanup, err := gitCommand(ctx, "config", "--get-all", "credential.helper")
	if err != nil {
		return "", false
	}
	defer cleanup()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.WaitDelay = credentialWaitDelay
	if err := cmd.Run(); err != nil {
		return "", false
	}
	return pickHelper(activeHelpers(stdout.String()))
}

// activeHelpers returns the helpers git will call, in order. An empty value
// resets the list (gitcredentials(7)), so only what follows the last one
// counts.
func activeHelpers(output string) []string {
	var active []string
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			active = nil
			continue
		}
		active = append(active, line)
	}
	return active
}

// pickHelper refuses the list when any helper writes plaintext, and otherwise
// names the last usable one: git resolves the last helper that answers.
func pickHelper(helpers []string) (string, bool) {
	for _, h := range helpers {
		if isPlaintextHelper(h) {
			return "", false
		}
	}
	for i := len(helpers) - 1; i >= 0; i-- {
		if name, ok := usableHelperName(helpers[i]); ok {
			return name, true
		}
	}
	return "", false
}

// isPlaintextHelper reports whether a credential.helper value is git's
// plaintext store: `store` with or without arguments, the helper by its path
// (`/usr/lib/git-core/git-credential-store`), or a shell command running it.
// It errs on the side of refusing: any mention of credential-store counts.
func isPlaintextHelper(value string) bool {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return false
	}
	return strings.TrimPrefix(fields[0], "!") == "store" || strings.Contains(value, "credential-store")
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
