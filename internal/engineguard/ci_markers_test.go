package engineguard_test

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engineguard"
)

// Review of OC-09a1, B2: the gate refuses on any CI marker, not on CI alone. The
// closed gate is reported first, as in enginecontract.Decide.
func TestTheContractGateRefusesEveryCIMarker(t *testing.T) {
	for _, marker := range []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "BUILDKITE", "TF_BUILD"} {
		t.Run(marker, func(t *testing.T) {
			env := map[string]string{"DEVDESK_ENGINE_CONTRACT": "1", marker: "true"}
			err := engineguard.ContractGate(func(n string) string { return env[n] })
			if err == nil {
				t.Fatalf("ContractGate() = nil with %s set, want a refusal", marker)
			}
			for _, w := range []string{"CI", marker} {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("ContractGate() = %q, want it to contain %q", err, w)
				}
			}
			closed := engineguard.ContractGate(func(n string) string { return map[string]string{marker: "true"}[n] })
			if closed == nil || !strings.Contains(closed.Error(), "DEVDESK_ENGINE_CONTRACT=1") {
				t.Errorf("ContractGate() with the gate closed = %v, want the closed-gate message", closed)
			}
		})
	}
}

func TestTheContractGateOpensWhenEveryMarkerIsEmpty(t *testing.T) {
	env := map[string]string{"DEVDESK_ENGINE_CONTRACT": "1"}
	for _, marker := range engineguard.CIMarkers() {
		env[marker] = ""
	}
	if err := engineguard.ContractGate(func(n string) string { return env[n] }); err != nil {
		t.Errorf("ContractGate() = %v, want nil", err)
	}
}
