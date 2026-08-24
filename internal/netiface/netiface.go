// Package netiface reads this machine's network interfaces, in this process.
//
// It exists for the same reason internal/ports does. The Topology tab used to
// run `ip addr`, `ip -s link`, `ip route`, `ip neigh` and `iptables` in
// ephemeral containers started with --network host, and on Docker Desktop that
// is the Linux VM's network namespace, not the machine's (D57). The tab showed
// eth0 10.254.254.3 and docker0 while the machine had Ethernet 2, a VPN and a
// Tailscale interface — no overlap of any kind. Under Linux the defect did not
// exist, which is why it went unnoticed.
//
// What is read here is what reads natively on all three platforms: the
// interfaces from the standard library, the error counters from gopsutil. The
// routing table, the ARP cache and the firewall were not translated, they were
// removed — §3.44 records why for each, and the route question moved to the
// Diagnostics pipeline, where it is asked about a target instead of in the
// abstract.
package netiface

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"

	gnet "github.com/shirou/gopsutil/v4/net"
)

// Interface is one network interface of this machine.
type Interface struct {
	Name      string
	State     string // UP, DOWN, or LOOP
	MTU       int
	MAC       string
	Addresses []string // CIDR notation

	// RxErrors and TxErrors are nil when the counters could not be read.
	//
	// They are pointers for the reason Result.SecretVerdict is: the counters
	// come from a second source that fails on its own, and a zero written
	// because nobody looked is indistinguishable from an interface with no
	// errors — which is the whole of D58, on a column instead of a section.
	RxErrors *uint64
	TxErrors *uint64
}

// Interface states, as the view renders them.
const (
	StateUp   = "UP"
	StateDown = "DOWN"
	StateLoop = "LOOP"
)

// List returns this machine's interfaces.
//
// The error is returned only when the interfaces themselves could not be read.
// Counters that fail leave RxErrors and TxErrors nil and cost nothing else: a
// list of interfaces without their error counts is still the answer to "which
// adapters does this machine have", and refusing to show it would be the
// failure standing in for the absence one more time.
func List(ctx context.Context) ([]Interface, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("reading the network interfaces: %w", err)
	}

	counters := errorCounters(ctx)

	out := make([]Interface, 0, len(ifaces))
	for _, i := range ifaces {
		item := Interface{
			Name:      i.Name,
			State:     stateOf(i.Flags),
			MTU:       i.MTU,
			MAC:       i.HardwareAddr.String(),
			Addresses: addressesOf(i),
		}
		if c, ok := counters[i.Name]; ok {
			rx, tx := c.Errin, c.Errout
			item.RxErrors, item.TxErrors = &rx, &tx
		}
		out = append(out, item)
	}

	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out, nil
}

// errorCounters indexes the per-interface counters by name.
//
// The names match the ones net.Interfaces reports character for character —
// measured on this machine, 10 interfaces and 10 counter rows with no
// exception — so there is no correspondence table to keep, and an interface
// gopsutil does not know about simply keeps its nil counters.
func errorCounters(ctx context.Context) map[string]gnet.IOCountersStat {
	stats, err := gnet.IOCountersWithContext(ctx, true)
	if err != nil {
		return nil
	}
	out := make(map[string]gnet.IOCountersStat, len(stats))
	for _, s := range stats {
		out[s.Name] = s
	}
	return out
}

// stateOf reduces the flags to the one word the view shows.
//
// Loopback is its own state rather than an UP interface, because "is this the
// machine talking to itself" is the first thing to know about a row and the
// flags say it plainly.
func stateOf(flags net.Flags) string {
	switch {
	case flags&net.FlagLoopback != 0:
		return StateLoop
	case flags&net.FlagUp != 0:
		return StateUp
	default:
		return StateDown
	}
}

// addressesOf renders an interface's addresses in CIDR notation.
func addressesOf(i net.Interface) []string {
	addrs, err := i.Addrs()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out
}

// HasMTU reports whether the MTU is a figure worth showing.
//
// Windows returns -1 for the loopback pseudo-interface where `ip` returns
// 65536. Neither is wrong, but a table cell reading "-1" is, so the view prints
// nothing rather than a number the machine does not mean.
func (i Interface) HasMTU() bool { return i.MTU > 0 }

// AddressList joins the addresses for a single-line cell.
func (i Interface) AddressList() string { return strings.Join(i.Addresses, "  ") }
