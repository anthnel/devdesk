package status

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"strings"
	"time"
)

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
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", hostWithPort(target), &tls.Config{
		InsecureSkipVerify: false, // Validate certificates
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	return conn.ConnectionState().PeerCertificates, nil
}
