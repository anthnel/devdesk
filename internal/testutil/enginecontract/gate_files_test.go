package enginecontract_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// OC-09a1, criterion 5: `mise run ci` and the GitHub CI run no contract. The
// variable that opens the gate is exported by hand, in a local shell, never by
// a task or a workflow, or the gate would stand open on every run.
//
// The two guards here read mise.toml and the workflows as text. A line that names
// the variable is accepted only when it cannot set it: a comment, or (in
// mise.toml) the `description` of a task that tells the user to export it.
// Anything else (an `env` entry, a `run` line, a workflow `env:` or `run:`) is
// reported with its line number.
//
// Limits, stated here and not hidden: a name assembled at run time
// ("DEVDESK_ENGINE_" + "CONTRACT"), a value reached through an include or a
// reusable workflow in another repository, and a script that mise.toml or a
// workflow calls (tools/engine-vms/contract.sh passes the variable to the VM on
// purpose; it applies the gate itself, see script_test.go) are not seen.

const gateVariable = "DEVDESK_ENGINE_CONTRACT"

// gateOpeners reports the (1-based) lines of text that name the gate variable
// where it could be set. mise is true for mise.toml, where a `description` key
// may name it.
func gateOpeners(text string, mise bool) []int {
	var lines []int
	for i, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, gateVariable) {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if mise && strings.HasPrefix(trimmed, "description") {
			continue
		}
		lines = append(lines, i+1)
	}
	return lines
}

func TestNothingOpensTheContractGate(t *testing.T) {
	root := repoRoot(t)

	mise, err := os.ReadFile(filepath.Join(root, "mise.toml"))
	if err != nil || !strings.Contains(string(mise), "[tasks.ci]") {
		t.Fatalf("mise.toml is missing or is not the one of the project (the guard would pass on nothing): %v", err)
	}
	for _, line := range gateOpeners(string(mise), true) {
		t.Errorf("mise.toml:%d names %s outside a comment or a description: a task must never open the contract gate, "+
			"the user exports it (OC-09a, T4)", line, gateVariable)
	}

	workflows, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.y*ml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) == 0 {
		t.Fatal("no workflow found: the guard would pass on nothing")
	}
	for _, w := range workflows {
		data, err := os.ReadFile(w)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range gateOpeners(string(data), false) {
			t.Errorf("%s:%d names %s: a workflow must never open the contract gate (OC-09a, T4)",
				filepath.ToSlash(strings.TrimPrefix(w, root+string(filepath.Separator))), line, gateVariable)
		}
	}
}

func TestGateOpenersOnSources(t *testing.T) {
	tests := []struct {
		name string
		text string
		mise bool
		want []int
	}{
		{"an env table", "[tasks.test]\nenv = { DEVDESK_ENGINE_CONTRACT = \"1\" }\n", true, []int{2}},
		{"a run line", "[tasks.test]\nrun = \"DEVDESK_ENGINE_CONTRACT=1 go test ./...\"\n", true, []int{2}},
		{"a line of a multi-line run", "[tasks.x]\nrun = '''\nexport DEVDESK_ENGINE_CONTRACT=1\ngo test\n'''\n", true, []int{3}},
		{"the tool variable counts too", "[tasks.x]\nenv = { DEVDESK_ENGINE_CONTRACT_TOOL = \"docker\" }\n", true, []int{2}},
		{"a global env", "[env]\nDEVDESK_ENGINE_CONTRACT = \"1\"\n", true, []int{2}},
		{"a description that tells the user to export it", "[tasks.\"engine-contract\"]\ndescription = \"needs DEVDESK_ENGINE_CONTRACT=1 in your shell\"\n", true, nil},
		{"a comment", "# DEVDESK_ENGINE_CONTRACT=1 is exported by hand\n[tasks.ci]\n", true, nil},
		{"a workflow env", "jobs:\n  test:\n    env:\n      DEVDESK_ENGINE_CONTRACT: \"1\"\n", false, []int{4}},
		{"a workflow run", "    steps:\n      - run: DEVDESK_ENGINE_CONTRACT=1 go test ./...\n", false, []int{2}},
		{"a workflow description key is not an exemption", "      description: DEVDESK_ENGINE_CONTRACT\n", false, []int{1}},
		{"a workflow comment", "      # never DEVDESK_ENGINE_CONTRACT here\n", false, nil},
		{"nothing", "[tasks.ci]\nrun = \"go test ./...\"\n", true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gateOpeners(tt.text, tt.mise); !slices.Equal(got, tt.want) {
				t.Errorf("gateOpeners() = %v, want %v", got, tt.want)
			}
		})
	}
}
