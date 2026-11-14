package netdiag

import (
	"context"
	"log"
	"net/netip"
	"time"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/netiface"
)

// bridgeState says what is known about the host interface of a container's
// network. The zero value means the read was not attempted.
type bridgeState int

const (
	bridgeNotRead    bridgeState = iota // nothing to read for this container
	bridgeFound                         // this machine has the interface and it carries the gateway
	bridgeAbsent                        // this machine has no interface of that name
	bridgeUnverified                    // the answer cannot be given with certainty
)

// pathBridge is the outcome of reading the container network's bridge. Name is
// set only when State is bridgeFound, and is display-safe.
type pathBridge struct {
	State bridgeState
	Name  string
}

// readPathBridge reads the host interface that carries the gateway of the
// container's default-route network: the engine gives the interface name, this
// machine's interfaces say whether it exists. A failed read is logged, never
// shown (unless the run was canceled), and reported as bridgeUnverified.
func readPathBridge(ctx context.Context, n docker.ContainerNetworking, timeout time.Duration, e pathEngine) pathBridge {
	if !n.Running {
		return pathBridge{}
	}
	ep, ok := routeEndpoint(n)
	if !ok {
		return pathBridge{}
	}
	if ep.NetworkID == "" {
		return pathBridge{State: bridgeUnverified}
	}
	nctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	nb, err := e.network(nctx, ep.NetworkID)
	if err != nil {
		logBridgeError(ctx, "network", err)
		return pathBridge{State: bridgeUnverified}
	}
	if nb.Name == "" {
		return pathBridge{State: bridgeUnverified}
	}
	ifaces, err := e.interfaces(nctx)
	if err != nil {
		logBridgeError(ctx, "interfaces", err)
		return pathBridge{State: bridgeUnverified}
	}
	b := classifyBridge(nb.Name, ep.Gateway, ifaces)
	if b.State == bridgeFound {
		if b.Name = engineText(b.Name); b.Name == "" {
			return pathBridge{State: bridgeUnverified}
		}
	}
	return b
}

// logBridgeError logs a read failure unless the run was canceled.
func logBridgeError(ctx context.Context, what string, err error) {
	if ctx.Err() == nil {
		log.Printf("ERROR [netdiag/path] bridge %s: %v", what, err)
	}
}

// hostBridge returns the name of this machine's interface called exactly want
// (case sensitive) that carries gw among its IPv4 addresses, or "" when there
// is none. Malformed CIDR entries are ignored.
func hostBridge(want string, gw netip.Addr, ifaces []netiface.Interface) string {
	if want == "" || !gw.IsValid() {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Name != want {
			continue
		}
		for _, cidr := range ifc.IPv4 {
			if p, err := netip.ParsePrefix(cidr); err == nil && p.Addr() == gw {
				return ifc.Name
			}
		}
	}
	return ""
}

// classifyBridge answers three ways: bridgeFound when the interface called
// exactly want carries gw, bridgeAbsent when no interface has that name, and
// bridgeUnverified otherwise (including an empty want or an invalid gw).
func classifyBridge(want string, gw netip.Addr, ifaces []netiface.Interface) pathBridge {
	if want == "" || !gw.IsValid() {
		return pathBridge{State: bridgeUnverified}
	}
	if name := hostBridge(want, gw, ifaces); name != "" {
		return pathBridge{State: bridgeFound, Name: name}
	}
	for _, ifc := range ifaces {
		if ifc.Name == want {
			return pathBridge{State: bridgeUnverified}
		}
	}
	return pathBridge{State: bridgeAbsent}
}
