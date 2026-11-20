package nexusfixture_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// A campaign is written as request-only "record: pending" files before the
// recorder fills them (NX-04, DEVDESK-580 PR3). Load refuses such a file on
// purpose, so a campaign that still holds one cannot be replayed, loaded by
// All() or checked for provenance: those checks cover the campaigns that are
// fully recorded, and the pending files are still checked for leaks and for
// the round trip, which read the files themselves. A campaign leaves this
// state by itself, as soon as the recorder has written its last file.
//
// A pending file must not stay in the tree, because the campaign it belongs to
// leaves the provenance, version and redaction checks while it does:
// TestNoPendingFixtureIsVersioned fails on it, unless a local recording session
// says so with DEVDESK_FIXTURE_RECORDING=1.

// recordingSessionEnv is the variable that lets pending files exist: set only
// while the recorder is filling a campaign on this machine.
const recordingSessionEnv = "DEVDESK_FIXTURE_RECORDING"

// pendingFiles lists, per campaign, the fixture files on disk that still say
// "record: pending", sorted. A campaign with none is absent.
func pendingFiles(t *testing.T) map[string][]string {
	t.Helper()
	pending := map[string][]string{}
	for _, campaign := range nexusfixture.Campaigns() {
		files, err := filepath.Glob(filepath.Join("fixtures", campaign, "*.http"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			f, err := nexusfixture.Parse(campaign, filepath.Base(file), data)
			if err != nil {
				t.Fatalf("Parse(%s): %v", file, err)
			}
			if f.Meta.Record == nexusfixture.RecordPending {
				pending[campaign] = append(pending[campaign], strings.TrimSuffix(filepath.Base(file), ".http"))
			}
		}
		sort.Strings(pending[campaign])
	}
	return pending
}

func countPending(pending map[string][]string) int {
	n := 0
	for _, names := range pending {
		n += len(names)
	}
	return n
}

// skippedFileCount is the number of fixture files on disk in the campaigns that
// loadableCampaigns skips (those that hold a pending file), recorded or pending.
func skippedFileCount(t *testing.T) int {
	t.Helper()
	n := 0
	for campaign := range pendingFiles(t) {
		files, err := filepath.Glob(filepath.Join("fixtures", campaign, "*.http"))
		if err != nil {
			t.Fatal(err)
		}
		n += len(files)
	}
	return n
}

// loadableCampaigns is Campaigns() without the campaigns that still hold a pending file.
func loadableCampaigns(t *testing.T) []string {
	t.Helper()
	pending := pendingFiles(t)
	var campaigns []string
	for _, c := range nexusfixture.Campaigns() {
		if len(pending[c]) == 0 {
			campaigns = append(campaigns, c)
		} else {
			t.Logf("campaign %s is skipped: %d fixture(s) still pending, e.g. %s", c, len(pending[c]), pending[c][0])
		}
	}
	return campaigns
}

// loadAllRecorded is All() over the loadable campaigns.
func loadAllRecorded(t *testing.T) []nexusfixture.Fixture {
	t.Helper()
	var all []nexusfixture.Fixture
	for _, c := range loadableCampaigns(t) {
		fixtures, err := nexusfixture.Load(c)
		if err != nil {
			t.Fatalf("Load(%s): %v", c, err)
		}
		all = append(all, fixtures...)
	}
	return all
}

// While a campaign holds a pending file, All() fails and names it: nothing
// replays a request that has no answer.
func TestAllRefusesACampaignWithAPendingFixture(t *testing.T) {
	pending := pendingFiles(t)
	if len(pending) == 0 {
		t.Skip("no pending fixture: every campaign is recorded")
	}
	_, err := nexusfixture.All()
	if err == nil {
		t.Fatalf("All() = nil error with pending fixtures %v, want an error", pending)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "pending") {
		t.Errorf("All() error %q does not say a fixture is pending", err)
	}
}

// No pending fixture may be versioned: while one exists its campaign is out of the
// provenance, version and redaction checks, so the suite would stay green on files
// nothing verifies. Only a local recording session (DEVDESK_FIXTURE_RECORDING=1)
// may hold them.
func TestNoPendingFixtureIsVersioned(t *testing.T) {
	pending := pendingFiles(t)
	if len(pending) == 0 {
		return
	}
	if os.Getenv(recordingSessionEnv) == "1" {
		t.Logf("%s=1: %d pending fixture(s) tolerated for this recording session", recordingSessionEnv, countPending(pending))
		return
	}
	for campaign, names := range pending {
		t.Errorf("campaign %s holds %d pending fixture(s), e.g. %s: record them with `nexus-bench record`, or set %s=1 for a local recording session", campaign, len(names), names[0], recordingSessionEnv)
	}
}
