package docker

import (
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// The code under test calls the engine from several goroutines (Bubble Tea Cmds):
// fixtureRunner.Run and Build must be safe for concurrent use. Run under -race.
func TestFixtureRunnerIsSafeForConcurrentUse(t *testing.T) {
	usingEngine(t, "docker")
	f := pickFixture(t, "rm-running", enginefixture.Docker)
	runner := useFixtures(t, f)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				out, err := runner.Run(dockerCmd{Args: f.Exchange.Argv})
				if len(out) != 0 || err == nil {
					t.Errorf("Run = %q, %v, want no stdout and the recorded failure", out, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
