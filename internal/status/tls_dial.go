package status

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/netproxy"
)

// certDialer opens the connection the SSL check reads a chain from. The zero
// value follows the proxy environment variables; tests swap it.
var certDialer = netproxy.Dialer{}

// certRootCAs is the pool the SSL check verifies against; nil means the system
// roots. Tests set it so a self-signed server can pass verification.
var certRootCAs *x509.CertPool

// hostWithPort defaults a bare target to :443, the assumption SSLChecker and
// the certificate viewer both make about an unqualified target.
func hostWithPort(target string) string {
	if strings.Contains(target, ":") {
		return target
	}
	return target + ":443"
}

// FetchCertificateChain dials target and returns the certificate chain the
// server presented, leaf first — possibly empty, which the caller (not this
// function) is left to judge: SSLChecker and the certificate viewer report an
// empty chain differently. It is the dial SSLChecker.Check already pays on
// every refresh, pulled out so the document viewer can ask the same question
// again without a second copy of the dial.
func FetchCertificateChain(target string, timeout time.Duration) ([]*x509.Certificate, error) {
	return FetchCertificateChainContext(context.Background(), target, timeout)
}

// FetchCertificateChainContext is FetchCertificateChain bounded by ctx as well
// as by timeout. The connection follows the proxy environment variables like
// the HTTPS monitor does; a failure of the proxy step is a *netproxy.Error,
// while a certificate or handshake failure of the target is returned as it
// would be without a proxy. The timeout also covers a proxy that never answers.
func FetchCertificateChainContext(ctx context.Context, target string, timeout time.Duration) ([]*x509.Certificate, error) {
	hostport := hostWithPort(target)
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := certDialer.DialTLS(ctx, hostport, &tls.Config{
		ServerName:         host,
		RootCAs:            certRootCAs,
		InsecureSkipVerify: false, // Validate certificates
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	return conn.ConnectionState().PeerCertificates, nil
}
