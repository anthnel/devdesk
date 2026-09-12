package status

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/status"
)

// defaultDiagnosticsPort is netdiag's own default (netdiag.defaultPort is
// unexported, and importing the UI package from here would be a cycle — the
// two are kept in sync by the same reasoning, not by sharing the constant).
const defaultDiagnosticsPort = 443

// diagnosticsTarget turns one monitor into a netcheck.Target for `H`
// (§3.66), and reports whether the port is certain enough to run the
// pipeline immediately rather than land on the prefilled form.
//
// http/https and ssl monitors carry their own port (explicit, or the
// protocol's default); icmp and dns have no port of their own, so a guess
// (netdiag's own default) is offered instead of one being invented on their
// behalf silently.
func diagnosticsTarget(c status.ComponentStatus) (t netcheck.Target, autoRun bool) {
	switch c.Type {
	case status.TypeHTTP, status.TypeHTTPS:
		host, port := hostPortFromURL(c.Target, string(c.Type))
		return netcheck.Target{Host: host, Port: port}, true
	case status.TypeSSL:
		host, port := hostPortFromHostPort(c.Target, defaultDiagnosticsPort)
		return netcheck.Target{Host: host, Port: port}, true
	default: // icmp, dns — no natural port
		return netcheck.Target{Host: c.Target, Port: defaultDiagnosticsPort}, false
	}
}

// hostPortFromURL extracts host and port from a monitor's target, which may
// or may not already carry a scheme — buildURL in http_checker.go does the
// same normalization for the request itself, but leaves ComponentStatus.Target
// as the user typed it.
func hostPortFromURL(target, componentType string) (host string, port int) {
	defaultPort := 80
	if componentType == "https" || strings.HasPrefix(target, "https://") {
		defaultPort = 443
	}

	raw := target
	if !strings.Contains(raw, "://") {
		raw = componentType + "://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return target, defaultPort
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			return u.Hostname(), n
		}
	}
	return u.Hostname(), defaultPort
}

// hostPortFromHostPort splits a bare host or host:port — the shape an SSL
// monitor's target has, never a URL with a scheme.
func hostPortFromHostPort(target string, fallbackPort int) (host string, port int) {
	h, p, err := net.SplitHostPort(target)
	if err != nil {
		return target, fallbackPort
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return h, fallbackPort
	}
	return h, n
}
