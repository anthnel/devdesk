package status

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"strings"
	"testing"
	"time"
)

// mintTestCert issues cn's certificate, signed by issuerCN's own — a nil
// issuer template self-signs, which is how a root gets minted in the first
// place. x509.CreateCertificate reads the issuer's Subject as the new
// certificate's Issuer, regardless of what tmpl.Issuer says: it is not
// consulted at all, so a self-signed leaf's Issuer would silently equal its
// own Subject if this took only one template.
func mintTestCert(t *testing.T, cn string, issuer *x509.Certificate, issuerKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(12345),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		DNSNames:     []string{cn, "www." + cn},
	}
	parent, parentKey := tmpl, key
	if issuer != nil {
		parent, parentKey = issuer, issuerKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert, key
}

// A chain's rendering carries everything SSLChecker measures, plus the PEM
// block Y copies — that second half is what this test is actually for, since
// the fields alone are already covered by CertStateOf's tests.
func TestFormatCertificateChainCarriesFieldsAndPEM(t *testing.T) {
	root, rootKey := mintTestCert(t, "Example Root CA", nil, nil)
	leaf, _ := mintTestCert(t, "example.com", root, rootKey)
	out := string(FormatCertificateChain([]*x509.Certificate{leaf}))

	for _, want := range []string{
		"Certificate 1 of 1 — leaf",
		"CN=example.com",
		"CN=Example Root CA",
		"2026-01-01T00:00:00Z",
		"2027-01-01T00:00:00Z",
		"www.example.com",
		"-----BEGIN CERTIFICATE-----",
		"-----END CERTIFICATE-----",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

// A chain of more than one certificate names the leaf apart from what
// follows it — the distinction the SSL checker's own chain check cares
// about (an intermediate the server forgot to send).
func TestFormatCertificateChainNamesTheLeafApartFromIntermediates(t *testing.T) {
	root, rootKey := mintTestCert(t, "Root CA", nil, nil)
	intermediate, intermediateKey := mintTestCert(t, "Intermediate CA", root, rootKey)
	leaf, _ := mintTestCert(t, "example.com", intermediate, intermediateKey)
	out := string(FormatCertificateChain([]*x509.Certificate{leaf, intermediate}))

	if !strings.Contains(out, "Certificate 1 of 2 — leaf") {
		t.Errorf("leaf not identified as such:\n%s", out)
	}
	if !strings.Contains(out, "Certificate 2 of 2 — intermediate") {
		t.Errorf("second certificate not identified as an intermediate:\n%s", out)
	}
}
