package enginefixture_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-09a3 (DEVDESK-944), step 13: enginefixture.Import, what the workstation runs
// on the directory the recorder wrote in the test VM, before anything reaches
// internal/enginefixture/fixtures. It trusts nothing the VM did: it parses every
// file again, reads the whole batch as ONE configuration, checks the version
// against N and N-1, runs the leak check with the external deny list and the
// identities of the machine, and writes nothing unless the whole batch passes.
//
// The API these tests pin down (package enginefixture; no "testing"; the
// command is internal/enginefixture/cmd/engine-fixture-import, see its tests):
//
//	type ImportOptions struct {
//		From, Into string    // Into "" = SourceDir(); both are directories <dir>/<campaign>/<name>.cli
//		Supported  Supported // N and N-1 per tool (ParseVersions of versions.env)
//		Deny       []string  // the external deny list (exact terms)
//		Local      []string  // the identities of this machine (MachineIdentities)
//	}
//	type ImportReport struct {
//		Engine                                string   // "podman 6.2.1 rootless"
//		Written, Unchanged, Kept, NotProduced []string // "<campaign>/<name>", name without ".cli", sorted
//	}
//	func Import(o ImportOptions) (ImportReport, error)
//
// Rules (plan OC-09a, 3.7):
//
//   - From holds only regular files <campaign>/<name>.cli, campaign matching
//     ^[a-z0-9-]+$: a link, any other file (also at the root), a sub directory of
//     a campaign, an empty batch or a campaign that is not a directory is an error;
//   - every file: Parse; recorded-by RecordedByContract; record empty (not
//     pending, not skip, not replay); one configuration (tool, version, mode) for
//     the whole batch; the version in Supported; CheckLeaksAs(Deny, Local) clean;
//   - ALL OR NOTHING: when any check fails, Into is not touched at all (not a
//     file written, not a directory made, not a temporary file left);
//   - then, per file: identical to the existing target except for the "recorded"
//     date: left as it is, bytes and modification time (Unchanged); the target says
//     "record: skip: ...": not overwritten (Kept); otherwise written atomically
//     (Written);
//   - the fixtures of Into with the same configuration that the batch did not
//     produce are listed (NotProduced) and never deleted; those of another
//     configuration are not even listed; an existing target that does not parse is
//     an error (nothing is written);
//   - an error says file and rule, never the text found; From is left as it is.

const (
	impPodman = enginefixture.Podman
	impNames  = "podman 6.2.1 rootless"
)

// impFx is a valid recorder output: a ps exchange of the given engine.
func impFx(campaign, scenario string, tool enginefixture.Tool, version string, mode enginefixture.Mode, recorded, stdout string) enginefixture.Fixture {
	return enginefixture.Fixture{
		Campaign: campaign,
		Name:     scenario + "--" + string(tool) + "-" + version + "-" + string(mode),
		Scenario: scenario,
		Meta: enginefixture.Meta{
			Tool: tool, Version: version, Mode: mode, Recorded: recorded,
			Scenario: "what " + scenario + " shows", Redacted: "none", RecordedBy: enginefixture.RecordedByContract,
		},
		Exchange: enginefixture.Exchange{Argv: []string{"ps", "-a"}, Stdout: []byte(stdout)},
	}
}

// impPodmanFx is impFx for the configuration most tests use.
func impPodmanFx(campaign, scenario, recorded, stdout string) enginefixture.Fixture {
	return impFx(campaign, scenario, impPodman, "6.2.1", enginefixture.Rootless, recorded, stdout)
}

// impPut writes a fixture under root as the recorder would.
func impPut(t *testing.T, root string, f enginefixture.Fixture) string {
	t.Helper()
	return impPutBytes(t, root, f.Campaign, f.Name+".cli", enginefixture.Format(f))
}

func impPutBytes(t *testing.T, root, campaign, file string, data []byte) string {
	t.Helper()
	dir := filepath.Join(root, campaign)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, file)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func impID(f enginefixture.Fixture) string { return f.Campaign + "/" + f.Name }

// impSnapshot describes a tree down to the byte and the modification time, the
// directories and the links included: two equal snapshots are the same tree.
func impSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		desc := info.Mode().String()
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			desc += " " + info.ModTime().UTC().Format(time.RFC3339Nano) + " " + string(data)
		}
		out[rel] = desc
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func impRead(t *testing.T, root string, f enginefixture.Fixture) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, f.Campaign, f.Name+".cli"))
	if err != nil {
		t.Fatalf("%s is not in the target: %v", impID(f), err)
	}
	return data
}

func impImport(from, into string) (enginefixture.ImportReport, error) {
	return enginefixture.Import(enginefixture.ImportOptions{From: from, Into: into, Supported: testSupported})
}

func TestImportWritesAValidBatch(t *testing.T) {
	from, into := t.TempDir(), t.TempDir()
	a := impPodmanFx("containers", "rm-running", "2026-11-27", "a\n")
	b := impPodmanFx("networks", "ls", "2026-11-27", "b\n")
	impPut(t, from, a)
	impPut(t, from, b)

	got, err := impImport(from, into)

	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if got.Engine != impNames {
		t.Errorf("Engine = %q, want %q", got.Engine, impNames)
	}
	if want := []string{impID(a), impID(b)}; !slices.Equal(got.Written, want) {
		t.Errorf("Written = %v, want %v (sorted, name without .cli)", got.Written, want)
	}
	if len(got.Unchanged)+len(got.Kept)+len(got.NotProduced) != 0 {
		t.Errorf("report = %+v, want only Written", got)
	}
	for _, f := range []enginefixture.Fixture{a, b} {
		if !bytes.Equal(impRead(t, into, f), enginefixture.Format(f)) {
			t.Errorf("%s was not written byte for byte as the recorder produced it", impID(f))
		}
	}
	// What is written is a tree of fixtures and nothing else: no temporary file stays.
	for rel := range impSnapshot(t, into) {
		if rel != "." && !strings.HasSuffix(rel, ".cli") && !slices.Contains([]string{"containers", "networks"}, rel) {
			t.Errorf("%s was left in the target next to the fixtures", rel)
		}
	}
	// Import does not consume what it reads: the batch is still there.
	if n := impCountFixtures(impSnapshot(t, from)); n != 2 {
		t.Errorf("From holds %d fixtures after the import, want the 2 it had", n)
	}
}

func impCountFixtures(snap map[string]string) int {
	n := 0
	for rel := range snap {
		if strings.HasSuffix(rel, ".cli") {
			n++
		}
	}
	return n
}

func TestImportReportNamesDockerRootful(t *testing.T) {
	from, into := t.TempDir(), t.TempDir()
	impPut(t, from, impFx("containers", "ps", enginefixture.Docker, "29.10.1", enginefixture.Rootful, "2026-11-27", "x\n"))

	got, err := impImport(from, into)

	if err != nil || got.Engine != "docker 29.10.1 rootful" {
		t.Errorf("Import = %+v, %v, want the engine docker 29.10.1 rootful", got, err)
	}
}

// Criterion 2: a fixture identical to the existing one except for its date is not
// rewritten: not a byte, not the modification time.
func TestImportDoesNotRewriteAFixtureThatOnlyDiffersByItsDate(t *testing.T) {
	into := t.TempDir()
	a1 := impPodmanFx("containers", "rm-running", "2026-11-27", "a\n")
	b1 := impPodmanFx("networks", "ls", "2026-11-27", "b\n")
	first := t.TempDir()
	impPut(t, first, a1)
	impPut(t, first, b1)
	if _, err := impImport(first, into); err != nil {
		t.Fatalf("first Import: %v", err)
	}
	// An old modification time, so a rewrite cannot hide behind the same second.
	old := time.Date(2001, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, f := range []enginefixture.Fixture{a1, b1} {
		if err := os.Chtimes(filepath.Join(into, f.Campaign, f.Name+".cli"), old, old); err != nil {
			t.Fatal(err)
		}
	}
	before := impSnapshot(t, into)

	second := t.TempDir()
	a2 := impPodmanFx("containers", "rm-running", "2027-03-04", "a\n")
	b2 := impPodmanFx("networks", "ls", "2027-03-04", "b\n")
	impPut(t, second, a2)
	impPut(t, second, b2)
	got, err := impImport(second, into)

	if err != nil {
		t.Fatalf("second Import: %v", err)
	}
	if want := []string{impID(a1), impID(b1)}; !slices.Equal(got.Unchanged, want) {
		t.Errorf("Unchanged = %v, want %v", got.Unchanged, want)
	}
	if len(got.Written) != 0 {
		t.Errorf("Written = %v, want none: only the date differs", got.Written)
	}
	if after := impSnapshot(t, into); !equalSnapshots(before, after) {
		t.Errorf("the target changed although only the date differs (bytes or modification time)")
	}
	if !strings.Contains(string(impRead(t, into, a1)), "# recorded: 2026-11-27\n") {
		t.Errorf("the existing fixture lost its original date")
	}
}

func equalSnapshots(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// Anything but the date makes a fixture a different one: the new one replaces it.
func TestImportRewritesAFixtureThatChangedBeyondItsDate(t *testing.T) {
	base := impPodmanFx("containers", "rm-running", "2026-11-27", "a\n")
	changed := map[string]enginefixture.Fixture{
		"the stdout": impPodmanFx("containers", "rm-running", "2026-11-27", "other\n"),
		"the scenario text": func() enginefixture.Fixture {
			f := base
			f.Meta.Scenario = "another sentence"
			return f
		}(),
		"the redacted header": func() enginefixture.Fixture {
			f := base
			f.Meta.Redacted = "home, ids"
			return f
		}(),
		"the exit code": func() enginefixture.Fixture {
			f := base
			f.Exchange.Exit = 2
			return f
		}(),
		"the argv": func() enginefixture.Fixture {
			f := base
			f.Exchange.Argv = []string{"ps"}
			return f
		}(),
	}
	for name, f := range changed {
		t.Run(name, func(t *testing.T) {
			into := t.TempDir()
			first := t.TempDir()
			impPut(t, first, base)
			if _, err := impImport(first, into); err != nil {
				t.Fatal(err)
			}
			second := t.TempDir()
			f.Meta.Recorded = "2027-03-04"
			impPut(t, second, f)

			got, err := impImport(second, into)

			if err != nil {
				t.Fatalf("Import: %v", err)
			}
			if want := []string{impID(base)}; !slices.Equal(got.Written, want) || len(got.Unchanged) != 0 {
				t.Errorf("report = %+v, want %v written", got, want)
			}
			if !bytes.Equal(impRead(t, into, base), enginefixture.Format(f)) {
				t.Errorf("the target does not hold the new fixture")
			}
		})
	}
}

// A fixture marked record: skip is the human's: the recorder never touches it, and
// says so.
func TestImportNeverTouchesASkippedFixture(t *testing.T) {
	into := t.TempDir()
	skipped := impPodmanFx("containers", "ps-health", "2026-01-02", "kept by hand\n")
	skipped.Meta.Record = "skip: the output carries a duration"
	impPut(t, into, skipped)
	old := time.Date(2001, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(into, "containers", skipped.Name+".cli"), old, old); err != nil {
		t.Fatal(err)
	}
	before := impSnapshot(t, into)
	from := t.TempDir()
	fresh := impPodmanFx("containers", "ps-health", "2027-03-04", "what the engine says today\n")
	impPut(t, from, fresh)

	got, err := impImport(from, into)

	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if want := []string{impID(fresh)}; !slices.Equal(got.Kept, want) {
		t.Errorf("Kept = %v, want %v", got.Kept, want)
	}
	if len(got.Written)+len(got.Unchanged) != 0 {
		t.Errorf("report = %+v, want the skipped fixture reported as kept and nothing else", got)
	}
	if after := impSnapshot(t, into); !equalSnapshots(before, after) {
		t.Errorf("the skipped fixture was touched")
	}
}

func TestImportListsWhatTheRunDidNotProduceAndDeletesNothing(t *testing.T) {
	into := t.TempDir()
	gone := impPodmanFx("containers", "gone", "2026-11-27", "old\n")
	otherVersion := impFx("containers", "gone", impPodman, "5.8.11", enginefixture.Rootless, "2026-11-27", "n-1\n")
	otherMode := impFx("containers", "gone", impPodman, "6.2.1", enginefixture.Rootful, "2026-11-27", "rootful\n")
	otherTool := impFx("containers", "gone", enginefixture.Docker, "29.10.1", enginefixture.Rootful, "2026-11-27", "docker\n")
	for _, f := range []enginefixture.Fixture{gone, otherVersion, otherMode, otherTool} {
		impPut(t, into, f)
	}
	kept := impSnapshot(t, into)
	from := t.TempDir()
	produced := impPodmanFx("containers", "produced", "2027-03-04", "new\n")
	impPut(t, from, produced)

	got, err := impImport(from, into)

	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if want := []string{impID(gone)}; !slices.Equal(got.NotProduced, want) {
		t.Errorf("NotProduced = %v, want only the fixture of the same configuration: %v", got.NotProduced, want)
	}
	after := impSnapshot(t, into)
	for rel, desc := range kept {
		if after[rel] != desc {
			t.Errorf("%s changed or was deleted: a run never deletes what it did not produce", rel)
		}
	}
	if _, ok := after[filepath.Join("containers", produced.Name+".cli")]; !ok {
		t.Errorf("the produced fixture was not written")
	}
}

// ---- Refusals: nothing is written. ----

type refusal struct {
	name string
	// build adds the offending item to a batch that holds one valid fixture (the
	// valid sibling, which would otherwise replace a fixture of the target). It may
	// return strings the error must not contain.
	build func(t *testing.T, from string) (secrets []string)
	opts  func(o *enginefixture.ImportOptions)
	// into prepares the target further, besides the two fixtures every case has.
	into func(t *testing.T, into string)
	want []string // words the error must contain: file names, rules
}

func refusals() []refusal {
	leak := func(stdout string) func(*testing.T, string) []string {
		return func(t *testing.T, from string) []string {
			impPut(t, from, impPodmanFx("containers", "leaky", "2026-11-27", stdout))
			return nil
		}
	}
	return []refusal{
		{name: "two engine versions in one batch", want: []string{"configuration"},
			build: func(t *testing.T, from string) []string {
				impPut(t, from, impFx("containers", "other", impPodman, "5.8.11", enginefixture.Rootless, "2026-11-27", "x\n"))
				return nil
			}},
		{name: "two modes in one batch", want: []string{"configuration"},
			build: func(t *testing.T, from string) []string {
				impPut(t, from, impFx("containers", "other", impPodman, "6.2.1", enginefixture.Rootful, "2026-11-27", "x\n"))
				return nil
			}},
		{name: "two tools in one batch", want: []string{"configuration"},
			build: func(t *testing.T, from string) []string {
				impPut(t, from, impFx("containers", "other", enginefixture.Docker, "29.10.1", enginefixture.Rootful, "2026-11-27", "x\n"))
				return nil
			}},
		{name: "a version that is neither N nor N-1", want: []string{"6.3.0"},
			build: func(t *testing.T, from string) []string {
				// The whole batch is one configuration, so the valid sibling is of that version too.
				if err := os.RemoveAll(filepath.Join(from, "containers")); err != nil {
					t.Fatal(err)
				}
				impPut(t, from, impFx("containers", "a", impPodman, "6.3.0", enginefixture.Rootless, "2026-11-27", "x\n"))
				impPut(t, from, impFx("containers", "b", impPodman, "6.3.0", enginefixture.Rootless, "2026-11-27", "y\n"))
				return nil
			}},
		{name: "a synthetic fixture", want: []string{"synthetic--podman-6.2.1-rootless"},
			build: func(t *testing.T, from string) []string {
				f := impPodmanFx("containers", "synthetic", "2026-11-27", "x\n")
				f.Meta.RecordedBy = "synthetic"
				impPut(t, from, f)
				return nil
			}},
		{name: "a fixture that says nobody recorded it", want: []string{"anonymous--podman-6.2.1-rootless"},
			build: func(t *testing.T, from string) []string {
				f := impPodmanFx("containers", "anonymous", "2026-11-27", "x\n")
				f.Meta.RecordedBy = ""
				impPut(t, from, f)
				return nil
			}},
		{name: "a pending fixture", want: []string{"pending--podman-6.2.1-rootless"},
			build: func(t *testing.T, from string) []string {
				f := impPodmanFx("containers", "pending", "2026-11-27", "")
				f.Meta.Record = enginefixture.RecordPending
				impPut(t, from, f)
				return nil
			}},
		{name: "a fixture that arrives already skipped", want: []string{"skipped--podman-6.2.1-rootless"},
			build: func(t *testing.T, from string) []string {
				f := impPodmanFx("containers", "skipped", "2026-11-27", "x\n")
				f.Meta.Record = "skip: by the VM"
				impPut(t, from, f)
				return nil
			}},
		{name: "a symbolic link to a fixture", want: []string{"linked--podman-6.2.1-rootless"},
			build: func(t *testing.T, from string) []string {
				target := impPut(t, t.TempDir(), impPodmanFx("elsewhere", "linked", "2026-11-27", "x\n"))
				link := filepath.Join(from, "containers", "linked--podman-6.2.1-rootless.cli")
				if err := os.Symlink(target, link); err != nil {
					t.Skipf("no symbolic links here: %v", err)
				}
				return nil
			}},
		{name: "a campaign that is a symbolic link", want: []string{"linked"},
			build: func(t *testing.T, from string) []string {
				real := t.TempDir()
				impPut(t, real, impPodmanFx("linked", "x", "2026-11-27", "x\n"))
				if err := os.Symlink(filepath.Join(real, "linked"), filepath.Join(from, "linked")); err != nil {
					t.Skipf("no symbolic links here: %v", err)
				}
				return nil
			}},
		{name: "a file that is not a fixture, in a campaign", want: []string{"x.txt"},
			build: func(t *testing.T, from string) []string {
				impPutBytes(t, from, "containers", "x.txt", []byte("hello\n"))
				return nil
			}},
		{name: "a file at the root of the batch", want: []string{"x.txt"},
			build: func(t *testing.T, from string) []string {
				if err := os.WriteFile(filepath.Join(from, "x.txt"), []byte("hello\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				return nil
			}},
		{name: "a fixture at the root of the batch", want: []string{"loose--podman-6.2.1-rootless"},
			build: func(t *testing.T, from string) []string {
				f := impPodmanFx("", "loose", "2026-11-27", "x\n")
				impPutBytes(t, from, "", f.Name+".cli", enginefixture.Format(f))
				return nil
			}},
		{name: "a sub directory in a campaign", want: []string{"sub"},
			build: func(t *testing.T, from string) []string {
				if err := os.MkdirAll(filepath.Join(from, "containers", "sub"), 0o750); err != nil {
					t.Fatal(err)
				}
				return nil
			}},
		{name: "a campaign name that is not lower case", want: []string{"Containers"},
			build: func(t *testing.T, from string) []string {
				impPut(t, from, impPodmanFx("Containers", "upper", "2026-11-27", "x\n"))
				return nil
			}},
		{name: "a file that is not a valid fixture", want: []string{"broken--podman-6.2.1-rootless"},
			build: func(t *testing.T, from string) []string {
				impPutBytes(t, from, "containers", "broken--podman-6.2.1-rootless.cli", []byte("this is not a fixture\n"))
				return nil
			}},
		{name: "a name that disagrees with the headers", want: []string{"liar--podman-5.8.11-rootless"},
			build: func(t *testing.T, from string) []string {
				f := impPodmanFx("containers", "liar", "2026-11-27", "x\n")
				impPutBytes(t, from, "containers", "liar--podman-5.8.11-rootless.cli", enginefixture.Format(f))
				return nil
			}},
		{name: "a personal path", want: []string{"personal-path", "leaky--podman-6.2.1-rootless"},
			build: func(t *testing.T, from string) []string {
				return leak("user alice at /home/alice/secret-project\n")(t, from)
			}},
		{name: "a secret token", want: []string{"secret-token-format"},
			build: func(t *testing.T, from string) []string {
				return leak("token ghp_"+strings.Repeat("a1B2c3D4e5", 4)+"\n")(t, from)
			}},
		{name: "a lab term", want: []string{"lab-identity"},
			build: func(t *testing.T, from string) []string { return leak("host jakku up\n")(t, from) }},
		{name: "a term of the external deny list", want: []string{"exact-term", "leaky--podman-6.2.1-rootless"},
			opts: func(o *enginefixture.ImportOptions) { o.Deny = []string{"ZorkMid-Project"} },
			build: func(t *testing.T, from string) []string {
				leak("label ZorkMid-Project\n")(t, from)
				return []string{"ZorkMid-Project"}
			}},
		{name: "an identity of this machine", want: []string{"local-identity"},
			opts: func(o *enginefixture.ImportOptions) { o.Local = []string{"alicebox"} },
			build: func(t *testing.T, from string) []string {
				leak("host alicebox up\n")(t, from)
				return []string{"alicebox"}
			}},
		{name: "an existing target that is not a valid fixture", want: []string{"garbage--podman-6.2.1-rootless"},
			build: func(t *testing.T, from string) []string { return nil },
			into: func(t *testing.T, into string) {
				impPutBytes(t, into, "containers", "garbage--podman-6.2.1-rootless.cli", []byte("not a fixture\n"))
			}},
	}
}

func TestImportRefusesAndWritesNothing(t *testing.T) {
	for _, tt := range refusals() {
		t.Run(tt.name, func(t *testing.T) {
			from, into := t.TempDir(), t.TempDir()
			// A valid sibling that would replace a fixture already in the target:
			// were the import not all or nothing, it would be written.
			sibling := impPodmanFx("containers", "sibling", "2027-03-04", "fresh\n")
			impPut(t, from, sibling)
			impPut(t, into, impPodmanFx("containers", "sibling", "2026-11-27", "stale\n"))
			impPut(t, into, impPodmanFx("containers", "untouched", "2026-11-27", "untouched\n"))
			secrets := tt.build(t, from)
			if tt.into != nil {
				tt.into(t, into)
			}
			before := impSnapshot(t, into)
			opts := enginefixture.ImportOptions{From: from, Into: into, Supported: testSupported}
			if tt.opts != nil {
				tt.opts(&opts)
			}

			_, err := enginefixture.Import(opts)

			if err == nil {
				t.Fatal("Import = nil, want an error")
			}
			if after := impSnapshot(t, into); !equalSnapshots(before, after) {
				t.Errorf("the target changed although the import failed: all or nothing")
			}
			msg := err.Error()
			for _, want := range tt.want {
				if !strings.Contains(msg, want) {
					t.Errorf("the error does not contain %q: %s", want, msg)
				}
			}
			for _, secret := range append(secrets, "alice", "ghp_", "ZorkMid") {
				if strings.Contains(msg, secret) {
					t.Errorf("the error repeats what it found (%q): %s", secret, msg)
				}
			}
		})
	}
}

func TestImportRefusesAnEmptyBatchAndAMissingOne(t *testing.T) {
	into := t.TempDir()
	empty := t.TempDir()
	if _, err := impImport(empty, into); err == nil {
		t.Error("an empty batch was accepted: with no fixture there is no engine to report")
	}
	if _, err := impImport(filepath.Join(t.TempDir(), "absent"), into); err == nil {
		t.Error("a batch that does not exist was accepted")
	}
	if got := impSnapshot(t, into); len(got) != 1 { // the root only
		t.Errorf("the target holds %v after two refusals", got)
	}
}
