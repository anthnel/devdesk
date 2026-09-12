package status

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"strings"
	"time"
)

// FormatCertificateChain renders a certificate chain for the document viewer:
// the fields SSLChecker already measures, plus every certificate's PEM block
// — which is what makes `Y` (copy the document) worth having on this source.
func FormatCertificateChain(certs []*x509.Certificate) []byte {
	var b strings.Builder
	for i, cert := range certs {
		role := "leaf"
		if i > 0 {
			role = "intermediate"
		}
		fmt.Fprintf(&b, "Certificate %d of %d — %s\n", i+1, len(certs), role)
		fmt.Fprintf(&b, "  Subject:     %s\n", cert.Subject.String())
		fmt.Fprintf(&b, "  Issuer:      %s\n", cert.Issuer.String())
		fmt.Fprintf(&b, "  Serial:      %s\n", cert.SerialNumber.Text(16))
		fmt.Fprintf(&b, "  Not Before:  %s\n", cert.NotBefore.UTC().Format(time.RFC3339))
		fmt.Fprintf(&b, "  Not After:   %s\n", cert.NotAfter.UTC().Format(time.RFC3339))
		fmt.Fprintf(&b, "  Signature:   %s\n", cert.SignatureAlgorithm.String())
		fmt.Fprintf(&b, "  Public Key:  %s\n", publicKeyDescription(cert))
		if len(cert.DNSNames) > 0 {
			fmt.Fprintf(&b, "  DNS Names:   %s\n", strings.Join(cert.DNSNames, ", "))
		}
		fmt.Fprintf(&b, "  SHA-256:     %s\n\n", fingerprint(cert))
		b.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
		if i < len(certs)-1 {
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

func fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	hexSum := hex.EncodeToString(sum[:])
	parts := make([]string, len(sum))
	for i := range sum {
		parts[i] = strings.ToUpper(hexSum[i*2 : i*2+2])
	}
	return strings.Join(parts, ":")
}

// publicKeyDescription reads the key type off the parsed public key rather
// than PublicKeyAlgorithm alone: that field names the family (RSA, ECDSA)
// but not the size, and the size is what actually says how strong the key is.
func publicKeyDescription(cert *x509.Certificate) string {
	switch pub := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		return fmt.Sprintf("RSA %d bit", pub.N.BitLen())
	case *ecdsa.PublicKey:
		return fmt.Sprintf("ECDSA %s", pub.Curve.Params().Name)
	case ed25519.PublicKey:
		return "Ed25519"
	default:
		return cert.PublicKeyAlgorithm.String()
	}
}
