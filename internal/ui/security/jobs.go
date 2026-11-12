package security

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/datatable"
)

// handleJobsChanged takes the router's snapshot and redecorates the rows. What
// is being rescanned is the registry's answer, not this view's (§3.58): nothing
// is carried across a reload, and a scan started from ws or the images tab
// marks the same row.
func (m Model) handleJobsChanged(msg jobs.ChangedMsg) (tea.Model, tea.Cmd) {
	m.jobs = msg.Runs
	m.failures = slices.Clone(msg.Failures)
	m.contextName = msg.Context
	m.setInventory(m.inventory.Items())
	return m, nil
}

// handleFrameMsg takes the animation's blink phase, which the table reads at
// render, and the footer's activity dot; no row is rebuilt. The view's own load
// and remediation spinner is a separate field and is not touched here.
func (m Model) handleFrameMsg(msg jobs.FrameMsg) (tea.Model, tea.Cmd) {
	m.inventory.SetBlinkPhase(msg.Blink)
	m.footer.SetBlinkPhase(msg.Blink)
	return m, nil
}

// ActivityTables returns the read-only face of the view's table, keyed by the
// name the activity-column guard (internal/app) knows it by. Tests only.
func (m Model) ActivityTables() map[string]datatable.ActivityTable {
	return map[string]datatable.ActivityTable{"sec/inventory": &m.inventory}
}

// scanRun builds the run behind ctrl+s and A. The label names the screen it was
// launched from, which is what `:jobs` shows.
func (m Model) scanRun(names []string) jobs.Run {
	return jobs.NewRun(jobs.KindScan, command.ViewSecurity, "", "inventory", names...)
}

// scanningTarget reports whether a target is queued or being scanned, whoever
// started it.
func (m Model) scanningTarget(name string) bool {
	return m.scanningTargets()[name]
}

// scanningTargets is every target with a scan in flight, from any view.
func (m Model) scanningTargets() map[string]bool {
	out := make(map[string]bool)
	for _, run := range jobs.Unfinished(m.jobs) {
		if run.Kind != jobs.KindScan {
			continue
		}
		for _, item := range run.Items {
			if !item.State.Terminal() {
				out[item.Target] = true
			}
		}
	}
	return out
}

// ── What each message tells the registry (jobs.Reporter) ─────────────────────

func (m InventoryScanStartingMsg) Transition() jobs.Transition {
	return jobs.Transition{Kind: jobs.KindScan, Target: m.Name, State: jobs.ItemRunning, Cancel: m.Cancel}
}

func (m InventoryScanFinishedMsg) Transition() jobs.Transition {
	t := jobs.Transition{Kind: jobs.KindScan, Target: m.Name, State: jobs.ItemDone}
	if m.Err != nil {
		t.State = jobs.ItemFailed
		t.Detail = "scan failed — check logs"
	}
	return t
}

// D10: rescanCmd had no starting message at all — only a finished one — so its
// items would have jumped straight from queued to done and the distinction
// between the two would have been decorative here.
var (
	_ jobs.Reporter = InventoryScanStartingMsg{}
	_ jobs.Reporter = InventoryScanFinishedMsg{}
)
