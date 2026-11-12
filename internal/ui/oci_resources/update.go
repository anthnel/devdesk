package ociresources

import (
	"github.com/anthnel/devdesk/internal/shared"
	"maps"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/jobs"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// Init initializes the OCI resources view
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		fetchImages(),
		loadScanCache(),
		loadRegistryGroupCache(),
		loadBrowserSelectionCmd(),
		fetchNetworks(),
		fetchVolumes(),
		checkTrustPolicyCmd(),
	)
}

// refreshReads lists images, networks and volumes and loads the scan cache.
// It never scans (Rule 126).
func (m Model) refreshReads() tea.Cmd {
	return tea.Batch(fetchImages(), loadScanCache(), fetchNetworks(), fetchVolumes())
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKeyMsg(msg)

	case tea.PasteMsg:
		return m.handlePaste(msg)

	// The router's detection (§3.86): it decides whether S is offered, and
	// what a scan runs on.
	case shared.ScanToolsMsg:
		m.deps = msg.Report
		return m, nil

	// An agent asks the way a key does (§3.61) — see mcp.go.
	case ImageScanRequestedMsg:
		return m.handleImageScanRequested(msg)

	case ImagePullRequestedMsg:
		return m.handleImagePullRequested(msg)

	case TrustPolicyCheckedMsg:
		return m.handleTrustPolicyChecked(msg)

	case ImageSignatureCheckedMsg:
		return m.handleImageSignatureChecked(msg)

	// Another view asks for an image to be shown (keymap.Jump) — see focus.go.
	case FocusImageRequestMsg:
		return m.handleFocusImage(msg)

	case ImageScanStartingMsg:
		return m.handleImageScanStarting(msg)

	case ImageScanFinishedMsg:
		return m.handleImageScanFinished(msg)

	case ScanRequestMsg:
		return m.handleScanRequest(msg)

	case jobs.ChangedMsg:
		return m.handleJobsChanged(msg)

	case jobs.FrameMsg:
		return m.handleFrameMsg(msg)

	case shared.TerminalFocusMsg:
		m.blurred = !msg.Focused
		if !msg.Resume {
			return m, nil
		}
		// One re-read on the focus edge, no new tick: the chain kept its timer.
		return m, m.refreshReads()

	case RefreshTickMsg:
		// The tick rearms itself (it used to run only once) and skips the
		// reads while the terminal is blurred.
		if m.blurred {
			return m, tickCmd()
		}
		return m, tea.Batch(m.refreshReads(), tickCmd())

	case ImagesListMsg:
		return m.handleImagesList(msg)

	case ImageUpdatesCheckedMsg:
		return m.handleImageUpdatesChecked(msg)

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
		m.authFile = msg.AuthFile
		m.updateRegistryTable()
		return m, nil

	case MultiRegistryTagsLoadedMsg:
		return m.handleMultiRegistryTagsLoaded(msg)

	case ImageSearchResultMsg:
		return m.handleImageSearchResult(msg)

	case RegistryBrowserWarnMsg:
		return m.handleRegistryBrowserWarn(msg)

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
func (m Model) handleKeyMsg(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
	// Priority 3: network inspect overlay
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

// handlePaste gives pasted text to the form, browser or filter bar that has the
// keyboard, in the order handleKeyMsg gives it. A modal or no focused field
// drops it: a paste is never read as keys (Rule 111).
func (m Model) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case m.registryBrowser != nil:
		m.registryBrowser, cmd = m.registryBrowser.Update(msg)
	case m.launchForm != nil:
		m.launchForm, cmd = m.launchForm.Update(msg)
	case m.resourceForm != nil:
		m.resourceForm, cmd = m.resourceForm.Update(msg)
	case m.registryForm != nil:
		m.registryForm, cmd = m.registryForm.Update(msg)
	case m.networkInspectForm != nil, m.confirmModal != nil, m.scanAllModal != nil:
	case m.imageTable.InEditMode():
		cmd = m.imageTable.Update(msg)
	}
	return m, cmd
}

// handleNormalKeyMsg handles keys in normal mode
func (m Model) handleNormalKeyMsg(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
