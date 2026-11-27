package docker

import (
	"strings"
	"testing"
)

// A network a container is connected to cannot be removed. The words differ (docker:
// active endpoints, podman: associated containers) and so does the exit code, so
// the contract requires a failure and the engine's reason, and the fixture holds the rest.
// OC-09b1 (DEVDESK-807), scenario networks/rm-used.
func TestContractRemovingANetworkInUseIsRefused(t *testing.T) {
	c := requireEngine(t)
	network, _ := c.usedNetwork("n1", "c1")

	var err error
	ex := c.exchange("networks", "rm-used", "rm of a network a container is connected to is refused", func() {
		err = RemoveNetwork(network)
	})

	if err == nil {
		t.Fatalf("RemoveNetwork(%s) = nil on a network a container uses, want the engine to refuse it", network)
	}
	if ex.Exit == 0 {
		t.Errorf("exit code = 0 for a refused removal, want non-zero")
	}
	if len(strings.TrimSpace(string(ex.Stderr))) == 0 {
		t.Errorf("stderr is empty, want the engine's reason")
	}
	t.Logf("%s %s %s: rm of a network in use exits %d, stderr %q",
		c.s.Engine.Tool, c.s.Engine.Version, c.s.Engine.Mode, ex.Exit, strings.TrimSpace(string(ex.Stderr)))
}
