package scan

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/trust"
)

// DefaultCosignImage is pinned by digest, unlike the other tools' images: the
// verifier is the trust anchor, and a tag that could be republished under it
// is precisely what it exists to catch. Bumping it means measuring again —
// the exit codes below are cosign v3.1.3's (§3.82).
const DefaultCosignImage = "ghcr.io/sigstore/cosign/cosign:v3.1.3@sha256:9e5c2f2edc34351160407ca3416c61855bdf9403c3c5936e0f0be7fc261611b8"

// cosign's exit codes, measured on v3.1.3 (§3.82). Only these three are read
// as they are. 12 ("no matching signatures") is not among them: a signature
// blob the network would not serve gives 12 too, even with every identity
// accepted, so it is a failure dressed as a mismatch. 1 covers a mismatch on
// the bundle format and every failure alike.
const (
	cosignVerified    = 0
	cosignNoSignature = 10
	cosignNoTag       = 11
)

// cosignKeyMount is where a key file is mounted inside the container.
const cosignKeyMount = "/devdesk-cosign.pub"

// Registry credentials reach cosign through its environment, never argv
// (measured: it reads both). A container is handed the names alone, `-e NAME`,
// so the value travels in the engine client's environment, not its arguments.
const (
	cosignUserEnv     = "COSIGN_REGISTRY_USERNAME"
	cosignPasswordEnv = "COSIGN_REGISTRY_PASSWORD"
)

// CosignVerifier is trust.Verifier, by cosign.
type CosignVerifier struct {
	Tool ToolSpec
	// Creds returns the credentials for a registry host, empty for anonymous.
	// Nil is anonymous everywhere.
	Creds func(host string) (user, password string)
}

var _ trust.Verifier = CosignVerifier{}

// Verify asks cosign whether ref, pinned by digest, is signed the way rule says.
func (c CosignVerifier) Verify(ctx context.Context, ref string, rule trust.Rule) (trust.Verdict, error) {
	switch rule.Mode {
	case trust.ModeKeyless:
		return c.verifyKeyless(ctx, ref, rule)
	case trust.ModeKey:
		return c.verifyKey(ctx, ref, rule)
	}
	return trust.NoPolicy, nil
}

// verifyKeyless reads 0, 10 and 11 as they are, and sends everything else
// through a permissive check: a signature valid for *some* identity means the
// expected one did not sign; none at all means unsigned; anything else is a
// question that could not be asked.
func (c CosignVerifier) verifyKeyless(ctx context.Context, ref string, rule trust.Rule) (trust.Verdict, error) {
	code, err := c.run(ctx, ref, keylessArgs(rule), "")
	switch {
	case err != nil:
		return trust.Failed, err
	case code == cosignVerified:
		return trust.Verified, nil
	case code == cosignNoSignature:
		return trust.Unsigned, nil
	case code == cosignNoTag:
		return trust.Failed, fmt.Errorf("%s: not found in its registry", ref)
	}
	code, err = c.run(ctx, ref, permissiveArgs(), "")
	switch {
	case err != nil:
		return trust.Failed, err
	case code == cosignVerified:
		return trust.IdentityMismatch, nil
	case code == cosignNoSignature:
		return trust.Unsigned, nil
	}
	return trust.Failed, fmt.Errorf("cosign verify %s: exit status %d", ref, code)
}

// verifyKey tries each accepted key. Key mode cannot tell a wrong key from a
// failure — both exit 1 on the bundle format, and no permissive check exists
// for a key — so every code but 0 and 11 is Unsigned: fail-closed, and true
// either way, since no signature could be verified by that key (§3.82). A tool
// that did not run at all is still Failed: that is not an exit code.
func (c CosignVerifier) verifyKey(ctx context.Context, ref string, rule trust.Rule) (trust.Verdict, error) {
	for _, key := range rule.Keys {
		code, err := c.runWithKey(ctx, ref, key, rule.TLog)
		switch {
		case err != nil:
			return trust.Failed, err
		case code == cosignVerified:
			return trust.Verified, nil
		case code == cosignNoTag:
			return trust.Failed, fmt.Errorf("%s: not found in its registry", ref)
		}
	}
	return trust.Unsigned, nil
}

func (c CosignVerifier) runWithKey(ctx context.Context, ref string, key trust.Key, tlog bool) (int, error) {
	var tail []string
	if !tlog {
		tail = []string{"--insecure-ignore-tlog"}
	}
	if key.KMS() {
		return c.run(ctx, ref, append([]string{"--key", key.Ref}, tail...), "")
	}
	// A key is always handed over as a file DevDesk wrote, whether it came from
	// trust.yaml or was compiled in: one path for both, and the container reads
	// it through one mount. 0644 — it is public, and a container user other
	// than the owner must read it (measured: 0600 is refused).
	f, err := os.CreateTemp("", "devdesk-cosign-*.pub")
	if err != nil {
		return 0, fmt.Errorf("cosign key file: %w", err)
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := f.Write(key.PEM); err != nil {
		_ = f.Close()
		return 0, fmt.Errorf("cosign key file: %w", err)
	}
	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("cosign key file: %w", err)
	}
	if err := os.Chmod(name, 0o644); err != nil { //nolint:gosec // a public key, read by the container's user
		return 0, fmt.Errorf("cosign key file: %w", err)
	}
	return c.run(ctx, ref, append([]string{"--key", name}, tail...), name)
}

func keylessArgs(rule trust.Rule) []string {
	args := []string{"--certificate-oidc-issuer", rule.Issuer}
	if rule.SubjectRegexp != "" {
		return append(args, "--certificate-identity-regexp", rule.SubjectRegexp)
	}
	return append(args, "--certificate-identity", rule.Subject)
}

func permissiveArgs() []string {
	return []string{"--certificate-identity-regexp", ".*", "--certificate-oidc-issuer-regexp", ".*"}
}

// run executes one `cosign verify` and returns its exit code. An error is a
// tool that could not be run or was stopped, never a non-zero exit.
func (c CosignVerifier) run(ctx context.Context, ref string, extra []string, keyFile string) (int, error) {
	_, err := runner.Run(ctx, c.verifyCmd(ref, extra, keyFile), nil)
	return exitCode(ctx, err)
}

// exitCode separates cosign's answer from a failure to get one. A context
// that ended makes any code meaningless: the process was killed.
func exitCode(ctx context.Context, err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	if ctx.Err() != nil {
		return 0, fmt.Errorf("cosign: %w", ctx.Err())
	}
	var exit *exitError
	if errors.As(err, &exit) {
		return exit.Code, nil
	}
	return 0, err
}

// verifyCmd builds `cosign verify`: the user's arguments right after the
// subcommand, and `--experimental-oci11` always — DHI keeps an old-format
// signature in the OCI referrers, where cosign does not look without it, and it
// changes nothing elsewhere (measured). keyFile is a key DevDesk wrote, to
// mount in a container; "" for none.
func (c CosignVerifier) verifyCmd(ref string, extra []string, keyFile string) toolCmd {
	args := afterSubcommand(append([]string{"verify", ref}, extra...), c.Tool.Args)
	return c.command(ref, append(args, "--experimental-oci11"), keyFile)
}

// downloadCmd builds `cosign download attestation`, which refuses
// `--experimental-oci11` (measured) and is not what the user's arguments are
// for.
func (c CosignVerifier) downloadCmd(ref string) toolCmd {
	return c.command(ref, []string{"download", "attestation", ref}, "")
}

// command wraps a cosign invocation: `--allow-http-registry` for a local
// registry, the way oci.RegistryAPIBase reaches one; the credentials in the
// environment; and, for a container, the engine around it.
func (c CosignVerifier) command(ref string, args []string, keyFile string) toolCmd {
	host := registryHost(ref)
	if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
		args = append(args, "--allow-http-registry")
	}
	env := c.credentialEnv(host)

	if c.Tool.Source != ToolSourceContainer {
		cmd := toolCmd{Name: cosignBinary(c.Tool), Args: args}
		if env != nil {
			cmd.Env = append(os.Environ(), env...)
		}
		return cmd
	}

	engineArgs := []string{"run", "--rm"}
	if env != nil {
		engineArgs = append(engineArgs, "-e", cosignUserEnv, "-e", cosignPasswordEnv)
	}
	if keyFile != "" {
		engineArgs = append(engineArgs, "-v", keyFile+":"+cosignKeyMount+":ro")
		for i, a := range args {
			if a == keyFile {
				args[i] = cosignKeyMount
			}
		}
	}
	cmd := toolCmd{Name: engine.Current().Binary, Args: append(append(engineArgs, cosignImage(c.Tool.Image)), args...)}
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd
}

func (c CosignVerifier) credentialEnv(host string) []string {
	if c.Creds == nil {
		return nil
	}
	user, pass := c.Creds(host)
	if user == "" && pass == "" {
		return nil
	}
	return []string{cosignUserEnv + "=" + user, cosignPasswordEnv + "=" + pass}
}

// registryHost is the host part of a reference, "docker.io" for Docker Hub.
func registryHost(ref string) string {
	repo := trust.Repository(ref)
	host, _, _ := strings.Cut(repo, "/")
	return host
}

func cosignImage(image string) string {
	if image == "" {
		return DefaultCosignImage
	}
	return image
}

func cosignBinary(tool ToolSpec) string {
	if tool.Binary != "" {
		return tool.Binary
	}
	return "cosign"
}

// ── Identities ───────────────────────────────────────────────────────────────

// Identities returns who the signatures on ref claim to be — hints for
// continuity, which proves each one before using it.
//
// A permissive `verify` names the identity itself on the old signature format
// (`optional.Subject`/`Issuer`); on the bundle format it names nothing, and
// the claims are read from the certificates `download attestation` returns.
// Those are unverified, and that is fine only because they are never believed:
// trust.Continuity verifies each strictly before asking it of anything.
func (c CosignVerifier) Identities(ctx context.Context, ref string) ([]trust.Identity, error) {
	out, err := runner.Run(ctx, c.verifyCmd(ref, permissiveArgs(), ""), nil)
	code, err := exitCode(ctx, err)
	switch {
	case err != nil:
		return nil, err
	case code == cosignNoSignature:
		return nil, nil
	case code != cosignVerified:
		return nil, fmt.Errorf("cosign verify %s: exit status %d", ref, code)
	}
	if ids := verifyOutputIdentities(out); len(ids) > 0 {
		return ids, nil
	}

	out, err = runner.Run(ctx, c.downloadCmd(ref), nil)
	if code, err := exitCode(ctx, err); err != nil {
		return nil, err
	} else if code != 0 {
		return nil, fmt.Errorf("cosign download attestation %s: exit status %d", ref, code)
	}
	return bundleIdentities(out), nil
}

// verifyOutputIdentities reads the identities `cosign verify` prints for the
// old signature format.
func verifyOutputIdentities(out []byte) []trust.Identity {
	var entries []struct {
		Optional struct {
			Subject string `json:"Subject"`
			Issuer  string `json:"Issuer"`
		} `json:"optional"`
	}
	if json.Unmarshal(out, &entries) != nil {
		return nil
	}
	var ids []trust.Identity
	for _, e := range entries {
		ids = appendIdentity(ids, trust.Identity{Issuer: e.Optional.Issuer, Subject: e.Optional.Subject})
	}
	return ids
}

// bundleIdentities reads the claimed identity of each Sigstore bundle, one per
// line of `cosign download attestation`. A line with no certificate — a
// key-signed bundle, a bare attestation — claims none.
func bundleIdentities(out []byte) []trust.Identity {
	var ids []trust.Identity
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var b struct {
			VerificationMaterial struct {
				Certificate struct {
					RawBytes string `json:"rawBytes"`
				} `json:"certificate"`
				Chain struct {
					Certificates []struct {
						RawBytes string `json:"rawBytes"`
					} `json:"certificates"`
				} `json:"x509CertificateChain"`
			} `json:"verificationMaterial"`
		}
		if json.Unmarshal(sc.Bytes(), &b) != nil {
			continue
		}
		raw := b.VerificationMaterial.Certificate.RawBytes
		if raw == "" && len(b.VerificationMaterial.Chain.Certificates) > 0 {
			raw = b.VerificationMaterial.Chain.Certificates[0].RawBytes
		}
		if id, ok := certificateIdentity(raw); ok {
			ids = appendIdentity(ids, id)
		}
	}
	return ids
}

// Fulcio's issuer extensions: 1.8 is the current one, DER-encoded; 1.1 the
// deprecated one, the raw string.
var (
	oidIssuerV2 = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 8}
	oidIssuerV1 = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}
)

// certificateIdentity reads a Fulcio certificate's subject (its SAN) and
// issuer. It verifies nothing.
func certificateIdentity(rawBase64 string) (trust.Identity, bool) {
	der, err := base64.StdEncoding.DecodeString(rawBase64)
	if err != nil || len(der) == 0 {
		return trust.Identity{}, false
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return trust.Identity{}, false
	}
	var id trust.Identity
	switch {
	case len(cert.URIs) > 0:
		id.Subject = cert.URIs[0].String()
	case len(cert.EmailAddresses) > 0:
		id.Subject = cert.EmailAddresses[0]
	}
	for _, ext := range cert.Extensions {
		switch {
		case ext.Id.Equal(oidIssuerV2):
			var s string
			if _, err := asn1.Unmarshal(ext.Value, &s); err == nil {
				id.Issuer = s
			}
		case ext.Id.Equal(oidIssuerV1) && id.Issuer == "":
			id.Issuer = string(ext.Value)
		}
	}
	return id, id.Subject != "" && id.Issuer != ""
}

func appendIdentity(ids []trust.Identity, id trust.Identity) []trust.Identity {
	if id.Subject == "" || id.Issuer == "" {
		return ids
	}
	for _, have := range ids {
		if have == id {
			return ids
		}
	}
	return append(ids, id)
}
