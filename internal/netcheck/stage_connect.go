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
func runConnect(ctx context.Context, t Target, env Env, _ Settings, _ *Results) []Check {
	elapsed, err := env.DialTCP(ctx, t.Addr())

	c := newCheck(CheckTCP, StageConnect, OK, "")
	c.fact("Address", t.Addr())

	if err != nil {
		c.Verdict = Fail
		c.Summary = fmt.Sprintf("Port %d does not accept connections", t.Port)
		c.fact("Error", err.Error())
		return []Check{c}
	}

	c.Summary = fmt.Sprintf("Port %d accepted the connection in %s", t.Port, roundedMillis(elapsed))
	c.fact("Connect time", roundedMillis(elapsed))
	return []Check{c}
}
