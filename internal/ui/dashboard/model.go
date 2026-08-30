package dashboard

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/metrics"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/status"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// Messages

// StatusCheckMsg contains results from a background status check
type StatusCheckMsg struct {
	Result status.MonitorResult
}

// ForgeStatsMsg contains fetched GitLab statistics
type ForgeStatsMsg struct {
	Stats forge.DashboardStats
}

// DockerStatsMsg contains fetched Docker statistics
type DockerStatsMsg struct {
	Stats shared.DockerStats
}

// OCIStatsMsg contains fetched OCI resource statistics
type OCIStatsMsg struct {
	Stats shared.OCIStats
}

// WorkspaceStatsMsg contains the workspace count.
//
// Il portait aussi la taille de l'arborescence, mesurée par `du -sh`. Le champ
// est retiré avec son producteur : un champ que plus personne ne remplit se lit
// comme une donnée qu'on a oublié d'afficher.
type WorkspaceStatsMsg struct {
	Count int
}

// ToolsDetectedMsg contains detected tool information
type ToolsDetectedMsg struct {
	Tools []shared.ToolInfo
}

// RefreshTickMsg triggers a periodic refresh
type RefreshTickMsg time.Time

// Trois horloges, et c'est le coût mesuré qui les sépare : un échantillon
// gopsutil coûte ≈ 5 ms, `docker stats --no-stream` ≈ 2 s, et le tour lent
// (GitLab, `system df`, workspaces, outils) ≈ 600 ms plus le réseau. Une seule
// horloge pour les trois ferait attendre l'appel bon marché derrière le cher.
const (
	hostTickInterval   = time.Second
	dockerTickInterval = 5 * time.Second
)

// HostTickMsg schedules the next host sample.
type HostTickMsg time.Time

// HostSampleMsg carries a host reading and the cumulative counters the next
// sample must be measured against — l'état du débit voyage avec le message
// plutôt que d'être modifié dans un Cmd (Rule 110).
type HostSampleMsg struct {
	Sample   metrics.HostSample
	Counters metrics.Counters
}

// DockerTickMsg schedules the next docker stats aggregate.
type DockerTickMsg time.Time

// DockerMetricsMsg carries what the running containers add up to.
type DockerMetricsMsg struct {
	Aggregate docker.Aggregate
}

// DiskUsageMsg carries free space on the workspaces volume.
type DiskUsageMsg struct {
	Workspaces metrics.DiskUsage
}

// WorkspaceSizeMsg carries what the workspaces tree occupies.
type WorkspaceSizeMsg struct {
	Size metrics.TreeSize
}

// PostureMsg carries what the scan caches say about this context.
type PostureMsg struct {
	Posture posture
}

// dashboardTab identifies the tab on screen. Un onglet n'existe que pour un
// contenu qui n'a pas de vue à lui : `Resources` en est un parce qu'il n'y a
// pas de :host et que :containers montre un conteneur, pas la machine. Un
// onglet Health réimprimerait les lignes que :status et :sec possèdent déjà.
type dashboardTab int

const (
	tabOverview dashboardTab = iota
	tabResources
	tabCount
)

// Model represents the dashboard view state
type Model struct {
	config    *config.Config
	shared    *shared.State
	width     int
	height    int
	activeTab dashboardTab

	// chartLines is measured by View() on a copy of the model — voir
	// fitCharts. Il n'est jamais écrit par Update() : ce n'est pas un état,
	// c'est un résultat de mise en page qui ne survit pas à la frame.
	chartLines int

	// Data
	serviceComponents []status.ComponentStatus
	serviceStatus     shared.ServiceGlobalStatus
	forgeStats        *forge.DashboardStats
	dockerStats       *shared.DockerStats
	ociStats          *shared.OCIStats
	tools             []shared.ToolInfo
	workspaceCount    int

	// Host and Docker samples. `samples` is the history the charts read in
	// phase 3: elle appartient au modèle et non au graphe, parce que
	// ntcharts.Resize rééchelonne son propre ring buffer — un changement de
	// palier tronquerait l'historique au moment où l'utilisateur agrandit la
	// fenêtre pour en voir davantage.
	host             metrics.HostSample
	netCounters      metrics.Counters
	samples          []metrics.HostSample
	dockerAgg        docker.Aggregate
	dockerSamples    []float64
	dockerMemSamples []float64
	dockerRead       bool
	wsDisk           metrics.DiskUsage
	wsSize           metrics.TreeSize
	posture          posture

	// Loading flags
	loadingServices   bool
	loadingForge      bool
	loadingDocker     bool
	loadingOCI        bool
	loadingWorkspaces bool
	loadingTools      bool

	// measuringSize is not a loading flag: il **exclut** un second parcours
	// tant que le premier n'a pas répondu. C'est le seul appel du dashboard
	// dont la durée grandit avec les données de l'utilisateur, donc le seul qui
	// puisse déborder de son intervalle — et deux parcours concurrents
	// doubleraient l'I/O pour un chiffre déjà en route.
	measuringSize bool

	// Refresh
	refreshInterval time.Duration

	// footer is the one line of transient state below the viewport (Rule 128).
	// The dashboard budgets an info line and left it permanently empty; a key
	// the header greys out has to be able to say why it declined (Rule 130).
	footer sharedcomponents.FooterMessage

	// jobs is the router's snapshot of what is running. The dashboard starts
	// none of it and shows only the count — it is the one screen a user is
	// likely to be looking at while a batch runs somewhere else, which is
	// exactly the case D67 made possible and D9 has to answer for.
	jobs []jobs.Run
}

// New creates a new dashboard model
func New(cfg *config.Config, state *shared.State) Model {
	return Model{
		config:            cfg,
		shared:            state,
		serviceStatus:     shared.ServiceStatusUnknown,
		loadingServices:   true,
		loadingForge:      true,
		loadingDocker:     true,
		loadingOCI:        true,
		loadingWorkspaces: true,
		loadingTools:      true,
		// Init() lance le parcours : le drapeau est donc levé dès la
		// construction, sinon le premier tick lent en lancerait un second.
		measuringSize:   true,
		refreshInterval: time.Duration(cfg.Status.RefreshInterval) * time.Second,
	}
}

// Init initializes the dashboard by loading all data
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.checkServices(),
		m.fetchForgeStats(),
		m.fetchDockerStats(),
		m.fetchOCIStats(),
		m.fetchWorkspaceStats(),
		m.detectTools(),
		m.scheduleRefresh(),
		m.sampleHost(),
		m.scheduleHostTick(),
		m.fetchDockerMetrics(),
		m.scheduleDockerTick(),
		m.fetchDiskUsage(),
		m.measureWorkspaceSize(),
		m.fetchPosture(),
	)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Un terminal qui s'agrandit jusqu'à `wide` met les boîtes de Resources
		// à l'écran et retire l'onglet : y rester laisserait la vue sur un
		// onglet qui n'existe plus.
		if m.activeTab >= dashboardTab(tabCountFor(layoutTier(m.width, m.height))) {
			m.activeTab = tabOverview
		}

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case jobs.ChangedMsg:
		m.jobs = msg.Runs
		m.footer.SetSpinnerFrame(msg.RenderedFrame)
		return m, nil

	case StatusCheckMsg:
		return m.handleStatusCheck(msg)

	case ForgeStatsMsg:
		return m.handleForgeStats(msg)

	case DockerStatsMsg:
		return m.handleDockerStats(msg)

	case OCIStatsMsg:
		return m.handleOCIStats(msg)

	case WorkspaceStatsMsg:
		return m.handleWorkspaceStats(msg)

	case ToolsDetectedMsg:
		return m.handleToolsDetected(msg)

	case RefreshTickMsg:
		return m.alsoMeasuringSize(m.refreshAll())

	case HostTickMsg:
		return m, tea.Batch(m.sampleHost(), m.scheduleHostTick())

	case HostSampleMsg:
		return m.handleHostSample(msg)

	case DockerTickMsg:
		return m, tea.Batch(m.fetchDockerMetrics(), m.scheduleDockerTick())

	case DockerMetricsMsg:
		return m.handleDockerMetrics(msg)

	case DiskUsageMsg:
		m.wsDisk = msg.Workspaces

	case WorkspaceSizeMsg:
		m.wsSize = msg.Size
		m.measuringSize = false

	case PostureMsg:
		m.posture = msg.Posture
	}

	// The footer consumes the expiry addressed to it; a message posted without
	// its timer being handled never clears (Rule 128).
	m.footer.Handle(msg)

	return m, nil
}

// handleKeyMsg processes keyboard input
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "shift+tab":
		return m.cycleTab(msg.String() == "tab"), nil
	case "ctrl+r":
		return m.handleReload()
	// The two deep links are the forge's: the paths differ per forge, and
	// building them here with fmt.Sprintf was a forge shape written in a view.
	// Both are greyed without a session (Rule 130) and both say so when pressed
	// anyway — they used to fall through and do nothing at all.
	case keymap.Requests:
		if links := m.forgeLinks(); !links.Enabled() {
			return m, m.footer.Warn(links.Reason)
		}
		return m, openURL(m.shared.Forge.ChangeRequestsURL())
	case keymap.Issues:
		if links := m.forgeLinks(); !links.Enabled() {
			return m, m.footer.Warn(links.Reason)
		}
		return m, openURL(m.shared.Forge.AssignedIssuesURL(m.shared.CurrentUser))
	}
	return m, nil
}

// reasonNoSession is why the two deep links do not apply. It names the view
// that fixes it rather than the state, because that is what the user does next.
var reasonNoSession = "Not signed in — open :" + string(command.ViewGitAuth)

// forgeLinks reports whether the forge deep links can be built.
//
// One state for both keys because it is one condition: a session. Reading
// IsAuthenticated in one place and the backend pointer in another is how R came
// to check less than I did — R opened a URL from a backend with no session.
func (m Model) forgeLinks() shortcut.Availability {
	if m.shared == nil || m.shared.Forge == nil || !m.shared.IsAuthenticated {
		return shortcut.Unavailable(reasonNoSession)
	}
	return shortcut.Availability{}
}

// handleReload is what ctrl+r does: every section goes back to loading, and the
// slow round runs at once.
func (m Model) handleReload() (tea.Model, tea.Cmd) {
	m.loadingServices = true
	m.loadingForge = true
	m.loadingDocker = true
	m.loadingOCI = true
	m.loadingWorkspaces = true
	m.loadingTools = true
	return m.alsoMeasuringSize(m.refreshNow())
}

// alsoMeasuringSize adds the workspaces walk to a round, **unless one is still
// running**. Il n'est pas dans refreshAll pour cette seule raison : c'est le
// seul appel qui peut durer plus longtemps que l'intervalle qui le déclenche,
// et un tour lent n'a aucune façon de le savoir.
//
// Le drapeau est levé ici, dans Update(), jamais dans le Cmd (Rule 110).
func (m Model) alsoMeasuringSize(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if m.measuringSize {
		return m, cmd
	}
	m.measuringSize = true
	return m, tea.Batch(cmd, m.measureWorkspaceSize())
}

// cycleTab moves to the next or previous tab the current palier offers. À
// `wide` il n'y en a qu'un, et la touche ne fait donc rien.
func (m Model) cycleTab(forward bool) Model {
	n := dashboardTab(tabCountFor(layoutTier(m.width, m.height)))
	if n < 2 {
		return m
	}
	if forward {
		m.activeTab = (m.activeTab + 1) % n
	} else {
		m.activeTab = (m.activeTab + n - 1) % n
	}
	return m
}

// maxSamples bounds the history the charts read. À 1 échantillon par seconde,
// 240 couvrent quatre minutes — plus que les 200 qu'un graphe braille de 100
// cellules peut montrer, donc un élargissement de fenêtre a de quoi se remplir.
const maxSamples = 240

// handleHostSample records a reading and the counters the next one is measured
// against. Le Push appartient à Update(), jamais à un Cmd (Rule 110).
func (m Model) handleHostSample(msg HostSampleMsg) (tea.Model, tea.Cmd) {
	m.host = msg.Sample
	m.netCounters = msg.Counters

	// Un échantillon sans débit est gardé quand même : ses valeurs CPU et
	// mémoire sont mesurées, c'est le seul débit qui manque.
	m.samples = append(m.samples, msg.Sample)
	if len(m.samples) > maxSamples {
		m.samples = m.samples[len(m.samples)-maxSamples:]
	}
	return m, nil
}

// handleDockerMetrics records the container aggregate and its history. Une
// mesure ratée n'entre pas dans l'historique : un zéro y ressemblerait à un
// creux d'activité au lieu d'une absence de mesure.
func (m Model) handleDockerMetrics(msg DockerMetricsMsg) (tea.Model, tea.Cmd) {
	m.dockerAgg = msg.Aggregate
	m.dockerRead = true

	if msg.Aggregate.Available {
		m.dockerSamples = appendBounded(m.dockerSamples, msg.Aggregate.CPUPercent)
		m.dockerMemSamples = appendBounded(m.dockerMemSamples, msg.Aggregate.MemPercent)
	}
	return m, nil
}

// appendBounded keeps the newest maxSamples readings of a series.
func appendBounded(series []float64, value float64) []float64 {
	series = append(series, value)
	if len(series) > maxSamples {
		return series[len(series)-maxSamples:]
	}
	return series
}

// handleStatusCheck processes status check results
func (m Model) handleStatusCheck(msg StatusCheckMsg) (tea.Model, tea.Cmd) {
	m.loadingServices = false
	m.serviceComponents = msg.Result.Components

	m.serviceStatus = computeGlobalStatus(m.serviceComponents)

	m.shared.ServiceStatus = m.serviceStatus
	m.shared.ServiceComponents = m.serviceComponents

	return m, nil
}

// handleForgeStats processes GitLab stats results
func (m Model) handleForgeStats(msg ForgeStatsMsg) (tea.Model, tea.Cmd) {
	m.loadingForge = false
	m.forgeStats = &msg.Stats
	m.shared.ForgeStats = &msg.Stats
	return m, nil
}

// handleDockerStats processes Docker stats results
func (m Model) handleDockerStats(msg DockerStatsMsg) (tea.Model, tea.Cmd) {
	m.loadingDocker = false
	m.dockerStats = &msg.Stats
	m.shared.DockerStats = &msg.Stats
	return m, nil
}

// handleOCIStats processes OCI stats results
func (m Model) handleOCIStats(msg OCIStatsMsg) (tea.Model, tea.Cmd) {
	m.loadingOCI = false
	m.ociStats = &msg.Stats
	m.shared.OCIStats = &msg.Stats
	return m, nil
}

// handleWorkspaceStats processes workspace stats results
func (m Model) handleWorkspaceStats(msg WorkspaceStatsMsg) (tea.Model, tea.Cmd) {
	m.loadingWorkspaces = false
	m.workspaceCount = msg.Count
	m.shared.WorkspaceCount = msg.Count
	return m, nil
}

// handleToolsDetected processes tool detection results
func (m Model) handleToolsDetected(msg ToolsDetectedMsg) (tea.Model, tea.Cmd) {
	m.loadingTools = false
	m.tools = msg.Tools
	m.shared.Tools = msg.Tools
	return m, nil
}

// Commands

func (m Model) checkServices() tea.Cmd {
	cfg := m.config
	return func() tea.Msg {
		result := status.RunCheck(cfg)
		return StatusCheckMsg{Result: result}
	}
}

func (m Model) fetchForgeStats() tea.Cmd {
	backend := m.shared.Forge
	user := m.shared.CurrentUser

	// No session means no counter was read, which is not the same as five
	// zeros — the zero DashboardStats says exactly that (D52).
	if backend == nil || !m.shared.IsAuthenticated {
		return func() tea.Msg {
			return ForgeStatsMsg{}
		}
	}

	return func() tea.Msg {
		stats, err := backend.DashboardStats(context.Background(), user)
		if err != nil {
			log.Printf("ERROR [dashboard] fetching forge stats: %v", err)
		}
		return ForgeStatsMsg{Stats: stats}
	}
}

func (m Model) fetchDockerStats() tea.Cmd {
	return func() tea.Msg {
		stats := docker.FetchContainerStats()
		return DockerStatsMsg{Stats: shared.DockerStats{
			Available: stats.Available,
			Running:   stats.Running,
			Stopped:   stats.Stopped,
			Paused:    stats.Paused,
		}}
	}
}

func (m Model) fetchOCIStats() tea.Cmd {
	return func() tea.Msg {
		stats := docker.FetchOCIStats()
		return OCIStatsMsg{Stats: shared.OCIStats{
			Available:       stats.Available,
			ImagesCount:     stats.ImagesCount,
			ImagesSize:      stats.ImagesSize,
			ContainersCount: stats.ContainersCount,
			ContainersSize:  stats.ContainersSize,
			VolumesCount:    stats.VolumesCount,
			VolumesSize:     stats.VolumesSize,
			NetworksCount:   stats.NetworksCount,
			BuildCacheSize:  stats.BuildCacheSize,
			Reclaimable:     stats.Reclaimable,
		}}
	}
}

// fetchWorkspaceStats counts the workspaces — un `os.ReadDir` d'un seul niveau.
// La taille de l'arborescence est mesurée à part, par measureWorkspaceSize :
// elle parcourt tout, donc elle a sa propre garde contre le recouvrement, que
// ce compte-ci n'a aucune raison de payer.
func (m Model) fetchWorkspaceStats() tea.Cmd {
	dir := expandHome(m.config.App.WorkspacesDir)
	return func() tea.Msg {
		entries, err := os.ReadDir(dir)
		count := 0
		if err == nil {
			for _, e := range entries {
				if e.IsDir() {
					count++
				}
			}
		}
		return WorkspaceStatsMsg{Count: count}
	}
}

// expandHome resolves a leading ~/ against the user's home directory.
func expandHome(dir string) string {
	if len(dir) >= 2 && dir[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, dir[2:])
	}
	return dir
}

func (m Model) detectTools() tea.Cmd {
	cfg := m.config
	return func() tea.Msg {
		var tools []shared.ToolInfo

		// Docker
		tools = append(tools, detectBinaryTool(toolDocker, "docker", "version", "--format", "{{.Client.Version}}"))

		// Security tools via scan.CheckDependencies. The whole ScanConfig goes
		// through: the configured tool paths and the per-tool source preference
		// decide availability as much as the images do (D27).
		deps := scan.CheckDependencies(cfg.Scan)
		tools = append(tools, shared.ToolInfo{
			Name:      toolTrivy,
			Available: deps.TrivyAvailable,
			Version:   cleanVersion(deps.TrivyVersion),
			Source:    string(deps.TrivySource),
		})
		tools = append(tools, shared.ToolInfo{
			Name:      toolGitleaks,
			Available: deps.GitleaksAvailable,
			Version:   cleanVersion(deps.GitleaksVersion),
			Source:    string(deps.GitleaksSource),
		})
		tools = append(tools, shared.ToolInfo{
			Name:      toolPlumber,
			Available: deps.PlumberAvailable,
			Version:   cleanVersion(deps.PlumberVersion),
			Source:    string(deps.PlumberSource),
		})

		// The OCI connectivity test image — la seule que l'application démarre
		// encore (§3.47).
		tools = append(tools, detectDockerImage(toolConnectivity, cfg.Network.ConnectivityImage))

		// Git
		tools = append(tools, detectBinaryTool(toolGit, "git", "--version"))

		return ToolsDetectedMsg{Tools: tools}
	}
}

// sampleHost reads the host. Les compteurs précédents sont *copiés* dans le
// Cmd et les nouveaux reviennent par message : rien de partagé n'est modifié.
func (m Model) sampleHost() tea.Cmd {
	prev := m.netCounters
	return func() tea.Msg {
		sample, next := metrics.SampleHost(prev)
		return HostSampleMsg{Sample: sample, Counters: next}
	}
}

func (m Model) scheduleHostTick() tea.Cmd {
	return tea.Tick(hostTickInterval, func(t time.Time) tea.Msg {
		return HostTickMsg(t)
	})
}

// fetchDockerMetrics runs `docker stats`, which costs about two seconds — d'où
// son horloge propre.
func (m Model) fetchDockerMetrics() tea.Cmd {
	return func() tea.Msg {
		return DockerMetricsMsg{Aggregate: docker.FetchAggregateMetrics()}
	}
}

func (m Model) scheduleDockerTick() tea.Cmd {
	return tea.Tick(dockerTickInterval, func(t time.Time) tea.Msg {
		return DockerTickMsg(t)
	})
}

// fetchDiskUsage reads free space on the workspaces volume — 1 ms, contre
// plusieurs secondes pour un `du -sh` dont le coût grandit avec les données de
// l'utilisateur. Elle reste sur l'horloge lente : la place libre ne bouge pas
// en une seconde.
func (m Model) fetchDiskUsage() tea.Cmd {
	dir := expandHome(m.config.App.WorkspacesDir)
	return func() tea.Msg {
		return DiskUsageMsg{Workspaces: metrics.Disk(dir)}
	}
}

// measureWorkspaceSize walks the workspaces tree. Le chemin est **copié** dans
// le Cmd, comme partout ailleurs (Rule 110).
//
// C'est le `du -sh` que §3.19 avait retiré, remis délibérément et à sa place :
// il ne tourne plus à chaque rafraîchissement du dashboard mais une fois par
// tour lent, et jamais pendant qu'un autre tourne (voir alsoMeasuringSize). La
// question qu'il répond — combien ces dépôts coûtent — n'a pas d'autre source.
func (m Model) measureWorkspaceSize() tea.Cmd {
	dir := expandHome(m.config.App.WorkspacesDir)
	return func() tea.Msg {
		return WorkspaceSizeMsg{Size: metrics.Size(dir)}
	}
}

// fetchPosture reads the two scan caches. Il **ne lance aucun scan** (Rule 126) :
// c'est ce qui relie le dashboard à l'inventaire de §3.11 pour le prix d'une
// lecture de fichier.
func (m Model) fetchPosture() tea.Cmd {
	context := config.CurrentContextName()
	return func() tea.Msg {
		// L'énumération des images est faite ici et non dans readPosture : c'est
		// le seul appel au démon de la lecture, et un Cmd est l'endroit de
		// l'I/O. Elle coûte un `docker image ls` par tour lent, sur la même
		// horloge que le `docker system df` de fetchOCIStats.
		images, known := docker.ImageNames()
		return PostureMsg{Posture: readPosture(context, images, known)}
	}
}

func (m Model) scheduleRefresh() tea.Cmd {
	interval := m.refreshInterval
	if interval < 10*time.Second {
		interval = 30 * time.Second
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return RefreshTickMsg(t)
	})
}

// refreshAll runs the slow clock's work. Il ne réarme que son horloge : les
// deux autres ont la leur, et un tour lent qui les relance ferait deux chaînes
// de ticks pour la même horloge, donc des échantillons deux fois plus rapides.
func (m Model) refreshAll() tea.Cmd {
	return tea.Batch(
		m.checkServices(),
		m.fetchForgeStats(),
		m.fetchDockerStats(),
		m.fetchOCIStats(),
		m.fetchWorkspaceStats(),
		m.fetchDiskUsage(),
		m.fetchPosture(),
		m.scheduleRefresh(),
	)
}

// refreshNow is what ctrl+r does: the slow round plus one immediate reading
// from each of the other two clocks, **sans réarmer** leurs ticks.
func (m Model) refreshNow() tea.Cmd {
	return tea.Batch(
		m.refreshAll(),
		m.sampleHost(),
		m.fetchDockerMetrics(),
		m.detectTools(),
	)
}

// computeGlobalStatus determines overall service health
func computeGlobalStatus(components []status.ComponentStatus) shared.ServiceGlobalStatus {
	if len(components) == 0 {
		return shared.ServiceStatusUnknown
	}

	okCount := 0
	for _, c := range components {
		if c.Status == status.StatusOK {
			okCount++
		}
	}

	if okCount == len(components) {
		return shared.ServiceStatusAllOK
	}
	if okCount == 0 {
		return shared.ServiceStatusDown
	}
	return shared.ServiceStatusDegraded
}

// InEditMode returns false - dashboard has no edit modes
func (m Model) InEditMode() bool {
	return false
}

// detectDockerImage checks if a Docker image is available locally
func detectDockerImage(name, image string) shared.ToolInfo {
	tool := shared.ToolInfo{Name: name, Version: image, Source: "image"}
	cmd := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", image)
	if err := cmd.Run(); err == nil {
		tool.Available = true
	}
	return tool
}

// detectBinaryTool checks if a binary is available and gets its version
func detectBinaryTool(name, binary string, versionArgs ...string) shared.ToolInfo {
	tool := shared.ToolInfo{Name: name}

	if _, err := exec.LookPath(binary); err != nil {
		return tool
	}

	tool.Available = true
	tool.Source = "binary"

	if len(versionArgs) > 0 {
		cmd := exec.Command(binary, versionArgs...)
		if out, err := cmd.Output(); err == nil {
			tool.Version = cleanVersion(string(out))
		}
	}

	return tool
}

// cleanVersion removes whitespace and "docker:" prefix from version strings
func cleanVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "docker:")
	v = strings.TrimSpace(v)
	// Take first line only
	if idx := strings.IndexByte(v, '\n'); idx >= 0 {
		v = v[:idx]
	}
	// Clean "git version X.Y.Z" -> "X.Y.Z"
	v = strings.TrimPrefix(v, "git version ")
	// Clean "Version: X.Y.Z" -> "X.Y.Z" (Trivy)
	v = strings.TrimPrefix(v, "Version: ")
	return v
}

// openURL opens a URL in the default browser
func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		_ = cmd.Start()
		return nil
	}
}

// vocab is the wording of the forge this context targets, resolved from the
// config. The Code box and its help name it, and both are rendered whether or
// not a session is open.
func (m Model) vocab() forge.Vocabulary {
	if m.config == nil {
		return forge.VocabularyFor("")
	}
	return forge.VocabularyFor(m.config.Forge.Type)
}

// forgeType is the configured platform, empty-safe.
func (m Model) forgeType() string {
	if m.config == nil {
		return ""
	}
	return m.config.Forge.Type
}
