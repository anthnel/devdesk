package netcheck

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
)

// runHTTP asks whether the service answers.
//
// The scheme comes from what the handshake observed, not from the port number.
// That is the defect this replaces: RunCurl chose HTTPS only when the port was
// literally 443, so a service on :8443 was probed in the clear, failed, and
// read as "the host is down".
//
// The stage depends on connect rather than on tls, which is deliberate: a
// broken certificate must not hide whether the application responds. "It works,
// the certificate is what is wrong" is a useful thing to be told, and the
// certificate problem has four rows of its own.
func runHTTP(ctx context.Context, t Target, env Env, _ Settings, prior *Results) []Check {
	scheme := "http"
	if prior.VerdictOf(CheckTLSHandshake) == OK {
		scheme = "https"
	}
	target := (&url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(t.Host, strconv.Itoa(t.Port)),
		Path:   "/",
	}).String()

	res, err := env.Head(ctx, target)

	c := newCheck(CheckHTTP, StageHTTP, OK, "")
	c.fact("URL", target)

	if err != nil {
		c.Verdict = Fail
		c.Summary = fmt.Sprintf("No HTTP response over %s", scheme)
		c.fact("Error", err.Error())
		return []Check{c}
	}

	c.fact("Status", strconv.Itoa(res.Status))
	c.fact("Server", res.Server)
	c.Duration = res.Total
	if res.DNSDuration > 0 {
		c.fact("DNS", roundedMillis(res.DNSDuration))
	}
	c.fact("Connect", roundedMillis(res.ConnectDuration))
	if scheme == "https" {
		c.fact("TLS", roundedMillis(res.TLSDuration))
	}
	c.fact("TTFB", roundedMillis(res.TTFB))
	c.fact("Total time", roundedMillis(res.Total))
	c.Phases = httpPhases(res, scheme == "https")

	switch {
	case res.Status >= 500:
		c.Verdict = Fail
		c.Summary = fmt.Sprintf("Server error: HTTP %d", res.Status)
	case res.Status >= 400:
		// The service answered, which is what this check asks. 401 and 403 are
		// a working endpoint declining an unauthenticated HEAD, and calling
		// that a failure would be wrong far more often than right.
		c.Verdict = Warn
		c.Summary = fmt.Sprintf("Service answered with HTTP %d", res.Status)
	default:
		c.Summary = fmt.Sprintf("Service answered with HTTP %d over %s", res.Status, scheme)
	}
	return []Check{c}
}

// httpPhases turns an HTTPResult's timings into a waterfall.
//
// DNS, connect and TLS are already non-overlapping spans, but TTFB is not: it
// is measured from the very start of the request (env.go's httpTiming), so it
// already *contains* DNS+connect+TLS plus however long the server took to
// start answering. Charting DNS/connect/TLS/TTFB side by side as if they were
// independent shares of Total would double-count everything TTFB already
// covers — the phases below subtract that overlap instead, so they sum to
// Total exactly and a percentage means what it says.
func httpPhases(res HTTPResult, https bool) []Phase {
	wait := res.TTFB - res.DNSDuration - res.ConnectDuration - res.TLSDuration
	if wait < 0 {
		wait = 0 // clock jitter between httptrace callbacks, not a real negative span
	}
	content := res.Total - res.TTFB
	if content < 0 {
		content = 0
	}

	var phases []Phase
	if res.DNSDuration > 0 {
		phases = append(phases, Phase{Name: "DNS", Duration: res.DNSDuration})
	}
	phases = append(phases, Phase{Name: "Connect", Duration: res.ConnectDuration})
	if https {
		phases = append(phases, Phase{Name: "TLS", Duration: res.TLSDuration})
	}
	phases = append(phases,
		Phase{Name: "Wait", Duration: wait},
		Phase{Name: "Content", Duration: content},
	)
	return phases
}
