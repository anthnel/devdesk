package netdiag

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/netip"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/netiface"
	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/wslenv"
)

// pathChanCap holds a whole trace: every hop (at most MaxHopsLimit), the
// resolved message and the done message. The producer therefore never waits
// for a reader. Contract: resolved now carries source and ctr but is still one
// message, so 1 resolved + MaxHopsLimit (30) hops + 1 done = 32 = pathChanCap,
// and sendDone never evicts resolved with the real engine.
const pathChanCap = nettrace.MaxHopsLimit + 2

// pathEngine is the seam between the path tab and the network: tests replace
// it with a fake.
type pathEngine struct {
	supported func() error
	resolve   func(ctx context.Context, host string) ([]netip.Addr, error)
	route     func(ctx context.Context, a netip.Addr) (netcheck.RouteHop, error)
	trace     func(ctx context.Context, a netip.Addr, o nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error)

	engineName func() string                                                            // engine.Current().Name
	engineOS   func(ctx context.Context) (string, error)                                // docker.EngineOperatingSystem
	running    func(ctx context.Context) ([]docker.Container, error)                    // running containers only
	inspect    func(ctx context.Context, id string) (docker.ContainerNetworking, error) // read-only
	network    func(ctx context.Context, id string) (docker.NetworkBridge, error)       // docker.InspectNetworkBridge
	interfaces func(ctx context.Context) ([]netiface.Interface, error)                  // netiface.List
	platform   func(ctx context.Context) wslenv.Info                                    // wslenv.Detect; nil means not WSL2
}

// systemPathEngine returns the engine backed by the real resolver and sockets.
func systemPathEngine() pathEngine {
	env := netcheck.SystemEnv(netcheck.DefaultSettings())
	return pathEngine{
		supported: systemPathSupport,
		resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
		},
		route: func(ctx context.Context, a netip.Addr) (netcheck.RouteHop, error) {
			return env.Route(ctx, net.IP(a.AsSlice()))
		},
		trace:      nettrace.TraceEach,
		engineName: func() string { return engine.Current().Name },
		engineOS:   docker.EngineOperatingSystem,
		running: func(ctx context.Context) ([]docker.Container, error) {
			return docker.ListContainersContext(ctx, false)
		},
		inspect:    docker.InspectContainerNetworking,
		network:    docker.InspectNetworkBridge,
		interfaces: netiface.List,
		platform:   wslenv.Detect,
	}
}

// pathResolvedMsg reports the address the target resolved to.
type pathResolvedMsg struct {
	gen      int
	addr     netip.Addr
	route    netcheck.RouteHop
	routeErr error
	source   pathSource                 // the source at launch
	ctr      docker.ContainerNetworking // zero for this machine
	ctrErr   error                      // logged by the producer, never shown
	bridge   pathBridge                 // what is known of the container network's host interface
	platform wslenv.Info                // WSL2 detection for this launch; zero when not WSL2
}

// pathSourcesMsg carries what the selector offers once the engine answered.
type pathSourcesMsg struct {
	gen  int // the request it answers, so a stale answer can be dropped
	list pathSourceList
}

// loadPathSourcesCmd reads the selector's list off the Update goroutine, under
// its own timeout so a hung engine cannot keep the selector loading. It
// touches no model (Rule 110): the answer comes back as a message.
func loadPathSourcesCmd(gen int, timeout time.Duration, e pathEngine) tea.Cmd {
	return func() tea.Msg {
		return pathSourcesMsg{gen: gen, list: listPathSourcesUnder(context.Background(), timeout, e)}
	}
}

// listPathSourcesUnder reads the list under a timeout derived from parent.
// Only parent decides whether an error is logged: a deadline exceeded on its
// own is a real failure, a canceled parent is not.
func listPathSourcesUnder(parent context.Context, timeout time.Duration, e pathEngine) pathSourceList {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return readPathSources(parent, ctx, e)
}

// listPathSources reads what the selector offers with a single context.
func listPathSources(ctx context.Context, e pathEngine) pathSourceList {
	return readPathSources(ctx, ctx, e)
}

// readPathSources reads what the selector offers. Only read-only engine calls
// are made (info, ps): nothing starts or enters a container. A raw error goes
// to the log, never to the note, unless parent was canceled.
func readPathSources(parent, ctx context.Context, e pathEngine) pathSourceList {
	name := ""
	if e.engineName != nil {
		name = e.engineName()
	}
	if name != "docker" { // podman: no exec at all
		return sourceListFrom(name, "", nil, nil, nil)
	}
	osName, osErr := e.engineOS(ctx)
	if osErr != nil {
		logSourcesError(parent, osErr)
		return sourceListFrom(name, "", osErr, nil, nil)
	}
	if isDockerDesktop(osName) {
		return sourceListFrom(name, osName, nil, nil, nil)
	}
	cs, psErr := e.running(ctx)
	if psErr != nil {
		logSourcesError(parent, psErr)
	}
	return sourceListFrom(name, osName, nil, cs, psErr)
}

// logSourcesError logs a read failure unless the caller gave up (parent is
// the context received, not the one bounded by the timeout).
func logSourcesError(parent context.Context, err error) {
	if parent.Err() == nil {
		log.Printf("ERROR [netdiag/path] sources: %v", err)
	}
}

// pathHopMsg carries one hop as the engine reports it.
type pathHopMsg struct {
	gen int
	hop nettrace.Hop
}

// pathDoneMsg ends a run: a result, or an error when no trace happened.
type pathDoneMsg struct {
	gen    int
	result nettrace.Result
	err    error
}

// producePath resolves the target, traces it and sends the messages to out,
// then closes out. It runs inside a Cmd and touches no model (Rule 110).
// out must have room for pathChanCap messages: sends never block, so a view
// that stops reading after canceling leaks nothing.
func producePath(ctx context.Context, gen int, tg pathTarget, src pathSource, resolveTimeout time.Duration, e pathEngine, out chan tea.Msg) {
	defer close(out)
	send := func(m tea.Msg) {
		select {
		case out <- m:
		default:
		}
	}
	// sendDone must never be lost: when the channel is full the oldest message
	// is dropped to make room. The producer is the only sender, so one drop
	// frees a slot for good.
	sendDone := func(m tea.Msg) {
		for range 2 {
			select {
			case out <- m:
				return
			default:
			}
			select {
			case <-out:
			default:
			}
		}
	}

	addr, err := resolvePathTarget(ctx, tg.Host, resolveTimeout, e)
	if err != nil {
		if ctx.Err() == nil { // esc during the resolution is not an error
			log.Printf("ERROR [netdiag/path] resolve %s: %v", tg.Host, err)
		}
		sendDone(pathDoneMsg{gen: gen, err: err})
		return
	}
	// The route is asked for the address actually traced. A failure is not
	// fatal and is never shown: the trace goes on without the route column.
	route, routeErr := e.route(ctx, addr)
	if routeErr != nil && ctx.Err() == nil {
		log.Printf("ERROR [netdiag/path] route %s: %v", tg.Host, routeErr)
	}
	ctr, ctrErr := inspectPathSource(ctx, src, resolveTimeout, e)
	var bridge pathBridge
	if ctrErr == nil && !src.isHost() {
		bridge = readPathBridge(ctx, ctr, resolveTimeout, e)
	}
	platform := detectPathPlatform(ctx, resolveTimeout, e)
	send(pathResolvedMsg{gen: gen, addr: addr, route: route, routeErr: routeErr, source: src, ctr: ctr, ctrErr: ctrErr, bridge: bridge, platform: platform})

	res, err := e.trace(ctx, addr, nettrace.Options{}, func(h nettrace.Hop) {
		send(pathHopMsg{gen: gen, hop: h})
	})
	if err != nil {
		log.Printf("ERROR [netdiag/path] trace %s: %v", tg.Host, err)
	}
	sendDone(pathDoneMsg{gen: gen, result: res, err: err})
}

// inspectPathSource reads the chosen container's network under its own
// timeout. This machine reads nothing. A failure is not fatal and is never
// shown: the raw error goes to the log unless the run was canceled.
func inspectPathSource(ctx context.Context, src pathSource, timeout time.Duration, e pathEngine) (docker.ContainerNetworking, error) {
	if src.isHost() {
		return docker.ContainerNetworking{}, nil
	}
	ictx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctr, err := e.inspect(ictx, src.ID)
	if err != nil && ctx.Err() == nil {
		log.Printf("ERROR [netdiag/path] inspect %s: %v", src.ID, err)
	}
	return ctr, err
}

// detectPathPlatform asks the engine once whether this machine is WSL2, under
// its own timeout. No engine function means not WSL2.
func detectPathPlatform(ctx context.Context, timeout time.Duration, e pathEngine) wslenv.Info {
	if e.platform == nil {
		return wslenv.Info{}
	}
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return e.platform(pctx)
}

// resolvePathTarget returns the IPv4 address to trace: a literal is used as
// is, a name is resolved under its own timeout.
func resolvePathTarget(ctx context.Context, host string, timeout time.Duration, e pathEngine) (netip.Addr, error) {
	if a, err := netip.ParseAddr(host); err == nil && a.Is4() {
		return a, nil
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addrs, err := e.resolve(rctx, host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%w: %w", errPathResolve, err)
	}
	if len(addrs) == 0 {
		return netip.Addr{}, errPathNoIPv4
	}
	return addrs[0], nil
}

// waitForPathMsg returns a Cmd that yields the next message of in, or nil
// once in is closed. The view re-arms it after every message.
func waitForPathMsg(in <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		m, ok := <-in
		if !ok {
			return nil
		}
		return m
	}
}
