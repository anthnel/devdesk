package shared

import (
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/forgeindex"
	"github.com/anthnel/devdesk/internal/forward"
	"github.com/anthnel/devdesk/internal/scan"
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

	// ForgeIndex is everything this session can see on the forge, at every
	// depth (internal/forgeindex). Nil until the first answer — the file the
	// previous walk left, or the walk itself — and set back to nil when the
	// session closes. Only the router writes it; the explorer reads it to show
	// a level before the forge answers, and its "g" prompt matches against it.
	//
	// D36 removed a groups/projects cache from here, and the reasons still
	// hold against *that* cache: nothing wrote it, and a flat slice fetched in
	// one blocking walk is what §3.16 took out because it froze the explorer.
	// This one is written at every session start, never blocks — it is a job,
	// and the previous walk's file stands in meanwhile — and answers per level
	// (Children), which is the shape a lazily-drilled tree needs. The explorer
	// still re-reads the level it shows, so what this holds is a head start,
	// never the last word.
	ForgeIndex *forgeindex.Index

	// Forwards are the open port redirections (§3.1). It lives here, created
	// once with the router and never replaced, because a listener is not a
	// view's to hold: reinitializeViews drops every view on a config save and
	// on a context switch, and the port would stay bound with nothing left
	// pointing at it.
	//
	// It deliberately survives a context switch, unlike the MCP server, which
	// restarts because it answers *for* a context. A forward is a local port
	// pointed at a host:port; it belongs to no context.
	Forwards *forward.Registry

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

	// Tools is the one detection of where each scanner runs from (§3.86),
	// made by the router and read by every view. Nil until it answers — and
	// not knowing is not knowing that not: a view keeps S offered meanwhile.
	//
	// It used to be written by the dashboard and read by nobody, while four
	// views each ran a detection of their own: a `--version` per tool per
	// view, and four answers free to disagree.
	Tools *scan.Report
}

// ScanToolsMsg hands a view the router's current detection. The router sends
// it to every view it holds whenever a detection lands, and to a view it
// builds, so a view never has to ask. Report is nil when there is none yet.
type ScanToolsMsg struct {
	Report *scan.Report
}

// ScanToolsDetectRequestMsg asks the router for a new detection: ctrl+r on a
// view that shows what is installed, since a tool installed while DevDesk runs
// is not seen otherwise.
type ScanToolsDetectRequestMsg struct{}

// ForgeIndexChangedMsg tells every view the router holds that ForgeIndex was
// replaced — a walk landed, the file was read, or an edit was applied. It
// carries nothing: the index is on the shared state, and a second copy here
// would be a second answer to the same question.
type ForgeIndexChangedMsg struct{}

// ForgeIndexEditMsg asks the router to change the index — a view that just
// created, deleted or re-read something on the forge. The edit is a function
// of the current index rather than a new index, because the view's copy may be
// older than the router's by the time the message arrives: a walk can land in
// between, and applying the edit to it keeps both.
//
// The router drops it when there is no index: an edit to nothing is nothing,
// and the next walk will see what the view saw.
type ForgeIndexEditMsg struct {
	Edit func(*forgeindex.Index) *forgeindex.Index
}

// ForgeIndexRefreshMsg asks the router to walk the forge again — ctrl+r in the
// explorer. The index in place stays until the new one lands.
type ForgeIndexRefreshMsg struct{}
