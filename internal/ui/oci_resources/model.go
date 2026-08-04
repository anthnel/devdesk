package ociresources

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/registrymgr"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ociTab represents the active tab in the OCI resources view
type ociTab int

const (
	tabImages     ociTab = iota
	tabNetworks          // Networks tab
	tabVolumes           // Volumes tab
	tabRegistries        // Registries tab
)

// Model represents the OCI resources view state
type Model struct {
	config    *config.Config
	activeTab ociTab
	// Images tab
	images          []docker.Image
	scanCache       map[string]cache.ImageScanEntry
	scanningImages  map[string]bool
	failedScans     map[string]bool
	spinnerFrameIdx int
	lastScanOptions scan.ScanOptions
	imageTable      datatable.Model[imageRow]
	spinner         spinner.Model
	loading         bool
	scanning        bool
	// Networks tab

	networkTable datatable.Model[docker.Network]
	loadingNets  bool
	// Volumes tab

	volumeTable datatable.Model[docker.Volume]
	loadingVols bool
	// Registries tab
	registries          []config.RegistryItem
	registryTable       table.Model
	loadingRegs         bool
	registryForm        *RegistryForm
	registryBrowser     *RegistryBrowser
	registryLoginStatus map[string]bool // URL → logged in
	// groupCache is what the Members column reads: slug → last discovery. It is
	// loaded from disk once and updated as discoveries come back.
	groupCache map[string]cache.RegistryGroupEntry
	// refreshingGroups holds the slugs a discovery is running for, so ctrl+r on
	// a row already refreshing does not fire a second one.
	refreshingGroups map[string]bool
	// browserDeselected is what the user unchecked in the registry browser, kept
	// so the next open starts where the last one left off.
	browserDeselected map[string]bool
	// registryGroupSlug is the group the Registries tab has drilled into, empty
	// at the top level.
	registryGroupSlug string
	// launch options pending save — set on submit, cleared after successful or failed launch
	lastLaunchImage string
	lastLaunchOpts  *cache.LaunchOptionsEntry
	// Forms (viewport-based, Rule 112)
	launchForm         *LaunchForm
	resourceForm       *ResourceCreateForm
	networkInspectForm *NetworkInspectForm
	connectivityForm   *ConnectivityTestForm
	// Shared state
	errorMsg         string
	infoMsg          string
	confirmModal     *sharedcomponents.ConfirmModal
	pendingAction    string
	width, height    int
	selectionMode    bool
	selectionMessage string
}

// Messages — Images

// ImageScanStartingMsg signals that scanning is starting for a single image
type ImageScanStartingMsg struct {
	ImageName string
}

// ImageScanFinishedMsg signals that scanning has completed for a single image
type ImageScanFinishedMsg struct {
	ImageName string
	Entry     cache.ImageScanEntry
	Err       error
}

// RefreshTickMsg triggers periodic refresh
type RefreshTickMsg time.Time

// ImagesListMsg contains fetched images
type ImagesListMsg struct {
	Images []docker.Image
	Err    error
}

// ImageActionMsg signals result of an image action (remove)
type ImageActionMsg struct {
	Action string
	ID     string
	Err    error
}

// PruneCompleteMsg signals result of image prune
type PruneCompleteMsg struct {
	Output string
	Err    error
}

// ScanRequestMsg is sent when user requests a security scan on a single image
type ScanRequestMsg struct {
	ImageName string
}

// ImageSelectedMsg is sent when an image is selected in selection mode
type ImageSelectedMsg struct {
	ImageName string
}

// SelectionCancelledMsg is sent when user cancels image selection
type SelectionCancelledMsg struct{}

// ScanAllRequestMsg is sent when user wants to scan all unscanned images
type ScanAllRequestMsg struct {
	ImageNames []string
}

// ScanCacheLoadedMsg contains the loaded scan cache
type ScanCacheLoadedMsg struct {
	Entries map[string]cache.ImageScanEntry
}

// LaunchBatchScanMsg requests the OCI images view to start a batch scan of all images with configured options
type LaunchBatchScanMsg struct {
	Opts scan.ScanOptions
}

// LaunchSingleImageScanMsg requests the OCI images view to scan a single image with configured options
type LaunchSingleImageScanMsg struct {
	ImageName string
	Opts      scan.ScanOptions
}

// ScanDetailsRequestMsg is sent when the user wants to view cached scan details for a specific image
type ScanDetailsRequestMsg struct {
	ImageName string
}

// ResetSelectionMsg clears selection mode without losing other state (e.g. ongoing scans)
type ResetSelectionMsg struct{}

// Messages — Network Diagnostics

// NetworkInspectLoadedMsg contains the result of inspecting a network
type NetworkInspectLoadedMsg struct {
	NetworkID   string
	NetworkName string
	Containers  []docker.NetworkContainer
	Err         error
}

// DiagnosticTestCompleteMsg contains the result of a connectivity test
type DiagnosticTestCompleteMsg struct {
	Output string
	Err    error
}

// Messages — Networks

// NetworksListMsg contains fetched networks
type NetworksListMsg struct {
	Networks []docker.Network
	Err      error
}

// NetworkActionMsg signals result of a network action
type NetworkActionMsg struct {
	Action string
	Err    error
}

// NetworkPruneCompleteMsg signals result of network prune
type NetworkPruneCompleteMsg struct {
	Output string
	Err    error
}

// Messages — Volumes

// VolumesListMsg contains fetched volumes
type VolumesListMsg struct {
	Volumes []docker.Volume
	Err     error
}

// VolumeActionMsg signals result of a volume action
type VolumeActionMsg struct {
	Action string
	Err    error
}

// VolumePruneCompleteMsg signals result of volume prune
type VolumePruneCompleteMsg struct {
	Output string
	Err    error
}

// Messages — Registry Browser

// MultiRegistryTag represents a single tag result from one registry.
type MultiRegistryTag struct {
	RegistryURL string
	Alias       string
	Repo        string
	Tag         string
	UpdatedAt   time.Time
}

// MultiRegistryTagsLoadedMsg carries the tag list from one registry search.
type MultiRegistryTagsLoadedMsg struct {
	RegistryURL string
	Alias       string
	Repo        string
	Tags        []string
	Err         error
}

// MultiRegistryTagsMetaMsg carries last-updated times for tags from one registry (background enrichment).
type MultiRegistryTagsMetaMsg struct {
	RegistryURL string
	Repo        string
	Meta        map[string]time.Time
	Err         error
}

// RegistryPullCompleteMsg signals the result of a docker pull initiated from the browser.
type RegistryPullCompleteMsg struct {
	ImageName string
	Err       error
}

// RegistryTagDirectScanMsg requests a direct remote scan of a registry image tag (no pull).
type RegistryTagDirectScanMsg struct {
	ImageName string
}

// RegistryGroupDetectedMsg carries the result of a group detection for one registry.
// Members is nil when the registry is not a group or detection failed.
//
// Slug is what the cache and the table are keyed on; RegistryURL is what the
// browser still matches on. Two registries configured with the same URL collide
// there — the slug is the natural key, and step 6 is where the browser moves to
// it (D13's second half).
type RegistryGroupDetectedMsg struct {
	RegistryURL string
	Slug        string
	Members     []registrymgr.GroupMember
	Err         error
}

// RegistryGroupCacheLoadedMsg carries every cached group discovery, keyed by
// group slug.
type RegistryGroupCacheLoadedMsg struct {
	Entries map[string]cache.RegistryGroupEntry
}

// BrowserSelectionLoadedMsg carries the registries this context had left
// unchecked in the browser.
type BrowserSelectionLoadedMsg struct {
	Deselected map[string]bool
}

// Messages — Registries

// RegistryLoginCompleteMsg signals result of a docker login operation
type RegistryLoginCompleteMsg struct {
	RegistryURL string
	Err         error
}

// RegistryLogoutCompleteMsg signals result of a docker logout operation
type RegistryLogoutCompleteMsg struct {
	RegistryURL string
	Err         error
}

// RegistryLoginStatusMsg carries the login status for all configured registries
type RegistryLoginStatusMsg struct {
	Status map[string]bool // URL → logged in
}

// Messages — Container launch

// LaunchOptionsCacheLoadedMsg contains cached launch options for an image.
// Entry is nil when no cached options exist for the image.
type LaunchOptionsCacheLoadedMsg struct {
	ImageName string
	Entry     *cache.LaunchOptionsEntry
}

// ClipboardCopyMsg signals the result of a clipboard copy operation
type ClipboardCopyMsg struct {
	Err error
}

// ImageExposedPortsMsg contains exposed ports fetched from an image
type ImageExposedPortsMsg struct {
	ImageName string
	Ports     []string
	Err       error
}

// ContainerLaunchCompleteMsg signals result of launching a container
type ContainerLaunchCompleteMsg struct {
	ContainerID string
	Err         error
}

// New creates a new OCI resources view
func New(cfg *config.Config) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.SpinnerStyle()

	it := datatable.New(datatable.Config[imageRow]{
		Columns:    imageColumns(),
		SortColumn: imageColumnName,
	})

	nt := datatable.New(datatable.Config[docker.Network]{Columns: networkColumns(), SortColumn: -1})
	nt.Blur()

	vt := datatable.New(datatable.Config[docker.Volume]{Columns: volumeColumns(), SortColumn: -1})
	vt.Blur()

	regColumns := []table.Column{
		{Title: "Alias", Width: 16},
		{Title: "URL", Width: 30},
		{Title: "Kind", Width: 10},
		{Title: "Auth", Width: 12}, // holds "credentials"
		{Title: "Logged", Width: 8},
		{Title: "Members", Width: 16},
	}
	rt := table.New(
		table.WithColumns(regColumns),
		table.WithFocused(false),
		table.WithHeight(10),
	)
	rt.SetStyles(theme.DefaultTableStyles())

	return Model{
		config:              cfg,
		activeTab:           tabImages,
		scanCache:           make(map[string]cache.ImageScanEntry),
		scanningImages:      make(map[string]bool),
		failedScans:         make(map[string]bool),
		spinner:             s,
		imageTable:          it,
		networkTable:        nt,
		volumeTable:         vt,
		registryTable:       rt,
		registries:          cfg.Registry.Registries,
		registryLoginStatus: make(map[string]bool),
		groupCache:          make(map[string]cache.RegistryGroupEntry),
		refreshingGroups:    make(map[string]bool),
		browserDeselected:   make(map[string]bool),
		loading:             true,
		loadingNets:         true,
		loadingVols:         true,
	}
}

// NewForSelection creates an OCI images view in selection mode for picking an image
func NewForSelection(cfg *config.Config, message string) Model {
	m := New(cfg)
	m.selectionMode = true
	m.selectionMessage = message
	return m
}
