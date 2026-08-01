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
//  1. Path-based:   https://nexus.example.com/repository/docker-group  (auto-detected)
//  2. Subdomain:    nexus-docker-group.example.com  (requires info.NexusURL to be set)
type NexusDetector struct{}

var nexusHTTPClient = &http.Client{Timeout: 8 * time.Second}

// CanHandle returns true when:
//   - the registry URL contains the Nexus "/repository/" path segment, OR
//   - info.ManagementURL is explicitly set (subdomain connector case)
func (n *NexusDetector) CanHandle(info RegistryInfo) bool {
	if info.ManagementURL != "" {
		return true
	}
	return strings.Contains(info.URL, "/repository/")
}

// DetectGroup calls the Nexus REST API to determine whether the registry points
// to a group repository and, if so, returns its member repositories.
// Returns nil, nil when the repository exists but is not a group.
func (n *NexusDetector) DetectGroup(ctx context.Context, info RegistryInfo) ([]GroupMember, error) {
	// Resolve which URL to use for the Nexus API call.
	apiBase := info.ManagementURL
	if apiBase == "" {
		apiBase = info.URL
	}

	host, repoName, err := parseNexusURL(apiBase)
	if err != nil {
		log.Printf("INFO [registrymgr/nexus] cannot parse Nexus URL %q: %v", apiBase, err)
		return nil, nil
	}

	hasAuth := info.Username != "" && info.Password != ""
	log.Printf("INFO [registrymgr/nexus] detecting group for %q (auth=%v)", repoName, hasAuth)

	// Step 1: generic endpoint — confirms this repo is a group, gets its format,
	// and may already include memberNames in the attributes field (no admin privilege needed).
	repoType, repoFormat, members, ok := n.fetchRepoMeta(ctx, host, repoName, info)
	if !ok || repoType != "group" {
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
// Returns type, format, any memberNames found in attributes, and whether the call succeeded.
// Some Nexus deployments include memberNames in attributes.group without requiring admin privilege.
func (n *NexusDetector) fetchRepoMeta(ctx context.Context, host, repoName string, info RegistryInfo) (repoType, format string, members []GroupMember, ok bool) {
	endpoint := fmt.Sprintf("%s/service/rest/v1/repositories/%s", host, repoName)
	body, err := n.nexusGET(ctx, endpoint, info)
	if err != nil {
		log.Printf("INFO [registrymgr/nexus] meta unreachable %q: %v", endpoint, err)
		return "", "", nil, false
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
		return "", "", nil, false
	}
	log.Printf("INFO [registrymgr/nexus] meta %q: type=%s format=%s", repoName, result.Type, result.Format)

	// Collect memberNames from whichever field Nexus populated.
	rawNames := result.Group.MemberNames
	if len(rawNames) == 0 {
		rawNames = result.Attributes.Group.MemberNames
	}
	for _, name := range rawNames {
		members = append(members, GroupMember{
			Alias: cleanMemberAlias(name),
			URL:   fmt.Sprintf("%s/repository/%s", host, name),
		})
	}
	return result.Type, result.Format, members, true
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
	members := make([]GroupMember, 0, len(result.Group.MemberNames))
	for _, name := range result.Group.MemberNames {
		members = append(members, GroupMember{
			Alias: cleanMemberAlias(name),
			URL:   fmt.Sprintf("%s/repository/%s", host, name),
		})
	}
	return members
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
