package netdiag

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
)

// pathStepKind says which kind of row a pathStep is.
type pathStepKind int

// The kinds of row, in the order the column shows them.
const (
	stepContainer pathStepKind = iota
	stepNetwork
	stepSource
	stepInterface
	stepGateway
	stepTrace
	stepHop
)

// pathRoute is the way out of this machine for the traced address.
type pathRoute struct {
	Known     bool // false: the lookup failed; interface and gateway are undetermined
	Interface string
	Source    netip.Addr // invalid: the stack did not say
	Gateway   netip.Addr // invalid: directly connected
}

// pathStep is one row of the Path column.
type pathStep struct {
	Kind      pathStepKind
	Container pathContainer // stepContainer, stepNetwork, stepTrace
	Route     pathRoute     // stepInterface, stepGateway
	Hop       nettrace.Hop  // stepHop
}

const (
	sourceLabel       = "this machine"
	routeUndetermined = "undetermined"
	gatewayDirect     = "directly connected"
)

// newPathRoute converts a route lookup into a pathRoute. A failed lookup
// discards whatever the hop holds. Addresses are unmapped to IPv4 when they can be.
func newPathRoute(hop netcheck.RouteHop, err error) pathRoute {
	if err != nil {
		return pathRoute{}
	}
	return pathRoute{
		Known:     true,
		Interface: hop.Interface,
		Source:    addrFromIP(hop.Source),
		Gateway:   addrFromIP(hop.Gateway),
	}
}

// addrFromIP returns ip as an unmapped netip.Addr, or the zero Addr for a nil or invalid ip.
func addrFromIP(ip net.IP) netip.Addr {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}
	}
	return addr.Unmap()
}

// pathSteps returns the rows of the Path column: the source, then the
// interface and gateway when route is not nil, then the hops as selected by
// visiblePathHops. It returns nil while neither a route nor a hop exists.
// Neither input is modified.
func pathSteps(ctr *pathContainer, route *pathRoute, hops []nettrace.Hop) []pathStep {
	if ctr == nil && route == nil && len(hops) == 0 {
		return nil
	}
	var steps []pathStep
	if ctr != nil {
		steps = append(steps,
			pathStep{Kind: stepContainer, Container: *ctr},
			pathStep{Kind: stepNetwork, Container: *ctr},
		)
	}
	steps = append(steps, pathStep{Kind: stepSource})
	if route != nil {
		steps = append(steps,
			pathStep{Kind: stepInterface, Route: *route},
			pathStep{Kind: stepGateway, Route: *route},
		)
	}
	if ctr != nil {
		steps = append(steps, pathStep{Kind: stepTrace, Container: *ctr})
	}
	for _, h := range visiblePathHops(hops) {
		steps = append(steps, pathStep{Kind: stepHop, Hop: h})
	}
	return steps
}

// hopCell is the TTL of a hop row; empty for the local rows.
func (s pathStep) hopCell() string {
	if s.Kind != stepHop {
		return ""
	}
	return strconv.Itoa(s.Hop.TTL)
}

// stepCell names the kind of row.
func (s pathStep) stepCell() string {
	switch s.Kind {
	case stepContainer:
		return "container"
	case stepNetwork:
		return "network"
	case stepTrace:
		return "trace"
	case stepSource:
		return "source"
	case stepInterface:
		return "interface"
	case stepGateway:
		return "gateway"
	}
	return s.Hop.Kind.String()
}

// addressCell is the main text of the row.
func (s pathStep) addressCell() string {
	switch s.Kind {
	case stepContainer:
		return s.Container.containerText()
	case stepNetwork:
		return s.Container.networkText()
	case stepTrace:
		return s.Container.traceText()
	case stepSource:
		return sourceLabel
	case stepInterface:
		return s.Route.interfaceText()
	case stepGateway:
		return s.Route.gatewayText()
	}
	return addrOrDash(s.Hop.Addr)
}

// rttCell is the round-trip time of a hop that answered; "-" otherwise.
func (s pathStep) rttCell() string {
	if s.Kind != stepHop {
		return ""
	}
	if s.Hop.RTT <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f ms", float64(s.Hop.RTT)/float64(time.Millisecond))
}

// interfaceText is the interface name, followed by the source address when known.
func (r pathRoute) interfaceText() string {
	if !r.Known || (r.Interface == "" && !r.Source.IsValid()) {
		return routeUndetermined
	}
	name := r.Interface
	if name == "" {
		name = "unnamed"
	}
	if !r.Source.IsValid() {
		return name
	}
	return name + " (" + r.Source.String() + ")"
}

// gatewayText is the gateway address, or says the destination is directly connected.
func (r pathRoute) gatewayText() string {
	switch {
	case !r.Known:
		return routeUndetermined
	case !r.Gateway.IsValid() || r.Gateway.IsUnspecified():
		return gatewayDirect
	}
	return r.Gateway.String()
}

// addrOrDash formats an address, or "-" when there is none.
func addrOrDash(a netip.Addr) string {
	if !a.IsValid() {
		return "-"
	}
	return a.String()
}
