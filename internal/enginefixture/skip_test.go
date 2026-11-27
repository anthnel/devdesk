package enginefixture_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-08b: what "record: skip: <reason>" means (point carried over from the review
// of OC-08a, which parsed the header and left its meaning open).
//
// A skipped fixture is a recorded exchange that the recorder kept in the tree but
// that no test may replay: it is documentation (an answer that depends on the
// date, on the machine, on an ordering). The safest reading is the one that never
// lets it answer by accident and never lets it disappear without a word:
//
//   - Parse, Format, Load, LoadFS and All keep it. It is a valid file, so the
//     leak check and the stale check read it like any other;
//   - ForConfig (the selection a test makes) leaves it out, so a test that
//     selects a configuration never replays it and is not broken by it;
//   - NewReplay refuses it, with an error that names the fixture and says it is
//     skipped: a test that hands one over by name gets a failure, not a silent
//     miss. fakecli.Fixtures goes through NewReplay and inherits the refusal.

const skipRecord = "skip: depends on the clock of the machine"

// skipped returns a copy of a synthetic fixture marked as skipped.
func skipped(t *testing.T, name string) enginefixture.Fixture {
	t.Helper()
	f := parseSynthetic(t, name)
	f.Meta.Record = skipRecord
	return f
}

func TestASkippedFixtureIsAValidFile(t *testing.T) {
	f := skipped(t, fileRmDocker)
	data := enginefixture.Format(f)
	back, err := enginefixture.Parse("synthetic", fileRmDocker, data)
	if err != nil {
		t.Fatalf("a skipped fixture does not parse back: %v", err)
	}
	if back.Meta.Record != skipRecord || !sameFixture(f, back) {
		t.Errorf("round trip changed the skipped fixture: record %q", back.Meta.Record)
	}
}

func TestLoadFSKeepsASkippedFixture(t *testing.T) {
	data := string(enginefixture.Format(skipped(t, fileRmDocker)))
	fsys := fstest.MapFS{"c/" + fileRmDocker: &fstest.MapFile{Data: []byte(data)}}
	fixtures, err := enginefixture.LoadFS(fs.FS(fsys), "c")
	if err != nil {
		t.Fatalf("LoadFS refused a skipped fixture: %v", err)
	}
	if len(fixtures) != 1 || fixtures[0].Meta.Record != skipRecord {
		t.Errorf("LoadFS = %v, want the skipped fixture, so that the leak and stale checks see it", names(fixtures))
	}
}

func TestForConfigLeavesASkippedFixtureOut(t *testing.T) {
	plain := parseSynthetic(t, fileRmDocker)
	skip := parseSynthetic(t, filePS)
	skip.Meta.Record = skipRecord
	input := []enginefixture.Fixture{skip, plain}

	got := enginefixture.ForConfig(input, enginefixture.Docker, "29.10.1", enginefixture.Rootful)
	if len(got) != 1 || got[0].Name != plain.Name {
		t.Errorf("ForConfig = %v, want only %s: a skipped fixture is never selected for a replay", names(got), plain.Name)
	}
	if len(input) != 2 || input[0].Meta.Record != skipRecord {
		t.Error("ForConfig modified its input")
	}
}

func TestNewReplayRefusesASkippedFixtureByName(t *testing.T) {
	plain := parseSynthetic(t, fileRmDocker)
	skip := skipped(t, filePS)
	_, err := enginefixture.NewReplay(plain, skip)
	if err == nil {
		t.Fatal("NewReplay accepted a skipped fixture")
	}
	for _, want := range []string{skip.Name, "skip"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}

	// A replay without it is fine, and never answers its argv.
	r, err := enginefixture.NewReplay(plain)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Answer(skip.Meta.Tool, skip.Exchange.Argv); ok {
		t.Error("a replay answered the argv of a skipped fixture")
	}
}
