package security

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The inventory is what `:sec` lands on: everything the two scan caches hold for
// this context, in one table. It replaces a form whose every option now comes
// from the configuration view, and whose target could only ever be a known image
// or something under workspaces_dir.
//
// It runs its own rescans rather than delegating them to the view a target came
// from: with the options in the config there is nothing left to carry across.

// handleInventoryState processes keys on the inventory.
func (m Model) handleInventoryState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.inventory.InEditMode() {
		return m, m.inventory.Update(msg)
	}
	switch msg.String() {
	case "enter":
		return m.openSelectedTarget()
	case keymap.Scan:
		return m.rescanSelected()
	case keymap.ScanAll:
		return m.confirmRescanAll()
	case "ctrl+r":
		return m, loadInventoryCmd()
	}
	return m, m.inventory.Update(msg)
}

// openSelectedTarget loads the stored result for the row under the cursor.
func (m Model) openSelectedTarget() (tea.Model, tea.Cmd) {
	target, ok := m.inventory.Selected()
	if !ok {
		return m, nil
	}
	if !target.Scanned {
		m.statusMessage = "No result yet for " + target.Name
		return m, clearStatusCmd()
	}
	return m, loadInventoryResultCmd(target)
}

// rescanSelected rescans the row under the cursor, overwriting its entry
// (Rule 126: ctrl+s overwrites where ctrl+a purges).
func (m Model) rescanSelected() (tea.Model, tea.Cmd) {
	target, ok := m.inventory.Selected()
	if !ok {
		return m, nil
	}
	if target.Scanning {
		m.statusMessage = "Scan already in progress"
		return m, clearStatusCmd()
	}
	tick := m.spinnerTickIfIdle()
	m.markScanning([]string{target.Name}, false)
	job := inventoryScanJob{Kind: target.Kind, Name: target.Name}
	return m, tea.Batch(tick, rescanCmd([]inventoryScanJob{job}, scan.OptionsFromConfig(m.config.Scan)))
}

// spinnerTickIfIdle restarts the spinner chain, unless something is already
// keeping it alive. handleSpinnerTick stops scheduling once nothing is running,
// so a rescan has to start it again — and starting a second chain alongside a
// live one makes the frames advance at twice the rate.
func (m Model) spinnerTickIfIdle() tea.Cmd {
	if m.inventoryScanning() {
		return nil
	}
	return m.spinner.Tick
}

// confirmRescanAll asks before rescanning everything, and the purge is the
// modal's checkbox rather than a second key.
//
// A and ctrl+a differed only by a modifier, and nothing in their shape said
// which one purged — the closest this application came to losing data by
// accident (§3.26). The destructive half is now a deliberate gesture, under the
// eyes of whoever triggers it.
func (m Model) confirmRescanAll() (tea.Model, tea.Cmd) {
	if len(m.inventory.Items()) == 0 {
		return m, nil
	}
	m.scanAllModal = sharedcomponents.NewOptionConfirmModal(
		"Scan All",
		fmt.Sprintf("Rescan all %d targets?", len(m.inventory.Items())),
		purgeCacheOptionLabel,
	)
	return m, nil
}

// purgeCacheOptionLabel names the destructive half in the one place three views
// show it, so they cannot word it differently.
const purgeCacheOptionLabel = "Purge cached results first"

// rescanAll rescans every target, purging their counts first when asked
// (Rule 126).
//
// The rows themselves stay: they are the list of what is known to have been
// scanned, and dropping them would leave an empty view for as long as the scans
// take — and lose the targets entirely if the application were closed meanwhile.
// It is the counts that are purged, on disk and on screen both.
func (m Model) rescanAll(purge bool) (tea.Model, tea.Cmd) {
	targets := m.inventory.Items()
	if len(targets) == 0 {
		return m, nil
	}
	jobs := make([]inventoryScanJob, 0, len(targets))
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		jobs = append(jobs, inventoryScanJob{Kind: t.Kind, Name: t.Name})
		names = append(names, t.Name)
	}
	tick := m.spinnerTickIfIdle()
	m.markScanning(names, purge)
	cmds := []tea.Cmd{tick}
	if purge {
		cmds = append(cmds, purgeInventoryCmd(jobs))
	}
	cmds = append(cmds, rescanCmd(jobs, scan.OptionsFromConfig(m.config.Scan)))
	return m, tea.Batch(cmds...)
}

// markScanning flags the named rows as scanning. purge also clears their counts,
// which is what makes a purged row print "-" rather than the number a scan is
// about to replace.
func (m *Model) markScanning(names []string, purge bool) {
	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}
	targets := m.inventory.Items()
	updated := make([]scanTarget, len(targets))
	for i, t := range targets {
		if wanted[t.Name] {
			t.Scanning = true
			t.Failed = false
			if purge {
				t.Scanned = false
				t.Counts = scan.SeverityCounts{}
			}
		}
		updated[i] = t
	}
	m.setInventory(updated)
}

// goHome returns to the inventory, which is the only landing state left now
// that the form is gone — `homeState` recorded which of the two it was.
//
// It reloads, because the caches are the source of truth and they move: a rescan
// just run here changed them, and so does a scan launched from the images or
// workspaces list while this view sat on a result.
func (m Model) goHome() (tea.Model, tea.Cmd) {
	m.state = StateInventory
	return m, loadInventoryCmd()
}

// handleInventoryLoaded installs the targets read from the caches.
//
// Rows currently being rescanned keep their in-flight state: the cache says
// nothing about a scan that has not finished writing to it, so a refresh landing
// mid-rescan would otherwise clear the spinner and leave the row looking settled.
func (m Model) handleInventoryLoaded(msg InventoryLoadedMsg) (tea.Model, tea.Cmd) {
	inFlight := make(map[string]bool)
	for _, t := range m.inventory.Items() {
		if t.Scanning {
			inFlight[t.Name] = true
		}
	}
	targets := make([]scanTarget, len(msg.Targets))
	for i, t := range msg.Targets {
		t.Scanning = inFlight[t.Name]
		targets[i] = t
	}
	m.setInventory(targets)
	return m, nil
}

// handleInventoryResultLoaded opens the results on a stored scan, or says why it
// could not. A missing file is not a dead end: the row is still there and
// ctrl+s rescans it.
func (m Model) handleInventoryResultLoaded(msg InventoryResultLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil || msg.Result == nil {
		m.statusMessage = "No stored result for " + msg.Name + " — ctrl+s to rescan"
		return m, clearStatusCmd()
	}
	m.result = msg.Result
	m.targetPath = msg.Name
	m.state = StateResults
	m.activeTab = TabCVE
	m.updateFindingsTable()
	return m, nil
}

// handleInventoryScanFinished folds one finished rescan back into its row.
func (m Model) handleInventoryScanFinished(msg InventoryScanFinishedMsg) (tea.Model, tea.Cmd) {
	targets := m.inventory.Items()
	updated := make([]scanTarget, len(targets))
	for i, t := range targets {
		if t.Name == msg.Name {
			t.Scanning = false
			t.Failed = msg.Err != nil
			if msg.Err == nil {
				t.Scanned = true
				t.Counts = msg.Counts
				t.Sensitive = msg.Sensitive
				t.ScannedAt = msg.ScannedAt
			}
		}
		updated[i] = t
	}
	m.setInventory(updated)
	if msg.Err != nil {
		m.statusMessage = fmt.Sprintf("Scan failed for %s — check logs", msg.Name)
		return m, clearStatusCmd()
	}
	return m, nil
}

// setInventory replaces the rows, stamping the current spinner frame on them so
// the ones being rescanned animate. The frame is read here rather than in a Cell
// function, which is built once and cannot reach the model.
//
// It is the spinner's *frame*, never its View(): the latter renders through
// SpinnerStyle, and a table cell carries no escape sequence (Rule 122). A styled
// frame measured 47 cells in a column 14 wide, so the cut landed inside the
// escape and the Scanned column rendered as nothing at all — a scan with no
// visible sign it was running. oci_resources and workspaces already stamp the
// bare frame; this was the one that did not.
func (m *Model) setInventory(targets []scanTarget) {
	frame := spinner.Dot.Frames[m.spinnerFrameIdx%len(spinner.Dot.Frames)]
	stamped := make([]scanTarget, len(targets))
	for i, t := range targets {
		t.SpinnerFrame = frame
		stamped[i] = t
	}
	m.inventory.SetItems(stamped)
}

// inventoryScanning reports whether any row is being rescanned, which is what
// keeps the spinner ticking outside StateScanning.
func (m Model) inventoryScanning() bool {
	for _, t := range m.inventory.Items() {
		if t.Scanning {
			return true
		}
	}
	return false
}

// renderInventoryView renders the table, or says why there is nothing in it.
func (m Model) renderInventoryView() string {
	if len(m.inventory.Items()) == 0 {
		return m.renderEmptyInventory()
	}
	return m.inventory.View()
}

// renderEmptyInventory explains where scans come from. Nothing scanned yet is
// the normal first state of this view, not an error.
func (m Model) renderEmptyInventory() string {
	lines := []string{
		theme.EmptyLineBg(m.width),
		theme.PadWithBg(theme.Bg("  ")+theme.SubTitleStyle.Render(theme.IconSecurity+" Nothing scanned yet"), m.width),
		theme.EmptyLineBg(m.width),
		theme.PadWithBg(theme.Bg("  ")+theme.DimStyle.Render("Scan an image from the OCI resources view, or a repository from the workspaces view."), m.width),
		theme.PadWithBg(theme.Bg("  ")+theme.DimStyle.Render("What they find is listed here."), m.width),
	}
	// strings.Join rather than lipgloss.JoinVertical, which inserts bare spaces
	// the application background does not reach (Rule 115).
	return lipgloss.NewStyle().Background(theme.ColorBackground).Render(strings.Join(lines, "\n"))
}
