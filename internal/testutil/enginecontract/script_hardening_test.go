package enginecontract_test

import (
	"regexp"
	"strings"
	"testing"
)

// OC-09a2 hardening (review of DEVDESK-943): what contract.sh does about the shape
// of the image and about a sentinel that did not come out as an id. The script is
// never run here; these read its text, as script_sentinel_test.go does.

func TestTheScriptChecksTheShapeOfTheImageBeforeAnyRemoteCall(t *testing.T) {
	lines := scriptCode(t)
	shape := first(lines, regexp.MustCompile(`\[\[ \$\{?CONTRACT_IMAGE(:-)?\}? =~ \^docker\\\.io/library/alpine@sha256:\[0-9a-f\]\{64\}\$ \]\]`), 0)
	if shape < 0 {
		t.Fatal(`contract.sh does not check CONTRACT_IMAGE against ^docker\.io/library/alpine@sha256:[0-9a-f]{64}$`)
	}
	if stmt := statement(lines, shape); !strings.Contains(stmt, "die") && !strings.Contains(statement(lines, shape+1), "die") {
		t.Errorf("contract.sh:%d: a CONTRACT_IMAGE of the wrong shape does not stop the script with die", lines[shape].n)
	}
	for _, re := range []*regexp.Regexp{regexp.MustCompile(`\bgo test\b`), regexp.MustCompile(`\breset\.sh\b`), reWordPull, reSentinelCreate} {
		if i := first(lines, re, 0); i >= 0 && i < shape {
			t.Errorf("contract.sh:%d (%s) runs before the check of the shape of CONTRACT_IMAGE at line %d", lines[i].n, strings.TrimSpace(lines[i].text), lines[shape].n)
		}
	}
}

// A `run -d` that does not print an id may still have made the container: it is
// removed by its name before the script gives up, and the trap is not left with junk.
func TestTheScriptRemovesAHalfCreatedSentinelByNameBeforeGivingUp(t *testing.T) {
	lines := scriptCode(t)
	create := first(lines, reSentinelCreate, 0)
	if create < 0 {
		t.Fatal("contract.sh never creates the sentinel")
	}
	if stmt := statement(lines, create); !regexp.MustCompile(`\|\|\s*sentinel=""`).MatchString(stmt) {
		t.Errorf("contract.sh:%d: a failing creation ends the script on set -e before the removal by name; want `|| sentinel=\"\"`:\n%s", lines[create].n, stmt)
	}
	byName := first(lines, regexp.MustCompile(`sentinel="ddc-sentinel-\$\{?run\}?"`), create+1)
	if byName < 0 {
		t.Fatal("contract.sh does not name the sentinel to remove it after a creation that printed no id")
	}
	rm := first(lines, regexp.MustCompile(`vm_as_mode .*\brm\b.*\$\{?sentinel\}?`), byName)
	dieAt := first(lines, regexp.MustCompile(`\bdie\b.*sentinel`), byName)
	if rm < 0 || dieAt < 0 || rm > dieAt {
		t.Errorf("contract.sh: after naming the sentinel at line %d, the removal is at %d and the die at %d; want the removal first", lines[byName].n, rm, dieAt)
	}
}

// "The engine does not answer" and "the sentinel is gone" are two failures, said
// differently: the second blames the cleanup, the first must not.
func TestTheScriptTellsAnUnreachableEngineFromAMissingSentinel(t *testing.T) {
	lines := scriptCode(t)
	inspect := first(lines, regexp.MustCompile(`\binspect\b.*\$\{?sentinel\}?`), 0)
	gone := first(lines, regexp.MustCompile(`the cleanup touched a container without the label`), 0)
	silent := first(lines, regexp.MustCompile(`did not answer after the contracts`), 0)
	if inspect < 0 || gone < 0 || silent < 0 {
		t.Fatalf("contract.sh lacks a step: inspect %d, the message for a missing sentinel %d, the message for an engine that does not answer %d", inspect, gone, silent)
	}
	if gone == silent {
		t.Error("both failures share one message")
	}
	probe := first(lines, regexp.MustCompile(`vm_as_mode "\$engine" version`), inspect)
	if probe < 0 || probe > gone || probe > silent {
		t.Errorf("contract.sh asks the engine something harmless (version) after the inspect and before either message: probe at %d, messages at %d and %d", probe, gone, silent)
	}
}
