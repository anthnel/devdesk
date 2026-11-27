package enginefixture_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-08a (DEVDESK-805, steps 2 to 5): the recorded engine fixtures, their file
// format and their replay.
//
// The API these tests pin down (package enginefixture, no import of "testing"):
//
//	type Tool string                  // Docker, Podman
//	type Mode string                  // Rootful, Rootless
//	const RecordPending = "pending"
//	type Meta struct{ Tool; Version; Mode; Recorded; Scenario; Redacted; Record; RecordedBy }
//	type Exchange struct{ Argv []string; Exit int; Stdout, Stderr []byte }
//	type Fixture struct{ Campaign, Name, Scenario string; Meta Meta; Exchange Exchange }
//	func Parse(campaign, fileName string, data []byte) (Fixture, error)
//	func Format(f Fixture) []byte
//	func Load(campaign string) ([]Fixture, error)
//	func LoadFS(fsys fs.FS, campaign string) ([]Fixture, error)
//	func Campaigns() []string
//	func All() ([]Fixture, error)
//	func ForConfig(fs []Fixture, tool Tool, version string, mode Mode) []Fixture
//	func NewReplay(fixtures ...Fixture) (*Replay, error)
//	func (*Replay) Answer(tool Tool, argv []string) (Fixture, bool)
//	func (*Replay) Unused() []string
//
// No real fixture is versioned before OC-09b (user decision): every mechanism is
// tested on the synthetic files of testdata/synthetic, which carry
// "recorded-by: synthetic".

const syntheticDir = "testdata/synthetic"

// The names of the synthetic files, one constant each so a rename is one edit.
const (
	fileRmDocker      = "rm-running--docker-29.10.1-rootful.cli"
	fileRmPodman      = "rm-running--podman-6.2.1-rootless.cli"
	fileInspectAbsent = "inspect-absent--podman-5.8.11-rootful.cli"
	fileInspectThere  = "inspect-present--docker-29.10.1-rootful.cli"
	filePortTaken     = "port-taken--docker-28.5.2-rootful.cli"
	fileBinary        = "binary-stdout--docker-29.10.1-rootful.cli"
	fileNoNewline     = "no-final-newline--podman-6.2.1-rootful.cli"
	filePS            = "ps-all--docker-29.10.1-rootful.cli"
)

// syntheticFile returns the bytes of one synthetic file.
func syntheticFile(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(syntheticDir, name))
	if err != nil {
		t.Fatalf("reading the synthetic fixture: %v", err)
	}
	return data
}

// syntheticNames lists every .cli file of testdata/synthetic, sorted.
func syntheticNames(t testing.TB) []string {
	t.Helper()
	entries, err := os.ReadDir(syntheticDir)
	if err != nil {
		t.Fatalf("listing %s: %v", syntheticDir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".cli") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	if len(names) < 8 {
		t.Fatalf("%s holds %d .cli files, want at least the 8 synthetic ones of OC-08a", syntheticDir, len(names))
	}
	return names
}

// parseSynthetic parses one synthetic file under the campaign "synthetic".
func parseSynthetic(t testing.TB, name string) enginefixture.Fixture {
	t.Helper()
	f, err := enginefixture.Parse("synthetic", name, syntheticFile(t, name))
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return f
}

// sameExchange compares two exchanges byte for byte; nil and empty streams are
// the same stream.
func sameExchange(a, b enginefixture.Exchange) bool {
	return slices.Equal(a.Argv, b.Argv) && a.Exit == b.Exit &&
		bytes.Equal(a.Stdout, b.Stdout) && bytes.Equal(a.Stderr, b.Stderr)
}

// sameFixture compares everything Parse reads from a file.
func sameFixture(a, b enginefixture.Fixture) bool {
	return a.Campaign == b.Campaign && a.Name == b.Name && a.Scenario == b.Scenario &&
		a.Meta == b.Meta && sameExchange(a.Exchange, b.Exchange)
}
