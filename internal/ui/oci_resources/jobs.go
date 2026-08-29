package ociresources

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/registryalias"
)

// What is being scanned is the registry's answer, not this view's (§3.58).
//
// It held a `scanningImages` map and a `scanning` bool derived from it, and both
// knew only what this view had launched. An image scanned from `:sec` — the same
// image, the same cache entry — went unnoticed here, so `S` offered itself on a
// row already being scanned and the two wrote the same file.
//
// The view's own `spinner.Model` stays, and deliberately: it animates *loading*
// — the image list, the networks, a `docker` action on a row — and a load is not
// a job. What moved is the frame the scan cells use, which now comes from the
// one chain the router holds (D5).

// handleJobsChanged takes the router's snapshot. It is the only writer of the
// two fields.
func (m Model) handleJobsChanged(msg jobs.ChangedMsg) (tea.Model, tea.Cmd) {
	m.jobs = msg.Runs
	m.jobFrame = msg.Frame
	// The footer's spinner is deliberately not touched: here it belongs to
	// *loading* — a tab reading its list — and this view's scan progress is on
	// the rows, not in the footer. Two writers of one field would fight every
	// frame a scan and a load overlapped.
	m.updateImageTable()
	if m.registryBrowser != nil {
		m.registryBrowser.SetTagScanningSet(m.scanningNames())
	}
	return m, nil
}

// scanRun builds the run behind S, A, ctrl+a and a direct scan from the browser.
func (m Model) scanRun(names []string) jobs.Run {
	run := jobs.NewRun(jobs.KindScan, command.ViewOCIResources, "", "images", names...)
	// The alias is applied here rather than by whoever shows the run: it is
	// this view's configuration that defines it, and `:jobs` has no business
	// reading a registry list to print a name.
	aliases := registryalias.From(m.registries)
	for _, name := range names {
		if display := docker.ApplyAliases(name, aliases); display != name {
			run = run.WithDisplay(name, display)
		}
	}
	return run
}

// scanningImage reports whether an image is queued or being scanned, whoever
// started it.
func (m Model) scanningImage(name string) bool {
	for _, run := range jobs.Unfinished(m.jobs) {
		if run.Kind != jobs.KindScan {
			continue
		}
		for _, item := range run.Items {
			if item.Target == name && !item.State.Terminal() {
				return true
			}
		}
	}
	return false
}

// scanningNames lists every image with a scan in flight, for the registry
// browser, which decorates its tag rows from the same source.
func (m Model) scanningNames() map[string]bool {
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

// anyScanRunning reports whether a scan is going anywhere. It is what A and
// ctrl+a refuse on, and it is true for a scan this view did not start — which
// is the point: the two would write the same cache entries.
func (m Model) anyScanRunning() bool { return len(m.scanningNames()) > 0 }

// ── What each message tells the registry (jobs.Reporter) ─────────────────────

func (m ImageScanStartingMsg) Transition() jobs.Transition {
	return jobs.Transition{Kind: jobs.KindScan, Target: m.ImageName, State: jobs.ItemRunning}
}

func (m ImageScanFinishedMsg) Transition() jobs.Transition {
	t := jobs.Transition{Kind: jobs.KindScan, Target: m.ImageName, State: jobs.ItemDone}
	if m.Err != nil {
		t.State = jobs.ItemFailed
		t.Detail = "scan failed — check logs"
	}
	return t
}

// Compile-time proof that both progress messages can be applied. One added
// without a transition would leave its row spinning for the life of the view.
var (
	_ jobs.Reporter = ImageScanStartingMsg{}
	_ jobs.Reporter = ImageScanFinishedMsg{}
)
