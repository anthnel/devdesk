package enginecontract_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09b1 (DEVDESK-807): the id of the sentinel container reaches the contracts. The sentinel
// of contract.sh is a container with no label that lives in the store the contracts use, so
// a `ps` they record lists it; its id changes at every run, and the recorder redacts it to one
// stable role. The script passes the id in DEVDESK_ENGINE_CONTRACT_SENTINEL, the name of
// which is pinned here because the script and the docs spell it.

func TestTheNameOfTheSentinelVariableIsFixed(t *testing.T) {
	if got, want := enginecontract.SentinelEnv, "DEVDESK_ENGINE_CONTRACT_SENTINEL"; got != want {
		t.Errorf("SentinelEnv = %q, want %q", got, want)
	}
}

func TestTheScriptGivesTheContractsTheIdOfTheSentinel(t *testing.T) {
	lines := scriptCode(t)
	create := first(lines, reSentinelCreate, 0)
	vars := first(lines, regexp.MustCompile(`^\s*vars=\(`), 0)
	set := first(lines, regexp.MustCompile(`"DEVDESK_ENGINE_CONTRACT_SENTINEL=\$\{?sentinel\}?"`), 0)
	run := first(lines, reContractsRun, 0)
	if create < 0 || vars < 0 || set < 0 || run < 0 {
		t.Fatalf("contract.sh lacks a step: sentinel %d, vars %d, DEVDESK_ENGINE_CONTRACT_SENTINEL=$sentinel %d, contracts %d (-1: none)", create, vars, set, run)
	}
	if create >= vars || vars >= set || set >= run {
		t.Errorf("order: sentinel at line %d, vars at %d, the variable at %d, contracts at %d; want the variable in the explicit environment, after the sentinel exists and before the contracts",
			lines[create].n, lines[vars].n, lines[set].n, lines[run].n)
	}
	// It is in the shared environment, so a verification and a recording both redact it.
	end := vars
	for ; end < len(lines) && strings.TrimSpace(lines[end].text) != ")"; end++ {
	}
	if set > end {
		t.Errorf("contract.sh:%d: the variable is outside the vars list that ends at line %d", lines[set].n, lines[end].n)
	}
}

// The id is a full id: 64 lower-case hexadecimal characters, checked in the script before the
// contracts get it, with a stop that cleans up (die runs the exit trap, which removes the sentinel).
func TestTheScriptChecksTheSentinelIdIsAFullIdBeforeHandingItOver(t *testing.T) {
	lines := scriptCode(t)
	check := first(lines, regexp.MustCompile(`\$sentinel =~ \^\[0-9a-f\]\{64\}\$`), 0)
	set := first(lines, regexp.MustCompile(`"DEVDESK_ENGINE_CONTRACT_SENTINEL=`), 0)
	if check < 0 || set < 0 {
		t.Fatalf("contract.sh lacks a step: the check of a 64 hexadecimal id at %d, the variable at %d (-1: none)", check, set)
	}
	if check > set {
		t.Errorf("the check of the sentinel id is at line %d, after the variable at line %d", lines[check].n, lines[set].n)
	}
	if stmt := statement(lines, check); !strings.Contains(stmt, "die") {
		t.Errorf("contract.sh:%d: a sentinel id that is not 64 hexadecimal characters does not stop the script with die:\n%s", lines[check].n, stmt)
	}
}

// Only the verifying and the recording environments carry it, never a recording variable.
func TestTheSentinelVariableIsNotARecordingVariable(t *testing.T) {
	for _, l := range scriptCode(t) {
		if strings.Contains(l.text, "DEVDESK_ENGINE_CONTRACT_SENTINEL") && strings.Contains(l.text, "DEVDESK_FIXTURE") {
			t.Errorf("contract.sh:%d mixes the sentinel variable with a recording variable: %s", l.n, strings.TrimSpace(l.text))
		}
	}
}
