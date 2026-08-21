package netcheck

import (
	"context"
	"fmt"
)

// runReach probes the target with ICMP.
//
// This stage gates nothing, and that is the decision it exists to record: ICMP
// is filtered on a large share of perfectly reachable hosts, so a silent ping
// says little about the host and nothing about its certificate. A no-reply is
// therefore a Warn, never a Fail — the question "is it reachable" is settled by
// the TCP connect, which is the port the user actually named.
func runReach(ctx context.Context, t Target, env Env, _ *Results) []Check {
	stats, err := env.Ping(ctx, t.Host, pingCount)

	// A probe that could not be sent is Unknown, not a failure of the host.
	// On a machine without the privilege to open an ICMP socket, reporting
	// "down" would blame the target for a local restriction.
	if err != nil {
		c := newCheck(CheckICMP, StageReach, Unknown, "ICMP could not be probed")
		c.fact("Error", err.Error())
		return []Check{c}
	}

	c := newCheck(CheckICMP, StageReach, OK, "")
	c.fact("Sent", fmt.Sprintf("%d", stats.Sent))
	c.fact("Received", fmt.Sprintf("%d", stats.Received))

	switch {
	case stats.Received == 0:
		c.Verdict = Warn
		c.Reason = ReasonNoReply
		c.Summary = "No ICMP reply — echo may be filtered"
	case stats.Received < stats.Sent:
		c.Verdict = Warn
		c.Reason = ReasonPartialLoss
		c.Summary = fmt.Sprintf("Partial loss: %d of %d replies, %s average",
			stats.Received, stats.Sent, roundedMillis(stats.AvgRTT))
		c.fact("Average RTT", roundedMillis(stats.AvgRTT))
	default:
		c.Summary = fmt.Sprintf("%d of %d replies, %s average",
			stats.Received, stats.Sent, roundedMillis(stats.AvgRTT))
		c.fact("Average RTT", roundedMillis(stats.AvgRTT))
	}
	return []Check{c}
}
