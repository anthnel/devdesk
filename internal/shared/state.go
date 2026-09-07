package shared

import (
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/status"
)

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
	// NetworksCount comes from `docker network ls`, not from `system df`: a
	// network holds no bytes, so the disk report has no row for it.
	NetworksCount int
	// BuildCacheSize is the fourth row of `docker system df`, and the one that
	// most often answers "where did the disk go".
	BuildCacheSize string
	// Reclaimable is what a prune would give back, summed over the three
	// families. It comes from the same call as the rest and used to be
	// discarded.
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

// State holds the state shared between all views
type State struct {
	// Secrets is where this context's secrets live, resolved once by the
	// router. Views read from it; only the auth view writes. SecretNotices is
	// what the migration off plaintext configuration had to say, if anything.
	Secrets       credentials.Selection
	SecretNotices []string

	// Forge is the code-hosting backend this context targets — exactly one,
	// never two (§3.6). Nil until a session opens.
	//
	// It used to be a *gitlabclient.Client, which bound every consumer to
	// go-gitlab rather than to a DevDesk abstraction, and doubled as the
	// authentication flag: three sites branched on `GitLabClient != nil` while
	// IsAuthenticated sat beside them saying the same thing. IsAuthenticated is
	// the flag; Forge is what you call.
	Forge           forge.Forge
	IsAuthenticated bool
	// CurrentUser is a value, not a pointer: IsAuthenticated already answers
	// "is there a session", and a second way to ask it is how the two came to
	// disagree.
	CurrentUser forge.User

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
	// ForgeStats are the dashboard's forge counters, nil until they are
	// fetched. It was a struct of its own here, field for field identical to
	// what internal/gitlab returned — a third copy of the same five numbers.
	ForgeStats     *forge.DashboardStats
	DockerStats    *DockerStats
	OCIStats       *OCIStats
	WorkspaceCount int
	Tools          []ToolInfo
}
