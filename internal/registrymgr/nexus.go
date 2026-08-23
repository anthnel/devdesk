package registrymgr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
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

var nexusHTTPClient = &http.Client{Timeout: 8 * time.Second}

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
		log.Printf("INFO [registrymgr/nexus] cannot parse Nexus URL %q: %v", apiBase, err)
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
		if info.Username != "" && info.Password != "" {
			anonInfo := RegistryInfo{URL: info.URL, ManagementURL: info.ManagementURL}
			body, err = n.nexusGET(ctx, endpoint, anonInfo)
		}
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

// nexusGET performs an authenticated GET and returns the response body.
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
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var buf []byte
	buf, err = io.ReadAll(resp.Body)
	return buf, err
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
