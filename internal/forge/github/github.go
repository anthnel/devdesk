// Package github is the GitHub backend of internal/forge.
//
// It is the only place in DevDesk that knows go-github exists, as its GitLab
// sibling is the only place that knows go-gitlab.
//
// # What it does not do, and says so
//
// GitHub has no API for creating or deleting an **organisation**, and no grace
// period a delete could bypass. The interface expects a backend to refuse what
// its Shape says it cannot express rather than to do something adjacent and
// report success — so both come back as errors naming the reason, and the
// permanent-delete checkbox is out of reach because `Shape.PermanentDelete` is
// false.
//
// # Identity is the path
//
// forge.Namespace.ID is opaque, which lets each backend pick what addresses an
// object. GitLab needs a number in two places; GitHub needs `owner` and
// `owner/repo` everywhere, so here ID and Path are the same string. Nothing
// outside this package may notice.
package github

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"

	gh "github.com/google/go-github/v68/github"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/forge"
)

// Forge is a GitHub instance — github.com or an Enterprise Server — bound to
// one token.
type Forge struct {
	client *gh.Client
	// baseURL is the *web* host as the user configured it. The SDK holds its
	// own API base, which on Enterprise is a different path; this one builds
	// clone URLs and browser links.
	baseURL string

	mu   sync.Mutex
	user *forge.User
}

var _ forge.Forge = (*Forge)(nil)

// New binds a GitHub instance to a token.
//
// A host other than github.com is taken as GitHub Enterprise Server, whose API
// lives under /api/v3/ — WithEnterpriseURLs appends that, so the configured URL
// stays the web host the user typed and the one clone URLs are built from.
func New(baseURL, token string) (*Forge, error) {
	client := gh.NewClient(nil).WithAuthToken(token)

	trimmed := strings.TrimSuffix(baseURL, "/")
	if !isDotCom(trimmed) && trimmed != "" {
		enterprise, err := client.WithEnterpriseURLs(trimmed, trimmed)
		if err != nil {
			return nil, fmt.Errorf("github enterprise URL: %w", err)
		}
		client = enterprise
	}

	return &Forge{client: client, baseURL: trimmed}, nil
}

// NewWithClient wraps a client that already exists, or none at all. A nil
// client is legitimate: the URL helpers touch nothing.
func NewWithClient(client *gh.Client, baseURL string) *Forge {
	return &Forge{client: client, baseURL: strings.TrimSuffix(baseURL, "/")}
}

// isDotCom reports whether a URL names the public instance.
func isDotCom(rawURL string) bool {
	host := strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
	host, _, _ = strings.Cut(host, "/")
	return host == "" || host == "github.com" || host == "www.github.com"
}

// Shape delegates, so the platform's capabilities are declared once (§3.6
// step 6). GitHub's are the interesting half of the abstraction: no nesting, no
// `internal`, no permanent delete.
func (f *Forge) Shape() forge.Shape {
	return forge.ShapeFor(config.ForgeGitHub)
}

// CurrentUser is who the token belongs to, and the connection test with it.
//
// Memoised for its GitLab sibling's reason: every decorated listing needs the
// caller's login for its role lookups, and asking the host each time would add
// a request per listing.
func (f *Forge) CurrentUser(ctx context.Context) (forge.User, error) {
	f.mu.Lock()
	if f.user != nil {
		defer f.mu.Unlock()
		return *f.user, nil
	}
	f.mu.Unlock()

	user, _, err := f.client.Users.Get(ctx, "")
	if err != nil {
		return forge.User{}, err
	}

	// The login, not the numeric id: it is what every other call addresses a
	// user by, and forge.User.ID is opaque precisely so a backend can choose.
	resolved := forge.User{
		ID:       user.GetLogin(),
		Username: user.GetLogin(),
		Name:     user.GetName(),
	}

	f.mu.Lock()
	f.user = &resolved
	f.mu.Unlock()
	return resolved, nil
}

// CloneURL builds the git URL from the web host, the path and the method.
func (f *Forge) CloneURL(repo forge.Repository, method forge.CloneMethod) string {
	base := f.webBase()
	if method == forge.CloneSSH {
		host := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
		return fmt.Sprintf("git@%s:%s.git", host, repo.Path)
	}
	return fmt.Sprintf("%s/%s.git", base, repo.Path)
}

// ChangeRequestsURL is GitHub's pull request dashboard.
func (f *Forge) ChangeRequestsURL() string { return f.webBase() + "/pulls" }

// AssignedIssuesURL is the issue dashboard filtered to one assignee.
//
// GitHub has no per-assignee path, so the filter goes in the `q` search
// parameter — which is exactly why it is escaped rather than concatenated.
func (f *Forge) AssignedIssuesURL(user forge.User) string {
	q := url.Values{"q": {"is:open is:issue assignee:" + user.Username}}
	return f.webBase() + "/issues?" + q.Encode()
}

// webBase is the host browser links and clone URLs are built from. An empty
// configured URL means github.com, which is the only host that has a default.
func (f *Forge) webBase() string {
	if f.baseURL == "" {
		return "https://github.com"
	}
	return f.baseURL
}
