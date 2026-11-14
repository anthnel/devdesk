package netdiag

import (
	"context"
	"net/netip"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// TestPathFlowProducerToModel replays the real producer's messages through
// PathModel.update, as the router would, until the done message.
func TestPathFlowProducerToModel(t *testing.T) {
	final := []nettrace.Hop{hopAt(1), hopAt(3), {TTL: 4, Kind: nettrace.HopTarget, Addr: netip.MustParseAddr("1.1.1.1")}}
	f := &fakeEngine{traceFn: func(_ context.Context, a netip.Addr, _ nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error) {
		for _, h := range []nettrace.Hop{final[1], final[0], final[2]} {
			onHop(h)
		}
		return nettrace.Result{Target: a, Outcome: nettrace.OutcomeReached, Hops: final}, nil
	}}
	pm := newPathModel(f.engine())
	pm.resize(120, 30)
	pm, _ = pmFeed(t, pm, testutil.Type("1.1.1.1")...)
	pm, _ = pmFeed(t, pm, testutil.Key("enter")) // the launch Cmd is not run here

	out := make(chan tea.Msg, pathChanCap)
	producePath(context.Background(), pm.gen, pm.target, pm.resolveTimeout, pm.engine, out)
	for msg := range out {
		pm, _ = pmFeed(t, pm, msg)
	}

	if pm.running() || pm.result == nil || pm.result.Outcome != nettrace.OutcomeReached {
		t.Fatalf("running=%v result=%+v, want a reached result", pm.running(), pm.result)
	}
	if !reflect.DeepEqual(pm.hops, final) {
		t.Errorf("hops = %v, want Result.Hops %v", pm.hops, final)
	}
	if got, want := pmHopTTLs(pm), []int{1, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

// A full channel must still deliver the done message.
func TestProducePathDoneSurvivesAFullChannel(t *testing.T) {
	f := &fakeEngine{traceFn: func(_ context.Context, a netip.Addr, _ nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error) {
		for i := 0; i < pathChanCap+5; i++ {
			onHop(hopAt(i + 1))
		}
		return nettrace.Result{Target: a}, nil
	}}
	out := make(chan tea.Msg, pathChanCap)
	producePath(context.Background(), 1, pathTarget{Host: "1.1.1.1"}, 0, f.engine(), out)
	var last tea.Msg
	for m := range out {
		last = m
	}
	if _, ok := last.(pathDoneMsg); !ok {
		t.Errorf("last message = %T, want pathDoneMsg", last)
	}
}
