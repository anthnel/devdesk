package netcheck

import (
	"context"
	"fmt"
	"net"

	netroute "github.com/libp2p/go-netroute"
)

// Route asks this machine's routing table which way out a packet to ip takes.
//
// github.com/libp2p/go-netroute is what makes this one implementation instead
// of three: it wraps GetBestRoute2 on Windows, an RTM_GETROUTE netlink query on
// Linux and the routing socket on the BSDs, and it needs no privilege on any of
// them — measured here at ~2 ms per lookup. Its only dependencies are x/net and
// x/sys, both already in this module's graph.
//
// It answers a query rather than listing the table, and that is why the answer
// lives on a target rather than in a topology screen: "which way out for this
// destination" is a question about something, and it is the question a split
// tunnel makes worth asking.
func (systemEnv) Route(ctx context.Context, ip net.IP) (RouteHop, error) {
	if ip == nil {
		return RouteHop{}, fmt.Errorf("no address to route to")
	}
	// The context is honoured before the call rather than inside it: the lookup
	// is a syscall against a kernel table with no wait to interrupt, so there is
	// nothing to cancel once it starts, and pretending otherwise would be a
	// deadline this method cannot keep.
	if err := ctx.Err(); err != nil {
		return RouteHop{}, err
	}

	// The router is built per call. It holds no connection on Windows, and on
	// Linux it opens a netlink socket that the query closes again; caching one
	// would mean holding a descriptor for the life of the process to save a
	// lookup that already costs two milliseconds.
	router, err := netroute.New()
	if err != nil {
		return RouteHop{}, fmt.Errorf("reading the routing table: %w", err)
	}

	iface, gateway, source, err := router.Route(ip)
	if err != nil {
		return RouteHop{}, err
	}

	hop := RouteHop{Gateway: gateway, Source: source}
	if iface != nil {
		hop.Interface = iface.Name
	}
	return hop, nil
}
