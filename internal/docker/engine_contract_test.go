package docker

import "testing"

// The pilot contract of OC-09a1 (DEVDESK-806): the first TestContract* function,
// there so that guard G5 (internal/engineguard) has a contract to hold to
// requireEngine(t), and so that the gate is exercised by a real contract in every
// run (the test of the child process, contract_gate_test.go, runs this binary
// again and checks that it is skipped, or refused in CI).
//
// It reaches a real engine, so it needs DEVDESK_ENGINE_CONTRACT=1, an empty CI and
// DEVDESK_ENGINE_CONTRACT_TOOL (mise run engine-contract does the last one in the
// test VMs); otherwise requireEngine(t) skips it. It asks, creates and records
// nothing: the probe answers, and a read-only listing works.
func TestContractEngineAnswersItsVersion(t *testing.T) {
	c := requireEngine(t)

	probed, err := probeEngine(c.s.Engine.Tool)
	if err != nil {
		t.Fatalf("probeEngine(%s) = %v, want the engine to answer", c.s.Engine.Tool, err)
	}
	if probed != c.s.Engine {
		t.Errorf("probeEngine(%s) = %+v, want what requireEngine read, %+v", c.s.Engine.Tool, probed, c.s.Engine)
	}
	if _, err := ListContainers(true); err != nil {
		t.Errorf("ListContainers(true) = %v on %s %s %s", err, c.s.Engine.Tool, c.s.Engine.Version, c.s.Engine.Mode)
	}
}
