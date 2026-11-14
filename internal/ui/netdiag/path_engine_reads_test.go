package netdiag

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/engine"
)

// envFakeEngineLog names the file the fake engine binary appends its argv to.
const envFakeEngineLog = "DEVDESK_TEST_FAKE_ENGINE_LOG"

// init turns this test binary into a recording fake engine when the
// environment asks for it: it appends its arguments, one line per call, and
// exits before the testing flags are parsed. No real docker is involved.
func init() {
	logPath := os.Getenv(envFakeEngineLog)
	if logPath == "" {
		return
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(3)
	}
	_, _ = f.WriteString(strings.Join(os.Args[1:], "\x1f") + "\n")
	_ = f.Close()
	os.Exit(0)
}

// recordedEngineCalls points the current engine at this test binary and
// returns a reader for the recorded argument lists.
func recordedEngineCalls(t *testing.T) func() [][]string {
	t.Helper()
	logPath := t.TempDir() + string(os.PathSeparator) + "engine.log"
	t.Setenv(envFakeEngineLog, logPath)
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	prev := engine.Current()
	shape := prev
	shape.Name, shape.Binary = engine.Docker, self
	engine.SetCurrent(shape)
	t.Cleanup(func() { engine.SetCurrent(prev) })
	return func() [][]string {
		raw, err := os.ReadFile(logPath)
		if err != nil {
			return nil
		}
		var calls [][]string
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if line != "" {
				calls = append(calls, strings.Split(line, "\x1f"))
			}
		}
		return calls
	}
}

// Criterion 7: the reads behind the source selector and the producer never
// start, create or enter a container. The arguments recorded on the real
// engine wiring are ps, info and container inspect, nothing else.
func TestPathEngineReadsStartNothing(t *testing.T) {
	calls := recordedEngineCalls(t)
	e := systemPathEngine()

	listPathSources(context.Background(), e)
	runProducerFrom(t, context.Background(), 1, pathTarget{Host: "10.0.0.9"}, testCtrSource(), time.Second, pathEngineWithoutNetwork(e))

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
		default:
			t.Fatalf("engine call %v: only ps, info and container inspect are allowed", args)
		}
		for _, a := range args {
			if slices.Contains(forbidden, a) {
				t.Fatalf("engine call %v carries the forbidden verb %q", args, a)
			}
		}
	}
	for _, want := range []string{"ps", "info", "inspect"} {
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
