package enginecontract

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

// LabMarkerFile is the file tools/engine-vms/vm/dd-use.sh writes in each test VM:
// the line of the matrix the VM is on, "n" or "n1". No other machine has it, which
// is what makes it a mark of the lab (OC-09a2, security decision M1).
const LabMarkerFile = "/etc/dd/current"

var (
	labMu     sync.Mutex
	labMarker = LabMarkerFile
)

// LabMarker is the path RequireLab reads: LabMarkerFile, except for the length of a
// test that swapped it with SetLabMarkerForTest.
func LabMarker() string {
	labMu.Lock()
	defer labMu.Unlock()
	return labMarker
}

// SetLabMarkerForTest makes RequireLab read path instead of LabMarkerFile until the
// end of t, then puts the previous path back. It panics outside a test binary, so
// production code cannot move the mark.
func SetLabMarkerForTest(t testing.TB, path string) {
	t.Helper()
	if !testing.Testing() {
		panic("enginecontract: SetLabMarkerForTest outside a test binary")
	}
	labMu.Lock()
	previous := labMarker
	labMarker = path
	labMu.Unlock()
	t.Cleanup(func() {
		labMu.Lock()
		labMarker = previous
		labMu.Unlock()
	})
}

// CheckLab reports what is missing for a contract to create or remove a resource.
// Two things are required, on top of the gate of Decide:
//
//   - RunEnv is set and is a valid run id: contract.sh sets it in the VM. Without
//     it Require would make one id up per process, and a developer who exported
//     DEVDESK_ENGINE_CONTRACT=1 would create and remove resources on his own engine;
//   - LabMarkerFile exists and holds exactly "n" or "n1" (a final newline is
//     allowed): the mark of a test VM. An empty file, another word or two lines is
//     refused, so a file made by hand with no thought does not open the gate.
//
// The environment and the reading of the file are passed in, so the check is pure.
// A read-only contract needs neither and does not call it.
//
// Limit: root on a developer machine can create LabMarkerFile. The mark stops an
// accident, not an adversary.
func CheckLab(getenv func(string) string, readFile func(name string) ([]byte, error)) error {
	var missing []string
	switch run := getenv(RunEnv); {
	case run == "":
		missing = append(missing, RunEnv+" is not set (contract.sh sets it in the test VM)")
	case !ValidRunID(run):
		missing = append(missing, fmt.Sprintf("%s=%q is not a valid run id", RunEnv, run))
	}
	data, err := readFile(LabMarkerFile)
	switch {
	case errors.Is(err, os.ErrNotExist):
		missing = append(missing, LabMarkerFile+" does not exist (dd-use.sh writes it in each test VM; this is not one)")
	case err != nil:
		missing = append(missing, fmt.Sprintf("%s cannot be read: %v", LabMarkerFile, err))
	default:
		if line := strings.TrimSuffix(string(data), "\n"); line != "n" && line != "n1" {
			missing = append(missing, fmt.Sprintf(`%s must hold exactly "n" or "n1" (the line of the VM), not %q`, LabMarkerFile, string(data)))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("contract refused: creating or removing a resource needs a test VM: %s; "+
		"run the contracts with mise run engine-contract (see docs/architecture/network.md, \"Engine contracts\")",
		strings.Join(missing, "; "))
}

// RequireLab ends the test with a failure unless CheckLab accepts the environment
// and the marker. A contract calls it, through the contract helpers, before it
// creates or removes the least resource.
func (s *Session) RequireLab(t testing.TB) {
	t.Helper()
	read := func(string) ([]byte, error) { return os.ReadFile(LabMarker()) }
	if err := CheckLab(os.Getenv, read); err != nil {
		t.Fatal(err)
	}
}
