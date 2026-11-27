//go:build unix

package docker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// OC-04 (DEVDESK-800), criterion 3: InspectContainer returns the engine's stdout
// and nothing from its stderr.
//
// The engine is a script in a temp dir that the engine seam points at, run
// through the real cliRunner: the test holds whichever way the package decides to
// keep the two streams apart (a flag on dockerCmd, a second runner method...).
// No real docker or podman is reached.
const warningThenJSON = `#!/bin/sh
echo "WARNING: this engine version is deprecated" >&2
echo '[{"Id":"aaaa111122223333","Name":"/web"}]'
`

func installScriptEngine(t *testing.T, script string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "engine")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil { //nolint:gosec // a test executable
		t.Fatal(err)
	}
	previousRunner, previousEngine := runner, engine.Current()
	runner = cliRunner{}
	shape := engine.ShapeFor("docker")
	shape.Binary = bin
	engine.SetCurrent(shape)
	t.Cleanup(func() {
		runner = previousRunner
		engine.SetCurrent(previousEngine)
	})
}

func TestInspectContainerReturnsStdoutOnly(t *testing.T) {
	installScriptEngine(t, warningThenJSON)

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
