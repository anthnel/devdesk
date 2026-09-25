package scan

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/trust"
)

const signedRef = "gcr.io/distroless/static-debian12@sha256:afa5c872"

var keylessRule = trust.Rule{
	Mode: trust.ModeKeyless, Issuer: "https://accounts.google.com",
	Subject: "keyless@distroless.iam.gserviceaccount.com", Source: trust.SourceBuiltin,
}

var keyRule = trust.Rule{
	Mode: trust.ModeKey, TLog: true, Source: trust.SourceBuiltin,
	Keys: []trust.Key{{Ref: "embedded: test", PEM: []byte("-----BEGIN PUBLIC KEY-----\nx\n-----END PUBLIC KEY-----\n")}},
}

// exit is how the runner reports a tool that ran and exited non-zero.
func exit(code int) error {
	if code == 0 {
		return nil
	}
	return &exitError{Code: code}
}

func isPermissive(tc toolCmd) bool {
	return slices.Contains(tc.Args, "--certificate-oidc-issuer-regexp")
}

// cosignAnswering gives strict and permissive checks their own exit codes.
func cosignAnswering(t *testing.T, strict, permissive int) *scriptedRunner {
	t.Helper()
	r := &scriptedRunner{reply: func(tc toolCmd) ([]byte, error) {
		if isPermissive(tc) {
			return nil, exit(permissive)
		}
		return nil, exit(strict)
	}}
	useRunner(t, r)
	return r
}

var binaryCosign = CosignVerifier{Tool: ToolSpec{Source: ToolSourceBinary, Binary: "cosign"}}

// Every line of §3.82's measurement, keyless.
func TestKeylessExitCodesAreClassifiedAsMeasured(t *testing.T) {
	for _, c := range []struct {
		name               string
		strict, permissive int
		want               trust.Verdict
	}{
		{"verified", 0, -1, trust.Verified},
		{"no signature", 10, -1, trust.Unsigned},
		{"tag not found", 11, -1, trust.Failed},
		{"legacy format, another identity", 12, 0, trust.IdentityMismatch},
		{"bundle format, another identity", 1, 0, trust.IdentityMismatch},
		{"blob store blocked: 12 even when permissive", 12, 12, trust.Failed},
		{"registry unreachable", 1, 1, trust.Failed},
		{"1 strict, then no signature", 1, 10, trust.Unsigned},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := cosignAnswering(t, c.strict, c.permissive)
			got, _ := binaryCosign.Verify(context.Background(), signedRef, keylessRule)
			if got != c.want {
				t.Errorf("strict %d, permissive %d: %v, want %v", c.strict, c.permissive, got, c.want)
			}
			// 0, 10 and 11 are read as they are: no second question.
			if direct := c.permissive == -1; direct && r.count() != 1 {
				t.Errorf("asked %d times for a code read directly", r.count())
			}
		})
	}
}

func TestKeyModeFailsClosed(t *testing.T) {
	for _, c := range []struct {
		name string
		code int
		want trust.Verdict
	}{
		{"verified", 0, trust.Verified},
		{"wrong key, legacy format", 10, trust.Unsigned},
		{"tag not found", 11, trust.Failed},
		// The case the fail-closed decision exists for: DHI moving to the bundle
		// format with a key that no longer matches exits 1, the same as an
		// unreachable registry. Read as Failed, it would only warn under a
		// built-in rule, and the pull would go through.
		{"DHI moves to the bundle format and the key no longer matches", 1, trust.Unsigned},
		{"blob store blocked", 12, trust.Unsigned},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := cosignAnswering(t, c.code, -1)
			got, _ := binaryCosign.Verify(context.Background(), signedRef, keyRule)
			if got != c.want {
				t.Errorf("exit %d: %v, want %v", c.code, got, c.want)
			}
			if r.count() != 1 {
				t.Errorf("asked %d times: key mode has no permissive check", r.count())
			}
		})
	}
	if trust.Decide(trust.Unsigned, trust.SourceBuiltin) != trust.Block {
		t.Error("an Unsigned key-mode verdict under a built-in rule must block")
	}
}

func TestAToolThatDidNotRunIsAFailureEvenInKeyMode(t *testing.T) {
	answering(t, "", errors.New("cosign failed to start: executable file not found"))
	if got, err := binaryCosign.Verify(context.Background(), signedRef, keyRule); got != trust.Failed || err == nil {
		t.Errorf("verdict %v, err %v", got, err)
	}
}

func TestATimeoutIsAFailureNotAnAnswer(t *testing.T) {
	answering(t, "", &exitError{Code: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, _ := binaryCosign.Verify(ctx, signedRef, keyRule); got != trust.Failed {
		t.Errorf("verdict %v: a killed process says nothing about the key", got)
	}
}

func TestTheNextKeyIsTriedWhileRotating(t *testing.T) {
	calls := 0
	useRunner(t, &scriptedRunner{reply: func(toolCmd) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, exit(1)
		}
		return nil, nil
	}})
	rule := keyRule
	rule.Keys = append(rule.Keys, trust.Key{Ref: "embedded: next", PEM: rule.Keys[0].PEM})
	if got, _ := binaryCosign.Verify(context.Background(), signedRef, rule); got != trust.Verified || calls != 2 {
		t.Errorf("verdict %v after %d calls", got, calls)
	}
}

func TestVerifyArguments(t *testing.T) {
	r := cosignAnswering(t, 0, 0)
	c := CosignVerifier{Tool: ToolSpec{Source: ToolSourceBinary, Binary: "cosign", Args: []string{"--timeout", "1m"}}}
	_, _ = c.Verify(context.Background(), signedRef, keylessRule)
	args := r.calls[0].Args
	want := []string{"verify", "--timeout", "1m", signedRef,
		"--certificate-oidc-issuer", keylessRule.Issuer, "--certificate-identity", keylessRule.Subject,
		"--experimental-oci11"}
	if !slices.Equal(args, want) {
		t.Errorf("args =\n%q\nwant\n%q", args, want)
	}
}

func TestTheTransparencyLogIsSkippedOnlyWhenTheRuleSaysSo(t *testing.T) {
	for _, tlog := range []bool{true, false} {
		r := cosignAnswering(t, 0, 0)
		rule := keyRule
		rule.TLog = tlog
		_, _ = binaryCosign.Verify(context.Background(), signedRef, rule)
		if got := slices.Contains(r.calls[0].Args, "--insecure-ignore-tlog"); got == tlog {
			t.Errorf("tlog %v: --insecure-ignore-tlog present = %v", tlog, got)
		}
	}
}

func TestALocalRegistryIsReachedOverHTTP(t *testing.T) {
	r := cosignAnswering(t, 0, 0)
	_, _ = binaryCosign.Verify(context.Background(), "localhost:5000/demo@sha256:x", keylessRule)
	if !slices.Contains(r.calls[0].Args, "--allow-http-registry") {
		t.Errorf("args = %q", r.calls[0].Args)
	}
}

func TestCredentialsNeverReachArgv(t *testing.T) {
	creds := func(host string) (string, string) {
		if host != "gcr.io" {
			t.Errorf("credentials asked for %q", host)
		}
		return "alice", "s3cret"
	}
	for _, source := range []ToolSource{ToolSourceBinary, ToolSourceContainer} {
		r := cosignAnswering(t, 0, 0)
		c := CosignVerifier{Tool: ToolSpec{Source: source, Binary: "cosign"}, Creds: creds}
		_, _ = c.Verify(context.Background(), signedRef, keylessRule)
		tc := r.calls[0]
		if strings.Contains(strings.Join(tc.Args, " "), "s3cret") {
			t.Errorf("%s: the password is in argv: %q", source, tc.Args)
		}
		if !slices.Contains(tc.Env, cosignPasswordEnv+"=s3cret") {
			t.Errorf("%s: the password is not in the environment", source)
		}
		if source == ToolSourceContainer && !slices.Contains(tc.Args, cosignPasswordEnv) {
			t.Errorf("container: -e %s (the name alone) missing: %q", cosignPasswordEnv, tc.Args)
		}
	}
}

func TestAnonymousDeclaresNoCredentials(t *testing.T) {
	r := cosignAnswering(t, 0, 0)
	c := CosignVerifier{Tool: ToolSpec{Source: ToolSourceContainer}, Creds: func(string) (string, string) { return "", "" }}
	_, _ = c.Verify(context.Background(), signedRef, keylessRule)
	if r.calls[0].Env != nil || slices.Contains(r.calls[0].Args, cosignUserEnv) {
		t.Errorf("an anonymous check declared credentials: %+v", r.calls[0])
	}
}

func TestAContainerMountsTheKeyAndUsesThePinnedImage(t *testing.T) {
	r := cosignAnswering(t, 0, 0)
	c := CosignVerifier{Tool: ToolSpec{Source: ToolSourceContainer}}
	_, _ = c.Verify(context.Background(), signedRef, keyRule)
	args := r.calls[0].Args
	i := slices.Index(args, "--key")
	if i < 0 || args[i+1] != cosignKeyMount {
		t.Fatalf("--key does not point at the mount: %q", args)
	}
	mount := ""
	for j, a := range args {
		if a == "-v" {
			mount = args[j+1]
		}
	}
	if !strings.HasSuffix(mount, ":"+cosignKeyMount+":ro") {
		t.Errorf("key mount = %q", mount)
	}
	if !slices.Contains(args, DefaultCosignImage) || !strings.Contains(DefaultCosignImage, "@sha256:") {
		t.Errorf("the default image must be pinned by digest: %q", args)
	}
}

// ── Identities ───────────────────────────────────────────────────────────────

func TestIdentitiesOfTheOldFormatComeFromVerify(t *testing.T) {
	out := `[{"optional":{"Subject":"keyless@distroless.iam.gserviceaccount.com","Issuer":"https://accounts.google.com"}}]`
	r := answering(t, out, nil)
	ids, err := binaryCosign.Identities(context.Background(), signedRef)
	if err != nil || len(ids) != 1 || ids[0].Subject != "keyless@distroless.iam.gserviceaccount.com" {
		t.Errorf("ids = %+v, err %v", ids, err)
	}
	if r.count() != 1 {
		t.Errorf("asked %d times; the old format names its identity at once", r.count())
	}
}

func TestIdentitiesOfTheBundleFormatComeFromTheCertificates(t *testing.T) {
	cert := fulcioCertificate(t, "https://github.com/acme/app/.github/workflows/release.yaml@refs/heads/main",
		"https://token.actions.githubusercontent.com")
	bundle := fmt.Sprintf(`{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json","verificationMaterial":{"certificate":{"rawBytes":%q}}}`, cert)
	useRunner(t, &scriptedRunner{reply: func(tc toolCmd) ([]byte, error) {
		if tc.Args[0] == "download" {
			if slices.Contains(tc.Args, "--experimental-oci11") {
				return nil, exit(1) // refused by cosign, measured
			}
			return []byte(bundle + "\n{\"mediaType\":\"x\"}\n"), nil
		}
		return []byte(`[{"critical":{},"optional":{}}]`), nil
	}})
	ids, err := binaryCosign.Identities(context.Background(), signedRef)
	want := trust.Identity{Issuer: "https://token.actions.githubusercontent.com",
		Subject: "https://github.com/acme/app/.github/workflows/release.yaml@refs/heads/main"}
	if err != nil || len(ids) != 1 || ids[0] != want {
		t.Errorf("ids = %+v, err %v", ids, err)
	}
}

func TestAnUnsignedImageClaimsNoIdentity(t *testing.T) {
	answering(t, "", exit(10))
	if ids, err := binaryCosign.Identities(context.Background(), signedRef); err != nil || ids != nil {
		t.Errorf("ids = %+v, err %v", ids, err)
	}
}

// fulcioCertificate builds a certificate shaped like Fulcio's: the subject in
// the SAN, the issuer in extension 1.3.6.1.4.1.57264.1.8. Self-signed —
// nothing reads it but certificateIdentity, which verifies nothing.
func fulcioCertificate(t *testing.T, subject, issuer string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	san, err := url.Parse(subject)
	if err != nil {
		t.Fatal(err)
	}
	value, err := asn1.Marshal(issuer)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{},
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour),
		URIs:            []*url.URL{san},
		ExtraExtensions: []pkix.Extension{{Id: oidIssuerV2, Value: value}},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(der)
}
