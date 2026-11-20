package nexusfixture

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

//go:embed fixtures
var embedded embed.FS

const (
	fixturesDir = "fixtures"

	// recordedByRecorder is the recorded-by value only the recorder writes.
	recordedByRecorder = "nexus-bench record"
)

// evidenceDirs maps a campaign to the dated measurement it was imported from,
// relative to the repository root.
var evidenceDirs = map[string]string{
	"catalog":    "docs/measurements/nexus-catalog",
	"cachedonly": "docs/measurements/nexus-cachedonly",
	"access":     "docs/measurements/nexus-access",
}

// Load reads the fixtures of one embedded campaign, sorted by name. It refuses
// a pending fixture: a request without an answer cannot be replayed.
func Load(campaign string) ([]Fixture, error) {
	sub, err := fs.Sub(embedded, fixturesDir)
	if err != nil {
		return nil, fmt.Errorf("nexusfixture: %w", err)
	}
	return LoadFS(sub, campaign)
}

// LoadFS is Load over any file system: it reads <campaign>/*.http from fsys.
func LoadFS(fsys fs.FS, campaign string) ([]Fixture, error) {
	entries, err := fs.ReadDir(fsys, campaign)
	if err != nil {
		return nil, fmt.Errorf("nexusfixture: campaign %q: %w", campaign, err)
	}
	var fixtures []Fixture
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), fileSuffix) {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(campaign, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("nexusfixture: campaign %q: %w", campaign, err)
		}
		f, err := Parse(campaign, e.Name(), data)
		if err != nil {
			return nil, err
		}
		if f.Meta.Record == RecordPending {
			return nil, fmt.Errorf("nexusfixture: %s is pending (no recorded answer): run the recorder first", f.Name)
		}
		fixtures = append(fixtures, f)
	}
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].Name < fixtures[j].Name })
	return fixtures, nil
}

// Campaigns lists the embedded campaigns, sorted.
func Campaigns() []string {
	entries, err := fs.ReadDir(embedded, fixturesDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
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

// EvidenceDir is the directory under docs/measurements/ whose files a
// campaign's unrecorded fixtures are byte-identical to.
func EvidenceDir(campaign string) (string, bool) {
	dir, ok := evidenceDirs[campaign]
	return dir, ok
}

// CheckProvenance reports why a fixture may not pass as measured, or nil. A
// fixture is either recorded-by "nexus-bench record", or byte-identical to its
// dated evidence under repoRoot (the repository root, see EvidenceDir). Any other
// recorded-by value, a campaign with no evidence directory, a missing evidence
// file and a difference are errors that name the fixture.
func CheckProvenance(f Fixture, repoRoot string) error {
	if f.Meta.RecordedBy == recordedByRecorder {
		return nil
	}
	if f.Meta.RecordedBy != "" {
		return fmt.Errorf("nexusfixture: %s: recorded-by = %q, want empty or %q", f.Name, f.Meta.RecordedBy, recordedByRecorder)
	}
	evidence, ok := EvidenceDir(f.Campaign)
	if !ok {
		return fmt.Errorf("nexusfixture: %s: campaign %q has no evidence directory and the fixture is not recorded-by %s", f.Name, f.Campaign, recordedByRecorder)
	}
	want, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(evidence), f.Name+fileSuffix))
	if err != nil {
		return fmt.Errorf("nexusfixture: %s: neither recorded-by %s nor backed by its evidence: %w", f.Name, recordedByRecorder, err)
	}
	if got := Format(f); !bytes.Equal(got, want) {
		return fmt.Errorf("nexusfixture: %s is not byte-identical to %s/%s%s and is not recorded-by %s", f.Name, evidence, f.Name, fileSuffix, recordedByRecorder)
	}
	return nil
}

// SourceDir is the fixtures directory in the source tree, for the recorder
// (the embedded copy is read-only). It never answers with a path that is not
// there: under -trimpath runtime.Caller gives a module-relative path, so the
// answer is then an empty path and an error.
func SourceDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(file) {
		return "", fmt.Errorf("nexusfixture: cannot locate the source tree")
	}
	dir := filepath.Join(filepath.Dir(file), fixturesDir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("nexusfixture: cannot locate the source tree: %s is not a directory", dir)
	}
	return dir, nil
}
