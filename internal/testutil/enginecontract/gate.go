package enginecontract

import (
	"fmt"
	"path/filepath"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/engineguard"
)

// The environment of a contract run. contract.sh sets them in the test VM; the
// names are spelled in tools/engine-vms and in the docs, so a rename is deliberate.
const (
	// LabelKey is the label every resource a contract creates carries, with the run id as value.
	LabelKey = "devdesk.test"
	// ToolEnv names the engine the run is about, docker or podman. There is no default.
	ToolEnv = "DEVDESK_ENGINE_CONTRACT_TOOL"
	// RunEnv is the run id contract.sh generated; without it one id is made per process.
	RunEnv = "DEVDESK_ENGINE_CONTRACT_RUN"
	// ImageEnv is the image of the containers a contract starts (OC-09a2).
	ImageEnv = "DEVDESK_ENGINE_CONTRACT_IMAGE"
	// SentinelEnv is the id (64 hexadecimal characters) of the container with no label
	// that contract.sh starts before the contracts (OC-09b1): the one foreign resource
	// a listing shows, which a recording redacts to a stable role.
	SentinelEnv = "DEVDESK_ENGINE_CONTRACT_SENTINEL"
	// VersionsEnv is the path of versions.env, as it is in the test VM.
	VersionsEnv = "DEVDESK_ENGINE_VERSIONS"
	// OutEnv is the directory a recording run writes to (OC-09a3).
	OutEnv = "DEVDESK_FIXTURE_OUT"
	// RecordingEnv switches a run to the recording mode.
	RecordingEnv = "DEVDESK_FIXTURE_RECORDING"
	// NamePrefix starts the name of every resource a contract creates.
	NamePrefix = "ddc-"
)

// Mode is what a contract run does with the exchanges it makes.
type Mode string

const (
	// Verify compares the exchanges to the recorded fixtures.
	Verify Mode = "verify"
	// Record writes the exchanges, redacted, to the output directory.
	Record Mode = "record"
)

// Decision is what the gate decided. At most one of Skip and Refuse is set; when
// both are empty the contract may run, in Mode, against Tool.
type Decision struct {
	Skip, Refuse string
	Mode         Mode
	Tool         enginefixture.Tool
	OutDir       string
}

// Decide applies the gate to an environment. The first rule that applies wins:
//
//  1. DEVDESK_ENGINE_CONTRACT is not "1": skip, so a plain `go test` ignores
//     every contract, with a message that says how to run them.
//  2. CI, GITHUB_ACTIONS, GITLAB_CI, BUILDKITE or TF_BUILD is not empty: refuse, even with the
//     variable. A contract never runs in CI.
//  3. DEVDESK_ENGINE_CONTRACT_TOOL is neither docker nor podman: refuse; nothing is assumed.
//  4. DEVDESK_FIXTURE_RECORDING is "1": the recording mode, which needs an
//     absolute DEVDESK_FIXTURE_OUT, else refuse.
//  5. Otherwise the verify mode.
func Decide(getenv func(string) string) Decision {
	if getenv(engineguard.ContractEnv) != "1" {
		return Decision{Skip: "contract test skipped: it reaches a real container engine; set " +
			engineguard.ContractEnv + "=1 outside CI (mise run engine-contract runs the contracts in the test VMs); " +
			"see docs/architecture/network.md, \"Engine contracts\""}
	}
	if name, value := engineguard.CIMarker(getenv); name != "" {
		return Decision{Refuse: fmt.Sprintf("contract test refused: CI is set (%s=%q); "+
			"contract tests never run in CI, even with %s=1 (OC-09a)", name, value, engineguard.ContractEnv)}
	}
	tool := enginefixture.Tool(getenv(ToolEnv))
	if tool != enginefixture.Docker && tool != enginefixture.Podman {
		return Decision{Refuse: fmt.Sprintf("contract test refused: %s must be docker or podman, it is %q; "+
			"no engine is assumed (mise run engine-contract sets it in the test VM)", ToolEnv, string(tool))}
	}
	if getenv(RecordingEnv) == "1" {
		out := getenv(OutEnv)
		if !filepath.IsAbs(out) {
			return Decision{Refuse: fmt.Sprintf("contract test refused: %s=1 needs %s, an absolute path, it is %q",
				RecordingEnv, OutEnv, out)}
		}
		return Decision{Mode: Record, Tool: tool, OutDir: out}
	}
	return Decision{Mode: Verify, Tool: tool}
}
