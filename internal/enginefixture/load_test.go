package enginefixture_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// syntheticFS is the synthetic files plus what a loader must ignore, served from
// memory so that error cases can add a broken file next to them.
func syntheticFS(t testing.TB, extra map[string]string) fstest.MapFS {
	t.Helper()
	fsys := fstest.MapFS{
		"synthetic/README.md": {Data: []byte("not a fixture\n")},
		"synthetic/notes.txt": {Data: []byte("# tool: docker\n")},
		"synthetic/sub/x.cli": {Data: []byte("a sub directory is not part of the campaign\n")},
	}
	for _, name := range syntheticNames(t) {
		fsys["synthetic/"+name] = &fstest.MapFile{Data: syntheticFile(t, name)}
	}
	for name, body := range extra {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return fsys
}

func names(fixtures []enginefixture.Fixture) []string {
	var out []string
	for _, f := range fixtures {
		out = append(out, f.Name+".cli")
	}
	return out
}

func TestLoadFSReadsASortedCampaignAndIgnoresOtherFiles(t *testing.T) {
	fixtures, err := enginefixture.LoadFS(syntheticFS(t, nil), "synthetic")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}

	want := syntheticNames(t) // sorted
	if got := names(fixtures); !slices.Equal(got, want) {
		t.Errorf("LoadFS names = %v, want the .cli files sorted by name %v", got, want)
	}
	for _, f := range fixtures {
		if f.Campaign != "synthetic" {
			t.Errorf("%s: Campaign = %q, want synthetic", f.Name, f.Campaign)
		}
		if f.Meta.Record == enginefixture.RecordPending {
			t.Errorf("%s: a pending fixture was loaded", f.Name)
		}
	}
}

// The same files read straight from the disk.
func TestLoadFSReadsTheDirectory(t *testing.T) {
	fixtures, err := enginefixture.LoadFS(os.DirFS("testdata"), "synthetic")
	if err != nil {
		t.Fatalf("LoadFS(os.DirFS(testdata)): %v", err)
	}
	if got, want := names(fixtures), syntheticNames(t); !slices.Equal(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
}

func TestLoadFSRefusesWhatItCannotUse(t *testing.T) {
	const broken = "synthetic/broken--docker-29.10.1-rootful.cli"
	const pending = "synthetic/rm-pending--podman-6.2.1-rootless.cli"
	tests := []struct {
		name     string
		fsys     fs.FS
		campaign string
		contains []string
	}{
		{"a campaign that does not exist", syntheticFS(t, nil), "nope", []string{"nope"}},
		{"an invalid file names itself", syntheticFS(t, map[string]string{broken: "not a fixture\n"}),
			"synthetic", []string{"broken--docker-29.10.1-rootful.cli"}},
		{"a pending file names itself and says pending", syntheticFS(t, map[string]string{pending: pendingText}),
			"synthetic", []string{"rm-pending--podman-6.2.1-rootless.cli", "pending"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixtures, err := enginefixture.LoadFS(tt.fsys, tt.campaign)
			if err == nil {
				t.Fatalf("LoadFS accepted it: %v", names(fixtures))
			}
			for _, want := range tt.contains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// ---- The embedded set. ----

// No real fixture is versioned before OC-09b: the embedded set is empty today.
// The test does not pin that (OC-09b adds files); it holds that the embedded set
// is coherent whatever it holds, and says how many there are.
func TestEmbeddedSetIsCoherent(t *testing.T) {
	campaigns := enginefixture.Campaigns()
	if !slices.IsSorted(campaigns) {
		t.Errorf("Campaigns() = %v, want it sorted", campaigns)
	}
	all, err := enginefixture.All()
	if err != nil {
		t.Fatalf("All(): %v", err)
	}
	total := 0
	for _, c := range campaigns {
		fixtures, err := enginefixture.Load(c)
		if err != nil {
			t.Errorf("Load(%s): %v", c, err)
		}
		if len(fixtures) == 0 {
			t.Errorf("Campaigns() lists %s, which holds no fixture", c)
		}
		total += len(fixtures)
	}
	if len(all) != total {
		t.Errorf("All() holds %d fixtures, the campaigns hold %d", len(all), total)
	}
	t.Logf("%d versioned fixture(s) in %d campaign(s)", len(all), len(campaigns))
}

func TestLoadRefusesAnUnknownCampaign(t *testing.T) {
	if _, err := enginefixture.Load("no-such-campaign"); err == nil || !strings.Contains(err.Error(), "no-such-campaign") {
		t.Errorf("Load(unknown) error = %v, want one naming the campaign", err)
	}
}

// The embed directive refuses a directory with no file, which is why README.md exists.
func TestTheFixturesDirectoryKeepsItsReadme(t *testing.T) {
	if _, err := os.Stat(filepath.Join("fixtures", "README.md")); err != nil {
		t.Errorf("fixtures/README.md: %v: go:embed needs a file in fixtures/ while no .cli is versioned", err)
	}
}

// Every .cli of fixtures/ is embedded, and nothing else is: a file added to the
// directory but missed by the embed pattern, or the reverse, fails here.
func TestEmbeddedSetMatchesTheDirectory(t *testing.T) {
	var onDisk []string
	err := filepath.WalkDir("fixtures", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".cli") {
			onDisk = append(onDisk, filepath.ToSlash(strings.TrimPrefix(p, "fixtures"+string(filepath.Separator))))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(onDisk)

	all, err := enginefixture.All()
	if err != nil {
		t.Fatalf("All(): %v", err)
	}
	var embedded []string
	for _, f := range all {
		embedded = append(embedded, f.Campaign+"/"+f.Name+".cli")
	}
	slices.Sort(embedded)

	if !slices.Equal(onDisk, embedded) {
		t.Errorf("fixtures/ on disk: %v\nembedded:          %v\nwant the same files", onDisk, embedded)
	}
}

// ---- Choosing a configuration. ----

func TestForConfigKeepsExactlyOneConfiguration(t *testing.T) {
	fixtures, err := enginefixture.LoadFS(os.DirFS("testdata"), "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	before := names(fixtures)

	tests := []struct {
		name    string
		tool    enginefixture.Tool
		version string
		mode    enginefixture.Mode
		want    []string // in the order of the input
	}{
		{"docker N rootful", enginefixture.Docker, "29.10.1", enginefixture.Rootful,
			[]string{fileBinary, fileInspectThere, filePS, fileRmDocker}},
		{"docker N-1 rootful", enginefixture.Docker, "28.5.2", enginefixture.Rootful, []string{filePortTaken}},
		{"podman N rootless", enginefixture.Podman, "6.2.1", enginefixture.Rootless, []string{fileRmPodman}},
		{"podman N rootful", enginefixture.Podman, "6.2.1", enginefixture.Rootful, []string{fileNoNewline}},
		{"podman N-1 rootful", enginefixture.Podman, "5.8.11", enginefixture.Rootful, []string{fileInspectAbsent}},
		{"podman N-1 rootless: nothing", enginefixture.Podman, "5.8.11", enginefixture.Rootless, nil},
		{"a version prefix is not a version", enginefixture.Docker, "29.10", enginefixture.Rootful, nil},
		{"the version of the other tool", enginefixture.Docker, "6.2.1", enginefixture.Rootful, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := names(enginefixture.ForConfig(fixtures, tt.tool, tt.version, tt.mode))
			if !slices.Equal(got, tt.want) {
				t.Errorf("ForConfig(%s, %s, %s) = %v, want %v", tt.tool, tt.version, tt.mode, got, tt.want)
			}
		})
	}

	if after := names(fixtures); !slices.Equal(before, after) {
		t.Errorf("ForConfig changed its input: %v then %v", before, after)
	}
}
