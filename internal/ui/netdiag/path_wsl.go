package netdiag

import (
	"net/netip"
	"strings"
	"unicode"

	"github.com/anthnel/devdesk/internal/wslenv"
)

const (
	wslSourcePrefix = "WSL2 (VM), not the Windows host"
	windowsHostTag  = " — Windows host (virtual switch)"
)

// withWSL returns a new slice of steps labelled for WSL2. The input is not
// modified. A zero Info (not WSL2) returns an unchanged copy.
func withWSL(steps []pathStep, info wslenv.Info) []pathStep {
	out := append([]pathStep(nil), steps...)
	if !info.WSL2 {
		return out
	}
	var host netip.Addr
	var labelled bool
	for _, s := range out {
		if s.Kind == stepGateway {
			host, labelled = windowsHostGateway(info, s.Route)
			break
		}
	}
	for i := range out {
		switch s := &out[i]; s.Kind {
		case stepSource:
			s.WSL = info
		case stepGateway:
			s.WindowsHost = labelled
		case stepHop:
			s.WindowsHost = labelled && s.Hop.TTL == 1 && s.Hop.Addr == host
		}
	}
	return out
}

// windowsHostGateway returns the NAT gateway when WSL2 is in NAT mode and
// route r leaves by the NAT interface through that gateway.
func windowsHostGateway(info wslenv.Info, r pathRoute) (netip.Addr, bool) {
	if !info.WSL2 || info.Mode != wslenv.ModeNAT || !info.NATGateway.IsValid() {
		return netip.Addr{}, false
	}
	if !r.Known || r.Gateway != info.NATGateway || r.Interface != info.NATInterface {
		return netip.Addr{}, false
	}
	return info.NATGateway, true
}

// wslSourceText is the source row text when WSL2 is proven.
func wslSourceText(info wslenv.Info) string {
	switch info.Mode {
	case wslenv.ModeNAT:
		return wslSourcePrefix + " · NAT mode"
	case wslenv.ModeMirrored:
		return wslSourcePrefix + " · mirrored mode"
	}
	return wslSourcePrefix + " · mode unknown"
}

// printable drops every non-printable rune so that text read from the host
// (an interface name) can never carry an escape sequence into a cell.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, s)
}
