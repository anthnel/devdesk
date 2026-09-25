package trust

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"testing"
)

// dhiKeyFingerprint is the SHA-256 of dhi-2's DER form, cross-checked on
// 2026-09-25 against three sources (§3.82). Changing the embedded key without
// changing this is the mistake this test exists to stop.
const dhiKeyFingerprint = "118ba556dd52f4aec67018efd316c285c783cd3e54cc0f4527605715c643887c"

func TestTheEmbeddedDHIKeyIsTheCrossCheckedOne(t *testing.T) {
	block, _ := pem.Decode(dhiKey)
	if block == nil {
		t.Fatal("the embedded DHI key is not PEM")
	}
	if _, err := x509.ParsePKIXPublicKey(block.Bytes); err != nil {
		t.Fatalf("the embedded DHI key does not parse: %v", err)
	}
	sum := sha256.Sum256(block.Bytes)
	if got := hex.EncodeToString(sum[:]); got != dhiKeyFingerprint {
		t.Errorf("embedded DHI key fingerprint = %s, want %s", got, dhiKeyFingerprint)
	}
}

func TestEveryBuiltinRuleIsComplete(t *testing.T) {
	for _, r := range Builtin() {
		if r.Source != SourceBuiltin || r.Origin == "" || !validPattern(r.Match) {
			t.Errorf("%s: source %v, origin %q", r.Match, r.Source, r.Origin)
		}
		switch r.Mode {
		case ModeKeyless:
			if r.Issuer == "" || r.Subject == "" {
				t.Errorf("%s: keyless without a pinned identity", r.Match)
			}
		case ModeKey:
			if len(r.Keys) == 0 || !r.TLog {
				t.Errorf("%s: key rule without a key, or without the log", r.Match)
			}
		default:
			t.Errorf("%s: a built-in rule must expect a signature", r.Match)
		}
	}
}
