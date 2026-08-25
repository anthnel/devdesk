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
	Name  string
	State string // UP, DOWN, or LOOP
	MTU   int
	MAC   string

	// IPv4 and IPv6 are the interface's addresses, in CIDR notation, split by
	// family rather than joined into one list.
	//
	// The split happens **here**, where each address is still a net.IP and the
	// family is a fact rather than something to read back out of a string. A
	// view splitting `AddressList()` again would be parsing text this package
	// produced, and would have to decide what an unparseable entry means —
	// a question that only exists once the type has been thrown away.
	IPv4 []string
	IPv6 []string

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
			Name:  i.Name,
			State: stateOf(i.Flags),
			MTU:   i.MTU,
			MAC:   i.HardwareAddr.String(),
		}
		item.IPv4, item.IPv6 = addressesOf(i)
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

// addressesOf renders an interface's addresses in CIDR notation, one slice per
// family.
//
// The family is read off the net.IP, never off the rendered string: To4()
// answers for an IPv4-mapped address (::ffff:192.0.2.1) as well as for a plain
// one, which is right — it is an IPv4 address, whatever notation it arrived in.
//
// An address of a kind neither branch recognises — a Unix socket address on an
// interface, which does not happen but is expressible — is dropped rather than
// filed under a family it does not belong to. Guessing would put a wrong answer
// in a cell, where an absent one is at least visibly absent.
func addressesOf(i net.Interface) (v4, v6 []string) {
	addrs, err := i.Addrs()
	if err != nil {
		return nil, nil
	}
	for _, a := range addrs {
		ip := ipOf(a)
		switch {
		case ip == nil:
			continue
		case ip.To4() != nil:
			v4 = append(v4, a.String())
		default:
			v6 = append(v6, a.String())
		}
	}
	return v4, v6
}

// ipOf pulls the address out of whichever net.Addr the platform returned.
//
// net.Interface.Addrs documents *net.IPNet, and every platform in the standard
// library returns that; *net.IPAddr is accepted because the interface permits
// it and a type switch that refused it would drop the address in silence.
func ipOf(a net.Addr) net.IP {
	switch v := a.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	}
	return nil
}

// HasMTU reports whether the MTU is a figure worth showing.
//
// Windows returns -1 for the loopback pseudo-interface where `ip` returns
// 65536. Neither is wrong, but a table cell reading "-1" is, so the view prints
// nothing rather than a number the machine does not mean.
func (i Interface) HasMTU() bool { return i.MTU > 0 }

// IPv4List and IPv6List join one family's addresses for a single-line cell.
func (i Interface) IPv4List() string { return strings.Join(i.IPv4, "  ") }
func (i Interface) IPv6List() string { return strings.Join(i.IPv6, "  ") }
