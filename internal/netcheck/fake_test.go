package netcheck

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"testing"
	"time"
)

// testNow is the clock every test runs against, so an expiry assertion does not
// depend on the day the suite runs.
var testNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

// fakeEnv is an Env whose every primitive can be overridden one at a time.
// Anything left nil answers plausibly, so a test states only the condition it
// is about.
type fakeEnv struct {
	resolve   func(ctx context.Context, host, resolver string) ([]net.IP, error)
	reverse   func(ctx context.Context, ip, resolver string) ([]string, error)
	ping      func(ctx context.Context, host string, count int) (PingStats, error)
	dial      func(ctx context.Context, addr string) (time.Duration, error)
	handshake func(ctx context.Context, addr, serverName string) (*tls.ConnectionState, error)
	head      func(ctx context.Context, url string) (HTTPResult, error)
	route     func(ctx context.Context, ip net.IP) (RouteHop, error)
	roots     *x509.CertPool
	now       time.Time
}

func (f fakeEnv) Resolve(ctx context.Context, host, resolver string) ([]net.IP, error) {
	if f.resolve != nil {
		return f.resolve(ctx, host, resolver)
	}
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}

func (f fakeEnv) ReverseLookup(ctx context.Context, ip, resolver string) ([]string, error) {
	if f.reverse != nil {
		return f.reverse(ctx, ip, resolver)
	}
	return nil, errors.New("no such host")
}

func (f fakeEnv) Ping(ctx context.Context, host string, count int) (PingStats, error) {
	if f.ping != nil {
		return f.ping(ctx, host, count)
	}
	return PingStats{Sent: count, Received: count, AvgRTT: 12 * time.Millisecond}, nil
}

func (f fakeEnv) Route(ctx context.Context, ip net.IP) (RouteHop, error) {
	if f.route != nil {
		return f.route(ctx, ip)
	}
	return RouteHop{
		Interface: "eth0",
		Source:    net.ParseIP("192.168.1.21"),
		Gateway:   net.ParseIP("192.168.1.1"),
	}, nil
}

func (f fakeEnv) DialTCP(ctx context.Context, addr string) (time.Duration, error) {
	if f.dial != nil {
		return f.dial(ctx, addr)
	}
	return 8 * time.Millisecond, nil
}

func (f fakeEnv) Handshake(ctx context.Context, addr, serverName string) (*tls.ConnectionState, error) {
	if f.handshake != nil {
		return f.handshake(ctx, addr, serverName)
	}
	return nil, tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}
}

func (f fakeEnv) Head(ctx context.Context, url string) (HTTPResult, error) {
	if f.head != nil {
		return f.head(ctx, url)
	}
	return HTTPResult{Status: 200, Server: "nginx"}, nil
}

func (f fakeEnv) TrustRoots() *x509.CertPool { return f.roots }

func (f fakeEnv) Now() time.Time {
	if f.now.IsZero() {
		return testNow
	}
	return f.now
}

// --- certificate minting -----------------------------------------------------

var serial int64

func nextSerial() *big.Int {
	serial++
	return big.NewInt(serial)
}

// mint issues a certificate from tmpl. A nil parent means self-signed.
func mint(t *testing.T, tmpl, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signee, signer := parent, parentKey
	if parent == nil {
		signee, signer = tmpl, key
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, signee, &key.PublicKey, signer)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert, key
}

func caTemplate(cn string) *x509.Certificate {
	return &x509.Certificate{
		SerialNumber:          nextSerial(),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             testNow.Add(-365 * 24 * time.Hour),
		NotAfter:              testNow.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
}

// chainOpts describes the certificate situation a test wants to reproduce.
type chainOpts struct {
	dnsNames  []string
	ips       []net.IP
	notBefore time.Time
	notAfter  time.Time
	// withIntermediate mints root -> intermediate -> leaf and has the server
	// present both leaf and intermediate.
	withIntermediate bool
	// omitIntermediate presents the leaf alone even though it was signed by an
	// intermediate — the "server forgot its chain" case.
	omitIntermediate bool
	// selfSigned mints a leaf that signed itself.
	selfSigned bool
	version    uint16
}

// buildChain returns a connection state as a server would present it, plus the
// pool a client would have to trust for it to verify.
func buildChain(t *testing.T, o chainOpts) (*tls.ConnectionState, *x509.CertPool) {
	t.Helper()

	notAfter := o.notAfter
	if notAfter.IsZero() {
		notAfter = testNow.Add(90 * 24 * time.Hour)
	}
	version := o.version
	if version == 0 {
		version = tls.VersionTLS13
	}
	notBefore := o.notBefore
	if notBefore.IsZero() {
		notBefore = testNow.Add(-24 * time.Hour)
		// An expired certificate still has to have been valid once, or x509
		// rejects the template rather than the expiry.
		if !notAfter.After(notBefore) {
			notBefore = notAfter.Add(-365 * 24 * time.Hour)
		}
	}
	leafTmpl := &x509.Certificate{
		SerialNumber:          nextSerial(),
		Subject:               pkix.Name{CommonName: "leaf"},
		DNSNames:              o.dnsNames,
		IPAddresses:           o.ips,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	if o.selfSigned {
		leaf, _ := mint(t, leafTmpl, nil, nil)
		return &tls.ConnectionState{Version: version, PeerCertificates: []*x509.Certificate{leaf}}, x509.NewCertPool()
	}

	root, rootKey := mint(t, caTemplate("test root"), nil, nil)
	pool := x509.NewCertPool()
	pool.AddCert(root)

	signerCert, signerKey := root, rootKey
	var intermediate *x509.Certificate
	if o.withIntermediate || o.omitIntermediate {
		intermediate, signerKey = mint(t, caTemplate("test intermediate"), root, rootKey)
		signerCert = intermediate
	}

	leaf, _ := mint(t, leafTmpl, signerCert, signerKey)

	presented := []*x509.Certificate{leaf}
	if o.withIntermediate && !o.omitIntermediate {
		presented = append(presented, intermediate)
	}
	return &tls.ConnectionState{Version: version, PeerCertificates: presented}, pool
}

// --- assertions --------------------------------------------------------------

func verdictOf(t *testing.T, checks []Check, id CheckID) Verdict {
	t.Helper()
	for _, c := range checks {
		if c.ID == id {
			return c.Verdict
		}
	}
	t.Fatalf("check %q was not produced; got %v", id, idsOf(checks))
	return Unknown
}

func checkNamed(t *testing.T, checks []Check, id CheckID) Check {
	t.Helper()
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("check %q was not produced; got %v", id, idsOf(checks))
	return Check{}
}

func idsOf(checks []Check) []CheckID {
	out := make([]CheckID, 0, len(checks))
	for _, c := range checks {
		out = append(out, c.ID)
	}
	return out
}

// handshakeReturning is the common case: a fake handshake that hands back a
// prepared connection state.
func handshakeReturning(state *tls.ConnectionState) func(context.Context, string, string) (*tls.ConnectionState, error) {
	return func(context.Context, string, string) (*tls.ConnectionState, error) { return state, nil }
}
