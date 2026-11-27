package enginecontract_test

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09a1 (DEVDESK-806), criterion 1: the gate of the contract tests. Decide is a
// pure function of an environment; the first rule that applies wins:
//
//  1. DEVDESK_ENGINE_CONTRACT is not "1"            -> Skip
//  2. CI is not empty                               -> Refuse
//  3. DEVDESK_ENGINE_CONTRACT_TOOL is not docker or podman -> Refuse (no default engine)
//  4. DEVDESK_FIXTURE_RECORDING is "1"               -> Record, which needs an absolute
//     DEVDESK_FIXTURE_OUT, else Refuse
//  5. otherwise                                      -> Verify
//
// The API these tests expect (package internal/testutil/enginecontract):
//
//	const (
//		LabelKey     = "devdesk.test"
//		ToolEnv      = "DEVDESK_ENGINE_CONTRACT_TOOL"
//		RunEnv       = "DEVDESK_ENGINE_CONTRACT_RUN"
//		ImageEnv     = "DEVDESK_ENGINE_CONTRACT_IMAGE"
//		VersionsEnv  = "DEVDESK_ENGINE_VERSIONS"
//		OutEnv       = "DEVDESK_FIXTURE_OUT"
//		RecordingEnv = "DEVDESK_FIXTURE_RECORDING"
//		NamePrefix   = "ddc-"
//	)
//	type Mode string // Verify = "verify", Record = "record"
//	type Decision struct {
//		Skip, Refuse string // at most one is set
//		Mode         Mode
//		Tool         enginefixture.Tool
//		OutDir       string
//	}
//	func Decide(getenv func(string) string) Decision

func env(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(name string) string { return m[name] }
}

func TestTheNamesOfTheContractVariablesAreFixed(t *testing.T) {
	// contract.sh and the docs spell these names; a rename must be deliberate.
	for _, tt := range []struct{ name, got, want string }{
		{"LabelKey", enginecontract.LabelKey, "devdesk.test"},
		{"ToolEnv", enginecontract.ToolEnv, "DEVDESK_ENGINE_CONTRACT_TOOL"},
		{"RunEnv", enginecontract.RunEnv, "DEVDESK_ENGINE_CONTRACT_RUN"},
		{"ImageEnv", enginecontract.ImageEnv, "DEVDESK_ENGINE_CONTRACT_IMAGE"},
		{"VersionsEnv", enginecontract.VersionsEnv, "DEVDESK_ENGINE_VERSIONS"},
		{"OutEnv", enginecontract.OutEnv, "DEVDESK_FIXTURE_OUT"},
		{"RecordingEnv", enginecontract.RecordingEnv, "DEVDESK_FIXTURE_RECORDING"},
		{"NamePrefix", enginecontract.NamePrefix, "ddc-"},
		{"Verify", string(enginecontract.Verify), "verify"},
		{"Record", string(enginecontract.Record), "record"},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestDecide(t *testing.T) {
	const gate, ci, tool, rec, out = "DEVDESK_ENGINE_CONTRACT", "CI", "DEVDESK_ENGINE_CONTRACT_TOOL", "DEVDESK_FIXTURE_RECORDING", "DEVDESK_FIXTURE_OUT"

	tests := []struct {
		name string
		env  func(string) string
		// skip and refuse are substrings the message must contain; both nil
		// means a go-ahead with the mode, tool and output directory below.
		skip, refuse []string
		mode         enginecontract.Mode
		tool         enginefixture.Tool
		outDir       string
	}{
		{
			name: "nothing set: skipped, and the message says how to run the contracts",
			env:  env(),
			skip: []string{"skipped", "DEVDESK_ENGINE_CONTRACT=1", "CI", "mise run engine-contract"},
		},
		{name: "the variable is 0: skipped", env: env(gate, "0"), skip: []string{"skipped"}},
		{name: "the variable is true, not 1: skipped", env: env(gate, "true"), skip: []string{"skipped"}},
		{name: "the variable is empty: skipped", env: env(gate, ""), skip: []string{"skipped"}},
		{name: "CI alone is skipped, not refused: the first rule wins", env: env(ci, "true"), skip: []string{"skipped"}},
		{
			name:   "the variable and CI: refused, and the message names CI",
			env:    env(gate, "1", ci, "true", tool, "docker"),
			refuse: []string{"refused", "CI"},
		},
		{name: "CI set to 1 is refused too", env: env(gate, "1", ci, "1", tool, "podman"), refuse: []string{"refused", "CI"}},
		{
			name:   "the variable, no CI, no engine: refused, the message names the tool variable",
			env:    env(gate, "1", ci, ""),
			refuse: []string{"refused", "DEVDESK_ENGINE_CONTRACT_TOOL"},
		},
		{
			name:   "an engine that is not docker or podman is refused",
			env:    env(gate, "1", tool, "kind"),
			refuse: []string{"refused", "DEVDESK_ENGINE_CONTRACT_TOOL"},
		},
		{name: "podman, verify", env: env(gate, "1", tool, "podman"), mode: enginecontract.Verify, tool: enginefixture.Podman},
		{name: "docker, verify, CI set to empty", env: env(gate, "1", ci, "", tool, "docker"), mode: enginecontract.Verify, tool: enginefixture.Docker},
		{
			name:   "recording without an output directory is refused, and names it",
			env:    env(gate, "1", tool, "podman", rec, "1"),
			refuse: []string{"refused", "DEVDESK_FIXTURE_OUT"},
		},
		{
			name:   "a relative output directory is refused",
			env:    env(gate, "1", tool, "podman", rec, "1", out, "out/dir"),
			refuse: []string{"refused", "DEVDESK_FIXTURE_OUT"},
		},
		{
			name: "recording with an absolute output directory",
			env:  env(gate, "1", tool, "podman", rec, "1", out, "/tmp/ddc-x/out"),
			mode: enginecontract.Record, tool: enginefixture.Podman, outDir: "/tmp/ddc-x/out",
		},
		{
			name: "recording without the gate is skipped before anything else is read",
			env:  env(tool, "podman", rec, "1", out, "/tmp/x"),
			skip: []string{"skipped"},
		},
		{
			name:   "recording in CI is refused",
			env:    env(gate, "1", ci, "true", tool, "podman", rec, "1", out, "/tmp/x"),
			refuse: []string{"refused", "CI"},
		},
		{
			name: "an output directory without the recording switch is ignored: verify",
			env:  env(gate, "1", tool, "docker", out, "/tmp/x"),
			mode: enginecontract.Verify, tool: enginefixture.Docker,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := enginecontract.Decide(tt.env)

			switch {
			case tt.skip != nil:
				if got.Refuse != "" {
					t.Errorf("Decide() refused (%q), want a skip", got.Refuse)
				}
				for _, w := range tt.skip {
					if !strings.Contains(got.Skip, w) {
						t.Errorf("Skip = %q, want it to contain %q", got.Skip, w)
					}
				}
			case tt.refuse != nil:
				if got.Skip != "" {
					t.Errorf("Decide() skipped (%q), want a refusal", got.Skip)
				}
				for _, w := range tt.refuse {
					if !strings.Contains(got.Refuse, w) {
						t.Errorf("Refuse = %q, want it to contain %q", got.Refuse, w)
					}
				}
			default:
				if got.Skip != "" || got.Refuse != "" {
					t.Fatalf("Decide() = skip %q, refuse %q, want a go-ahead", got.Skip, got.Refuse)
				}
				if got.Mode != tt.mode || got.Tool != tt.tool || got.OutDir != tt.outDir {
					t.Errorf("Decide() = mode %q tool %q out %q, want mode %q tool %q out %q",
						got.Mode, got.Tool, got.OutDir, tt.mode, tt.tool, tt.outDir)
				}
			}
			if got.Skip != "" && got.Refuse != "" {
				t.Error("Decide() set both Skip and Refuse: at most one")
			}
		})
	}
}
