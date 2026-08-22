package netcheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// scenario is one situation the battery drives through Run. Between them they
// have to produce every row of the guidance table — which is what
// TestNoGuidanceRowIsUnreachable checks, so a row written for a state that
// cannot happen fails the build rather than sitting there looking helpful.
type scenario struct {
	name string
	env  Env
	tg   Target
}

func batteries(t *testing.T) []scenario {
	t.Helper()

	good, goodRoots := buildChain(t, chainOpts{dnsNames: []string{"example.com"}, withIntermediate: true})
	leafOnly, leafRoots := buildChain(t, chainOpts{dnsNames: []string{"example.com"}})
	incomplete, incRoots := buildChain(t, chainOpts{
		dnsNames: []string{"example.com"}, withIntermediate: true, omitIntermediate: true})
	selfSigned, _ := buildChain(t, chainOpts{dnsNames: []string{"example.com"}, selfSigned: true})
	wrongName, wrongRoots := buildChain(t, chainOpts{dnsNames: []string{"other.test"}, withIntermediate: true})
	old, oldRoots := buildChain(t, chainOpts{
		dnsNames: []string{"example.com"}, withIntermediate: true, version: tls.VersionTLS10})
	soon, soonRoots := buildChain(t, chainOpts{
		dnsNames: []string{"example.com"}, withIntermediate: true, notAfter: testNow.Add(9 * 24 * time.Hour)})
	expired, expRoots := buildChain(t, chainOpts{
		dnsNames: []string{"example.com"}, notAfter: testNow.Add(-3 * 24 * time.Hour)})
	future, futureRoots := buildChain(t, chainOpts{
		dnsNames:  []string{"example.com"},
		notBefore: testNow.Add(48 * time.Hour), notAfter: testNow.Add(400 * 24 * time.Hour)})
	onIP, ipRoots := buildChain(t, chainOpts{
		ips: []net.IP{net.ParseIP("10.2.3.4")}, withIntermediate: true})

	ip := Target{Host: "10.2.3.4", Port: 443}

	return []scenario{
		{"everything sound", fakeEnv{roots: goodRoots, handshake: handshakeReturning(good)}, target()},
		{"chain verifies but no intermediates were sent",
			fakeEnv{roots: leafRoots, handshake: handshakeReturning(leafOnly)}, target()},
		{"incomplete chain",
			fakeEnv{roots: incRoots, handshake: handshakeReturning(incomplete)}, target()},
		{"self-signed",
			fakeEnv{roots: x509.NewCertPool(), handshake: handshakeReturning(selfSigned)}, target()},
		{"untrusted root",
			fakeEnv{roots: x509.NewCertPool(), handshake: handshakeReturning(good)}, target()},
		{"wrong hostname",
			fakeEnv{roots: wrongRoots, handshake: handshakeReturning(wrongName)}, target()},
		{"deprecated protocol version",
			fakeEnv{roots: oldRoots, handshake: handshakeReturning(old)}, target()},
		{"expiring soon",
			fakeEnv{roots: soonRoots, handshake: handshakeReturning(soon)}, target()},
		{"expired",
			fakeEnv{roots: expRoots, handshake: handshakeReturning(expired)}, target()},
		{"not yet valid",
			fakeEnv{roots: futureRoots, handshake: handshakeReturning(future)}, target()},

		{"the name does not resolve", fakeEnv{
			resolve: func(context.Context, string, string) ([]net.IP, error) {
				return nil, errors.New("no such host")
			}}, target()},
		{"a literal address with a reverse record", fakeEnv{
			roots: ipRoots, handshake: handshakeReturning(onIP),
			reverse: func(context.Context, string, string) ([]string, error) {
				return []string{"host.corp.internal."}, nil
			}}, ip},
		{"a literal address without a reverse record",
			fakeEnv{roots: ipRoots, handshake: handshakeReturning(onIP)}, ip},

		{"echo is filtered", fakeEnv{
			roots: goodRoots, handshake: handshakeReturning(good),
			ping: func(_ context.Context, _ string, n int) (PingStats, error) {
				return PingStats{Sent: n}, nil
			}}, target()},
		{"partial packet loss", fakeEnv{
			roots: goodRoots, handshake: handshakeReturning(good),
			ping: func(_ context.Context, _ string, n int) (PingStats, error) {
				return PingStats{Sent: n, Received: n - 1, AvgRTT: 30 * time.Millisecond}, nil
			}}, target()},
		{"icmp cannot be probed", fakeEnv{
			roots: goodRoots, handshake: handshakeReturning(good),
			ping: func(context.Context, string, int) (PingStats, error) {
				return PingStats{}, errors.New("operation not permitted")
			}}, target()},

		{"the port is refused", fakeEnv{
			dial: func(context.Context, string) (time.Duration, error) {
				return 0, errors.New("connection refused")
			}}, target()},
		{"the port does not speak TLS", fakeEnv{}, Target{Host: "example.com", Port: 22}},
		{"the handshake breaks", fakeEnv{
			handshake: func(context.Context, string, string) (*tls.ConnectionState, error) {
				return nil, errors.New("tls: protocol version not supported")
			}}, target()},
		{"no certificate is presented", fakeEnv{
			handshake: handshakeReturning(&tls.ConnectionState{Version: tls.VersionTLS13})}, target()},

		{"the service declines the request", fakeEnv{
			roots: goodRoots, handshake: handshakeReturning(good),
			head: func(context.Context, string) (HTTPResult, error) {
				return HTTPResult{Status: 401}, nil
			}}, target()},
		{"the application is failing", fakeEnv{
			roots: goodRoots, handshake: handshakeReturning(good),
			head: func(context.Context, string) (HTTPResult, error) {
				return HTTPResult{Status: 503}, nil
			}}, target()},
	}
}

// produced runs the battery and returns every check it observed.
func produced(t *testing.T) []Check {
	t.Helper()
	var all []Check
	for _, s := range batteries(t) {
		res, err := Run(context.Background(), s.tg, s.env, DefaultSettings())
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		all = append(all, res.All()...)
	}
	return all
}

// TestEveryCheckAScenarioProducesIsExplained is the contract: no state a user
// can reach leaves them reading a verdict with nothing telling them what it
// means. It is behavioural rather than a list of pairs, because a list would
// have to be kept in step with the stages by hand.
func TestEveryCheckAScenarioProducesIsExplained(t *testing.T) {
	for _, s := range batteries(t) {
		res, err := Run(context.Background(), s.tg, s.env, DefaultSettings())
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		for _, c := range res.All() {
			e := Explain(c)
			if strings.Contains(e.Means, "No guidance is written") {
				t.Errorf("%s: %s at %s (reason %q) fell through to the fallback",
					s.name, c.ID, c.Verdict, c.Reason)
			}
			if strings.TrimSpace(e.Means) == "" {
				t.Errorf("%s: %s at %s has no explanation", s.name, c.ID, c.Verdict)
			}
			if e.Observed != c.Summary {
				t.Errorf("%s: %s restates the observation instead of carrying it", s.name, c.ID)
			}
		}
	}
}

// TestNoGuidanceRowIsUnreachable catches the other drift: a row written for a
// combination no stage produces. It is dead text that reads as coverage.
func TestNoGuidanceRowIsUnreachable(t *testing.T) {
	type key struct {
		id     CheckID
		v      Verdict
		reason Reason
	}
	seen := map[key]bool{}
	for _, c := range produced(t) {
		seen[key{c.ID, c.Verdict, c.Reason}] = true
	}

	for _, g := range explanations {
		if g.reason != "" {
			if !seen[key{g.check, g.verdict, g.reason}] {
				t.Errorf("guidance for %s/%s/%s is unreachable — no scenario produces it",
					g.check, g.verdict, g.reason)
			}
			continue
		}
		// A reason-less row is reached by any reason for that verdict.
		found := false
		for k := range seen {
			if k.id == g.check && k.v == g.verdict {
				found = true
			}
		}
		if !found {
			t.Errorf("guidance for %s/%s is unreachable — no scenario produces it", g.check, g.verdict)
		}
	}
}

// TestAPassingCheckOffersNoAction: an action invented for a green row teaches
// the reader to skip the field on the rows that have one.
func TestAPassingCheckOffersNoAction(t *testing.T) {
	for _, g := range explanations {
		if g.verdict == OK && g.do != "" {
			t.Errorf("%s at OK suggests an action: %q", g.check, g.do)
		}
		if g.verdict == Fail && strings.TrimSpace(g.do) == "" {
			t.Errorf("%s at Fail has nothing to do about it", g.check)
		}
	}
}

// TestNoGuidanceRowIsNotApplicable: that verdict is answered structurally, from
// Because, so a per-check row for it would never be consulted.
func TestNoGuidanceRowIsNotApplicable(t *testing.T) {
	for _, g := range explanations {
		if g.verdict == NotApplicable {
			t.Errorf("%s declares guidance for NotApplicable, which Explain answers generically", g.check)
		}
		if checkTitles[g.check] == "" {
			t.Errorf("guidance names %q, which is not a check", g.check)
		}
	}
}

// TestTheMostSpecificGuidanceWins is why Reason exists: one Fail, three jobs.
func TestTheMostSpecificGuidanceWins(t *testing.T) {
	texts := map[Reason]string{}
	for _, r := range []Reason{ReasonSelfSigned, ReasonNoIntermediates, ReasonUntrustedRoot} {
		e := Explain(Check{ID: CheckTLSChain, Verdict: Fail, Reason: r})
		if e.Means == "" {
			t.Fatalf("no guidance for chain/%s", r)
		}
		texts[r] = e.Do
	}
	if texts[ReasonSelfSigned] == texts[ReasonNoIntermediates] ||
		texts[ReasonSelfSigned] == texts[ReasonUntrustedRoot] ||
		texts[ReasonNoIntermediates] == texts[ReasonUntrustedRoot] {
		t.Fatal("the three chain failures share advice; they are three different jobs")
	}
}

// TestABlockedCheckNamesWhatBlockedIt — the point of the cascade is that one
// row tells you where to go, so the explanation has to name it.
func TestABlockedCheckNamesWhatBlockedIt(t *testing.T) {
	e := Explain(Check{ID: CheckTLSChain, Verdict: NotApplicable, Because: CheckResolve})
	if !strings.Contains(e.Means, checkTitles[CheckResolve]) {
		t.Errorf("means %q does not name the blocker", e.Means)
	}
	if !strings.Contains(e.Do, checkTitles[CheckResolve]) {
		t.Errorf("do %q does not point at the blocker", e.Do)
	}
}

// TestAnIrrelevantCheckAsksForNothing: NotApplicable with no Because is not a
// problem, so it must not read as one.
func TestAnIrrelevantCheckAsksForNothing(t *testing.T) {
	e := Explain(Check{ID: CheckReverseDNS, Verdict: NotApplicable})
	if e.Do != "" {
		t.Errorf("an irrelevant check suggests an action: %q", e.Do)
	}
	if e.Means == "" {
		t.Error("an irrelevant check says nothing at all")
	}
}

// TestANonTlsPortIsExplainedAsNormal — it is the one NotApplicable-without-blame
// that deserves more than the generic line, because a user who expected TLS
// there needs to know the port answered.
func TestANonTlsPortIsExplainedAsNormal(t *testing.T) {
	e := Explain(Check{ID: CheckTLSHandshake, Verdict: NotApplicable, Reason: ReasonNotTLS})
	if !strings.Contains(strings.ToLower(e.Means), "not with tls") {
		t.Errorf("means %q does not say the port answered with something else", e.Means)
	}
	if e.Do == "" {
		t.Error("a port that does not speak TLS when one was expected deserves a next step")
	}
}

// TestNoTwoRowsShareTheirText catches the copy-paste that makes a table look
// complete while saying the same thing everywhere.
func TestNoTwoRowsShareTheirText(t *testing.T) {
	seen := map[string]string{}
	for _, g := range explanations {
		key := string(g.check) + "/" + g.verdict.String() + "/" + string(g.reason)
		if prev, ok := seen[g.means]; ok {
			t.Errorf("%s repeats the explanation of %s", key, prev)
		}
		seen[g.means] = key
		if len(g.means) < 40 {
			t.Errorf("%s explains in %d characters, which is a restatement rather than a meaning",
				key, len(g.means))
		}
	}
}
