// Package trust decides whether an image's content is the one its publisher
// released (§3.82): which identity must have signed it, what a verifier found,
// and whether that blocks, warns or passes.
//
// It holds the policy and the decision, not the tool. The verification itself
// is a Verifier — cosign, run by internal/scan — so this package imports
// nothing of DevDesk's own and every consumer (the remediation tab, the pull,
// the scan) reads the same table.
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Source is who declared a rule. It decides how hard a verdict lands: a rule
// the user wrote asks for a guarantee, a built-in one is DevDesk's guess on
// their behalf, and continuity is no declaration at all.
type Source int

const (
	// SourceContinuity is A: no rule, the identity that signed the image in use.
	SourceContinuity Source = iota
	// SourceBuiltin is B: a publisher DevDesk knows, measured before entering.
	SourceBuiltin
	// SourceUser is C: a rule of ~/.devdesk/trust.yaml.
	SourceUser
)

func (s Source) String() string {
	switch s {
	case SourceBuiltin:
		return "built-in"
	case SourceUser:
		return "user"
	}
	return "continuity"
}

// Mode is how a rule expects the image to be signed. It also names the tool:
// key and keyless are cosign's notions, and a notation block — refused for
// now — would be another verifier's.
type Mode int

const (
	// ModeKeyless is a Sigstore certificate: an OIDC issuer and a subject.
	ModeKeyless Mode = iota + 1
	// ModeKey is a public key the signature must verify against.
	ModeKey
	// ModeNone is `expect: none`: no signature is expected, and none is asked.
	ModeNone
)

// Identity is who a keyless certificate names.
type Identity struct {
	Issuer  string
	Subject string
}

// Key is one public key a key-mode rule accepts.
type Key struct {
	// Ref is where the key came from, for a person: a path, a KMS URI, or the
	// name of an embedded key.
	Ref string
	// PEM is the key itself. Empty for a KMS URI, which cosign resolves.
	PEM []byte
}

// KMS reports a key cosign fetches itself (awskms://, gcpkms://, …).
func (k Key) KMS() bool { return len(k.PEM) == 0 && strings.Contains(k.Ref, "://") }

// Rule is one declaration: images whose repository matches are signed this way.
type Rule struct {
	// Match is a glob on the normalized repository, without tag or digest:
	// `gcr.io/distroless/*`. A trailing `/**` matches any depth below.
	Match string
	Mode  Mode

	// Keys are accepted in key mode: the signature verifies if one of them
	// does. More than one only while a publisher rotates.
	Keys []Key
	// TLog asks for a transparency-log entry, in key mode. On by default: a
	// signature nobody logged is refused unless the rule says otherwise.
	TLog bool

	// Issuer, and one of Subject or SubjectRegexp, in keyless mode.
	Issuer        string
	Subject       string
	SubjectRegexp string

	Source Source
	// Origin is what a refusal names so the reader knows what to change:
	// "~/.devdesk/trust.yaml:12", "built-in: distroless".
	Origin string
}

// Fingerprint identifies what a rule asks, and nothing else: a cached verdict
// is keyed by it, so editing the identity or the key invalidates the entry,
// while moving the rule or renaming its file does not.
func (r Rule) Fingerprint() string {
	h := sha256.New()
	write := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	switch r.Mode {
	case ModeKey:
		write("key", boolString(r.TLog))
		for _, k := range r.Keys {
			if k.KMS() {
				write("kms", k.Ref)
			} else {
				write("pem", string(k.PEM))
			}
		}
	case ModeKeyless:
		write("keyless", r.Issuer, r.Subject, r.SubjectRegexp)
	default:
		write("none")
	}
	return hex.EncodeToString(h.Sum(nil))
}

func boolString(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
