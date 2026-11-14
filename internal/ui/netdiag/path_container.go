package netdiag

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/anthnel/devdesk/internal/docker"
)

// containerAttach says how a container is attached to a network.
type containerAttach int

// The ways a container can be attached.
const (
	attachNetwork containerAttach = iota
	attachHost
	attachNone
	attachShared
	attachUndetermined
)

// pathContainer is what the column says about the container a trace starts in.
type pathContainer struct {
	Name     string // display-safe
	Known    bool   // false: the inspection failed
	Running  bool
	Attach   containerAttach
	Network  string     // attachNetwork only, display-safe
	Networks int        // number of endpoints, for "undetermined (N networks)"
	Addr     netip.Addr // the container's IPv4 on Network
	Gateway  netip.Addr // Network's gateway
	Bridge   string     // attachNetwork only: the host interface read for Network; "" when none
	Engine   engineView // attachNetwork only: why there is no bridge row, when this machine cannot see it
}

// engineView says why the column shows an engine row instead of a bridge row.
// The zero value means no engine row.
type engineView int

const (
	engineNone       engineView = iota // no engine row
	engineHidden                       // this machine has no interface for the network: the bridge is elsewhere
	engineUnverified                   // the bridge could not be verified
)

// withBridgeRead returns a copy of c updated with the outcome of the bridge
// read. Only a container attached to a network is affected.
func (c pathContainer) withBridgeRead(b pathBridge) pathContainer {
	if c.Attach != attachNetwork {
		return c
	}
	switch b.State {
	case bridgeFound:
		return c.withBridge(b.Name)
	case bridgeAbsent:
		c.Engine = engineHidden
	case bridgeUnverified:
		c.Engine = engineUnverified
	}
	return c
}

// engineText is the Address cell of the engine row.
func (c pathContainer) engineText() string {
	if c.Engine == engineUnverified {
		return textEngineUnverified
	}
	return textEngineHidden
}

// withBridge returns a copy of c carrying the bridge interface name. The name
// is kept only for a container attached to a network, and only when not empty.
func (c pathContainer) withBridge(name string) pathContainer {
	if c.Attach == attachNetwork && name != "" {
		c.Bridge = name
	}
	return c
}

const (
	modeHost            = "host"
	modeNone            = "none"
	modeSharedPrefix    = "container:"
	textUndetermined    = "undetermined"
	textHostNetwork     = "host (shares this machine's stack)"
	textNoNetwork       = "none (no network)"
	textSharedNetwork   = "shares another container's network"
	textNotRunning      = "not running"
	textTraceFromHost   = "from this machine, not from the container"
	textTraceSharedHost = "from this machine (shared with the container)"

	textTraceFromEngineHost = "from this machine (host or WSL VM)"
	textEngineHidden        = "internal path not visible (location unknown)"
	textEngineUnverified    = "undetermined (bridge not verified)"
)

// newPathContainer converts an inspection into a pathContainer. Every text the
// engine returned is sanitized here, so the cells are display-safe.
func newPathContainer(src pathSource, n docker.ContainerNetworking, err error) pathContainer {
	c := pathContainer{Name: src.Name, Attach: attachUndetermined}
	if err != nil {
		return c
	}
	c.Known = true
	if name := engineText(n.Name); name != "" {
		c.Name = name
	}
	c.Running = n.Running
	if !n.Running {
		return c
	}
	switch {
	case n.Mode == modeHost:
		c.Attach = attachHost
	case n.Mode == modeNone:
		c.Attach = attachNone
	case strings.HasPrefix(n.Mode, modeSharedPrefix):
		c.Attach = attachShared
	default:
		c.Networks = len(n.Networks)
		if picked, ok := routeEndpoint(n); ok {
			c.Attach = attachNetwork
			c.Network = engineText(picked.Name)
			c.Addr = picked.IPv4
			c.Gateway = picked.Gateway
		}
	}
	return c
}

// routeEndpoint returns the endpoint holding the container's default route.
// It reports false for host, none and shared-namespace modes, and when the
// route cannot be told (see defaultRouteNetwork).
func routeEndpoint(n docker.ContainerNetworking) (docker.EndpointNetwork, bool) {
	if n.Mode == modeHost || n.Mode == modeNone || strings.HasPrefix(n.Mode, modeSharedPrefix) {
		return docker.EndpointNetwork{}, false
	}
	return defaultRouteNetwork(n.Networks)
}

// defaultRouteNetwork picks the network that holds the container's default
// route: among the endpoints with a gateway, the one with the strictly highest
// gateway priority. A tie, or a priority missing among several candidates, is
// never guessed. The input is not modified.
func defaultRouteNetwork(nets []docker.EndpointNetwork) (docker.EndpointNetwork, bool) {
	var candidates []docker.EndpointNetwork
	for _, e := range nets {
		if e.Gateway.IsValid() {
			candidates = append(candidates, e)
		}
	}
	switch len(candidates) {
	case 0:
		return docker.EndpointNetwork{}, false
	case 1:
		return candidates[0], true
	}
	best := -1
	for i, e := range candidates {
		if e.GwPriority == nil {
			return docker.EndpointNetwork{}, false
		}
		if best < 0 || *e.GwPriority > *candidates[best].GwPriority {
			best = i
		}
	}
	for i, e := range candidates {
		if i != best && *e.GwPriority == *candidates[best].GwPriority {
			return docker.EndpointNetwork{}, false
		}
	}
	return candidates[best], true
}

// containerText is the Address cell of the container row.
func (c pathContainer) containerText() string {
	switch {
	case c.Known && !c.Running:
		return c.Name + " (" + textNotRunning + ")"
	case c.Attach == attachNetwork && c.Addr.IsValid():
		return c.Name + " (" + c.Addr.String() + ")"
	}
	return c.Name
}

// networkText is the Address cell of the network row.
func (c pathContainer) networkText() string {
	switch c.Attach {
	case attachNetwork:
		if c.Gateway.IsValid() {
			return c.Network + " via " + c.Gateway.String()
		}
		return c.Network
	case attachHost:
		return textHostNetwork
	case attachNone:
		return textNoNetwork
	case attachShared:
		return textSharedNetwork
	}
	if c.Networks > 1 {
		return fmt.Sprintf("%s (%d networks)", textUndetermined, c.Networks)
	}
	return textUndetermined
}

// traceText is the Address cell of the trace row.
func (c pathContainer) traceText() string {
	if c.Attach == attachHost {
		return textTraceSharedHost
	}
	if c.Engine != engineNone {
		return textTraceFromEngineHost
	}
	return textTraceFromHost
}

// dimNetwork reports whether the network row says nothing about a path.
func (c pathContainer) dimNetwork() bool {
	return c.Attach == attachUndetermined || c.Attach == attachNone
}
