package enginecontract

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// Comparer decides whether the streams of an exchange a contract just made agree
// with the recorded one, for the scenarios whose output carries a value that moves
// (a duration, a health state). It gets the recorded exchange and the redacted one;
// nil means they agree. The argv and the exit code are always compared exactly,
// before the Comparer is asked.
type Comparer func(recorded, got enginefixture.Exchange) error

// nameWord is a campaign or a scenario: it becomes a directory and a file name, so
// nothing that climbs, nests or hides is accepted.
var nameWord = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// recordMode is the mode of a recorded file: the VM user's, and the user who brings
// the directory back (root, through tar) reads it.
const recordMode = 0o600

// Record writes the exchange a contract made as a fixture of the recording output
// directory, once redacted. It needs a recording session (Mode Record, an absolute
// OutDir) in a test VM (RequireLab). The scenario is the file name's, the text is
// the "scenario" header. In order: the names are checked, the exchange is redacted,
// formatted and run through the leak check with the identities of this machine; a
// leak ends the test with the rules and the lines, never the text, and nothing is
// written. The file is <OutDir>/<campaign>/<scenario>--<tool>-<version>-<mode>.cli,
// created without following a link: a link as the directory, as the campaign
// directory or as the file ends the test, and so does a file already there (two
// contracts for one scenario).
func (s *Session) Record(t testing.TB, campaign, scenario, text string, ex enginefixture.Exchange, r enginefixture.Redaction) {
	t.Helper()
	if s.Mode != Record || !filepath.IsAbs(s.OutDir) {
		t.Fatalf("recording needs a recording session: mode %q and output directory %q (%s=1 and an absolute %s)",
			s.Mode, s.OutDir, RecordingEnv, OutEnv)
	}
	s.RequireLab(t)
	if !nameWord.MatchString(campaign) || !nameWord.MatchString(scenario) || strings.Contains(scenario, "--") {
		t.Fatalf("recording %q/%q: the campaign and the scenario are lower-case words of letters, digits and single dashes, no \"--\"", campaign, scenario)
	}

	r = withPinnedImage(r)
	redacted, header := enginefixture.Redact(ex, r)
	if n := enginefixture.UnknownEngineIDs(redacted, r); n > 0 {
		t.Fatalf("recording %s/%s refused: rule %s, %d word(s) of 64 hexadecimal characters that Redact was not told about "+
			"(a container of another run, a sentinel); nothing was written, the text is not shown; declare a public id in Redaction.Public",
			campaign, scenario, enginefixture.RuleUnknownEngineID, n)
	}
	e := s.Engine
	f := enginefixture.Fixture{
		Campaign: campaign,
		Name:     fixtureName(scenario, e),
		Scenario: scenario,
		Meta: enginefixture.Meta{
			Tool: e.Tool, Version: e.Version, Mode: e.Mode, Recorded: time.Now().UTC().Format(time.DateOnly),
			Scenario: text, Redacted: header, RecordedBy: enginefixture.RecordedByContract,
		},
		Exchange: redacted,
	}
	data := enginefixture.Format(f)
	id := campaign + "/" + f.Name
	if leaks := enginefixture.CheckLeaksAs(id, data, nil, enginefixture.MachineIdentities()); len(leaks) > 0 {
		rules := make([]string, len(leaks))
		for i, l := range leaks {
			rules[i] = fmt.Sprintf("%s (line %d)", l.Rule, l.Line)
		}
		t.Fatalf("recording %s refused: the redacted exchange still leaks: %s (nothing was written; the text is not shown)", id, strings.Join(rules, ", "))
	}
	if err := writeFixture(s.OutDir, campaign, f.Name+".cli", data); err != nil {
		t.Fatalf("recording %s: %v", id, err)
	}
}

// withPinnedImage adds the digest of the pinned image (ImageEnv), which is public, to
// the ids a recording may hold; Redact and UnknownEngineIDs both get it, so a scenario that
// anonymizes unknown ids leaves the digest in the argv alone.
func withPinnedImage(r enginefixture.Redaction) enginefixture.Redaction {
	if _, digest, ok := strings.Cut(os.Getenv(ImageEnv), "@sha256:"); ok && ValidImage(os.Getenv(ImageEnv)) {
		r.Public = append(slices.Clone(r.Public), digest)
	}
	return r
}

// fixtureName is "<scenario>--<tool>-<version>-<mode>", the name Parse expects.
func fixtureName(scenario string, e Engine) string {
	return fmt.Sprintf("%s--%s-%s-%s", scenario, e.Tool, e.Version, e.Mode)
}

// writeFixture creates out/campaign/name with data, never through a link and never
// over an existing file.
func writeFixture(out, campaign, name string, data []byte) error {
	if info, err := os.Lstat(out); err != nil || !info.IsDir() {
		return fmt.Errorf("the output directory is not a directory (a link is refused): %s", out)
	}
	dir := filepath.Join(out, campaign)
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.Mkdir(dir, 0o700); err != nil {
			return err
		}
	case err != nil:
		return err
	case !info.IsDir():
		return fmt.Errorf("%s is not a directory (a link is refused)", campaign)
	}
	// O_EXCL: an existing file, a link included, is an error and is not followed.
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, recordMode) //nolint:gosec // names are checked words under a directory this function just checked
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s already exists: two contracts record the same scenario", name)
	}
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return err
	}
	return file.Close()
}

// RequireFixtures says whether Verify fails when the engine has no recorded fixture
// at all. It is true since OC-09b2 versioned the first fixtures (minVersionedFixtures in
// internal/enginefixture/stale_test.go is above 0), and TestVerifyFailsHardOnceFixturesAreVersioned
// fails when the floor is raised and this is not. An engine outside N and N-1 never gets here
// (Require skips it before any contract starts).
const RequireFixtures = true

// Verify compares what a contract just saw to the fixture recorded for the engine
// and the scenario of the embedded campaign. A campaign nothing recorded is the
// case "no fixture", see VerifyAgainst.
func (s *Session) Verify(t testing.TB, campaign, scenario string, ex enginefixture.Exchange, r enginefixture.Redaction, cmp Comparer) {
	t.Helper()
	fixtures, err := enginefixture.Load(campaign)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("loading the recorded fixtures of %q: %v", campaign, err)
	}
	s.VerifyAgainst(t, fixtures, scenario, ex, r, cmp)
}

// VerifyAgainst is Verify over the fixtures of one campaign it was given: the seam
// of the tests, which have no real fixture to load.
//
// With no fixture at all for this engine (tool, version and mode) in the campaign it
// logs a line that says so and names the task that records it, then returns: it does
// not skip the test, because a Skip ends the contract and the assertions it makes
// after the exchange would never run on a checkout without fixtures (every checkout
// before OC-09b). When RequireFixtures is true that case is a failure.
//
// With fixtures for the engine but none for the scenario (a misspelled or renamed
// scenario) it fails the test: a contract that silently compares nothing is worse
// than none.
//
// Otherwise the redacted exchange is compared to the recorded one: the argv and the
// exit code exactly, the streams through cmp (exactly when nil). Every difference is
// an Errorf with the lengths and the first line that differs. The text of a line is
// cited only when it passes the leak check, since the exchange of the VM was
// redacted but not checked.
func (s *Session) VerifyAgainst(t testing.TB, fixtures []enginefixture.Fixture, scenario string, ex enginefixture.Exchange, r enginefixture.Redaction, cmp Comparer) {
	t.Helper()
	s.verifyAgainst(t, fixtures, scenario, ex, r, cmp, RequireFixtures && !s.MissingFixturesOK)
}

func (s *Session) verifyAgainst(t testing.TB, fixtures []enginefixture.Fixture, scenario string, ex enginefixture.Exchange, r enginefixture.Redaction, cmp Comparer, require bool) {
	t.Helper()
	e := s.Engine
	config := enginefixture.ForConfig(fixtures, e.Tool, e.Version, e.Mode)
	var recorded *enginefixture.Fixture
	for _, f := range config {
		if f.Scenario == scenario {
			recorded = &f
			break
		}
	}
	switch {
	case recorded == nil && len(config) > 0:
		t.Fatalf("no recorded fixture for scenario %q, but %d exist for %s %s %s: the scenario is misspelled or was renamed; "+
			"record it with mise run engine-contract:record", scenario, len(config), e.Tool, e.Version, e.Mode)
	case recorded == nil && require:
		t.Fatalf("no recorded fixture for %s %s %s, scenario %q: fixtures are versioned, so a contract may not go without; "+
			"record it with mise run engine-contract:record", e.Tool, e.Version, e.Mode, scenario)
	case recorded == nil:
		t.Logf("no recorded fixture for %s %s %s, scenario %q: nothing was compared; record it with mise run engine-contract:record",
			e.Tool, e.Version, e.Mode, scenario)
		return
	}
	// The line contract.sh counts: a green run must be able to prove that it compared.
	t.Logf("compared %s/%s against %s", recorded.Campaign, scenario, recorded.Name)
	got, _ := enginefixture.Redact(ex, withPinnedImage(r))
	for _, d := range differences(recorded.Exchange, got, cmp) {
		t.Errorf("scenario %q differs from %s: %s", scenario, recorded.Name, d)
	}
}

// differences lists how got differs from recorded.
func differences(recorded, got enginefixture.Exchange, cmp Comparer) []string {
	var out []string
	if !slices.Equal(recorded.Argv, got.Argv) {
		out = append(out, fmt.Sprintf("argv is %s, recorded %s", cited(strings.Join(got.Argv, " ")), cited(strings.Join(recorded.Argv, " "))))
	}
	if recorded.Exit != got.Exit {
		out = append(out, fmt.Sprintf("exit code is %d, recorded %d", got.Exit, recorded.Exit))
	}
	if cmp != nil {
		if err := cmp(recorded, got); err != nil {
			out = append(out, err.Error())
		}
		return out
	}
	for _, stream := range []struct {
		name          string
		recorded, got []byte
	}{{"stdout", recorded.Stdout, got.Stdout}, {"stderr", recorded.Stderr, got.Stderr}} {
		if !bytes.Equal(stream.recorded, stream.got) {
			out = append(out, describeStreams(stream.name, stream.recorded, stream.got))
		}
	}
	return out
}

// describeStreams says how two streams differ: the lengths and the first line that
// does not match, each shortened.
func describeStreams(name string, recorded, got []byte) string {
	want, have := strings.Split(string(recorded), "\n"), strings.Split(string(got), "\n")
	line := 0
	for line < len(want) && line < len(have) && want[line] == have[line] {
		line++
	}
	return fmt.Sprintf("%s is %d bytes, recorded %d; first difference at line %d: got %s, recorded %s",
		name, len(got), len(recorded), line+1, quoteLine(have, line), quoteLine(want, line))
}

// maxQuoted bounds the text of a line in a report.
const maxQuoted = 120

// quoteLine is line i of lines, quoted and shortened, or a word when it has none. A
// line is shown only when it passes the leak check.
func quoteLine(lines []string, i int) string {
	if i >= len(lines) {
		return "(no such line)"
	}
	return cited(lines[i])
}

// cited quotes text for a report, shortened, unless it would leak: then only the
// rules are named. The exchange a contract compares is redacted, not checked, and
// a failing test log is not a place for what the check would have refused.
func cited(text string) string {
	if leaks := enginefixture.CheckLeaksAs("report", []byte(text), nil, enginefixture.MachineIdentities()); len(leaks) > 0 {
		rules := make([]string, 0, len(leaks))
		for _, l := range leaks {
			if !slices.Contains(rules, l.Rule) {
				rules = append(rules, l.Rule)
			}
		}
		return fmt.Sprintf("(%d bytes, not shown: it would leak, rule %s)", len(text), strings.Join(rules, ", "))
	}
	if len(text) > maxQuoted {
		text = text[:maxQuoted] + "..."
	}
	return fmt.Sprintf("%q", text)
}
