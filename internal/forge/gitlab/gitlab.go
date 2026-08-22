// Package gitlab is the GitLab backend of internal/forge.
//
// It is the only place in DevDesk that knows go-gitlab exists. Everything it
// exports is either forge's vocabulary or the constructor that binds a host and
// a token to it.
//
// # What it hides
//
// Pagination (D34), the numeric identifiers GitLab wants where a path will not
// do, the two-step permanent delete and its renamed path, and the mapping from
// GitLab's numeric access levels to a word. Each of those leaked into a view
// before this package existed, and each is the kind of thing a second forge
// would have contradicted quietly.
package gitlab

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/forge"
)

// Forge is a GitLab instance, bound to one host and one token.
type Forge struct {
	client *gitlabclient.Client
	// baseURL is the host as the user configured it, trailing slash removed.
	// The SDK holds its own API base; this one builds clone URLs and web links,
	// which are not API paths.
	baseURL string
}

var _ forge.Forge = (*Forge)(nil)

// New binds a GitLab instance to a token.
func New(baseURL, token string) (*Forge, error) {
	client, err := gitlabclient.NewClient(token, gitlabclient.WithBaseURL(baseURL))
	if err != nil {
		return nil, err
	}
	return NewWithClient(client, baseURL), nil
}

// NewWithClient wraps a client that already exists, or none at all.
//
// A nil client is legitimate: CloneURL, ChangeRequestsURL and AssignedIssuesURL
// are built from the host and touch nothing, so they answer without a session.
func NewWithClient(client *gitlabclient.Client, baseURL string) *Forge {
	return &Forge{client: client, baseURL: strings.TrimSuffix(baseURL, "/")}
}

// Shape is GitLab's, and it is a constant: nothing here depends on the instance
// or on the token.
//
// MaxNamespaceDepth is 0 — unbounded. GitLab documents a limit of 20 on
// self-managed instances, but it is configurable and not reported by the API,
// so declaring 20 would be asserting something this backend cannot check. An
// over-deep create is refused by the server with its own message, which is
// better than a guess refusing a legitimate one.
func (f *Forge) Shape() forge.Shape {
	return forge.Shape{
		Name:              "gitlab",
		MaxNamespaceDepth: 0,
		Visibilities:      []string{"private", "internal", "public"},
		PermanentDelete:   true,
	}
}

// CurrentUser is who the token belongs to, and the connection test with it.
func (f *Forge) CurrentUser(ctx context.Context) (forge.User, error) {
	user, _, err := f.client.Users.CurrentUser(gitlabclient.WithContext(ctx))
	if err != nil {
		return forge.User{}, err
	}
	return forge.User{
		ID:       fmt.Sprint(user.ID),
		Username: user.Username,
		Name:     user.Name,
	}, nil
}

// CloneURL builds the git URL. No network and no token: the token travels
// through git's environment, never in the URL (§3.16).
func (f *Forge) CloneURL(repo forge.Repository, method forge.CloneMethod) string {
	if method == forge.CloneSSH {
		host := strings.TrimPrefix(strings.TrimPrefix(f.baseURL, "https://"), "http://")
		return fmt.Sprintf("git@%s:%s.git", host, repo.Path)
	}
	return fmt.Sprintf("%s/%s.git", f.baseURL, repo.Path)
}

// ChangeRequestsURL is GitLab's merge request dashboard.
func (f *Forge) ChangeRequestsURL() string {
	return f.baseURL + "/dashboard/merge_requests"
}

// AssignedIssuesURL is the issue dashboard filtered to one assignee.
//
// The username is escaped rather than interpolated raw: it reaches this from
// the API, but a URL built by concatenation is how a query parameter ends up
// meaning something else, and nothing about the call site says the value is
// safe.
func (f *Forge) AssignedIssuesURL(user forge.User) string {
	q := url.Values{
		"sort":                {"created_date"},
		"state":               {"opened"},
		"assignee_username[]": {user.Username},
	}
	return f.baseURL + "/dashboard/issues?" + q.Encode()
}

// roleName turns a GitLab access level into the word forge.Namespace.Role
// promises. It is the mapping TreeNode.AccessLevelName held, moved to the only
// place that knows what a 40 means.
//
// Zero is "not known" and returns the empty string — not "Guest". A role the
// backend could not read is not the lowest role there is.
func roleName(level int) string {
	switch {
	case level >= 50:
		return "Owner"
	case level >= 40:
		return "Maintainer"
	case level >= 30:
		return "Developer"
	case level >= 20:
		return "Reporter"
	case level >= 10:
		return "Guest"
	default:
		return ""
	}
}

// namespaceID renders a GitLab numeric ID as the opaque string forge uses.
func namespaceID(id int64) string { return fmt.Sprint(id) }

// numericID reads back what namespaceID wrote.
//
// Only this package may do it — that is the package doc's rule, and the reason
// forge.Namespace.ID is a string is so that no caller outside can. GitLab needs
// the number in two places where a path is not accepted: CreateGroupOptions
// .ParentID and CreateProjectOptions.NamespaceID are both *int64.
func numericID(id string) (int64, error) {
	var n int64
	if _, err := fmt.Sscanf(id, "%d", &n); err != nil {
		return 0, fmt.Errorf("not a GitLab id: %q", id)
	}
	return n, nil
}

// deletionPath is what GitLab renames a group or project to when a deletion is
// scheduled. The permanent removal addresses that name, not the original.
func deletionPath(path string, id int64) string {
	return fmt.Sprintf("%s-deletion_scheduled-%d", path, id)
}

// propagationDelay is the pause between scheduling a deletion and asking for
// the permanent removal.
//
// It was in the code being moved, undocumented. It is kept because removing it
// would be an unrelated behaviour change inside a refactor that promises none —
// not because 500 ms is known to be the right number.
const propagationDelay = 500 * time.Millisecond
