package docker

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// The support file of the engine contract tests (OC-09a, DEVDESK-806): a contract
// is a TestContract* function of a *_contract_test.go file of this package that
// calls the functions of the package against a real docker or podman, outside CI,
// in the test VMs of tools/engine-vms (mise run engine-contract). Everything that
// is not a contract lives here, because a *_contract_test.go file declares nothing
// but TestContract* functions (guard G5, internal/engineguard).
//
// The engine-free core (the gate, the run id, the label, the version check, the
// banner) is internal/testutil/enginecontract, shared with the other packages
// that will have contracts. What speaks to the engine stays in this package, in
// _test.go files, so the guards on engine launches, destructive verbs and pulls
// (G2, G3, G8) have nothing to report in the shared package.

// contract is what a contract test holds: its test and the session the run
// opened. Later stories add the recording runner and the resources of the run.
type contract struct {
	t *testing.T
	s *enginecontract.Session
}

// requireEngine is the first statement of every contract test (guard G5). It
// applies the gate, so without DEVDESK_ENGINE_CONTRACT=1 the test is skipped and
// with CI set it fails, both before the engine is reached; probes the engine the
// run is about; then pins that engine for the length of the test and puts the
// previous one back afterwards.
func requireEngine(t *testing.T) *contract {
	s := enginecontract.Require(t, probeEngine)
	previous := engine.Current()
	t.Cleanup(func() { engine.SetCurrent(previous) })
	useEngine(t, string(s.Engine.Tool))
	return &contract{t: t, s: s}
}

// probeEngine asks the engine of tool what it is. It pins that engine only for
// its own length, so the functions under contract and the probe agree on the
// binary, and it asks two questions of the engine itself rather than infer
// anything from the binary's name or path. Only stdout is parsed: a warning on
// stderr must not change the answer.
//
// The templates are those of plan OC-09a, section 3.4; they are still to be
// confirmed on the test VMs, for N and N-1 of both engines. A template an
// engine does not accept is an error here, never a guess.
func probeEngine(tool enginefixture.Tool) (enginecontract.Engine, error) {
	previous := engine.Current()
	defer engine.SetCurrent(previous)
	engine.SetCurrent(engine.ShapeFor(string(tool)))

	versionArgs, modeArgs := probeArgs(tool)
	out, err := dockerOutput(versionArgs...)
	if err != nil {
		return enginecontract.Engine{}, probeFailure(tool, "version", err)
	}
	version, err := parseEngineVersion(tool, string(out))
	if err != nil {
		return enginecontract.Engine{}, err
	}
	out, err = dockerOutput(modeArgs...)
	if err != nil {
		return enginecontract.Engine{}, probeFailure(tool, "info", err)
	}
	mode, err := parseEngineMode(tool, string(out))
	if err != nil {
		return enginecontract.Engine{}, err
	}
	return enginecontract.Engine{Tool: tool, Version: version, Mode: mode}, nil
}

// probeArgs are the two questions the probe asks: the version (docker: client
// and server, which must agree; podman: the client's) and whether the engine
// runs rootless.
func probeArgs(tool enginefixture.Tool) (version, mode []string) {
	if tool == enginefixture.Docker {
		return []string{"version", "--format", "{{.Client.Version}} {{.Server.Version}}"},
			[]string{"info", "--format", "{{json .SecurityOptions}}"}
	}
	return []string{"version", "--format", "{{.Client.Version}}"},
		[]string{"info", "--format", "{{.Host.Security.Rootless}}"}
}

// probeFailure names the engine, the question and, when the engine said why,
// its own diagnostic.
func probeFailure(tool enginefixture.Tool, question string, err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if detail := strings.TrimSpace(string(exit.Stderr)); detail != "" {
			return fmt.Errorf("%s %s: %w: %s", tool, question, err, detail)
		}
	}
	return fmt.Errorf("%s %s: %w", tool, question, err)
}

// parseEngineVersion reads the answer to the version question: docker's client
// and server versions, which must be equal, or podman's single version.
func parseEngineVersion(tool enginefixture.Tool, out string) (string, error) {
	fields := strings.Fields(out)
	if tool == enginefixture.Docker {
		if len(fields) != 2 {
			return "", fmt.Errorf("docker version output %q: want the client and the server version", out)
		}
		if fields[0] != fields[1] {
			return "", fmt.Errorf("docker client and server differ: %s and %s", fields[0], fields[1])
		}
		return fields[0], nil
	}
	if len(fields) != 1 {
		return "", fmt.Errorf("%s version output %q: want one version", tool, out)
	}
	return fields[0], nil
}

// parseEngineMode reads the answer to the mode question: docker lists
// name=rootless among its security options when it runs rootless; podman says
// true or false.
func parseEngineMode(tool enginefixture.Tool, out string) (enginefixture.Mode, error) {
	out = strings.TrimSpace(out)
	if tool == enginefixture.Docker {
		if strings.Contains(out, "name=rootless") {
			return enginefixture.Rootless, nil
		}
		return enginefixture.Rootful, nil
	}
	switch out {
	case "true":
		return enginefixture.Rootless, nil
	case "false":
		return enginefixture.Rootful, nil
	}
	return "", fmt.Errorf("%s info output %q: want true or false", tool, out)
}
