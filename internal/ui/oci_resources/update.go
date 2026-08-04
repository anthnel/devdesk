package ociresources

import (
	"maps"
	"time"

	"github.com/charmbracelet/bubbles/spinner"

	tea "github.com/charmbracelet/bubbletea"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
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
		loadRegistryGroupCache(),
		loadBrowserSelectionCmd(),
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
		return m.closeMultiRegistryBrowser()

	case RegistryGroupDetectedMsg:
		return m.handleRegistryGroupDetected(msg)

	case RegistryGroupCacheLoadedMsg:
		return m.handleRegistryGroupCacheLoaded(msg)

	case BrowserSelectionLoadedMsg:
		if msg.Deselected != nil {
			m.browserDeselected = msg.Deselected
		}
		return m, nil

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
	if m.confirmModal == nil && !m.imageTable.InEditMode() {
		switch m.activeTab {
		case tabImages:
			var cmd tea.Cmd
			cmd = m.imageTable.Update(msg)
			return m, cmd
		case tabNetworks:
			var cmd tea.Cmd
			cmd = m.networkTable.Update(msg)
			return m, cmd
		case tabVolumes:
			var cmd tea.Cmd
			cmd = m.volumeTable.Update(msg)
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
	if m.imageTable.InEditMode() {
		return m, m.imageTable.Update(msg)
	}
	// Priority 5: normal mode
	return m.handleNormalKeyMsg(msg)
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
		// The registries come from config, so the table can be filled now
		// rather than waiting on the login check — otherwise the tab opens
		// empty and only fills once docker answers.
		m.errorMsg = ""
		m.updateRegistryTable()
		return m, m.registryLoginStatusCmd()
	}
	m.errorMsg = ""
	return m, nil
}
