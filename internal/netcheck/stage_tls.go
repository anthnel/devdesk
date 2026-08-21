package netcheck

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math"
	"time"
)

// runTLS completes a handshake and then asks four separate questions of what
// came back: does the chain verify, is it valid for this name, when does it
// expire, and which protocol version was negotiated.
//
// The handshake is deliberately unverified (see systemEnv.Handshake). That is
// what makes the four questions separable: crypto/tls aborting on an untrusted
// root would collapse all of them into one error string and take the
// certificate with it. The check this replaces greped the leaf's text and never
// looked at the chain at all.
func runTLS(ctx context.Context, t Target, env Env, _ *Results) []Check {
	state, err := env.Handshake(ctx, t.Addr(), t.Host)
	if err != nil {
		return handshakeFailed(t, err)
	}
	if len(state.PeerCertificates) == 0 {
		hs := newCheck(CheckTLSHandshake, StageTLS, Unknown, "Handshake completed but no certificate was presented")
		return append([]Check{hs}, dependentsOf(CheckTLSHandshake, "Skipped — no certificate to inspect")...)
	}

	leaf := state.PeerCertificates[0]
	now := env.Now()

	hs := newCheck(CheckTLSHandshake, StageTLS, OK,
		fmt.Sprintf("Handshake completed, %d certificate(s) presented", len(state.PeerCertificates)))
	hs.fact("Negotiated version", tlsVersionName(state.Version))
	hs.fact("Subject", leaf.Subject.CommonName)
	hs.fact("Issuer", leaf.Issuer.CommonName)

	expiry := expiryCheck(leaf, now)

	return []Check{
		hs,
		chainCheck(state, now, expiry.Verdict, env.TrustRoots()),
		hostnameCheck(leaf, t.Host),
		expiry,
		versionCheck(state.Version),
	}
}

// handshakeFailed tells "this port does not speak TLS" apart from "TLS is
// broken here", because they are different answers and only one is a problem.
//
// The signal is crypto/tls's own: a server that answers with something that is
// not a TLS record produces a RecordHeaderError. That is an observation, not a
// guess from the port number — which matters, since guessing from the port is
// exactly the heuristic §3.8 settled against elsewhere.
//
// The limit, stated rather than discovered: a server that closes the connection
// without answering at all is indistinguishable from a broken TLS endpoint, and
// is reported as a failure.
func handshakeFailed(t Target, err error) []Check {
	var rhe tls.RecordHeaderError
	if errors.As(err, &rhe) {
		c := newCheck(CheckTLSHandshake, StageTLS, NotApplicable,
			fmt.Sprintf("Port %d does not speak TLS", t.Port))
		return append([]Check{c}, dependentsOf("", "Port does not speak TLS")...)
	}

	c := newCheck(CheckTLSHandshake, StageTLS, Fail, "TLS handshake failed")
	c.fact("Error", err.Error())
	return append([]Check{c}, dependentsOf(CheckTLSHandshake, "Skipped — the handshake did not complete")...)
}

// dependentsOf marks the four certificate checks as unanswerable. `because` is
// empty when the port simply does not speak TLS: nothing failed, so nothing is
// blaming anything.
func dependentsOf(because CheckID, summary string) []Check {
	ids := []CheckID{CheckTLSChain, CheckTLSHostname, CheckTLSExpiry, CheckTLSVersion}
	out := make([]Check, 0, len(ids))
	for _, id := range ids {
		c := newCheck(id, StageTLS, NotApplicable, summary)
		c.Because = because
		out = append(out, c)
	}
	return out
}

// chainCheck verifies the leaf against the system trust store, using only the
// intermediates the server actually sent.
//
// Two things are reported that the old check could not see at all:
//
//   - whether the chain verifies, and why it does not — an unknown authority
//     and an incomplete chain are different problems with different fixes.
//   - whether the server sent its intermediates, *independently of whether
//     verification succeeded*. A chain the local platform completes for itself
//     still breaks on clients that do not, so it is a Warn rather than a pass.
//
// An expired certificate makes verification fail for a reason the expiry check
// already reports, so the chain comes back NotApplicable pointing at it rather
// than saying the same thing twice.
func chainCheck(state *tls.ConnectionState, now time.Time, expiryVerdict Verdict, roots *x509.CertPool) Check {
	leaf := state.PeerCertificates[0]
	intermediates := x509.NewCertPool()
	for _, cert := range state.PeerCertificates[1:] {
		intermediates.AddCert(cert)
	}

	c := newCheck(CheckTLSChain, StageTLS, OK, "")
	c.fact("Certificates presented", fmt.Sprintf("%d", len(state.PeerCertificates)))
	for _, cert := range state.PeerCertificates[1:] {
		c.fact("Intermediate", cert.Subject.CommonName)
	}

	if expiryVerdict == Fail {
		c.Verdict = NotApplicable
		c.Because = CheckTLSExpiry
		c.Summary = "Not verified — the certificate is expired"
		return c
	}

	_, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   now,
	})

	selfSigned := len(state.PeerCertificates) == 1 && isSelfSigned(leaf)

	switch {
	case err != nil && selfSigned:
		c.Verdict = Fail
		c.Summary = "Certificate is self-signed and not trusted"
		c.fact("Error", err.Error())
	case err != nil && len(state.PeerCertificates) == 1:
		c.Verdict = Fail
		c.Summary = "Chain does not verify — the server sent no intermediate certificates"
		c.fact("Error", err.Error())
	case err != nil:
		c.Verdict = Fail
		c.Summary = "Chain does not verify against the system trust store"
		c.fact("Error", err.Error())
	case len(state.PeerCertificates) == 1 && !selfSigned:
		c.Verdict = Warn
		c.Summary = "Chain verifies here, but the server sent no intermediates — other clients may fail"
	default:
		c.Summary = fmt.Sprintf("Chain verifies against the system trust store (%d certificate(s))",
			len(state.PeerCertificates))
	}
	return c
}

// hostnameCheck asks whether the certificate is valid for the name that was
// dialled. It is separate from the chain on purpose: a perfectly trusted
// certificate for the wrong name and an untrusted certificate for the right one
// are different problems, and the old check reported neither.
func hostnameCheck(leaf *x509.Certificate, host string) Check {
	c := newCheck(CheckTLSHostname, StageTLS, OK, "")
	for _, name := range leaf.DNSNames {
		c.fact("SAN", name)
	}
	for _, ip := range leaf.IPAddresses {
		c.fact("SAN", ip.String())
	}

	if err := leaf.VerifyHostname(host); err != nil {
		c.Verdict = Fail
		c.Summary = fmt.Sprintf("Certificate is not valid for %s", host)
		c.fact("Error", err.Error())
		return c
	}
	c.Summary = fmt.Sprintf("Certificate is valid for %s", host)
	return c
}

// expiryCheck reports the validity window in days, which is the unit the
// decision is actually made in.
func expiryCheck(leaf *x509.Certificate, now time.Time) Check {
	c := newCheck(CheckTLSExpiry, StageTLS, OK, "")
	c.fact("Not before", leaf.NotBefore.UTC().Format(time.RFC3339))
	c.fact("Not after", leaf.NotAfter.UTC().Format(time.RFC3339))

	switch {
	case now.Before(leaf.NotBefore):
		c.Verdict = Fail
		c.Summary = fmt.Sprintf("Certificate is not valid until %s (in %s)",
			leaf.NotBefore.UTC().Format(time.DateOnly), inDays(leaf.NotBefore.Sub(now)))
	case now.After(leaf.NotAfter):
		c.Verdict = Fail
		c.Summary = fmt.Sprintf("Certificate expired %s ago, on %s",
			inDays(now.Sub(leaf.NotAfter)), leaf.NotAfter.UTC().Format(time.DateOnly))
	case leaf.NotAfter.Sub(now) < expiryWarnWindow:
		c.Verdict = Warn
		c.Summary = fmt.Sprintf("Certificate expires in %s, on %s",
			inDays(leaf.NotAfter.Sub(now)), leaf.NotAfter.UTC().Format(time.DateOnly))
	default:
		c.Summary = fmt.Sprintf("Certificate is valid for another %s, until %s",
			inDays(leaf.NotAfter.Sub(now)), leaf.NotAfter.UTC().Format(time.DateOnly))
	}
	return c
}

// versionCheck reports the negotiated protocol version. TLS 1.0 and 1.1 are a
// Warn and not a Fail: the connection works, which is what Fail is reserved
// for, and both are deprecated rather than broken.
func versionCheck(version uint16) Check {
	name := tlsVersionName(version)
	c := newCheck(CheckTLSVersion, StageTLS, OK, "Negotiated "+name)
	c.fact("Version", name)
	if version < tls.VersionTLS12 {
		c.Verdict = Warn
		c.Summary = "Negotiated " + name + ", which is deprecated"
	}
	return c
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("unknown (0x%04x)", v)
	}
}

// inDays renders a duration in whole days, the unit certificate decisions are
// made in. Under a day it says so rather than rounding to zero, which would
// read as "expires today" on something with hours left.
func inDays(d time.Duration) string {
	days := int(math.Floor(d.Hours() / 24))
	switch {
	case days < 1:
		return "less than a day"
	case days == 1:
		return "1 day"
	default:
		return fmt.Sprintf("%d days", days)
	}
}

// isSelfSigned reports whether a certificate issued itself.
//
// It cannot be x509.CheckSignatureFrom: that one enforces CA constraints, and a
// self-signed *leaf* — the exact case worth naming, since it is what a
// development server presents — carries neither IsCA nor KeyUsageCertSign, so
// it is refused for a reason that has nothing to do with who signed it.
//
// Matching the issuer to the subject alone would accept a certificate issued by
// a different key under the same name, so the signature is verified too.
func isSelfSigned(cert *x509.Certificate) bool {
	if !bytes.Equal(cert.RawIssuer, cert.RawSubject) {
		return false
	}
	return cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) == nil
}
