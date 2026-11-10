package security

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-145 step 4: the :sec footer names the SELECTED row's activity
// (components.DescribeRow, as ws does), composed with the batch progress
// (JobsStatusLine) or the load text. Timers are never run: expiry is built
// with ClearFooterMsg.

const (
	ftRepo  = "/w/api"
	ftOther = "/w/web"
	ftBoom  = "boom-raw-secret-detail"
)

// footerModel has two scanned repos and the aliased image, snapshot fed.
func footerModel(t *testing.T, runs []jobs.Run, failures []jobs.Failure) Model {
	t.Helper()
	m := aliasedModel(t,
		scanTarget{Kind: kindRepo, Name: ftRepo, Scanned: true, ScannedAt: time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)},
		scanTarget{Kind: kindRepo, Name: ftOther, Scanned: true, ScannedAt: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)},
		imageTarget(aliasedRef),
	)
	return feed(t, m, jobs.ChangedMsg{Runs: runs, Failures: failures, Context: secCtx})
}

func selectTarget(t *testing.T, m Model, name string) Model {
	t.Helper()
	m, _ = step(t, m, testutil.Key("home"))
	for range 10 {
		if r, ok := m.inventory.Selected(); ok && r.Name == name {
			return m
		}
		m, _ = step(t, m, testutil.Key("down"))
	}
	t.Fatalf("could not select %q", name)
	return m
}

func expire(t *testing.T, m Model) Model {
	t.Helper()
	return feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
}

// batch is a 3-scan run launched from :sec: api running, web done, image queued.
func batch() jobs.Run {
	run := jobs.NewRun(jobs.KindScan, command.ViewSecurity, secCtx, "inventory", ftRepo, ftOther, aliasedRef)
	run.Items[0].State = jobs.ItemRunning
	run.Items[1].State = jobs.ItemDone
	run.Items[2].State = jobs.ItemQueued
	return run
}

func infoLine(m Model, width int) string {
	lines := strings.Split(m.RenderFooter(width), "\n")
	return lines[len(lines)-1]
}

func TestFooterNamesTheSelectedRowAndKeepsTheProgress(t *testing.T) {
	m := footerModel(t, []jobs.Run{batch()}, nil)
	m = feed(t, m, jobs.FrameMsg{Frame: "SPIN", RenderedFrame: "SPIN", Blink: true})

	running := infoLine(selectTarget(t, m, ftRepo), 200)
	for _, w := range []string{"Scanning", ftRepo, "Scanning — 1/3"} {
		if !strings.Contains(running, w) {
			t.Errorf("selected running row: footer lacks %q:\n%s", w, running)
		}
	}
	if strings.Contains(running, "SPIN") {
		t.Errorf("a row detail with a glyph drops the spinner:\n%s", running)
	}

	idle := infoLine(selectTarget(t, m, ftOther), 200)
	if !strings.Contains(idle, "1/3") || !strings.Contains(idle, "SPIN") {
		t.Errorf("idle row during the batch: want the progress alone, spinning:\n%s", idle)
	}
	if strings.Contains(idle, ftRepo) {
		t.Errorf("idle row shows another row's detail:\n%s", idle)
	}
}

func TestFooterOfAnIdleRowWithNoWorkIsEmpty(t *testing.T) {
	m := selectTarget(t, footerModel(t, nil, nil), ftRepo)
	if got := strings.TrimSpace(ansi.Strip(infoLine(m, 120))); got != "" {
		t.Errorf("idle row, no work: info line = %q, want empty", got)
	}
}

func TestFooterFollowsTheCursor(t *testing.T) {
	m := footerModel(t, []jobs.Run{batch()}, nil)
	if got := infoLine(selectTarget(t, m, ftRepo), 200); !strings.Contains(got, ftRepo) {
		t.Errorf("on api: %q", got)
	}
	if got := infoLine(selectTarget(t, m, ftOther), 200); strings.Contains(got, ftRepo) {
		t.Errorf("after moving to web the api detail stays: %q", got)
	}
}

func TestFooterOfAnImageIsTheRawReferenceNotTheAlias(t *testing.T) { // H3
	run := secRun(jobs.KindScan, secCtx, jobs.ItemRunning, aliasedRef)
	m := selectTarget(t, footerModel(t, []jobs.Run{run}, nil), aliasedRef)
	got := infoLine(m, 300)
	if !strings.Contains(got, aliasedRef) {
		t.Errorf("footer lacks the raw reference %q:\n%s", aliasedRef, got)
	}
	if strings.Contains(got, "nx/") {
		t.Errorf("footer shows the registry alias:\n%s", got)
	}
}

func TestFailedRowKeepsItsCauseAfterTheErrorExpires(t *testing.T) {
	f := secFailure(jobs.KindScan, secCtx, ftRepo, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC))
	m := selectTarget(t, footerModel(t, nil, []jobs.Failure{f}), ftRepo)
	m, _ = step(t, m, InventoryScanFinishedMsg{Name: ftRepo, Err: fmt.Errorf("%s", ftBoom)})

	if m.footer.Level() != components.LevelError || !strings.Contains(m.footer.Text(), "Failed to scan") {
		t.Fatalf("footer = %v %q, want an Error 'Failed to scan …'", m.footer.Level(), m.footer.Text())
	}
	if strings.Contains(m.RenderFooter(200), ftBoom) {
		t.Error("raw err.Error() reached the footer (Rule 128)")
	}

	m = expire(t, m)
	got := infoLine(m, 200)
	for _, w := range []string{"last scan failed", "check logs", ftRepo} {
		if !strings.Contains(got, w) {
			t.Errorf("after expiry, failed row: footer lacks %q:\n%s", w, got)
		}
	}
	if strings.Contains(got, ftBoom) {
		t.Errorf("raw error in the retained cause:\n%s", got)
	}
	if other := infoLine(selectTarget(t, m, ftOther), 200); strings.Contains(other, "last scan failed") {
		t.Errorf("another row shows the failure:\n%s", other)
	}
}

func TestLoadComposesWithTheSelectedRowDetail(t *testing.T) {
	f := secFailure(jobs.KindScan, secCtx, ftRepo, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC))
	m := selectTarget(t, footerModel(t, nil, []jobs.Failure{f}), ftRepo)
	m, _ = step(t, m, testutil.Key("ctrl+r"))
	if !m.inventoryLoading {
		t.Fatal("ctrl+r did not start a load")
	}
	got := infoLine(m, 200)
	for _, w := range []string{"Loading scan inventory...", "last scan failed", ftRepo} {
		if !strings.Contains(got, w) {
			t.Errorf("loading on a failed row: footer lacks %q:\n%s", w, got)
		}
	}
}

func TestFooterStaysOneLineAt40Columns(t *testing.T) {
	base := footerModel(t, nil, nil).GetFooterHeight()
	m := selectTarget(t, footerModel(t, []jobs.Run{batch()}, nil), ftRepo)
	if m.GetFooterHeight() != base {
		t.Errorf("footer height %d, want unchanged %d", m.GetFooterHeight(), base)
	}
	if n := len(strings.Split(m.RenderFooter(40), "\n")); n != m.GetFooterHeight() {
		t.Errorf("footer renders %d lines, GetFooterHeight says %d", n, m.GetFooterHeight())
	}
	last := infoLine(m, 40)
	if w := lipgloss.Width(last); w > 40 {
		t.Errorf("info line is %d wide at 40: %q", w, last)
	}
	if !strings.Contains(last, "1/3") {
		t.Errorf("progress must survive truncation: %q", last)
	}
}

// Rule 130: the key set is the same for an idle, a running and a failed row,
// and Disabled equals the availability helpers.
func TestShortcutsAreTheSameForIdleRunningAndFailedRows(t *testing.T) {
	f := secFailure(jobs.KindScan, secCtx, ftOther, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC))
	m := footerModel(t, []jobs.Run{secRun(jobs.KindScan, secCtx, jobs.ItemRunning, ftRepo)}, []jobs.Failure{f})
	states := map[string]Model{
		"running": selectTarget(t, m, ftRepo),
		"failed":  selectTarget(t, m, ftOther),
		"idle":    selectTarget(t, m, aliasedRef),
	}
	var want []string
	for name, s := range states {
		sc := s.GetShortcuts()
		keys := testutil.ShortcutKeys(sc)
		if want == nil {
			want = keys
		} else if strings.Join(keys, ",") != strings.Join(want, ",") {
			t.Errorf("%s: keys %v differ from %v", name, keys, want)
		}
		for key, av := range map[string]bool{
			"enter": s.canOpenTarget().Enabled(), "S": s.canRescanSelected().Enabled(), "A": s.canRescanAll().Enabled(),
		} {
			if got := testutil.ShortcutEnabled(sc, key); got != av {
				t.Errorf("%s: %q enabled = %v, availability says %v", name, key, got, av)
			}
		}
		// A failed but scanned row can still be opened and rescanned.
		if name == "failed" {
			for _, key := range []string{"enter", "S", "A"} {
				if !testutil.ShortcutEnabled(sc, key) {
					t.Errorf("failed scanned row: %q should stay enabled", key)
				}
			}
		}
	}
}
