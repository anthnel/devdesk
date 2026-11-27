package fakecli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-08a criterion 6, the fakecli half: fakecli.Fixtures(t, ...) turns recorded
// fixtures into a Source, so a view test starts the fake engine and gets the
// recorded bytes.
//
//	func Fixtures(t testing.TB, fixtures ...enginefixture.Fixture) Source
//
// The Source holds one exact exchange per fixture (no Prefix), restricted to the
// tool of the fixture, with the recorded reply. The strictness of fakecli applies
// as it is: an argv no fixture holds fails the test with the argv. A selection
// the replay cannot serve (none, a pending file, two fixtures with one key)
// fails the test at once.

func synthetic(t testing.TB) []enginefixture.Fixture {
	t.Helper()
	fixtures, err := enginefixture.LoadFS(os.DirFS(filepath.Join("..", "..", "enginefixture", "testdata")), "synthetic")
	if err != nil {
		t.Fatalf("loading the synthetic fixtures: %v", err)
	}
	return fixtures
}

func scenario(t testing.TB, name string, tool enginefixture.Tool) enginefixture.Fixture {
	t.Helper()
	for _, f := range synthetic(t) {
		if f.Scenario == name && f.Meta.Tool == tool {
			return f
		}
	}
	t.Fatalf("no synthetic fixture %q for %s", name, tool)
	return enginefixture.Fixture{}
}

func TestFixturesHoldOneExactExchangePerFixture(t *testing.T) {
	rm := scenario(t, "rm-running", enginefixture.Docker)

	got := fakecli.Fixtures(t, rm).Exchanges()

	want := []fakecli.Exchange{{
		Tool: "docker", Args: rm.Exchange.Argv, Prefix: false,
		Reply: fakecli.Reply{Stdout: "", Stderr: string(rm.Exchange.Stderr), Exit: 1},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Exchanges() = %+v, want %+v", got, want)
	}
}

func TestFixturesKeepTheExactBytes(t *testing.T) {
	bin := scenario(t, "binary-stdout", enginefixture.Docker)

	got := fakecli.Fixtures(t, bin).Exchanges()

	if len(got) != 1 || got[0].Reply.Stdout != "\xff\x00\r" || got[0].Reply.Stderr != string(bin.Exchange.Stderr) {
		t.Errorf("Exchanges() = %+v, want the raw bytes ff 00 0d on stdout and the recorded stderr", got)
	}
}

// Every configuration of the synthetic set, served by a real process: the code
// under test reads exactly the recorded bytes and exit code.
func TestFixturesAreServedByAFakeProcess(t *testing.T) {
	configs := []struct {
		tool    enginefixture.Tool
		version string
		mode    enginefixture.Mode
	}{
		{enginefixture.Docker, "29.10.1", enginefixture.Rootful},
		{enginefixture.Docker, "28.5.2", enginefixture.Rootful},
		{enginefixture.Podman, "6.2.1", enginefixture.Rootless},
		{enginefixture.Podman, "6.2.1", enginefixture.Rootful},
		{enginefixture.Podman, "5.8.11", enginefixture.Rootful},
	}
	for _, c := range configs {
		t.Run(fmt.Sprintf("%s-%s-%s", c.tool, c.version, c.mode), func(t *testing.T) {
			selected := enginefixture.ForConfig(synthetic(t), c.tool, c.version, c.mode)
			if len(selected) == 0 {
				t.Fatal("test bug: the configuration selects no synthetic fixture")
			}
			fakecli.Install(t, fakecli.Fixtures(t, selected...), string(c.tool))

			for _, f := range selected {
				stdout, stderr, exit := raw(t, string(c.tool), f.Exchange.Argv...)

				if !bytes.Equal(stdout, f.Exchange.Stdout) || !bytes.Equal(stderr, f.Exchange.Stderr) || exit != f.Exchange.Exit {
					t.Errorf("%s: got stdout %q, stderr %q, exit %d; want %q, %q, %d", f.Name,
						clip(stdout), clip(stderr), exit, clip(f.Exchange.Stdout), clip(f.Exchange.Stderr), f.Exchange.Exit)
				}
			}
		})
	}
}

// A fixture is for its own tool: the same argv asked of the other tool is not
// answered, and the test fails with the tool and the argv.
func TestAFixtureAnswersOnlyItsOwnTool(t *testing.T) {
	rm := scenario(t, "rm-running", enginefixture.Docker)
	spy := &spyT{T: t}
	fakecli.Install(spy, fakecli.Fixtures(t, rm), "docker", "podman")

	_, _, exit := raw(t, "podman", rm.Exchange.Argv...)
	spy.finish()

	if exit == 0 {
		t.Error("podman answered a docker fixture")
	}
	report := strings.Join(spy.errs, "\n")
	if !strings.Contains(report, "podman") || !strings.Contains(report, fmt.Sprintf("%q", rm.Exchange.Argv)) {
		t.Errorf("failure report = %q, want the tool and the argv", report)
	}
}

// The strictness of fakecli applies to a fixture source: an argv no fixture holds
// fails the test when it ends, with the argv.
func TestAnArgvNoFixtureHoldsFailsTheTest(t *testing.T) {
	rm := scenario(t, "rm-running", enginefixture.Docker)
	spy := &spyT{T: t}
	fakecli.Install(spy, fakecli.Fixtures(t, rm), "docker")

	_, stderr, exit := raw(t, "docker", "rm", "ddm-other")
	spy.finish()

	if exit == 0 {
		t.Error("an undeclared argv exited 0")
	}
	want := fmt.Sprintf("%q", []string{"rm", "ddm-other"})
	if !strings.Contains(string(stderr), want) {
		t.Errorf("stderr = %q, want the received argv %s", stderr, want)
	}
	if report := strings.Join(spy.errs, "\n"); !strings.Contains(report, want) || !strings.Contains(report, "docker") {
		t.Errorf("failure report = %q, want the tool and the argv %s", report, want)
	}
}

// An unused fixture is not a failure here: a source may be shared by a table of
// cases (the same rule as an unused Exchange, TestAnUnusedExchangeIsNotAFailure).
func TestAnUnaskedFixtureIsNotAFailureOfFakecli(t *testing.T) {
	spy := &spyT{T: t}
	fakecli.Install(spy, fakecli.Fixtures(t, scenario(t, "rm-running", enginefixture.Docker),
		scenario(t, "binary-stdout", enginefixture.Docker)), "docker")

	spy.finish()

	if len(spy.errs) != 0 {
		t.Errorf("an unasked fixture failed the test: %q", spy.errs)
	}
}

// Fixtures is a Source: the Install contract is untouched.
func TestFixturesIsASource(t *testing.T) {
	var src fakecli.Source = fakecli.Fixtures(t, scenario(t, "rm-running", enginefixture.Podman))

	if got := src.Exchanges(); len(got) != 1 || got[0].Tool != "podman" {
		t.Errorf("Exchanges() = %+v, want one podman exchange", got)
	}
}
