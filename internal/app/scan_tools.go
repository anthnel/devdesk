package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/shared"
)

// One detection of the scanners, for every view (§3.86).
//
// Each view that could start a scan used to run its own: `ws`, `oci`,
// `templates` and the dashboard, each a LookPath and a `--version` per tool,
// plus a `docker images` per tool in image mode — and four answers that could
// disagree about the same machine. The router now runs it, keeps the answer in
// shared.State.Tools, and hands it to every view.
//
// When: at start, on a context switch, when a saved configuration changes where
// a tool runs from (or the engine), and on request — ctrl+r on the dashboard.
// Not on a timer: a tool is installed far less often than the dashboard ticks.

// scanToolsDetectedMsg is a detection that finished. gen is the request it
// answers: one that is not the latest is dropped, or a slow detection started
// before a source change could land after the next one and show the old state.
type scanToolsDetectedMsg struct {
	gen    int
	report scan.Report
}

// detectScanTools starts a detection against the current tool settings. The
// settings are copied into the Cmd, which never touches the model (Rule 110).
func (a *App) detectScanTools() tea.Cmd {
	a.toolsGen++
	gen, tools := a.toolsGen, a.config.Scan.Tools
	a.toolsDetectedFor = tools
	return func() tea.Msg {
		return scanToolsDetectedMsg{gen: gen, report: scan.Detect(tools)}
	}
}

// forgetScanTools drops the current detection, for a context whose tools may
// run from elsewhere: the views say "not known yet" rather than show another
// context's answer. It also retires any detection in flight.
func (a *App) forgetScanTools() {
	a.toolsGen++
	a.sharedState.Tools = nil
}

// handleScanToolsDetected keeps the answer and hands it to every view.
func (a *App) handleScanToolsDetected(msg scanToolsDetectedMsg) (tea.Model, tea.Cmd) {
	if msg.gen != a.toolsGen {
		return a, nil
	}
	report := msg.report
	a.sharedState.Tools = &report
	return a, a.broadcastScanTools()
}

// redetectIfToolsMoved re-detects when a saved configuration changed where a
// tool runs from, or which engine runs images. Anything else a save touches —
// a category, a filter — changes what is required, which the views work out
// from the configuration without a new detection.
func (a *App) redetectIfToolsMoved(engineChanged bool) tea.Cmd {
	if !engineChanged && scan.SameDetection(a.toolsDetectedFor, a.config.Scan.Tools) {
		return nil
	}
	return a.detectScanTools()
}

// broadcastScanTools hands the current detection to every view the router
// holds, on screen or not — broadcastJobs's reasoning.
func (a *App) broadcastScanTools() tea.Cmd {
	msg := shared.ScanToolsMsg{Report: a.sharedState.Tools}
	cmds := make([]tea.Cmd, 0, len(a.views))
	for name, view := range a.views {
		updated, cmd := view.Update(msg)
		a.views[name] = updated
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// sendScanToolsTo hands the detection to one view: one built after it landed
// was not there to receive it.
func (a *App) sendScanToolsTo(target command.ViewType) {
	view, ok := a.views[target]
	if !ok || a.sharedState.Tools == nil {
		return
	}
	// A view stores the report and asks for nothing, so the Cmd is nil.
	a.views[target], _ = view.Update(shared.ScanToolsMsg{Report: a.sharedState.Tools})
}

// toolsState is the router's bookkeeping of detection, kept apart so App reads
// as the list of concerns it holds.
type toolsState struct {
	// toolsGen numbers the detection requests; only the latest one's answer is
	// kept.
	toolsGen int
	// toolsDetectedFor is the tool settings the latest detection ran against.
	toolsDetectedFor config.ScanTools
}
