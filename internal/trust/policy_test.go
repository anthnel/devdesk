package trust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testKey = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEKdROmntRJFBrOJOQF5ww6gDBJqGm
Fxa4333s1KsL9ISjtmRzGNih9lNRsqfRVjgFgJIdL6EQ9dohdanvn7r2cg==
-----END PUBLIC KEY-----
`

// writePolicy writes a trust.yaml, and a key beside it, in a fresh directory.
func writePolicy(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "corp.pub"), []byte(testKey), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "trust.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustLoad(t *testing.T, body string) (Policy, []string) {
	t.Helper()
	p, warnings, err := Load(writePolicy(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return p, warnings
}

// mustReject asserts the file is refused, and refused whole.
func mustReject(t *testing.T, body string) {
	t.Helper()
	_ = loadErr(t, body)
}

// loadErr is mustReject, returning the error for a test that reads it.
func loadErr(t *testing.T, body string) error {
	t.Helper()
	p, _, err := Load(writePolicy(t, body))
	if err == nil {
		t.Fatalf("Load accepted the file: %+v", p)
	}
	if len(p.Rules) != 0 {
		t.Errorf("a rejected file still yielded %d rules — the whole file must go", len(p.Rules))
	}
	return err
}

func TestLoadReadsEveryMode(t *testing.T) {
	p, warnings := mustLoad(t, `version: 1
rules:
  - match: registry.corp.example/base/*
    key: corp.pub
    tlog: false
  - match: cgr.dev/chainguard/**
    keyless:
      issuer: https://token.actions.githubusercontent.com
      subject: https://github.com/chainguard-images/images/.github/workflows/release.yaml@refs/heads/main
  - match: ghcr.io/acme/*
    keyless:
      issuer: https://token.actions.githubusercontent.com
      subject_regexp: ^https://github.com/acme/
  - match: registry.corp.example/legacy/*
    expect: none
  - match: registry.corp.example/signed/*
    key: corp.pub
`)
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if len(p.Rules) != 5 {
		t.Fatalf("got %d rules, want 5", len(p.Rules))
	}
	key, keyless, re, none, tlogDefault := p.Rules[0], p.Rules[1], p.Rules[2], p.Rules[3], p.Rules[4]

	if key.Mode != ModeKey || key.TLog || len(key.Keys) != 1 || string(key.Keys[0].PEM) != testKey {
		t.Errorf("key rule = %+v", key)
	}
	if keyless.Mode != ModeKeyless || keyless.Issuer == "" || keyless.Subject == "" {
		t.Errorf("keyless rule = %+v", keyless)
	}
	if re.SubjectRegexp != "^https://github.com/acme/" || re.Subject != "" {
		t.Errorf("regexp rule = %+v", re)
	}
	if none.Mode != ModeNone {
		t.Errorf("expect: none gave mode %v", none.Mode)
	}
	if !tlogDefault.TLog {
		t.Error("a key rule that says nothing about tlog must require it")
	}
	for _, r := range p.Rules {
		if r.Source != SourceUser {
			t.Errorf("%s: source %v, want user", r.Match, r.Source)
		}
	}
	// The origin a refusal names is the file and the rule's own line.
	if !strings.HasSuffix(key.Origin, "trust.yaml:3") || !strings.HasSuffix(keyless.Origin, "trust.yaml:6") {
		t.Errorf("origins = %q, %q", key.Origin, keyless.Origin)
	}
}

// The reason the file is strict: a typo must not weaken a rule in silence.
func TestAnUnknownKeyRejectsTheWholeFile(t *testing.T) {
	err := loadErr(t, `version: 1
rules:
  - match: registry.corp.example/other/*
    expect: none
  - match: cgr.dev/chainguard/**
    keyless:
      isuer: https://token.actions.githubusercontent.com
      subject: x
`)
	if !strings.Contains(err.Error(), "isuer") {
		t.Errorf("error %q does not name the unknown key", err)
	}
}

func TestARuleDeclaresExactlyOneMode(t *testing.T) {
	mustReject(t, "version: 1\nrules:\n  - match: a.io/*\n    key: corp.pub\n    expect: none\n")
	mustReject(t, "version: 1\nrules:\n  - match: a.io/*\n")
}

func TestTheVersionIsRequired(t *testing.T) {
	mustReject(t, "rules: []\n")
	mustReject(t, "")
	err := loadErr(t, "version: 2\nrules: []\n")
	if !strings.Contains(err.Error(), "version must be 1") {
		t.Errorf("error = %q", err)
	}
}

func TestNotationIsReservedAndRefused(t *testing.T) {
	err := loadErr(t, "version: 1\nrules:\n  - match: a.io/*\n    notation:\n      trust_store: ca:corp\n")
	if !strings.Contains(err.Error(), "not supported yet") {
		t.Errorf("error = %q", err)
	}
}

func TestInvalidRulesAreRefused(t *testing.T) {
	for name, body := range map[string]string{
		"tlog on keyless":       "  - match: a.io/*\n    tlog: false\n    keyless: {issuer: i, subject: s}\n",
		"no issuer":             "  - match: a.io/*\n    keyless: {subject: s}\n",
		"subject and regexp":    "  - match: a.io/*\n    keyless: {issuer: i, subject: s, subject_regexp: r}\n",
		"bad regexp":            "  - match: a.io/*\n    keyless: {issuer: i, subject_regexp: '('}\n",
		"expect something else": "  - match: a.io/*\n    expect: signed\n",
		"no match":              "  - expect: none\n",
		"double star inside":    "  - match: a.io/**/x\n    expect: none\n",
		"missing key file":      "  - match: a.io/*\n    key: nope.pub\n",
	} {
		t.Run(name, func(t *testing.T) { mustReject(t, "version: 1\nrules:\n"+body) })
	}
}

func TestAKeyThatIsNotAPublicKeyIsRefused(t *testing.T) {
	path := writePolicy(t, "version: 1\nrules:\n  - match: a.io/*\n    key: junk.pub\n")
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "junk.pub"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("a key file that is not a PEM public key was accepted")
	}
}

func TestAKMSKeyIsLeftToCosign(t *testing.T) {
	p, _ := mustLoad(t, "version: 1\nrules:\n  - match: a.io/*\n    key: awskms:///alias/cosign\n")
	k := p.Rules[0].Keys[0]
	if !k.KMS() || k.Ref != "awskms:///alias/cosign" {
		t.Errorf("key = %+v", k)
	}
}

func TestAMissingFileIsAnEmptyPolicy(t *testing.T) {
	p, warnings, err := Load(filepath.Join(t.TempDir(), "trust.yaml"))
	if err != nil || len(p.Rules) != 0 || len(warnings) != 0 {
		t.Errorf("Load(missing) = %+v, %v, %v", p, warnings, err)
	}
}

func TestARuleNoImageCanReachIsWarned(t *testing.T) {
	_, warnings := mustLoad(t, `version: 1
rules:
  - match: registry.corp.example/*
    expect: none
  - match: registry.corp.example/app
    expect: none
  - match: registry.corp.example/b*
    expect: none
`)
	// The literal one is certainly shadowed; the pattern one is not reported,
	// since the check only speaks when it is sure.
	if len(warnings) != 1 || !strings.Contains(warnings[0], "registry.corp.example/app") {
		t.Errorf("warnings = %v", warnings)
	}
}
