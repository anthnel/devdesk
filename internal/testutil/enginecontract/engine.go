package enginecontract

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// Engine is the engine a contract run reached.
type Engine struct {
	Tool    enginefixture.Tool
	Version string             // the client version, equal to the server's for docker
	Mode    enginefixture.Mode // rootful or rootless
}

// Probe asks the engine of tool what it is. It is a function of the package
// under contract: this package starts nothing.
type Probe func(tool enginefixture.Tool) (Engine, error)

// VersionsFile is the path of versions.env: VersionsEnv when set (in the test VM
// the script pushes its copy there), else the file of the repository this source
// file sits in. A binary built with -trimpath has no such path, and the VM always
// sets the variable.
func VersionsFile(getenv func(string) string) (string, error) {
	if v := getenv(VersionsEnv); v != "" {
		return v, nil
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("enginecontract: the path of the source is unknown: set " + VersionsEnv)
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "tools", "engine-vms", "versions.env"), nil
}

// CheckSupported reports whether the engine is one of the two versions (N and
// N-1) tools/engine-vms/versions.env pins for its tool.
func CheckSupported(e Engine, s enginefixture.Supported) error {
	list := s[e.Tool]
	if len(list) == 0 {
		return fmt.Errorf("%s is not a tool versions.env lists", e.Tool)
	}
	for _, v := range list {
		if v == e.Version {
			return nil
		}
	}
	return fmt.Errorf("%s %s is neither N nor N-1: versions.env pins %s (N, N-1)", e.Tool, e.Version, strings.Join(list, ", "))
}

// Banner is the line a run prints: engine, version, mode, run id and run mode.
func Banner(e Engine, runID string, m Mode) string {
	return fmt.Sprintf("engine-contract: %s %s %s, run %s, mode %s", e.Tool, e.Version, e.Mode, runID, m)
}
