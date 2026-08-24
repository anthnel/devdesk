package ports

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"
)

// resolveLookupAddr is the seam to reverse DNS, so a test never asks the
// network a question.
var resolveLookupAddr = func(ctx context.Context, ip string) ([]string, error) {
	var r net.Resolver
	return r.LookupAddr(ctx, ip)
}

// reverseCache holds what has already been asked, for the life of the process.
//
// It is package-level rather than a field on the model because the lookups run
// inside a Cmd, and a Cmd may not touch the model (Rule 110). A reverse name
// does not change on the scale of a session, and the table re-reads every two
// seconds: without the cache, turning names on would put one DNS query per
// socket on the wire every tick.
var (
	reverseMu    sync.Mutex
	reverseCache = map[string]string{}
)

// reverseWorkers bounds how many lookups are in flight at once. A machine with
// a hundred established connections would otherwise open a hundred queries in
// one go, which is a burst the tool has no business producing.
const reverseWorkers = 8

// reverseTimeout is what one lookup gets. The whole pass is bounded by the
// caller's context as well; this is what stops one unanswerable address from
// spending it all.
const reverseTimeout = 2 * time.Second

// resolveAddrs rewrites the addresses in place, replacing the IP with a host
// name where reverse DNS answers.
//
// **Host names only.** `ss` without -n also resolved the port to a service name
// — 22 shown as "ssh" — and that half is not reproduced: Go resolves a name to
// a port and not the other way round, so honouring it would mean shipping a
// copy of /etc/services and calling the result the machine's opinion. Saying
// less is better than saying something the system did not.
//
// An address that does not resolve is cached as itself, so a machine talking to
// hosts with no PTR record does not re-ask for them on every refresh.
func resolveAddrs(ctx context.Context, sockets []Socket) {
	wanted := map[string]bool{}
	for _, s := range sockets {
		collectHost(wanted, s.LocalAddr)
		collectHost(wanted, s.PeerAddr)
	}

	pending := make([]string, 0, len(wanted))
	reverseMu.Lock()
	for ip := range wanted {
		if _, ok := reverseCache[ip]; !ok {
			pending = append(pending, ip)
		}
	}
	reverseMu.Unlock()

	lookupAll(ctx, pending)

	reverseMu.Lock()
	defer reverseMu.Unlock()
	for i := range sockets {
		sockets[i].LocalAddr = substitute(sockets[i].LocalAddr, reverseCache)
		sockets[i].PeerAddr = substitute(sockets[i].PeerAddr, reverseCache)
	}
}

// lookupAll resolves what is not cached yet, writing every outcome — including
// the failures — so nothing is asked twice.
func lookupAll(ctx context.Context, ips []string) {
	if len(ips) == 0 {
		return
	}
	queue := make(chan string)
	var wg sync.WaitGroup
	for range min(reverseWorkers, len(ips)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range queue {
				name := lookupOne(ctx, ip)
				reverseMu.Lock()
				reverseCache[ip] = name
				reverseMu.Unlock()
			}
		}()
	}
	for _, ip := range ips {
		select {
		case queue <- ip:
		case <-ctx.Done():
			close(queue)
			wg.Wait()
			return
		}
	}
	close(queue)
	wg.Wait()
}

// lookupOne returns the host name, or the address itself when there is none.
func lookupOne(ctx context.Context, ip string) string {
	ctx, cancel := context.WithTimeout(ctx, reverseTimeout)
	defer cancel()
	names, err := resolveLookupAddr(ctx, ip)
	if err != nil || len(names) == 0 || names[0] == "" {
		return ip
	}
	return strings.TrimSuffix(names[0], ".")
}

// collectHost records the host half of an address when it is worth asking about.
//
// The wildcards are skipped because they name no host, and the loopback and
// unspecified addresses because their answer is either already known or
// meaningless — and asking would put a query on the wire for a socket that
// never leaves the machine.
func collectHost(into map[string]bool, addr string) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
		return
	}
	into[host] = true
}

// substitute swaps the host half of an address for its resolved name, leaving
// the port alone.
func substitute(addr string, cache map[string]string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	name, ok := cache[host]
	if !ok || name == "" || name == host {
		return addr
	}
	return net.JoinHostPort(name, port)
}
