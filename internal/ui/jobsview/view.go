package jobsview

import (
	"strconv"
	"strings"

	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// GetTitle names the view, and the run when one is open.
func (m Model) GetTitle() string {
	base := theme.IconHourglass + " Jobs"
	if run, ok := m.openedRun(); ok && m.level == levelItems {
		return base + " " + theme.IconChevronRight + " " + run.Label
	}
	return base
}

// GetIcon returns the view icon. The title carries it, as everywhere else.
func (m Model) GetIcon() string { return "" }

// GetHeaderInfo says how much of the session is on screen.
//
// Running first, because it is why the view is open. The total is the runs of
// this context, which is what the table holds — not the registry's, which also
// counts the ones a context switch is hiding.
func (m Model) GetHeaderInfo(_ string) []shortcut.HeaderInfo {
	runs := m.visibleRuns()
	return []shortcut.HeaderInfo{
		{Key: "Running", Value: strconv.Itoa(len(jobs.Unfinished(runs))), Style: theme.HeaderValueStyle},
		{Key: "Runs", Value: strconv.Itoa(len(runs)), Style: theme.HeaderValueStyle},
	}
}

// GetShortcuts returns the header's shortcut column (Rules 130, 137, 138).
//
// The obvious keys are absent: ↑↓, PageUp/PageDown, Home/End. What is left is
// the drill, the sort, the filter and help — and the three that vary are greyed
// rather than dropped, so the column is the same four lines at both levels.
func (m Model) GetShortcuts() shortcut.Shortcuts {
	a := m.availability()
	return []shortcut.Shortcut{
		{Key: keymap.Kill, Description: "Stop", Disabled: !a.Stop.Enabled()},
		{Key: "→", Description: "Open targets", Disabled: !a.Open.Enabled()},
		{Key: "esc", Description: "Go back", Disabled: !a.Close.Enabled()},
		{Key: ".", Description: "Sort", Disabled: !a.Sort.Enabled()},
		{Key: "/", Description: "Filter"},
		{Key: "?", Description: "Help"},
	}
}

// GetFooterHeight budgets the footer (Rules 124, 136).
//
// The breadcrumb is a line of the footer at the item level, and the filter bar
// adds its own when visible. Both are asked for here so the router can take
// them off the viewport.
func (m Model) GetFooterHeight() int {
	height := 2 + m.filterBar().ExtraHeight()
	if m.level == levelItems {
		height++
	}
	return height
}

// RenderFooter renders the lines below the viewport (Rule 124).
//
// Order: breadcrumb, filter bar, blank, message. The breadcrumb sits directly
// under the table because it names the level the table is showing (Rule 123).
func (m Model) RenderFooter(width int) string {
	var parts []string
	if m.level == levelItems {
		parts = append(parts, m.renderBreadcrumb(width))
	}
	if bar := m.filterBar(); bar.IsVisible() {
		parts = append(parts, bar.View())
	}
	parts = append(parts, theme.EmptyLineBg(width), m.footer.View(width, m.status()))
	return strings.Join(parts, "\n")
}

// renderBreadcrumb is the trail below the table at the item level (Rule 123).
// Breadcrumb mode: it is not navigable, the level shown is highlighted and the
// one above it is dimmed.
func (m Model) renderBreadcrumb(width int) string {
	run, ok := m.openedRun()
	if !ok {
		return theme.EmptyLineBg(width)
	}
	tabs := []theme.TabItem{
		{Label: "Runs"},
		{Label: string(run.Kind) + " " + theme.IconChevronRight + " " + run.Label},
	}
	return theme.PadWithBg(theme.Bg(" ")+theme.RenderTabs(tabs, 1), width)
}

// status is the derived line, with no timer (Rule 128).
//
// It is the shared sentence every other footer says while work runs, and it is
// asked for with no origin: this view launched none of it, so the detailed form
// would be a lie about ownership. What it says instead is the count — which is
// also the one form that does not end in ":jobs for details", because that is
// where the reader already is.
func (m Model) status() sharedcomponents.Status {
	live := jobs.Unfinished(m.visibleRuns())
	if len(live) == 0 {
		return sharedcomponents.Status{}
	}
	return sharedcomponents.Status{
		Text:    sharedcomponents.Plural(len(live), "job", "jobs") + " running",
		Spinner: true,
	}
}

// View renders the table of the level on screen.
//
// There is no loading branch: this view fetches nothing, so it is never between
// a request and an answer. A table with nothing to show still renders its
// header and no rows (datatable.View() does this on its own) — the count that
// says why (zero runs, or a filter matching none of them) lives in the header
// (GetHeaderInfo), not in the body.
func (m Model) View() string {
	if m.level == levelItems {
		return m.itemTable.View()
	}
	return m.runTable.View()
}

// GetHelpContent returns the help for `?` (Rule 114).
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title: "Jobs",
		Description: "Everything long-running the application has started this session: scans, syncs, clones, image pulls and deletes. " +
			"A run is one batch started in one go from one view; press → to see the targets it holds. " +
			"This view starts nothing and owns nothing — it reads the same record every other view reads.",
		KeyBindings: []help.KeyBinding{
			{Key: keymap.Kill, Description: "Stop the selected run, or the selected target inside one"},
			{Key: "→", Description: "Open the selected run and list its targets"},
			{Key: "esc", Description: "Go back to the list of runs"},
			{Key: ".", Description: "Cycle the sort column (runs only). Each press toggles asc/desc, then moves to the next column"},
			{Key: "/", Description: "Filter by kind, state, label or target"},
			{Key: "↑/↓", Description: "Move the selection"},
			{Key: "ctrl+p", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Runs",
				Body: "State (first, untitled): a spinner while the run is queued or going, " +
					theme.IconOK + " done, " + theme.IconError + " failed, " + theme.IconCanceled + " cancelled.\n" +
					"Kind: what the run does — scan, sync, clone, pull, delete.\n" +
					"Label: what the batch was launched on — the directory, the registry, the repository.\n" +
					"Progress: targets settled over targets held. A target that was skipped or that failed has settled: progress reports on what is left to wait for, not on what succeeded.\n" +
					"State: queued, running, done, failed or cancelled. A run is failed as soon as one of its targets is, and cancelled beats failed — a run stopped on purpose reads cancelled even though its targets failed because of it.\n" +
					"Started: when the run was admitted, relative.",
			},
			{
				Title: "Targets",
				Body: "State (first, untitled): " + theme.IconPending + " queued, a spinner while running, " +
					theme.IconOK + " done, " + theme.IconSkipped + " skipped, " + theme.IconError + " failed.\n" +
					"Target: the repository path, the image reference, the group.\n" +
					"State: queued and running are different things — queued is waiting for a worker, running is holding one. A batch of twelve on four workers is four running and eight queued.\n" +
					"Detail: what the state alone cannot say — why a sync was skipped, that a scan failed. A dash means there was nothing to add.\n\n" +
					"The order is the one the batch was dispatched in, and it does not change: sorting by state would move a row out from under the cursor every time one settled.",
			},
			{
				Title: "What is listed",
				Body: "Runs of the current context only. A run is stamped with the context it started in and kept for the session, so switching context hides them rather than dropping them — switching back brings them back.\n\n" +
					"The last 20 settled runs are kept. A run still going is never dropped, however many have settled.",
			},
			{
				Title: "Stopping work",
				Body: "'" + keymap.Kill + "' on a run stops its queue: nothing further starts, whatever the kind. Targets still waiting are marked skipped, and the ones already running report their own outcome when they get there — a run that was stopped reads 'cancelled' rather than 'done', which is the distinction the list exists to keep.\n\n" +
					"'" + keymap.Kill + "' on a single target is offered only where cutting the work leaves nothing behind: a scan and an image pull can be cut, a clone cannot — a half-written repository on disk is worse than one that finished. A sync waits out the fetch in flight, and a delete is never cut at all.\n\n" +
					"The key is greyed when there is nothing to stop, and pressing it then says why in the footer rather than doing nothing.",
			},
			{
				Title: "Where the rows come from",
				Body: "The router owns one record of what is running and hands every view a copy of it. That is why a scan started in the workspaces list shows up here, and why the security inventory marks a row that this view also lists — it is the same work, recorded once.\n\n" +
					"Nothing on this screen is fetched, so there is nothing to refresh: the rows change when the work does.",
			},
		},
	}
}
