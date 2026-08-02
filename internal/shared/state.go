package shared

import (
	"github.com/anthnel/devdesk/internal/status"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"
)

// GitLabStats contains aggregated GitLab statistics for the dashboard
type GitLabStats struct {
	AssignedMRs    int
	ReviewMRs      int
	AssignedIssues int
	TotalProjects  int
	TotalGroups    int
}

// DockerStats contains Docker container statistics for the dashboard
type DockerStats struct {
	Available bool
	Running   int
	Stopped   int
	Paused    int
}

// OCIStats contains OCI resource counts and disk usage from docker system df
type OCIStats struct {
	Available       bool
	ImagesCount     int
	ImagesSize      string
	ContainersCount int
	ContainersSize  string
	VolumesCount    int
	VolumesSize     string
}

// ToolInfo describes an available DevSecOps tool
type ToolInfo struct {
	Name      string
	Available bool
	Version   string
	Source    string // "binary", "docker", or ""
}

// ServiceGlobalStatus represents the overall health of monitored services
type ServiceGlobalStatus string

const (
	ServiceStatusAllOK    ServiceGlobalStatus = "all_ok"
	ServiceStatusDegraded ServiceGlobalStatus = "degraded"
	ServiceStatusDown     ServiceGlobalStatus = "down"
	ServiceStatusUnknown  ServiceGlobalStatus = "unknown"
)

// State contient l'état partagé entre toutes les vues
type State struct {
	// GitLab
	GitLabClient    *gitlabclient.Client
	IsAuthenticated bool
	CurrentUser     *gitlabclient.User

	// Cache
	CachedGroups   []*gitlabclient.Group
	CachedProjects []*gitlabclient.Project

	// Dashboard data
	ServiceStatus     ServiceGlobalStatus
	ServiceComponents []status.ComponentStatus
	GitLabStats       *GitLabStats
	DockerStats       *DockerStats
	OCIStats          *OCIStats
	WorkspaceCount    int
	Tools             []ToolInfo
}
