package dashboard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gitlab.com/anthnell/devsecops/devdesk/internal/config"
	"gitlab.com/anthnell/devsecops/devdesk/internal/docker"
	gitlabpkg "gitlab.com/anthnell/devsecops/devdesk/internal/gitlab"
	"gitlab.com/anthnell/devsecops/devdesk/internal/scan"
	"gitlab.com/anthnell/devsecops/devdesk/internal/shared"
	"gitlab.com/anthnell/devsecops/devdesk/internal/status"
)

// Messages

// StatusCheckMsg contains results from a background status check
type StatusCheckMsg struct {
	Result status.MonitorResult
}

// GitLabStatsMsg contains fetched GitLab statistics
type GitLabStatsMsg struct {
	Stats shared.GitLabStats
}

// DockerStatsMsg contains fetched Docker statistics
type DockerStatsMsg struct {
	Stats shared.DockerStats
}

// OCIStatsMsg contains fetched OCI resource statistics
type OCIStatsMsg struct {
	Stats shared.OCIStats
}

// WorkspaceStatsMsg contains workspace count and disk usage
type WorkspaceStatsMsg struct {
	Count    int
	DiskSize string
}

// ToolsDetectedMsg contains detected tool information
type ToolsDetectedMsg struct {
	Tools []shared.ToolInfo
}

// RefreshTickMsg triggers a periodic refresh
type RefreshTickMsg time.Time

// Model represents the dashboard view state
type Model struct {
	config *config.Config
	shared *shared.State
	width  int
	height int

	// Data
	serviceComponents []status.ComponentStatus
	serviceStatus     shared.ServiceGlobalStatus
	gitlabStats       *shared.GitLabStats
	dockerStats       *shared.DockerStats
	ociStats          *shared.OCIStats
	tools             []shared.ToolInfo
	workspaceCount    int
	workspaceDisk     string

	// Loading flags
	loadingServices   bool
	loadingGitLab     bool
	loadingDocker     bool
	loadingOCI        bool
	loadingWorkspaces bool
	loadingTools      bool

	// Refresh
	refreshInterval time.Duration
	lastRefresh     time.Time
}

// New creates a new dashboard model
func New(cfg *config.Config, state *shared.State) Model {
	return Model{
		config:            cfg,
		shared:            state,
		serviceStatus:     shared.ServiceStatusUnknown,
		loadingServices:   true,
		loadingGitLab:     true,
		loadingDocker:     true,
		loadingOCI:        true,
		loadingWorkspaces: true,
		loadingTools:      true,
		refreshInterval:   time.Duration(cfg.Status.RefreshInterval) * time.Second,
	}
}

// Init initializes the dashboard by loading all data
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.checkServices(),
		m.fetchGitLabStats(),
		m.fetchDockerStats(),
		m.fetchOCIStats(),
		m.fetchWorkspaceStats(),
		m.detectTools(),
		m.scheduleRefresh(),
	)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case StatusCheckMsg:
		return m.handleStatusCheck(msg)

	case GitLabStatsMsg:
		return m.handleGitLabStats(msg)

	case DockerStatsMsg:
		return m.handleDockerStats(msg)

	case OCIStatsMsg:
		return m.handleOCIStats(msg)

	case WorkspaceStatsMsg:
		return m.handleWorkspaceStats(msg)

	case ToolsDetectedMsg:
		return m.handleToolsDetected(msg)

	case RefreshTickMsg:
		return m, m.refreshAll()
	}

	return m, nil
}

// handleKeyMsg processes keyboard input
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+r":
		m.loadingServices = true
		m.loadingGitLab = true
		m.loadingDocker = true
		m.loadingOCI = true
		m.loadingWorkspaces = true
		return m, m.refreshAll()
	case "m":
		return m, openURL(fmt.Sprintf("%s/dashboard/merge_requests", m.config.GitLab.URL))
	case "i":
		if m.shared.CurrentUser != nil {
			return m, openURL(fmt.Sprintf("%s/dashboard/issues?sort=created_date&state=opened&assignee_username[]=%s", m.config.GitLab.URL, m.shared.CurrentUser.Username))
		}
	}
	return m, nil
}

// handleStatusCheck processes status check results
func (m Model) handleStatusCheck(msg StatusCheckMsg) (tea.Model, tea.Cmd) {
	m.loadingServices = false
	m.serviceComponents = msg.Result.Components
	m.lastRefresh = msg.Result.Timestamp

	m.serviceStatus = computeGlobalStatus(m.serviceComponents)

	m.shared.ServiceStatus = m.serviceStatus
	m.shared.ServiceComponents = m.serviceComponents

	return m, nil
}

// handleGitLabStats processes GitLab stats results
func (m Model) handleGitLabStats(msg GitLabStatsMsg) (tea.Model, tea.Cmd) {
	m.loadingGitLab = false
	m.gitlabStats = &msg.Stats
	m.shared.GitLabStats = &msg.Stats
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
	m.workspaceDisk = msg.DiskSize
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

func (m Model) fetchGitLabStats() tea.Cmd {
	client := m.shared.GitLabClient
	user := m.shared.CurrentUser

	if client == nil || user == nil {
		return func() tea.Msg {
			return GitLabStatsMsg{Stats: shared.GitLabStats{}}
		}
	}

	userID := user.ID
	return func() tea.Msg {
		stats := gitlabpkg.FetchDashboardStats(client, userID)
		return GitLabStatsMsg{Stats: shared.GitLabStats{
			AssignedMRs:    stats.AssignedMRs,
			ReviewMRs:      stats.ReviewMRs,
			AssignedIssues: stats.AssignedIssues,
			TotalProjects:  stats.TotalProjects,
			TotalGroups:    stats.TotalGroups,
		}}
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
		}}
	}
}

func (m Model) fetchWorkspaceStats() tea.Cmd {
	workspacesDir := m.config.App.WorkspacesDir
	return func() tea.Msg {
		dir := workspacesDir
		if len(dir) >= 2 && dir[:2] == "~/" {
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, dir[2:])
		}

		entries, err := os.ReadDir(dir)
		count := 0
		if err == nil {
			for _, e := range entries {
				if e.IsDir() {
					count++
				}
			}
		}

		diskSize := calculateDiskUsage(dir)

		return WorkspaceStatsMsg{Count: count, DiskSize: diskSize}
	}
}

func (m Model) detectTools() tea.Cmd {
	cfg := m.config
	return func() tea.Msg {
		var tools []shared.ToolInfo

		// Docker
		tools = append(tools, detectBinaryTool("Docker", "docker", "version", "--format", "{{.Client.Version}}"))

		// Security tools via scan.CheckDependencies
		deps := scan.CheckDependenciesWithImages(cfg.Scan.TrivyImage, cfg.Scan.GitleaksImage)
		tools = append(tools, shared.ToolInfo{
			Name:      "Trivy",
			Available: deps.TrivyAvailable,
			Version:   cleanVersion(deps.TrivyVersion),
			Source:    string(deps.TrivySource),
		})
		tools = append(tools, shared.ToolInfo{
			Name:      "Gitleaks",
			Available: deps.GitleaksAvailable,
			Version:   cleanVersion(deps.GitleaksVersion),
			Source:    string(deps.GitleaksSource),
		})

		// Network Diagnostics image
		tools = append(tools, detectDockerImage("Net Diag", cfg.Docker.NetworkToolImage))

		// Git
		tools = append(tools, detectBinaryTool("Git", "git", "--version"))

		return ToolsDetectedMsg{Tools: tools}
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

func (m Model) refreshAll() tea.Cmd {
	return tea.Batch(
		m.checkServices(),
		m.fetchGitLabStats(),
		m.fetchDockerStats(),
		m.fetchOCIStats(),
		m.fetchWorkspaceStats(),
		m.scheduleRefresh(),
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

// calculateDiskUsage returns human-readable disk usage for a directory
func calculateDiskUsage(dir string) string {
	cmd := exec.Command("du", "-sh", dir)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	parts := strings.Fields(string(out))
	if len(parts) >= 1 {
		return parts[0]
	}
	return ""
}
