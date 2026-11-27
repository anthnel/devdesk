package enginecontract_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// OC-09b2 (DEVDESK-807): contract.sh counts the `compared` lines Verify logs and says so in its
// summary, and warns (does not fail) when contracts ran in verify mode and nothing was compared. And
// reset.sh names the line it really restores for.

func TestTheScriptCountsTheScenariosThatWereCompared(t *testing.T) {
	text := codeText(t)

	if !regexp.MustCompile(`grep -c [^\n]*compared [^\n]*"\$out"`).MatchString(text) {
		t.Error("contract.sh does not count the `compared` lines of the output of each test binary")
	}
	if !strings.Contains(text, "scenarios compared") {
		t.Error("contract.sh does not say how many scenarios were compared in its summary")
	}
}

func TestTheScriptWarnsWhenNothingWasCompared(t *testing.T) {
	lines := scriptCode(t)
	warn := first(lines, regexp.MustCompile(`WARNING: .*compared`), 0)
	if warn < 0 {
		t.Fatal("contract.sh has no WARNING for a verification in which nothing was compared")
	}
	cond := ""
	for j := max(warn-3, 0); j <= warn; j++ {
		cond += lines[j].text + "\n"
	}
	for _, want := range []string{"verify", "compared", "passed"} {
		if !strings.Contains(cond, want) {
			t.Errorf("the warning at line %d is not conditioned on %q:\n%s", lines[warn].n, want, cond)
		}
	}
	if regexp.MustCompile(`\bdie\b|status=1`).MatchString(lines[warn].text) {
		t.Errorf("contract.sh:%d: the warning ends the run, it must only warn:\n%s", lines[warn].n, lines[warn].text)
	}
}

func TestTheSummaryLineKeepsTheCountsAndAddsTheComparison(t *testing.T) {
	text := codeText(t)

	if !regexp.MustCompile(`\$passed passed, \$skipped skipped, \$failed failed`).MatchString(text) {
		t.Error("the summary lost its passed, skipped and failed counts")
	}
}

func TestResetNamesTheLineItRestoresFor(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "engine-vms", "reset.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)

	if strings.Contains(text, "restoring clean (line n)") {
		t.Error(`reset.sh says "restoring clean (line n)" whatever line it is asked for`)
	}
	if !strings.Contains(text, `restoring clean (line $version)`) {
		t.Error(`reset.sh does not say "restoring clean (line $version)"`)
	}
}
