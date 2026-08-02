package ociresources

import (
	"fmt"
	"log"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// clearInfoMsgMsg is sent after a delay to clear the transient footer info message.
type clearInfoMsgMsg struct{}

// clearInfoMsgCmd returns a command that clears the footer info message after 3 seconds.
func clearInfoMsgCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return clearInfoMsgMsg{}
	})
}

// Init initializes the OCI resources view
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		tickCmd(),
		fetchImages(),
		loadScanCache(),
		fetchNetworks(),
		fetchVolumes(),
	)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case ImageScanStartingMsg:
		return m.handleImageScanStarting(msg)

	case clearInfoMsgMsg:
		m.infoMsg = ""
		m.errorMsg = ""

	case ImageScanFinishedMsg:
		return m.handleImageScanFinished(msg)

	case LaunchBatchScanMsg:
		return m.handleLaunchBatchScan(msg)

	case LaunchSingleImageScanMsg:
		return m.handleLaunchSingleImageScan(msg)

	case spinner.TickMsg:
		var cmds []tea.Cmd
		if m.loading || m.scanning || m.loadingNets || m.loadingVols || m.loadingRegs {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			m.spinnerFrameIdx = (m.spinnerFrameIdx + 1) % len(spinner.Dot.Frames)
			cmds = append(cmds, cmd)
		}
		if m.registryBrowser != nil {
			var cmd tea.Cmd
			m.registryBrowser, cmd = m.registryBrowser.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if len(cmds) > 0 {
			return m, tea.Batch(cmds...)
		}

	case RefreshTickMsg:
		return m, tea.Batch(fetchImages(), loadScanCache(), fetchNetworks(), fetchVolumes())

	case ImagesListMsg:
		return m.handleImagesList(msg)

	case ScanCacheLoadedMsg:
		return m.handleScanCacheLoaded(msg)

	case ImageActionMsg:
		return m.handleImageAction(msg)

	case PruneCompleteMsg:
		return m.handlePruneComplete(msg)

	case NetworksListMsg:
		return m.handleNetworksList(msg)

	case NetworkActionMsg:
		return m.handleNetworkAction(msg)

	case NetworkPruneCompleteMsg:
		return m.handleNetworkPruneComplete(msg)

	case VolumesListMsg:
		return m.handleVolumesList(msg)

	case VolumeActionMsg:
		return m.handleVolumeAction(msg)

	case VolumePruneCompleteMsg:
		return m.handleVolumePruneComplete(msg)

	case ImageExposedPortsMsg:
		return m.handleImageExposedPorts(msg)

	case LaunchOptionsCacheLoadedMsg:
		return m.handleLaunchOptionsCacheLoaded(msg)

	case ContainerLaunchCompleteMsg:
		return m.handleContainerLaunchComplete(msg)

	case ClipboardCopyMsg:
		return m.handleClipboardCopy(msg)

	case LaunchFormCancelMsg:
		m.launchForm = nil
		return m, nil

	case ResourceFormCancelMsg:
		m.resourceForm = nil
		return m, nil

	case LaunchFormSubmitMsg:
		return m.handleLaunchFormSubmitWithCache(msg)

	case ResourceFormSubmitMsg:
		return m.handleResourceFormSubmit(msg)

	case sharedcomponents.ConfirmModalYesMsg:
		return m.handleConfirmYes()

	case sharedcomponents.ConfirmModalNoMsg:
		m.confirmModal = nil
		m.pendingAction = ""
		return m, nil

	case ResetSelectionMsg:
		m.selectionMode = false
		return m, nil

	case NetworkInspectLoadedMsg:
		return m.handleNetworkInspectLoaded(msg)

	case DiagnosticTestCompleteMsg:
		return m.handleDiagnosticTestComplete(msg)

	case RegistryFormCancelMsg:
		m.registryForm = nil
		return m, nil

	case RegistryFormSubmitMsg:
		return m.handleRegistryFormSubmit(msg)

	case RegistryLoginCompleteMsg:
		return m.handleRegistryLoginComplete(msg)

	case RegistryLogoutCompleteMsg:
		return m.handleRegistryLogoutComplete(msg)

	case RegistryLoginStatusMsg:
		maps.Copy(m.registryLoginStatus, msg.Status)
		m.updateRegistryTable()
		return m, nil

	case MultiRegistryTagsLoadedMsg:
		return m.handleMultiRegistryTagsLoaded(msg)

	case MultiRegistryTagsMetaMsg:
		return m.handleMultiRegistryTagsMeta(msg)

	case RegistryPullCompleteMsg:
		return m.handleRegistryPullComplete(msg)

	case RegistryBrowserCloseMsg:
		m.registryBrowser = nil
		return m, nil

	case RegistryGroupDetectedMsg:
		return m.handleRegistryGroupDetected(msg)

	case RegistryTagDirectScanMsg:
		return m.handleRegistryTagDirectScan(msg)
	}

	// Delegate to active form or table
	return m.delegateUpdate(msg)
}

// delegateUpdate forwards unhandled messages to the active form or table
func (m Model) delegateUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.registryBrowser != nil {
		var cmd tea.Cmd
		m.registryBrowser, cmd = m.registryBrowser.Update(msg)
		return m, cmd
	}
	if m.launchForm != nil {
		var cmd tea.Cmd
		m.launchForm, cmd = m.launchForm.Update(msg)
		return m, cmd
	}
	if m.resourceForm != nil {
		var cmd tea.Cmd
		m.resourceForm, cmd = m.resourceForm.Update(msg)
		return m, cmd
	}
	if m.registryForm != nil {
		var cmd tea.Cmd
		m.registryForm, cmd = m.registryForm.Update(msg)
		return m, cmd
	}
	if m.connectivityForm != nil {
		var cmd tea.Cmd
		m.connectivityForm, cmd = m.connectivityForm.Update(msg)
		return m, cmd
	}
	if m.networkInspectForm != nil {
		var cmd tea.Cmd
		m.networkInspectForm, cmd = m.networkInspectForm.Update(msg)
		return m, cmd
	}
	if m.confirmModal == nil && !m.filterBar.InEditMode() {
		switch m.activeTab {
		case tabImages:
			var cmd tea.Cmd
			m.imageTable, cmd = m.imageTable.Update(msg)
			return m, cmd
		case tabNetworks:
			var cmd tea.Cmd
			m.networkTable, cmd = m.networkTable.Update(msg)
			return m, cmd
		case tabVolumes:
			var cmd tea.Cmd
			m.volumeTable, cmd = m.volumeTable.Update(msg)
			return m, cmd
		case tabRegistries:
			var cmd tea.Cmd
			m.registryTable, cmd = m.registryTable.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

// handleKeyMsg processes keyboard input with priority chain
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Priority 0: registry browser
	if m.registryBrowser != nil {
		var cmd tea.Cmd
		m.registryBrowser, cmd = m.registryBrowser.Update(msg)
		return m, cmd
	}
	// Priority 1: launch form
	if m.launchForm != nil {
		var cmd tea.Cmd
		m.launchForm, cmd = m.launchForm.Update(msg)
		return m, cmd
	}
	// Priority 2: resource creation form
	if m.resourceForm != nil {
		var cmd tea.Cmd
		m.resourceForm, cmd = m.resourceForm.Update(msg)
		return m, cmd
	}
	// Priority 2.5: registry form
	if m.registryForm != nil {
		var cmd tea.Cmd
		m.registryForm, cmd = m.registryForm.Update(msg)
		return m, cmd
	}
	// Priority 3: connectivity form (full viewport)
	if m.connectivityForm != nil {
		return m.handleConnectivityFormKeyMsg(msg)
	}
	// Priority 3.5: network inspect overlay
	if m.networkInspectForm != nil {
		return m.handleNetworkInspectKeyMsg(msg)
	}
	// Priority 4: confirm modal
	if m.confirmModal != nil {
		var cmd tea.Cmd
		m.confirmModal, cmd = m.confirmModal.Update(msg)
		return m, cmd
	}
	// Priority 4: filter input active (images tab only)
	if m.filterBar.InEditMode() {
		return m.handleFilterKeyMsg(msg)
	}
	// Priority 5: normal mode
	return m.handleNormalKeyMsg(msg)
}

// handleFilterKeyMsg handles keys when filter input is active
func (m Model) handleFilterKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.filterBar, cmd = m.filterBar.Update(msg)
	m.updateImageTable()
	return m, cmd
}

// handleNormalKeyMsg handles keys in normal mode
func (m Model) handleNormalKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.selectionMode {
		return m.handleSelectionKeyMsg(msg)
	}

	// Tab switching (Rule 111: Tab/Shift+Tab)
	switch msg.String() {
	case "tab":
		return m.switchTab((int(m.activeTab) + 1) % 4)
	case "shift+tab":
		return m.switchTab((int(m.activeTab) - 1 + 4) % 4)
	}

	// Tab-specific keys
	switch m.activeTab {
	case tabImages:
		return m.handleImagesKeyMsg(msg)
	case tabNetworks:
		return m.handleNetworksKeyMsg(msg)
	case tabVolumes:
		return m.handleVolumesKeyMsg(msg)
	case tabRegistries:
		return m.handleRegistriesKeyMsg(msg)
	}
	return m, nil
}

// switchTab changes the active tab and updates table focus
func (m Model) switchTab(idx int) (tea.Model, tea.Cmd) {
	m.activeTab = ociTab(idx)
	m.imageTable.Blur()
	m.networkTable.Blur()
	m.volumeTable.Blur()
	m.registryTable.Blur()
	switch m.activeTab {
	case tabImages:
		m.imageTable.Focus()
	case tabNetworks:
		m.networkTable.Focus()
	case tabVolumes:
		m.volumeTable.Focus()
	case tabRegistries:
		m.registryTable.Focus()
		return m, m.registryLoginStatusCmd()
	}
	m.errorMsg = ""
	return m, nil
}

// handleImagesKeyMsg handles keys on the Images tab
func (m Model) handleImagesKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "/":
		return m, m.filterBar.ActivateSearch()
	case "b":
		return m.openMultiRegistryBrowser()
	case "enter":
		img := m.getSelectedImage()
		if img == nil {
			return m, nil
		}
		name := img.Name()
		return m, func() tea.Msg { return ScanDetailsRequestMsg{ImageName: name} }
	case "ctrl+e":
		if m.isSelectedImageScanning() {
			return m, nil
		}
		return m.openLaunchForm()
	case "ctrl+d":
		if m.isSelectedImageScanning() {
			return m, nil
		}
		return m.deleteSelectedImage()
	case "p":
		return m.pruneImages()
	case "ctrl+s":
		if m.isSelectedImageScanning() {
			return m, nil
		}
		return m.scanSelectedImage()
	case "A":
		return m.scanAllUnscanned()
	case "ctrl+a":
		return m.requestScanAll()
	case ".":
		return m.cycleSort()
	case "ctrl+r":
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, fetchImages(), loadScanCache())
	case "up", "k":
		m.imageTable.MoveUp(1)
		return m, nil
	case "down", "j":
		m.imageTable.MoveDown(1)
		return m, nil
	case "g", "home":
		m.imageTable.GotoTop()
		return m, nil
	case "G", "end":
		m.imageTable.GotoBottom()
		return m, nil
	}
	return m, nil
}

// handleNetworkInspectKeyMsg handles keys when the network inspect overlay is open.
func (m Model) handleNetworkInspectKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.networkInspectForm = nil
		return m, nil
	case "c":
		sel := m.networkInspectForm.SelectedContainer()
		containerName := ""
		if sel != nil {
			containerName = sel.Name
		}
		m.connectivityForm = newConnectivityTestForm(
			containerName,
			m.networkInspectForm.networkID,
			m.config.Docker.NetworkToolImage,
			m.networkInspectForm.containers,
			m.width-2,
			m.height,
		)
		return m, nil
	}
	var cmd tea.Cmd
	m.networkInspectForm, cmd = m.networkInspectForm.Update(msg)
	return m, cmd
}

// handleConnectivityFormKeyMsg handles keys when the connectivity test form is open.
func (m Model) handleConnectivityFormKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.connectivityForm = nil
		return m, nil
	}
	var cmd tea.Cmd
	m.connectivityForm, cmd = m.connectivityForm.Update(msg)
	return m, cmd
}

// handleNetworkInspectLoaded processes the result of a network inspect command.
func (m Model) handleNetworkInspectLoaded(msg NetworkInspectLoadedMsg) (tea.Model, tea.Cmd) {
	if m.networkInspectForm == nil || m.networkInspectForm.networkID != msg.NetworkID {
		return m, nil
	}
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] network inspect %s: %v", msg.NetworkID, msg.Err)
		m.networkInspectForm = nil
		m.errorMsg = "Failed to inspect network — check logs"
		return m, clearInfoMsgCmd()
	}
	m.networkInspectForm.SetContainers(msg.Containers)
	return m, nil
}

// handleDiagnosticTestComplete processes the result of a connectivity test.
func (m Model) handleDiagnosticTestComplete(msg DiagnosticTestCompleteMsg) (tea.Model, tea.Cmd) {
	if m.connectivityForm != nil {
		m.connectivityForm.SetResult(msg.Output, msg.Err)
	}
	return m, nil
}

// handleNetworksKeyMsg handles keys on the Networks tab
func (m Model) handleNetworksKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return m.openNetworkInspect()
	case "ctrl+n":
		m.resourceForm = NewResourceForm(resourceFormNetwork, m.width-2)
		return m, nil
	case "ctrl+d":
		return m.deleteSelectedNetwork()
	case "p":
		m.pendingAction = "prune-networks"
		m.confirmModal = sharedcomponents.NewConfirmModal("Prune Networks", "Remove all unused networks?")
		return m, nil
	case "ctrl+r":
		m.loadingNets = true
		return m, tea.Batch(m.spinner.Tick, fetchNetworks())
	case "up", "k":
		m.networkTable.MoveUp(1)
		return m, nil
	case "down", "j":
		m.networkTable.MoveDown(1)
		return m, nil
	case "g", "home":
		m.networkTable.GotoTop()
		return m, nil
	case "G", "end":
		m.networkTable.GotoBottom()
		return m, nil
	}
	return m, nil
}

// handleVolumesKeyMsg handles keys on the Volumes tab
func (m Model) handleVolumesKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+n":
		m.resourceForm = NewResourceForm(resourceFormVolume, m.width-2)
		return m, nil
	case "ctrl+d":
		return m.deleteSelectedVolume()
	case "p":
		m.pendingAction = "prune-volumes"
		m.confirmModal = sharedcomponents.NewConfirmModal("Prune Volumes", "Remove all unused volumes?")
		return m, nil
	case "ctrl+r":
		m.loadingVols = true
		return m, tea.Batch(m.spinner.Tick, fetchVolumes())
	case "up", "k":
		m.volumeTable.MoveUp(1)
		return m, nil
	case "down", "j":
		m.volumeTable.MoveDown(1)
		return m, nil
	case "g", "home":
		m.volumeTable.GotoTop()
		return m, nil
	case "G", "end":
		m.volumeTable.GotoBottom()
		return m, nil
	}
	return m, nil
}

// handleRegistriesKeyMsg handles keys on the Registries tab
func (m Model) handleRegistriesKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+n":
		m.registryForm = NewRegistryForm(m.width - 2)
		return m, nil
	case "e":
		return m.editSelectedRegistry()
	case "l":
		return m.loginSelectedRegistry()
	case "L":
		return m.logoutSelectedRegistry()
	case "ctrl+d":
		return m.deleteSelectedRegistry()
	case "ctrl+r":
		m.registries = m.config.Registry.Registries
		m.updateRegistryTable()
		return m, m.registryLoginStatusCmd()
	case "up", "k":
		m.registryTable.MoveUp(1)
		return m, nil
	case "down", "j":
		m.registryTable.MoveDown(1)
		return m, nil
	case "g", "home":
		m.registryTable.GotoTop()
		return m, nil
	case "G", "end":
		m.registryTable.GotoBottom()
		return m, nil
	}
	return m, nil
}

// handleSelectionKeyMsg handles keys when in selection mode
func (m Model) handleSelectionKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m, func() tea.Msg { return SelectionCancelledMsg{} }
	case "enter":
		img := m.getSelectedImage()
		if img == nil {
			return m, nil
		}
		name := img.Name()
		return m, func() tea.Msg { return ImageSelectedMsg{ImageName: name} }
	case "/":
		return m, m.filterBar.ActivateSearch()
	case ".":
		return m.cycleSort()
	case "up", "k":
		m.imageTable.MoveUp(1)
		return m, nil
	case "down", "j":
		m.imageTable.MoveDown(1)
		return m, nil
	case "g", "home":
		m.imageTable.GotoTop()
		return m, nil
	case "G", "end":
		m.imageTable.GotoBottom()
		return m, nil
	}
	return m, nil
}

// openLaunchForm fetches exposed ports then opens the launch form
func (m Model) openLaunchForm() (tea.Model, tea.Cmd) {
	img := m.getSelectedImage()
	if img == nil {
		return m, nil
	}
	name := img.Name()
	return m, fetchImageExposedPortsCmd(name)
}

// isSelectedImageScanning returns true if the currently selected image is being scanned.
func (m Model) isSelectedImageScanning() bool {
	img := m.getSelectedImage()
	if img == nil {
		return false
	}
	return m.scanningImages[img.Name()]
}

// getSelectedImage returns the selected image or nil
func (m Model) getSelectedImage() *docker.Image {
	sorted := m.sortedImages(m.filteredImages())
	cursor := m.imageTable.Cursor()
	if cursor < 0 || cursor >= len(sorted) {
		return nil
	}
	return &sorted[cursor]
}

// openNetworkInspect opens the network inspect overlay for the selected network.
func (m Model) openNetworkInspect() (tea.Model, tea.Cmd) {
	net := m.getSelectedNetwork()
	if net == nil {
		return m, nil
	}
	m.networkInspectForm = newNetworkInspectForm(net.ID, net.Name, m.width-2, m.height)
	return m, inspectNetworkCmd(net.ID, net.Name)
}

// getSelectedNetwork returns the selected network or nil
func (m Model) getSelectedNetwork() *docker.Network {
	cursor := m.networkTable.Cursor()
	if cursor < 0 || cursor >= len(m.networks) {
		return nil
	}
	return &m.networks[cursor]
}

// getSelectedVolume returns the selected volume or nil
func (m Model) getSelectedVolume() *docker.Volume {
	cursor := m.volumeTable.Cursor()
	if cursor < 0 || cursor >= len(m.volumes) {
		return nil
	}
	return &m.volumes[cursor]
}

// getSelectedRegistry returns a pointer to the selected registry item or nil
func (m Model) getSelectedRegistry() *config.RegistryItem {
	cursor := m.registryTable.Cursor()
	if cursor < 0 || cursor >= len(m.registries) {
		return nil
	}
	return &m.registries[cursor]
}

// getSelectedRegistryIndex returns the index of the selected registry or -1
func (m Model) getSelectedRegistryIndex() int {
	cursor := m.registryTable.Cursor()
	if cursor < 0 || cursor >= len(m.registries) {
		return -1
	}
	return cursor
}

// editSelectedRegistry opens a registry edit form for the selected entry
func (m Model) editSelectedRegistry() (tea.Model, tea.Cmd) {
	idx := m.getSelectedRegistryIndex()
	if idx < 0 {
		return m, nil
	}
	m.registryForm = NewRegistryEditForm(idx, m.registries[idx], m.width-2)
	return m, nil
}

// loginSelectedRegistry triggers a docker login for the selected registry
// loginSelectedRegistry opens the edit form so the user can enter a password.
// Passwords are never stored in config, so login always requires re-entry.
func (m Model) loginSelectedRegistry() (tea.Model, tea.Cmd) {
	idx := m.getSelectedRegistryIndex()
	if idx < 0 {
		return m, nil
	}
	reg := m.registries[idx]
	if !reg.AuthEnabled {
		m.errorMsg = "Auth not enabled for this registry"
		return m, clearInfoMsgCmd()
	}
	m.registryForm = NewRegistryEditForm(idx, reg, m.width-2)
	return m, nil
}

// deleteSelectedRegistry shows a confirm modal for registry removal
func (m Model) deleteSelectedRegistry() (tea.Model, tea.Cmd) {
	reg := m.getSelectedRegistry()
	if reg == nil {
		return m, nil
	}
	m.pendingAction = "delete-registry"
	m.confirmModal = sharedcomponents.NewConfirmModal("Remove Registry", fmt.Sprintf("Remove registry '%s'?", reg.URL))
	return m, nil
}

// deleteSelectedImage shows confirm modal for deletion
func (m Model) deleteSelectedImage() (tea.Model, tea.Cmd) {
	img := m.getSelectedImage()
	if img == nil {
		return m, nil
	}
	name := img.Name()
	m.pendingAction = "delete-image"
	m.confirmModal = sharedcomponents.NewConfirmModal("Delete Image", fmt.Sprintf("Delete '%s'?", name))
	return m, nil
}

// deleteSelectedNetwork shows confirm modal for network deletion
func (m Model) deleteSelectedNetwork() (tea.Model, tea.Cmd) {
	net := m.getSelectedNetwork()
	if net == nil {
		return m, nil
	}
	m.pendingAction = "delete-network"
	m.confirmModal = sharedcomponents.NewConfirmModal("Remove Network", fmt.Sprintf("Remove network '%s'?", net.Name))
	return m, nil
}

// deleteSelectedVolume shows confirm modal for volume deletion
func (m Model) deleteSelectedVolume() (tea.Model, tea.Cmd) {
	vol := m.getSelectedVolume()
	if vol == nil {
		return m, nil
	}
	m.pendingAction = "delete-volume"
	m.confirmModal = sharedcomponents.NewConfirmModal("Remove Volume", fmt.Sprintf("Remove volume '%s'?", vol.Name))
	return m, nil
}

// pruneImages shows confirm modal for pruning
func (m Model) pruneImages() (tea.Model, tea.Cmd) {
	m.pendingAction = "prune-images"
	m.confirmModal = sharedcomponents.NewConfirmModal("Prune Images", "Remove all dangling (unused) images?")
	return m, nil
}

// scanSelectedImage launches a scan for the selected image using saved options
func (m Model) scanSelectedImage() (tea.Model, tea.Cmd) {
	img := m.getSelectedImage()
	if img == nil {
		return m, nil
	}
	name := img.Name()
	if m.scanningImages[name] {
		m.infoMsg = "Scan already in progress"
		return m, clearInfoMsgCmd()
	}
	m.scanning = true
	return m, batchScanCmd([]imageScanJob{{Name: name, Target: img.ScanTarget()}}, m.defaultScanOpts())
}

// defaultScanOpts returns default scan options from configuration
func (m Model) defaultScanOpts() scan.ScanOptions {
	return scan.ScanOptions{
		EnableVuln:      m.config.Scan.EnableVuln,
		EnableSecret:    m.config.Scan.EnableSecret,
		EnableMisconfig: m.config.Scan.EnableMisconfig,
		EnableLicense:   m.config.Scan.EnableLicense,
		GenerateSBOM:    m.config.Scan.GenerateSBOM,
		SBOMOutputDir:   m.config.Scan.SBOMOutputDir,
		TrivyImage:      m.config.Scan.TrivyImage,
		GitleaksImage:   m.config.Scan.GitleaksImage,
		TrivyServer:     m.config.Scan.TrivyServer,
		IgnoreUnfixed:   m.config.Scan.IgnoreUnfixed,
		GitleaksHistory: m.config.Scan.GitleaksHistory,
		GitleaksConfig:  m.config.Scan.GitleaksConfig,
	}
}

// scanAllUnscanned triggers batch scanning of all unscanned images using config defaults
func (m Model) scanAllUnscanned() (tea.Model, tea.Cmd) {
	if m.scanning {
		return m, nil
	}
	var jobs []imageScanJob
	for _, img := range m.images {
		if img.Repository == "<none>" {
			continue
		}
		name := img.Name()
		if _, ok := m.scanCache[name]; !ok {
			jobs = append(jobs, imageScanJob{Name: name, Target: img.ScanTarget()})
		}
	}
	if len(jobs) == 0 {
		m.errorMsg = "All images are already scanned"
		return m, clearInfoMsgCmd()
	}
	m.scanning = true
	return m, batchScanCmd(jobs, m.defaultScanOpts())
}

// sortableColumns lists columns in cycle order for the '.' key
var sortableColumns = []sortField{
	sortByName,
	sortByDiskUsage,
	sortByContentSize,
	sortByCritical,
	sortByHigh,
	sortByMedium,
	sortByLow,
	sortByScanned,
}

// cycleSort cycles through sort options
func (m Model) cycleSort() (tea.Model, tea.Cmd) {
	if m.sortAsc {
		m.sortAsc = false
	} else {
		m.sortAsc = true
		nextIdx := 0
		for i, col := range sortableColumns {
			if col == m.sortColumn {
				nextIdx = (i + 1) % len(sortableColumns)
				break
			}
		}
		m.sortColumn = sortableColumns[nextIdx]
	}
	m.updateImageTable()
	return m, nil
}

// handleConfirmYes executes the pending action
func (m Model) handleConfirmYes() (tea.Model, tea.Cmd) {
	action := m.pendingAction
	m.confirmModal = nil
	m.pendingAction = ""

	switch action {
	case "delete-image":
		img := m.getSelectedImage()
		if img == nil {
			return m, nil
		}
		name := img.Name()
		return m, removeImageCmd(img.ID, name)
	case "prune-images":
		return m, pruneImagesCmd()
	case "delete-network":
		net := m.getSelectedNetwork()
		if net == nil {
			return m, nil
		}
		return m, removeNetworkCmd(net.ID)
	case "prune-networks":
		return m, pruneNetworksCmd()
	case "delete-volume":
		vol := m.getSelectedVolume()
		if vol == nil {
			return m, nil
		}
		return m, removeVolumeCmd(vol.Name)
	case "prune-volumes":
		return m, pruneVolumesCmd()
	case "delete-registry":
		idx := m.getSelectedRegistryIndex()
		if idx < 0 {
			return m, nil
		}
		regs := m.config.Registry.Registries
		m.config.Registry.Registries = append(regs[:idx], regs[idx+1:]...)
		m.registries = m.config.Registry.Registries
		m.updateRegistryTable()
		if err := config.Save(m.config); err != nil {
			log.Printf("ERROR [oci_resources] delete registry: %v", err)
			m.errorMsg = "Failed to save config — check logs"
		}
		return m, nil
	}
	return m, nil
}

// handleImagesList processes the image list response
func (m Model) handleImagesList(msg ImagesListMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] list: %v", msg.Err)
		m.errorMsg = "Failed to load images — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	m.images = msg.Images
	m.updateImageTable()
	return m, nil
}

// handleScanCacheLoaded updates the scan cache data
func (m Model) handleScanCacheLoaded(msg ScanCacheLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Entries != nil {
		m.scanCache = msg.Entries
	}
	m.updateImageTable()
	return m, nil
}

// handleImageAction processes action results
func (m Model) handleImageAction(msg ImageActionMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] %s %s: %v", msg.Action, msg.ID, msg.Err)
		m.errorMsg = "Action failed — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	return m, fetchImages()
}

// handlePruneComplete processes prune results
func (m Model) handlePruneComplete(msg PruneCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] prune: %v", msg.Err)
		m.errorMsg = "Prune failed — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	return m, fetchImages()
}

// handleNetworksList processes the network list response
func (m Model) handleNetworksList(msg NetworksListMsg) (tea.Model, tea.Cmd) {
	m.loadingNets = false
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] network list: %v", msg.Err)
		return m, nil
	}
	m.networks = msg.Networks
	m.updateNetworkTable()
	return m, nil
}

// handleNetworkAction processes network action results
func (m Model) handleNetworkAction(msg NetworkActionMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] network %s: %v", msg.Action, msg.Err)
		m.errorMsg = "Network action failed — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	return m, fetchNetworks()
}

// handleNetworkPruneComplete processes network prune results
func (m Model) handleNetworkPruneComplete(msg NetworkPruneCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] network prune: %v", msg.Err)
		m.errorMsg = "Network prune failed — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	return m, fetchNetworks()
}

// handleVolumesList processes the volume list response
func (m Model) handleVolumesList(msg VolumesListMsg) (tea.Model, tea.Cmd) {
	m.loadingVols = false
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] volume list: %v", msg.Err)
		return m, nil
	}
	m.volumes = msg.Volumes
	m.updateVolumeTable()
	return m, nil
}

// handleVolumeAction processes volume action results
func (m Model) handleVolumeAction(msg VolumeActionMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] volume %s: %v", msg.Action, msg.Err)
		m.errorMsg = "Volume action failed — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	return m, fetchVolumes()
}

// handleVolumePruneComplete processes volume prune results
func (m Model) handleVolumePruneComplete(msg VolumePruneCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] volume prune: %v", msg.Err)
		m.errorMsg = "Volume prune failed — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	return m, fetchVolumes()
}

// handleImageExposedPorts opens the launch form once exposed ports are fetched
func (m Model) handleImageExposedPorts(msg ImageExposedPortsMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] exposed ports %s: %v", msg.ImageName, msg.Err)
	}
	m.launchForm = NewLaunchForm(msg.ImageName, msg.Ports, m.networks, m.width-2)
	return m, loadLaunchOptionsCmd(msg.ImageName)
}

// handleLaunchOptionsCacheLoaded applies cached options to the launch form if it is still open.
func (m Model) handleLaunchOptionsCacheLoaded(msg LaunchOptionsCacheLoadedMsg) (tea.Model, tea.Cmd) {
	if m.launchForm == nil || msg.Entry == nil || m.launchForm.image != msg.ImageName {
		return m, nil
	}
	m.launchForm.ApplyCached(*msg.Entry)
	// Trigger entrypoint verification if a cached entrypoint is present
	if msg.Entry.Entrypoint != "" {
		m.launchForm.verify = verifyChecking
		m.launchForm.verifySeq++
		return m, verifyEntrypointCmd(m.launchForm.verifySeq, m.launchForm.image, msg.Entry.Entrypoint)
	}
	return m, nil
}

// handleContainerLaunchComplete processes container launch results
func (m Model) handleContainerLaunchComplete(msg ContainerLaunchCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] launch container: %v", msg.Err)
		m.errorMsg = "Failed to launch container — check logs"
		m.lastLaunchOpts = nil
		m.lastLaunchImage = ""
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	if m.lastLaunchOpts != nil {
		saveCmd := saveLaunchOptionsCmd(m.lastLaunchImage, *m.lastLaunchOpts)
		m.lastLaunchOpts = nil
		m.lastLaunchImage = ""
		return m, saveCmd
	}
	return m, nil
}

// handleClipboardCopy processes the result of copying the docker command to clipboard (Rule 128).
// clearInfoMsgCmd clears both errorMsg and infoMsg after 3 seconds — correct for both branches.
func (m Model) handleClipboardCopy(msg ClipboardCopyMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] clipboard copy: %v", msg.Err)
		m.errorMsg = "Failed to copy command to clipboard"
	} else {
		m.infoMsg = "Docker command copied to clipboard"
	}
	return m, clearInfoMsgCmd()
}

// handleLaunchFormSubmit dispatches the container launch.
// handleLaunchFormSubmitWithCache snapshots form state into the pending save
// fields before delegating to handleLaunchFormSubmit.
func (m Model) handleLaunchFormSubmitWithCache(msg LaunchFormSubmitMsg) (tea.Model, tea.Cmd) {
	if m.launchForm != nil {
		entry := m.launchForm.ToCacheEntry()
		m.lastLaunchOpts = &entry
		m.lastLaunchImage = m.launchForm.image
	}
	m.launchForm = nil
	return m.handleLaunchFormSubmit(msg)
}

// Interactive (-it) containers require a real TTY, so they are handed off via
// tea.ExecProcess instead of the background launchContainerCmd.
func (m Model) handleLaunchFormSubmit(msg LaunchFormSubmitMsg) (tea.Model, tea.Cmd) {
	opts := msg.Opts
	if opts.Interactive && opts.TTY {
		cmd, err := docker.BuildLaunchCmd(opts)
		if err != nil {
			log.Printf("ERROR [oci_resources] build launch cmd: %v", err)
			m.errorMsg = "Failed to launch container — check logs"
			return m, clearInfoMsgCmd()
		}
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			if err != nil {
				log.Printf("ERROR [oci_resources] interactive container exit: %v", err)
			}
			return ContainerLaunchCompleteMsg{Err: err}
		})
	}
	return m, launchContainerCmd(opts)
}

// handleResourceFormSubmit handles resource form submission (network or volume creation)
func (m Model) handleResourceFormSubmit(msg ResourceFormSubmitMsg) (tea.Model, tea.Cmd) {
	m.resourceForm = nil
	if msg.Kind == resourceFormNetwork {
		return m, createNetworkCmd(msg.Name, msg.Driver)
	}
	return m, createVolumeCmd(msg.Name, msg.Driver)
}

// handleRegistryFormSubmit saves a new or edited registry to the config and optionally logs in.
func (m Model) handleRegistryFormSubmit(msg RegistryFormSubmitMsg) (tea.Model, tea.Cmd) {
	m.registryForm = nil
	if msg.Index < 0 {
		m.config.Registry.Registries = append(m.config.Registry.Registries, msg.Item)
	} else if msg.Index < len(m.config.Registry.Registries) {
		m.config.Registry.Registries[msg.Index] = msg.Item
	}
	m.registries = m.config.Registry.Registries
	m.updateRegistryTable()
	if err := config.Save(m.config); err != nil {
		log.Printf("ERROR [oci_resources] save registry config: %v", err)
		m.errorMsg = "Failed to save registry — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	if msg.Password != "" && msg.Item.AuthEnabled {
		return m, registryLoginCmd(msg.Item.URL, msg.Item.Username, msg.Password)
	}
	return m, nil
}

// handleRegistryLoginComplete processes the result of a docker login operation.
// registryLoginStatusCmd builds a command that checks login status for all configured registries.
func (m Model) registryLoginStatusCmd() tea.Cmd {
	urls := make([]string, 0, len(m.registries))
	for _, reg := range m.registries {
		if reg.AuthEnabled {
			urls = append(urls, reg.URL)
		}
	}
	return checkRegistryLoginStatusCmd(urls)
}

func (m Model) handleRegistryLoginComplete(msg RegistryLoginCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] login %s: %v", msg.RegistryURL, msg.Err)
		m.errorMsg = "Login failed — check logs"
		m.registryLoginStatus[msg.RegistryURL] = false
		m.updateRegistryTable()
		return m, clearInfoMsgCmd()
	}
	log.Printf("INFO [oci_resources] login successful: %s", msg.RegistryURL)
	m.errorMsg = ""
	// Optimistic update + re-check from disk to confirm credential helper cases
	m.registryLoginStatus[msg.RegistryURL] = true
	m.updateRegistryTable()
	return m, m.registryLoginStatusCmd()
}

// logoutSelectedRegistry triggers a docker logout for the selected registry
func (m Model) logoutSelectedRegistry() (tea.Model, tea.Cmd) {
	reg := m.getSelectedRegistry()
	if reg == nil {
		return m, nil
	}
	return m, registryLogoutCmd(reg.URL)
}

func (m Model) handleRegistryLogoutComplete(msg RegistryLogoutCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] logout %s: %v", msg.RegistryURL, msg.Err)
		m.errorMsg = "Logout failed — check logs"
		return m, clearInfoMsgCmd()
	}
	log.Printf("INFO [oci_resources] logout successful: %s", msg.RegistryURL)
	m.errorMsg = ""
	// Do NOT call registryLoginStatusCmd here: docker logout docker.io may not remove
	// the https://index.docker.io/v1/ key from config.json, causing the file-check to
	// override this correct false status back to true.
	m.registryLoginStatus[msg.RegistryURL] = false
	m.updateRegistryTable()
	return m, nil
}

// requestScanAll launches a batch scan for all images with the configured options.
// Rule 126: purges the in-memory and disk cache before scanning.
func (m Model) requestScanAll() (tea.Model, tea.Cmd) {
	if m.scanning {
		return m, nil
	}
	var jobs []imageScanJob
	var cacheKeys []string
	for _, img := range m.images {
		if img.Repository == "<none>" {
			continue
		}
		name := img.Name()
		jobs = append(jobs, imageScanJob{Name: name, Target: img.ScanTarget()})
		cacheKeys = append(cacheKeys, name)
		delete(m.scanCache, name)
	}
	if len(jobs) == 0 {
		return m, nil
	}
	m.scanning = true
	return m, tea.Batch(deleteScanCacheCmd(cacheKeys), batchScanCmd(jobs, m.defaultScanOpts()))
}

// handleLaunchBatchScan starts a batch scan of all images with the configured options.
func (m Model) handleLaunchBatchScan(msg LaunchBatchScanMsg) (tea.Model, tea.Cmd) {
	if m.scanning {
		return m, nil
	}
	var jobs []imageScanJob
	for _, img := range m.images {
		if img.Repository == "<none>" {
			continue
		}
		jobs = append(jobs, imageScanJob{Name: img.Name(), Target: img.ScanTarget()})
	}
	if len(jobs) == 0 {
		return m, nil
	}
	m.lastScanOptions = msg.Opts
	m.scanning = true
	return m, batchScanCmd(jobs, msg.Opts)
}

// handleLaunchSingleImageScan starts a scan for a single image with the configured options.
// The ImageName is user-provided (from security view) so Name and Target are identical.
func (m Model) handleLaunchSingleImageScan(msg LaunchSingleImageScanMsg) (tea.Model, tea.Cmd) {
	if m.scanningImages[msg.ImageName] {
		return m, nil
	}
	m.lastScanOptions = msg.Opts
	m.scanning = true
	job := imageScanJob{Name: msg.ImageName, Target: msg.ImageName}
	return m, batchScanCmd([]imageScanJob{job}, msg.Opts)
}

// handleImageScanStarting marks an image as currently scanning and refreshes the table.
func (m Model) handleImageScanStarting(msg ImageScanStartingMsg) (tea.Model, tea.Cmd) {
	wasScanning := len(m.scanningImages) > 0
	m.scanningImages[msg.ImageName] = true
	m.infoMsg = ""
	m.updateImageTable()
	if m.registryBrowser != nil {
		m.registryBrowser.SetTagScanning(msg.ImageName, true)
	}
	if !wasScanning {
		return m, m.spinner.Tick
	}
	return m, nil
}

// handleImageScanFinished updates scan results for a completed image scan.
func (m Model) handleImageScanFinished(msg ImageScanFinishedMsg) (tea.Model, tea.Cmd) {
	delete(m.scanningImages, msg.ImageName)
	if msg.Err != nil {
		m.failedScans[msg.ImageName] = true
	} else {
		delete(m.failedScans, msg.ImageName)
		m.scanCache[msg.ImageName] = msg.Entry
		m.errorMsg = ""
	}
	m.scanning = len(m.scanningImages) > 0
	m.updateImageTable()
	if m.registryBrowser != nil {
		m.registryBrowser.SetTagScanning(msg.ImageName, false)
		m.registryBrowser.SetScanCache(m.scanCache)
	}
	return m, nil
}

// filteredImages returns images matching the current filter
func (m *Model) filteredImages() []docker.Image {
	query := strings.ToLower(m.filterBar.SearchQuery())
	if query == "" {
		return m.images
	}
	var result []docker.Image
	for _, img := range m.images {
		if strings.Contains(strings.ToLower(img.Repository), query) ||
			strings.Contains(strings.ToLower(img.Tag), query) {
			result = append(result, img)
		}
	}
	return result
}

// sortedImages returns images sorted by the current sort column
func (m *Model) sortedImages(images []docker.Image) []docker.Image {
	sorted := make([]docker.Image, len(images))
	copy(sorted, images)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		nameA := a.Name()
		nameB := b.Name()
		cacheA := m.scanCache[nameA]
		cacheB := m.scanCache[nameB]
		var less bool
		switch m.sortColumn {
		case sortByDiskUsage:
			less = a.UniqueSize < b.UniqueSize
		case sortByContentSize:
			less = a.Size < b.Size
		case sortByCritical:
			less = cacheA.Critical < cacheB.Critical
		case sortByHigh:
			less = cacheA.High < cacheB.High
		case sortByMedium:
			less = cacheA.Medium < cacheB.Medium
		case sortByLow:
			less = cacheA.Low < cacheB.Low
		case sortByScanned:
			less = cacheA.ScannedAt.Before(cacheB.ScannedAt)
		default:
			less = strings.ToLower(nameA) < strings.ToLower(nameB)
		}
		if m.sortAsc {
			return less
		}
		return !less
	})
	return sorted
}

// formatBytes formats bytes into human-readable string
func formatBytes(b int64) string {
	switch {
	case b >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(b)/1e9)
	case b >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(b)/1e6)
	case b >= 1e3:
		return fmt.Sprintf("%.1f kB", float64(b)/1e3)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// formatCVECount formats a CVE count for display, returns "-" if not scanned
func formatCVECount(count int, scanned bool) string {
	if !scanned {
		return "-"
	}
	return fmt.Sprintf("%d", count)
}

// updateImageTable rebuilds the image table rows
func (m *Model) updateImageTable() {
	// Build alias list for display name substitution
	aliases := make([]docker.RegistryAlias, 0, len(m.registries))
	for _, reg := range m.registries {
		if reg.Alias != "" {
			aliases = append(aliases, docker.RegistryAlias{URL: reg.URL, Alias: reg.Alias})
		}
	}

	sorted := m.sortedImages(m.filteredImages())
	rows := make([]table.Row, 0, len(sorted))
	for _, img := range sorted {
		rawName := img.Name()
		displayName := docker.ApplyAliases(rawName, aliases)
		diskUsage := formatBytes(img.UniqueSize)
		contentSize := formatBytes(img.Size)
		shortID := img.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		entry, scanned := m.scanCache[rawName]
		scanning := m.scanningImages[rawName]
		crit := formatCVECount(entry.Critical, scanned)
		high := formatCVECount(entry.High, scanned)
		med := formatCVECount(entry.Medium, scanned)
		low := formatCVECount(entry.Low, scanned)
		scannedAt := "-"
		switch {
		case scanning:
			frame := spinner.Dot.Frames[m.spinnerFrameIdx%len(spinner.Dot.Frames)]
			scannedAt = frame + "scanning"
		case m.failedScans[rawName]:
			scannedAt = theme.IconError + " error"
		case scanned:
			scannedAt = timeAgo(entry.ScannedAt)
		}
		rows = append(rows, table.Row{shortID, displayName, diskUsage, contentSize, crit, high, med, low, scannedAt})
	}

	// Update sort indicators in column headers
	cols := m.imageTable.Columns()
	if len(cols) >= 9 {
		sortColIndex := map[sortField]int{
			sortByName: 1, sortByDiskUsage: 2, sortByContentSize: 3,
			sortByCritical: 4, sortByHigh: 5, sortByMedium: 6, sortByLow: 7, sortByScanned: 8,
		}
		baseTitles := map[int]string{1: "Name", 2: "Disk Usage", 3: "Content Size", 4: "C", 5: "H", 6: "M", 7: "L", 8: "Scanned"}
		for idx, title := range baseTitles {
			cols[idx].Title = title
		}
		if idx, ok := sortColIndex[m.sortColumn]; ok {
			arrow := " ▲"
			if !m.sortAsc {
				arrow = " ▼"
			}
			cols[idx].Title = baseTitles[idx] + arrow
		}
		m.imageTable.SetColumns(cols)
	}
	m.imageTable.SetRows(rows)
	m.imageTable.SetStyles(theme.DefaultTableStyles())
	m.imageTable.SetHeight(m.tableHeight())
}

// updateNetworkTable rebuilds the network table rows
func (m *Model) updateNetworkTable() {
	rows := make([]table.Row, 0, len(m.networks))
	for _, net := range m.networks {
		shortID := net.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		rows = append(rows, table.Row{shortID, net.Name, net.Driver, net.Scope})
	}
	m.networkTable.SetRows(rows)
	m.networkTable.SetStyles(theme.DefaultTableStyles())
	m.networkTable.SetHeight(m.tableHeight())
}

// updateVolumeTable rebuilds the volume table rows
func (m *Model) updateVolumeTable() {
	rows := make([]table.Row, 0, len(m.volumes))
	for _, vol := range m.volumes {
		rows = append(rows, table.Row{vol.Name, vol.Driver, vol.Mountpoint})
	}
	m.volumeTable.SetRows(rows)
	m.volumeTable.SetStyles(theme.DefaultTableStyles())
	m.volumeTable.SetHeight(m.tableHeight())
}

// updateRegistryTable rebuilds the registry table rows
func (m *Model) updateRegistryTable() {
	rows := make([]table.Row, 0, len(m.registries))
	for _, reg := range m.registries {
		auth := "no"
		if reg.AuthEnabled {
			auth = "yes"
		}
		logged := "-"
		if reg.AuthEnabled {
			if m.registryLoginStatus[reg.URL] {
				logged = theme.IconOK
			} else {
				logged = theme.IconError
			}
		}
		rows = append(rows, table.Row{reg.URL, reg.Username, reg.Alias, auth, logged})
	}
	m.registryTable.SetRows(rows)
	m.registryTable.SetStyles(theme.DefaultTableStyles())
	m.registryTable.SetHeight(m.tableHeight())
}

// tableHeight computes the data row count for the table based on current state
func (m *Model) tableHeight() int {
	return max(m.height-1, 1)
}

// resize adjusts all table dimensions
func (m *Model) resize(width, height int) {
	m.width = width
	m.height = height
	m.filterBar.Resize(width)

	if m.launchForm != nil {
		m.launchForm.SetWidth(width - 2)
	}
	if m.resourceForm != nil {
		m.resourceForm.SetWidth(width - 2)
	}
	if m.registryForm != nil {
		m.registryForm.SetWidth(width - 2)
	}
	if m.networkInspectForm != nil {
		m.networkInspectForm.resize(width-2, height)
	}
	if m.connectivityForm != nil {
		m.connectivityForm.width = width - 2
		m.connectivityForm.height = height
	}
	if m.registryBrowser != nil {
		m.registryBrowser.SetSize(width-2, height)
	}

	m.imageTable.SetHeight(m.tableHeight())
	m.networkTable.SetHeight(m.tableHeight())
	m.volumeTable.SetHeight(m.tableHeight())
	m.registryTable.SetHeight(m.tableHeight())

	m.resizeImageTable(width)
	m.resizeNetworkTable(width)
	m.resizeVolumeTable(width)
	m.resizeRegistryTable(width)
}

func (m *Model) resizeImageTable(width int) {
	contentWidth := width - 2
	columns := m.imageTable.Columns()
	numCols := len(columns)
	if numCols >= 9 {
		available := contentWidth - numCols*2
		fixedID := 14
		fixedDisk := 12
		fixedContent := 14
		fixedC := 4
		fixedH := 4
		fixedM := 4
		fixedL := 4
		fixedScanned := 14
		fixedTotal := fixedID + fixedDisk + fixedContent + fixedC + fixedH + fixedM + fixedL + fixedScanned
		flexName := max(available-fixedTotal, 20)
		columns[0].Width = fixedID
		columns[1].Width = flexName
		columns[2].Width = fixedDisk
		columns[3].Width = fixedContent
		columns[4].Width = fixedC
		columns[5].Width = fixedH
		columns[6].Width = fixedM
		columns[7].Width = fixedL
		columns[8].Width = available - fixedID - flexName - fixedDisk - fixedContent - fixedC - fixedH - fixedM - fixedL
		m.imageTable.SetColumns(columns)
	}
}

func (m *Model) resizeNetworkTable(width int) {
	contentWidth := width - 2
	columns := m.networkTable.Columns()
	numCols := len(columns)
	if numCols >= 4 {
		available := contentWidth - numCols*2
		fixedID := 14
		fixedDriver := 12
		fixedScope := 10
		flexName := max(available-fixedID-fixedDriver-fixedScope, 20)
		columns[0].Width = fixedID
		columns[1].Width = flexName
		columns[2].Width = fixedDriver
		columns[3].Width = available - fixedID - flexName - fixedDriver
		m.networkTable.SetColumns(columns)
	}
}

func (m *Model) resizeVolumeTable(width int) {
	contentWidth := width - 2
	columns := m.volumeTable.Columns()
	numCols := len(columns)
	if numCols >= 3 {
		available := contentWidth - numCols*2
		fixedDriver := 12
		fixedName := 30
		columns[0].Width = fixedName
		columns[1].Width = fixedDriver
		columns[2].Width = max(available-fixedName-fixedDriver, 20)
		m.volumeTable.SetColumns(columns)
	}
}

func (m *Model) resizeRegistryTable(width int) {
	contentWidth := width - 2
	columns := m.registryTable.Columns()
	numCols := len(columns)
	if numCols >= 5 {
		available := contentWidth - numCols*2
		fixedUsername := 16
		fixedAlias := 10
		fixedAuth := 6
		fixedLogged := 8
		flexURL := max(available-fixedUsername-fixedAlias-fixedAuth-fixedLogged, 20)
		columns[0].Width = flexURL
		columns[1].Width = fixedUsername
		columns[2].Width = fixedAlias
		columns[3].Width = fixedAuth
		columns[4].Width = available - flexURL - fixedUsername - fixedAlias - fixedAuth
		m.registryTable.SetColumns(columns)
	}
}

// timeAgo returns a compact relative time string (Rule 127).
func timeAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return theme.TimeAgo(t)
}

// openMultiRegistryBrowser opens the multi-registry browser (triggered by 'b' on Images tab).
func (m Model) openMultiRegistryBrowser() (tea.Model, tea.Cmd) {
	if len(m.registries) == 0 {
		m.errorMsg = "No registries configured — add one in the Registries tab"
		return m, clearInfoMsgCmd()
	}
	browser, cmd := newRegistryBrowser(m.registries, m.width-2, m.height)
	m.registryBrowser = browser
	m.registryBrowser.SetScanCache(m.scanCache)
	return m, cmd
}

// handleMultiRegistryTagsLoaded incorporates tag results from one registry into the browser.
func (m Model) handleMultiRegistryTagsLoaded(msg MultiRegistryTagsLoadedMsg) (tea.Model, tea.Cmd) {
	if m.registryBrowser == nil {
		return m, nil
	}
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] registry tags %s from %s: %v", msg.Repo, msg.RegistryURL, msg.Err)
		m.registryBrowser.AddRegistryTags(msg.RegistryURL, msg.Alias, msg.Repo, nil)
		return m, nil
	}
	m.registryBrowser.SetScanCache(m.scanCache)
	m.registryBrowser.AddRegistryTags(msg.RegistryURL, msg.Alias, msg.Repo, msg.Tags)
	return m, loadMultiRegistryTagsMetaCmd(msg.RegistryURL, msg.Repo)
}

// handleMultiRegistryTagsMeta stores tag metadata for one registry in the browser.
func (m Model) handleMultiRegistryTagsMeta(msg MultiRegistryTagsMetaMsg) (tea.Model, tea.Cmd) {
	if m.registryBrowser == nil {
		return m, nil
	}
	if msg.Err != nil {
		log.Printf("INFO [oci_resources] tag meta unavailable for %s at %s: %v", msg.Repo, msg.RegistryURL, msg.Err)
		return m, nil
	}
	if msg.Meta != nil {
		m.registryBrowser.SetMultiTagsMeta(msg.RegistryURL, msg.Meta)
	}
	return m, nil
}

// handleRegistryPullComplete processes the result of a pull from the browser.
func (m Model) handleRegistryPullComplete(msg RegistryPullCompleteMsg) (tea.Model, tea.Cmd) {
	if m.registryBrowser == nil {
		return m, nil
	}
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] pull %s: %v", msg.ImageName, msg.Err)
		m.registryBrowser.SetOperationError("")
		m.errorMsg = "Pull failed — check logs"
		return m, clearInfoMsgCmd()
	}
	m.registryBrowser.SetOperationSuccess()
	m.infoMsg = "Image pulled: " + msg.ImageName
	return m, tea.Batch(fetchImages(), clearInfoMsgCmd())
}

// handleRegistryTagDirectScan starts a direct remote Trivy scan without pulling the image.
func (m Model) handleRegistryTagDirectScan(msg RegistryTagDirectScanMsg) (tea.Model, tea.Cmd) {
	if m.scanningImages[msg.ImageName] {
		m.infoMsg = "Scan already in progress"
		return m, clearInfoMsgCmd()
	}
	job := imageScanJob{Name: msg.ImageName, Target: msg.ImageName}
	return m, batchScanCmd([]imageScanJob{job}, m.defaultScanOpts())
}

// unscannedCount returns the number of images not yet scanned
func (m *Model) unscannedCount() int {
	count := 0
	for _, img := range m.images {
		name := img.Name()
		if img.Repository == "<none>" {
			continue
		}
		if _, ok := m.scanCache[name]; !ok {
			count++
		}
	}
	return count
}

// handleRegistryGroupDetected forwards a group detection result to the active browser.
func (m Model) handleRegistryGroupDetected(msg RegistryGroupDetectedMsg) (tea.Model, tea.Cmd) {
	if m.registryBrowser == nil {
		return m, nil
	}
	if msg.Err != nil {
		log.Printf("INFO [oci_resources] group detection failed for %s: %v", msg.RegistryURL, msg.Err)
	}
	var cmd tea.Cmd
	m.registryBrowser, cmd = m.registryBrowser.HandleGroupDetected(msg)
	return m, cmd
}
