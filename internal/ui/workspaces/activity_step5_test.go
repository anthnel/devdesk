package workspaces

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-144 step 5, ws side: the help legend and Rule 130.

func TestHelpCarriesTheActivityLegend(t *testing.T) {
	m := loadedModel(t)
	want := indicator.Legend()
	var body string
	for _, s := range m.GetHelpContent().Sections {
		if s.Title == want.Title {
			body = s.Body
		}
	}
	if body == "" {
		t.Fatalf("help has no %q section", want.Title)
	}
	if body != want.Body {
		t.Errorf("the legend was altered: ws shows running, queued, failed and idle, nothing more\n%s", body)
	}
	for _, g := range []string{
		indicator.Glyph(indicator.Running, true), indicator.Glyph(indicator.Queued, false), indicator.Glyph(indicator.Failed, false),
	} {
		if !strings.Contains(body, g) {
			t.Errorf("legend lacks %q", g)
		}
	}
}

// The old help said a deleting row "spins in the Git Status column"; it now
// shows a pastille and the words "deleting".
func TestHelpNoLongerSaysTheRowSpins(t *testing.T) {
	m := loadedModel(t)
	for _, kb := range m.GetHelpContent().KeyBindings {
		if strings.Contains(kb.Description, "spins") {
			t.Errorf("stale help for %q: %s", kb.Key, kb.Description)
		}
	}
}

// The Git Status help mentions the no-upstream marker through the constant.
func TestHelpNamesTheNoUpstreamMarker(t *testing.T) {
	m := loadedModel(t)
	for _, s := range m.GetHelpContent().Sections {
		if s.Title == "Git Status" && strings.Contains(s.Body, gitMarkNoUpstream) {
			return
		}
	}
	t.Errorf("the Git Status help does not mention %q", gitMarkNoUpstream)
}

// Rule 130, crit. 8: an operation in progress is not a cause of greying out.
// Idle, running and failed rows offer the same keys with the same Disabled.
func TestShortcutsDoNotDependOnTheRowsActivity(t *testing.T) {
	const path = "/tmp/workspaces/devdesk" // the first fixture, selected
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	states := []struct {
		name     string
		runs     []jobs.Run
		failures []jobs.Failure
	}{
		{"idle", nil, nil},
		{"running", []jobs.Run{actRun(jobs.KindScan, testContext, jobs.ItemRunning, path)}, nil},
		{"queued", []jobs.Run{actRun(jobs.KindScan, testContext, jobs.ItemQueued, path)}, nil},
		{"failed", nil, []jobs.Failure{actFailure(jobs.KindScan, testContext, path, "scan failed — check logs", at)}},
	}
	base := loadedModel(t)
	ref := base.GetShortcuts()
	for _, st := range states {
		t.Run(st.name, func(t *testing.T) {
			m := withSnapshot(t, base, st.runs, st.failures)
			got := m.GetShortcuts()
			if !slices.Equal(testutil.ShortcutKeys(got), testutil.ShortcutKeys(ref)) {
				t.Errorf("keys = %v, want %v", testutil.ShortcutKeys(got), testutil.ShortcutKeys(ref))
			}
			for _, key := range testutil.ShortcutKeys(ref) {
				if testutil.ShortcutDisabled(got, key) != testutil.ShortcutDisabled(ref, key) {
					t.Errorf("key %q: Disabled = %v, idle row has %v", key, testutil.ShortcutDisabled(got, key), testutil.ShortcutDisabled(ref, key))
				}
			}
		})
	}
	// A failed row is an idle row for the actions: S and F stay available.
	failed := withSnapshot(t, base, nil, []jobs.Failure{actFailure(jobs.KindScan, testContext, path, "scan failed — check logs", at)})
	for _, key := range []string{"F"} {
		if testutil.HasShortcut(failed.GetShortcuts(), key) && testutil.ShortcutDisabled(failed.GetShortcuts(), key) != testutil.ShortcutDisabled(ref, key) {
			t.Errorf("%s changed on a failed row", key)
		}
	}
}
