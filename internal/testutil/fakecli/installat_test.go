package fakecli_test

import (
	"os/exec"
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// A tool installed at an explicit path answers the scenario and is journaled,
// while PATH still holds nothing: the binary a configuration names by its path.
func TestAToolInstalledAtAPathAnswersTheScenario(t *testing.T) {
	f := fakecli.Install(t, fakecli.Scenario{
		{Tool: "trivy", Args: []string{"--version"}, Reply: fakecli.Reply{Stdout: "Version: 0.50.1\n"}},
	})
	path := f.InstallAt(t, t.TempDir(), "trivy")

	if _, err := exec.LookPath("trivy"); err == nil {
		t.Error("trivy resolves through PATH, want it reachable by its path only")
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil || string(out) != "Version: 0.50.1\n" {
		t.Fatalf("%s --version = %q, %v", path, out, err)
	}
	inv := f.Invocations()
	if len(inv) != 1 || inv[0].Tool != "trivy" || !slices.Equal(inv[0].Args, []string{"--version"}) {
		t.Errorf("journal = %+v, want the one run", inv)
	}
}
