package netdiag

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/wslenv"
)

// DEVDESK-333 step 4: the WSL2 detection wired into the Path flow. The platform
// seam is always a fake; nothing here runs wslinfo.

// wslNATFake returns a fake whose route leaves by the NAT interface through the
// NAT gateway and whose first hop is that gateway, so the labels apply.
func wslNATFake(info wslenv.Info) *fakeEngine {
	return &fakeEngine{
		platformFn: func(context.Context) wslenv.Info { return info },
		routeFn: func(context.Context, netip.Addr) (netcheck.RouteHop, error) {
			return netcheck.RouteHop{Interface: "eth0", Source: net.ParseIP("172.27.130.5"), Gateway: net.ParseIP("172.27.128.1")}, nil
		},
		traceFn: func(_ context.Context, a netip.Addr, _ nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error) {
			onHop(wslHop1)
			return nettrace.Result{Target: a, Outcome: nettrace.OutcomeReached, Hops: []nettrace.Hop{wslHop1}}, nil
		},
	}
}

func resolvedMsgOf(t *testing.T, msgs []tea.Msg) pathResolvedMsg {
	t.Helper()
	for _, m := range msgs {
		if r, ok := m.(pathResolvedMsg); ok {
			return r
		}
	}
	t.Fatal("no pathResolvedMsg among the producer's messages")
	return pathResolvedMsg{}
}

func TestProducePathCarriesThePlatformOnTheResolvedMessage(t *testing.T) {
	f := wslNATFake(wslInfoNAT)
	msgs := runProducer(t, context.Background(), 3, pathTarget{Host: "1.1.1.1"}, time.Second, f.engine())
	if got := resolvedMsgOf(t, msgs).platform; got != wslInfoNAT {
		t.Errorf("resolved.platform = %+v, want %+v", got, wslInfoNAT)
	}
	if len(msgs) != 3 { // resolved, one hop, done: no extra message
		t.Errorf("%d messages, want 3 (resolved, hop, done)", len(msgs))
	}
}

func TestProducePathAsksThePlatformOncePerLaunchUnderTheTimeout(t *testing.T) {
	f := wslNATFake(wslInfoNAT)
	f.traceFn = func(_ context.Context, a netip.Addr, _ nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error) {
		for i := 1; i <= 3; i++ {
			onHop(hopAt(i))
		}
		return nettrace.Result{Target: a}, nil
	}
	runProducer(t, context.Background(), 1, pathTarget{Host: "1.1.1.1"}, time.Second, f.engine())
	if f.platformCalls != 1 {
		t.Fatalf("platform called %d times, want 1", f.platformCalls)
	}
	if _, ok := f.platformCtx.Deadline(); !ok {
		t.Error("platform ran without a deadline: it must run under resolveTimeout")
	}
}

func TestProducePathWithoutPlatformSeamSendsAnEmptyInfo(t *testing.T) {
	f := &fakeEngine{traceFn: oneHopTrace}
	msgs := runProducer(t, context.Background(), 1, pathTarget{Host: "1.1.1.1"}, time.Second, f.engine())
	if got := resolvedMsgOf(t, msgs).platform; got != (wslenv.Info{}) {
		t.Errorf("resolved.platform = %+v, want the zero Info", got)
	}
}

func TestSystemPathEngineDetectsThePlatform(t *testing.T) {
	if systemPathEngine().platform == nil {
		t.Error("systemPathEngine().platform is nil, want wslenv.Detect")
	}
}

func TestPathModelLabelsTheWindowsHostFromTheResolvedPlatform(t *testing.T) {
	pm := pmWith(t, wslNATFake(wslInfoNAT))
	pm, cmd := launched(t, pm, "1.1.1.1")
	pm = feedAll(t, pm, cmd)

	if pm.platform != wslInfoNAT {
		t.Errorf("pm.platform = %+v, want %+v", pm.platform, wslInfoNAT)
	}
	items := pm.table.Items()
	if got := wslStep(items, stepSource).addressCell(); got != wslSourceNAT {
		t.Errorf("source cell = %q, want %q", got, wslSourceNAT)
	}
	if !strings.Contains(strings.Join(wslCells(items), "|"), wslHostCell) {
		t.Errorf("no cell reads %q: %v", wslHostCell, wslCells(items))
	}
	if !strings.Contains(ansi.Strip(pm.view()), "Windows host (virtual switch)") {
		t.Error("the rendered column does not name the Windows host")
	}
}

func TestPathModelWithoutPlatformRendersAsBefore(t *testing.T) {
	pm := pmWith(t, &fakeEngine{traceFn: oneHopTrace})
	pm, cmd := launched(t, pm, "1.1.1.1")
	pm = feedAll(t, pm, cmd)
	if pm.platform != (wslenv.Info{}) {
		t.Errorf("pm.platform = %+v, want zero", pm.platform)
	}
	if got := wslStep(pm.table.Items(), stepSource).addressCell(); got != sourceLabel {
		t.Errorf("source cell = %q, want %q", got, sourceLabel)
	}
	if wslLabelled(pm.table.Items()) {
		t.Errorf("a row is labelled with no platform: %v", wslCells(pm.table.Items()))
	}
}

func TestStaleRunDoesNotApplyItsPlatform(t *testing.T) {
	pm := pmWith(t, &fakeEngine{traceFn: oneHopTrace})
	pm, _ = launched(t, pm, "1.1.1.1")
	stale := pathResolvedMsg{gen: pm.gen - 1, addr: netip.MustParseAddr("1.1.1.1"), platform: wslInfoNAT}
	pm, _ = pmFeed(t, pm, stale)
	if pm.platform != (wslenv.Info{}) {
		t.Errorf("a stale resolved message set pm.platform = %+v", pm.platform)
	}
}

func TestRelaunchClearsThePreviousPlatform(t *testing.T) {
	pm := pmWith(t, wslNATFake(wslInfoNAT))
	pm, cmd := launched(t, pm, "1.1.1.1")
	pm = feedAll(t, pm, cmd)
	if pm.platform != wslInfoNAT || pm.state != pathDone {
		t.Fatalf("setup: platform %+v, state %v", pm.platform, pm.state)
	}
	pm, _ = pmFeed(t, pm, testutil.Key("enter")) // trace again: nothing resolved yet
	if pm.platform != (wslenv.Info{}) {
		t.Errorf("pm.platform = %+v right after a relaunch, want zero", pm.platform)
	}
}

func TestFailedRunResetsThePlatform(t *testing.T) {
	pm := pmWith(t, wslNATFake(wslInfoNAT))
	pm, _ = launched(t, pm, "1.1.1.1")
	pm, _ = pmFeed(t, pm, pathResolvedMsg{gen: pm.gen, addr: netip.MustParseAddr("1.1.1.1"), platform: wslInfoNAT})
	if pm.platform != wslInfoNAT {
		t.Fatalf("setup: pm.platform = %+v", pm.platform)
	}
	pm, _ = pmFeed(t, pm, pathDoneMsg{gen: pm.gen, err: errPathResolve})
	if pm.platform != (wslenv.Info{}) {
		t.Errorf("pm.platform = %+v after a failed run, want zero", pm.platform)
	}
}

// Rule 114: the help says that under WSL2 the source and the first hop are labelled.
func TestPathHelpMentionsTheWSL2Labels(t *testing.T) {
	var body string
	for _, s := range New(testConfig()).GetHelpContent().Sections {
		if s.Title == "Diagnostics Tab — Tracing the path" {
			body = strings.Join(strings.Fields(s.Body), " ")
		}
	}
	if body == "" {
		t.Fatal(`no section "Diagnostics Tab — Tracing the path"`)
	}
	for _, want := range []string{"WSL2", "Windows host"} {
		if !strings.Contains(body, want) {
			t.Errorf("Path help lacks %q", want)
		}
	}
}
