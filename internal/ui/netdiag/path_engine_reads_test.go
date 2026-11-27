package netdiag

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// recordedEngineCalls points the current engine at a fake engine
// (internal/testutil/fakecli) and returns a reader for the recorded argument
// lists. No real docker is involved. The fake answers every argv with nothing
// and success, as the recording binary it replaces did.
func recordedEngineCalls(t *testing.T) func() [][]string {
	t.Helper()
	fake := fakecli.Install(t, fakecli.Scenario{{Tool: "docker", Prefix: true}})
	prev := engine.Current()
	shape := prev
	shape.Name, shape.Binary = engine.Docker, fake.InstallAt(t, t.TempDir(), "docker")
	engine.SetCurrent(shape)
	t.Cleanup(func() { engine.SetCurrent(prev) })
	return func() [][]string {
		var calls [][]string
		for _, inv := range fake.Invocations() {
			calls = append(calls, inv.Args)
		}
		return calls
	}
}

// Criterion 7: the reads behind the source selector and the producer never
// start, create or enter a container. The arguments recorded on the real
// engine wiring are ps, info, container inspect and network inspect (DEVDESK-331),
// nothing else.
func TestPathEngineReadsStartNothing(t *testing.T) {
	calls := recordedEngineCalls(t)
	e := systemPathEngine()

	listPathSources(context.Background(), e)
	runProducerFrom(t, context.Background(), 1, pathTarget{Host: "10.0.0.9"}, testCtrSource(), time.Second, pathEngineWithoutNetwork(e))
	// The bridge read, on the real network wiring and a fake interface list.
	be := pathEngineWithoutNetwork(e)
	be.network = e.network
	readPathBridge(context.Background(), netCtr("app_net", testNetID), time.Second, be)

	got := calls()
	forbidden := []string{"start", "run", "exec", "create", "restart", "unpause", "attach", "kill", "stop", "pause", "rm", "cp", "commit"}
	seen := map[string]bool{}
	for _, args := range got {
		if len(args) == 0 {
			t.Fatalf("empty engine call in %v", got)
		}
		switch args[0] {
		case "ps", "info":
			seen[args[0]] = true
		case "container":
			if len(args) < 2 || args[1] != "inspect" {
				t.Fatalf("engine call %v: only `container inspect` is allowed", args)
			}
			seen["inspect"] = true
		case "network":
			if len(args) < 2 || args[1] != "inspect" {
				t.Fatalf("engine call %v: only `network inspect` is allowed", args)
			}
			seen["network"] = true
		default:
			t.Fatalf("engine call %v: only ps, info, container inspect and network inspect are allowed", args)
		}
		for _, a := range args {
			if slices.Contains(forbidden, a) {
				t.Fatalf("engine call %v carries the forbidden verb %q", args, a)
			}
		}
	}
	for _, want := range []string{"ps", "info", "inspect", "network"} {
		if !seen[want] {
			t.Fatalf("the reads never ran %q (calls: %v): the guard would pass on nothing", want, got)
		}
	}
}

// pathEngineWithoutNetwork keeps the real container wiring and replaces the
// socket and resolver parts, which this guard must not touch.
func pathEngineWithoutNetwork(e pathEngine) pathEngine {
	f := &fakeEngine{traceFn: oneHopTrace}
	fe := f.engine()
	fe.engineName, fe.engineOS, fe.running, fe.inspect = e.engineName, e.engineOS, e.running, e.inspect
	return fe
}
