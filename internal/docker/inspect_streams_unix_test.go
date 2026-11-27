//go:build unix

package docker

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-04 (DEVDESK-800), criterion 3: InspectContainer returns the engine's stdout
// and nothing from its stderr.
//
// The engine is a fakecli docker (declared to the engine guard, DEVDESK-799) that
// answers a deprecation warning on stderr and the JSON document on stdout, run
// through the real cliRunner: the test holds whichever way the package decides to
// keep the two streams apart (a flag on dockerCmd, a second runner method...).
// No real docker or podman is reached.
func installWarningThenJSONEngine(t *testing.T) {
	t.Helper()
	fakecli.Install(t, fakecli.Scenario{{
		Tool: "docker", Args: []string{"inspect"}, Prefix: true,
		Reply: fakecli.Reply{
			Stdout: `[{"Id":"aaaa111122223333","Name":"/web"}]` + "\n",
			Stderr: "WARNING: this engine version is deprecated\n",
		},
	}}, "docker")
	previousRunner, previousEngine := runner, engine.Current()
	runner = cliRunner{}
	engine.SetCurrent(engine.ShapeFor("docker"))
	t.Cleanup(func() {
		runner = previousRunner
		engine.SetCurrent(previousEngine)
	})
}

func TestInspectContainerReturnsStdoutOnly(t *testing.T) {
	installWarningThenJSONEngine(t)

	got, err := InspectContainer("aaaa111122223333")

	if err != nil {
		t.Fatalf("InspectContainer() error = %v", err)
	}
	if strings.Contains(string(got), "WARNING") {
		t.Errorf("InspectContainer() carries the engine's stderr:\n%s", got)
	}
	var doc []map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Errorf("InspectContainer() is not valid JSON (%v):\n%s", err, got)
	}
}
