package fakecli

import (
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// Fixtures turns recorded engine fixtures (OC-08, DEVDESK-805) into a Source: one
// exact exchange per fixture, restricted to the tool the fixture names, with the
// recorded stdout, stderr and exit code, byte for byte. The tool comes from the
// fixture's own "# tool:" header; nothing here reads the engine in use.
//
// A selection the replay cannot serve (none, a pending fixture, two fixtures with
// the same tool and argv) fails the test at once with Fatalf, naming the
// fixtures. The strictness of Install applies as it is: an argv no fixture holds
// fails the test when it ends, with the tool and the argv. A fixture nothing asks
// for is not a failure here, like an unused Exchange.
func Fixtures(t testing.TB, fixtures ...enginefixture.Fixture) Source {
	t.Helper()
	if _, err := enginefixture.NewReplay(fixtures...); err != nil {
		t.Fatalf("fakecli: %v", err)
	}
	exchanges := make(Scenario, 0, len(fixtures))
	for _, f := range fixtures {
		exchanges = append(exchanges, Exchange{
			Tool: string(f.Meta.Tool),
			Args: slices.Clone(f.Exchange.Argv),
			Reply: Reply{
				Stdout: string(f.Exchange.Stdout),
				Stderr: string(f.Exchange.Stderr),
				Exit:   f.Exchange.Exit,
			},
		})
	}
	return exchanges
}
