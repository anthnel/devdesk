package mcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/netcheck"
)

func TestNetCheckReportsAVerdictPerStage(t *testing.T) {
	fakeCacheHome(t)
	stubNetwork(t, reachableHost())

	var out netCheckOut
	callTool(t, connect(t, testEnv(nil)), "net_check", map[string]any{"host": "example.test"}, &out)

	if out.Host != "example.test" {
		t.Errorf("host = %q", out.Host)
	}
	if out.Port != defaultCheckPort {
		t.Errorf("port = %d, want the default %d", out.Port, defaultCheckPort)
	}
	if len(out.Checks) == 0 {
		t.Fatal("no checks were reported")
	}

	stages := map[string]bool{}
	for _, c := range out.Checks {
		stages[c.Stage] = true
		if c.Verdict == "" {
			t.Errorf("check %s carries no verdict", c.ID)
		}
		if c.Means == "" {
			t.Errorf("check %s carries no explanation — Means is what makes a verdict actionable", c.ID)
		}
	}
	for _, want := range []string{"resolve", "connect"} {
		if !stages[want] {
			t.Errorf("no check from the %s stage; got %v", want, stages)
		}
	}
}

// The headline is the worst verdict among the checks that ran, which is what an
// agent reads before deciding whether to look at the rest.
func TestTheHeadlineIsTheWorstVerdict(t *testing.T) {
	fakeCacheHome(t)
	stubNetwork(t, unresolvableHost())

	var out netCheckOut
	callTool(t, connect(t, testEnv(nil)), "net_check", map[string]any{"host": "nowhere.test"}, &out)

	if out.Verdict != "FAIL" {
		t.Errorf("verdict = %q, want FAIL — the name does not resolve", out.Verdict)
	}
}

// The distinction an agent reading a wall of N/A needs most: a check blocked by
// an upstream failure names what blocked it, where one that is simply irrelevant
// to this target does not.
func TestABlockedCheckNamesWhatBlockedIt(t *testing.T) {
	fakeCacheHome(t)
	stubNetwork(t, unresolvableHost())

	var out netCheckOut
	callTool(t, connect(t, testEnv(nil)), "net_check", map[string]any{"host": "nowhere.test"}, &out)

	blocked := 0
	for _, c := range out.Checks {
		if c.Verdict == "N/A" && c.Because != "" {
			blocked++
		}
	}
	if blocked == 0 {
		t.Errorf("no check says why it did not run, though resolution failed: %+v", out.Checks)
	}
}

// A port under 1 or over 65535 is refused before any probe, and as a tool error
// rather than a protocol one — the question was wrong, the transport was not.
func TestAnImpossiblePortIsRefusedWithoutProbing(t *testing.T) {
	fakeCacheHome(t)
	probed := false
	env := reachableHost()
	env.onResolve = func() { probed = true }
	stubNetwork(t, env)

	res := callToolExpectingError(t, connect(t, testEnv(nil)), "net_check",
		map[string]any{"host": "example.test", "port": 99999})

	if !strings.Contains(res, "port") {
		t.Errorf("the error does not name the offending field: %s", res)
	}
	if probed {
		t.Error("a probe ran for a target that could never be valid")
	}
}

func TestAnEmptyHostIsRefused(t *testing.T) {
	fakeCacheHome(t)
	stubNetwork(t, reachableHost())

	res := callToolExpectingError(t, connect(t, testEnv(nil)), "net_check", map[string]any{"host": "  "})

	if !strings.Contains(strings.ToLower(res), "target") {
		t.Errorf("the error does not say what is missing: %s", res)
	}
}

// The dials come from `network:` and not from a tool argument: they are the
// operator's calibration of this machine, and a caller that could override them
// could make the server hammer a host or hang on one.
func TestTheProbeUsesTheContextsOwnDials(t *testing.T) {
	fakeCacheHome(t)
	env := reachableHost()
	stubNetwork(t, env)

	served := testEnv(nil)
	served.Config.Network.PingCount = 7
	served.Config.Network.CheckTimeout = 3

	var out netCheckOut
	callTool(t, connect(t, served), "net_check", map[string]any{"host": "example.test"}, &out)

	if env.pingCount != 7 {
		t.Errorf("the reachability stage sent %d echo requests, want the configured 7", env.pingCount)
	}
}

// A zero left in a hand-edited config must not become a dial with no deadline.
func TestAZeroInTheConfigBecomesTheDefault(t *testing.T) {
	served := testEnv(nil)
	served.Config.Network.CheckTimeout = 0
	served.Config.Network.PingCount = 0
	served.Config.Network.CertExpiryWarnDays = 0

	got := checkSettings(served)
	want := netcheck.DefaultSettings()

	if got.CheckTimeout != want.CheckTimeout {
		t.Errorf("check_timeout = %v, want the default %v", got.CheckTimeout, want.CheckTimeout)
	}
	if got.PingCount != want.PingCount {
		t.Errorf("ping_count = %d, want the default %d", got.PingCount, want.PingCount)
	}
	if got.ExpiryWarnWindow != want.ExpiryWarnWindow {
		t.Errorf("expiry window = %v, want the default %v", got.ExpiryWarnWindow, want.ExpiryWarnWindow)
	}
}

// ── a network that answers however the test says ────────────────────────────

type fakeNetEnv struct {
	addrs     []net.IP
	resolveEr error
	dialEr    error
	pingEr    error
	pingCount int
	onResolve func()
}

func (e *fakeNetEnv) Resolve(context.Context, string, string) ([]net.IP, error) {
	if e.onResolve != nil {
		e.onResolve()
	}
	return e.addrs, e.resolveEr
}

func (e *fakeNetEnv) ReverseLookup(context.Context, string, string) ([]string, error) {
	return nil, errors.New("no reverse record")
}

func (e *fakeNetEnv) Ping(_ context.Context, _ string, count int) (netcheck.PingStats, error) {
	e.pingCount = count
	if e.pingEr != nil {
		return netcheck.PingStats{}, e.pingEr
	}
	return netcheck.PingStats{Sent: count, Received: count, AvgRTT: 10 * time.Millisecond}, nil
}

func (e *fakeNetEnv) DialTCP(context.Context, string) (time.Duration, error) {
	if e.dialEr != nil {
		return 0, e.dialEr
	}
	return 5 * time.Millisecond, nil
}

func (e *fakeNetEnv) Handshake(context.Context, string, string) (*tls.ConnectionState, error) {
	return nil, errors.New("not a TLS port")
}

func (e *fakeNetEnv) Head(context.Context, string) (netcheck.HTTPResult, error) {
	return netcheck.HTTPResult{}, errors.New("no HTTP response")
}

func (e *fakeNetEnv) TrustRoots() *x509.CertPool { return x509.NewCertPool() }

func (e *fakeNetEnv) Now() time.Time { return time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC) }

func reachableHost() *fakeNetEnv {
	return &fakeNetEnv{addrs: []net.IP{net.ParseIP("192.0.2.10")}}
}

func unresolvableHost() *fakeNetEnv {
	return &fakeNetEnv{resolveEr: errors.New("no such host")}
}

// stubNetwork replaces the production Env so no test touches the real network —
// which would make the suite depend on the machine it runs on, and on whether
// example.test happens to resolve there.
func stubNetwork(t *testing.T, env *fakeNetEnv) {
	t.Helper()
	previous := checkEnv
	checkEnv = func(netcheck.Settings) netcheck.Env { return env }
	t.Cleanup(func() { checkEnv = previous })
}
