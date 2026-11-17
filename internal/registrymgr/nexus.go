package registrymgr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/oci"
)

func init() {
	Register(&NexusDetector{})
}

// NexusDetector handles Sonatype Nexus Repository Manager group repositories.
// Two URL formats are supported:
//  1. Path-based:   https://nexus.example.com/repository/docker-group
//  2. Subdomain:    nexus-docker-group.example.com  (requires info.ManagementURL)
//
// Which one applies is read from the URL. Whether this detector runs at all is
// not: that is the declared provider's decision (§3.8, decision F).
type NexusDetector struct{}

// maxNexusResponse bounds what one Nexus reply may take in memory. The replies
// are small JSON documents (a repository and the names of its members); the
// figure follows the reasoning of the catalog page cap and is not measured.
const maxNexusResponse = 1 << 20

// errEmptyReply and errNoRepositoryType mark a 200 reply that is not a
// repository document. Both fail the discovery: reading either as "not a group"
// would be cached (D23) and erase the members last known.
var (
	errEmptyReply       = errors.New("empty reply")
	errNoRepositoryType = errors.New("reply names no repository type")
)

// nexusHTTPClient refuses a redirect to another origin, so the account of the
// registry is never sent to a host it was not given for.
var nexusHTTPClient = &http.Client{Timeout: 8 * time.Second, CheckRedirect: oci.RefuseOffOriginRedirect}

// Provider returns the provider this detector serves.
func (n *NexusDetector) Provider() string { return ProviderNexus }

// DetectGroup calls the Nexus REST API to determine whether the registry points
// to a group repository and, if so, returns its member repositories.
//
// Returns nil, nil when the manager answered and the repository is not a group,
// and an error when it could not be asked. Those two are not the same thing:
// the caller caches the first and must not cache the second, or one unreachable
// minute erases what was last known (D23).
func (n *NexusDetector) DetectGroup(ctx context.Context, info RegistryInfo) ([]GroupMember, error) {
	// Resolve which URL to use for the Nexus API call.
	apiBase := info.ManagementURL
	if apiBase == "" {
		apiBase = info.URL
	}

	host, repoName, err := parseNexusURL(apiBase)
	if err != nil {
		// Not an error: a URL that is not a Nexus repository path is a settled
		// answer, not a failure to reach one.
		// The cause only: a URL that does not parse may hold a password, and
		// url.Error repeats it whole.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		log.Printf("INFO [registrymgr/nexus] cannot parse Nexus URL: %v", err)
		return nil, nil
	}

	hasAuth := info.Username != "" && info.Password != ""
	log.Printf("INFO [registrymgr/nexus] detecting group for %q (auth=%v)", repoName, hasAuth)

	// Step 1: generic endpoint — confirms this repo is a group, gets its format,
	// and may already include memberNames in the attributes field (no admin privilege needed).
	repoType, repoFormat, members, err := n.fetchRepoMeta(ctx, host, repoName, info)
	if err != nil {
		return nil, err
	}
	if repoType != "group" {
		return nil, nil
	}

	if len(members) > 0 {
		log.Printf("INFO [registrymgr/nexus] group %q (%s) has %d members (from generic endpoint)", repoName, repoFormat, len(members))
		return members, nil
	}

	// Step 2: format-specific endpoint — requires nx-repository-admin privilege
	// but returns memberNames reliably on Nexus instances that support it.
	members = n.fetchGroupMembers(ctx, host, repoName, repoFormat, info)
	log.Printf("INFO [registrymgr/nexus] group %q (%s) has %d members (from format endpoint)", repoName, repoFormat, len(members))
	return members, nil
}

// fetchRepoMeta calls the generic /v1/repositories/{name} endpoint.
// Returns type, format and any memberNames found in attributes, or an error when
// the manager could not be asked.
// Some Nexus deployments include memberNames in attributes.group without requiring admin privilege.
func (n *NexusDetector) fetchRepoMeta(ctx context.Context, host, repoName string, info RegistryInfo) (repoType, format string, members []GroupMember, err error) {
	endpoint := fmt.Sprintf("%s/service/rest/v1/repositories/%s", host, repoName)
	body, err := n.nexusGET(ctx, endpoint, info)
	if err != nil {
		log.Printf("INFO [registrymgr/nexus] meta unreachable %q: %v", endpoint, err)
		return "", "", nil, fmt.Errorf("reading %s: %w", endpoint, err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		log.Printf("INFO [registrymgr/nexus] meta %q: empty reply", repoName)
		return "", "", nil, fmt.Errorf("parsing the reply from %s: %w", endpoint, errEmptyReply)
	}
	var result struct {
		Type   string `json:"type"`
		Format string `json:"format"`
		Group  struct {
			MemberNames []string `json:"memberNames"`
		} `json:"group"`
		Attributes struct {
			Group struct {
				MemberNames []string `json:"memberNames"`
			} `json:"group"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("INFO [registrymgr/nexus] meta parse error for %q: %v", repoName, err)
		return "", "", nil, fmt.Errorf("parsing the reply from %s: %w", endpoint, err)
	}
	if result.Type == "" {
		// Valid JSON that is not a repository document: `{}`, `null`.
		log.Printf("INFO [registrymgr/nexus] meta %q: reply names no repository type", repoName)
		return "", "", nil, fmt.Errorf("parsing the reply from %s: %w", endpoint, errNoRepositoryType)
	}
	log.Printf("INFO [registrymgr/nexus] meta %q: type=%s format=%s", repoName, result.Type, result.Format)

	// Collect memberNames from whichever field Nexus populated.
	rawNames := result.Group.MemberNames
	if len(rawNames) == 0 {
		rawNames = result.Attributes.Group.MemberNames
	}
	members = memberList(info.URL, rawNames)
	return result.Type, result.Format, members, nil
}

// fetchGroupMembers calls the format-specific group endpoint which reliably
// includes memberNames: GET /v1/repositories/{format}/group/{name}
// Requires nx-repository-admin privilege; falls back to anonymous when credentials fail.
func (n *NexusDetector) fetchGroupMembers(ctx context.Context, host, repoName, format string, info RegistryInfo) []GroupMember {
	if format == "" {
		format = "docker"
	}
	endpoint := fmt.Sprintf("%s/service/rest/v1/repositories/%s/group/%s", host, format, repoName)
	body, err := n.nexusGET(ctx, endpoint, info)
	if err != nil {
		// When auth is present but the request fails, retry anonymously.
		// Some Nexus instances allow anonymous read of the group admin endpoint.
		// A refused redirect to another origin is not such a failure: the
		// detection stops there (DEVDESK-412), no silent anonymous retry.
		if info.Username != "" && info.Password != "" && !errors.Is(err, oci.ErrOffOriginRedirect) {
			anonInfo := RegistryInfo{URL: info.URL, ManagementURL: info.ManagementURL}
			body, err = n.nexusGET(ctx, endpoint, anonInfo)
		}
	}
	if errors.Is(err, oci.ErrOffOriginRedirect) {
		// The advice below is for an account Nexus refused, not for this.
		log.Printf("INFO [registrymgr/nexus] members unreachable %q: %v — redirect to another origin refused (the registry URL may need to be the one Nexus serves)", endpoint, err)
		return nil
	}
	if err != nil {
		log.Printf("INFO [registrymgr/nexus] members unreachable %q: %v — account may lack nx-repository-admin privilege; run 'docker login %s' with an admin account", endpoint, err, host)
		return nil
	}
	var result struct {
		Group struct {
			MemberNames []string `json:"memberNames"`
		} `json:"group"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil
	}
	return memberList(info.URL, result.Group.MemberNames)
}

// memberList turns the names a group declares into addresses.
func memberList(dockerURL string, names []string) []GroupMember {
	members := make([]GroupMember, 0, len(names))
	for _, name := range names {
		url, prefix := memberAddress(dockerURL, name)
		members = append(members, GroupMember{
			Alias:      cleanMemberAlias(name),
			URL:        url,
			RepoPrefix: prefix,
		})
	}
	return members
}

// memberAddress decides how one member of a group is reached, from the URL the
// group itself is pulled from.
//
// A group written as a path prefix — `host/repository/<group>` — is an instance
// serving its Docker repositories that way, so its members are reached the same
// way: the host, with the member's name in front of the repository. That pair is
// consistent by construction, because browse and pull build the same path from
// it.
//
// Anything else is a connector of the group's own — a dedicated port, a
// subdomain — and says nothing about a member. Which of those a repository
// answers on is a setting on that repository (`docker.httpPort`,
// `docker.httpsPort`, `docker.subdomain`), and the endpoint carrying it is
// refused to an ordinary pull account. So nothing is claimed: the member is
// offered at the group's own address with no prefix, which browses to nothing
// visibly rather than pulling to nothing plausibly (§3.18).
func memberAddress(dockerURL, memberName string) (url, prefix string) {
	host, _, err := parseNexusURL(dockerURL)
	if err != nil {
		return dockerURL, ""
	}
	// parseNexusURL supplies https:// for a URL written without a scheme. The
	// group's own spelling is what the rest of the application is keyed on, so
	// it is kept rather than normalised here.
	if !strings.Contains(strings.ToLower(dockerURL), "://") {
		host = strings.TrimPrefix(host, "https://")
	}
	return host, memberName
}

// nexusGET performs an authenticated GET and returns the response body. A status
// other than 200 is a *oci.StatusError from the management API: the code only, no
// status line, body or header. A context error stays a context error.
func (n *NexusDetector) nexusGET(ctx context.Context, endpoint string, info RegistryInfo) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	if info.Username != "" && info.Password != "" {
		req.SetBasicAuth(info.Username, info.Password)
	}
	resp, err := nexusHTTPClient.Do(req)
	if err != nil {
		// A refused redirect comes back without the URL it pointed to, which
		// may be signed.
		return nil, oci.StripRedirectTarget(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &oci.StatusError{Code: resp.StatusCode, Source: oci.SourceManagementAPI}
	}
	body, err := oci.ReadAllLimited(resp.Body, maxNexusResponse)
	if err != nil {
		// Which error net/http gives a body cut by a cancel varies: report the
		// context's own.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("read Nexus response: %w", ctxErr)
		}
		return nil, fmt.Errorf("read Nexus response: %w", err)
	}
	return body, nil
}

// parseNexusURL extracts the scheme+host and repository name from a Nexus URL.
// e.g. "https://nexus.example.com/repository/docker-group" → ("https://nexus.example.com", "docker-group")
func parseNexusURL(registryURL string) (host, repoName string, err error) {
	raw := registryURL
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 3)
	// Expected: ["repository", "<repoName>", ...]
	if len(parts) < 2 || parts[0] != "repository" || parts[1] == "" {
		return "", "", fmt.Errorf("not a Nexus repository path: %q", u.Path)
	}
	host = u.Scheme + "://" + u.Host
	repoName = parts[1]
	return host, repoName, nil
}

// cleanMemberAlias strips common suffixes (-proxy, -hosted, -local) for a friendlier display name.
func cleanMemberAlias(name string) string {
	for _, suffix := range []string{"-proxy", "-hosted", "-local"} {
		if strings.HasSuffix(name, suffix) {
			return name[:len(name)-len(suffix)]
		}
	}
	return name
}
