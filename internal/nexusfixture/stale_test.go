package nexusfixture_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// The stale-fixture signal (DEVDESK-573, step 6 of the DEVDESK-511 plan, Q3 (a)).
//
// The reference is internal/nexusbench/descriptor.json, the versioned descriptor
// the bench writes from the instance's own answer. It is NOT
// testdata/descriptor.example.json, which is a constant of a test and would not
// move when the bench does. The descriptor is decoded here for three fields only:
// this package does not import nexusbench, and nexusbench does not import this
// package, so no import cycle can appear and no production code reaches the
// fixtures. The comparison lives in this test file for the same reason.

const (
	// recordCommand is the command a stale fixture asks for. The task is delivered
	// by step 8 of the plan.
	recordCommand = "mise run nexus-bench:record"
	// descriptorPath is the bench descriptor, relative to this package.
	descriptorPath = "../nexusbench/descriptor.json"
	// descriptorSchema is the only descriptor schema this test knows how to read.
	descriptorSchema = 2
)

// benchVersion is what the bench descriptor says about the instance.
type benchVersion struct {
	Nexus, Edition string
}

func (b benchVersion) String() string { return b.Nexus + " " + b.Edition }

// readBenchVersion reads nexusVersion and edition from a descriptor file. An
// unreadable file, a schema other than descriptorSchema or an empty field is an
// error that names the path.
func readBenchVersion(path string) (benchVersion, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return benchVersion{}, fmt.Errorf("cannot read the bench descriptor %s: %w", path, err)
	}
	var d struct {
		SchemaVersion int    `json:"schemaVersion"`
		NexusVersion  string `json:"nexusVersion"`
		Edition       string `json:"edition"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return benchVersion{}, fmt.Errorf("cannot decode the bench descriptor %s: %w", path, err)
	}
	if d.SchemaVersion != descriptorSchema {
		return benchVersion{}, fmt.Errorf("the bench descriptor %s has schemaVersion %d, this test reads %d: update it with the descriptor schema",
			path, d.SchemaVersion, descriptorSchema)
	}
	if d.NexusVersion == "" || d.Edition == "" {
		return benchVersion{}, fmt.Errorf("the bench descriptor %s has no nexusVersion or edition (%q, %q)", path, d.NexusVersion, d.Edition)
	}
	return benchVersion{Nexus: d.NexusVersion, Edition: d.Edition}, nil
}

// staleFixtures returns the fixtures whose recorded Nexus version or edition is
// not the bench's, in the order given. A pending fixture has no recorded answer
// yet, so there is nothing in it to be stale: it is skipped (Load refuses it
// anyway). A fixture with no version, or no edition, is stale: its provenance
// cannot be told to match.
func staleFixtures(fixtures []nexusfixture.Fixture, bench benchVersion) []nexusfixture.Fixture {
	var stale []nexusfixture.Fixture
	for _, f := range fixtures {
		if f.Meta.Record == nexusfixture.RecordPending {
			continue
		}
		if f.Meta.NexusVersion != bench.Nexus || f.Meta.Edition != bench.Edition {
			stale = append(stale, f)
		}
	}
	return stale
}

// staleMessage is the failure text for a non-empty stale set: the count per
// campaign, every fixture with the version it carries, the version the bench
// has, and the command that regenerates them.
func staleMessage(stale []nexusfixture.Fixture, bench benchVersion) string {
	perCampaign := map[string]int{}
	for _, f := range stale {
		perCampaign[f.Campaign]++
	}
	campaigns := make([]string, 0, len(perCampaign))
	for c := range perCampaign {
		campaigns = append(campaigns, c)
	}
	sort.Strings(campaigns)
	counts := make([]string, 0, len(campaigns))
	for _, c := range campaigns {
		counts = append(counts, fmt.Sprintf("%s: %d", c, perCampaign[c]))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d recorded fixture(s) are stale (%s): the bench descriptor says Nexus %s, they say otherwise.\n",
		len(stale), strings.Join(counts, ", "), bench)
	for _, f := range stale {
		carried := strings.TrimSpace(f.Meta.NexusVersion + " " + f.Meta.Edition)
		if carried == "" {
			carried = "no version"
		}
		fmt.Fprintf(&b, "  %s/%s: %s, bench: %s\n", f.Campaign, f.Name, carried, bench)
	}
	fmt.Fprintf(&b, "Regenerate them against the bench with `%s` and commit the result.", recordCommand)
	return b.String()
}

// fixtureAt builds a synthetic fixture.
func fixtureAt(campaign, name, version, edition, record string) nexusfixture.Fixture {
	return nexusfixture.Fixture{
		Campaign: campaign,
		Name:     name,
		Meta:     nexusfixture.Meta{NexusVersion: version, Edition: edition, Record: record},
	}
}

func fixtureNames(fixtures []nexusfixture.Fixture) []string {
	out := make([]string, 0, len(fixtures))
	for _, f := range fixtures {
		out = append(out, f.Name)
	}
	return out
}

// TestFixturesMatchTheBenchVersion is the signal itself: every embedded
// fixture was recorded against the Nexus the bench descriptor describes. When
// the bench moves to another Nexus this test turns red, on purpose: the
// fixtures are regenerated in the same change.
func TestFixturesMatchTheBenchVersion(t *testing.T) {
	bench, err := readBenchVersion(filepath.FromSlash(descriptorPath))
	if err != nil {
		t.Fatal(err)
	}
	fixtures, err := nexusfixture.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no embedded fixture: the stale check would pass on nothing")
	}
	if stale := staleFixtures(fixtures, bench); len(stale) > 0 {
		t.Fatal("\n" + staleMessage(stale, bench))
	}
}

func TestStaleFixtures(t *testing.T) {
	bench := benchVersion{Nexus: "3.98.1-01", Edition: "COMMUNITY"}
	tests := []struct {
		name     string
		fixtures []nexusfixture.Fixture
		want     []string
	}{
		{"same version and edition", []nexusfixture.Fixture{
			fixtureAt("c", "a--admin", "3.98.1-01", "COMMUNITY", ""),
		}, nil},
		{"another version", []nexusfixture.Fixture{
			fixtureAt("c", "a--admin", "3.99.0-01", "COMMUNITY", ""),
		}, []string{"a--admin"}},
		{"another build of the same release", []nexusfixture.Fixture{
			fixtureAt("c", "a--admin", "3.98.1-02", "COMMUNITY", ""),
		}, []string{"a--admin"}},
		{"another edition", []nexusfixture.Fixture{
			fixtureAt("c", "a--admin", "3.98.1-01", "PRO", ""),
		}, []string{"a--admin"}},
		{"no version is stale", []nexusfixture.Fixture{
			fixtureAt("c", "a--admin", "", "COMMUNITY", ""),
		}, []string{"a--admin"}},
		{"no edition is stale", []nexusfixture.Fixture{
			fixtureAt("c", "a--admin", "3.98.1-01", "", ""),
		}, []string{"a--admin"}},
		{"a pending fixture has no answer to be stale", []nexusfixture.Fixture{
			fixtureAt("c", "a--admin", "3.99.0-01", "PRO", nexusfixture.RecordPending),
		}, nil},
		{"only the stale ones, in order", []nexusfixture.Fixture{
			fixtureAt("c", "b--admin", "3.99.0-01", "COMMUNITY", ""),
			fixtureAt("c", "c--admin", "3.98.1-01", "COMMUNITY", ""),
			fixtureAt("d", "a--pull", "3.97.0-01", "COMMUNITY", ""),
		}, []string{"b--admin", "a--pull"}},
		{"nothing to compare", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fixtureNames(staleFixtures(tt.fixtures, bench))
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("stale = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStaleMessageNamesTheFixtureTheVersionsAndTheCommand(t *testing.T) {
	bench := benchVersion{Nexus: "3.98.1-01", Edition: "COMMUNITY"}
	stale := []nexusfixture.Fixture{
		fixtureAt("catalog", "catalog-hosted-repository--anon", "3.99.0-01", "COMMUNITY", ""),
		fixtureAt("catalog", "page-group-n1000--admin", "3.99.0-01", "COMMUNITY", ""),
		fixtureAt("realm", "bearer--pull", "", "", ""),
	}
	msg := staleMessage(stale, bench)
	for _, want := range []string{
		"catalog/catalog-hosted-repository--anon", // the fixtures
		"catalog/page-group-n1000--admin",
		"realm/bearer--pull",
		"3.99.0-01 COMMUNITY",   // the version a fixture carries
		"3.98.1-01 COMMUNITY",   // the bench's
		"no version",            // a fixture without one says so
		"catalog: 2, realm: 1",  // the count per campaign
		"3 recorded fixture(s)", // the total
		"`mise run nexus-bench:record`",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message does not contain %q:\n%s", want, msg)
		}
	}
}

func TestReadBenchVersion(t *testing.T) {
	write := func(t *testing.T, content string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "descriptor.json")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("reads version and edition", func(t *testing.T) {
		p := write(t, `{"schemaVersion": 2, "nexusVersion": "3.99.0-01", "edition": "PRO", "namespace": "x"}`)
		got, err := readBenchVersion(p)
		if err != nil {
			t.Fatal(err)
		}
		if want := (benchVersion{Nexus: "3.99.0-01", Edition: "PRO"}); got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
	for name, content := range map[string]string{
		"another schema":   `{"schemaVersion": 3, "nexusVersion": "3.98.1-01", "edition": "COMMUNITY"}`,
		"no schema":        `{"nexusVersion": "3.98.1-01", "edition": "COMMUNITY"}`,
		"no nexus version": `{"schemaVersion": 2, "edition": "COMMUNITY"}`,
		"no edition":       `{"schemaVersion": 2, "nexusVersion": "3.98.1-01"}`,
		"not json":         `nexusVersion: 3.98.1-01`,
	} {
		t.Run(name+" is an error naming the path", func(t *testing.T) {
			p := write(t, content)
			_, err := readBenchVersion(p)
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), p) {
				t.Errorf("error %q does not name %s", err, p)
			}
		})
	}
	t.Run("a missing file is an error naming the path", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "absent.json")
		if _, err := readBenchVersion(p); err == nil || !strings.Contains(err.Error(), p) {
			t.Errorf("err = %v, want one naming %s", err, p)
		}
	})
}
