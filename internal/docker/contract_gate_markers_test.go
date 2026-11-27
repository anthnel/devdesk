package docker

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engineguard"
)

// Review of OC-09a1, B2: every CI marker refuses a contract, with the gate open and
// an engine named, before any call to the engine. A child process, as in
// TestEveryContractIsSkippedOrRefusedOutsideAContractRun.
func TestEveryCIMarkerRefusesTheContractsOfAChildRun(t *testing.T) {
	contracts := listedContracts(t)
	for _, marker := range engineguard.CIMarkers() {
		t.Run(marker, func(t *testing.T) {
			env := childEnvironment(t, marker+"=true", "DEVDESK_ENGINE_CONTRACT=1", "DEVDESK_ENGINE_CONTRACT_TOOL=docker")

			out, code := runChild(t, env, "-test.run=^TestContract", "-test.v", "-test.count=1")

			if code == 0 {
				t.Errorf("exit code = 0, want non-zero\n%s", out)
			}
			failed := verdicts(out, "FAIL")
			for _, name := range contracts {
				if !failed[name] {
					t.Errorf("%s was not reported as failed:\n%s", name, out)
				}
			}
			if !strings.Contains(out, "refused") || !strings.Contains(out, marker) {
				t.Errorf("the refusal does not name %s:\n%s", marker, out)
			}
			if strings.Contains(out, "engineguard:") || strings.Contains(out, "panic:") {
				t.Errorf("the refusal came after an engine call:\n%s", out)
			}
		})
	}
}
