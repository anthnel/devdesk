package templates

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-149 step 3: the footer says what the selected row says about itself,
// a failed operation is announced with the one shared sentence, and deleting a
// template forgets its sync failure (and only that one).

// named is a run whose item carries the display name the view gives it (H4).
func named(kind jobs.Kind, state jobs.ItemState, target, name string) jobs.Run {
	run := indRun(kind, state, target).WithDisplay(target, name)
	return run
}

// namedFailure is a remembered failure carrying the template's display name.
func namedFailure(kind jobs.Kind, target, name string) jobs.Failure {
	f := indFailure(kind, target, string(kind)+" failed — check logs")
	f.Display = name
	return f
}

// footerOf is the rendered footer, escape sequences stripped.
func footerOf(m Model, width int) string {
	return ansi.Strip(m.RenderFooter(width))
}

// footerLine is the message line (the last one) of the footer.
func footerLine(m Model, width int) string {
	lines := strings.Split(footerOf(m, width), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func TestTheFooterSaysWhatTheSelectedRowIsDoing(t *testing.T) {
	apiScanDir := func(t *testing.T) string { return scanDirOf(t, "api") }
	tests := []struct {
		name  string
		build func(t *testing.T) ([]jobs.Run, []jobs.Failure)
		want  string
	}{
		{"sync running", func(*testing.T) ([]jobs.Run, []jobs.Failure) {
			return []jobs.Run{named(jobs.KindSync, jobs.ItemRunning, "api", "API")}, nil
		}, glyphOf(indicator.Running) + " Syncing API"},
		{"sync queued", func(*testing.T) ([]jobs.Run, []jobs.Failure) {
			return []jobs.Run{named(jobs.KindSync, jobs.ItemQueued, "api", "API")}, nil
		}, glyphOf(indicator.Queued) + " Queued: sync API"},
		{"sync failed", func(*testing.T) ([]jobs.Run, []jobs.Failure) {
			return nil, []jobs.Failure{namedFailure(jobs.KindSync, "api", "API")}
		}, glyphOf(indicator.Failed) + " API: last sync failed — check logs"},
		{"scan failed", func(t *testing.T) ([]jobs.Run, []jobs.Failure) {
			return nil, []jobs.Failure{namedFailure(jobs.KindScan, apiScanDir(t), "API")}
		}, glyphOf(indicator.Failed) + " API: last scan failed — check logs"},
		{"scan failed, sync running", func(t *testing.T) ([]jobs.Run, []jobs.Failure) {
			return []jobs.Run{named(jobs.KindSync, jobs.ItemRunning, "api", "API")},
				[]jobs.Failure{namedFailure(jobs.KindScan, apiScanDir(t), "API")}
		}, glyphOf(indicator.Running) + " Syncing API · earlier failure: scan (see :jobs)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := indFixture(t)
			runs, failures := tc.build(t)
			m = snapshot(t, m, runs, failures)
			m = send(t, m, jobs.FrameMsg{Blink: true})
			got := footerLine(m, 120)
			if !strings.Contains(got, tc.want) {
				t.Errorf("footer = %q, want it to contain %q", got, tc.want)
			}
			if strings.ContainsAny(got, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
				t.Errorf("a detail with a glyph still shows a spinner frame: %q", got)
			}
		})
	}
}

func TestARunningSyncKeepsTheJobsProgressAfterTheDetail(t *testing.T) {
	m := indFixture(t)
	m = snapshot(t, m, []jobs.Run{named(jobs.KindSync, jobs.ItemRunning, "api", "API")}, nil)
	got := footerLine(m, 120)
	if !strings.Contains(got, "Syncing API") || !strings.Contains(got, "0/1") {
		t.Errorf("footer = %q, want the detail and the batch progress", got)
	}
}

func TestAnIdleRowKeepsTheDotWhileAnotherRowSyncs(t *testing.T) {
	m := indFixture(t) // the cursor is on API
	m = snapshot(t, m, []jobs.Run{named(jobs.KindSync, jobs.ItemRunning, "web", "Web")}, nil)
	m = send(t, m, jobs.FrameMsg{Blink: true})
	got := footerLine(m, 120)
	if !strings.Contains(got, indicator.Glyph(indicator.Running, true)) || !strings.Contains(got, "0/1") {
		t.Errorf("footer = %q, want the activity dot and the progress", got)
	}
	if strings.Contains(got, "Syncing API") || strings.Contains(got, "Syncing Web") {
		t.Errorf("an idle row's footer carries another row's detail: %q", got)
	}
}

func TestTheFooterDetailFollowsTheCursor(t *testing.T) {
	m := indFixture(t)
	m = snapshot(t, m, nil, []jobs.Failure{namedFailure(jobs.KindSync, "web", "Web")})
	if got := footerLine(m, 120); strings.Contains(got, "last sync failed") {
		t.Fatalf("API is selected and idle, footer = %q", got)
	}
	// Sorted API, Img, Web: two steps down.
	m = send(t, m, testutil.Key("down"), testutil.Key("down"))
	if got := footerLine(m, 120); !strings.Contains(got, "Web: last sync failed — check logs") {
		t.Errorf("after moving to Web, footer = %q", got)
	}
}

func TestAnIdleRowWithNoWorkHasAnEmptyFooterLine(t *testing.T) {
	m := indFixture(t)
	if got := footerLine(m, 120); got != "" {
		t.Errorf("footer = %q, want empty", got)
	}
}

func TestAFailedOperationIsAnnouncedWithTheSharedSentence(t *testing.T) {
	tests := []struct {
		name string
		msg  func(t *testing.T) any
		want string
		kind jobs.Kind
	}{
		{"sync", func(*testing.T) any { return SyncCompleteMsg{Slug: "api", Name: "API", Err: errors.New("boom")} },
			"Failed to sync API — check logs", jobs.KindSync},
		{"scan", func(t *testing.T) any {
			return ScanCompleteMsg{Path: scanDirOf(t, "api"), Name: "API", Err: errors.New("boom")}
		}, "Failed to scan API — check logs", jobs.KindScan},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := indFixture(t)
			m = snapshot(t, m, nil, []jobs.Failure{namedFailure(tc.kind, targetFor(t, tc.kind), "API")})
			m = send(t, m, tc.msg(t))
			if got := m.footer.Text(); got != tc.want {
				t.Errorf("footer = %q, want %q", got, tc.want)
			}
			if m.footer.Level() != components.LevelError {
				t.Errorf("level = %v, want error", m.footer.Level())
			}
			// The event expires and the row's persistent detail is back.
			m = send(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
			want := "last " + string(tc.kind) + " failed — check logs"
			if got := footerLine(m, 120); !strings.Contains(got, want) {
				t.Errorf("after the event expired, footer = %q, want it to contain %q", got, want)
			}
		})
	}
}

func targetFor(t *testing.T, kind jobs.Kind) string {
	t.Helper()
	if kind == jobs.KindScan {
		return scanDirOf(t, "api")
	}
	return "api"
}

func TestTheFooterOfThePickerIsTheSelectionPromptAlone(t *testing.T) {
	m := pickerOn(t, entry("api", "API"))
	m = snapshot(t, m, nil, []jobs.Failure{namedFailure(jobs.KindSync, "api", "API")})
	got := footerLine(m, 120)
	if !strings.Contains(got, "Choose the template") {
		t.Errorf("footer = %q, want the selection prompt", got)
	}
	if strings.Contains(got, "last sync failed") {
		t.Errorf("the picker's footer carries a row detail: %q", got)
	}
}

func TestTheFooterStaysOnOneLineAtSixtyColumns(t *testing.T) {
	m := indFixture(t)
	before := m.GetFooterHeight()
	m = snapshot(t, m,
		[]jobs.Run{named(jobs.KindSync, jobs.ItemRunning, "api", "API")},
		[]jobs.Failure{namedFailure(jobs.KindScan, scanDirOf(t, "api"), "API")})
	m = send(t, m, testutil.Resize(60, 30))
	out := footerOf(m, 60)
	if n := len(strings.Split(out, "\n")); n != 2 {
		t.Errorf("the footer is %d lines, want 2:\n%s", n, out)
	}
	if w := ansi.StringWidth(footerLine(m, 60)); w > 60 {
		t.Errorf("the message line is %d columns wide, want at most 60", w)
	}
	if got := m.GetFooterHeight(); got != before || got != 2 {
		t.Errorf("GetFooterHeight = %d (was %d), want 2", got, before)
	}
}

func TestDeletingATemplateForgetsItsSyncFailureOnly(t *testing.T) {
	m := indFixture(t)
	_, cmd := m.Update(DeletedMsg{Slug: "api"})
	var forgotten []string
	for _, msg := range testutil.Msgs(cmd) {
		if f, ok := msg.(jobs.ForgetTargetMsg); ok {
			forgotten = append(forgotten, f.Target)
		}
	}
	if len(forgotten) != 1 || forgotten[0] != "api" {
		t.Errorf("forgotten targets = %q, want exactly [api] (the scan directory is kept)", forgotten)
	}
}

func TestAFailedDeleteForgetsNothing(t *testing.T) {
	m := indFixture(t)
	_, cmd := m.Update(DeletedMsg{Slug: "api", Err: errors.New("boom")})
	for _, msg := range testutil.Msgs(cmd) {
		if f, ok := msg.(jobs.ForgetTargetMsg); ok {
			t.Errorf("a failed delete forgot %q", f.Target)
		}
	}
}

// Rule 130 (H9): the footer detail never changes which keys the view offers.
func TestTheShortcutKeysDoNotDependOnTheRowActivity(t *testing.T) {
	states := map[string]func(t *testing.T) ([]jobs.Run, []jobs.Failure){
		"idle": func(*testing.T) ([]jobs.Run, []jobs.Failure) { return nil, nil },
		"queued": func(*testing.T) ([]jobs.Run, []jobs.Failure) {
			return []jobs.Run{named(jobs.KindSync, jobs.ItemQueued, "api", "API")}, nil
		},
		"running": func(*testing.T) ([]jobs.Run, []jobs.Failure) {
			return []jobs.Run{named(jobs.KindSync, jobs.ItemRunning, "api", "API")}, nil
		},
		"failed": func(*testing.T) ([]jobs.Run, []jobs.Failure) {
			return nil, []jobs.Failure{namedFailure(jobs.KindSync, "api", "API")}
		},
	}
	var want []string
	for _, name := range []string{"idle", "queued", "running", "failed"} {
		m := indFixture(t)
		runs, failures := states[name](t)
		m = snapshot(t, m, runs, failures)
		got := testutil.ShortcutKeys(m.GetShortcuts())
		if want == nil {
			want = got
			continue
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: shortcut keys %v differ from idle's %v", name, got, want)
		}
	}
}

// glyphOf is the steady glyph: the footer never blinks.
func glyphOf(s indicator.State) string { return indicator.Glyph(s, true) }
