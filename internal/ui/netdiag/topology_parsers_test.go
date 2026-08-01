package netdiag

import (
	"reflect"
	"testing"
)

// Fixtures below are verbatim samples of iproute2 / iptables / nft output as
// produced inside the ephemeral diagnostic containers. Keeping them literal is
// deliberate: these parsers exist only to survive upstream format changes, so a
// hand-simplified fixture would defeat the purpose of the test.

func TestParseIPAddr(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []InterfaceInfo
	}{
		{
			name: "empty input yields no interfaces",
			raw:  "",
			want: nil,
		},
		{
			name: "loopback is reported as LOOP regardless of state token",
			raw: `1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
    inet 127.0.0.1/8 scope host lo
       valid_lft forever preferred_lft forever
    inet6 ::1/128 scope host
       valid_lft forever preferred_lft forever`,
			want: []InterfaceInfo{
				{Name: "lo", State: "LOOP", MTU: 65536, Addresses: []string{"127.0.0.1/8", "::1/128"}},
			},
		},
		{
			name: "regular interface with a single IPv4 address",
			raw: `2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc mq state UP group default qlen 1000
    link/ether 02:42:ac:11:00:02 brd ff:ff:ff:ff:ff:ff
    inet 172.17.0.2/16 brd 172.17.255.255 scope global eth0
       valid_lft forever preferred_lft forever`,
			want: []InterfaceInfo{
				{Name: "eth0", State: "UP", MTU: 1500, Addresses: []string{"172.17.0.2/16"}},
			},
		},
		{
			name: "unplugged interface is DOWN even though the UP flag is set",
			raw: `3: docker0: <NO-CARRIER,BROADCAST,MULTICAST,UP> mtu 1500 qdisc noqueue state DOWN group default
    link/ether 02:42:8f:1c:2a:3b brd ff:ff:ff:ff:ff:ff
    inet 172.18.0.1/16 brd 172.18.255.255 scope global docker0`,
			want: []InterfaceInfo{
				{Name: "docker0", State: "DOWN", MTU: 1500, Addresses: []string{"172.18.0.1/16"}},
			},
		},
		{
			name: "tunnel reporting state UNKNOWN falls back to its flags and stays UP",
			raw: `9: tun0: <POINTOPOINT,MULTICAST,NOARP,UP,LOWER_UP> mtu 1500 qdisc pfifo_fast state UNKNOWN group default qlen 500
    inet 10.8.0.6/24 scope global tun0`,
			want: []InterfaceInfo{
				{Name: "tun0", State: "UP", MTU: 1500, Addresses: []string{"10.8.0.6/24"}},
			},
		},
		{
			name: "veth peer suffix is stripped from the interface name",
			raw: `7: eth0@if8: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1450 qdisc noqueue state UP group default
    inet 10.244.1.5/24 brd 10.244.1.255 scope global eth0`,
			want: []InterfaceInfo{
				{Name: "eth0", State: "UP", MTU: 1450, Addresses: []string{"10.244.1.5/24"}},
			},
		},
		{
			name: "interface with no address yields an empty address list",
			raw:  `4: dummy0: <BROADCAST,NOARP> mtu 1500 qdisc noop state DOWN group default qlen 1000`,
			want: []InterfaceInfo{
				{Name: "dummy0", State: "DOWN", MTU: 1500},
			},
		},
		{
			name: "multiple interfaces are all captured in order",
			raw: `1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000
    inet 127.0.0.1/8 scope host lo
2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc mq state UP group default qlen 1000
    inet 192.168.1.42/24 brd 192.168.1.255 scope global eth0
3: wlan0: <BROADCAST,MULTICAST> mtu 1500 qdisc noop state DOWN group default qlen 1000`,
			want: []InterfaceInfo{
				{Name: "lo", State: "LOOP", MTU: 65536, Addresses: []string{"127.0.0.1/8"}},
				{Name: "eth0", State: "UP", MTU: 1500, Addresses: []string{"192.168.1.42/24"}},
				{Name: "wlan0", State: "DOWN", MTU: 1500},
			},
		},
		{
			name: "garbage lines are ignored rather than producing phantom interfaces",
			raw: `Cannot open netlink socket: Permission denied
some unrelated text`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIPAddr(tt.raw)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseIPAddr() =\n  %+v\nwant\n  %+v", got, tt.want)
			}
		})
	}
}

func TestParseIfaceState(t *testing.T) {
	tests := []struct {
		name  string
		flags string
		want  string
	}{
		{"loopback wins over UP", "<LOOPBACK,UP,LOWER_UP>", "LOOP"},
		{"up in the middle of the flag list", "<BROADCAST,MULTICAST,UP,LOWER_UP>", "UP"},
		{"up as the last flag", "<NO-CARRIER,BROADCAST,MULTICAST,UP>", "UP"},
		{"up as the only flag", "<UP>", "UP"},
		{"no up flag means down", "<BROADCAST,MULTICAST>", "DOWN"},
		{"empty flags means down", "", "DOWN"},
		{"lowercase flags are normalised", "<broadcast,multicast,up,lower_up>", "UP"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseIfaceState(tt.flags); got != tt.want {
				t.Errorf("parseIfaceState(%q) = %q, want %q", tt.flags, got, tt.want)
			}
		})
	}
}

func TestOperState(t *testing.T) {
	tests := []struct {
		name      string
		token     string
		want      string
		wantKnown bool
	}{
		{"up is authoritative", "UP", "UP", true},
		{"down is authoritative", "DOWN", "DOWN", true},
		{"lower layer down maps to down", "LOWERLAYERDOWN", "DOWN", true},
		{"dormant maps to down", "DORMANT", "DOWN", true},
		{"unknown defers to the interface flags", "UNKNOWN", "", false},
		{"token case is normalised", "up", "UP", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, known := operState(tt.token)
			if got != tt.want || known != tt.wantKnown {
				t.Errorf("operState(%q) = (%q, %v), want (%q, %v)", tt.token, got, known, tt.want, tt.wantKnown)
			}
		})
	}
}

func TestParseIPLink(t *testing.T) {
	// `ip -s link show` — the stat values sit on the line *after* each RX:/TX: header.
	raw := `1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
    RX: bytes  packets  errors  dropped missed  mcast
    1000       10       0       0       0       0
    TX: bytes  packets  errors  dropped carrier collsns
    1000       10       0       0       0       0
2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc mq state UP mode DEFAULT group default qlen 1000
    link/ether 02:42:ac:11:00:02 brd ff:ff:ff:ff:ff:ff
    RX: bytes  packets  errors  dropped missed  mcast
    98765432   12345    7       2       0       33
    TX: bytes  packets  errors  dropped carrier collsns
    12345678   6789     4       1       0       0`

	in := []InterfaceInfo{
		{Name: "lo", State: "LOOP"},
		{Name: "eth0", State: "UP"},
	}
	want := []InterfaceInfo{
		{Name: "lo", State: "LOOP", RxErrors: 0, TxErrors: 0},
		{Name: "eth0", State: "UP", RxErrors: 7, TxErrors: 4},
	}

	got := parseIPLink(raw, in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseIPLink() =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestParseIPLinkIgnoresUnknownInterfaces(t *testing.T) {
	// An interface present in `ip -s link` but absent from the caller's slice must
	// not panic or append a new entry.
	raw := `9: tun0: <POINTOPOINT,MULTICAST,NOARP,UP,LOWER_UP> mtu 1500 qdisc pfifo_fast state UNKNOWN mode DEFAULT
    RX: bytes  packets  errors  dropped missed  mcast
    500        5        99      0       0       0
    TX: bytes  packets  errors  dropped carrier collsns
    500        5        88      0       0       0`

	in := []InterfaceInfo{{Name: "eth0", State: "UP"}}
	got := parseIPLink(raw, in)

	if len(got) != 1 {
		t.Fatalf("parseIPLink() returned %d interfaces, want 1", len(got))
	}
	if got[0].RxErrors != 0 || got[0].TxErrors != 0 {
		t.Errorf("parseIPLink() leaked tun0 stats into eth0: rx=%d tx=%d", got[0].RxErrors, got[0].TxErrors)
	}
}

func TestParseIPRoute(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []RouteInfo
	}{
		{
			name: "empty input yields no routes",
			raw:  "",
			want: nil,
		},
		{
			name: "default route carries a gateway",
			raw:  "default via 192.168.1.1 dev eth0 proto dhcp metric 100",
			want: []RouteInfo{{Destination: "default", Gateway: "192.168.1.1", Interface: "eth0"}},
		},
		{
			name: "directly connected route has no gateway",
			raw:  "172.17.0.0/16 dev docker0 proto kernel scope link src 172.17.0.1",
			want: []RouteInfo{{Destination: "172.17.0.0/16", Interface: "docker0"}},
		},
		{
			name: "full routing table",
			raw: `default via 192.168.1.1 dev eth0 proto dhcp metric 100
169.254.0.0/16 dev eth0 scope link metric 1000
172.17.0.0/16 dev docker0 proto kernel scope link src 172.17.0.1
192.168.1.0/24 dev eth0 proto kernel scope link src 192.168.1.42 metric 100`,
			want: []RouteInfo{
				{Destination: "default", Gateway: "192.168.1.1", Interface: "eth0"},
				{Destination: "169.254.0.0/16", Interface: "eth0"},
				{Destination: "172.17.0.0/16", Interface: "docker0"},
				{Destination: "192.168.1.0/24", Interface: "eth0"},
			},
		},
		{
			name: "blank lines are skipped",
			raw:  "\n\ndefault via 10.0.0.1 dev eth0\n\n",
			want: []RouteInfo{{Destination: "default", Gateway: "10.0.0.1", Interface: "eth0"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIPRoute(tt.raw)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseIPRoute() =\n  %+v\nwant\n  %+v", got, tt.want)
			}
		})
	}
}

func TestParseIPNeigh(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []NeighbourInfo
	}{
		{
			name: "empty input yields no neighbours",
			raw:  "",
			want: nil,
		},
		{
			name: "reachable neighbour with a MAC address",
			raw:  "192.168.1.1 dev eth0 lladdr aa:bb:cc:dd:ee:ff REACHABLE",
			want: []NeighbourInfo{
				{IP: "192.168.1.1", MAC: "aa:bb:cc:dd:ee:ff", Interface: "eth0", State: "REACHABLE"},
			},
		},
		{
			name: "failed neighbour has no MAC address",
			raw:  "192.168.1.99 dev eth0  FAILED",
			want: []NeighbourInfo{
				{IP: "192.168.1.99", Interface: "eth0", State: "FAILED"},
			},
		},
		{
			name: "router flag before the state token is not mistaken for the state",
			raw:  "10.0.0.5 dev wlan0 lladdr de:ad:be:ef:00:01 router STALE",
			want: []NeighbourInfo{
				{IP: "10.0.0.5", MAC: "de:ad:be:ef:00:01", Interface: "wlan0", State: "STALE"},
			},
		},
		{
			name: "truncated line is skipped rather than half-parsed",
			raw:  "192.168.1.7 dev eth0",
			want: nil,
		},
		{
			name: "mixed neighbour table",
			raw: `192.168.1.1 dev eth0 lladdr aa:bb:cc:dd:ee:ff REACHABLE
192.168.1.20 dev eth0 lladdr 11:22:33:44:55:66 STALE
192.168.1.99 dev eth0  FAILED`,
			want: []NeighbourInfo{
				{IP: "192.168.1.1", MAC: "aa:bb:cc:dd:ee:ff", Interface: "eth0", State: "REACHABLE"},
				{IP: "192.168.1.20", MAC: "11:22:33:44:55:66", Interface: "eth0", State: "STALE"},
				{IP: "192.168.1.99", Interface: "eth0", State: "FAILED"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIPNeigh(tt.raw)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseIPNeigh() =\n  %+v\nwant\n  %+v", got, tt.want)
			}
		})
	}
}

func TestParseFirewallDispatch(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantSource string
		wantChains int
	}{
		{
			name:       "iptables prefix selects the iptables parser",
			raw:        "IPTABLES:\nChain INPUT (policy ACCEPT)\n",
			wantSource: "iptables",
			wantChains: 1,
		},
		{
			name:       "nftables prefix selects the nftables parser",
			raw:        "NFTABLES:\ntable inet filter {\n\tchain input {\n\t}\n}\n",
			wantSource: "nftables",
			wantChains: 1,
		},
		{
			name:       "unknown prefix reports unavailable",
			raw:        "command not found: iptables",
			wantSource: "unavailable",
			wantChains: 0,
		},
		{
			name:       "empty output reports unavailable",
			raw:        "",
			wantSource: "unavailable",
			wantChains: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chains, source := parseFirewall(tt.raw)
			if source != tt.wantSource {
				t.Errorf("parseFirewall() source = %q, want %q", source, tt.wantSource)
			}
			if len(chains) != tt.wantChains {
				t.Errorf("parseFirewall() returned %d chains, want %d", len(chains), tt.wantChains)
			}
		})
	}
}

func TestParseFirewallIPTables(t *testing.T) {
	raw := `Chain INPUT (policy ACCEPT)
num  target     prot opt source               destination
1    ACCEPT     all  --  0.0.0.0/0            0.0.0.0/0
2    DROP       tcp  --  0.0.0.0/0            0.0.0.0/0

Chain FORWARD (policy DROP)
num  target     prot opt source               destination

Chain DOCKER-USER (1 references)
num  target     prot opt source               destination
1    RETURN     all  --  0.0.0.0/0            0.0.0.0/0`

	want := []FirewallChain{
		{Name: "INPUT", Policy: "ACCEPT", Rules: 2},
		{Name: "FORWARD", Policy: "DROP", Rules: 0},
		{Name: "DOCKER-USER", Policy: "n/a", Rules: 1},
	}

	got := parseFirewallIPTables(raw)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseFirewallIPTables() =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestParseFirewallIPTablesEmpty(t *testing.T) {
	if got := parseFirewallIPTables(""); got != nil {
		t.Errorf("parseFirewallIPTables(\"\") = %+v, want nil", got)
	}
}

func TestParseFirewallNFTables(t *testing.T) {
	raw := `table inet filter {
	chain input {
		type filter hook input priority 0; policy drop;
		ct state established,related accept
		iif "lo" accept
	}

	chain forward {
		type filter hook forward priority 0; policy accept;
	}

	chain output {
		type filter hook output priority 0; policy accept;
		ct state established,related accept
	}
}`

	want := []FirewallChain{
		{Name: "input", Policy: "DROP", Rules: 2},
		{Name: "forward", Policy: "ACCEPT", Rules: 0},
		{Name: "output", Policy: "ACCEPT", Rules: 1},
	}

	got := parseFirewallNFTables(raw)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseFirewallNFTables() =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestParseFirewallNFTablesEmpty(t *testing.T) {
	if got := parseFirewallNFTables(""); got != nil {
		t.Errorf("parseFirewallNFTables(\"\") = %+v, want nil", got)
	}
}
