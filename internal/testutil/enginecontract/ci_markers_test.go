package enginecontract_test

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/engineguard"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// Defence in depth (review of OC-09a1, B2): besides CI, the markers of GitHub
// Actions, GitLab CI, Buildkite and Azure Pipelines refuse a contract run, with the
// gate open, in the same place the order of the rules puts CI.

// clearCIMarkers empties every CI marker for the length of t, so a test that opens
// the gate on purpose does not depend on the system that runs the suite.
func clearCIMarkers(t *testing.T) {
	t.Helper()
	for _, name := range engineguard.CIMarkers() {
		t.Setenv(name, "")
	}
}

var ciMarkerNames = []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "BUILDKITE", "TF_BUILD"}

func TestTheCIMarkersAreTheFiveOfTheReview(t *testing.T) {
	got := engineguard.CIMarkers()
	if strings.Join(got, ",") != strings.Join(ciMarkerNames, ",") {
		t.Errorf("CIMarkers() = %v, want %v", got, ciMarkerNames)
	}
}

func TestDecideRefusesEveryCIMarkerWithTheGateOpen(t *testing.T) {
	for _, marker := range ciMarkerNames {
		t.Run(marker, func(t *testing.T) {
			got := enginecontract.Decide(env("DEVDESK_ENGINE_CONTRACT", "1", "DEVDESK_ENGINE_CONTRACT_TOOL", "docker", marker, "true"))

			if got.Skip != "" {
				t.Fatalf("Decide() skipped (%q), want a refusal", got.Skip)
			}
			for _, w := range []string{"refused", "CI", marker} {
				if !strings.Contains(got.Refuse, w) {
					t.Errorf("Refuse = %q, want it to contain %q", got.Refuse, w)
				}
			}
		})
	}
}

func TestDecideSkipsBeforeLookingAtTheMarkers(t *testing.T) {
	for _, marker := range ciMarkerNames {
		t.Run(marker, func(t *testing.T) {
			got := enginecontract.Decide(env(marker, "true"))
			if got.Refuse != "" || got.Skip == "" {
				t.Errorf("Decide() = skip %q, refuse %q, want a skip: the variable comes first", got.Skip, got.Refuse)
			}
		})
	}
}

func TestDecideIgnoresAMarkerSetToEmpty(t *testing.T) {
	e := map[string]string{"DEVDESK_ENGINE_CONTRACT": "1", "DEVDESK_ENGINE_CONTRACT_TOOL": "podman"}
	for _, marker := range ciMarkerNames {
		e[marker] = ""
	}
	got := enginecontract.Decide(func(n string) string { return e[n] })
	if got.Skip != "" || got.Refuse != "" || got.Tool != enginefixture.Podman {
		t.Errorf("Decide() = %+v, want a go-ahead for podman", got)
	}
}

func TestRequireIsFatalForEveryCIMarkerAndNeverProbes(t *testing.T) {
	for _, marker := range ciMarkerNames {
		t.Run(marker, func(t *testing.T) {
			openGate(t, enginefixture.Podman)
			t.Setenv(marker, "true")

			got := callRequire(t, podmanN, nil)

			if !got.c.failed || got.c.skipped || !strings.Contains(got.c.msg, marker) {
				t.Fatalf("failed = %v, skipped = %v, message %q, want a refusal that names %s", got.c.failed, got.c.skipped, got.c.msg, marker)
			}
			if len(got.probed) != 0 {
				t.Errorf("the probe was called %d time(s) under %s", len(got.probed), marker)
			}
		})
	}
}
