package netcheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"runtime"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

// Timeouts and counts are named constants, not configuration. Every scalar
// added to the config is a row in configuration/fields.go and its tests; these
// become settings when someone asks for them, not before.
const (
	resolveTimeout = 5 * time.Second
	dialTimeout    = 5 * time.Second
	tlsTimeout     = 8 * time.Second
	httpTimeout    = 8 * time.Second
	pingTimeout    = 4 * time.Second
	pingCount      = 3

	// expiryWarnWindow is how close a certificate may come to expiring before
	// the check warns. Thirty days is a renewal cycle: past it, nobody has
	// started renewing yet, so warning earlier is noise.
	expiryWarnWindow = 30 * 24 * time.Hour
)

// PingStats is what one ICMP probe run observed.
type PingStats struct {
	Sent     int
	Received int
	AvgRTT   time.Duration
}

// HTTPResult is what one HEAD request observed.
type HTTPResult struct {
	Status int
	Server string
	// TLS reports whether the request was made over TLS, so the check can say
	// which scheme it actually spoke rather than which one was intended.
	TLS bool
}

// Env is the seam every stage reaches the network through.
//
// internal/docker's dockerRunner is the precedent, with one difference: that
// one is a package-level var tests swap, which forces those tests to run
// serially. Here the seam is a parameter, so a fake is per-call and the
// package stays safe to use concurrently.
type Env interface {
	// Resolve returns the addresses host maps to. resolver is a nameserver
	// ("host" or "host:port"); empty means the system resolver.
	Resolve(ctx context.Context, host, resolver string) ([]net.IP, error)
	// ReverseLookup returns the names ip maps back to.
	ReverseLookup(ctx context.Context, ip, resolver string) ([]string, error)
	// Ping sends count ICMP echo requests.
	Ping(ctx context.Context, host string, count int) (PingStats, error)
	// DialTCP opens and immediately closes a TCP connection, reporting how
	// long the handshake took. It returns no connection: the stage wants to
	// know whether the port answers, and handing back a Conn would make every
	// fake responsible for closing it.
	DialTCP(ctx context.Context, addr string) (time.Duration, error)
	// Handshake completes a TLS handshake without verifying the chain.
	Handshake(ctx context.Context, addr, serverName string) (*tls.ConnectionState, error)
	// Head issues an HTTP HEAD request.
	Head(ctx context.Context, url string) (HTTPResult, error)
	// TrustRoots is the store the chain is verified against. nil means the
	// host's own, which is the only answer that matters in production — a
	// chain "verifies" only against a store somebody actually uses.
	//
	// It is on the seam rather than hidden inside the stage because which
	// store to trust is a property of the environment, and because a test
	// cannot get a certificate it minted into the machine's root store.
	TrustRoots() *x509.CertPool
	// Now is the clock, injected so an expiry test does not depend on the day
	// it runs.
	Now() time.Time
}

// systemEnv is the production Env: it answers from this process, on this
// machine's network stack.
type systemEnv struct{}

// SystemEnv returns the Env that talks to the real network.
func SystemEnv() Env { return systemEnv{} }

func (systemEnv) Now() time.Time { return time.Now() }

// TrustRoots returns nil: the host's own trust store is the one whose verdict
// the user lives with.
func (systemEnv) TrustRoots() *x509.CertPool { return nil }

// resolverFor builds a net.Resolver aimed at a specific nameserver, or the
// system one when none is named. The pattern mirrors status/dns_checker.go —
// PreferGo plus a Dial that redirects every query — which is the only way to
// query a chosen server without a DNS library.
func resolverFor(nameserver string) *net.Resolver {
	if nameserver == "" {
		return &net.Resolver{}
	}
	addr := nameserver
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(nameserver, "53")
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: resolveTimeout}
			return d.DialContext(ctx, network, addr)
		},
	}
}

func (systemEnv) Resolve(ctx context.Context, host, resolver string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	addrs, err := resolverFor(resolver).LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	return ips, nil
}

func (systemEnv) ReverseLookup(ctx context.Context, ip, resolver string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	return resolverFor(resolver).LookupAddr(ctx, ip)
}

func (systemEnv) Ping(ctx context.Context, host string, count int) (PingStats, error) {
	pinger, err := probing.NewPinger(host)
	if err != nil {
		return PingStats{}, err
	}
	pinger.Count = count
	pinger.Timeout = pingTimeout
	// Windows has no unprivileged ICMP socket: pro-bing's UDP mode silently
	// receives nothing there, which reads as "host down" on a host that is up.
	// Elsewhere unprivileged is right, because DevDesk must not need root.
	pinger.SetPrivileged(runtime.GOOS == "windows")

	if err := pinger.RunWithContext(ctx); err != nil {
		return PingStats{}, err
	}
	s := pinger.Statistics()
	return PingStats{Sent: s.PacketsSent, Received: s.PacketsRecv, AvgRTT: s.AvgRtt}, nil
}

func (systemEnv) DialTCP(ctx context.Context, addr string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	start := time.Now()
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return 0, err
	}
	elapsed := time.Since(start)
	_ = conn.Close() //nolint:errcheck // the probe is the dial; a close error says nothing about it
	return elapsed, nil
}

// Handshake deliberately does not verify the chain.
//
// Verification is the tls stage's job, so that it can report *which* part
// failed — untrusted root, missing intermediate, wrong hostname, expired — as
// four separate checks. Letting crypto/tls abort the handshake would collapse
// all four into one error string and lose the certificate we need to inspect,
// which is the defect this package exists to fix: the old check greped the
// leaf and never looked at the chain at all.
func (systemEnv) Handshake(ctx context.Context, addr, serverName string) (*tls.ConnectionState, error) {
	ctx, cancel := context.WithTimeout(ctx, tlsTimeout)
	defer cancel()

	d := tls.Dialer{
		NetDialer: &net.Dialer{},
		Config: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // G402: see the doc comment — the tls stage verifies, and must see a bad chain to describe it
			ServerName:         serverName,
			MinVersion:         tls.VersionTLS10, // probing, not securing: report old versions rather than refuse them
		},
	}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return nil, errNotTLSConn
	}
	state := tlsConn.ConnectionState()
	return &state, nil
}

// Head does not verify the chain either, and for a reason worth stating: a
// certificate problem must not hide whether the service answers. "The app
// works, the cert is what is broken" is a useful thing to be told, and the TLS
// checks report the cert problem on rows of their own.
func (systemEnv) Head(ctx context.Context, url string) (HTTPResult, error) {
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // G402: see the doc comment — chain problems are reported by the tls stage, not by hiding the response
				MinVersion:         tls.VersionTLS10,
			},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // the first answer is the observation; a redirect chain is a different question
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return HTTPResult{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return HTTPResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	return HTTPResult{
		Status: resp.StatusCode,
		Server: resp.Header.Get("Server"),
		TLS:    resp.TLS != nil,
	}, nil
}
