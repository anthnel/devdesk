package explorer

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-147 / DEVDESK-224 step 4: the footer says what the selected row is
// doing (Status.Detail), and a failure is a 3-second event built by
// components.FailureEvent. Q1=C: a failed create is an event only, never a row.

// selected moves the cursor to a path and fails if that row is not there.
func selected(t *testing.T, m Model, path string) Model {
	t.Helper()
	m.selectRow(path)
	if node, ok := m.selectedNode(); !ok || node.FullPath != path {
		t.Fatalf("cannot select %q", path)
	}
	return m
}

func withRuns(t *testing.T, m Model, kind jobs.Kind, target string) Model {
	t.Helper()
	return feed(t, m, jobs.ChangedMsg{Runs: actRunning(t, kind, actCtx, target), Context: actCtx})
}

func withFailure(t *testing.T, m Model, kind jobs.Kind, target, detail string) Model {
	t.Helper()
	return feed(t, m, jobs.ChangedMsg{
		Failures: []jobs.Failure{actFailure(kind, actCtx, target, detail)}, Context: actCtx,
	})
}

func TestStatusDetailForARunningDelete(t *testing.T) {
	m := selected(t, withRuns(t, drilledModel(t), jobs.KindDelete, "alpha/api"), "alpha/api")

	st := m.status()
	if got := st.Detail.String(); got != "● Deleting alpha/api" {
		t.Errorf("Status.Detail = %q, want %q", got, "● Deleting alpha/api")
	}
	if st.Level != components.LevelInfo {
		t.Errorf("Status.Level = %v, want Info", st.Level)
	}
	if footer := m.RenderFooter(160); !strings.Contains(footer, "● Deleting alpha/api") {
		t.Errorf("the footer does not carry the row detail:\n%s", footer)
	}
}

func TestStatusDetailJoinsTheUpdatingLine(t *testing.T) {
	m := selected(t, withRuns(t, drilledModel(t), jobs.KindDelete, "alpha/api"), "alpha/api")
	m.refreshing = 1

	if footer := m.RenderFooter(160); !strings.Contains(footer, "● Deleting alpha/api · Updating from GitLab...") {
		t.Errorf("detail and progress are not on one line:\n%s", footer)
	}
}

func TestStatusDetailForAFailedDelete(t *testing.T) {
	m := selected(t, withFailure(t, drilledModel(t), jobs.KindDelete, "alpha/api", "check logs"), "alpha/api")

	if got, want := m.status().Detail.String(), "⊗ alpha/api: last delete failed — check logs"; got != want {
		t.Errorf("Status.Detail = %q, want %q", got, want)
	}
	// Another idle row says nothing.
	m = selected(t, m, "alpha/sub")
	if d := m.status().Detail; !d.IsZero() {
		t.Errorf("idle row detail = %q, want none", d.String())
	}
}

func TestTheGlobalLoadKeepsItsLine(t *testing.T) {
	m := newTestModel(t)
	st := m.status()
	if !st.Activity || st.Spinner || st.Text != "Loading GitLab groups..." {
		t.Errorf("first load status = %+v, want the activity dot and %q (criterion 2)", st, "Loading GitLab groups...")
	}
	if !st.Detail.IsZero() {
		t.Errorf("first load carries a row detail: %q", st.Detail.String())
	}
}

func TestSelectionModeShowsNoRowDetail(t *testing.T) {
	m := selected(t, withFailure(t, drilledModel(t), jobs.KindDelete, "alpha/api", "check logs"), "alpha/api")
	m = feed(t, m, testutil.Key(keymap.Clone))
	if m.mode != ModeSelecting {
		t.Fatalf("mode = %v, want ModeSelecting", m.mode)
	}
	st := m.status()
	if !st.Detail.IsZero() {
		t.Errorf("selection mode carries a detail: %q (H2)", st.Detail.String())
	}
	if st.Text != m.selectionStatusLine() {
		t.Errorf("Status.Text = %q, want the selection line", st.Text)
	}
}

func TestADeleteFailureIsAFooterEventThenAPersistentDetail(t *testing.T) {
	m := selected(t, drilledModel(t), "alpha/api")
	m = feed(t, m, DeleteCompleteMsg{Target: "alpha/api", Error: errors.New("boom")})

	if got, want := m.footer.Text(), "Failed to delete alpha/api — check logs"; got != want {
		t.Errorf("footer = %q, want %q", got, want)
	}
	if m.footer.Level() != components.LevelError {
		t.Errorf("footer level = %v, want Error", m.footer.Level())
	}

	// The registry would now report the failure; the event then expires.
	m = withFailure(t, m, jobs.KindDelete, "alpha/api", "check logs")
	m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
	if m.footer.IsSet() {
		t.Fatalf("the event did not expire: %q", m.footer.Text())
	}
	if got, want := m.status().Detail.String(), "⊗ alpha/api: last delete failed — check logs"; got != want {
		t.Errorf("after the event, Status.Detail = %q, want %q", got, want)
	}
}

func TestACreateFailureIsAnEventAndNeverARow(t *testing.T) {
	tests := []struct {
		name string
		msg  func(target string) any
		want string
	}{
		{"group", func(p string) any { return GroupCreatedMsg{Target: p, Error: errors.New("boom")} },
			"Failed to create alpha/new — check logs"},
		{"project", func(p string) any { return ProjectCreatedMsg{Target: p, Error: errors.New("boom")} },
			"Failed to create alpha/new — check logs"},
		{"project, template unavailable", func(p string) any {
			return ProjectCreatedMsg{Target: p, Error: errors.New("boom"), TemplateUnavailable: true}
		}, "Failed to create alpha/new — template unavailable — nothing was created"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := drilledModel(t)
			m = feed(t, m, components.CreationFormSubmitMsg{FormType: components.FormTypeGroup, Name: "new"})
			m = withRuns(t, m, jobs.KindCreate, "alpha/new")
			m = feed(t, m, tt.msg("alpha/new"))

			if got := m.footer.Text(); got != tt.want {
				t.Errorf("footer = %q, want %q", got, tt.want)
			}
			if m.footer.Level() != components.LevelError {
				t.Errorf("footer level = %v, want Error", m.footer.Level())
			}
			if _, ok := rowFor(m, "alpha/new"); ok {
				t.Error("the placeholder row is still there")
			}
			// Even if the registry remembers the failure, no row shows it (Q1=C).
			m = withFailure(t, m, jobs.KindCreate, "alpha/new", "create failed — check logs")
			for _, l := range strings.Split(m.table.View(), "\n") {
				if anyGlyph(l) {
					t.Errorf("a row carries an activity glyph after a failed create:\n%s", l)
				}
			}
		})
	}
}

// Rule 130: the set of keys never changes with the row's state; only the
// greyed state does.
func TestShortcutKeysAreTheSameOnIdleRunningAndFailedRows(t *testing.T) {
	idle := selected(t, drilledModel(t), "alpha/api")
	running := selected(t, withRuns(t, drilledModel(t), jobs.KindDelete, "alpha/api"), "alpha/api")
	failed := selected(t, withFailure(t, drilledModel(t), jobs.KindDelete, "alpha/api", "check logs"), "alpha/api")

	want := testutil.ShortcutKeys(idle.GetShortcuts())
	for name, m := range map[string]Model{"running": running, "failed": failed} {
		got := testutil.ShortcutKeys(m.GetShortcuts())
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s row keys = %v, want %v", name, got, want)
		}
	}
	for name, m := range map[string]Model{"idle": idle, "running": running, "failed": failed} {
		if testutil.HasShortcut(m.GetShortcuts(), "K") {
			t.Errorf("%s row advertises K; the explorer offers none", name)
		}
	}
	if !testutil.ShortcutDisabled(running.GetShortcuts(), keymap.Delete) {
		t.Error("D is not greyed on a row whose delete is running")
	}
	for name, m := range map[string]Model{"idle": idle, "failed": failed} {
		if !testutil.ShortcutEnabled(m.GetShortcuts(), keymap.Delete) {
			t.Errorf("D is not enabled on the %s row", name)
		}
	}
}

// 40 columns (review of step 4): the footer is one line, the detail gives way
// first, and the height the router budgeted does not move.
func TestFooterAt40ColumnsTruncatesTheDetailFirst(t *testing.T) {
	const cause = "check logs and the very long cause that cannot fit in forty columns"
	base := selected(t, withFailure(t, drilledModel(t), jobs.KindDelete, "alpha/api", cause), "alpha/api")
	wantHeight := base.GetFooterHeight()

	tests := []struct {
		name       string
		refreshing int
		keep       string // must survive whole
	}{
		{"detail alone", 0, "⊗"},
		{"detail and the Updating line", 1, "Updating from GitLab..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := base
			m.refreshing = tt.refreshing

			footer := m.RenderFooter(40)
			if got := strings.Count(footer, "\n") + 1; got != m.GetFooterHeight() {
				t.Errorf("RenderFooter(40) emitted %d lines, GetFooterHeight() = %d", got, m.GetFooterHeight())
			}
			if got := m.GetFooterHeight(); got != wantHeight {
				t.Errorf("GetFooterHeight() = %d with %q, %d without: a long detail changed the footer", got, tt.name, wantHeight)
			}
			info := ansi.Strip(m.renderInfoLine(40))
			if strings.Contains(info, "\n") {
				t.Errorf("the info line wrapped:\n%s", info)
			}
			if w := theme.StringWidth(info); w > 40 {
				t.Errorf("the info line is %d columns wide, want at most 40: %q", w, info)
			}
			if strings.Contains(info, "forty columns") {
				t.Errorf("the detail was not truncated: %q", info)
			}
			if !strings.Contains(info, tt.keep) {
				t.Errorf("the info line lost %q (the detail must give way first): %q", tt.keep, info)
			}
		})
	}
}
