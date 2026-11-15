package netdiag

import (
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/wslenv"
)

const (
	wslSourceNAT          = "WSL2 (VM), not the Windows host · NAT mode"
	wslSourceMirrored     = "WSL2 (VM), not the Windows host · mirrored mode"
	wslSourceUndetermined = "WSL2 (VM), not the Windows host · mode unknown"
	wslHostCell           = "172.27.128.1 — Windows host (virtual switch)"
)

var (
	wslGW        = netip.MustParseAddr("172.27.128.1")
	wslInfoNAT   = wslenv.Info{WSL2: true, Mode: wslenv.ModeNAT, NATInterface: "eth0", NATGateway: wslGW}
	wslRoute     = &pathRoute{Known: true, Interface: "eth0", Source: netip.MustParseAddr("172.27.130.5"), Gateway: wslGW}
	wslHop1      = nettrace.Hop{TTL: 1, Kind: nettrace.HopRouter, Addr: wslGW}
	wslHop1Quiet = nettrace.Hop{TTL: 1, Kind: nettrace.HopSilent}
	wslHop2      = nettrace.Hop{TTL: 2, Kind: nettrace.HopRouter, Addr: netip.MustParseAddr("192.168.1.1")}
	wslHop3GW    = nettrace.Hop{TTL: 3, Kind: nettrace.HopRouter, Addr: wslGW}
)

// wslCells returns the address cell of every step.
func wslCells(steps []pathStep) []string {
	cells := make([]string, len(steps))
	for i, s := range steps {
		cells[i] = s.addressCell()
	}
	return cells
}

// wslStep returns the first step of the kind, or a step of kind -1.
func wslStep(steps []pathStep, kind pathStepKind) pathStep {
	for _, s := range steps {
		if s.Kind == kind {
			return s
		}
	}
	return pathStep{Kind: -1}
}

// wslLabelled reports whether a cell other than the source row names the Windows host.
func wslLabelled(steps []pathStep) bool {
	for _, s := range steps {
		if s.Kind != stepSource && strings.Contains(s.addressCell(), "Windows host") {
			return true
		}
	}
	return false
}

func TestWindowsHostGateway(t *testing.T) {
	mirrored := wslenv.Info{WSL2: true, Mode: wslenv.ModeMirrored, NATInterface: "eth0", NATGateway: wslGW}
	undetermined := wslenv.Info{WSL2: true, NATInterface: "eth0", NATGateway: wslGW}
	noGateway := wslenv.Info{WSL2: true, Mode: wslenv.ModeNAT, NATInterface: "eth0"}
	tests := []struct {
		name  string
		info  wslenv.Info
		route pathRoute
		want  bool
	}{
		{"nat, same interface and gateway", wslInfoNAT, *wslRoute, true},
		{"different interface", wslInfoNAT, pathRoute{Known: true, Interface: "wlan0", Gateway: wslGW}, false},
		{"different gateway", wslInfoNAT, pathRoute{Known: true, Interface: "eth0", Gateway: netip.MustParseAddr("10.0.0.1")}, false},
		{"route not known", wslInfoNAT, pathRoute{Interface: "eth0", Gateway: wslGW}, false},
		{"invalid NATGateway", noGateway, pathRoute{Known: true, Interface: "eth0"}, false},
		{"mirrored", mirrored, *wslRoute, false},
		{"undetermined", undetermined, *wslRoute, false},
		{"not WSL2", wslenv.Info{}, *wslRoute, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gw, ok := windowsHostGateway(tc.info, tc.route)
			if ok != tc.want {
				t.Fatalf("ok = %v, want %v", ok, tc.want)
			}
			if ok && gw != wslGW {
				t.Errorf("gateway = %v, want %v", gw, wslGW)
			}
		})
	}
}

// C1: every WSL2 mode replaces the source row, with or without a container.
func TestWithWSLReplacesTheSourceRow(t *testing.T) {
	tests := []struct {
		name string
		info wslenv.Info
		want string
	}{
		{"nat", wslInfoNAT, wslSourceNAT},
		{"mirrored", wslenv.Info{WSL2: true, Mode: wslenv.ModeMirrored}, wslSourceMirrored},
		{"undetermined", wslenv.Info{WSL2: true}, wslSourceUndetermined},
	}
	ctr := &pathContainer{Name: "web", Known: true, Running: true}
	for _, tc := range tests {
		for _, withCtr := range []bool{false, true} {
			name := tc.name
			var c *pathContainer
			if withCtr {
				name += "/container"
				c = ctr
			}
			t.Run(name, func(t *testing.T) {
				steps := withWSL(pathSteps(c, wslRoute, []nettrace.Hop{wslHop1, wslHop2}), tc.info)
				if got := wslStep(steps, stepSource).addressCell(); got != tc.want {
					t.Errorf("source cell = %q, want %q", got, tc.want)
				}
				if slices.Contains(wslCells(steps), sourceLabel) {
					t.Errorf("%q is still shown", sourceLabel)
				}
			})
		}
	}
}

// C2: NAT labels the gateway row and a TTL 1 hop that is the NAT gateway.
func TestWithWSLNATLabelsTheWindowsHost(t *testing.T) {
	otherHop1 := nettrace.Hop{TTL: 1, Kind: nettrace.HopRouter, Addr: netip.MustParseAddr("10.9.9.9")}
	otherGW := netip.MustParseAddr("10.0.0.1")
	otherGWHop1 := nettrace.Hop{TTL: 1, Kind: nettrace.HopRouter, Addr: otherGW}
	noGateway := wslenv.Info{WSL2: true, Mode: wslenv.ModeNAT, NATInterface: "eth0"}
	tests := []struct {
		name     string
		info     wslenv.Info
		route    *pathRoute
		hops     []nettrace.Hop
		wantGW   bool
		wantHop1 bool
	}{
		{"ttl 1 answers with the gateway", wslInfoNAT, wslRoute, []nettrace.Hop{wslHop1, wslHop2}, true, true},
		{"ttl 1 silent", wslInfoNAT, wslRoute, []nettrace.Hop{wslHop1Quiet, wslHop2}, true, false},
		{"no hops yet", wslInfoNAT, wslRoute, nil, true, false},
		{"ttl 1 answers with another address", wslInfoNAT, wslRoute, []nettrace.Hop{otherHop1}, true, false},
		{"gateway address at ttl 3 is not labelled", wslInfoNAT, wslRoute, []nettrace.Hop{wslHop1Quiet, wslHop2, wslHop3GW}, true, false},
		{"other interface", wslInfoNAT, &pathRoute{Known: true, Interface: "wlan0", Gateway: wslGW}, []nettrace.Hop{wslHop1}, false, false},
		{"other gateway", wslInfoNAT, &pathRoute{Known: true, Interface: "eth0", Gateway: otherGW}, []nettrace.Hop{otherGWHop1}, false, false},
		{"invalid NATGateway", noGateway, wslRoute, []nettrace.Hop{wslHop1}, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			steps := withWSL(pathSteps(nil, tc.route, tc.hops), tc.info)
			if got := wslStep(steps, stepGateway).addressCell() == wslHostCell; got != tc.wantGW {
				t.Errorf("gateway labelled = %v, want %v (cells %q)", got, tc.wantGW, wslCells(steps))
			}
			labelled := 0
			for _, s := range steps {
				if s.Kind == stepHop && s.addressCell() == wslHostCell {
					labelled++
					if s.Hop.TTL != 1 {
						t.Errorf("hop with TTL %d is labelled", s.Hop.TTL)
					}
				}
			}
			if labelled > 1 || (labelled == 1) != tc.wantHop1 {
				t.Errorf("labelled hops = %d, want hop 1 labelled = %v", labelled, tc.wantHop1)
			}
		})
	}
}

// C3: mirrored never claims a Windows host, even when TTL 1 is the gateway.
func TestWithWSLMirroredHasNoWindowsHostLabel(t *testing.T) {
	info := wslenv.Info{WSL2: true, Mode: wslenv.ModeMirrored, NATInterface: "eth0", NATGateway: wslGW}
	steps := withWSL(pathSteps(nil, wslRoute, []nettrace.Hop{wslHop1, wslHop2}), info)
	if got := wslStep(steps, stepSource).addressCell(); got != wslSourceMirrored {
		t.Errorf("source cell = %q, want %q", got, wslSourceMirrored)
	}
	if wslLabelled(steps) {
		t.Errorf("a cell names the Windows host: %q", wslCells(steps))
	}
}

// C5: an undetermined mode gets its own suffix and no Windows label.
func TestWithWSLUndeterminedHasNoWindowsHostLabel(t *testing.T) {
	info := wslenv.Info{WSL2: true, NATInterface: "eth0", NATGateway: wslGW}
	steps := withWSL(pathSteps(nil, wslRoute, []nettrace.Hop{wslHop1}), info)
	if got := wslStep(steps, stepSource).addressCell(); got != wslSourceUndetermined {
		t.Errorf("source cell = %q, want %q", got, wslSourceUndetermined)
	}
	if wslLabelled(steps) {
		t.Errorf("a cell names the Windows host: %q", wslCells(steps))
	}
}

// C4: native Linux (zero Info) is identical to pathSteps alone.
func TestWithWSLZeroInfoChangesNothing(t *testing.T) {
	ctr := &pathContainer{Name: "web", Known: true, Running: true}
	for _, c := range []*pathContainer{nil, ctr} {
		base := pathSteps(c, wslRoute, []nettrace.Hop{wslHop1, wslHop2})
		got := withWSL(base, wslenv.Info{})
		if !slices.Equal(got, base) {
			t.Errorf("steps differ: %q vs %q", wslCells(got), wslCells(base))
		}
		for _, cell := range wslCells(got) {
			if strings.Contains(cell, "WSL") || strings.Contains(cell, "Windows") {
				t.Errorf("cell %q mentions WSL or Windows", cell)
			}
		}
		if wslStep(got, stepSource).addressCell() != sourceLabel {
			t.Error("source row is not this machine")
		}
	}
}

func TestWithWSLDoesNotModifyItsInput(t *testing.T) {
	base := pathSteps(nil, wslRoute, []nettrace.Hop{wslHop1, wslHop2})
	snapshot := slices.Clone(base)
	out := withWSL(base, wslInfoNAT)
	if !slices.Equal(base, snapshot) {
		t.Error("input steps were modified")
	}
	if len(out) > 0 && len(base) > 0 && &out[0] == &base[0] {
		t.Error("result shares its backing array with the input")
	}
	if wslStep(out, stepSource).addressCell() != wslSourceNAT {
		t.Error("the result is not labelled")
	}
}

// The interface name comes from the host: no escape sequence may reach a cell.
func TestWithWSLStripsControlCharactersFromTheInterface(t *testing.T) {
	info := wslInfoNAT
	info.NATInterface = "eth0\x1b[31m"
	tests := []struct{ name, iface, want string }{
		{"escape inside the name", "eth0\x1b[31m", "eth0[31m (172.27.130.5)"},
		{"fully filtered name", "\x1b\x07", "unnamed (172.27.130.5)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			route := *wslRoute
			route.Interface = tc.iface
			steps := withWSL(pathSteps(nil, &route, []nettrace.Hop{wslHop1}), info)
			if got := wslStep(steps, stepInterface).addressCell(); got != tc.want {
				t.Errorf("interface cell = %q, want %q", got, tc.want)
			}
			for _, cell := range wslCells(steps) {
				if strings.ContainsRune(cell, '\x1b') {
					t.Errorf("cell %q holds an escape character", cell)
				}
			}
		})
	}
}

// The labelled source row stays a single line of bounded width.
func TestWithWSLSourceCellIsPlainAndSingleLine(t *testing.T) {
	steps := withWSL(pathSteps(nil, wslRoute, nil), wslInfoNAT)
	cell := wslStep(steps, stepSource).addressCell()
	if cell == sourceLabel {
		t.Fatal("source row is not labelled")
	}
	if cell != ansi.Strip(cell) || strings.ContainsAny(cell, "\n\r\t") {
		t.Errorf("source cell %q is not plain single-line text", cell)
	}
	if w := ansi.StringWidth(cell); w == 0 || w > 47 {
		t.Errorf("source cell width = %d, want 1..47", w)
	}
}

// A no-route lookup proves nothing about the gateway: no Windows host label.
func TestWithWSLNoRouteHasNoWindowsHostLabel(t *testing.T) {
	route := &pathRoute{NoRoute: true}
	steps := withWSL(pathSteps(nil, route, []nettrace.Hop{wslHop1}), wslInfoNAT)
	if wslLabelled(steps) {
		t.Errorf("a cell names the Windows host: %q", wslCells(steps))
	}
	if got := wslStep(steps, stepGateway).addressCell(); got != "-" {
		t.Errorf("gateway cell = %q, want %q", got, "-")
	}
	if got := wslStep(steps, stepInterface).addressCell(); got != "no route" {
		t.Errorf("interface cell = %q, want %q", got, "no route")
	}
}
