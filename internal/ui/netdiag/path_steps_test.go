package netdiag

import (
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
)

var (
	stepsRoute = &pathRoute{
		Known:     true,
		Interface: "wlan0",
		Source:    netip.MustParseAddr("192.168.1.21"),
		Gateway:   netip.MustParseAddr("192.168.1.1"),
	}
	stepsHop1 = nettrace.Hop{TTL: 1, Kind: nettrace.HopRouter, Addr: netip.MustParseAddr("192.168.1.1"), RTT: 2 * time.Millisecond}
	stepsHop2 = nettrace.Hop{TTL: 2, Kind: nettrace.HopSilent}
	stepsHop3 = nettrace.Hop{TTL: 3, Kind: nettrace.HopTarget, Addr: netip.MustParseAddr("1.1.1.1"), RTT: 12300 * time.Microsecond}
)

func stepKinds(steps []pathStep) []pathStepKind {
	kinds := make([]pathStepKind, len(steps))
	for i, s := range steps {
		kinds[i] = s.Kind
	}
	return kinds
}

func TestPathStepsNewPathRoute(t *testing.T) {
	tests := []struct {
		name string
		hop  netcheck.RouteHop
		err  error
		want pathRoute
	}{
		{
			name: "all fields",
			hop: netcheck.RouteHop{
				Interface: "eth0",
				Source:    net.IPv4(192, 168, 1, 21),
				Gateway:   net.IPv4(192, 168, 1, 1),
			},
			want: pathRoute{
				Known:     true,
				Interface: "eth0",
				Source:    netip.MustParseAddr("192.168.1.21"),
				Gateway:   netip.MustParseAddr("192.168.1.1"),
			},
		},
		{
			name: "no gateway means directly connected",
			hop:  netcheck.RouteHop{Interface: "eth0", Source: net.ParseIP("10.0.0.2")},
			want: pathRoute{Known: true, Interface: "eth0", Source: netip.MustParseAddr("10.0.0.2")},
		},
		{
			name: "no source",
			hop:  netcheck.RouteHop{Interface: "tun0", Gateway: net.ParseIP("10.0.0.1")},
			want: pathRoute{Known: true, Interface: "tun0", Gateway: netip.MustParseAddr("10.0.0.1")},
		},
		{
			name: "IPv6 source and gateway are kept",
			hop: netcheck.RouteHop{
				Interface: "eth0",
				Source:    net.ParseIP("fd00::2"),
				Gateway:   net.ParseIP("fe80::1"),
			},
			want: pathRoute{
				Known:     true,
				Interface: "eth0",
				Source:    netip.MustParseAddr("fd00::2"),
				Gateway:   netip.MustParseAddr("fe80::1"),
			},
		},
		{
			name: "an error discards whatever the hop holds",
			hop:  netcheck.RouteHop{Interface: "eth0", Source: net.ParseIP("10.0.0.2")},
			err:  net.ErrClosed,
			want: pathRoute{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newPathRoute(tt.hop, tt.err)
			if got != tt.want {
				t.Fatalf("newPathRoute = %+v, want %+v", got, tt.want)
			}
			if got.Source.Is4In6() {
				t.Errorf("source %v must be unmapped to IPv4", got.Source)
			}
			if got.Gateway.Is4In6() {
				t.Errorf("gateway %v must be unmapped to IPv4", got.Gateway)
			}
		})
	}
}

func TestPathStepsOrder(t *testing.T) {
	unknown := &pathRoute{Known: false}
	tests := []struct {
		name  string
		route *pathRoute
		hops  []nettrace.Hop
		want  []pathStepKind
		hopTT []int // TTLs of the stepHop rows, in order
	}{
		{"nothing known yet", nil, nil, nil, nil},
		{"route, no hop yet", stepsRoute, nil,
			[]pathStepKind{stepSource, stepInterface, stepGateway}, nil},
		{"criterion 1: source, interface, gateway, then sorted hops", stepsRoute,
			[]nettrace.Hop{stepsHop3, stepsHop1},
			[]pathStepKind{stepSource, stepInterface, stepGateway, stepHop, stepHop}, []int{1, 3}},
		{"hop before route: the source still comes first", nil,
			[]nettrace.Hop{stepsHop1},
			[]pathStepKind{stepSource, stepHop}, []int{1}},
		{"failed lookup keeps the three local rows", unknown,
			[]nettrace.Hop{stepsHop1, stepsHop2},
			[]pathStepKind{stepSource, stepInterface, stepGateway, stepHop, stepHop}, []int{1, 2}},
		{"empty hop list", stepsRoute, []nettrace.Hop{},
			[]pathStepKind{stepSource, stepInterface, stepGateway}, nil},
		{"unresolved hops are dropped and the list is cut after the target", stepsRoute,
			[]nettrace.Hop{
				stepsHop3,
				{TTL: 2, Kind: nettrace.HopUnresolved},
				{TTL: 4, Kind: nettrace.HopRouter, Addr: netip.MustParseAddr("9.9.9.9")},
				stepsHop1,
			},
			[]pathStepKind{stepSource, stepInterface, stepGateway, stepHop, stepHop}, []int{1, 3}},
		{"the list is cut after an unreachable hop", stepsRoute,
			[]nettrace.Hop{
				stepsHop1,
				{TTL: 3, Kind: nettrace.HopRouter, Addr: netip.MustParseAddr("9.9.9.9")},
				{TTL: 2, Kind: nettrace.HopUnreachable, Addr: netip.MustParseAddr("10.0.0.1")},
			},
			[]pathStepKind{stepSource, stepInterface, stepGateway, stepHop, stepHop}, []int{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hopsBefore := slices.Clone(tt.hops)
			var routeBefore pathRoute
			if tt.route != nil {
				routeBefore = *tt.route
			}

			got := pathSteps(tt.route, tt.hops)

			if kinds := stepKinds(got); !slices.Equal(kinds, tt.want) {
				t.Fatalf("kinds = %v, want %v", kinds, tt.want)
			}
			var ttls []int
			for _, s := range got {
				if s.Kind == stepHop {
					ttls = append(ttls, s.Hop.TTL)
				}
			}
			if !slices.Equal(ttls, tt.hopTT) {
				t.Errorf("hop TTLs = %v, want %v", ttls, tt.hopTT)
			}
			if !slices.EqualFunc(tt.hops, hopsBefore, func(a, b nettrace.Hop) bool { return a == b }) {
				t.Errorf("pathSteps modified its hops input: %v, was %v", tt.hops, hopsBefore)
			}
			if tt.route != nil && *tt.route != routeBefore {
				t.Errorf("pathSteps modified its route input: %+v, was %+v", *tt.route, routeBefore)
			}
		})
	}
}

// cells holds the four displayed texts of a row.
type cells struct{ hop, step, address, rtt string }

func cellsOf(s pathStep) cells {
	return cells{s.hopCell(), s.stepCell(), s.addressCell(), s.rttCell()}
}

func TestPathStepsCells(t *testing.T) {
	route := func(iface, src, gw string) pathRoute {
		r := pathRoute{Known: true, Interface: iface}
		if src != "" {
			r.Source = netip.MustParseAddr(src)
		}
		if gw != "" {
			r.Gateway = netip.MustParseAddr(gw)
		}
		return r
	}
	tests := []struct {
		name string
		step pathStep
		want cells
	}{
		{"source", pathStep{Kind: stepSource, Route: *stepsRoute},
			cells{"", "source", "this machine", ""}},
		{"source ignores a route whose source is a hop address",
			pathStep{Kind: stepSource, Route: route("eth0", "1.1.1.1", "1.1.1.1")},
			cells{"", "source", "this machine", ""}},
		{"source with an unknown route", pathStep{Kind: stepSource},
			cells{"", "source", "this machine", ""}},
		{"interface with source address", pathStep{Kind: stepInterface, Route: *stepsRoute},
			cells{"", "interface", "wlan0 (192.168.1.21)", ""}},
		{"interface without source address", pathStep{Kind: stepInterface, Route: route("tun0", "", "")},
			cells{"", "interface", "tun0", ""}},
		{"interface without name", pathStep{Kind: stepInterface, Route: route("", "192.168.1.21", "")},
			cells{"", "interface", "unnamed (192.168.1.21)", ""}},
		{"criterion 2: a VPN interface is shown as is",
			pathStep{Kind: stepInterface, Route: route("wg0", "10.2.0.2", "")},
			cells{"", "interface", "wg0 (10.2.0.2)", ""}},
		{"interface of a failed lookup", pathStep{Kind: stepInterface},
			cells{"", "interface", "undetermined", ""}},
		{"gateway", pathStep{Kind: stepGateway, Route: *stepsRoute},
			cells{"", "gateway", "192.168.1.1", ""}},
		{"no gateway", pathStep{Kind: stepGateway, Route: route("wg0", "10.2.0.2", "")},
			cells{"", "gateway", "directly connected", ""}},
		{"unspecified IPv4 gateway means directly connected",
			pathStep{Kind: stepGateway, Route: route("eth0", "10.0.0.2", "0.0.0.0")},
			cells{"", "gateway", "directly connected", ""}},
		{"unspecified IPv6 gateway means directly connected",
			pathStep{Kind: stepGateway, Route: route("eth0", "fd00::2", "::")},
			cells{"", "gateway", "directly connected", ""}},
		{"IPv6 gateway", pathStep{Kind: stepGateway, Route: route("eth0", "fd00::2", "fe80::1")},
			cells{"", "gateway", "fe80::1", ""}},
		{"IPv6 source", pathStep{Kind: stepInterface, Route: route("eth0", "fd00::2", "fe80::1")},
			cells{"", "interface", "eth0 (fd00::2)", ""}},
		{"gateway of a failed lookup", pathStep{Kind: stepGateway},
			cells{"", "gateway", "undetermined", ""}},
		{"router hop", pathStep{Kind: stepHop, Hop: stepsHop1},
			cells{"1", "router", "192.168.1.1", "2.0 ms"}},
		{"silent hop", pathStep{Kind: stepHop, Hop: stepsHop2},
			cells{"2", "silent", "-", "-"}},
		{"target hop", pathStep{Kind: stepHop, Hop: stepsHop3},
			cells{"3", "target", "1.1.1.1", "12.3 ms"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cellsOf(tt.step)
			if got != tt.want {
				t.Errorf("cells = %+v, want %+v", got, tt.want)
			}
			// Rule 122: a cell is plain text, colour goes through Style.
			for _, c := range []string{got.hop, got.step, got.address, got.rtt} {
				if ansi.Strip(c) != c {
					t.Errorf("cell %q carries an escape sequence", c)
				}
			}
		})
	}
}

// Criterion 5: whatever the route and the hops, "this machine" labels the
// first row only, and no other row repeats it.
func TestPathStepsSourceIsAlwaysFirstAndAlone(t *testing.T) {
	routes := []*pathRoute{nil, {Known: false}, stepsRoute}
	for _, r := range routes {
		steps := pathSteps(r, []nettrace.Hop{stepsHop1, stepsHop3})
		if len(steps) == 0 || steps[0].Kind != stepSource || steps[0].addressCell() != "this machine" {
			t.Fatalf("route %+v: first row = %+v, want the source", r, steps)
		}
		for _, s := range steps[1:] {
			if s.addressCell() == "this machine" {
				t.Errorf("route %+v: row kind %d also says this machine", r, s.Kind)
			}
		}
	}
}
