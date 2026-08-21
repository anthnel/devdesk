package netcheck

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func tlsChecks(t *testing.T, o chainOpts, tg Target) []Check {
	t.Helper()
	state, roots := buildChain(t, o)
	var prior Results
	return runTLS(context.Background(), tg, fakeEnv{roots: roots, handshake: handshakeReturning(state)}, &prior)
}

// --- the chain ---------------------------------------------------------------

// TestAChainWithItsIntermediateVerifies is the case the old check could not
// even express: it greped the leaf's text and never parsed a second certificate.
func TestAChainWithItsIntermediateVerifies(t *testing.T) {
	checks := tlsChecks(t, chainOpts{dnsNames: []string{"example.com"}, withIntermediate: true}, target())

	c := checkNamed(t, checks, CheckTLSChain)
	if c.Verdict != OK {
		t.Fatalf("chain verdict = %v (%s), want OK", c.Verdict, c.Summary)
	}
	if !hasFact(c, "Intermediate", "test intermediate") {
		t.Errorf("the intermediate is not among the facts: %v", c.Facts)
	}
}

// TestAServerThatSendsNoIntermediatesWarnsEvenWhenItVerifiesHere is the check
// that only exists because the chain is inspected rather than the leaf: the
// local platform can complete a chain another client will not, so a pass here
// is not a pass everywhere.
func TestAServerThatSendsNoIntermediatesWarnsEvenWhenItVerifiesHere(t *testing.T) {
	// Leaf signed directly by the trusted root, presented alone: verification
	// succeeds, but nothing was sent to complete it.
	checks := tlsChecks(t, chainOpts{dnsNames: []string{"example.com"}}, target())

	c := checkNamed(t, checks, CheckTLSChain)
	if c.Verdict != Warn {
		t.Fatalf("chain verdict = %v (%s), want Warn", c.Verdict, c.Summary)
	}
	if !strings.Contains(c.Summary, "no intermediates") {
		t.Errorf("summary %q does not say what is missing", c.Summary)
	}
}

func TestAnIncompleteChainFails(t *testing.T) {
	checks := tlsChecks(t, chainOpts{
		dnsNames:         []string{"example.com"},
		withIntermediate: true,
		omitIntermediate: true,
	}, target())

	c := checkNamed(t, checks, CheckTLSChain)
	if c.Verdict != Fail {
		t.Fatalf("chain verdict = %v (%s), want Fail", c.Verdict, c.Summary)
	}
	if !strings.Contains(c.Summary, "no intermediate") {
		t.Errorf("summary %q does not name the cause", c.Summary)
	}
}

func TestASelfSignedCertificateIsNamedAsSuch(t *testing.T) {
	checks := tlsChecks(t, chainOpts{dnsNames: []string{"example.com"}, selfSigned: true}, target())

	c := checkNamed(t, checks, CheckTLSChain)
	if c.Verdict != Fail {
		t.Fatalf("chain verdict = %v, want Fail", c.Verdict)
	}
	if !strings.Contains(c.Summary, "self-signed") {
		t.Errorf("summary %q does not say self-signed — an untrusted root and a self-signed leaf have different fixes", c.Summary)
	}
}

// TestAnExpiredCertificateIsReportedOnceReflects the decision that the chain
// does not repeat what the expiry row already says.
func TestAnExpiredCertificateIsReportedOnce(t *testing.T) {
	checks := tlsChecks(t, chainOpts{
		dnsNames: []string{"example.com"},
		notAfter: testNow.Add(-5 * 24 * time.Hour),
	}, target())

	expiry := checkNamed(t, checks, CheckTLSExpiry)
	if expiry.Verdict != Fail {
		t.Fatalf("expiry verdict = %v, want Fail", expiry.Verdict)
	}
	if !strings.Contains(expiry.Summary, "5 days ago") {
		t.Errorf("summary %q does not say how long ago", expiry.Summary)
	}

	chain := checkNamed(t, checks, CheckTLSChain)
	if chain.Verdict != NotApplicable || chain.Because != CheckTLSExpiry {
		t.Fatalf("chain = %v because %q, want NotApplicable because %q — the expiry row already says it",
			chain.Verdict, chain.Because, CheckTLSExpiry)
	}
}

// --- hostname ----------------------------------------------------------------

func TestHostnameMatchingIsSeparateFromTrust(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts chainOpts
		tg   Target
		want Verdict
	}{
		{"exact name", chainOpts{dnsNames: []string{"example.com"}, withIntermediate: true}, target(), OK},
		{"wildcard", chainOpts{dnsNames: []string{"*.example.com"}, withIntermediate: true},
			Target{Host: "api.example.com", Port: 443}, OK},
		{"wrong name", chainOpts{dnsNames: []string{"other.test"}, withIntermediate: true}, target(), Fail},
		{"ip in SAN", chainOpts{ips: []net.IP{net.ParseIP("10.2.3.4")}, withIntermediate: true},
			Target{Host: "10.2.3.4", Port: 443}, OK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks := tlsChecks(t, tc.opts, tc.tg)
			if got := verdictOf(t, checks, CheckTLSHostname); got != tc.want {
				t.Fatalf("hostname verdict = %v, want %v", got, tc.want)
			}
			// Trust is unaffected by the name: the two rows exist so that a
			// trusted certificate for the wrong host reads as one problem, not
			// as a broken chain.
			if got := verdictOf(t, checks, CheckTLSChain); got == Fail && tc.name == "wrong name" {
				t.Errorf("a name mismatch must not fail the chain")
			}
		})
	}
}

// --- expiry ------------------------------------------------------------------

func TestExpiryIsGradedInDays(t *testing.T) {
	for _, tc := range []struct {
		name     string
		notAfter time.Time
		want     Verdict
		contains string
	}{
		{"comfortable", testNow.Add(90 * 24 * time.Hour), OK, "90 days"},
		{"inside the renewal window", testNow.Add(9 * 24 * time.Hour), Warn, "9 days"},
		{"expired", testNow.Add(-2 * 24 * time.Hour), Fail, "2 days ago"},
		{"hours left", testNow.Add(6 * time.Hour), Warn, "less than a day"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks := tlsChecks(t, chainOpts{dnsNames: []string{"example.com"}, notAfter: tc.notAfter}, target())
			c := checkNamed(t, checks, CheckTLSExpiry)
			if c.Verdict != tc.want {
				t.Fatalf("expiry verdict = %v (%s), want %v", c.Verdict, c.Summary, tc.want)
			}
			if !strings.Contains(c.Summary, tc.contains) {
				t.Errorf("summary %q does not contain %q", c.Summary, tc.contains)
			}
		})
	}
}

func TestACertificateNotYetValidFails(t *testing.T) {
	checks := tlsChecks(t, chainOpts{
		dnsNames:  []string{"example.com"},
		notBefore: testNow.Add(48 * time.Hour),
		notAfter:  testNow.Add(400 * 24 * time.Hour),
	}, target())

	c := checkNamed(t, checks, CheckTLSExpiry)
	if c.Verdict != Fail {
		t.Fatalf("expiry verdict = %v (%s), want Fail", c.Verdict, c.Summary)
	}
	if !strings.Contains(c.Summary, "not valid until") {
		t.Errorf("summary %q does not distinguish a premature certificate from an expired one", c.Summary)
	}
}

// --- version -----------------------------------------------------------------

func TestADeprecatedVersionWarnsRatherThanFails(t *testing.T) {
	for _, tc := range []struct {
		version uint16
		want    Verdict
		name    string
	}{
		{tls.VersionTLS10, Warn, "TLS 1.0"},
		{tls.VersionTLS11, Warn, "TLS 1.1"},
		{tls.VersionTLS12, OK, "TLS 1.2"},
		{tls.VersionTLS13, OK, "TLS 1.3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks := tlsChecks(t, chainOpts{
				dnsNames: []string{"example.com"}, withIntermediate: true, version: tc.version,
			}, target())
			c := checkNamed(t, checks, CheckTLSVersion)
			if c.Verdict != tc.want {
				t.Fatalf("version verdict = %v, want %v", c.Verdict, tc.want)
			}
			if !strings.Contains(c.Summary, tc.name) {
				t.Errorf("summary %q does not name the version", c.Summary)
			}
		})
	}
}

// --- the handshake -----------------------------------------------------------

// TestAPortThatDoesNotSpeakTlsIsNotAFailure is the observation that replaces a
// port-number heuristic: crypto/tls itself reports that the bytes were not a
// TLS record, so nothing has to guess from the port.
func TestAPortThatDoesNotSpeakTlsIsNotAFailure(t *testing.T) {
	var prior Results
	checks := runTLS(context.Background(), Target{Host: "example.com", Port: 22},
		fakeEnv{}, &prior) // the fake's default handshake is a RecordHeaderError

	for _, c := range checks {
		if c.Verdict != NotApplicable {
			t.Errorf("%s = %v, want NotApplicable on a port that does not speak TLS", c.ID, c.Verdict)
		}
		if c.Because != "" {
			t.Errorf("%s blamed %q; nothing failed, so nothing should be blamed", c.ID, c.Because)
		}
	}
}

func TestABrokenHandshakeFailsAndTheCertificateChecksBlameIt(t *testing.T) {
	var prior Results
	checks := runTLS(context.Background(), target(), fakeEnv{
		handshake: func(context.Context, string, string) (*tls.ConnectionState, error) {
			return nil, errors.New("tls: protocol version not supported")
		},
	}, &prior)

	if got := verdictOf(t, checks, CheckTLSHandshake); got != Fail {
		t.Fatalf("handshake verdict = %v, want Fail", got)
	}
	for _, id := range []CheckID{CheckTLSChain, CheckTLSHostname, CheckTLSExpiry, CheckTLSVersion} {
		c := checkNamed(t, checks, id)
		if c.Verdict != NotApplicable || c.Because != CheckTLSHandshake {
			t.Errorf("%s = %v because %q, want NotApplicable because %q",
				id, c.Verdict, c.Because, CheckTLSHandshake)
		}
	}
}

// --- resolution --------------------------------------------------------------

func TestResolutionAndReverseAreMutuallyExclusive(t *testing.T) {
	t.Run("a name resolves forward", func(t *testing.T) {
		checks := runWith(t, fakeEnv{}, target())
		if got := verdictOf(t, checks, CheckResolve); got != OK {
			t.Errorf("resolve verdict = %v, want OK", got)
		}
		c := checkNamed(t, checks, CheckReverseDNS)
		if c.Verdict != NotApplicable || c.Because != "" {
			t.Errorf("reverse = %v because %q, want NotApplicable with no blame", c.Verdict, c.Because)
		}
	})

	t.Run("an address resolves backward", func(t *testing.T) {
		checks := runWith(t, fakeEnv{
			reverse: func(context.Context, string, string) ([]string, error) {
				return []string{"host.corp.internal."}, nil
			},
		}, Target{Host: "10.2.3.4", Port: 443})

		c := checkNamed(t, checks, CheckReverseDNS)
		if c.Verdict != OK {
			t.Fatalf("reverse verdict = %v, want OK", c.Verdict)
		}
		if !strings.Contains(c.Summary, "host.corp.internal") || strings.Contains(c.Summary, "internal.") {
			t.Errorf("summary %q should carry the name with its trailing dot trimmed", c.Summary)
		}
	})
}

// TestAMissingPtrRecordWarnsRatherThanFails: plenty of reachable hosts have no
// reverse record, and the objective does not depend on one.
func TestAMissingPtrRecordWarnsRatherThanFails(t *testing.T) {
	checks := runWith(t, fakeEnv{}, Target{Host: "10.2.3.4", Port: 443})
	if got := verdictOf(t, checks, CheckReverseDNS); got != Warn {
		t.Fatalf("reverse verdict = %v, want Warn", got)
	}
}

// TestAnAddressCarriesItsClass pins the fact the diagnosis actually rests on —
// and the one that survives pseudonymisation if a payload is ever built.
func TestAnAddressCarriesItsClass(t *testing.T) {
	for _, tc := range []struct{ ip, want string }{
		{"10.2.3.4", "private"},
		{"192.168.1.2", "private"},
		{"203.0.113.9", "public"},
		{"127.0.0.1", "loopback"},
		{"169.254.1.1", "link-local"},
		{"2001:db8::1", "public IPv6"},
	} {
		if got := addressClass(net.ParseIP(tc.ip)); got != tc.want {
			t.Errorf("addressClass(%s) = %q, want %q", tc.ip, got, tc.want)
		}
	}
}

// --- reachability and the port ----------------------------------------------

func TestPartialPacketLossWarns(t *testing.T) {
	checks := runWith(t, fakeEnv{
		ping: func(_ context.Context, _ string, count int) (PingStats, error) {
			return PingStats{Sent: count, Received: count - 1, AvgRTT: 20 * time.Millisecond}, nil
		},
	}, target())

	c := checkNamed(t, checks, CheckICMP)
	if c.Verdict != Warn {
		t.Fatalf("icmp verdict = %v (%s), want Warn", c.Verdict, c.Summary)
	}
}

// --- HTTP --------------------------------------------------------------------

// TestTheSchemeComesFromTheHandshakeNotThePort is defect 4 of the old view:
// RunCurl chose HTTPS only when the port was literally 443, so :8443 was probed
// in the clear and read as a dead host.
func TestTheSchemeComesFromTheHandshakeNotThePort(t *testing.T) {
	state, roots := buildChain(t, chainOpts{dnsNames: []string{"example.com"}, withIntermediate: true})

	var got string
	capture := func(_ context.Context, url string) (HTTPResult, error) {
		got = url
		return HTTPResult{Status: 200}, nil
	}

	t.Run("tls on a non-standard port is https", func(t *testing.T) {
		runWith(t, fakeEnv{roots: roots, handshake: handshakeReturning(state), head: capture},
			Target{Host: "example.com", Port: 8443})
		if !strings.HasPrefix(got, "https://") {
			t.Fatalf("requested %q, want https", got)
		}
	})

	t.Run("no tls is http", func(t *testing.T) {
		runWith(t, fakeEnv{head: capture}, Target{Host: "example.com", Port: 8080})
		if !strings.HasPrefix(got, "http://") {
			t.Fatalf("requested %q, want http", got)
		}
	})
}

func TestAnAnsweringServiceIsNotAFailure(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   Verdict
	}{
		{200, OK},
		{301, OK},
		{401, Warn}, // a working endpoint declining an unauthenticated HEAD
		{404, Warn},
		{503, Fail},
	} {
		checks := runWith(t, fakeEnv{
			head: func(context.Context, string) (HTTPResult, error) {
				return HTTPResult{Status: tc.status}, nil
			},
		}, target())
		if got := verdictOf(t, checks, CheckHTTP); got != tc.want {
			t.Errorf("HTTP %d = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// --- formatting --------------------------------------------------------------

func TestRoundedMillisNeverReadsAsAMissingMeasurement(t *testing.T) {
	if got := roundedMillis(320 * time.Microsecond); got != "0.32 ms" {
		t.Errorf("roundedMillis(320µs) = %q, want a sub-millisecond figure", got)
	}
	if got := roundedMillis(12 * time.Millisecond); got != "12 ms" {
		t.Errorf("roundedMillis(12ms) = %q", got)
	}
}

func TestAnEmptyFactIsNotRecorded(t *testing.T) {
	c := Check{}
	c.fact("Server", "")
	c.fact("Status", "200")
	if len(c.Facts) != 1 {
		t.Fatalf("facts = %v, want the empty one dropped", c.Facts)
	}
}

func hasFact(c Check, key, value string) bool {
	for _, f := range c.Facts {
		if f.Key == key && f.Value == value {
			return true
		}
	}
	return false
}
