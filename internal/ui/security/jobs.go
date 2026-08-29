package security

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
)

// What is being rescanned is the registry's answer, not this view's (§3.58).
//
// `scanTarget.Scanning` was a flag this view set and cleared itself, and
// `handleInventoryLoaded` had to carry it across every reload by hand: the
// caches say nothing about a scan that has not finished writing to them, so a
// refresh landing mid-rescan cleared the spinner and left the row looking
// settled. Reading the snapshot removes the reconciliation rather than fixing
// it — there is nothing to carry across a reload when the state was never here.
//
// It also makes the marker true for a scan started **elsewhere**. The inventory
// lists what `ws` and the images tab scan; a rescan launched from either of them
// is the same work on the same cache entry, and this view had no way to know.

// handleJobsChanged takes the router's snapshot and redecorates the rows.
func (m Model) handleJobsChanged(msg jobs.ChangedMsg) (tea.Model, tea.Cmd) {
	m.jobs = msg.Runs
	m.jobFrame = msg.Frame
	m.setInventory(m.inventory.Items())
	return m, nil
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
