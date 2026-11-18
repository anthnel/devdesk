package netcheck

import (
	"context"
	"net"
	"net/url"
	"strconv"

	"github.com/anthnel/devdesk/internal/netproxy"
)

// runProxy says whether a proxy variable applies to the target. It observes,
// it does not test: nothing is sent to the proxy, nothing is sent anywhere, and
// the checks around it stay direct.
//
// The scheme the HEAD will speak depends on a handshake that runs later, so the
// stage asks about both, https first, and merges the answers.
func runProxy(_ context.Context, t Target, env Env, _ Settings, _ *Results) []Check {
	host := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	https := env.ProxyFor(&url.URL{Scheme: "https", Host: host})
	http := env.ProxyFor(&url.URL{Scheme: "http", Host: host})

	c := newCheck(CheckProxy, StageProxy, NotApplicable, "")
	c.fact("HTTPS", describeDecision(https))
	c.fact("HTTP", describeDecision(http))

	// A proxy is configured as soon as one scheme is proxied, even when the other
	// scheme's variable is invalid and the merged verdict says so.
	c.Proxy = proxyShown(https, http)

	switch {
	case https.Kind == netproxy.DecisionInvalid || http.Kind == netproxy.DecisionInvalid:
		c.Verdict, c.Reason = Warn, ReasonProxyInvalid
		c.Summary = invalidSummary(https, http)
	case https.Kind == netproxy.DecisionProxied || http.Kind == netproxy.DecisionProxied:
		c.Verdict, c.Reason = Info, ReasonProxyConfigured
		c.Summary = "Configured for this target: " + proxiedSummary(https, http)
	case https.Kind == netproxy.DecisionExcluded || http.Kind == netproxy.DecisionExcluded:
		c.Reason, c.Summary = ReasonProxyExcluded, "Excluded by NO_PROXY"
	case https.Kind == netproxy.DecisionLoopback || http.Kind == netproxy.DecisionLoopback:
		c.Reason, c.Summary = ReasonProxyLoopback, "Not used for a loopback target"
	default:
		c.Reason, c.Summary = ReasonProxyNone, "None"
	}
	return []Check{c}
}

// proxyShown is the proxy a proxied scheme goes through, https first, and empty
// when neither scheme is proxied.
func proxyShown(https, http netproxy.Decision) string {
	switch {
	case https.Kind == netproxy.DecisionProxied:
		return https.Proxy
	case http.Kind == netproxy.DecisionProxied:
		return http.Proxy
	default:
		return ""
	}
}

// proxyConfigured reads, from the Proxy row already produced, whether a proxy
// applies to the target and which one to show. It never reads a Summary.
func proxyConfigured(prior *Results) (display string, ok bool) {
	if prior == nil {
		return "", false
	}
	c, found := prior.Get(CheckProxy)
	if !found || c.Proxy == "" {
		return "", false
	}
	return c.Proxy, true
}

// describeDecision is the fact value for one scheme. It names the variable, never
// its value: an invalid value is whatever the user typed, and may hold a secret.
func describeDecision(d netproxy.Decision) string {
	switch d.Kind {
	case netproxy.DecisionProxied:
		return d.Proxy + " (" + d.Variable + ")"
	case netproxy.DecisionExcluded:
		return "excluded by NO_PROXY"
	case netproxy.DecisionLoopback:
		return "not used for a loopback target"
	case netproxy.DecisionInvalid:
		return "invalid value in " + d.Variable
	default:
		return "none"
	}
}

func invalidSummary(https, http netproxy.Decision) string {
	switch {
	case https.Kind == netproxy.DecisionInvalid && http.Kind == netproxy.DecisionInvalid:
		return "Invalid " + https.Variable + " and " + http.Variable + " values"
	case https.Kind == netproxy.DecisionInvalid:
		return "Invalid " + https.Variable + " value"
	default:
		return "Invalid " + http.Variable + " value"
	}
}

// proxiedSummary names the proxy, or the two when the schemes differ, and the
// one scheme it covers when only one does.
func proxiedSummary(https, http netproxy.Decision) string {
	hs := https.Kind == netproxy.DecisionProxied
	ps := http.Kind == netproxy.DecisionProxied
	switch {
	case hs && ps && https.Proxy == http.Proxy:
		return https.Proxy
	case hs && ps:
		return https.Proxy + " (https), " + http.Proxy + " (http)"
	case hs:
		return https.Proxy + " (https only)"
	default:
		return http.Proxy + " (http only)"
	}
}
