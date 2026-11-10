package workspaces

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-144 step 4: the footer names the operation of the SELECTED row, keeps
// the batch progress beside it, posts a 3 s Error for a new failure and then
// keeps the cause for the failed row (Rule 128). Timers are never run: expiry
// is built with ClearFooterMsg.

const (
	ftDev   = "/tmp/workspaces/devdesk"
	ftClean = "/tmp/workspaces/clean-repo"
	ftBoom  = "boom-raw-secret-detail"
)

// selectRow moves the cursor onto the row with the given name.
func selectRow(t *testing.T, m Model, name string) Model {
	t.Helper()
	m, _ = step(t, m, testutil.Key("home"))
	for range 20 {
		if r, ok := m.table.Selected(); ok && r.Entry.Name == name {
			return m
		}
		m, _ = step(t, m, testutil.Key("down"))
	}
	t.Fatalf("could not select %q", name)
	return m
}

// expire drops the 3 s message without running its timer.
func expire(t *testing.T, m Model) Model {
	t.Helper()
	return feed(t, m, sharedcomponents.ClearFooterMsg{ID: m.footer.ID()})
}

// syncBatch builds a 12-repo sync: item 0 is devdesk (running), 1-2 done, the
// rest queued, so the progress reads 3/12-ish whatever the exact wording.
func syncBatch() jobs.Run {
	targets := []string{ftDev}
	for i := 1; i < 12; i++ {
		targets = append(targets, fmt.Sprintf("/tmp/workspaces/other%02d", i))
	}
	run := jobs.NewRun(jobs.KindSync, "workspaces", testContext, "workspaces", targets...)
	for i := range run.Items {
		switch {
		case i == 0:
			run.Items[i].State = jobs.ItemRunning
		case i < 3:
			run.Items[i].State = jobs.ItemDone
		default:
			run.Items[i].State = jobs.ItemQueued
		}
	}
	return run
}

func TestFooterNamesTheSelectedRowsOperation(t *testing.T) {
	sub := "/tmp/workspaces/clients/a"
	tests := []struct {
		name     string
		run      jobs.Run
		selected string
		want     []string
		notWant  []string
	}{
		{"running scan on the selected repo",
			actRun(jobs.KindScan, testContext, jobs.ItemRunning, ftDev), "devdesk",
			[]string{"Scanning", ftDev}, nil},
		{"another row selected says nothing of it",
			actRun(jobs.KindScan, testContext, jobs.ItemRunning, ftDev), "clean-repo",
			nil, []string{ftDev}},
		{"non-git directory names the winning sub-repo",
			actRun(jobs.KindScan, testContext, jobs.ItemRunning, sub), "clients",
			[]string{"Scanning", sub}, nil},
		{"queued sync",
			actRun(jobs.KindSync, testContext, jobs.ItemQueued, ftDev), "devdesk",
			[]string{"Queued", ftDev}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := withSnapshot(t, loadedModel(t), []jobs.Run{tc.run}, nil)
			m = selectRow(t, m, tc.selected)
			got := m.RenderFooter(200)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("footer lacks %q:\n%s", w, got)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(got, w) {
					t.Errorf("footer should not say %q:\n%s", w, got)
				}
			}
		})
	}
}

func TestFooterKeepsTheBatchProgressBesideTheRowDetail(t *testing.T) {
	m := withSnapshot(t, loadedModel(t), []jobs.Run{syncBatch()}, nil)
	m = feed(t, m, jobsFrame("SPIN"))

	running := selectRow(t, m, "devdesk").RenderFooter(200)
	for _, w := range []string{"Syncing", ftDev, "/12"} {
		if !strings.Contains(running, w) {
			t.Errorf("selected running row: footer lacks %q:\n%s", w, running)
		}
	}
	if strings.Contains(running, "SPIN") {
		t.Errorf("a row detail with a glyph must drop the spinner:\n%s", running)
	}

	idle := selectRow(t, m, "clean-repo").RenderFooter(200)
	if !strings.Contains(idle, "/12") || !strings.Contains(idle, "SPIN") {
		t.Errorf("idle row: want the progress alone, spinning:\n%s", idle)
	}
	if strings.Contains(idle, ftDev) {
		t.Errorf("idle row shows another row's detail:\n%s", idle)
	}
}

func TestScanFailureIsAnErrorThenTheCauseStaysForTheRow(t *testing.T) {
	f := actFailure(jobs.KindScan, testContext, ftDev, "scan failed — check logs", t0())
	m := selectRow(t, loadedModel(t), "devdesk")
	m = withSnapshot(t, m, nil, []jobs.Failure{f})
	m, _ = step(t, m, WorkspaceScanCompleteMsg{RepoPath: ftDev, Error: errors.New(ftBoom)})

	if m.footer.Level() != sharedcomponents.LevelError {
		t.Fatalf("level = %v, want Error", m.footer.Level())
	}
	for _, w := range []string{"Failed to scan", ftDev, "check logs"} {
		if !strings.Contains(m.footer.Text(), w) {
			t.Errorf("event %q lacks %q", m.footer.Text(), w)
		}
	}
	if strings.Contains(m.RenderFooter(200), ftBoom) {
		t.Error("raw err.Error() reached the footer (Rule 128)")
	}

	m = expire(t, m)
	got := m.RenderFooter(200)
	for _, w := range []string{"last scan failed", "check logs", ftDev} {
		if !strings.Contains(got, w) {
			t.Errorf("after expiry, selected failed row: footer lacks %q:\n%s", w, got)
		}
	}
	if strings.Contains(got, ftBoom) {
		t.Errorf("raw error in the retained cause:\n%s", got)
	}
	if other := selectRow(t, m, "clean-repo").RenderFooter(200); strings.Contains(other, "last scan failed") {
		t.Errorf("another row shows the failure:\n%s", other)
	}
}

func TestACompletedOperationLeavesNoDetail(t *testing.T) {
	f := actFailure(jobs.KindScan, testContext, ftDev, "scan failed — check logs", t0())
	m := selectRow(t, loadedModel(t), "devdesk")
	m = withSnapshot(t, m, nil, []jobs.Failure{f})
	// The relaunch succeeded: the registry cleared the failure and finished the run.
	done := actRun(jobs.KindScan, testContext, jobs.ItemDone, ftDev)
	m = withSnapshot(t, m, []jobs.Run{done}, nil)
	m, _ = step(t, m, WorkspaceScanCompleteMsg{RepoPath: ftDev})
	m = expire(t, m)
	got := m.RenderFooter(200)
	for _, w := range []string{ftDev, "failed", "Scanning"} {
		if strings.Contains(got, w) {
			t.Errorf("finished row still has detail %q:\n%s", w, got)
		}
	}
}

func TestSyncSummaryLevelAndPersistence(t *testing.T) {
	tests := []struct {
		name  string
		fail  bool
		level sharedcomponents.Level
	}{
		{"all fine is Info", false, sharedcomponents.LevelInfo},
		{"one failure is Error", true, sharedcomponents.LevelError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run := jobs.NewRun(jobs.KindSync, "workspaces", testContext, "workspaces", ftDev, ftClean)
			run.Items[0].State = jobs.ItemDone
			run.Items[1].State = jobs.ItemDone
			var fails []jobs.Failure
			if tc.fail {
				run.Items[1].State = jobs.ItemFailed
				run.Items[1].Detail = "sync failed — check logs"
				fails = append(fails, actFailure(jobs.KindSync, testContext, ftClean, "sync failed — check logs", t0()))
			}
			m := withSnapshot(t, loadedModel(t), []jobs.Run{run}, fails)
			m, _ = step(t, m, WorkspaceSyncCompleteMsg{RepoPath: ftClean, Outcome: git.SyncUpToDate,
				Error: map[bool]error{true: errors.New(ftBoom)}[tc.fail]})
			if m.footer.Level() != tc.level {
				t.Errorf("summary level = %v, want %v (%q)", m.footer.Level(), tc.level, m.footer.Text())
			}
			summary := m.footer.Text()
			m = selectRow(t, m, "clients") // moving the cursor does not clear it
			if m.footer.Text() != summary || !strings.Contains(m.RenderFooter(200), summary) {
				t.Errorf("summary %q gone after moving the cursor:\n%s", summary, m.RenderFooter(200))
			}
			if tc.fail {
				m = expire(t, m)
				got := selectRow(t, m, "clean-repo").RenderFooter(200)
				if !strings.Contains(got, "last sync failed") || strings.Contains(got, ftBoom) {
					t.Errorf("failed row after expiry:\n%s", got)
				}
			}
		})
	}
}

func TestDeleteFailureIsAFooterErrorNotAnErrorScreen(t *testing.T) {
	f := actFailure(jobs.KindDelete, testContext, ftClean, "delete failed — check logs", t0())
	m := selectRow(t, loadedModel(t), "clean-repo")
	m = withSnapshot(t, m, nil, []jobs.Failure{f})
	m, _ = step(t, m, EntryDeletedMsg{Path: ftClean, Error: errors.New(ftBoom)})

	if m.error != "" {
		t.Errorf("m.error = %q: the body must stay the table (Rule 139)", m.error)
	}
	if v := m.View(); !strings.Contains(v, "clean-repo") || strings.Contains(v, ftBoom) {
		t.Errorf("body is not the table or leaks the raw error:\n%s", v)
	}
	if m.footer.Level() != sharedcomponents.LevelError ||
		!strings.Contains(m.footer.Text(), "Failed to delete") || strings.Contains(m.footer.Text(), ftBoom) {
		t.Errorf("footer = %v %q, want an Error 'Failed to delete … check logs'", m.footer.Level(), m.footer.Text())
	}
	m = expire(t, m)
	if got := m.RenderFooter(200); !strings.Contains(got, "last delete failed") {
		t.Errorf("failure not retained for the row:\n%s", got)
	}
}

func TestFooterStaysOneLineAtNarrowWidth(t *testing.T) {
	idle := loadedModel(t)
	base := idle.GetFooterHeight()

	m := withSnapshot(t, idle, []jobs.Run{syncBatch()}, nil)
	m = selectRow(t, m, "devdesk")
	if m.GetFooterHeight() != base {
		t.Errorf("footer height %d, want unchanged %d", m.GetFooterHeight(), base)
	}
	lines := strings.Split(m.RenderFooter(40), "\n")
	last := lines[len(lines)-1]
	if lipgloss.Width(last) > 40 {
		t.Errorf("info line is %d wide at 40: %q", lipgloss.Width(last), last)
	}
	if !strings.Contains(last, "/12") {
		t.Errorf("progress must survive truncation: %q", last)
	}
}

func jobsFrame(rendered string) jobs.FrameMsg {
	return jobs.FrameMsg{Frame: rendered, RenderedFrame: rendered, Blink: true}
}

func t0() time.Time { return time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC) }
