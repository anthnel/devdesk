package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/engineguard"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-09a1 (DEVDESK-806), step 3: what contract_support_test.go holds is tested
// here, without an engine. The probe runs under a strict stub or a fakecli tool;
// nothing starts a real docker or podman.
//
// The helpers the coder writes in contract_support_test.go (the support file, in
// package docker, named by guard G5 as the home of requireEngine):
//
//	type contract struct {
//		t *testing.T
//		s *enginecontract.Session // Engine, RunID, Mode, OutDir
//		// a2 adds the recording runner and the resources of the run
//	}
//	func requireEngine(t *testing.T) *contract // first statement: enginecontract.Require(t, probeEngine)
//	func probeEngine(tool enginefixture.Tool) (enginecontract.Engine, error)
//	func parseEngineVersion(tool enginefixture.Tool, out string) (string, error)
//	func parseEngineMode(tool enginefixture.Tool, out string) (enginefixture.Mode, error)
//
// The names of these tests do not start with "TestContract": a function of that
// name outside a *_contract_test.go file breaks G5 (R3), and `-run '^TestContract'`
// is what contract.sh selects.

func TestParseEngineVersion(t *testing.T) {
	tests := []struct {
		name    string
		tool    enginefixture.Tool
		out     string
		want    string
		wantErr string // substring, "" for none
	}{
		{name: "docker, client and server agree", tool: enginefixture.Docker, out: "29.10.1 29.10.1\n", want: "29.10.1"},
		{name: "docker, no trailing newline", tool: enginefixture.Docker, out: "28.5.2 28.5.2", want: "28.5.2"},
		{name: "docker, client and server differ", tool: enginefixture.Docker, out: "29.10.1 28.5.2\n", wantErr: "client and server differ"},
		{name: "docker, empty output", tool: enginefixture.Docker, out: "", wantErr: "version"},
		{name: "docker, only whitespace", tool: enginefixture.Docker, out: " \n", wantErr: "version"},
		{name: "docker, one word", tool: enginefixture.Docker, out: "29.10.1\n", wantErr: "version"},
		{name: "docker, three words", tool: enginefixture.Docker, out: "29.10.1 29.10.1 29.10.1\n", wantErr: "version"},
		{name: "podman", tool: enginefixture.Podman, out: "6.2.1\n", want: "6.2.1"},
		{name: "podman N-1", tool: enginefixture.Podman, out: "5.8.11", want: "5.8.11"},
		{name: "podman, empty output", tool: enginefixture.Podman, out: "\n", wantErr: "version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEngineVersion(tt.tool, tt.out)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseEngineVersion(%q, %q) = %q, %v, want an error containing %q", tt.tool, tt.out, got, err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("parseEngineVersion(%q, %q) = %q, %v, want %q", tt.tool, tt.out, got, err, tt.want)
			}
		})
	}
}

func TestParseEngineMode(t *testing.T) {
	tests := []struct {
		name    string
		tool    enginefixture.Tool
		out     string
		want    enginefixture.Mode
		wantErr bool
	}{
		{name: "docker, rootless listed", tool: enginefixture.Docker, out: `["name=seccomp,profile=builtin","name=rootless"]`, want: enginefixture.Rootless},
		{name: "docker, rootless alone, newline", tool: enginefixture.Docker, out: "[\"name=rootless\"]\n", want: enginefixture.Rootless},
		{name: "docker, no rootless", tool: enginefixture.Docker, out: `["name=seccomp,profile=builtin"]`, want: enginefixture.Rootful},
		{name: "docker, no option at all", tool: enginefixture.Docker, out: "[]", want: enginefixture.Rootful},
		{name: "podman, rootless", tool: enginefixture.Podman, out: "true\n", want: enginefixture.Rootless},
		{name: "podman, rootful", tool: enginefixture.Podman, out: "false\n", want: enginefixture.Rootful},
		{name: "podman, no newline", tool: enginefixture.Podman, out: "true", want: enginefixture.Rootless},
		{name: "podman, something else", tool: enginefixture.Podman, out: "maybe\n", wantErr: true},
		{name: "podman, empty", tool: enginefixture.Podman, out: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEngineMode(tt.tool, tt.out)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseEngineMode(%q, %q) = %q, want an error", tt.tool, tt.out, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("parseEngineMode(%q, %q) = %q, %v, want %q", tt.tool, tt.out, got, err, tt.want)
			}
		})
	}
}

// probeArgv are the two questions the probe asks, per engine (plan §3.4). The
// templates are to be confirmed on the test VMs for N and N-1; a change there is a
// change here, and the strict stub says which argv the probe sent instead.
func probeArgv(tool enginefixture.Tool) (version, mode []string) {
	if tool == enginefixture.Docker {
		return []string{"version", "--format", "{{.Client.Version}} {{.Server.Version}}"},
			[]string{"info", "--format", "{{json .SecurityOptions}}"}
	}
	return []string{"version", "--format", "{{.Client.Version}}"},
		[]string{"info", "--format", "{{.Host.Security.Rootless}}"}
}

// otherEngine is the engine the probe must not be misled by.
func otherEngine(tool enginefixture.Tool) string {
	if tool == enginefixture.Docker {
		return engine.Podman
	}
	return engine.Docker
}

func TestProbeEngineAsksExactlyTheTwoQuestionsOfItsEngine(t *testing.T) {
	tests := []struct {
		name          string
		tool          enginefixture.Tool
		version, mode string // what the engine answers
		want          enginecontract.Engine
	}{
		{"docker N, rootful", enginefixture.Docker, "29.10.1 29.10.1\n", `["name=seccomp,profile=builtin"]` + "\n",
			enginecontract.Engine{Tool: enginefixture.Docker, Version: "29.10.1", Mode: enginefixture.Rootful}},
		{"docker N-1, rootless", enginefixture.Docker, "28.5.2 28.5.2\n", `["name=seccomp,profile=builtin","name=rootless"]` + "\n",
			enginecontract.Engine{Tool: enginefixture.Docker, Version: "28.5.2", Mode: enginefixture.Rootless}},
		{"podman N, rootless", enginefixture.Podman, "6.2.1\n", "true\n",
			enginecontract.Engine{Tool: enginefixture.Podman, Version: "6.2.1", Mode: enginefixture.Rootless}},
		{"podman N-1, rootful", enginefixture.Podman, "5.8.11\n", "false\n",
			enginecontract.Engine{Tool: enginefixture.Podman, Version: "5.8.11", Mode: enginefixture.Rootful}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			versionArgv, modeArgv := probeArgv(tt.tool)
			s := stubStrict(t,
				reply{argv: versionArgv, out: tt.version},
				reply{argv: modeArgv, out: tt.mode},
			)
			// The engine in use is the other one: the probe is asked about tt.tool and
			// must not take its questions from engine.Current().
			useEngine(t, otherEngine(tt.tool))

			got, err := probeEngine(tt.tool)

			if err != nil || got != tt.want {
				t.Errorf("probeEngine(%q) = %+v, %v, want %+v", tt.tool, got, err, tt.want)
			}
			if len(s.calls) != 2 {
				t.Errorf("the probe ran %d invocations, want exactly 2: %v", len(s.calls), s.calls)
			}
			if name := engine.Current().Name; name != otherEngine(tt.tool) {
				t.Errorf("engine.Current() = %q after the probe, want it left as it was (%q): pinning the engine is requireEngine's", name, otherEngine(tt.tool))
			}
		})
	}
}

func TestProbeEngineFailsWhenTheEngineDoesNotAnswer(t *testing.T) {
	tests := []struct {
		name    string
		tool    enginefixture.Tool
		stub    *stubRunner
		wantErr string
	}{
		{
			name:    "the version call fails",
			tool:    enginefixture.Podman,
			stub:    &stubRunner{output: map[string][]byte{"version": []byte("cannot connect")}, err: map[string]error{"version": errExit}},
			wantErr: "",
		},
		{
			name:    "the info call fails",
			tool:    enginefixture.Docker,
			stub:    &stubRunner{output: map[string][]byte{"version": []byte("29.10.1 29.10.1\n")}, err: map[string]error{"info": errExit}},
			wantErr: "",
		},
		{
			name:    "docker client and server differ",
			tool:    enginefixture.Docker,
			stub:    &stubRunner{output: map[string][]byte{"version": []byte("29.10.1 28.5.2\n"), "info": []byte("[]")}},
			wantErr: "client and server differ",
		},
		{
			name:    "podman answers something that is not a mode",
			tool:    enginefixture.Podman,
			stub:    &stubRunner{output: map[string][]byte{"version": []byte("6.2.1\n"), "info": []byte("maybe\n")}},
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub(t, tt.stub)

			got, err := probeEngine(tt.tool)

			if err == nil {
				t.Fatalf("probeEngine(%q) = %+v, want an error", tt.tool, got)
			}
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("probeEngine(%q) error = %q, want it to contain %q", tt.tool, err, tt.wantErr)
			}
			if got != (enginecontract.Engine{}) {
				t.Errorf("probeEngine(%q) = %+v with an error, want the zero Engine", tt.tool, got)
			}
		})
	}
}

// versionsFile is a versions.env of the synthetic lines, for the run below.
func versionsFile(t *testing.T) string {
	t.Helper()
	const text = "DOCKER_N_VERSION=29.10.1\nDOCKER_N1_VERSION=28.5.2\nPODMAN_N_VERSION=6.2.1\nPODMAN_N1_VERSION=5.8.11\n"
	path := filepath.Join(t.TempDir(), "versions.env")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// requireEngine is the whole of the contract entry: the gate, the probe, the
// version check, then the engine of the run is pinned for the length of the test
// and put back afterwards. Run against a fakecli tool that answers the two probe
// questions, with the gate open: the PATH of the test holds the fake and nothing
// else, so no real engine can be reached.
func TestRequireEnginePinsTheEngineOfTheRunForTheLengthOfTheTest(t *testing.T) {
	tests := []struct {
		tool          enginefixture.Tool
		version, mode string
		want          enginecontract.Engine
	}{
		{enginefixture.Docker, "29.10.1 29.10.1\n", `["name=seccomp,profile=builtin"]`,
			enginecontract.Engine{Tool: enginefixture.Docker, Version: "29.10.1", Mode: enginefixture.Rootful}},
		{enginefixture.Podman, "5.8.11\n", "true\n",
			enginecontract.Engine{Tool: enginefixture.Podman, Version: "5.8.11", Mode: enginefixture.Rootless}},
	}
	for _, tt := range tests {
		t.Run(string(tt.tool), func(t *testing.T) {
			versionArgv, modeArgv := probeArgv(tt.tool)
			fake := fakecli.Install(t, fakecli.Scenario{
				{Tool: string(tt.tool), Args: versionArgv, Reply: fakecli.Reply{Stdout: tt.version}},
				{Tool: string(tt.tool), Args: modeArgv, Reply: fakecli.Reply{Stdout: tt.mode}},
			}, string(tt.tool))
			t.Setenv(engineguard.ContractEnv, "1")
			t.Setenv("CI", "")
			t.Setenv(enginecontract.ToolEnv, string(tt.tool))
			t.Setenv(enginecontract.RunEnv, "20261127-a1b2c3d4")
			t.Setenv(enginecontract.VersionsEnv, versionsFile(t))
			t.Setenv(enginecontract.RecordingEnv, "")
			previous := engine.Current()
			t.Cleanup(func() { engine.SetCurrent(previous) })

			t.Run("the contract", func(t *testing.T) {
				c := requireEngine(t)

				if c == nil || c.s == nil {
					t.Fatal("requireEngine returned no contract or no session")
				}
				if c.s.Engine != tt.want || c.s.RunID != "20261127-a1b2c3d4" || c.s.Mode != enginecontract.Verify {
					t.Errorf("Session = %+v, want engine %+v, run 20261127-a1b2c3d4, verify", *c.s, tt.want)
				}
				if got := engine.Current().Name; got != string(tt.tool) {
					t.Errorf("engine.Current() = %q during the contract, want %q: the functions under contract use the engine of the run", got, tt.tool)
				}
			})

			if got := engine.Current(); got != previous {
				t.Errorf("engine.Current() = %+v after the contract, want the engine it found, %+v", got, previous)
			}
			// Only the two probe questions went to the engine.
			if n := len(fake.Invocations()); n != 2 {
				t.Errorf("the fake engine was started %d times, want 2 (version, info): %+v", n, fake.Invocations())
			}
		})
	}
}
