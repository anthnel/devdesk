package docker

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-09a3 (DEVDESK-944), step 12: c.exchange joins the recorder and the verifier.
// In a recording session it redacts the one exchange of its window and hands it to
// Session.Record; in a verifying one it hands it to Session.Verify. Nothing here
// reaches an engine: the exchange comes from a fakecli tool, in a fakecli directory
// (G1), and the contract is the one startContract builds.
//
// What the contract passes (the expected shape of c.exchange after this step; its
// signature does not change):
//
//	enginefixture.Redaction{
//		RunID: c.s.RunID,
//		IDs:   the keys of c.ours that are 64 lower-case hex characters, each with its role (c.ours[id]),
//		Home:  os.UserHomeDir(), User: user.Current().Username, Host: os.Hostname() (a failure of one leaves it empty),
//	}
//
// and what it returns is the raw exchange, as before (the contract asserts on real
// ids), never the redacted one. Two decisions of this step, beyond the plan:
//
//   - the role of a container and the role of a network of the same name share one
//     placeholder (ours holds a role, not a kind): a contract gives its roles
//     distinct names, which the "ddc-<run>-<role>" naming already needs;
//   - in a verifying session with no fixture for the scenario the contract goes on
//     (see enginecontract Verify); it is not skipped.

// recordingContract builds a contract in a recording session of shape, in a lab,
// whose HOME is a path the test controls, and returns it with its output directory.
func recordingContract(t *testing.T, shape engine.Shape, fake fakecli.Scenario) (*contract, *contractT, string) {
	t.Helper()
	openLab(t)
	t.Setenv("HOME", "/home/alicetest")
	t.Setenv(enginecontract.SentinelEnv, sentinelID) // a recording demands it (OC-09b1 hardening)
	out := t.TempDir()
	s := sessionOf(shape, labRunID)
	s.Mode = enginecontract.Record
	s.OutDir = out
	fakecli.Install(t, fake, shape.Binary)
	ct := &contractT{TB: t}
	c := startContract(ct, s)
	c.ours[idA] = "c1"
	c.ours[enginecontract.ResourceName(labRunID, "c1")] = "c1"
	return c, ct, out
}

func fixtureFile(t *testing.T, out, campaign, scenario string, shape engine.Shape) (enginefixture.Fixture, []byte) {
	t.Helper()
	name := scenario + "--" + shape.Name + "-6.2.1-rootless.cli"
	data, err := os.ReadFile(filepath.Join(out, campaign, name))
	if err != nil {
		t.Fatalf("the exchange was not recorded: %v", err)
	}
	f, err := enginefixture.Parse(campaign, name, data)
	if err != nil {
		t.Fatalf("what was recorded is not a valid fixture: %v", err)
	}
	return f, data
}

func TestExchangeRecordsTheRedactedExchangeInARecordingSession(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		stdout := `[{"Id":"` + idA + `","Name":"` + enginecontract.ResourceName(labRunID, "c1") + `","Path":"/home/alicetest/.local/share"}]` + "\n"
		c, ct, out := recordingContract(t, shape, fakecli.Scenario{
			{Tool: shape.Binary, Args: []string{"inspect", idA}, Reply: fakecli.Reply{Stdout: stdout}},
		})

		ex := c.exchange("containers", "inspect-one", "inspect of a container of the run", func() { _, _ = dockerOutput("inspect", idA) })
		ct.runCleanups()

		if len(ct.errs)+len(ct.fatals) != 0 {
			t.Fatalf("reported: %q %q", ct.errs, ct.fatals)
		}
		if !strings.Contains(string(ex.Stdout), idA) || !strings.Contains(string(ex.Stdout), "/home/alicetest") {
			t.Errorf("exchange returned = %q, want the raw exchange: the contract asserts on real ids", ex.Stdout)
		}
		f, data := fixtureFile(t, out, "containers", "inspect-one", shape)
		if f.Meta.Redacted != "home, ids, run" {
			t.Errorf("redacted = %q, want %q", f.Meta.Redacted, "home, ids, run")
		}
		if f.Meta.RecordedBy != enginefixture.RecordedByContract || f.Meta.Scenario != "inspect of a container of the run" {
			t.Errorf("meta = %+v", f.Meta)
		}
		want := `[{"Id":"` + enginefixture.PlaceholderID("c1") + `","Name":"ddc-fixture-run-c1","Path":"/home/fixture-user/.local/share"}]` + "\n"
		if string(f.Exchange.Stdout) != want {
			t.Errorf("recorded stdout = %q, want %q", f.Exchange.Stdout, want)
		}
		for _, real := range []string{idA, labRunID, "alicetest"} {
			if strings.Contains(string(data), real) {
				t.Errorf("the file holds %q, which must be redacted", real)
			}
		}
		if leaks := enginefixture.CheckLeaksAs("recorded", data, nil, enginefixture.MachineIdentities()); len(leaks) != 0 {
			t.Errorf("the recorded fixture leaks: %v", leaks)
		}
	})
}

// Each resource of the run gets the placeholder of its own role, a prefix of 12
// characters, as ps prints it, included.
func TestExchangeGivesEachResourceTheTagOfItsRole(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		c, ct, out := recordingContract(t, shape, fakecli.Scenario{
			{Tool: shape.Binary, Args: []string{"ps", "-a"}, Reply: fakecli.Reply{
				Stdout: "CONTAINER ID  NAMES\n" + idA[:12] + "  one\n" + idB[:12] + "  two\n" + idB + "\n",
			}},
		})
		c.ours[idB] = "c2"

		c.exchange("containers", "ps-two", "ps of two containers", func() { _, _ = dockerOutput("ps", "-a") })
		ct.runCleanups()

		f, _ := fixtureFile(t, out, "containers", "ps-two", shape)
		p1, p2 := enginefixture.PlaceholderID("c1"), enginefixture.PlaceholderID("c2")
		want := "CONTAINER ID  NAMES\n" + p1[:12] + "  one\n" + p2[:12] + "  two\n" + p2 + "\n"
		if string(f.Exchange.Stdout) != want {
			t.Errorf("recorded stdout = %q, want %q", f.Exchange.Stdout, want)
		}
	})
}

// The leak check runs before the write, inside Record: a leak ends the contract
// and nothing reaches the output directory.
func TestExchangeInARecordingSessionStopsOnALeak(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		c, ct, out := recordingContract(t, shape, fakecli.Scenario{
			{Tool: shape.Binary, Args: []string{"ps", "-a"}, Reply: fakecli.Reply{Stdout: "host jakku up\n"}},
		})

		ended := ct.run(func() {
			c.exchange("containers", "ps-leak", "ps that leaks", func() { _, _ = dockerOutput("ps", "-a") })
		})
		ct.runCleanups()

		if !ended || len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], "lab-identity") || strings.Contains(ct.fatals[0], "jakku") {
			t.Errorf("ended = %v, fatals = %q, want one Fatal that names lab-identity and not the text", ended, ct.fatals)
		}
		if _, err := os.Stat(filepath.Join(out, "containers")); err == nil {
			entries, _ := os.ReadDir(filepath.Join(out, "containers"))
			if len(entries) != 0 {
				t.Errorf("files written for a leaking exchange: %v", entries)
			}
		}
	})
}

func TestExchangeInARecordingSessionRefusesASecondRecordingOfTheScenario(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		c, ct, out := recordingContract(t, shape, fakecli.Scenario{
			{Tool: shape.Binary, Args: []string{"ps", "-a"}, Reply: fakecli.Reply{Stdout: "x\n"}},
		})
		call := func() { _, _ = dockerOutput("ps", "-a") }
		c.exchange("containers", "ps-one", "first", call)
		_, first := fixtureFile(t, out, "containers", "ps-one", shape)

		ended := ct.run(func() { c.exchange("containers", "ps-one", "second", call) })
		ct.runCleanups()

		if !ended || len(ct.fatals) != 1 {
			t.Errorf("ended = %v, fatals = %q, want a Fatal: two contracts for one scenario", ended, ct.fatals)
		}
		if _, now := fixtureFile(t, out, "containers", "ps-one", shape); string(now) != string(first) {
			t.Errorf("the first recording was changed by the second")
		}
	})
}

// Recording needs the test VM, on top of the gate: with no mark, nothing is
// written, even though the exchange itself was a read.
func TestExchangeInARecordingSessionNeedsTheTestVM(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		c, ct, out := recordingContract(t, shape, fakecli.Scenario{
			{Tool: shape.Binary, Args: []string{"ps", "-a"}, Reply: fakecli.Reply{Stdout: "x\n"}},
		})
		setLab(t, false, false)

		ended := ct.run(func() { c.exchange("containers", "ps-one", "first", func() { _, _ = dockerOutput("ps", "-a") }) })
		ct.runCleanups()

		if !ended || len(ct.fatals) != 1 {
			t.Errorf("ended = %v, fatals = %q, want a Fatal", ended, ct.fatals)
		}
		if entries, _ := os.ReadDir(out); len(entries) != 0 {
			names := []string{}
			for _, e := range entries {
				names = append(names, e.Name())
			}
			t.Errorf("written outside a test VM: %v", names)
		}
	})
}

// The window rule still comes first: no call, or two, is a failure of the
// contract, in a recording session as in a verifying one, and records nothing.
func TestExchangeInARecordingSessionStillWantsExactlyOneCall(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		c, ct, out := recordingContract(t, shape, fakecli.Scenario{
			{Tool: shape.Binary, Args: []string{"ps", "-a"}, Reply: fakecli.Reply{Stdout: "x\n"}},
		})

		none := ct.run(func() { c.exchange("containers", "none", "no call", func() {}) })
		two := ct.run(func() {
			c.exchange("containers", "two", "two calls", func() { _, _ = dockerOutput("ps", "-a"); _, _ = dockerOutput("ps", "-a") })
		})
		ct.runCleanups()

		if !none || !two || len(ct.fatals) != 2 {
			t.Errorf("none ended = %v, two ended = %v, fatals = %q, want two Fatals", none, two, ct.fatals)
		}
		var tree []string
		_ = filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				tree = append(tree, p)
			}
			return nil
		})
		if !slices.Equal(tree, nil) {
			t.Errorf("files written: %v", tree)
		}
	})
}

// A verifying session with no fixture for the scenario goes on: the contract's own
// assertions after the exchange must run (a Skip would end the test first).
func TestExchangeInAVerifyingSessionWithoutAFixtureGoesOn(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		setLab(t, false, false)
		fakecli.Install(t, fakecli.Scenario{
			{Tool: shape.Binary, Args: []string{"ps", "-a"}, Reply: fakecli.Reply{Stdout: "x\n"}},
		}, shape.Binary)
		ct := &contractT{TB: t}
		c := startContract(ct, sessionOf(shape, labRunID))

		reached := false
		ended := ct.run(func() {
			ex := c.exchange("no-such-campaign-recorded", "ps-one", "ps", func() { _, _ = dockerOutput("ps", "-a") })
			reached = string(ex.Stdout) == "x\n"
		})
		ct.runCleanups()

		if ended || !reached || len(ct.errs)+len(ct.fatals) != 0 {
			t.Errorf("ended = %v, reached the contract's own assertions = %v, errs = %q, fatals = %q", ended, reached, ct.errs, ct.fatals)
		}
	})
}
