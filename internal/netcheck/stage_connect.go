package netcheck

import (
	"context"
	"fmt"
)

// runConnect opens a TCP connection to the port the user named.
//
// This is the check that decides reachability, and it is the stage's gate: if
// the port does not answer, there is no handshake to inspect and no response to
// read, so TLS and HTTP come back NotApplicable pointing here rather than
// producing failures of their own about a connection that never existed.
//
// The dial is always direct. When it fails and a proxy applies to the target,
// the conclusion says so: behind a mandatory proxy a closed direct path is
// expected and "does not accept connections" would mislead.
func runConnect(ctx context.Context, t Target, env Env, _ Settings, prior *Results) []Check {
	elapsed, err := env.DialTCP(ctx, t.Addr())

	c := newCheck(CheckTCP, StageConnect, OK, "")
	c.fact("Address", t.Addr())

	if err != nil {
		c.Verdict = Fail
		c.Summary = fmt.Sprintf("Port %d does not accept connections", t.Port)
		if display, ok := proxyConfigured(prior); ok {
			c.Summary = fmt.Sprintf(
				"Port %d does not accept direct connections; a proxy is configured for this target", t.Port)
			c.Reason = ReasonDirectFailedProxyConfigured
			c.fact("Proxy", display)
		}
		c.fact("Error", err.Error())
		return []Check{c}
	}

	c.Summary = fmt.Sprintf("Port %d accepted the connection in %s", t.Port, roundedMillis(elapsed))
	c.fact("Connect time", roundedMillis(elapsed))
	c.Duration = elapsed
	return []Check{c}
}
