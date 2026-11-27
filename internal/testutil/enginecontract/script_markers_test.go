package enginecontract_test

import (
	"strings"
	"testing"
)

// Review of OC-09a1, B2: contract.sh refuses on every CI marker, as the Go gate does,
// before any connection to the VM host.
func TestTheScriptRefusesEveryCIMarkerEvenWithTheVariable(t *testing.T) {
	for _, marker := range ciMarkerNames {
		t.Run(marker, func(t *testing.T) {
			out, code := runScript(t, scriptEnv("DEVDESK_ENGINE_CONTRACT=1", marker+"=true"), "verify", "docker", "n")

			if code != 1 {
				t.Errorf("exit code = %d, want 1\n%s", code, out)
			}
			if !strings.Contains(out, "refused") || !strings.Contains(out, marker) {
				t.Errorf("output does not say the run was refused because of %s:\n%s", marker, out)
			}
			noRemoteTrace(t, out)
		})
	}
}

// The variable comes first: a marker alone is a skip, as in Decide.
func TestTheScriptSkipsBeforeLookingAtTheMarkers(t *testing.T) {
	for _, marker := range ciMarkerNames {
		t.Run(marker, func(t *testing.T) {
			out, code := runScript(t, scriptEnv(marker+"=true"), "verify", "docker", "n")

			if code != 0 || !strings.Contains(out, "skipped") {
				t.Errorf("exit code = %d, want 0 and a skip message\n%s", code, out)
			}
			noRemoteTrace(t, out)
		})
	}
}

// The advice to set the variable for the one command, never to export it (review of
// OC-09a1, M1), is in the skip message, the usage header and the README.
func TestTheScriptAdvisesAgainstExportingTheVariable(t *testing.T) {
	out, _ := runScript(t, scriptEnv(), "verify", "docker", "n")
	if !strings.Contains(out, "do not export") {
		t.Errorf("the skip message does not say not to export the variable:\n%s", out)
	}
}
