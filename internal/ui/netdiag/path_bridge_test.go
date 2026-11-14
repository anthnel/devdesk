package netdiag

import (
	"net/netip"
	"reflect"
	"testing"

	"github.com/anthnel/devdesk/internal/netiface"
)

// TestHostBridge pins hostBridge: the name of this machine's interface called
// want that carries gw among its IPv4 addresses, or "" otherwise.
func TestHostBridge(t *testing.T) {
	gw17 := netip.MustParseAddr("172.17.0.1")
	gw18 := netip.MustParseAddr("172.18.0.1")

	tests := []struct {
		name   string
		want   string
		gw     netip.Addr
		ifaces []netiface.Interface
		expect string
	}{
		{
			name: "docker0 carries the gateway",
			want: "docker0", gw: gw17,
			ifaces: []netiface.Interface{
				{Name: "eth0", IPv4: []string{"192.168.1.10/24"}},
				{Name: "docker0", IPv4: []string{"172.17.0.1/16"}},
			},
			expect: "docker0",
		},
		{
			name: "user-defined bridge carries the gateway",
			want: "br-3f2a1b4c5d6e", gw: gw18,
			ifaces: []netiface.Interface{
				{Name: "lo", IPv4: []string{"127.0.0.1/8"}},
				{Name: "br-3f2a1b4c5d6e", IPv4: []string{"172.18.0.1/16"}},
			},
			expect: "br-3f2a1b4c5d6e",
		},
		{
			name: "no interface of that name",
			want: "docker0", gw: gw17,
			ifaces: []netiface.Interface{
				{Name: "eth0", IPv4: []string{"192.168.1.10/24"}},
				{Name: "lo", IPv4: []string{"127.0.0.1/8"}},
			},
			expect: "",
		},
		{
			name: "interface exists but carries another address",
			want: "docker0", gw: gw17,
			ifaces: []netiface.Interface{
				{Name: "docker0", IPv4: []string{"172.20.0.1/16"}},
			},
			expect: "",
		},
		{
			name: "gateway carried by an interface of another name",
			want: "docker0", gw: gw17,
			ifaces: []netiface.Interface{
				{Name: "docker0", IPv4: []string{"172.20.0.1/16"}},
				{Name: "br-abc", IPv4: []string{"172.17.0.1/16"}},
			},
			expect: "",
		},
		{
			name: "empty want",
			want: "", gw: gw17,
			ifaces: []netiface.Interface{
				{Name: "", IPv4: []string{"172.17.0.1/16"}},
				{Name: "docker0", IPv4: []string{"172.17.0.1/16"}},
			},
			expect: "",
		},
		{
			name: "invalid gateway",
			want: "docker0", gw: netip.Addr{},
			ifaces: []netiface.Interface{
				{Name: "docker0", IPv4: []string{"172.17.0.1/16"}},
			},
			expect: "",
		},
		{
			name: "nil interfaces",
			want: "docker0", gw: gw17,
			ifaces: nil,
			expect: "",
		},
		{
			name: "IPv6 only",
			want: "docker0", gw: gw17,
			ifaces: []netiface.Interface{
				{Name: "docker0", IPv6: []string{"fe80::1/64", "172.17.0.1/16"}},
			},
			expect: "",
		},
		{
			name: "malformed CIDR is ignored, a valid sibling is still found",
			want: "docker0", gw: gw17,
			ifaces: []netiface.Interface{
				{Name: "docker0", IPv4: []string{"not-a-cidr", "172.17.0.1", "", "172.17.0.1/16"}},
			},
			expect: "docker0",
		},
		{
			name: "only malformed CIDRs",
			want: "docker0", gw: gw17,
			ifaces: []netiface.Interface{
				{Name: "docker0", IPv4: []string{"garbage", "172.17.0.1/99"}},
			},
			expect: "",
		},
		{
			name: "name match is case sensitive (want capitalised)",
			want: "Docker0", gw: gw17,
			ifaces: []netiface.Interface{
				{Name: "docker0", IPv4: []string{"172.17.0.1/16"}},
			},
			expect: "",
		},
		{
			name: "name match is case sensitive (interface capitalised)",
			want: "docker0", gw: gw17,
			ifaces: []netiface.Interface{
				{Name: "Docker0", IPv4: []string{"172.17.0.1/16"}},
			},
			expect: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hostBridge(tt.want, tt.gw, tt.ifaces); got != tt.expect {
				t.Errorf("hostBridge(%q, %v) = %q, want %q", tt.want, tt.gw, got, tt.expect)
			}
		})
	}
}

// TestHostBridgeDoesNotModifyItsInput checks the interface list is left
// untouched, down to the backing arrays of the address slices.
func TestHostBridgeDoesNotModifyItsInput(t *testing.T) {
	ifaces := []netiface.Interface{
		{Name: "eth0", IPv4: []string{"192.168.1.10/24"}, IPv6: []string{"fe80::1/64"}},
		{Name: "docker0", IPv4: []string{"bad", "172.17.0.1/16"}},
	}
	before := make([]netiface.Interface, len(ifaces))
	for i, ifc := range ifaces {
		before[i] = ifc
		before[i].IPv4 = append([]string(nil), ifc.IPv4...)
		before[i].IPv6 = append([]string(nil), ifc.IPv6...)
	}

	got := hostBridge("docker0", netip.MustParseAddr("172.17.0.1"), ifaces)
	if got != "docker0" {
		t.Fatalf("hostBridge = %q, want docker0", got)
	}
	if !reflect.DeepEqual(ifaces, before) {
		t.Errorf("hostBridge modified its input:\n got %#v\nwant %#v", ifaces, before)
	}
}

// TestClassifyBridge pins the three-way answer (DEVDESK-332): Found when the
// interface called want carries gw, Absent when no interface has that name,
// Unverified when the answer cannot be given with certainty.
func TestClassifyBridge(t *testing.T) {
	gw := netip.MustParseAddr("172.17.0.1")
	gw6 := netip.MustParseAddr("fd00::1")
	tests := []struct {
		name   string
		want   string
		gw     netip.Addr
		ifaces []netiface.Interface
		expect pathBridge
	}{
		{"exact name carries the gateway", "docker0", gw,
			[]netiface.Interface{{Name: "eth0", IPv4: []string{"192.168.1.2/24"}}, {Name: "docker0", IPv4: []string{"172.17.0.1/16"}}},
			pathBridge{State: bridgeFound, Name: "docker0"}},
		{"no interface of that name", "docker0", gw,
			[]netiface.Interface{{Name: "eth0", IPv4: []string{"172.17.0.1/16"}}},
			pathBridge{State: bridgeAbsent}},
		{"empty interface list", "docker0", gw, nil, pathBridge{State: bridgeAbsent}},
		{"name differs by case only", "docker0", gw,
			[]netiface.Interface{{Name: "Docker0", IPv4: []string{"172.17.0.1/16"}}},
			pathBridge{State: bridgeAbsent}},
		{"right name, other address", "docker0", gw,
			[]netiface.Interface{{Name: "docker0", IPv4: []string{"10.9.0.1/16"}}},
			pathBridge{State: bridgeUnverified}},
		{"right name, no address", "docker0", gw,
			[]netiface.Interface{{Name: "docker0"}},
			pathBridge{State: bridgeUnverified}},
		{"gateway only in IPv6", "docker0", gw6,
			[]netiface.Interface{{Name: "docker0", IPv4: []string{"172.17.0.1/16"}, IPv6: []string{"fd00::1/64"}}},
			pathBridge{State: bridgeUnverified}},
		{"malformed CIDR only", "docker0", gw,
			[]netiface.Interface{{Name: "docker0", IPv4: []string{"bad", "172.17.0.1"}}},
			pathBridge{State: bridgeUnverified}},
		{"malformed CIDR then a good one", "docker0", gw,
			[]netiface.Interface{{Name: "docker0", IPv4: []string{"bad", "172.17.0.1/16"}}},
			pathBridge{State: bridgeFound, Name: "docker0"}},
		{"empty wanted name", "", gw,
			[]netiface.Interface{{Name: "docker0", IPv4: []string{"172.17.0.1/16"}}},
			pathBridge{State: bridgeUnverified}},
		{"invalid gateway", "docker0", netip.Addr{},
			[]netiface.Interface{{Name: "docker0", IPv4: []string{"172.17.0.1/16"}}},
			pathBridge{State: bridgeUnverified}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyBridge(tt.want, tt.gw, tt.ifaces); got != tt.expect {
				t.Errorf("classifyBridge(%q, %v) = %+v, want %+v", tt.want, tt.gw, got, tt.expect)
			}
		})
	}
}

// TestClassifyBridgeDoesNotModifyItsInput checks the list is left untouched.
func TestClassifyBridgeDoesNotModifyItsInput(t *testing.T) {
	ifaces := []netiface.Interface{
		{Name: "eth0", IPv4: []string{"192.168.1.10/24"}, IPv6: []string{"fe80::1/64"}},
		{Name: "docker0", IPv4: []string{"bad", "172.17.0.1/16"}},
	}
	before := make([]netiface.Interface, len(ifaces))
	for i, ifc := range ifaces {
		before[i] = ifc
		before[i].IPv4 = append([]string(nil), ifc.IPv4...)
		before[i].IPv6 = append([]string(nil), ifc.IPv6...)
	}
	got := classifyBridge("docker0", netip.MustParseAddr("172.17.0.1"), ifaces)
	if got.State != bridgeFound || got.Name != "docker0" {
		t.Fatalf("classifyBridge = %+v, want Found docker0", got)
	}
	if !reflect.DeepEqual(ifaces, before) {
		t.Errorf("classifyBridge modified its input:\n got %#v\nwant %#v", ifaces, before)
	}
}
