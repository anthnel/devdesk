package netdiag

import (
	"context"
	"net/netip"

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
//
// TODO(DEVDESK-473): declaration only, written for the red tests of step 2.
// The coder replaces this body.
func knownRoutesOf(host string, r netcheck.Results) pathKnownRoutes {
	return pathKnownRoutes{}
}

// find returns the lookup for a, when the snapshot is for host.
//
// TODO(DEVDESK-473): declaration only, written for the red tests of step 2.
// The coder replaces this body.
func (k pathKnownRoutes) find(host string, a netip.Addr) (netcheck.RouteLookup, bool) {
	return netcheck.RouteLookup{}, false
}

// routeFirst returns a route function that answers from the snapshot for host
// and falls back to read, which it calls at most once per call.
//
// TODO(DEVDESK-473): declaration only, written for the red tests of step 2.
// It always reads for now. The coder replaces this body.
func (k pathKnownRoutes) routeFirst(host string, read pathRouteFunc) pathRouteFunc {
	return read
}
