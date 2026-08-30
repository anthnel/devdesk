package ociresources

import (
	"maps"

	"github.com/charmbracelet/bubbles/spinner"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/jobs"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

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

	case ImageScanFinishedMsg:
		return m.handleImageScanFinished(msg)

	case ScanRequestMsg:
		return m.handleScanRequest(msg)

	case jobs.ChangedMsg:
		return m.handleJobsChanged(msg)

	case spinner.TickMsg:
		var cmds []tea.Cmd
		// The tables' frames turn while anything is running on a row, which is
		// not the same condition as the view's own spinner: the lists are
		// loaded and on screen while an image is removed.
		busy := m.advanceBusySpinners()
		if busy || m.loading || m.loadingNets || m.loadingVols || m.loadingRegs {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			m.spinnerFrameIdx = (m.spinnerFrameIdx + 1) % len(spinner.Dot.Frames)
			// A tab's load is reported in the footer, so the frame has to reach
			// it — a spinner stuck on frame zero reads as a hang.
			m.footer.SetSpinnerFrame(m.spinner.View())
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

	case sharedcomponents.OptionConfirmModalYesMsg:
		m.scanAllModal = nil
		return m.scanAll(msg.Option)

	case sharedcomponents.OptionConfirmModalNoMsg:
		m.scanAllModal = nil
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

	case RegistryPullRequestedMsg:
		return m.handleRegistryPullRequested(msg)

	case RegistryPullStartingMsg:
		// No handler of its own: the row is already spinning once the router
		// has recorded the transition and rebuilt the table (same reasoning
		// as handleImageScanStarting).
		return m, nil

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

	if m.footer.Handle(msg) {
		return m, nil
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
			cmd = m.registryTable.Update(msg)
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
	// Priority 4: whichever modal is open
	if m.confirmModal != nil {
		var cmd tea.Cmd
		m.confirmModal, cmd = m.confirmModal.Update(msg)
		return m, cmd
	}
	if m.scanAllModal != nil {
		var cmd tea.Cmd
		m.scanAllModal, cmd = m.scanAllModal.Update(msg)
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
		m.footer.Clear()
		m.updateRegistryTable()
		return m, m.registryLoginStatusCmd()
	}
	m.footer.Clear()
	return m, nil
}
