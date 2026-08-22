// Package forge is what DevDesk asks of a code-hosting platform.
//
// It exists so a configuration context can target GitLab or GitHub — exactly
// one of the two, never both (§3.6). Everything above it speaks this
// vocabulary; only the backends know which SDK is underneath.
//
// # It is derived from what the code consumes, not from what an API offers
//
// Every method here answers a call site that exists today: the auth view wants
// the current user, the explorer browses and creates and deletes, the clone
// pipeline walks and builds URLs, the dashboard counts. Nothing was added
// because a forge happens to expose it. A method with no caller is a promise
// nobody checks, and two backends would honour it two different ways before
// anyone noticed.
//
// # A backend declares its shape
//
// The two forges differ in *words* and in *shapes*, and only the first is a
// presentation problem — Vocabulary handles those, and it is not in this
// package. A shape is what the abstraction can promise at all: how deep
// namespaces nest, which visibilities exist, whether a delete can be made
// permanent. Those are Shape, returned by the backend and never guessed from a
// URL — the registry `provider` field is the precedent (§3.8).
//
// # Identity is opaque; the path is not
//
// A namespace or a repository carries two strings. ID addresses it with the
// backend and means nothing outside: GitLab needs a number to create under a
// parent (CreateGroupOptions.ParentID is an *int64), GitHub addresses by owner.
// Path is the human one — `group/subgroup`, `owner/repo` — and it is what the
// clone URL, the display and GitLab's renamed-path deletion are built from.
//
// A caller that parses an ID is a caller bound to one forge. The type is a
// string so that arithmetic on it cannot be written.
//
// # A namespace and a repository are different types
//
// Not one node with a kind field. Only a namespace has children; only a
// repository has a pipeline status and a deletion schedule. Distinct types make
// "drill into a repository" unexpressible rather than forbidden by review —
// the same trade datatable's `Cell func(T) string` makes for Rule 122.
//
// The explorer still renders them in one table, and still holds its own
// TreeNode for what a *tree* needs — expanded, loading, parent, depth. Those
// are view state, and a forge has no opinion on them.
//
// # The backend paginates, and the caller never sees a page
//
// D34 was four copies of `PerPage: 100, Page: 1`, so a group with more than a
// hundred children was silently cut. A list returned here is complete or it is
// an error; there is no page to forget.
//
// # Every call takes a context
//
// The clone's discovery is cancellable and its clones are not (§3.16), which is
// a distinction the caller can only make if the reads take a context. Today
// every call passes context.Background(); that is what this fixes.
package forge

import "context"

// Forge is one code-hosting backend, bound to one host and one token.
//
// A consumer should declare the subset it needs rather than depend on this
// whole interface — Go resolves that at the consumer, so the backend does not
// have to be split. The dashboard wants DashboardStats and nothing else.
type Forge interface {
	// Shape reports what this backend can express. It is a value, not a
	// request: nothing about it depends on the network or on the token.
	Shape() Shape

	// CurrentUser is who the token belongs to. It is also the connection test:
	// authentication succeeded exactly when this returns.
	CurrentUser(ctx context.Context) (User, error)

	// RootNamespaces lists the top-level namespaces the user can see — GitLab's
	// top-level groups, GitHub's organisations. Complete, every page.
	RootNamespaces(ctx context.Context) ([]Namespace, error)

	// Children lists one namespace's direct children, complete.
	//
	// Direct, not recursive: the explorer drills one level at a time and the
	// clone walks levels itself, so a recursive listing would fetch what
	// neither asked for.
	Children(ctx context.Context, namespaceID string, opts ChildrenOptions) (Children, error)

	// CreateNamespace creates a namespace, optionally under a parent. A backend
	// whose Shape refuses the requested depth returns an error rather than
	// creating it at the root.
	CreateNamespace(ctx context.Context, spec NewNamespace) (Namespace, error)

	// CreateRepository creates a repository inside a namespace.
	CreateRepository(ctx context.Context, spec NewRepository) (Repository, error)

	// InitialCommit writes the first commit of a repository — what a template
	// is applied by. It is separate from CreateRepository because a repository
	// is created whether or not a template was chosen.
	InitialCommit(ctx context.Context, repositoryID string, files []FileChange) error

	// DeleteNamespace deletes a namespace and everything under it.
	//
	// It takes the whole value rather than an ID because permanent deletion
	// needs both: GitLab schedules the deletion, which *renames* the namespace
	// to `<path>-deletion_scheduled-<id>`, and the second call addresses that
	// new path. A backend whose Shape says PermanentDelete is false returns an
	// error when asked for one, rather than deleting normally and reporting
	// success for something else.
	DeleteNamespace(ctx context.Context, ns Namespace, permanently bool) error

	// DeleteRepository deletes a repository. Same two-argument reasoning as
	// DeleteNamespace.
	DeleteRepository(ctx context.Context, repo Repository, permanently bool) error

	// DashboardStats fetches the dashboard's counters for one user.
	DashboardStats(ctx context.Context, user User) (DashboardStats, error)

	// CloneURL is the git URL for a repository. No network, no token: it is
	// built from the host, the path and the method, and the token travels
	// through git's environment rather than through the URL (§3.16).
	CloneURL(repo Repository, method CloneMethod) string

	// ChangeRequestsURL is the web page listing the user's merge or pull
	// requests, and AssignedIssuesURL the one listing their open issues.
	//
	// They are on the backend because the paths differ per forge and the views
	// used to build them with fmt.Sprintf against the configured URL — which is
	// a forge shape written in a view.
	ChangeRequestsURL() string
	AssignedIssuesURL(user User) string
}
