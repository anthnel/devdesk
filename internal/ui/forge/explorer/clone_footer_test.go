package explorer

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-148 step 3 (H1, H2): in clone mode the footer describes the selected
// row (Status.Detail) and keeps the run's progress (Status.Text) after it.

// cloningAt is the clone screen at a given width with one row selected.
func cloningAt(t *testing.T, width int) *cloneHarness {
	t.Helper()
	h := cloning(t)
	h.m = feed(t, h.m, testutil.Resize(width, 30))
	return h
}

func failedClone(path, cause string) CloneEventMsg {
	return CloneEventMsg{event: cloneEvent{kind: cloneEnded, path: path, err: errors.New(cause)}}
}

func skippedClone(path string) CloneEventMsg {
	return CloneEventMsg{event: cloneEvent{kind: cloneEnded, path: path, skipped: true}}
}

func footerLine(h *cloneHarness, width int) string {
	return ansi.Strip(h.m.renderInfoLine(width))
}

func TestCloneFooterShowsTheCauseAndTheProgressOfAFailedRow(t *testing.T) {
	for _, width := range []int{120, 60} {
		h := cloningAt(t, width)
		h.emit(found("alpha/api"), began("alpha/api"), failedClone("alpha/api", "403"))

		line := footerLine(h, width)
		progress := h.m.cloneStatusLine()
		if !strings.Contains(line, progress) {
			t.Errorf("%d cols: footer lacks the progress %q:\n%q", width, progress, line)
		}
		if w := theme.StringWidth(line); w > width {
			t.Errorf("%d cols: footer is %d columns wide", width, w)
		}
		if width == 120 && !strings.Contains(line, "last clone failed — 403") {
			t.Errorf("120 cols: footer lacks the cause:\n%q", line)
		}
	}
}

func TestCloneFooterIsTheRowDetailThenTheProgress(t *testing.T) {
	h := cloningAt(t, 120)
	h.emit(found("alpha/api"), began("alpha/api"), failedClone("alpha/api", "403"))

	want := components.RowStatusLine(
		components.DescribeItem(jobs.KindClone, h.item(0)).Detail, h.m.cloneStatusLine(), 120)
	line := footerLine(h, 120)
	if !strings.Contains(line, strings.TrimSpace(ansi.Strip(want))) {
		t.Errorf("footer = %q, want it to contain %q", line, ansi.Strip(want))
	}
	st := h.m.status()
	if st.Text != h.m.cloneStatusLine() || st.Detail.IsZero() {
		t.Errorf("status = %+v, want Text = progress and a non-zero Detail", st)
	}
}

func TestCloneFooterCauseCarriesNoSecret(t *testing.T) {
	h := cloningAt(t, 120)
	h.emit(found("alpha/api"), began("alpha/api"), failedClone("alpha/api", leakyCloneErr))
	line := footerLine(h, 120)
	for _, secret := range []string{"glpat-SECRET", "oauth2:"} {
		if strings.Contains(line, secret) {
			t.Errorf("footer leaks %q: %q", secret, line)
		}
	}
}

func TestCloneFooterDetailReturnsAfterTheEndOfRunError(t *testing.T) {
	h := cloningAt(t, 120)
	h.emit(found("alpha/api"), began("alpha/api"), failedClone("alpha/api", "403"))
	h.emit(CloneRunFinishedMsg{})

	if line := footerLine(h, 120); !strings.Contains(line, "alpha/api") || strings.Contains(line, "Done —") {
		t.Fatalf("the end-of-run Error is not on screen: %q", line)
	}
	h.m = feed(t, h.m, components.ClearFooterMsg{ID: h.m.footer.ID()})

	line := footerLine(h, 120)
	if !strings.Contains(line, "403") {
		t.Errorf("after the Error expired, the cause is missing: %q", line)
	}
	if !strings.Contains(line, "Done — ") {
		t.Errorf("after the Error expired, the progress is missing: %q", line)
	}
}

func TestCloneFooterKeepsTheProgressInEveryState(t *testing.T) {
	tests := []struct {
		name string
		evs  []tea.Msg
		want string // part of the row sentence
	}{
		{"queued", []tea.Msg{found("alpha/api")}, "Queued"},
		{"running", []tea.Msg{found("alpha/api"), began("alpha/api")}, "alpha/api"},
		{"cloned", []tea.Msg{found("alpha/api"), began("alpha/api"), ended("alpha/api")}, "finished"},
		{"already on disk", []tea.Msg{found("alpha/api"), began("alpha/api"), skippedClone("alpha/api")}, "already on disk"},
		{"cancelled", []tea.Msg{found("alpha/api"), nil}, "cancelled"},
	}
	for _, tc := range tests {
		for _, width := range []int{120, 60} {
			t.Run(tc.name, func(t *testing.T) {
				h := cloningAt(t, width)
				for _, ev := range tc.evs {
					if ev == nil { // the cancel of the open run
						h.reg.CancelOpen(jobs.KindClone)
						h.broadcast()
						continue
					}
					h.emit(ev)
				}
				line := footerLine(h, width)
				if p := h.m.cloneStatusLine(); !strings.Contains(line, p) {
					t.Errorf("%d cols: footer lacks the progress %q:\n%q", width, p, line)
				}
				if width == 120 && !strings.Contains(line, tc.want) {
					t.Errorf("120 cols: footer lacks %q:\n%q", tc.want, line)
				}
			})
		}
	}
}

func TestCloneShortcutKeysAreTheSameInEveryRowState(t *testing.T) {
	states := map[string][]tea.Msg{
		"queued":   {found("alpha/api")},
		"running":  {found("alpha/api"), began("alpha/api")},
		"cloned":   {found("alpha/api"), began("alpha/api"), ended("alpha/api")},
		"skipped":  {found("alpha/api"), began("alpha/api"), skippedClone("alpha/api")},
		"failed":   {found("alpha/api"), began("alpha/api"), failedClone("alpha/api", "403")},
		"finished": {found("alpha/api"), began("alpha/api"), failedClone("alpha/api", "403"), CloneRunFinishedMsg{}},
	}
	var want []string
	for _, name := range []string{"queued", "running", "cloned", "skipped", "failed", "finished"} {
		h := cloningAt(t, 120)
		h.emit(states[name]...)
		got := testutil.ShortcutKeys(h.m.GetShortcuts())
		if want == nil {
			want = got
			continue
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s row keys = %v, want %v", name, got, want)
		}
	}
}

func TestCloneFooterDetailFollowsTheCursor(t *testing.T) {
	h := cloningAt(t, 120)
	h.emit(found("alpha/api"), found("alpha/web"), began("alpha/api"), failedClone("alpha/api", "403"))

	if line := footerLine(h, 120); !strings.Contains(line, "403") {
		t.Fatalf("first row selected, footer lacks its cause: %q", line)
	}
	h.key("down")
	line := footerLine(h, 120)
	if strings.Contains(line, "403") {
		t.Errorf("after down, the footer still describes the first row: %q", line)
	}
	if !strings.Contains(line, "Queued") {
		t.Errorf("after down, the footer lacks the second row's sentence: %q", line)
	}
}

func TestCloneFooterOfAnEmptyListIsTheProgressAlone(t *testing.T) {
	h := cloningAt(t, 120)
	if st := h.m.status(); !st.Detail.IsZero() {
		t.Errorf("an empty list has a Detail: %+v", st.Detail)
	}
	line := strings.TrimSpace(footerLine(h, 120))
	if want := strings.TrimSpace(h.m.cloneStatusLine()); line != want {
		t.Errorf("footer = %q, want the progress alone %q", line, want)
	}
}

func TestCloneFooterHeightDoesNotDependOnTheRowState(t *testing.T) {
	states := [][]tea.Msg{
		{found("alpha/api")},
		{found("alpha/api"), began("alpha/api")},
		{found("alpha/api"), began("alpha/api"), ended("alpha/api")},
		{found("alpha/api"), began("alpha/api"), skippedClone("alpha/api")},
		{found("alpha/api"), began("alpha/api"), failedClone("alpha/api", "403")},
	}
	want := cloningAt(t, 60).m.GetFooterHeight()
	for i, evs := range states {
		h := cloningAt(t, 60)
		h.emit(evs...)
		if got := h.m.GetFooterHeight(); got != want {
			t.Errorf("state %d: GetFooterHeight = %d, want %d", i, got, want)
		}
		if n := strings.Count(h.m.RenderFooter(60), "\n") + 1; n != want {
			t.Errorf("state %d: RenderFooter has %d lines, want %d", i, n, want)
		}
	}
}
