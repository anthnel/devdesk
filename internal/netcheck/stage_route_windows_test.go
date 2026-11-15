//go:build windows

package netcheck

import (
	"context"
	"net"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// On Windows go-netroute hands back a raw windows.Errno. The stage must still
// treat ERROR_NETWORK_UNREACHABLE (1231) as proof of no route, by value.
func TestAWindowsNoRouteErrnoIsProofOfNoRoute(t *testing.T) {
	noRoute := windows.Errno(1231)
	routed := RouteHop{Interface: "Ethernet 2", Source: net.ParseIP("192.168.1.21")}
	v4, v6 := net.ParseIP("93.184.216.34"), net.ParseIP("2606:2800:220:1::1")

	t.Run("every address fails", func(t *testing.T) {
		env := fakeEnv{route: func(context.Context, net.IP) (RouteHop, error) { return RouteHop{}, noRoute }}
		c := routeCheck(t, env, target(), resolvedTo(t, v4, v6))
		if c.Verdict != Fail {
			t.Fatalf("verdict = %v (%s), want Fail", c.Verdict, c.Summary)
		}
		if !strings.Contains(c.Summary, "has no local route") {
			t.Errorf("summary = %q, want it to say \"has no local route\"", c.Summary)
		}
	})

	t.Run("one address is routed", func(t *testing.T) {
		env := fakeEnv{route: func(_ context.Context, ip net.IP) (RouteHop, error) {
			if ip.To4() == nil {
				return RouteHop{}, noRoute
			}
			return routed, nil
		}}
		c := routeCheck(t, env, target(), resolvedTo(t, v4, v6))
		if c.Verdict != Warn || c.Reason != ReasonPartialRoute {
			t.Fatalf("verdict = %v/%q (%s), want Warn/partial-route", c.Verdict, c.Reason, c.Summary)
		}
	})
}
