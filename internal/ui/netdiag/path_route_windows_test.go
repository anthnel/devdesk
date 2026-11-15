//go:build windows

package netdiag

import (
	"fmt"
	"reflect"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/anthnel/devdesk/internal/netcheck"
)

// DEVDESK-492: go-netroute answers a raw windows.Errno when Windows has no
// route; the interface row must say "no route", not "undetermined".
func TestWindowsNoRouteErrnoSaysNoRouteOnTheInterfaceRow(t *testing.T) {
	const errNetworkUnreachable = windows.Errno(1231) // ERROR_NETWORK_UNREACHABLE
	for name, err := range map[string]error{
		"raw":     errNetworkUnreachable,
		"wrapped": fmt.Errorf("route lookup: %w", errNetworkUnreachable),
	} {
		t.Run(name, func(t *testing.T) {
			pm := resolvedWith(t, netcheck.RouteHop{}, err)

			if got, want := pmAddrs(pm), []string{"this machine", "no route", "-"}; !reflect.DeepEqual(got, want) {
				t.Errorf("Address column = %q, want %q", got, want)
			}
			if pm.route == nil || !pm.route.NoRoute {
				t.Errorf("route = %+v, want NoRoute", pm.route)
			}
		})
	}
}
