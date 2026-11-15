package wslenv

import (
	"encoding/hex"
	"net/netip"
	"strconv"
	"strings"
)

// Column positions in /proc/net/route.
const (
	colIface   = 0
	colDest    = 1
	colGateway = 2
	colMetric  = 6
	routeCols  = 7
)

// defaultGateway returns the interface and gateway of the default route with
// the lowest metric in a /proc/net/route dump (the first one on equal
// metrics). ok is false when there is no such route, or when that route has no
// gateway (0.0.0.0): a higher-metric route is never used instead. Lines with
// too few columns, an undecodable gateway or a non-numeric metric are skipped.
func defaultGateway(procNetRoute []byte) (iface string, gw netip.Addr, ok bool) {
	var best uint64
	for _, line := range strings.Split(string(procNetRoute), "\n") {
		f := strings.Fields(line)
		if len(f) < routeCols || f[colDest] != "00000000" {
			continue
		}
		addr, valid := parseLEAddr(f[colGateway])
		if !valid {
			continue
		}
		metric, err := strconv.ParseUint(f[colMetric], 10, 32)
		if err != nil {
			continue
		}
		if !ok || metric < best {
			iface, gw, best, ok = f[colIface], addr, metric, true
		}
	}
	if !ok || gw.IsUnspecified() {
		return "", netip.Addr{}, false
	}
	return iface, gw, true
}

// parseLEAddr decodes the little-endian hex IPv4 address used by /proc/net/route.
func parseLEAddr(s string) (netip.Addr, bool) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 4 {
		return netip.Addr{}, false
	}
	return netip.AddrFrom4([4]byte{b[3], b[2], b[1], b[0]}), true
}
