package enginefixture

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// The embedded set: fixtures/<campaign>/<scenario>--<tool>-<version>-<mode>.cli.
// While no real fixture is versioned (before OC-09b) the directory holds only its
// README, which go:embed needs to compile, and Campaigns is empty.
//
//go:embed fixtures
var embedded embed.FS

const fixturesDir = "fixtures"

// Load reads the fixtures of one embedded campaign, sorted by name. It refuses a
// pending fixture: a request without an answer cannot be replayed.
func Load(campaign string) ([]Fixture, error) {
	sub, err := fs.Sub(embedded, fixturesDir)
	if err != nil {
		return nil, fmt.Errorf("enginefixture: %w", err)
	}
	return LoadFS(sub, campaign)
}

// LoadFS is Load over any file system: it reads <campaign>/*.cli from fsys,
// ignoring every other file and every sub directory.
func LoadFS(fsys fs.FS, campaign string) ([]Fixture, error) {
	entries, err := fs.ReadDir(fsys, campaign)
	if err != nil {
		return nil, fmt.Errorf("enginefixture: campaign %q: %w", campaign, err)
	}
	var fixtures []Fixture
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), fileSuffix) {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(campaign, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("enginefixture: campaign %q: %w", campaign, err)
		}
		f, err := Parse(campaign, e.Name(), data)
		if err != nil {
			return nil, err
		}
		if f.Meta.Record == RecordPending {
			return nil, fail(e.Name(), "the fixture is pending (no recorded answer) and cannot be loaded: run the recorder first")
		}
		fixtures = append(fixtures, f)
	}
	slices.SortFunc(fixtures, func(a, b Fixture) int { return strings.Compare(a.Name, b.Name) })
	return fixtures, nil
}

// Campaigns lists the embedded campaigns that hold at least one fixture file,
// sorted.
func Campaigns() []string {
	entries, err := fs.ReadDir(embedded, fixturesDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && holdsFixture(e.Name()) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names
}

// holdsFixture reports whether an embedded campaign has a .cli file.
func holdsFixture(campaign string) bool {
	entries, err := fs.ReadDir(embedded, path.Join(fixturesDir, campaign))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), fileSuffix) {
			return true
		}
	}
	return false
}

// All loads every embedded campaign.
func All() ([]Fixture, error) {
	var all []Fixture
	for _, c := range Campaigns() {
		fixtures, err := Load(c)
		if err != nil {
			return nil, err
		}
		all = append(all, fixtures...)
	}
	return all, nil
}

// ForConfig keeps the fixtures of one engine configuration, in their order. A
// skipped fixture is left out: it stays in the tree for the leak and stale checks
// and is never selected for a replay. The input is not modified. Version is
// compared whole: "29.10" is not "29.10.1".
func ForConfig(fixtures []Fixture, tool Tool, version string, mode Mode) []Fixture {
	var out []Fixture
	for _, f := range fixtures {
		if f.Meta.Tool == tool && f.Meta.Version == version && f.Meta.Mode == mode && !isSkipped(f) {
			out = append(out, f)
		}
	}
	return out
}

// isSkipped reports a fixture marked "record: skip: <reason>".
func isSkipped(f Fixture) bool {
	return strings.HasPrefix(f.Meta.Record, recordSkipPrefix)
}
