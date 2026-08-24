package netcheck

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// maxRoutedAddresses bounds how many of the resolved addresses are looked up.
// A name behind a large CDN pool resolves to dozens, and they leave through the
// same interface: the question this stage answers is "which way out", not "how
// many addresses are there", which the resolution check already says.
const maxRoutedAddresses = 8

// runRoute answers which way out of this machine the target's traffic leaves.
//
// It is the one stage that asks the local network stack rather than the
// network, and it exists for the failure that no other check can explain: a
// split tunnel. When a VPN captures the default route, a host that is plainly
// reachable from the office is unreachable here, and every other row in the
// pipeline reports the symptom — no ICMP reply, no TCP connect — while none of
// them reports the cause.
//
// It gates nothing, for the same reason reach does not: a machine whose routing
// table cannot be read still has a perfectly answerable question about whether
// the port responds.
func runRoute(ctx context.Context, t Target, env Env, _ Settings, prior *Results) []Check {
	addrs := targetAddresses(t, prior)

	c := newCheck(CheckRoute, StageRoute, Unknown, "")
	if len(addrs) == 0 {
		// Resolution failed, so the stage gate has already skipped us — this is
		// the belt to that brace, and it says which of the two happened rather
		// than reporting a route lookup that was never attempted.
		c.Summary = "No address to route to"
		return []Check{c}
	}

	var routed []routedAddress
	var unreachable []net.IP
	for _, ip := range addrs {
		hop, err := env.Route(ctx, ip)
		if err != nil {
			// The error is not shown: Windows returns ERROR_NETWORK_UNREACHABLE
			// as a message in the machine's own language, and the UI is English
			// US (Rule 129). It is a fact under a key of ours, so the raw text
			// is available in the detail pane without being the summary.
			c.fact("No route", fmt.Sprintf("%s — %v", ip, err))
			unreachable = append(unreachable, ip)
			continue
		}
		routed = append(routed, routedAddress{ip: ip, hop: hop})
	}

	switch {
	case len(routed) == 0:
		// Every lookup failed. This is Unknown and not Fail on purpose: a
		// lookup that could not answer is not the same as a machine with no
		// route, and rendering the first as the second is the defect this
		// codebase keeps finding (D20).
		c.Summary = fmt.Sprintf("The local route to %s could not be determined", t.Host)
		return []Check{c}
	case len(unreachable) > 0:
		c.Verdict = Warn
		c.Reason = ReasonPartialRoute
		c.Summary = fmt.Sprintf("%d of %d addresses have no local route",
			len(unreachable), len(routed)+len(unreachable))
	default:
		c.Verdict = OK
		c.Summary = routeSummary(routed)
	}

	describeRoutes(&c, routed)
	return []Check{c}
}

// routedAddress pairs an address with the way out it was given.
type routedAddress struct {
	ip  net.IP
	hop RouteHop
}

// routeSummary states the way out in one line.
//
// It names the interface rather than the gateway because the interface is what
// the user recognises — "ProtonVPN" answers the question, "10.2.0.1" needs a
// second lookup to mean anything.
func routeSummary(routed []routedAddress) string {
	ifaces := make([]string, 0, len(routed))
	seen := map[string]bool{}
	for _, r := range routed {
		if name := r.hop.Interface; name != "" && !seen[name] {
			seen[name] = true
			ifaces = append(ifaces, name)
		}
	}
	switch len(ifaces) {
	case 0:
		return "Routed, but the outgoing interface has no name"
	case 1:
		return "Traffic leaves through " + ifaces[0]
	default:
		return "Traffic leaves through " + strings.Join(ifaces, " and ")
	}
}

// describeRoutes records one fact per address, plus the source and gateway of
// the first. The source address is what a firewall rule or an ACL sees, so it
// is worth a line of its own rather than being buried in a per-address string.
func describeRoutes(c *Check, routed []routedAddress) {
	if len(routed) == 0 {
		return
	}
	first := routed[0].hop
	if first.Interface != "" {
		c.fact("Interface", first.Interface)
	}
	if len(first.Source) > 0 {
		c.fact("Source address", first.Source.String())
	}
	if len(first.Gateway) > 0 {
		c.fact("Gateway", first.Gateway.String())
	} else {
		// "Directly connected" is an answer, and a blank line is not. A target
		// on the local segment has no gateway, which is worth saying because it
		// rules out every hop beyond it.
		c.fact("Gateway", "directly connected")
	}
	for _, r := range routed {
		c.fact("Route", fmt.Sprintf("%s via %s", r.ip, routeVia(r.hop)))
	}
}

// routeVia renders one hop for a per-address fact.
func routeVia(h RouteHop) string {
	name := h.Interface
	if name == "" {
		name = "an unnamed interface"
	}
	if len(h.Gateway) > 0 {
		return fmt.Sprintf("%s (gateway %s)", name, h.Gateway)
	}
	return name
}

// targetAddresses is where this stage gets its addresses from.
//
// A literal target is its own answer. A name is not resolved a second time: the
// resolve stage already asked, and asking again would let the two disagree on a
// round-robin name — the route would then describe an address no other check in
// the run ever touched. The coupling is deliberate and pinned by
// TestTheRouteStageReadsWhatTheResolveStageWrote.
func targetAddresses(t Target, prior *Results) []net.IP {
	if ip := net.ParseIP(strings.TrimSpace(t.Host)); ip != nil {
		return []net.IP{ip}
	}
	if prior == nil {
		return nil
	}
	c, ok := prior.Get(CheckResolve)
	if !ok {
		return nil
	}
	var out []net.IP
	for _, f := range c.Facts {
		if f.Key != "Address" {
			continue
		}
		// The value is "<ip> (<class>)"; anything that is not an address is
		// dropped by ParseIP rather than guarded against by shape.
		if ip := net.ParseIP(strings.TrimSpace(strings.SplitN(f.Value, " ", 2)[0])); ip != nil {
			out = append(out, ip)
			if len(out) == maxRoutedAddresses {
				break
			}
		}
	}
	return out
}
