package netdiag

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/netiface"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-331 step 4: the bridge row wired into the producer. The network
// reads are always fakes; nothing here reaches a real engine or interface.

const (
	testNetID    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testNetName  = "app_net"
	testGateway  = "172.18.0.1"
	bridgeTimout = 2 * time.Second
)

// netCtr is a running container on one network, whose endpoint carries id.
func netCtr(mode, id string) docker.ContainerNetworking {
	return docker.ContainerNetworking{
		Name: "web-1", Running: true, Mode: mode,
		Networks: []docker.EndpointNetwork{{
			Name: testNetName, NetworkID: id,
			IPv4:    netip.MustParseAddr("172.18.0.5"),
			Gateway: netip.MustParseAddr(testGateway),
		}},
	}
}

// ifaceWith is an interface carrying one IPv4 CIDR.
func ifaceWith(name, cidr string) netiface.Interface {
	return netiface.Interface{Name: name, State: "UP", IPv4: []string{cidr}}
}

// bridgeFake answers the network read with name and offers ifaces.
func bridgeFake(name string, ifaces ...netiface.Interface) *fakeEngine {
	return &fakeEngine{
		traceFn:    oneHopTrace,
		inspectNet: netCtr("app_net", testNetID),
		networkVal: docker.NetworkBridge{Driver: "bridge", Name: name},
		ifacesVal:  ifaces,
	}
}

// bridgeRun launches a trace of 1.1.1.1 from the web-1 source and feeds every
// message of the producer to the model.
func bridgeRun(t *testing.T, f *fakeEngine) *PathModel {
	t.Helper()
	captureLog(t)
	pm := pmWith(t, f)
	pm.source = webSource
	pm, cmd := launched(t, pm, "1.1.1.1")
	for _, msg := range testutil.Msgs(cmd) {
		pm, _ = pmFeed(t, pm, msg)
	}
	for msg := range pm.msgs {
		pm, _ = pmFeed(t, pm, msg)
	}
	return pm
}

func bridgeRows(pm *PathModel) []string {
	var out []string
	for _, s := range pm.table.Items() {
		if s.Kind == stepBridge {
			out = append(out, s.addressCell())
		}
	}
	return out
}

func requireBridge(t *testing.T, pm *PathModel, want string) {
	t.Helper()
	got := bridgeRows(pm)
	if want == "" {
		if len(got) != 0 {
			t.Fatalf("bridge rows = %v, want none (rows %v)", got, rowKinds(pm))
		}
		return
	}
	if !slices.Equal(got, []string{want}) {
		t.Fatalf("bridge rows = %v, want [%s] (rows %v)", got, want, rowKinds(pm))
	}
}

// Criterion 1: the default network and the host's docker0 give a bridge line,
// and so does a user-defined br-... network.
func TestPathBridgeRowShownWhenTheHostInterfaceCarriesTheGateway(t *testing.T) {
	for _, name := range []string{"docker0", "br-3f2a1b4c5d6e"} {
		t.Run(name, func(t *testing.T) {
			f := bridgeFake(name, ifaceWith("lo", "127.0.0.1/8"), ifaceWith(name, testGateway+"/16"))
			pm := bridgeRun(t, f)

			requireBridge(t, pm, name)
			kinds := rowKinds(pm)
			if want := []string{"container", "network", "bridge", "source"}; !slices.Equal(kinds[:4], want) {
				t.Fatalf("rows = %v, want %v first", kinds, want)
			}
			if f.networkCalls != 1 || f.ifacesCalls != 1 {
				t.Fatalf("network calls %d, interfaces calls %d, want 1 and 1", f.networkCalls, f.ifacesCalls)
			}
		})
	}
}

// Criterion 2: no interface of that name carries the gateway, so no row, and
// the hops still show.
func TestPathBridgeRowAbsentWhenNoInterfaceMatches(t *testing.T) {
	f := bridgeFake("docker0", ifaceWith("eth0", "192.168.1.21/24"), ifaceWith("docker0", "10.9.0.1/16"))
	pm := bridgeRun(t, f)

	requireBridge(t, pm, "")
	if got := pmHopTTLs(pm); !slices.Equal(got, []int{1}) {
		t.Fatalf("hops = %v, want [1]: the trace must go on", got)
	}
	if pm.state != pathDone {
		t.Fatalf("state = %v, want pathDone", pm.state)
	}
}

// Criterion 3: same container, same engine answer; only this machine's
// interface list differs. The engine is never asked anything else for it.
func TestPathBridgeFollowsThisMachinesInterfaces(t *testing.T) {
	with := bridgeFake("docker0", ifaceWith("docker0", testGateway+"/16"))
	without := bridgeFake("docker0", ifaceWith("eth0", "192.168.1.21/24"))

	pmWithIf, pmWithout := bridgeRun(t, with), bridgeRun(t, without)

	requireBridge(t, pmWithIf, "docker0")
	requireBridge(t, pmWithout, "")
	for name, f := range map[string]*fakeEngine{"with": with, "without": without} {
		if f.osCalls != 0 {
			t.Errorf("%s: engineOS called %d times for the bridge, want 0", name, f.osCalls)
		}
	}
}

func TestReadPathBridgeReadsNothingWhenThereIsNoBridgeToFind(t *testing.T) {
	stopped := netCtr("app_net", testNetID)
	stopped.Running = false
	ambiguous := netCtr("app_net", testNetID)
	ambiguous.Networks = append(ambiguous.Networks, docker.EndpointNetwork{
		Name: "b_net", NetworkID: testNetID, Gateway: netip.MustParseAddr("172.19.0.1"),
	})
	tests := []struct {
		name string
		ctr  docker.ContainerNetworking
	}{
		{"stopped", stopped},
		{"host mode", netCtr("host", testNetID)},
		{"none mode", netCtr("none", testNetID)},
		{"shared namespace", netCtr("container:abc", testNetID)},
		{"undetermined (network equality)", ambiguous},
		{"empty network id", netCtr("app_net", "")},
		{"zero value", docker.ContainerNetworking{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := bridgeFake("docker0", ifaceWith("docker0", testGateway+"/16"))
			if got := readPathBridge(context.Background(), tc.ctr, bridgeTimout, f.engine()); got != "" {
				t.Fatalf("bridge = %q, want empty", got)
			}
			if f.networkCalls != 0 || f.ifacesCalls != 0 {
				t.Fatalf("network calls %d, interfaces calls %d, want 0 and 0", f.networkCalls, f.ifacesCalls)
			}
		})
	}
}

// The same cases through the producer, in a model: no call, no row.
func TestPathBridgeNoReadForHostNoneSharedStopped(t *testing.T) {
	for _, mode := range []string{"host", "none", "container:abc"} {
		t.Run(mode, func(t *testing.T) {
			f := bridgeFake("docker0", ifaceWith("docker0", testGateway+"/16"))
			f.inspectNet = netCtr(mode, testNetID)
			pm := bridgeRun(t, f)
			requireBridge(t, pm, "")
			if f.networkCalls != 0 || f.ifacesCalls != 0 {
				t.Fatalf("network calls %d, interfaces calls %d, want 0 and 0", f.networkCalls, f.ifacesCalls)
			}
		})
	}
	t.Run("stopped", func(t *testing.T) {
		f := bridgeFake("docker0", ifaceWith("docker0", testGateway+"/16"))
		f.inspectNet.Running = false
		pm := bridgeRun(t, f)
		requireBridge(t, pm, "")
		if f.networkCalls != 0 || f.ifacesCalls != 0 {
			t.Fatalf("network calls %d, interfaces calls %d, want 0 and 0", f.networkCalls, f.ifacesCalls)
		}
	})
}

func TestPathBridgeNotReadFromThisMachine(t *testing.T) {
	f := bridgeFake("docker0", ifaceWith("docker0", testGateway+"/16"))
	captureLog(t)
	pm := pmWith(t, f)
	pm, cmd := launched(t, pm, "1.1.1.1")
	for _, msg := range testutil.Msgs(cmd) {
		pm, _ = pmFeed(t, pm, msg)
	}
	requireBridge(t, pm, "")
	if f.networkCalls != 0 || f.ifacesCalls != 0 || f.inspectCalls != 0 {
		t.Fatalf("network %d, interfaces %d, inspect %d calls from this machine, want none", f.networkCalls, f.ifacesCalls, f.inspectCalls)
	}
}

// A macvlan or any other driver without a bridge of its own answers Name "":
// no row, and the interfaces are not even listed.
func TestPathBridgeEmptyNameShowsNoRow(t *testing.T) {
	f := bridgeFake("", ifaceWith("docker0", testGateway+"/16"))
	f.networkVal.Driver = "macvlan"
	pm := bridgeRun(t, f)

	requireBridge(t, pm, "")
	if f.networkCalls != 1 {
		t.Fatalf("network calls = %d, want 1", f.networkCalls)
	}
	if f.ifacesCalls != 0 {
		t.Fatalf("interfaces listed %d times for an empty name, want 0", f.ifacesCalls)
	}
}

// A failed read shows nothing, is logged raw with the package prefix, and the
// trace runs to its end.
func TestPathBridgeReadFailureIsLoggedNotShown(t *testing.T) {
	tests := []struct {
		name string
		fake func() *fakeEngine
		raw  string
	}{
		{"network read fails", func() *fakeEngine {
			f := bridgeFake("docker0", ifaceWith("docker0", testGateway+"/16"))
			f.networkErr = errors.New("net-boom-raw")
			return f
		}, "net-boom-raw"},
		{"interface list fails", func() *fakeEngine {
			f := bridgeFake("docker0")
			f.ifacesErr = errors.New("if-boom-raw")
			return f
		}, "if-boom-raw"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.fake()
			logs := captureLog(t)
			pm := pmWith(t, f)
			pm.source = webSource
			pm, cmd := launched(t, pm, "1.1.1.1")
			for _, msg := range testutil.Msgs(cmd) {
				pm, _ = pmFeed(t, pm, msg)
			}
			for msg := range pm.msgs {
				pm, _ = pmFeed(t, pm, msg)
			}

			requireBridge(t, pm, "")
			if f.traceCalls != 1 || pm.state != pathDone || !slices.Equal(pmHopTTLs(pm), []int{1}) {
				t.Fatalf("traceCalls=%d state=%v hops=%v, want the trace carried to its end", f.traceCalls, pm.state, pmHopTTLs(pm))
			}
			if !strings.Contains(logs.String(), "ERROR [netdiag/path] ") || !strings.Contains(logs.String(), tc.raw) {
				t.Fatalf("log %q lacks an ERROR [netdiag/path] line with %q", logs.String(), tc.raw)
			}
			if strings.Contains(pm.footer.Text(), tc.raw) || strings.Contains(pm.statusLine().Text, tc.raw) || pm.errText != "" {
				t.Fatalf("the raw error reached the screen: footer %q status %q err %q", pm.footer.Text(), pm.statusLine().Text, pm.errText)
			}
		})
	}
}

// esc during the read is not a failure: nothing is logged.
func TestPathBridgeCanceledReadIsNotLogged(t *testing.T) {
	t.Run("readPathBridge", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f := bridgeFake("docker0", ifaceWith("docker0", testGateway+"/16"))
		e := f.engine()
		e.network = func(ctx context.Context, _ string) (docker.NetworkBridge, error) {
			cancel()
			return docker.NetworkBridge{}, fmt.Errorf("canceled sentinel: %w", ctx.Err())
		}
		logs := captureLog(t)
		if got := readPathBridge(ctx, netCtr("app_net", testNetID), bridgeTimout, e); got != "" {
			t.Fatalf("bridge = %q, want empty", got)
		}
		if strings.Contains(logs.String(), "sentinel") {
			t.Fatalf("a canceled read was logged: %q", logs.String())
		}
	})
	t.Run("producer", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f := bridgeFake("docker0", ifaceWith("docker0", testGateway+"/16"))
		e := f.engine()
		e.interfaces = func(ctx context.Context) ([]netiface.Interface, error) {
			cancel() // esc during the read
			return nil, fmt.Errorf("canceled sentinel: %w", ctx.Err())
		}
		logs := captureLog(t)
		msgs := runProducerFrom(t, ctx, 1, pathTarget{Host: "10.0.0.8"}, webSource, time.Second, e)

		if strings.Contains(logs.String(), "sentinel") {
			t.Fatalf("a canceled read was logged: %q", logs.String())
		}
		if _, ok := msgs[len(msgs)-1].(pathDoneMsg); !ok {
			t.Fatalf("last = %#v, want a done message", msgs[len(msgs)-1])
		}
	})
}

// The network is read by its ID, never by its name (a name could be taken for
// an option by the CLI), under a deadline.
func TestPathBridgeReadsByIDUnderATimeout(t *testing.T) {
	f := bridgeFake("docker0", ifaceWith("docker0", testGateway+"/16"))
	got := readPathBridge(context.Background(), netCtr("app_net", testNetID), bridgeTimout, f.engine())

	if got != "docker0" {
		t.Fatalf("bridge = %q, want docker0", got)
	}
	if !slices.Equal(f.networkIDs, []string{testNetID}) {
		t.Fatalf("network ids = %v, want [%s]", f.networkIDs, testNetID)
	}
	if slices.Contains(f.networkIDs, testNetName) {
		t.Fatal("the network was read by its name")
	}
	dl, ok := f.networkCtx.Deadline()
	if !ok {
		t.Fatal("network context carries no deadline")
	}
	if left := time.Until(dl); left > bridgeTimout || left <= 0 {
		t.Fatalf("network deadline in %v, want within (0, %v]", left, bridgeTimout)
	}
}

// The gateway used is the one of the default-route endpoint, not of another.
func TestReadPathBridgeUsesTheDefaultRouteEndpoint(t *testing.T) {
	n := docker.ContainerNetworking{Name: "web-1", Running: true, Mode: "a_net", Networks: []docker.EndpointNetwork{
		{Name: "a_net", NetworkID: "id-a", Gateway: netip.MustParseAddr("172.18.0.1"), GwPriority: prioOf(0)},
		{Name: "b_net", NetworkID: "id-b", Gateway: netip.MustParseAddr("172.19.0.1"), GwPriority: prioOf(10)},
	}}
	f := bridgeFake("br-b", ifaceWith("br-a", "172.18.0.1/16"), ifaceWith("br-b", "172.19.0.1/16"))
	got := readPathBridge(context.Background(), n, bridgeTimout, f.engine())

	if got != "br-b" || !slices.Equal(f.networkIDs, []string{"id-b"}) {
		t.Fatalf("bridge = %q, ids = %v, want br-b read by id-b", got, f.networkIDs)
	}
}

func TestRouteEndpoint(t *testing.T) {
	one := netCtr("app_net", testNetID)
	tests := []struct {
		name   string
		in     docker.ContainerNetworking
		wantOK bool
		want   string
	}{
		{"attached to one network", one, true, testNetName},
		{"host", netCtr("host", testNetID), false, ""},
		{"none", netCtr("none", testNetID), false, ""},
		{"shared", netCtr("container:abc", testNetID), false, ""},
		{"no gateway", docker.ContainerNetworking{Mode: "app_net", Networks: []docker.EndpointNetwork{{Name: "x"}}}, false, ""},
		{"zero value", docker.ContainerNetworking{}, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := routeEndpoint(tc.in)
			if ok != tc.wantOK || got.Name != tc.want {
				t.Fatalf("routeEndpoint = (%q, %v), want (%q, %v)", got.Name, ok, tc.want, tc.wantOK)
			}
		})
	}
	// newPathContainer agrees with it.
	if ep, ok := routeEndpoint(one); !ok {
		t.Fatal("precondition")
	} else if c := newPathContainer(webSource, one, nil); c.Attach != attachNetwork || c.Gateway != ep.Gateway {
		t.Fatalf("newPathContainer %+v disagrees with routeEndpoint %+v", c, ep)
	}
}

// The name is display-safe like every other engine text: a control or escape
// sequence in an interface name never reaches a cell.
func TestPathBridgeNameIsDisplaySafe(t *testing.T) {
	const raw = "dock\x1b[31mer0\n\x07"
	f := bridgeFake(raw, ifaceWith(raw, testGateway+"/16"))

	if got := readPathBridge(context.Background(), netCtr("app_net", testNetID), bridgeTimout, f.engine()); got != "docker0" {
		t.Fatalf("bridge = %q, want %q", got, "docker0")
	}

	pm := bridgeRun(t, bridgeFake(raw, ifaceWith(raw, testGateway+"/16")))
	requireBridge(t, pm, "docker0")
	for _, s := range pm.table.Items() {
		if strings.ContainsAny(s.addressCell(), "\x1b\x07\n") {
			t.Fatalf("row %q carries a control character", s.addressCell())
		}
	}
}

// The resolved message carries the bridge and the model puts it in the column.
func TestPathResolvedCarriesTheBridge(t *testing.T) {
	f := bridgeFake("docker0", ifaceWith("docker0", testGateway+"/16"))
	msgs := runProducerFrom(t, context.Background(), 1, pathTarget{Host: "1.1.1.1"}, webSource, time.Second, f.engine())

	res, ok := msgs[0].(pathResolvedMsg)
	if !ok || res.bridge != "docker0" {
		t.Fatalf("msgs[0] = %#v, want a resolved message with bridge docker0", msgs[0])
	}

	pm := pmWith(t, f)
	pm.source = webSource
	pm, _ = launched(t, pm, "1.1.1.1")
	pm, _ = pmFeed(t, pm,
		pathResolvedMsg{gen: pm.gen, addr: netip.MustParseAddr("1.1.1.1"), route: defaultRouteHop(), source: webSource, ctr: f.inspectNet, bridge: "docker0"},
		pathHopMsg{gen: pm.gen, hop: hopAt(1)},
	)
	requireBridge(t, pm, "docker0")
}
