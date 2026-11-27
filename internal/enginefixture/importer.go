package enginefixture

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// ImportOptions are the inputs of Import. Both directories hold
// <campaign>/<name>.cli files.
type ImportOptions struct {
	From, Into string    // Into "" = SourceDir()
	Supported  Supported // N and N-1 per tool (ParseVersions of versions.env)
	Deny       []string  // the external deny list (exact terms)
	Local      []string  // the identities of this machine (MachineIdentities)
}

// ImportReport says what Import did, as "<campaign>/<name>" (the name without
// ".cli"), each list sorted.
type ImportReport struct {
	Engine      string   // "podman 6.2.1 rootless"
	Written     []string // new or changed, now in Into
	Unchanged   []string // identical to the existing file but for the "recorded" date: left as it was
	Kept        []string // the existing file says "record: skip: ...": left as it was
	NotProduced []string // of the same configuration, in Into, absent from the batch: listed, never deleted
}

const (
	// maxImportFileBytes bounds what Import reads from one file of a batch that came
	// out of a VM.
	maxImportFileBytes = 16 << 20
	// importFileMode is the mode of a written fixture, as the rest of the tree.
	importFileMode = 0o644
	importDirMode  = 0o755
)

var (
	campaignPattern   = regexp.MustCompile(`^[a-z0-9-]+$`)
	importNamePattern = regexp.MustCompile(`^[a-z0-9][A-Za-z0-9._-]*` + regexp.QuoteMeta(fileSuffix) + `$`)
)

// batchFile is one file of the batch, read and checked.
type batchFile struct {
	fixture Fixture
	data    []byte
}

func (b batchFile) id() string { return b.fixture.Campaign + "/" + b.fixture.Name }

// Import is what the workstation runs on the directory the recorder wrote in the
// test VM, before anything reaches the fixtures of the repository. It trusts
// nothing the VM did and is all or nothing: every file is read and checked first
// (the target is not touched), then written.
//
// From holds regular files <campaign>/<name>.cli only: a link, any other file, a
// sub directory, an empty batch is an error. Every file is parsed again, says
// "recorded-by: engine-contract:record" and has no "record" header, and the whole
// batch is one configuration (tool, version, mode) whose version is one of
// Supported; the leak check runs on each with Deny and Local. An existing file of
// Into that Import would replace or list must parse too.
//
// Then, per file: identical to the existing one but for the "recorded" date, it is
// left as it is (Unchanged); an existing "record: skip" file is not overwritten
// (Kept); anything else is written through a temporary file and a rename (Written).
// The files of Into with the same configuration that the batch did not produce are
// listed (NotProduced) and never deleted; a skipped one is the human's and not
// listed. An error names files and rules, never the text found.
func Import(o ImportOptions) (ImportReport, error) {
	batch, err := readBatch(o.From)
	if err != nil {
		return ImportReport{}, err
	}
	engine, err := checkBatch(batch, o)
	if err != nil {
		return ImportReport{}, err
	}
	into := o.Into
	if into == "" {
		if into, err = SourceDir(); err != nil {
			return ImportReport{}, err
		}
	}
	plan, err := planImport(batch, into)
	if err != nil {
		return ImportReport{}, err
	}
	plan.report.Engine = engine
	if err := plan.apply(into); err != nil {
		return ImportReport{}, err
	}
	return plan.report, nil
}

// readBatch reads the batch directory and refuses anything that is not a regular
// <campaign>/<name>.cli file.
func readBatch(from string) ([]batchFile, error) {
	if info, err := os.Lstat(from); err != nil || !info.IsDir() {
		return nil, errors.New("enginefixture: the batch is not a directory (a link is refused too)")
	}
	roots, err := os.ReadDir(from)
	if err != nil {
		return nil, fmt.Errorf("enginefixture: reading the batch: %w", err)
	}
	var batch []batchFile
	for _, root := range roots {
		campaign := root.Name()
		switch {
		case !root.IsDir():
			return nil, fmt.Errorf("enginefixture: %q at the root of the batch is not a campaign directory", campaign)
		case !campaignPattern.MatchString(campaign):
			return nil, fmt.Errorf("enginefixture: the campaign %q does not match %s", campaign, campaignPattern)
		}
		files, err := readCampaign(from, campaign)
		if err != nil {
			return nil, err
		}
		batch = append(batch, files...)
	}
	if len(batch) == 0 {
		return nil, errors.New("enginefixture: the batch holds no fixture")
	}
	return batch, nil
}

func readCampaign(from, campaign string) ([]batchFile, error) {
	entries, err := os.ReadDir(filepath.Join(from, campaign))
	if err != nil {
		return nil, fmt.Errorf("enginefixture: reading the campaign %q: %w", campaign, err)
	}
	var files []batchFile
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() {
			return nil, fmt.Errorf("enginefixture: %s/%q is not a regular file (a link, a directory or a special file is refused)", campaign, name)
		}
		if !importNamePattern.MatchString(name) {
			return nil, fmt.Errorf("enginefixture: %s/%q is not a fixture file name (<scenario>--<tool>-<version>-<mode>%s)", campaign, name, fileSuffix)
		}
		data, err := readBounded(filepath.Join(from, campaign, name))
		if err != nil {
			return nil, fmt.Errorf("enginefixture: %s/%s: %w", campaign, name, err)
		}
		f, err := Parse(campaign, name, data)
		if err != nil {
			// The file came out of a VM: say which rule it breaks, not what it holds.
			var pe *ParseError
			if errors.As(err, &pe) {
				return nil, fmt.Errorf("enginefixture: %s/%s: invalid format (%s)", campaign, name, pe.Rule)
			}
			return nil, fmt.Errorf("enginefixture: %s/%s: invalid format", campaign, name)
		}
		files = append(files, batchFile{fixture: f, data: data})
	}
	return files, nil
}

// readBounded reads a file of at most maxImportFileBytes.
func readBounded(path string) ([]byte, error) {
	file, err := os.Open(path) //nolint:gosec // the path comes from a directory listing of the batch
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	// The listing said a regular file; what was opened is checked again (a swap in between).
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxImportFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImportFileBytes {
		return nil, fmt.Errorf("the file is larger than %d bytes", maxImportFileBytes)
	}
	return data, nil
}

// checkBatch applies the rules of Import to the files read: what the recorder
// stamps, one configuration, a supported version, no leak. It returns the engine,
// "podman 6.2.1 rootless".
func checkBatch(batch []batchFile, o ImportOptions) (string, error) {
	for _, b := range batch {
		m := b.fixture.Meta
		switch {
		case m.RecordedBy != RecordedByContract:
			return "", fmt.Errorf("enginefixture: %s: recorded-by is not %s: only the recorder's output is imported", b.id(), RecordedByContract)
		case m.Record != "":
			return "", fmt.Errorf("enginefixture: %s: the record header must be empty (a pending, skipped or replay file is not a recording)", b.id())
		}
	}
	first := batch[0].fixture.Meta
	engine := fmt.Sprintf("%s %s %s", first.Tool, first.Version, first.Mode)
	for _, b := range batch[1:] {
		m := b.fixture.Meta
		if m.Tool != first.Tool || m.Version != first.Version || m.Mode != first.Mode {
			return "", fmt.Errorf("enginefixture: %s: the batch mixes engine configurations (%s, and %s %s %s): one recording is one configuration",
				b.id(), engine, m.Tool, m.Version, m.Mode)
		}
	}
	if !slices.Contains(o.Supported[first.Tool], first.Version) {
		return "", fmt.Errorf("enginefixture: %s is neither N nor N-1: versions.env pins %s (N, N-1)",
			engine, strings.Join(o.Supported[first.Tool], ", "))
	}
	var leaks []string
	for _, b := range batch {
		for _, l := range CheckLeaksAs(b.id(), b.data, o.Deny, o.Local) {
			leaks = append(leaks, fmt.Sprintf("%s: line %d: rule %s", l.Fixture, l.Line, l.Rule))
		}
	}
	if len(leaks) > 0 {
		return "", fmt.Errorf("enginefixture: the batch leaks:\n  %s", strings.Join(leaks, "\n  "))
	}
	return engine, nil
}

// importPlan is what Import decided, before anything is written.
type importPlan struct {
	report ImportReport
	writes []batchFile
}

// planImport reads the target and decides, per file of the batch, whether it is
// written, unchanged or kept, and what the batch did not produce.
func planImport(batch []batchFile, into string) (importPlan, error) {
	existing, err := readTarget(into)
	if err != nil {
		return importPlan{}, err
	}
	var plan importPlan
	produced := map[string]bool{}
	for _, b := range batch {
		produced[b.id()] = true
		old, found := existing[b.id()]
		switch {
		case !found:
			plan.writes = append(plan.writes, b)
			plan.report.Written = append(plan.report.Written, b.id())
		case isSkipped(old):
			plan.report.Kept = append(plan.report.Kept, b.id())
		case sameButForTheDate(old, b.fixture):
			plan.report.Unchanged = append(plan.report.Unchanged, b.id())
		default:
			plan.writes = append(plan.writes, b)
			plan.report.Written = append(plan.report.Written, b.id())
		}
	}
	first := batch[0].fixture.Meta
	for id, old := range existing {
		m := old.Meta
		if !produced[id] && !isSkipped(old) && m.Tool == first.Tool && m.Version == first.Version && m.Mode == first.Mode {
			plan.report.NotProduced = append(plan.report.NotProduced, id)
		}
	}
	slices.Sort(plan.report.NotProduced)
	return plan, nil
}

// sameButForTheDate reports whether the existing fixture, with the date of the new
// one, is byte for byte the new one.
func sameButForTheDate(old, fresh Fixture) bool {
	old.Meta.Recorded = fresh.Meta.Recorded
	return bytes.Equal(Format(old), Format(fresh))
}

// readTarget parses every fixture of Into, by "<campaign>/<name>". A directory that
// does not exist yet holds none. Anything else that is not a campaign directory or
// a .cli file is left alone (the README of the fixtures directory); a campaign that
// is a link, or a fixture that is not a regular file or does not parse, is an error.
func readTarget(into string) (map[string]Fixture, error) {
	found := map[string]Fixture{}
	roots, err := os.ReadDir(into)
	if errors.Is(err, fs.ErrNotExist) {
		return found, nil
	}
	if err != nil {
		return nil, fmt.Errorf("enginefixture: reading the target: %w", err)
	}
	for _, root := range roots {
		campaign := root.Name()
		if !root.IsDir() {
			if root.Type()&fs.ModeSymlink != 0 && campaignPattern.MatchString(campaign) {
				return nil, fmt.Errorf("enginefixture: the target campaign %q is a link: refused", campaign)
			}
			continue
		}
		entries, err := os.ReadDir(filepath.Join(into, campaign))
		if err != nil {
			return nil, fmt.Errorf("enginefixture: reading the target campaign %q: %w", campaign, err)
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), fileSuffix) {
				continue
			}
			f, err := readTargetFile(into, campaign, e)
			if err != nil {
				return nil, err
			}
			found[campaign+"/"+f.Name] = f
		}
	}
	return found, nil
}

func readTargetFile(into, campaign string, e fs.DirEntry) (Fixture, error) {
	if !e.Type().IsRegular() {
		return Fixture{}, fmt.Errorf("enginefixture: the target %s/%q is not a regular file: refused", campaign, e.Name())
	}
	data, err := readBounded(filepath.Join(into, campaign, e.Name()))
	if err != nil {
		return Fixture{}, fmt.Errorf("enginefixture: the target %s/%s: %w", campaign, e.Name(), err)
	}
	return Parse(campaign, e.Name(), data)
}

// apply writes the files in two phases, so a failure leaves no half-written
// fixture: every file goes to a temporary file next to its target, then each is
// renamed over it.
func (p importPlan) apply(into string) error {
	type staged struct{ tmp, target string }
	var stage []staged
	discard := func() {
		for _, s := range stage {
			_ = os.Remove(s.tmp)
		}
	}
	for _, b := range p.writes {
		dir := filepath.Join(into, b.fixture.Campaign)
		if err := os.MkdirAll(dir, importDirMode); err != nil {
			discard()
			return fmt.Errorf("enginefixture: %w", err)
		}
		tmp, err := writeTemp(dir, b.data)
		if err != nil {
			discard()
			return fmt.Errorf("enginefixture: writing %s: %w", b.id(), err)
		}
		stage = append(stage, staged{tmp, filepath.Join(dir, b.fixture.Name+fileSuffix)})
	}
	for i, s := range stage {
		if err := os.Rename(s.tmp, s.target); err != nil {
			stage = stage[i:]
			discard()
			return fmt.Errorf("enginefixture: installing %s: %w", filepath.Base(s.target), err)
		}
	}
	return nil
}

// writeTemp writes data to a new file of dir and returns its path.
func writeTemp(dir string, data []byte) (string, error) {
	file, err := os.CreateTemp(dir, ".import-*.tmp")
	if err != nil {
		return "", err
	}
	_, werr := file.Write(data)
	cerr := file.Close()
	if err := errors.Join(werr, cerr, os.Chmod(file.Name(), importFileMode)); err != nil { //nolint:gosec // a fixture is public text, read by every user of the repository
		_ = os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}
