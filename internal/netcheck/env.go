package netcheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptrace"
	"runtime"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

// Defaults for Settings. These were the package's constants, and the comment
// they carried said they would become settings when somebody asked for them.
// Somebody has, so the constants are now the fallback rather than the rule.
const (
	// DefaultCheckTimeout is the old maximum of the five per-stage timeouts
	// (5 s for DNS and the dial, 8 s for TLS and HTTP, 4 s for the ping). Taking
	// the maximum is what keeps a target that answers today answering; the cost
	// is that an unreachable host spends longer on the stages that were quicker.
	DefaultCheckTimeout = 8 * time.Second
	// DefaultPingCount is how many echo requests a reachability probe sends.
	DefaultPingCount = 3
	// DefaultExpiryWarnWindow is how close a certificate may come to expiring
	// before the check warns. Thirty days is a renewal cycle: past it, nobody
	// has started renewing yet, so warning earlier is noise.
	DefaultExpiryWarnWindow = 30 * 24 * time.Hour
)

// Settings are the dials the user turns. They travel beside Env rather than on
// it because Env is the seam to the network and these are policy: PingCount and
// ExpiryWarnWindow are read by stages, not by any network call, so putting them
// behind the seam would make every fake answer for a preference.
//
// It is deliberately closed at three fields. Everything else the netdiag view
// configures — the traceroute hop limit, the ports refresh — belongs to callers
// that do not go through this pipeline at all.
type Settings struct {
	// CheckTimeout bounds every probe: the resolution, the ping, the dial, the
	// handshake and the HTTP request each get it in full.
	CheckTimeout time.Duration
	// PingCount is how many ICMP echo requests the reachability stage sends.
	PingCount int
	// ExpiryWarnWindow is how close a certificate may come to expiring before
	// the expiry check warns.
	ExpiryWarnWindow time.Duration
}

// DefaultSettings returns what the package used to hardcode.
func DefaultSettings() Settings {
	return Settings{
		CheckTimeout:     DefaultCheckTimeout,
		PingCount:        DefaultPingCount,
		ExpiryWarnWindow: DefaultExpiryWarnWindow,
	}
}

// Normalized fills in what a caller left at zero.
//
// The zero value must not mean "wait forever" or "send no packets": Settings is
// a plain struct, so a caller that builds one by hand and forgets a field would
// otherwise get a dial with no deadline — a hang rather than a verdict. Every
// entry point normalizes, so no stage has to check.
//
// It is exported because a caller assembling one from a config file has a real
// question to ask of it — "what will actually be used" — and answering that by
// reimplementing the fallbacks is how two rules for one question start.
func (s Settings) Normalized() Settings {
	d := DefaultSettings()
	if s.CheckTimeout <= 0 {
		s.CheckTimeout = d.CheckTimeout
	}
	if s.PingCount <= 0 {
		s.PingCount = d.PingCount
	}
	if s.ExpiryWarnWindow <= 0 {
		s.ExpiryWarnWindow = d.ExpiryWarnWindow
	}
	return s
}

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
	// DNSDuration, ConnectDuration and TLSDuration are zero when that phase
	// did not happen for this request — DNSDuration when the host is a
	// literal IP, TLSDuration on a plain http:// target.
	DNSDuration     time.Duration
	ConnectDuration time.Duration
	TLSDuration     time.Duration
	// TTFB is time to first response byte, measured from the start of the
	// request.
	TTFB time.Duration
	// Total is the full round trip, measured the same way DialTCP's elapsed
	// time is: wrapping the call rather than summing the phases.
	Total time.Duration
}

// RouteHop is how a packet to one address leaves this machine.
//
// Every field may be empty, and each emptiness means something: no gateway is a
// directly-connected destination, no source is a stack that did not say which
// address it would use. They are net.IP rather than strings so a caller cannot
// be handed "<nil>".
type RouteHop struct {
	Interface string
	Source    net.IP
	Gateway   net.IP
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
	// Route reports how a packet to ip would leave this machine. It sends
	// nothing: it asks the local routing table, which is why it is the one
	// method here that answers without touching the network.
	Route(ctx context.Context, ip net.IP) (RouteHop, error)
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
type systemEnv struct{ timeout time.Duration }

// SystemEnv returns the Env that talks to the real network, bounded by s.
func SystemEnv(s Settings) Env { return systemEnv{timeout: s.Normalized().CheckTimeout} }

func (systemEnv) Now() time.Time { return time.Now() }

// TrustRoots returns nil: the host's own trust store is the one whose verdict
// the user lives with.
func (systemEnv) TrustRoots() *x509.CertPool { return nil }

// resolverFor builds a net.Resolver aimed at a specific nameserver, or the
// system one when none is named. The pattern mirrors status/dns_checker.go —
// PreferGo plus a Dial that redirects every query — which is the only way to
// query a chosen server without a DNS library.
func resolverFor(nameserver string, timeout time.Duration) *net.Resolver {
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
			d := net.Dialer{Timeout: timeout}
			return d.DialContext(ctx, network, addr)
		},
	}
}

func (e systemEnv) Resolve(ctx context.Context, host, resolver string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	addrs, err := resolverFor(resolver, e.timeout).LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	return ips, nil
}

func (e systemEnv) ReverseLookup(ctx context.Context, ip, resolver string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	return resolverFor(resolver, e.timeout).LookupAddr(ctx, ip)
}

func (e systemEnv) Ping(ctx context.Context, host string, count int) (PingStats, error) {
	pinger, err := probing.NewPinger(host)
	if err != nil {
		return PingStats{}, err
	}
	pinger.Count = count
	pinger.Timeout = e.timeout
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

func (e systemEnv) DialTCP(ctx context.Context, addr string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
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
func (e systemEnv) Handshake(ctx context.Context, addr, serverName string) (*tls.ConnectionState, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
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
func (e systemEnv) Head(ctx context.Context, url string) (HTTPResult, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
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

	var timing httpTiming
	ctx = httptrace.WithClientTrace(ctx, timing.clientTrace())

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return HTTPResult{}, err
	}
	start := time.Now()
	resp, err := client.Do(req)
	total := time.Since(start)
	if err != nil {
		return HTTPResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	return HTTPResult{
		Status:          resp.StatusCode,
		Server:          resp.Header.Get("Server"),
		TLS:             resp.TLS != nil,
		DNSDuration:     timing.dnsDuration(),
		ConnectDuration: timing.connectDuration(),
		TLSDuration:     timing.tlsDuration(),
		TTFB:            timing.ttfb(start),
		Total:           total,
	}, nil
}

// httpTiming records the timestamps an httptrace.ClientTrace observes over
// the course of one request. Its callbacks run sequentially on the goroutine
// performing the round trip — systemEnv.Head issues one request at a time, so
// there is nothing here for two callbacks to race on.
type httpTiming struct {
	dnsStart, dnsDone         time.Time
	connectStart, connectDone time.Time
	tlsStart, tlsDone         time.Time
	gotFirstResponseByte      time.Time
}

func (t *httpTiming) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart:             func(httptrace.DNSStartInfo) { t.dnsStart = time.Now() },
		DNSDone:              func(httptrace.DNSDoneInfo) { t.dnsDone = time.Now() },
		ConnectStart:         func(string, string) { t.connectStart = time.Now() },
		ConnectDone:          func(string, string, error) { t.connectDone = time.Now() },
		TLSHandshakeStart:    func() { t.tlsStart = time.Now() },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { t.tlsDone = time.Now() },
		GotFirstResponseByte: func() { t.gotFirstResponseByte = time.Now() },
	}
}

func (t httpTiming) dnsDuration() time.Duration {
	if t.dnsStart.IsZero() || t.dnsDone.IsZero() {
		return 0
	}
	return t.dnsDone.Sub(t.dnsStart)
}

func (t httpTiming) connectDuration() time.Duration {
	if t.connectStart.IsZero() || t.connectDone.IsZero() {
		return 0
	}
	return t.connectDone.Sub(t.connectStart)
}

func (t httpTiming) tlsDuration() time.Duration {
	if t.tlsStart.IsZero() || t.tlsDone.IsZero() {
		return 0
	}
	return t.tlsDone.Sub(t.tlsStart)
}

func (t httpTiming) ttfb(start time.Time) time.Duration {
	if t.gotFirstResponseByte.IsZero() {
		return 0
	}
	return t.gotFirstResponseByte.Sub(start)
}
