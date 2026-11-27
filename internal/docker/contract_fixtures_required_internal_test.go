package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// OC-09b2 (DEVDESK-807): fixtures are required (enginecontract.RequireFixtures is true), and the one
// relaxation, Session.MissingFixturesOK, is used by labSession only, that is by the tests of the
// helpers, which run scenarios nobody recorded on a fake engine. A real contract gets its session
// from Require (requireEngine), which never sets it. This test keeps the identifier out of every other
// file of the package: a contract that set it would hide a missing fixture behind a green run.
// The names of these tests do not start with "TestContract" (guard G5, R3).

func TestOnlyTheSessionOfTheHelperTestsRelaxesTheFixtures(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no .go file read: %v", err)
	}
	allowed := map[string]bool{"contract_helpers_internal_test.go": true, "contract_fixtures_required_internal_test.go": true}
	seen := false
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "MissingFixturesOK") {
			continue
		}
		seen = seen || file == "contract_helpers_internal_test.go"
		if !allowed[file] {
			t.Errorf("%s names MissingFixturesOK: only labSession (contract_helpers_internal_test.go) may relax the fixtures", file)
		}
	}
	if !seen {
		t.Error("labSession no longer sets MissingFixturesOK: the tests of the helpers would fail on a scenario nobody recorded, or this test is stale")
	}
}

// requireEngine builds its session with Require: a source test, since a contract cannot run here.
func TestRequireEngineTakesItsSessionFromRequire(t *testing.T) {
	data, err := os.ReadFile("contract_support_test.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "enginecontract.Require(t, probeEngine)") {
		t.Error("requireEngine does not take its session from enginecontract.Require")
	}
}
