package netdiag

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/wslenv"
)

// pathStepKind says which kind of row a pathStep is.
type pathStepKind int

// The kinds of row, in the order the column shows them.
const (
	stepContainer pathStepKind = iota
	stepNetwork
	stepBridge
	stepEngine
	stepSource
	stepInterface
	stepGateway
	stepTrace
	stepHop
	stepNoResponse
)

// pathRoute is the way out of this machine for the traced address.
type pathRoute struct {
	Known     bool // false: the lookup failed; interface and gateway are undetermined
	NoRoute   bool // the lookup proved this machine has no route (Known stays false)
	Interface string
	Source    netip.Addr // invalid: the stack did not say
	Gateway   netip.Addr // invalid: directly connected
}

// pathStep is one row of the Path column.
type pathStep struct {
	Kind      pathStepKind
	Container pathContainer // stepContainer, stepNetwork, stepBridge, stepEngine, stepTrace
	Route     pathRoute     // stepInterface, stepGateway
	Hop       nettrace.Hop  // stepHop
	WSL       wslenv.Info   // stepSource: set by withWSL
	// WindowsHost marks the gateway row and the TTL 1 hop as the Windows
	// virtual switch (WSL2 NAT mode only): set by withWSL.
	WindowsHost bool
	Tail        pathTail // stepNoResponse
	// LastAnswer marks the last hop that answered before a silent end: set by
	// withNoResponse.
	LastAnswer bool
}

const (
	sourceLabel       = "this machine"
	routeUndetermined = "undetermined"
	routeNone         = "no route"
	gatewayNone       = "-"
	gatewayDirect     = "directly connected"
)

// newPathRoute converts a route lookup into a pathRoute. A failed lookup
// discards whatever the hop holds; a no-route failure is told apart from any
// other, which stays undetermined. Addresses are unmapped to IPv4 when they can be.
func newPathRoute(hop netcheck.RouteHop, err error) pathRoute {
	if err != nil {
		return pathRoute{NoRoute: nettrace.IsNoRoute(err)}
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
		switch {
		case ctr.Bridge != "":
			steps = append(steps, pathStep{Kind: stepBridge, Container: *ctr})
		case ctr.Engine != engineNone:
			steps = append(steps, pathStep{Kind: stepEngine, Container: *ctr})
		}
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
	switch s.Kind {
	case stepHop:
		return strconv.Itoa(s.Hop.TTL)
	case stepNoResponse:
		return s.Tail.hopCell()
	}
	return ""
}

// stepCell names the kind of row.
func (s pathStep) stepCell() string {
	switch s.Kind {
	case stepContainer:
		return "container"
	case stepNetwork:
		return "network"
	case stepBridge:
		return "bridge"
	case stepEngine:
		return "engine"
	case stepTrace:
		return "trace"
	case stepSource:
		return "source"
	case stepInterface:
		return "interface"
	case stepGateway:
		return "gateway"
	case stepNoResponse:
		return "silent"
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
	case stepBridge:
		return s.Container.Bridge
	case stepEngine:
		return s.Container.engineText()
	case stepTrace:
		return s.Container.traceText()
	case stepSource:
		if s.WSL.WSL2 {
			return wslSourceText(s.WSL)
		}
		return sourceLabel
	case stepInterface:
		return s.Route.interfaceText()
	case stepGateway:
		return s.Route.gatewayText() + s.hostTag()
	case stepNoResponse:
		return s.Tail.addressCell()
	}
	text := addrOrDash(s.Hop.Addr) + s.hostTag()
	if s.LastAnswer {
		text += lastAnswerSuffix
	}
	return text
}

// hostTag is the Windows host label of a row marked by withWSL.
func (s pathStep) hostTag() string {
	if s.WindowsHost {
		return windowsHostTag
	}
	return ""
}

// rttCell is the round-trip time of a hop that answered; "-" otherwise.
func (s pathStep) rttCell() string {
	if s.Kind == stepNoResponse {
		return "-"
	}
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
	if r.NoRoute {
		return routeNone
	}
	if !r.Known || (r.Interface == "" && !r.Source.IsValid()) {
		return routeUndetermined
	}
	name := printable(r.Interface)
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
	case r.NoRoute:
		return gatewayNone
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
