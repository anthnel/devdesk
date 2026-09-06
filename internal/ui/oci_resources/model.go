package ociresources

import (
	tea "github.com/charmbracelet/bubbletea"

	"context"

	"time"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/registrymgr"
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
	images []docker.Image
	// listed says the daemon has answered at least once — empty is then a real
	// answer rather than "not asked yet", which is what an agent's request has
	// to be able to tell apart (§3.61).
	listed bool
	// pendingRequests holds what an agent asked for before that answer came.
	pendingRequests []tea.Msg
	scanCache       map[string]cache.ImageScanEntry
	// jobs is the router snapshot of everything running anywhere, and jobFrame
	// the spinner frame that goes with it — bare, because it lands in a table
	// cell (Rule 122). It replaced a scanningImages map and the `scanning`
	// bool derived from it, both of which knew only what this view launched.
	//
	// spinnerFrameIdx below stays: it animates *loading*, and a load is not a
	// job. See jobs.go.
	jobs            []jobs.Run
	jobFrame        string
	failedScans     map[string]bool
	spinnerFrameIdx int
	imageTable      datatable.Model[imageRow]
	spinner         spinner.Model
	loading         bool
	// Networks tab

	networkTable datatable.Model[docker.Network]
	loadingNets  bool
	// Volumes tab

	volumeTable datatable.Model[docker.Volume]
	loadingVols bool
	// Registries tab
	registries          []config.RegistryItem
	registryTable       datatable.Model[registryRow]
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
	// browserDeselected holds the keys of the entries the user unchecked in the
	// registry browser, kept so the next open starts where the last one left
	// off. Keyed on the browser's entry key rather than a URL — see
	// browserRegistryEntry.key (D40).
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
	// pruning names what a prune is working on, empty when none is. A prune
	// acts on no row — marking every row would say something false — so it gets
	// a footer line of its own instead.
	pruning string
	// Shared state
	// footer is the one line of transient state below the tab bar (Rule 128).
	footer        sharedcomponents.FooterMessage
	confirmModal  *sharedcomponents.ConfirmModal
	pendingAction string
	// scanAllModal carries A's purge checkbox. Separate from confirmModal
	// because the two answer different messages, and one field holding either
	// would make the handler guess which question was asked.
	scanAllModal  *sharedcomponents.OptionConfirmModal
	width, height int
}

// Messages — Images

// ImageScanStartingMsg signals that scanning is starting for a single image
type ImageScanStartingMsg struct {
	ImageName string

	// Cancel stops the scan, and is what makes `K` on this row mean anything
	// (D7). It rides on the starting message because the context is created
	// inside the Cmd: the registry stores it in the same Update that marks the
	// item running, so there is no window where the row is running and cannot
	// be stopped.
	Cancel context.CancelFunc
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

// ImageActionMsg signals result of an image action (remove).
//
// ID used to hold the image *reference*, not its ID: removeImageCmd filled it
// from its `name` argument. Nothing caught it because the only reader was a log
// line. It matters now — the busy marker is keyed on the ID, so a message
// carrying a name would never lift it and the row would spin for good.
type ImageActionMsg struct {
	Action string
	ID     string
	Name   string
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

// ScanAllRequestMsg is sent when user wants to scan all unscanned images
type ScanAllRequestMsg struct {
	ImageNames []string
}

// ScanCacheLoadedMsg contains the loaded scan cache
type ScanCacheLoadedMsg struct {
	Entries map[string]cache.ImageScanEntry
}

// ScanDetailsRequestMsg is sent when the user wants to view cached scan details for a specific image
type ScanDetailsRequestMsg struct {
	ImageName string
}

// Messages — Network Diagnostics

// NetworkInspectLoadedMsg contains the result of inspecting a network
type NetworkInspectLoadedMsg struct {
	NetworkID   string
	NetworkName string
	Containers  []docker.NetworkContainer
	Err         error
}

// Messages — Networks

// NetworksListMsg contains fetched networks
type NetworksListMsg struct {
	Networks []docker.Network
	Err      error
}

// NetworkActionMsg signals result of a network action.
//
// ID names which network it was. The message carried no identity at all, which
// was survivable while the only thing it did was refetch the list — a busy
// marker has to be lifted from the row it was put on.
type NetworkActionMsg struct {
	Action string
	ID     string
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

// VolumeActionMsg signals result of a volume action.
//
// Name is a volume's identity — it has no ID of its own. Same reason as
// NetworkActionMsg: the message carried nothing to lift a marker from.
type VolumeActionMsg struct {
	Action string
	Name   string
	Err    error
}

// VolumePruneCompleteMsg signals result of volume prune
type VolumePruneCompleteMsg struct {
	Output string
	Err    error
}

// Messages — Registry Browser

// MultiRegistryTag represents a single tag result from one registry.
//
// EntryKey is what the result is attributed by. RegistryURL no longer
// identifies anything on its own: several entries share one host once a
// registry is an address — a host plus a repo prefix — rather than a URL
// (§3.18). It is kept because it is what the pull reference is built from.
// Repo already carries the prefix, applied once in submitSearch.
type MultiRegistryTag struct {
	EntryKey    string
	RegistryURL string
	Alias       string
	Repo        string
	Tag         string
	UpdatedAt   time.Time
}

// MultiRegistryTagsLoadedMsg carries the tag list from one registry search.
type MultiRegistryTagsLoadedMsg struct {
	EntryKey    string
	RegistryURL string
	Alias       string
	Repo        string
	Tags        []string
	Err         error
}

// MultiRegistryTagsMetaMsg carries last-updated times for tags from one registry (background enrichment).
type MultiRegistryTagsMetaMsg struct {
	EntryKey    string
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

// RegistryPullRequestedMsg asks the parent model to launch a pull as a job.
// The browser emits this instead of calling docker.PullImage itself, so the
// pull is admitted through jobs.Start like the direct scan already is.
type RegistryPullRequestedMsg struct{ ImageName string }

// RegistryPullStartingMsg signals that a pull has been admitted and is running.
type RegistryPullStartingMsg struct {
	ImageName string

	// Cancel stops the pull, and is what makes `K` on this row mean anything
	// (D7) — cutting one leaves nothing behind, Docker resuming a pull by
	// layer. It rides on the starting message for the same reason the scan's
	// does: the context is created inside the Cmd, and the registry stores it
	// in the same Update that marks the item running.
	Cancel context.CancelFunc
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
		// A pulling placeholder row carries no Image yet, so its ID is empty —
		// falling back to the name keeps two concurrent pulls from colliding
		// on the same key.
		Key: func(r imageRow) string {
			if r.Image.ID == "" {
				return "pull:" + r.RawName
			}
			return r.Image.ID
		},
		// The ID cell, not a column of its own. §3.16 settled that argument for
		// the clone checkbox: a column costs cells on every screen to say
		// nothing on all but one row, and at 80 columns this view has none to
		// spare. The ID is the cell to spend — it neither sorts nor searches,
		// twelve hex characters being nothing anyone orders or looks for, and
		// it is not what the user is watching while the image is removed.
		StatusColumn: imageColumnID,
	})

	nt := datatable.New(datatable.Config[docker.Network]{
		Columns:      networkColumns(),
		SortColumn:   -1,
		Key:          func(n docker.Network) string { return n.ID },
		StatusColumn: networkColumnID,
	})
	nt.Blur()

	vt := datatable.New(datatable.Config[docker.Volume]{
		Columns:    volumeColumns(),
		SortColumn: -1,
		// A volume has no ID: its name is its identity, which is also why the
		// spinner cannot go there — it is the one cell that says which row this
		// is. Driver is the expendable one, and it reads "local" on very nearly
		// every volume there has ever been.
		Key:          func(v docker.Volume) string { return v.Name },
		StatusColumn: volumeColumnDriver,
	})
	vt.Blur()

	rt := datatable.New(datatable.Config[registryRow]{
		Columns:    registryColumns(),
		SortColumn: -1,
		// The URL, because that is what docker keys a login on and what
		// RegistryLoginCompleteMsg carries back. Two entries declared on one
		// host therefore spin together — which is right: one `docker login`
		// really does change the answer for both. This is the one place D40's
		// rule does not apply, and it does not apply because the *operation* is
		// host-scoped, not because the identity is convenient.
		Key: func(r registryRow) string { return r.url },
		// Logged is the cell a login is about to change, so it is the one to
		// spend while it runs.
		StatusColumn: registryColumnLogged,
	})
	rt.Blur()

	return Model{
		config:              cfg,
		activeTab:           tabImages,
		scanCache:           make(map[string]cache.ImageScanEntry),
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
