package enginecontract_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09a3 (DEVDESK-944), review finding 3 and security findings B2, B3.

func otherScenario() enginefixture.Fixture {
	f := recorded("6.2.1", enginefixture.Rootless, "")
	f.Scenario, f.Name = "other", "other--podman-6.2.1-rootless"
	return f
}

// A campaign that has fixtures for this engine but none for this scenario is a
// misspelled or renamed scenario: the contract would silently compare nothing.
func TestVerifyFailsWhenTheEngineHasFixturesButNotThisScenario(t *testing.T) {
	ex, red := rawInspect()

	rt := verify(t, []enginefixture.Fixture{otherScenario()}, ex, red, nil)

	if len(rt.fatals) != 1 || !strings.Contains(rt.fatals[0], "inspect-absent") || !strings.Contains(rt.fatals[0], "engine-contract:record") {
		t.Errorf("fatals = %q, errs = %q, want one Fatal that names the scenario and the way out", rt.fatals, rt.errs)
	}
}

// Fixtures of another version, another mode, or a skipped one are no fixture for
// this engine: the contract logs and goes on (unchanged).
func TestVerifyLogsWhenOnlyOtherConfigurationsHaveFixtures(t *testing.T) {
	ex, red := rawInspect()
	for name, fixtures := range map[string][]enginefixture.Fixture{
		"another version": {recorded("5.8.11", enginefixture.Rootless, "")},
		"another mode":    {recorded("6.2.1", enginefixture.Rootful, "")},
		"a skipped one":   {recorded("6.2.1", enginefixture.Rootless, "skip: carries a duration")},
	} {
		t.Run(name, func(t *testing.T) {
			rt := verify(t, fixtures, ex, red, nil)
			if !rt.quiet() || !strings.Contains(strings.Join(rt.logs, "\n"), "no recorded fixture") {
				t.Errorf("errs %q, fatals %q, skips %q, logs %q, want only a log", rt.errs, rt.fatals, rt.skips, rt.logs)
			}
		})
	}
}

// With RequireFixtures, no fixture at all is a failure as well.
func TestVerifyRequiringFixturesFailsWithoutOne(t *testing.T) {
	ex, red := rawInspect()
	rt := &recT{TB: t}

	rt.run(func() { verifySession().VerifyAgainstRequiring(rt, nil, "inspect-absent", ex, red, nil) })

	if len(rt.fatals) != 1 || !strings.Contains(rt.fatals[0], "engine-contract:record") {
		t.Errorf("fatals = %q, want one that says how to record", rt.fatals)
	}
	rt = &recT{TB: t}
	rt.run(func() {
		verifySession().VerifyAgainstRequiring(rt, []enginefixture.Fixture{recorded("6.2.1", enginefixture.Rootless, "")}, "inspect-absent", ex, red, nil)
	})
	if !rt.quiet() {
		t.Errorf("a recorded fixture exists and matches, reported: %s", rt.text())
	}
}

// minVersionedFixtures is the floor of internal/enginefixture/stale_test.go, which
// OC-09b raises when it versions the first fixtures. Read as source, since a test
// file of another package cannot be imported: when it is above zero,
// RequireFixtures must be true, so that "no fixture" stops being a log line.
func TestVerifyFailsHardOnceFixturesAreVersioned(t *testing.T) {
	floor := minVersionedFixtures(t)
	if floor > 0 && !enginecontract.RequireFixtures {
		t.Errorf("minVersionedFixtures = %d: fixtures are versioned, so enginecontract.RequireFixtures must be true "+
			"(Verify with no recorded fixture is then a failure, not a log line)", floor)
	}
	if floor == 0 && enginecontract.RequireFixtures {
		t.Errorf("RequireFixtures is true while minVersionedFixtures is 0: nothing can satisfy it")
	}
}

func minVersionedFixtures(t *testing.T) int {
	t.Helper()
	path := filepath.Join("..", "..", "enginefixture", "stale_test.go")
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != "minVersionedFixtures" || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					t.Fatalf("minVersionedFixtures is not an integer literal any more: update this test")
				}
				n, err := strconv.Atoi(lit.Value)
				if err != nil {
					t.Fatalf("minVersionedFixtures = %s: %v", lit.Value, err)
				}
				return n
			}
		}
	}
	t.Fatalf("%s no longer declares minVersionedFixtures: update this test", path)
	return 0
}

// ---- B3: what a report cites. ----

func TestVerifyDoesNotCiteALineThatWouldLeak(t *testing.T) {
	ex, red := rawInspect()
	// The recorded stderr is "Error: no such container <id>"; the engine now says a lab name too.
	ex.Stderr = []byte("Error: no such container " + recID + "\nhost jakku up, see /home/bob/project\n")

	rt := verify(t, []enginefixture.Fixture{recorded("6.2.1", enginefixture.Rootless, "")}, ex, red, nil)

	text := strings.Join(rt.errs, "\n")
	if !strings.Contains(text, "stderr") || !strings.Contains(text, "line 2") {
		t.Fatalf("the difference is not reported with its line:\n%s", text)
	}
	for _, secret := range []string{"jakku", "bob", "/home/"} {
		if strings.Contains(text, secret) {
			t.Errorf("the report cites what the leak check refuses (%q):\n%s", secret, text)
		}
	}
	if !strings.Contains(text, "not shown") || !strings.Contains(text, "lab-identity") {
		t.Errorf("the report does not say why the line is not shown:\n%s", text)
	}
}

func TestVerifyDoesNotCiteAnArgvThatWouldLeak(t *testing.T) {
	ex, red := rawInspect()
	ex.Argv = []string{"inspect", "--format", "{{.X}}", "93.184.216.34"}

	rt := verify(t, []enginefixture.Fixture{recorded("6.2.1", enginefixture.Rootless, "")}, ex, red, nil)

	text := strings.Join(rt.errs, "\n")
	if !strings.Contains(text, "argv") || strings.Contains(text, "93.184.216.34") || !strings.Contains(text, "public-address") {
		t.Errorf("report:\n%s\nwant the argv difference, without the address, naming the rule", text)
	}
}

// ---- B2: ids Redact was not told about. ----

func TestRecordRefusesAnIDNobodyAnnounced(t *testing.T) {
	s, out := recordingSession(t, enginecontract.Record)
	ex, red := rawInspect()
	stranger := recHex("the sentinel of the lab")
	ex.Stdout = []byte("sentinel " + stranger + "\n")

	rt := record(t, s, "containers", "inspect-absent", ex, red)

	if len(rt.fatals) != 1 || !strings.Contains(rt.fatals[0], "unknown-engine-id") || strings.Contains(rt.text(), stranger) {
		t.Errorf("fatals = %q, want one that names unknown-engine-id and not the id", rt.fatals)
	}
	if tree := treeOf(t, out); len(tree) != 0 {
		t.Errorf("something was written: %v", tree)
	}
}

func TestRecordAcceptsADeclaredPublicIDAndThePinnedImage(t *testing.T) {
	digest := recHex("the pinned image")
	image := recHex("an image id")
	s, out := recordingSession(t, enginecontract.Record)
	t.Setenv(enginecontract.ImageEnv, "docker.io/library/alpine@sha256:"+digest)
	ex, red := rawInspect()
	ex.Stdout = []byte("pinned sha256:" + digest + " image " + image + "\n")
	red.Public = []string{image}

	rt := record(t, s, "containers", "inspect-absent", ex, red)

	if !rt.quiet() {
		t.Fatalf("Record reported: %s", rt.text())
	}
	if _, err := os.Stat(filepath.Join(out, "containers", recFile)); err != nil {
		t.Errorf("the fixture was not written: %v", err)
	}
}
