package netdiag

import (
	"context"
	"errors"
	"net/netip"
	"slices"

	"github.com/anthnel/devdesk/internal/netcheck"
)

// pathRouteFunc is the shape of pathEngine.route.
type pathRouteFunc func(ctx context.Context, a netip.Addr) (netcheck.RouteHop, error)

// pathKnownRoutes is the route stage's lookups for one host, handed to the
// trace so the same route is not read twice (DEVDESK-473). The zero value
// knows nothing.
type pathKnownRoutes struct {
	host    string
	lookups []netcheck.RouteLookup // cloned: never shared with Model.results
}

// knownRoutesOf builds the snapshot for host from r; empty when host is empty.
// RouteLookups already returns a new slice, so the snapshot never shares it
// with Model.results.
func knownRoutesOf(host string, r netcheck.Results) pathKnownRoutes {
	if host == "" {
		return pathKnownRoutes{}
	}
	return pathKnownRoutes{host: host, lookups: r.RouteLookups()}
}

// find returns the lookup for a, when the snapshot is for host. Addresses are
// compared after Unmap: the route stage holds 16-byte IPv4 forms, the trace
// 4-byte ones. A lookup that ended on a cancellation or a deadline is not a
// reading of the routing table (systemEnv.Route returns ctx.Err()), so it is
// never found.
func (k pathKnownRoutes) find(host string, a netip.Addr) (netcheck.RouteLookup, bool) {
	if k.host == "" || k.host != host {
		return netcheck.RouteLookup{}, false
	}
	a = a.Unmap()
	for _, l := range k.lookups {
		if ip, ok := netip.AddrFromSlice(l.IP); ok && ip.Unmap() == a {
			if errors.Is(l.Err, context.Canceled) || errors.Is(l.Err, context.DeadlineExceeded) {
				return netcheck.RouteLookup{}, false
			}
			return l, true
		}
	}
	return netcheck.RouteLookup{}, false
}

// routeFirst returns a route function that answers from the snapshot for host
// and falls back to read, which it calls at most once per call.
func (k pathKnownRoutes) routeFirst(host string, read pathRouteFunc) pathRouteFunc {
	return func(ctx context.Context, a netip.Addr) (netcheck.RouteHop, error) {
		if l, ok := k.find(host, a); ok {
			return l.Hop, l.Err
		}
		return read(ctx, a)
	}
}

// prefer returns addrs with the first IPv4 address the snapshot holds a reading
// of moved to the front, the others in their order, so that the trace takes an
// address Diagnostics already read when its own resolution returns one. Only
// addresses of addrs are ever chosen: the snapshot orders a resolution, it adds
// nothing to it. With nothing read, for another host, or with no IPv4 address in
// common, addrs is returned as it is (the first address is traced, D144). The
// result never shares memory with addrs.
func (k pathKnownRoutes) prefer(host string, addrs []netip.Addr) []netip.Addr {
	out := slices.Clone(addrs)
	for i, a := range out {
		if !a.Unmap().Is4() {
			continue
		}
		if _, ok := k.find(host, a); !ok {
			continue
		}
		return slices.Insert(slices.Delete(out, i, i+1), 0, a)
	}
	return out
}
