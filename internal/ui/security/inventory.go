package security

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/forge/session"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/registryalias"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
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
		return m.reloadInventory()
	}
	return m, m.inventory.Update(msg)
}

// openSelectedTarget loads the stored result for the row under the cursor.
func (m Model) openSelectedTarget() (tea.Model, tea.Cmd) {
	if open := m.canOpenTarget(); !open.Enabled() {
		return m, m.footer.Warn(open.Reason)
	}
	target, _ := m.inventory.Selected()
	return m, loadInventoryResultCmd(target)
}

// The three inventory actions, and why each does not apply. They are the one
// place the header reads to grey and the handlers read to refuse (Rule 130).
const (
	reasonNoTarget   = "No target selected"
	reasonEmptyList  = "Nothing has been scanned in this context yet"
	reasonNotScanned = "No result yet — press S to scan it"
)

// canOpenTarget reports whether enter has findings to open.
func (m Model) canOpenTarget() shortcut.Availability {
	target, ok := m.inventory.Selected()
	switch {
	case !ok:
		return shortcut.Unavailable(reasonNoTarget)
	case !target.Scanned:
		return shortcut.Unavailable(reasonNotScanned)
	}
	return shortcut.Availability{}
}

// canRescanSelected reports whether S has a row to act on.
func (m Model) canRescanSelected() shortcut.Availability {
	if _, ok := m.inventory.Selected(); !ok {
		return shortcut.Unavailable(reasonNoTarget)
	}
	return shortcut.Availability{}
}

// canRescanAll reports whether A has anything to rescan. An empty inventory is
// not a failure — it is a context nobody has scanned yet.
func (m Model) canRescanAll() shortcut.Availability {
	if len(m.inventory.Items()) == 0 {
		return shortcut.Unavailable(reasonEmptyList)
	}
	return shortcut.Availability{}
}

// rescanSelected rescans the row under the cursor, overwriting its entry
// (Rule 126: ctrl+s overwrites where ctrl+a purges).
func (m Model) rescanSelected() (tea.Model, tea.Cmd) {
	if rescan := m.canRescanSelected(); !rescan.Enabled() {
		return m, m.footer.Warn(rescan.Reason)
	}
	target, _ := m.inventory.Selected()
	if target.Scanning {
		return m, m.footer.Warn("Scan already in progress")
	}
	tick := m.spinnerTickIfIdle()
	m.markScanning([]string{target.Name}, false)
	job := inventoryScanJob{Kind: target.Kind, Name: target.Name}
	return m, tea.Batch(tick, rescanCmd([]inventoryScanJob{job}, m.scanOptions()))
}

// spinnerTickIfIdle restarts the spinner chain, unless something is already
// keeping it alive. handleSpinnerTick stops scheduling once nothing is running,
// so a rescan has to start it again — and starting a second chain alongside a
// live one makes the frames advance at twice the rate.
func (m Model) spinnerTickIfIdle() tea.Cmd {
	if m.spinnerAlive() {
		return nil
	}
	return m.spinner.Tick
}

// spinnerAlive is the one predicate deciding whether the frames keep coming:
// handleSpinnerTick reads it to schedule the next one, spinnerTickIfIdle to
// refuse starting a second chain. Two conditions answer it — a rescan stamping
// rows, and a load spinning in the footer — and asking them separately in the
// two places is what would let one restart a chain the other is running.
func (m Model) spinnerAlive() bool {
	return m.inventoryLoading || m.inventoryScanning()
}

// reloadInventory re-reads the caches and says so in the footer.
//
// The tick is taken **before** the flag flips: spinnerTickIfIdle reads
// spinnerAlive, so setting the flag first would make it answer "already
// running" about the chain this reload is trying to start.
func (m Model) reloadInventory() (tea.Model, tea.Cmd) {
	tick := m.spinnerTickIfIdle()
	m.inventoryLoading = true
	return m, tea.Batch(tick, loadInventoryCmd(m.ciForgeURL()))
}

// confirmRescanAll asks before rescanning everything, and the purge is the
// modal's checkbox rather than a second key.
//
// A and ctrl+a differed only by a modifier, and nothing in their shape said
// which one purged — the closest this application came to losing data by
// accident (§3.26). The destructive half is now a deliberate gesture, under the
// eyes of whoever triggers it.
func (m Model) confirmRescanAll() (tea.Model, tea.Cmd) {
	if all := m.canRescanAll(); !all.Enabled() {
		return m, m.footer.Warn(all.Reason)
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
	cmds = append(cmds, rescanCmd(jobs, m.scanOptions()))
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
	return m.reloadInventory()
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
	m.inventoryLoading = false
	m.setInventory(targets)
	return m, nil
}

// handleInventoryResultLoaded opens the results on a stored scan, or says why it
// could not. A missing file is not a dead end: the row is still there and
// ctrl+s rescans it.
func (m Model) handleInventoryResultLoaded(msg InventoryResultLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil || msg.Result == nil {
		return m, m.footer.Warn("No stored result for " + m.labelFor(msg.Name) + " — ctrl+s to rescan")
	}
	m.result = msg.Result
	// targetPath stays the cache key: it is what AddToGitleaksIgnore writes
	// into. The label is the folded form, and only the title reads it.
	m.targetPath = msg.Name
	m.targetLabel = m.labelFor(msg.Name)
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
				t.CIScore = msg.CIScore
				t.ScannedAt = msg.ScannedAt
			}
		}
		updated[i] = t
	}
	m.setInventory(updated)
	if msg.Err != nil {
		return m, m.footer.Error(fmt.Sprintf("Scan failed for %s — check logs", m.labelFor(msg.Name)))
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
	// The alias is stamped here for the same reason as the frame, and it is the
	// only place that can: the rows come from a Cmd, which must not read the
	// model, and the Target column is built once in New with nothing to reach.
	aliases := registryalias.From(m.config.Registry.Registries)
	stamped := make([]scanTarget, len(targets))
	for i, t := range targets {
		t.SpinnerFrame = frame
		if t.Kind == kindImage {
			t.Display = docker.ApplyAliases(t.Name, aliases)
		}
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
// renderInventoryView keeps the table on screen while the caches are read
// (Rule 139). The empty message is conditioned on the load being over, because
// before that the view does not know whether anything was scanned — and saying
// so anyway is what made "Nothing scanned yet" flash before the list.
func (m Model) renderInventoryView() string {
	if !m.inventoryLoading && len(m.inventory.Items()) == 0 {
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

// scanOptions is the one place this view assembles scan options, so the forge
// token loader cannot be forgotten at either of the two sites that rescan.
func (m Model) scanOptions() scan.ScanOptions {
	opts := scan.OptionsFromConfig(m.config)
	opts.LoadForgeToken = forgeTokenLoader(m.secrets, m.config.Forge.URL)
	return opts
}

// forgeTokenLoader loads the context's forge token once, on the scan goroutine.
// A store or a URL that is not there yields no token rather than an error: a
// scan without one still runs, and plumber says so itself by withholding the
// score.
func forgeTokenLoader(storage credentials.Storage, forgeURL string) func() string {
	if storage == nil || forgeURL == "" {
		return func() string { return "" }
	}
	var once sync.Once
	var token string
	return func() string {
		once.Do(func() {
			loaded, err := session.NewAuth(storage).LoadCredentials(forgeURL)
			if err != nil {
				log.Printf("ERROR [security/inventory] loading the forge token: %v", err)
				return
			}
			token = loaded
		})
		return token
	}
}
