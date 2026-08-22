package forge

import "time"

// User is who a token belongs to.
//
// Three fields, because three are read: the username names the session in the
// header and the auth view, the name qualifies it in parentheses, and the ID is
// what the dashboard counters and the role lookups are keyed on. An avatar and
// an email are available from both forges and neither is displayed.
type User struct {
	// ID addresses the user with the backend. Opaque — see the package doc.
	ID       string
	Username string
	Name     string
}

// Namespace is a container of repositories and of other namespaces: a GitLab
// group or subgroup, a GitHub organisation.
type Namespace struct {
	// ID addresses it with the backend; Path is `group/subgroup`, the form the
	// user reads and the clone URL is built from. See the package doc.
	ID   string
	Path string
	Name string

	Visibility string
	// Role is already humanised — "Owner", "Maintainer", "Member". A backend
	// returns a word, never a number: GitLab's levels (50, 40, 30…) and
	// GitHub's (`admin`, `maintain`, `push`, `triage`, `pull`) do not align one
	// to one, so a caller translating an integer would be translating GitLab's.
	// Empty means the role was not asked for, or is not known.
	Role string

	CreatedAt *time.Time
	// WebURL opens it in a browser. It comes from the forge rather than being
	// assembled from the host and the path — a self-hosted instance under a
	// path prefix would be assembled wrong.
	WebURL string
}

// Repository is one repository: a GitLab project, a GitHub repository.
type Repository struct {
	ID   string
	Path string
	Name string

	Visibility string
	// Role, humanised. See Namespace.Role.
	Role string

	CreatedAt      *time.Time
	LastActivityAt *time.Time
	WebURL         string

	// CIStatus is the last pipeline or workflow run — "success", "failed",
	// "running"… Empty when it was not asked for, when there is none, or when
	// the backend has no equivalent.
	CIStatus string

	// DeletionScheduled says the forge has already marked it for deletion and
	// only a permanent removal remains. It is always false on a backend whose
	// Shape says PermanentDelete is false, because there is no such state
	// there — a delete is immediate.
	DeletionScheduled bool
}

// Children is what one namespace directly contains.
//
// Two slices rather than one list of a sum type: the caller does different
// things with them — the explorer decorates repositories and not namespaces,
// the clone recurses into namespaces and clones repositories — and a single
// list would have every caller switch on a kind to get back here.
type Children struct {
	Namespaces   []Namespace
	Repositories []Repository
}

// BrowseOptions is what a listing may vary. Both fields cost something, which
// is why neither is implied.
//
// It covers RootNamespaces as well as Children, because the difference the
// clone needs is the same at both levels: its walk starts from a root and pays
// for no decoration anywhere. IncludeArchived is meaningless for namespaces —
// neither forge archives one — and is ignored there rather than split into a
// second type for one field.
type BrowseOptions struct {
	// IncludeArchived lists archived repositories. Browsing passes true — the
	// explorer shows what is there — and a clone passes
	// `gitlab.pull.include_archived`, which is what that setting means and the
	// only place it is read.
	IncludeArchived bool

	// Decorated fills Role and CIStatus on every repository returned.
	//
	// It is optional because it is expensive and because one caller genuinely
	// does not want it: on GitLab it is two extra requests *per repository*, so
	// a two-hundred-repository group costs 400 calls for a CI badge and a role
	// that a clone never looks at. The explorer asks for it, the clone's walk
	// does not.
	Decorated bool
}

// NewNamespace is a namespace to create.
type NewNamespace struct {
	Name        string
	Slug        string
	Description string
	Visibility  string
	// ParentID is the namespace it goes under, empty for a root one. A backend
	// whose Shape cannot nest that deep refuses rather than creating it at the
	// root — a namespace silently created somewhere else is worse than an
	// error, because the user goes looking for it where they asked for it.
	ParentID string
}

// NewRepository is a repository to create.
type NewRepository struct {
	Name        string
	Slug        string
	Description string
	Visibility  string
	// NamespaceID is where it goes; empty means the user's own namespace.
	NamespaceID string
}

// FileChange is one file in an initial commit.
type FileChange struct {
	Action  FileAction
	Path    string
	Content string
}

// FileAction is what an initial commit does to a file.
type FileAction string

const (
	FileCreate FileAction = "create"
	FileUpdate FileAction = "update"
	FileDelete FileAction = "delete"
)

// CloneMethod is how a repository is cloned.
type CloneMethod string

const (
	CloneHTTPS CloneMethod = "https"
	CloneSSH   CloneMethod = "ssh"
)

// DashboardStats are the dashboard's counters.
//
// Every count is a pointer, and nil means **nobody looked** — the counters come
// from five independent requests and any of them can fail on its own. A plain
// int cannot say that: today each failure leaves a zero, and the dashboard
// renders a dim `0` that is indistinguishable from "no merge requests assigned
// to you". That is D20 in five fields, and `Sensitive *bool` in the scan caches
// is the precedent for the fix — a value that says "not known" rather than a
// zero that reads as an answer.
type DashboardStats struct {
	AssignedChangeRequests *int
	ReviewChangeRequests   *int
	AssignedIssues         *int
	Repositories           *int
	Namespaces             *int
}

// Count is a small helper for a backend filling DashboardStats: it returns a
// pointer to n, so a successful request reads `stats.AssignedIssues = Count(n)`
// and a failed one leaves the field nil by doing nothing.
func Count(n int) *int { return &n }
