package wslenv

import (
	"errors"
	"net/netip"
	"testing"
)

const routeHeader = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"

// natRoutes is the spike's NAT table: default via 172.27.128.1 on eth0.
const natRoutes = routeHeader +
	"eth0\t00000000\t01801BAC\t0003\t0\t0\t0\t00000000\t0\t0\t0\n" +
	"eth0\t00801BAC\t00000000\t0001\t0\t0\t0\t00F0FFFF\t0\t0\t0\n"

func TestClassify(t *testing.T) {
	const nat = "6.18.33.2-microsoft-standard-WSL2"
	gw := netip.MustParseAddr("172.27.128.1")
	nativeIfaces := []string{"lo", "eth0"}
	mirroredIfaces := []string{"lo", "eth0", "eth1", "loopback0"}

	tests := []struct {
		name string
		in   Facts
		want Info
	}{
		{"native linux", Facts{OSRelease: "6.12.0-amd64", Interfaces: nativeIfaces}, Info{}},
		{"nat", Facts{DistroName: "archlinux", OSRelease: nat, NetworkingMode: "nat", Interfaces: nativeIfaces, ProcNetRoute: []byte(natRoutes)},
			Info{WSL2: true, Mode: ModeNAT, NATInterface: "eth0", NATGateway: gw}},
		{"nat, trimmed and upper case", Facts{DistroName: "archlinux", OSRelease: nat, NetworkingMode: " NAT\n", Interfaces: nativeIfaces, ProcNetRoute: []byte(natRoutes)},
			Info{WSL2: true, Mode: ModeNAT, NATInterface: "eth0", NATGateway: gw}},
		{"mirrored", Facts{DistroName: "archlinux", OSRelease: nat, NetworkingMode: "mirrored", Interfaces: mirroredIfaces, ProcNetRoute: []byte(natRoutes)},
			Info{WSL2: true, Mode: ModeMirrored}},
		{"nat reported but loopback0 present", Facts{DistroName: "archlinux", OSRelease: nat, NetworkingMode: "nat", Interfaces: mirroredIfaces, ProcNetRoute: []byte(natRoutes)},
			Info{WSL2: true}},
		{"mirrored reported but no loopback0", Facts{DistroName: "archlinux", OSRelease: nat, NetworkingMode: "mirrored", Interfaces: nativeIfaces, ProcNetRoute: []byte(natRoutes)},
			Info{WSL2: true}},
		{"wslinfo error", Facts{DistroName: "archlinux", OSRelease: nat, NetworkingModeErr: errors.New("exec: wslinfo: not found"), Interfaces: nativeIfaces, ProcNetRoute: []byte(natRoutes)},
			Info{WSL2: true}},
		{"virtioproxy", Facts{DistroName: "archlinux", OSRelease: nat, NetworkingMode: "virtioproxy", Interfaces: nativeIfaces, ProcNetRoute: []byte(natRoutes)},
			Info{WSL2: true}},
		{"env -i: no distro name", Facts{OSRelease: nat, NetworkingMode: "nat", Interfaces: nativeIfaces, ProcNetRoute: []byte(natRoutes)},
			Info{WSL2: true, Mode: ModeNAT, NATInterface: "eth0", NATGateway: gw}},
		{"nat without default route", Facts{DistroName: "archlinux", OSRelease: nat, NetworkingMode: "nat", Interfaces: nativeIfaces, ProcNetRoute: []byte(routeHeader)},
			Info{WSL2: true, Mode: ModeNAT}},
		{"empty mode without error", Facts{DistroName: "archlinux", OSRelease: nat, Interfaces: nativeIfaces, ProcNetRoute: []byte(natRoutes)},
			Info{WSL2: true}},
		{"wsl1", Facts{DistroName: "Ubuntu", OSRelease: "4.4.0-19041-Microsoft", NetworkingMode: "nat", Interfaces: nativeIfaces, ProcNetRoute: []byte(natRoutes)},
			Info{}},
		{"distro name without WSL2 osrelease", Facts{DistroName: "Ubuntu", OSRelease: "6.12.0-amd64"}, Info{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.in)
			if got != tt.want {
				t.Errorf("Classify() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDefaultGateway(t *testing.T) {
	tests := []struct {
		name      string
		route     string
		wantIface string
		wantGW    string
		wantOK    bool
	}{
		{"little endian", natRoutes, "eth0", "172.27.128.1", true},
		{"lowest metric wins", routeHeader +
			"eth3\t00000000\t0101A8C0\t0003\t0\t0\t25\t00000000\t0\t0\t0\n" +
			"eth0\t00000000\t01801BAC\t0003\t0\t0\t1\t00000000\t0\t0\t0\n" +
			"eth1\t00000000\t0201A8C0\t0003\t0\t0\t50\t00000000\t0\t0\t0\n", "eth0", "172.27.128.1", true},
		{"non default routes ignored", routeHeader +
			"eth0\t00801BAC\t00000000\t0001\t0\t0\t0\t00F0FFFF\t0\t0\t0\n" +
			"eth3\t00000000\t0101A8C0\t0003\t0\t0\t25\t00000000\t0\t0\t0\n", "eth3", "192.168.1.1", true},
		{"no route", "", "", "", false},
		{"invalid gateway, 8 chars", routeHeader +
			"eth0\t00000000\tZZZZZZZZ\t0003\t0\t0\t0\t00000000\t0\t0\t0\n", "", "", false},
		{"invalid gateway, 7 chars", routeHeader +
			"eth0\t00000000\t01801BA\t0003\t0\t0\t0\t00000000\t0\t0\t0\n", "", "", false},
		{"zero gateway with lowest metric", routeHeader +
			"eth0\t00000000\t00000000\t0003\t0\t0\t1\t00000000\t0\t0\t0\n" +
			"eth3\t00000000\t0101A8C0\t0003\t0\t0\t25\t00000000\t0\t0\t0\n", "", "", false},
		{"non numeric metric", routeHeader +
			"eth0\t00000000\t01801BAC\t0003\t0\t0\tabc\t00000000\t0\t0\t0\n", "", "", false},
		{"equal metrics, first wins", routeHeader +
			"eth3\t00000000\t0101A8C0\t0003\t0\t0\t5\t00000000\t0\t0\t0\n" +
			"eth0\t00000000\t01801BAC\t0003\t0\t0\t5\t00000000\t0\t0\t0\n", "eth3", "192.168.1.1", true},
		{"header only", routeHeader, "", "", false},
		{"no default route", routeHeader +
			"eth0\t00801BAC\t00000000\t0001\t0\t0\t0\t00F0FFFF\t0\t0\t0\n", "", "", false},
		{"malformed line", routeHeader + "eth0\t00000000\tZZZZ\n", "", "", false},
		{"truncated line", routeHeader + "eth0\t00000000\n", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iface, gw, ok := defaultGateway([]byte(tt.route))
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if iface != tt.wantIface || gw != netip.MustParseAddr(tt.wantGW) {
				t.Errorf("got (%q, %v), want (%q, %s)", iface, gw, tt.wantIface, tt.wantGW)
			}
		})
	}
}
