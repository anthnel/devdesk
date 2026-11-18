package netdiag

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/netproxy"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-473, step 3: the Env the Diagnostics pipeline runs against is the
// model's, so a test can count what the pipeline asks the routing table.

// countingEnv is a netcheck.Env that answers without touching the network and
// counts its Route calls per address. The pipeline runs it on a goroutine in
// production, so the counts sit behind a mutex.
type countingEnv struct {
	mu     sync.Mutex
	routes map[string]int
	// proxied lists the URLs the pipeline asked the proxy question about, in
	// the order asked.
	proxied []string
}

func newCountingEnv() *countingEnv {
	return &countingEnv{routes: map[string]int{}}
}

// routeCalls is how many times Route was asked about ip.
func (e *countingEnv) routeCalls(ip string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.routes[ip]
}

func (e *countingEnv) totalRouteCalls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, c := range e.routes {
		n += c
	}
	return n
}

// proxyQuestions is the URLs ProxyFor was asked about.
func (e *countingEnv) proxyQuestions() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.proxied...)
}

// ProxyFor records the question and answers "no proxy": the proxy variables of
// the machine running the suite must not reach a test.
func (e *countingEnv) ProxyFor(u *url.URL) netproxy.Decision {
	e.mu.Lock()
	e.proxied = append(e.proxied, u.String())
	e.mu.Unlock()
	return netproxy.Decision{Kind: netproxy.DecisionNone}
}

func (e *countingEnv) Resolve(_ context.Context, host, _ string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}

func (e *countingEnv) ReverseLookup(context.Context, string, string) ([]string, error) {
	return nil, errors.New("no such host")
}

func (e *countingEnv) Ping(_ context.Context, _ string, count int) (netcheck.PingStats, error) {
	return netcheck.PingStats{Sent: count}, nil // every echo lost
}

func (e *countingEnv) DialTCP(context.Context, string) (time.Duration, error) {
	return 0, errors.New("connection refused")
}

func (e *countingEnv) Handshake(context.Context, string, string) (*tls.ConnectionState, error) {
	return nil, errors.New("connection refused")
}

func (e *countingEnv) Head(context.Context, string) (netcheck.HTTPResult, error) {
	return netcheck.HTTPResult{}, errors.New("connection refused")
}

func (e *countingEnv) Route(_ context.Context, ip net.IP) (netcheck.RouteHop, error) {
	e.mu.Lock()
	e.routes[ip.String()]++
	e.mu.Unlock()
	return netcheck.RouteHop{
		Interface: "eth0",
		Source:    net.ParseIP("192.168.1.21"),
		Gateway:   net.ParseIP("192.168.1.1"),
	}, nil
}

func (e *countingEnv) TrustRoots() *x509.CertPool { return nil }

func (e *countingEnv) Now() time.Time { return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC) }

// runDiagnostics presses enter on a form holding target, then runs every Cmd
// the pipeline returns, in order, feeding each stageDoneMsg back, until the
// results are in. Running the Cmds here is safe because the model's Env is a
// fake: nothing leaves the process.
func runDiagnostics(t *testing.T, m *Model, target string) *Model {
	t.Helper()
	m = typeInto(t, m, target)
	m, cmd := step(t, m, testutil.Key("enter"))
	for i := 0; cmd != nil && m.state == StateRunning; i++ {
		if i > len(netcheck.Steps())+1 {
			t.Fatalf("the pipeline did not settle after %d stages", i)
		}
		msg := cmd()
		done, ok := msg.(stageDoneMsg)
		if !ok {
			t.Fatalf("a stage returned %T, want stageDoneMsg", msg)
		}
		m, cmd = step(t, m, done)
	}
	if m.state != StateResults {
		t.Fatalf("state = %v after the run, want StateResults", m.state)
	}
	return m
}

func TestRunDiagnosticsUsesTheModelEnv(t *testing.T) {
	env := newCountingEnv()
	m := newTestModel(t)
	m.checkEnv = func(netcheck.Settings) netcheck.Env { return env }

	m = runDiagnostics(t, m, "127.0.0.1")

	if got := env.routeCalls("127.0.0.1"); got != 1 {
		t.Errorf("Route called %d times for 127.0.0.1, want 1 (total %d)", got, env.totalRouteCalls())
	}
	c, ok := m.results.Get(netcheck.CheckRoute)
	if !ok {
		t.Fatal("the run left no route check")
	}
	if want := "Traffic leaves through eth0"; c.Summary != want {
		t.Errorf("route summary = %q, want %q (the fake env was not the one called)", c.Summary, want)
	}
}

func TestNewUsesTheSystemEnv(t *testing.T) {
	m := New(testConfig())
	if m.checkEnv == nil {
		t.Fatal("checkEnv is nil: a run would have no Env")
	}
	if m.checkEnv(m.checkSettings()) == nil {
		t.Error("the default checkEnv returned a nil Env")
	}
}

// DEVDESK-582: the proxy row is a check like the others, so it is read through
// the model's Env, never from the real environment.
func TestRunDiagnosticsAsksTheModelEnvForTheProxy(t *testing.T) {
	env := newCountingEnv()
	m := newTestModel(t)
	m.checkEnv = func(netcheck.Settings) netcheck.Env { return env }

	m = runDiagnostics(t, m, "127.0.0.1")

	want := []string{"https://127.0.0.1:443", "http://127.0.0.1:443"}
	if got := env.proxyQuestions(); !slices.Equal(got, want) {
		t.Errorf("ProxyFor was asked about %v, want %v", got, want)
	}
	c, ok := m.results.Get(netcheck.CheckProxy)
	if !ok {
		t.Fatal("the run left no proxy check")
	}
	if c.Verdict != netcheck.NotApplicable || c.Summary != "None" || c.Reason != netcheck.ReasonProxyNone {
		t.Errorf("proxy check = %v %q (reason %q), want N/A \"None\" (proxy-none)", c.Verdict, c.Summary, c.Reason)
	}
}

// DEVDESK-582: the row is in the table, between the ICMP echo and the TCP
// connect, with the fake env's "no proxy" answer, and it is not hidden by the
// run having nothing to say about a proxy.
func TestTheProxyRowIsInTheTableBetweenIcmpAndTcp(t *testing.T) {
	m := newTestModel(t)
	m.checkEnv = func(netcheck.Settings) netcheck.Env { return newCountingEnv() }

	m = runDiagnostics(t, m, "127.0.0.1")

	var ids []netcheck.CheckID
	for _, c := range m.checksTable.Visible() {
		ids = append(ids, c.ID)
	}
	icmp, proxy, tcp := slices.Index(ids, netcheck.CheckICMP), slices.Index(ids, netcheck.CheckProxy), slices.Index(ids, netcheck.CheckTCP)
	if icmp < 0 || proxy != icmp+1 || tcp != proxy+1 {
		t.Fatalf("rows = %v, want proxy right after icmp and right before tcp", ids)
	}
	if out := m.Render(); !strings.Contains(out, "Proxy") {
		t.Errorf("the table does not show the Proxy row:\n%s", out)
	}
}

// The stage is a question the user waits on like the others, so the footer
// names it while it runs: 4 of 7, after the reachability probe.
func TestTheProgressLineNamesTheProxyStage(t *testing.T) {
	m := runningModel(t, "example.com")
	steps := netcheck.Steps()
	reach := slices.Index(steps, netcheck.StageReach)
	if reach < 0 || steps[reach+1] != netcheck.StageProxy {
		t.Fatalf("steps = %v, want the proxy stage right after reach", steps)
	}

	m, _ = step(t, m, stageDoneMsg{gen: m.runGen, stage: netcheck.StageReach, next: reach + 1, results: netcheck.ResultsOf()})

	footer := m.RenderFooter(120)
	if want := "Reading the proxy settings (4/7)"; !strings.Contains(footer, want) {
		t.Errorf("the footer does not say %q:\n%s", want, footer)
	}
}
