package netdiag

import (
	"context"
	"log"
	"net/netip"
	"time"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/netiface"
)

// readPathBridge reads the host interface that carries the gateway of the
// container's default-route network: the engine gives the interface name, this
// machine's interfaces say whether it exists. It returns "" when there is
// nothing to find or a read failed (logged, never shown, unless the run was
// canceled). The result is display-safe.
func readPathBridge(ctx context.Context, n docker.ContainerNetworking, timeout time.Duration, e pathEngine) string {
	if !n.Running {
		return ""
	}
	ep, ok := routeEndpoint(n)
	if !ok || ep.NetworkID == "" {
		return ""
	}
	nctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	nb, err := e.network(nctx, ep.NetworkID)
	if err != nil {
		logBridgeError(ctx, "network", err)
		return ""
	}
	if nb.Name == "" {
		return ""
	}
	ifaces, err := e.interfaces(nctx)
	if err != nil {
		logBridgeError(ctx, "interfaces", err)
		return ""
	}
	return engineText(hostBridge(nb.Name, ep.Gateway, ifaces))
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
