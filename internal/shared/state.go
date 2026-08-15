package shared

import (
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/status"
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
	// BuildCacheSize is the fourth row of `docker system df`, and the one that
	// most often answers "where did the disk go".
	BuildCacheSize string
	// Reclaimable is what a prune would give back, summed over the three
	// families. Il vient du même appel que le reste et était jeté.
	Reclaimable string
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
	// Secrets is where this context's secrets live, resolved once by the
	// router. Views read from it; only the auth view writes. SecretNotices is
	// what the migration off plaintext configuration had to say, if anything.
	Secrets       credentials.Selection
	SecretNotices []string

	// GitLab
	GitLabClient    *gitlabclient.Client
	IsAuthenticated bool
	CurrentUser     *gitlabclient.User

	// There is no groups/projects cache here, and that is a decision rather
	// than an omission (D36). `CachedGroups` and `CachedProjects` were declared
	// and cleared in three places for months without a single production write,
	// so every explorer open was said to be refetching against a cache that had
	// never held anything.
	//
	// Two things ruled out filling them. The explorer keeps its own tree for as
	// long as it exists, and `createView` only rebuilds a view it has dropped —
	// on a config save, a context switch or a logout, which are exactly the
	// three moments this cache was being emptied. It could therefore only ever
	// be consulted when it was deliberately empty.
	//
	// And the shape is wrong anyway. §3.16 made the explorer a lazily-walked,
	// paginated tree; a flat slice of every group cannot say which level was
	// fetched, and filling one needs the full API walk that §3.16 removed
	// precisely because it froze the view for minutes. The right cache for a
	// tree is the tree, and the explorer already holds it.

	// Dashboard data
	ServiceStatus     ServiceGlobalStatus
	ServiceComponents []status.ComponentStatus
	GitLabStats       *GitLabStats
	DockerStats       *DockerStats
	OCIStats          *OCIStats
	WorkspaceCount    int
	Tools             []ToolInfo
}
