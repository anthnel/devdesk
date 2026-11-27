package enginecontract_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09a2 (DEVDESK-943), security decision M1 of the audit of OC-09a1: before a
// contract creates or removes the least resource, the gate asks for a mark that
// only a lab VM carries, on top of the gate of Decide. Two things, both required:
//
//   - DEVDESK_ENGINE_CONTRACT_RUN is set and is a valid run id. contract.sh
//     sets it in the VM; without it Require would invent one id per process, and
//     a developer who exported DEVDESK_ENGINE_CONTRACT=1 would be running
//     contracts that create and remove resources on his own engine;
//   - the file /etc/dd/current exists and says "n" or "n1". It is the file
//     tools/engine-vms/vm/dd-use.sh writes in each VM (the line of the matrix the
//     VM is on, which verify.sh and contract.sh read back), so it is the mark the
//     VMs already carry; no other machine has it.
//
// A read-only contract (the pilot TestContractEngineAnswersItsVersion) needs
// neither: it stays possible outside a VM, in verify mode, as the team lead
// decided ("contracts on the workstation, to verify only"). The mark is asked by
// what creates or removes: in package docker, c.container, c.volume, c.network
// and c.cleanupNow (contract_resources_internal_test.go).
//
// The API these tests expect, in internal/testutil/enginecontract (lab.go):
//
//	const LabMarkerFile = "/etc/dd/current"
//	func CheckLab(getenv func(string) string, readFile func(name string) ([]byte, error)) error
//	func (s *Session) RequireLab(t testing.TB)        // t.Fatalf(CheckLab(os.Getenv, <marker reader>))
//	func LabMarker() string                            // the path RequireLab reads, LabMarkerFile in production
//	func SetLabMarkerForTest(t testing.TB, path string) // swaps it until the end of t; panics outside a test binary
//
// CheckLab is pure: the environment and the reading of the file are passed in.
// The error names each thing that is missing (the variable, the file), says what
// is accepted, and points to `mise run engine-contract`. A marker that is not a
// plain "n" or "n1" (empty, another word, two lines) or that cannot be read is
// refused: an empty file made by hand must not open the gate.
//
// Known limit, noted in the docs: root on a developer machine can create
// /etc/dd/current; the mark stops an accident, not an adversary.

const labRun = "20261127-a1b2c3d4"

func TestCheckLab(t *testing.T) {
	notExist := &fs.PathError{Op: "open", Path: enginecontract.LabMarkerFile, Err: fs.ErrNotExist}
	denied := &fs.PathError{Op: "open", Path: enginecontract.LabMarkerFile, Err: fs.ErrPermission}
	isDir := &fs.PathError{Op: "read", Path: enginecontract.LabMarkerFile, Err: errors.New("is a directory")}

	tests := []struct {
		name    string
		run     string // the value of DEVDESK_ENGINE_CONTRACT_RUN
		marker  string
		readErr error
		want    []string // substrings of the error; nil for none
	}{
		{name: "both present, line n", run: labRun, marker: "n\n"},
		{name: "both present, line n1, no newline", run: labRun, marker: "n1"},
		{name: "marker absent", run: labRun, readErr: notExist,
			want: []string{enginecontract.LabMarkerFile, "mise run engine-contract"}},
		{name: "variable absent", marker: "n\n",
			want: []string{enginecontract.RunEnv, "mise run engine-contract"}},
		{name: "both absent", readErr: notExist,
			want: []string{enginecontract.RunEnv, enginecontract.LabMarkerFile}},
		{name: "marker unreadable", run: labRun, readErr: denied, want: []string{enginecontract.LabMarkerFile}},
		{name: "marker is a directory", run: labRun, readErr: isDir, want: []string{enginecontract.LabMarkerFile}},
		{name: "empty marker", run: labRun, marker: "", want: []string{enginecontract.LabMarkerFile, `"n" or "n1"`}},
		{name: "marker names another line", run: labRun, marker: "n2\n", want: []string{enginecontract.LabMarkerFile}},
		{name: "marker with a word", run: labRun, marker: "latest\n", want: []string{enginecontract.LabMarkerFile}},
		{name: "marker with two lines", run: labRun, marker: "n\nn1\n", want: []string{enginecontract.LabMarkerFile}},
		{name: "run id too short", run: "abc", marker: "n\n", want: []string{enginecontract.RunEnv, "not a valid run id"}},
		{name: "run id with a slash", run: "../../etc/passwd", marker: "n\n", want: []string{enginecontract.RunEnv, "not a valid run id"}},
		{name: "run id in capitals", run: "20261127-A1B2C3D4", marker: "n\n", want: []string{enginecontract.RunEnv}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var asked []string
			readFile := func(name string) ([]byte, error) {
				asked = append(asked, name)
				if tt.readErr != nil {
					return nil, tt.readErr
				}
				return []byte(tt.marker), nil
			}
			getenv := func(k string) string {
				if k == enginecontract.RunEnv {
					return tt.run
				}
				return ""
			}

			err := enginecontract.CheckLab(getenv, readFile)

			if len(tt.want) == 0 {
				if err != nil {
					t.Fatalf("CheckLab() = %v, want nil: both marks are there", err)
				}
			} else {
				if err == nil {
					t.Fatalf("CheckLab() = nil, want a refusal naming %q", tt.want)
				}
				for _, w := range tt.want {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("CheckLab() = %q, want it to contain %q", err, w)
					}
				}
			}
			for _, name := range asked {
				if name != enginecontract.LabMarkerFile {
					t.Errorf("CheckLab read %q, want only %q", name, enginecontract.LabMarkerFile)
				}
			}
		})
	}
}

func TestLabMarkerFileIsTheOneTheVMsWrite(t *testing.T) {
	if enginecontract.LabMarkerFile != "/etc/dd/current" {
		t.Errorf("LabMarkerFile = %q, want /etc/dd/current: the file dd-use.sh writes and contract.sh reads back", enginecontract.LabMarkerFile)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "engine-vms", "vm", "dd-use.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), ">"+enginecontract.LabMarkerFile) {
		t.Errorf("tools/engine-vms/vm/dd-use.sh does not write %s: the mark would exist in no VM", enginecontract.LabMarkerFile)
	}
}

// callRequireLab runs s.RequireLab under a captureT (require_test.go) and reports
// whether it ended the test, and with what.
func callRequireLab(t *testing.T, s *enginecontract.Session) *captureT {
	t.Helper()
	c := &captureT{TB: t}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(endOfTest); !ok {
					panic(r)
				}
			}
		}()
		s.RequireLab(c)
	}()
	return c
}

func TestSessionRequireLab(t *testing.T) {
	tests := []struct {
		name       string
		withMarker bool
		run        string
		want       []string // substrings of the fatal message; nil: no failure
	}{
		{name: "marker and variable", withMarker: true, run: labRun},
		{name: "marker absent", run: labRun, want: []string{enginecontract.LabMarkerFile}},
		{name: "variable absent", withMarker: true, want: []string{enginecontract.RunEnv}},
		{name: "both absent", want: []string{enginecontract.LabMarkerFile, enginecontract.RunEnv}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "current")
			if tt.withMarker {
				if err := os.WriteFile(path, []byte("n1\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			enginecontract.SetLabMarkerForTest(t, path)
			t.Setenv(enginecontract.RunEnv, tt.run)
			s := &enginecontract.Session{
				Engine: enginecontract.Engine{Tool: enginefixture.Podman, Version: "6.2.1", Mode: enginefixture.Rootless},
				RunID:  labRun, Mode: enginecontract.Verify,
			}

			got := callRequireLab(t, s)

			if len(tt.want) == 0 {
				if got.failed || got.skipped {
					t.Fatalf("RequireLab ended the test (failed %v, skipped %v, %q), want it to let a lab run through", got.failed, got.skipped, got.msg)
				}
				return
			}
			if !got.failed || got.skipped {
				t.Fatalf("RequireLab failed = %v, skipped = %v, want a failure: nothing may be created or removed without the mark", got.failed, got.skipped)
			}
			for _, w := range tt.want {
				if !strings.Contains(got.msg, w) {
					t.Errorf("fatal message = %q, want it to contain %q", got.msg, w)
				}
			}
		})
	}
}

// The swap is for the length of one test: a path left behind would let a later
// test pass the mark on a file it never made.
func TestSetLabMarkerForTestIsUndoneWithTheTest(t *testing.T) {
	t.Run("inside", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "current")
		enginecontract.SetLabMarkerForTest(t, path)
		if got := enginecontract.LabMarker(); got != path {
			t.Errorf("LabMarker() = %q inside the test, want %q", got, path)
		}
	})
	if got := enginecontract.LabMarker(); got != enginecontract.LabMarkerFile {
		t.Errorf("LabMarker() = %q after the test, want %q put back", got, enginecontract.LabMarkerFile)
	}
}

// A read-only contract is not concerned: Require opens the gate on the variable
// of Decide, the tool, the probe and the versions, as before, with no mark.
func TestRequireNeedsNoLabMarkerForAReadOnlyContract(t *testing.T) {
	openGate(t, enginefixture.Podman)
	t.Setenv(enginecontract.RunEnv, "")
	enginecontract.SetLabMarkerForTest(t, filepath.Join(t.TempDir(), "absent"))

	got := callRequire(t, podmanN, nil)

	if got.c.failed || got.c.skipped || got.session == nil {
		t.Fatalf("Require failed = %v, skipped = %v (%q), session %v: a read-only contract runs outside a VM, in verify mode",
			got.c.failed, got.c.skipped, got.c.msg, got.session)
	}
	if got.session.Mode != enginecontract.Verify {
		t.Errorf("Session.Mode = %q, want verify", got.session.Mode)
	}
}
