package enginefixture_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-08b, criterion 3: a fixture recorded from an engine version outside the
// supported list (N and N-1, read from tools/engine-vms/versions.env) fails
// TestFixturesAreNotStale, and the failure says what to do. Like the Nexus stale
// test (internal/nexusfixture/stale_test.go) the comparison lives in this test
// file: no production code reaches the fixtures.

const (
	// minVersionedFixtures is how many versioned fixtures the embedded set must hold: the 10
	// scenarios of OC-09b (containers: ps-health, rm-running, inspect-absent; images: rmi-used;
	// networks: rm-used; volumes: ls, rm-used, prune-unused; launch: name-taken, port-taken), on the
	// four configurations of tools/engine-vms (docker 29.10.1 and 28.5.2 rootful, podman 6.2.1 and
	// 5.8.11 rootless). It never goes down: a deleted fixture fails the test. A new scenario or a new
	// configuration raises it, in the same change that records it.
	minVersionedFixtures = 40

	// recordHint is where a stale fixture sends the reader: the task that records
	// the fixtures of the current engine versions again (OC-09a3, DEVDESK-944).
	recordHint = "mise run engine-contract:record"
)

// stale returns the fixtures whose engine version is neither N nor N-1 of their
// tool, in the order given. A pending fixture has no recorded answer, so there is
// nothing in it to be stale. A fixture whose tool has no supported version is
// stale.
func stale(fixtures []enginefixture.Fixture, supported enginefixture.Supported) []enginefixture.Fixture {
	var out []enginefixture.Fixture
	for _, f := range fixtures {
		if f.Meta.Record == enginefixture.RecordPending {
			continue
		}
		if !slices.Contains(supported[f.Meta.Tool], f.Meta.Version) {
			out = append(out, f)
		}
	}
	return out
}

// staleMessage is the failure text for a non-empty stale set: the count, every
// fixture with the version it carries, the supported versions of its tool, and
// what to do.
func staleMessage(fixtures []enginefixture.Fixture, supported enginefixture.Supported) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d recorded fixture(s) are stale: their engine version is neither N nor N-1 in tools/engine-vms/versions.env.\n", len(fixtures))
	for _, f := range fixtures {
		fmt.Fprintf(&b, "  %s/%s: %s %s, supported (N, N-1): %s\n",
			f.Campaign, f.Name, f.Meta.Tool, f.Meta.Version, strings.Join(supported[f.Meta.Tool], ", "))
	}
	fmt.Fprintf(&b, "Record them again against the supported engines (see %s) and commit the result.", recordHint)
	return b.String()
}

func versioned(campaign, name string, tool enginefixture.Tool, version, record string) enginefixture.Fixture {
	return enginefixture.Fixture{
		Campaign: campaign, Name: name,
		Meta: enginefixture.Meta{Tool: tool, Version: version, Record: record},
	}
}

func staleNames(fixtures []enginefixture.Fixture) []string {
	out := make([]string, 0, len(fixtures))
	for _, f := range fixtures {
		out = append(out, f.Name)
	}
	return out
}

var testSupported = enginefixture.Supported{
	enginefixture.Docker: {"29.10.1", "28.5.2"},
	enginefixture.Podman: {"6.2.1", "5.8.11"},
}

func TestStale(t *testing.T) {
	d, p := enginefixture.Docker, enginefixture.Podman
	tests := []struct {
		name     string
		fixtures []enginefixture.Fixture
		want     []string
	}{
		{"N of Docker", []enginefixture.Fixture{versioned("c", "a", d, "29.10.1", "")}, nil},
		{"N-1 of Docker", []enginefixture.Fixture{versioned("c", "a", d, "28.5.2", "")}, nil},
		{"N of Podman", []enginefixture.Fixture{versioned("c", "a", p, "6.2.1", "")}, nil},
		{"N-1 of Podman", []enginefixture.Fixture{versioned("c", "a", p, "5.8.11", "")}, nil},
		{"older than N-1, Docker", []enginefixture.Fixture{versioned("c", "a", d, "29.9.0", "")}, []string{"a"}},
		{"older than N-1, Podman", []enginefixture.Fixture{versioned("c", "a", p, "5.7.0", "")}, []string{"a"}},
		{"newer than N", []enginefixture.Fixture{versioned("c", "a", d, "30.0.0", "")}, []string{"a"}},
		{"the version of the other tool", []enginefixture.Fixture{versioned("c", "a", d, "6.2.1", "")}, []string{"a"}},
		{"a prefix of a supported version", []enginefixture.Fixture{versioned("c", "a", d, "29.10", "")}, []string{"a"}},
		{"no version", []enginefixture.Fixture{versioned("c", "a", d, "", "")}, []string{"a"}},
		{"a pending fixture has no answer to be stale", []enginefixture.Fixture{versioned("c", "a", d, "1.0.0", enginefixture.RecordPending)}, nil},
		{"a skipped fixture is checked like the others", []enginefixture.Fixture{versioned("c", "a", d, "1.0.0", "skip: depends on the clock")}, []string{"a"}},
		{"only the stale ones, in order", []enginefixture.Fixture{
			versioned("c", "b", d, "29.9.0", ""),
			versioned("c", "c", d, "29.10.1", ""),
			versioned("e", "a", p, "5.7.0", ""),
		}, []string{"b", "a"}},
		{"nothing to compare", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := staleNames(stale(tt.fixtures, testSupported)); !slices.Equal(got, tt.want) {
				t.Errorf("stale = %v, want %v", got, tt.want)
			}
		})
	}
	if got := stale([]enginefixture.Fixture{versioned("c", "a", d, "29.10.1", "")}, nil); len(got) != 1 {
		t.Errorf("with no supported version at all, stale = %v, want the fixture", staleNames(got))
	}
}

func TestStaleMessageNamesTheFixturesTheVersionsAndTheWayOut(t *testing.T) {
	fixtures := []enginefixture.Fixture{
		versioned("lifecycle", "rm-running--docker-29.9.0-rootful", enginefixture.Docker, "29.9.0", ""),
		versioned("lifecycle", "rm-running--podman-5.7.0-rootless", enginefixture.Podman, "5.7.0", ""),
	}
	msg := staleMessage(fixtures, testSupported)
	for _, want := range []string{
		"2 recorded fixture(s)",
		"lifecycle/rm-running--docker-29.9.0-rootful", // each fixture, with its campaign
		"lifecycle/rm-running--podman-5.7.0-rootless",
		"docker 29.9.0", // the version it carries
		"podman 5.7.0",
		"29.10.1, 28.5.2", // N and N-1 of its tool
		"6.2.1, 5.8.11",
		recordHint, // the way out
		"versions.env",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message does not contain %q:\n%s", want, msg)
		}
	}
	// The way out must exist: the task the message names is declared in mise.toml.
	mise, err := os.ReadFile(filepath.Join("..", "..", "mise.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if task := strings.TrimPrefix(recordHint, "mise run "); !strings.Contains(string(mise), `[tasks."`+task+`"]`) {
		t.Errorf("the message sends the reader to %s, but mise.toml declares no task %q", recordHint, task)
	}
}

// The stale check is wired to the real list, not to a copy: a fixture of an old
// version is reported against what versions.env says today.
func TestStaleAgainstTheRealList(t *testing.T) {
	current := realVersions(t)
	old := versioned("c", "old", enginefixture.Docker, "1.0.0", "")
	if got := stale([]enginefixture.Fixture{old}, current); len(got) != 1 {
		t.Fatalf("a Docker 1.0.0 fixture is not stale against %v", current)
	}
	for _, tool := range []enginefixture.Tool{enginefixture.Docker, enginefixture.Podman} {
		for _, v := range current[tool] {
			if got := stale([]enginefixture.Fixture{versioned("c", "ok", tool, v, "")}, current); len(got) != 0 {
				t.Errorf("%s %s is in the supported list and reported stale", tool, v)
			}
		}
	}
}

// TestFixturesAreNotStale is the signal itself: every versioned fixture was
// recorded from an engine N or N-1. When versions.env moves, it turns red, on
// purpose: the fixtures are recorded again in the same change.
func TestFixturesAreNotStale(t *testing.T) {
	supported := realVersions(t)

	embedded, err := enginefixture.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	t.Logf("%d versioned fixture(s) (floor minVersionedFixtures = %d)", len(embedded), minVersionedFixtures)
	if len(embedded) < minVersionedFixtures {
		t.Fatalf("%d versioned fixtures, want at least %d: a fixture was deleted, or the embedded set is empty", len(embedded), minVersionedFixtures)
	}
	if s := stale(embedded, supported); len(s) > 0 {
		t.Fatal("\n" + staleMessage(s, supported))
	}

	// While nothing is versioned the check above passes on nothing. The synthetic
	// files go through the same function, so that it runs on real data.
	t.Run("synthetic", func(t *testing.T) {
		var synthetic []enginefixture.Fixture
		for _, name := range syntheticNames(t) {
			synthetic = append(synthetic, parseSynthetic(t, name))
		}
		if s := stale(synthetic, supported); len(s) > 0 {
			t.Fatal("\n" + staleMessage(s, supported) + "\n(the synthetic files of testdata/synthetic follow the supported versions too)")
		}
	})
}

// A synthetic fixture exists for each supported version of each tool, so the
// mechanism is exercised on N and on N-1 of both.
func TestSyntheticFixturesCoverBothVersionsOfBothTools(t *testing.T) {
	supported := realVersions(t)
	seen := map[string]bool{}
	for _, name := range syntheticNames(t) {
		f := parseSynthetic(t, name)
		seen[string(f.Meta.Tool)+" "+f.Meta.Version] = true
	}
	var missing []string
	for tool, versions := range supported {
		for _, v := range versions {
			if !seen[string(tool)+" "+v] {
				missing = append(missing, string(tool)+" "+v)
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no synthetic fixture for %v", missing)
	}
}
