package enginecontract_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09a3 (DEVDESK-944), step 12: Session.Record writes what a contract saw, once
// redacted, as a fixture; Session.Verify compares what a contract sees to the
// fixture recorded for its engine. Neither reaches an engine: they take the
// exchange the contract made.
//
// The API these tests pin down (package enginecontract, file record.go):
//
//	type Comparer func(recorded, got enginefixture.Exchange) error // nil error = they agree; a nil Comparer = exact
//	func (s *Session) Record(t testing.TB, campaign, scenario, text string, ex enginefixture.Exchange, r enginefixture.Redaction)
//	func (s *Session) Verify(t testing.TB, campaign, scenario string, ex enginefixture.Exchange, r enginefixture.Redaction, cmp Comparer)
//	func (s *Session) VerifyAgainst(t testing.TB, fixtures []enginefixture.Fixture, scenario string,
//		ex enginefixture.Exchange, r enginefixture.Redaction, cmp Comparer) // Verify, over the fixtures of one campaign it was given
//
// VerifyAgainst is the seam of the tests: Verify is Load(campaign) then
// VerifyAgainst, and the embedded set holds no real fixture before OC-09b.
//
// Record, in order: the session must be a recording one (Mode Record, an absolute
// OutDir) and in a test VM (RequireLab: the run id variable and /etc/dd/current),
// else t.Fatal; campaign and scenario are checked (a campaign matches
// ^[a-z0-9-]+$, a scenario ^[a-z0-9-]+$ without "--"); Redact; a Fixture with
// Meta{Tool, Version, Mode of the engine, Recorded: today in UTC, Scenario: text,
// Redacted: the header Redact returned, RecordedBy: RecordedByContract}; Format;
// CheckLeaksAs(name, data, nil, MachineIdentities()); a leak is t.Fatal naming the
// rules and never the text, and nothing is written; then
// <OutDir>/<campaign>/<scenario>--<tool>-<version>-<mode>.cli, written without
// following a link: a link as OutDir, as campaign directory or as the file is a
// Fatal, and so is a file already there (two contracts for one scenario).
//
// Verify, decision of this step beyond the plan: when there is no fixture of the
// scenario for the engine (none recorded, another version, a skipped one) it does
// NOT skip the test, it logs a line that says no fixture was recorded and names
// `mise run engine-contract:record`, and returns. A Skip ends the test (runtime.Goexit),
// so the assertions of the contract after c.exchange would never run on a checkout
// without fixtures, which is every checkout before OC-09b. What exists is compared
// to Redact(got): argv and exit code exactly, the streams through cmp (exact when
// nil); a difference is t.Errorf with the lengths and the first line that differs
// (redacted text, so printable).

const recRun = "20261127-a1b2c3d4"

func recHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

var recID = recHex("a container of the contract")

// recEnd is what a captured Fatal or Skip panics with.
type recEnd struct{}

// recT stands for the testing.TB a contract holds: it records every report instead
// of failing the real test, and a Fatal or a Skip ends the function it runs in.
type recT struct {
	testing.TB
	errs, fatals, skips, logs []string
}

func (r *recT) Helper() {}
func (r *recT) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}
func (r *recT) Error(args ...any) { r.errs = append(r.errs, fmt.Sprint(args...)) }
func (r *recT) Fatalf(format string, args ...any) {
	r.fatals = append(r.fatals, fmt.Sprintf(format, args...))
	panic(recEnd{})
}
func (r *recT) Fatal(args ...any) { r.fatals = append(r.fatals, fmt.Sprint(args...)); panic(recEnd{}) }
func (r *recT) FailNow()          { r.fatals = append(r.fatals, ""); panic(recEnd{}) }
func (r *recT) Fail()             { r.errs = append(r.errs, "") }
func (r *recT) Failed() bool      { return len(r.errs)+len(r.fatals) > 0 }
func (r *recT) Skipf(format string, args ...any) {
	r.skips = append(r.skips, fmt.Sprintf(format, args...))
	panic(recEnd{})
}
func (r *recT) Skip(args ...any) { r.skips = append(r.skips, fmt.Sprint(args...)); panic(recEnd{}) }
func (r *recT) SkipNow()         { r.skips = append(r.skips, ""); panic(recEnd{}) }
func (r *recT) Log(args ...any)  { r.logs = append(r.logs, fmt.Sprint(args...)) }
func (r *recT) Logf(format string, args ...any) {
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}
func (r *recT) Cleanup(f func()) { r.TB.Cleanup(f) }
func (r *recT) quiet() bool      { return len(r.errs)+len(r.fatals)+len(r.skips) == 0 }
func (r *recT) text() string {
	return strings.Join(slices.Concat(r.errs, r.fatals, r.skips, r.logs), "\n")
}
func (r *recT) run(f func()) {
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(recEnd); !ok {
				panic(p)
			}
		}
	}()
	f()
}

// labSession is a recording session in a "test VM": the run id variable set and a
// marker file that says "n".
func recordingSession(t *testing.T, mode enginecontract.Mode) (*enginecontract.Session, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "current")
	if err := os.WriteFile(marker, []byte("n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	enginecontract.SetLabMarkerForTest(t, marker)
	t.Setenv(enginecontract.RunEnv, recRun)
	out := t.TempDir()
	return &enginecontract.Session{Engine: podmanN, RunID: recRun, Mode: mode, OutDir: out}, out
}

func rawInspect() (enginefixture.Exchange, enginefixture.Redaction) {
	return enginefixture.Exchange{
		Argv:   []string{"inspect", recID},
		Exit:   2,
		Stdout: []byte("path /home/alice/.local/x\n"),
		Stderr: []byte("Error: no such container " + recID + "\n"),
	}, enginefixture.Redaction{
		RunID: recRun, IDs: map[string]string{recID: "c1"}, Home: "/home/alice", User: "alice", Host: "alicebox",
	}
}

const recFile = "inspect-absent--podman-6.2.1-rootless.cli"

// treeOf lists the files and directories under root, slash separated.
func treeOf(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != root {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// record runs Record under a captured T and returns what it reported.
func record(t *testing.T, s *enginecontract.Session, campaign, scenario string, ex enginefixture.Exchange, r enginefixture.Redaction) *recT {
	t.Helper()
	rt := &recT{TB: t}
	rt.run(func() { s.Record(rt, campaign, scenario, "inspect of a container that is gone", ex, r) })
	return rt
}

func TestRecordWritesTheRedactedFixtureOfTheScenario(t *testing.T) {
	s, out := recordingSession(t, enginecontract.Record)
	ex, red := rawInspect()
	before := time.Now().UTC().Format(time.DateOnly)

	rt := record(t, s, "containers", "inspect-absent", ex, red)

	after := time.Now().UTC().Format(time.DateOnly)
	if !rt.quiet() {
		t.Fatalf("Record reported: %s", rt.text())
	}
	path := filepath.Join(out, "containers", recFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the fixture was not written where the importer looks for it: %v", err)
	}
	f, err := enginefixture.Parse("containers", recFile, data)
	if err != nil {
		t.Fatalf("what Record wrote is not a valid fixture: %v", err)
	}
	m := f.Meta
	if m.Tool != enginefixture.Podman || m.Version != "6.2.1" || m.Mode != enginefixture.Rootless {
		t.Errorf("configuration = %s %s %s, want that of the engine of the session: podman 6.2.1 rootless", m.Tool, m.Version, m.Mode)
	}
	if m.Scenario != "inspect of a container that is gone" || m.RecordedBy != enginefixture.RecordedByContract || m.Record != "" {
		t.Errorf("meta = %+v, want the text as scenario, recorded-by %s and no record header", m, enginefixture.RecordedByContract)
	}
	if m.Recorded != before && m.Recorded != after {
		t.Errorf("recorded = %q, want today in UTC (%s)", m.Recorded, after)
	}
	if m.Redacted != "home, ids" {
		t.Errorf("redacted = %q, want %q: the header Redact returned", m.Redacted, "home, ids")
	}
	wantID := enginefixture.PlaceholderID("c1")
	if len(f.Exchange.Argv) != 2 || f.Exchange.Argv[0] != "inspect" || f.Exchange.Argv[1] != wantID {
		t.Errorf("argv = %q, want [inspect %s]", f.Exchange.Argv, wantID)
	}
	if f.Exchange.Exit != 2 || string(f.Exchange.Stdout) != "path /home/fixture-user/.local/x\n" ||
		string(f.Exchange.Stderr) != "Error: no such container "+wantID+"\n" {
		t.Errorf("exchange = %+v, want the redacted one", f.Exchange)
	}
	for _, real := range []string{recID, "alice", "alicebox"} {
		if strings.Contains(string(data), real) {
			t.Errorf("the file holds %q, which Redact had to replace", real)
		}
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm()&0o022 != 0 {
		t.Errorf("mode of the fixture = %v (%v), want it not writable by group or others", st.Mode(), err)
	}
	if leaks := enginefixture.CheckLeaksAs("written", data, nil, enginefixture.MachineIdentities()); len(leaks) != 0 {
		t.Errorf("what was written leaks: %v", leaks)
	}
}

func TestRecordRefusesTheSameScenarioTwice(t *testing.T) {
	s, out := recordingSession(t, enginecontract.Record)
	ex, red := rawInspect()
	if rt := record(t, s, "containers", "inspect-absent", ex, red); !rt.quiet() {
		t.Fatalf("first Record reported: %s", rt.text())
	}
	path := filepath.Join(out, "containers", recFile)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	other := ex
	other.Stdout = []byte("something else\n")

	rt := record(t, s, "containers", "inspect-absent", other, red)

	if len(rt.fatals) != 1 || !strings.Contains(rt.fatals[0], "inspect-absent") {
		t.Errorf("fatals = %q, want one that names the scenario: two contracts for one scenario", rt.fatals)
	}
	if now, err := os.ReadFile(path); err != nil || string(now) != string(first) {
		t.Errorf("the first fixture was changed by the refused second one (%v)", err)
	}
}

// A leak is the net under Redact: the recording ends, naming the rule, and
// nothing is written.
func TestRecordRefusesALeakAndWritesNothing(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		red    func(r enginefixture.Redaction) enginefixture.Redaction
		rule   string
		secret string
	}{
		{"a lab term Redact does not know", "host jakku up\n", nil, "lab-identity", "jakku"},
		{"a personal path of a home Redact was not given", "path /home/bob/project\n",
			func(r enginefixture.Redaction) enginefixture.Redaction { r.Home = ""; return r }, "personal-path", "bob"},
		{"the user dd, too short to be replaced", "uid=1000(dd)\n",
			func(r enginefixture.Redaction) enginefixture.Redaction { r.User = "dd"; return r }, "lab-identity", "uid=1000(dd)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, out := recordingSession(t, enginecontract.Record)
			ex, red := rawInspect()
			ex.Stdout = []byte(tt.stdout)
			if tt.red != nil {
				red = tt.red(red)
			}

			rt := record(t, s, "containers", "inspect-absent", ex, red)

			if len(rt.fatals) != 1 {
				t.Fatalf("fatals = %q, errs = %q, want exactly one Fatal", rt.fatals, rt.errs)
			}
			if !strings.Contains(rt.fatals[0], tt.rule) {
				t.Errorf("the Fatal does not name the rule %q: %s", tt.rule, rt.fatals[0])
			}
			if strings.Contains(rt.text(), tt.secret) {
				t.Errorf("a report repeats what was found (%q): %s", tt.secret, rt.text())
			}
			if tree := treeOf(t, out); slices.ContainsFunc(tree, func(p string) bool { return strings.HasSuffix(p, ".cli") }) {
				t.Errorf("a fixture was written although it leaks: %v", tree)
			}
		})
	}
}

func TestRecordRefusesNamesThatCouldLeaveTheOutputDirectory(t *testing.T) {
	tests := []struct{ name, campaign, scenario string }{
		{"a campaign that climbs", "../x", "ok"},
		{"a campaign with a slash", "a/b", "ok"},
		{"an empty campaign", "", "ok"},
		{"an upper-case campaign", "Containers", "ok"},
		{"a campaign with a space", "x y", "ok"},
		{"an empty scenario", "containers", ""},
		{"a scenario with a slash", "containers", "a/b"},
		{"a scenario that climbs", "containers", "../x"},
		{"a scenario with the separator of the file name", "containers", "a--b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, out := recordingSession(t, enginecontract.Record)
			ex, red := rawInspect()

			rt := record(t, s, tt.campaign, tt.scenario, ex, red)

			if len(rt.fatals) != 1 {
				t.Errorf("fatals = %q, want one", rt.fatals)
			}
			if tree := treeOf(t, out); len(tree) != 0 {
				t.Errorf("something was written: %v", tree)
			}
			if _, err := os.Lstat(filepath.Join(out, "..", "x")); err == nil {
				t.Errorf("a path outside the output directory was created")
			}
		})
	}
}

func TestRecordDoesNotFollowALink(t *testing.T) {
	linked := func(t *testing.T, link, target string) {
		t.Helper()
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("no symbolic links here: %v", err)
		}
	}
	t.Run("the output directory is a link", func(t *testing.T) {
		s, _ := recordingSession(t, enginecontract.Record)
		real := t.TempDir()
		s.OutDir = filepath.Join(t.TempDir(), "out")
		linked(t, s.OutDir, real)
		ex, red := rawInspect()

		rt := record(t, s, "containers", "inspect-absent", ex, red)

		if len(rt.fatals) != 1 || len(treeOf(t, real)) != 0 {
			t.Errorf("fatals = %q, files behind the link = %v, want a Fatal and nothing written", rt.fatals, treeOf(t, real))
		}
	})
	t.Run("the campaign directory is a link", func(t *testing.T) {
		s, out := recordingSession(t, enginecontract.Record)
		real := t.TempDir()
		linked(t, filepath.Join(out, "containers"), real)
		ex, red := rawInspect()

		rt := record(t, s, "containers", "inspect-absent", ex, red)

		if len(rt.fatals) != 1 || len(treeOf(t, real)) != 0 {
			t.Errorf("fatals = %q, files behind the link = %v, want a Fatal and nothing written", rt.fatals, treeOf(t, real))
		}
	})
	t.Run("the file is a link to somewhere that does not exist yet", func(t *testing.T) {
		s, out := recordingSession(t, enginecontract.Record)
		if err := os.MkdirAll(filepath.Join(out, "containers"), 0o750); err != nil {
			t.Fatal(err)
		}
		beyond := filepath.Join(t.TempDir(), "created-through-the-link")
		linked(t, filepath.Join(out, "containers", recFile), beyond)
		ex, red := rawInspect()

		rt := record(t, s, "containers", "inspect-absent", ex, red)

		if len(rt.fatals) != 1 {
			t.Errorf("fatals = %q, want one", rt.fatals)
		}
		if _, err := os.Lstat(beyond); err == nil {
			t.Errorf("Record wrote through the link")
		}
	})
}

// Recording needs the mark of a test VM on top of the gate (plan: "le mode
// enregistrement exige aussi le marqueur de VM d'OC-09a2"): a developer who set
// the recording variables by hand on his own machine writes nothing.
func TestRecordNeedsATestVM(t *testing.T) {
	tests := []struct {
		name string
		set  func(t *testing.T)
	}{
		{"no run id variable", func(t *testing.T) { t.Setenv(enginecontract.RunEnv, "") }},
		{"no marker file", func(t *testing.T) { enginecontract.SetLabMarkerForTest(t, filepath.Join(t.TempDir(), "absent")) }},
		{"a marker that is not a line of the lab", func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "current")
			if err := os.WriteFile(p, []byte("whatever\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			enginecontract.SetLabMarkerForTest(t, p)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, out := recordingSession(t, enginecontract.Record)
			tt.set(t)
			ex, red := rawInspect()

			rt := record(t, s, "containers", "inspect-absent", ex, red)

			if len(rt.fatals) != 1 || !strings.Contains(rt.fatals[0], "test VM") {
				t.Errorf("fatals = %q, want one that says a test VM is needed", rt.fatals)
			}
			if tree := treeOf(t, out); len(tree) != 0 {
				t.Errorf("something was written outside a test VM: %v", tree)
			}
		})
	}
}

func TestRecordNeedsARecordingSession(t *testing.T) {
	t.Run("a verifying session", func(t *testing.T) {
		s, out := recordingSession(t, enginecontract.Verify)
		ex, red := rawInspect()

		rt := record(t, s, "containers", "inspect-absent", ex, red)

		if len(rt.fatals) != 1 || len(treeOf(t, out)) != 0 {
			t.Errorf("fatals = %q, tree = %v, want a Fatal and nothing written", rt.fatals, treeOf(t, out))
		}
	})
	t.Run("no output directory", func(t *testing.T) {
		s, _ := recordingSession(t, enginecontract.Record)
		s.OutDir = ""
		ex, red := rawInspect()

		rt := record(t, s, "containers", "inspect-absent", ex, red)

		if len(rt.fatals) != 1 {
			t.Errorf("fatals = %q, want one: Record would write into the working directory", rt.fatals)
		}
	})
}

// ---- Verify. ----

// recorded is the fixture a recording of rawInspect produced.
func recorded(version string, mode enginefixture.Mode, record string) enginefixture.Fixture {
	id := enginefixture.PlaceholderID("c1")
	return enginefixture.Fixture{
		Campaign: "containers", Name: "inspect-absent--podman-" + version + "-" + string(mode), Scenario: "inspect-absent",
		Meta: enginefixture.Meta{
			Tool: enginefixture.Podman, Version: version, Mode: mode, Recorded: "2026-11-27",
			Scenario: "inspect of a container that is gone", Redacted: "home, ids", Record: record,
			RecordedBy: enginefixture.RecordedByContract,
		},
		Exchange: enginefixture.Exchange{
			Argv: []string{"inspect", id}, Exit: 2,
			Stdout: []byte("path /home/fixture-user/.local/x\n"), Stderr: []byte("Error: no such container " + id + "\n"),
		},
	}
}

// verifySession is a session that has no lab mark: Verify reads, it creates nothing.
func verifySession() *enginecontract.Session {
	return &enginecontract.Session{Engine: podmanN, RunID: recRun, Mode: enginecontract.Verify}
}

func verify(t *testing.T, fixtures []enginefixture.Fixture, ex enginefixture.Exchange, r enginefixture.Redaction, cmp enginecontract.Comparer) *recT {
	t.Helper()
	rt := &recT{TB: t}
	rt.run(func() { verifySession().VerifyAgainst(rt, fixtures, "inspect-absent", ex, r, cmp) })
	return rt
}

func TestVerifyComparesTheRedactedExchangeToTheFixture(t *testing.T) {
	fixtures := []enginefixture.Fixture{
		recorded("6.2.1", enginefixture.Rootful, ""), // another mode
		recorded("5.8.11", enginefixture.Rootless, ""),
		recorded("6.2.1", enginefixture.Rootless, ""),
	}
	ex, red := rawInspect()

	rt := verify(t, fixtures, ex, red, nil)

	if !rt.quiet() {
		t.Errorf("an exchange equal to the recorded one, once redacted, was reported: %s", rt.text())
	}
	if len(rt.logs) != 0 && strings.Contains(strings.ToLower(rt.text()), "no recorded fixture") {
		t.Errorf("the fixture exists, and Verify says it does not: %s", rt.text())
	}
}

func TestVerifyReportsEveryKindOfDifference(t *testing.T) {
	tests := []struct {
		name   string
		change func(ex *enginefixture.Exchange)
		cmp    enginecontract.Comparer
		want   []string // words of the report; none = no report
	}{
		{"a different exit code", func(ex *enginefixture.Exchange) { ex.Exit = 125 }, nil, []string{"exit"}},
		{"a different argv, even with a comparer that accepts anything", func(ex *enginefixture.Exchange) { ex.Argv = []string{"inspect", "other"} },
			func(_, _ enginefixture.Exchange) error { return nil }, []string{"argv"}},
		{"a different stderr, reported with its length and its first differing line", func(ex *enginefixture.Exchange) {
			ex.Stderr = []byte("Error: no such container " + recID + "\nunexpected-line-zz\n")
		}, nil, []string{"stderr", "unexpected-line-zz", strconv.Itoa(len("Error: no such container " + enginefixture.PlaceholderID("c1") + "\n")),
			strconv.Itoa(len("Error: no such container " + enginefixture.PlaceholderID("c1") + "\nunexpected-line-zz\n"))}},
		{"a different stdout", func(ex *enginefixture.Exchange) { ex.Stdout = []byte("path /home/alice/.local/y\n") }, nil, []string{"stdout"}},
		{"a comparer that accepts the difference", func(ex *enginefixture.Exchange) { ex.Stdout = []byte("took 3.4s\n") },
			func(_, _ enginefixture.Exchange) error { return nil }, nil},
		{"a comparer that refuses", func(ex *enginefixture.Exchange) { ex.Stdout = []byte("took 3.4s\n") },
			func(_, _ enginefixture.Exchange) error { return fmt.Errorf("duration out of range") }, []string{"duration out of range"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex, red := rawInspect()
			tt.change(&ex)

			rt := verify(t, []enginefixture.Fixture{recorded("6.2.1", enginefixture.Rootless, "")}, ex, red, tt.cmp)

			if len(tt.want) == 0 {
				if !rt.quiet() {
					t.Errorf("reported: %s", rt.text())
				}
				return
			}
			if len(rt.errs) == 0 || len(rt.fatals) != 0 || len(rt.skips) != 0 {
				t.Fatalf("errs = %q, fatals = %q, skips = %q, want an Errorf (the contract goes on and says every difference)", rt.errs, rt.fatals, rt.skips)
			}
			for _, w := range tt.want {
				if !strings.Contains(strings.Join(rt.errs, "\n"), w) {
					t.Errorf("the report does not contain %q:\n%s", w, strings.Join(rt.errs, "\n"))
				}
			}
		})
	}
}

// A comparer is given the recorded exchange and the redacted one, in that order.
func TestVerifyGivesTheComparerTheRecordedAndTheRedactedExchange(t *testing.T) {
	ex, red := rawInspect()
	var recordedSeen, gotSeen enginefixture.Exchange
	cmp := func(rec, got enginefixture.Exchange) error { recordedSeen, gotSeen = rec, got; return nil }

	verify(t, []enginefixture.Fixture{recorded("6.2.1", enginefixture.Rootless, "")}, ex, red, cmp)

	want := recorded("6.2.1", enginefixture.Rootless, "").Exchange
	if string(recordedSeen.Stdout) != string(want.Stdout) || string(gotSeen.Stdout) != string(want.Stdout) ||
		string(gotSeen.Stderr) != string(want.Stderr) {
		t.Errorf("comparer saw recorded %+v and got %+v, want both to be the redacted text %+v", recordedSeen, gotSeen, want)
	}
}

// No fixture for the engine: the contract is not skipped (a Skip would end it
// before its own assertions), and the log says how to record one.
func TestVerifyWithoutAFixtureLogsHowToRecordOne(t *testing.T) {
	ex, red := rawInspect()
	tests := []struct {
		name     string
		fixtures []enginefixture.Fixture
	}{
		{"none at all", nil},
		{"only another version", []enginefixture.Fixture{recorded("5.8.11", enginefixture.Rootless, "")}},
		{"only another mode", []enginefixture.Fixture{recorded("6.2.1", enginefixture.Rootful, "")}},
		{"only a skipped one", []enginefixture.Fixture{recorded("6.2.1", enginefixture.Rootless, "skip: carries a duration")}},
		{"only another scenario", []enginefixture.Fixture{func() enginefixture.Fixture {
			f := recorded("6.2.1", enginefixture.Rootless, "")
			f.Scenario, f.Name = "other", "other--podman-6.2.1-rootless"
			return f
		}()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := verify(t, tt.fixtures, ex, red, nil)

			if !rt.quiet() {
				t.Fatalf("reported %s, want only a log: the contract must go on", rt.text())
			}
			logged := strings.Join(rt.logs, "\n")
			for _, want := range []string{"no recorded fixture", "inspect-absent", "engine-contract:record"} {
				if !strings.Contains(logged, want) {
					t.Errorf("the log does not contain %q:\n%s", want, logged)
				}
			}
		})
	}
}

// Verify over the embedded set: a campaign nothing recorded is the same case.
func TestVerifyOfACampaignThatIsNotEmbeddedLogsAndGoesOn(t *testing.T) {
	ex, red := rawInspect()
	rt := &recT{TB: t}

	rt.run(func() { verifySession().Verify(rt, "no-such-campaign-recorded", "inspect-absent", ex, red, nil) })

	if !rt.quiet() || !strings.Contains(strings.Join(rt.logs, "\n"), "engine-contract:record") {
		t.Errorf("errs %q, fatals %q, skips %q, logs %q, want only a log that names engine-contract:record", rt.errs, rt.fatals, rt.skips, rt.logs)
	}
}

// The default path of versions.env is the one the importer uses: one definition.
func TestVersionsFileDefaultsToTheVersionsSourceFile(t *testing.T) {
	want, err := enginefixture.VersionsSourceFile()
	if err != nil {
		t.Skipf("no source tree (-trimpath?): %v", err)
	}
	got, err := enginecontract.VersionsFile(func(string) string { return "" })
	if err != nil || got != want {
		t.Errorf("VersionsFile() = %q, %v, want enginefixture.VersionsSourceFile() = %q", got, err, want)
	}
}
