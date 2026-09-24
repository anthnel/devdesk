package oci

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// registryHTTPClient is swapped by tests that must reach a stand-in for a
// registry by another address.
var registryHTTPClient = &http.Client{Timeout: 15 * time.Second}

const (
	// maxTagPages bounds how many pages of a tag list are followed. A registry
	// that pages (`?n=`, `Link: rel="next"`) answers a page at a time, and one
	// that keeps answering "next" must not hold a caller forever.
	maxTagPages = 100
	// maxTagResponse bounds one page. Docker Hub answers a whole list in one
	// response when no page size is given — measured at 9 125 tags for
	// library/node (§3.2) — so the bound is generous, but it is a bound.
	maxTagResponse = 32 << 20
)

// ListRegistryTags lists the tags of a repository through the registry's v2 API.
//
// apiURL is the API base (see the browser's registryAPIURL). Credentials, when
// given, go as Basic auth and, on a 401 that names a Bearer realm, to that
// realm's token endpoint — the flow Docker Hub and most hosted registries use.
// The token is kept for the following pages.
//
// The list is followed across pages: whatever a registry caps a response at,
// and Docker Hub does not, a repository with more tags than one page holds is
// returned whole. This is the one tag lister that speaks the Bearer flow; the
// template client's ListTags (Client.ListTags) reads a registry the user
// configured for templates and only ever sends Basic auth.
func ListRegistryTags(apiURL, repo, username, password string) ([]string, error) {
	next := fmt.Sprintf("%s/v2/%s/tags/list", strings.TrimSuffix(apiURL, "/"), repo)
	var (
		tags  []string
		token string
	)
	for page := 0; next != ""; page++ {
		if page == maxTagPages {
			return nil, fmt.Errorf("tag list of %s is longer than %d pages", repo, maxTagPages)
		}
		body, link, usedToken, err := registryGET(next, username, password, token)
		if err != nil {
			return nil, err
		}
		token = usedToken
		var result struct {
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("decode tags response: %w", err)
		}
		tags = append(tags, result.Tags...)
		next = resolveNext(next, link)
	}
	return tags, nil
}

// registryGET performs a GET, answering a Bearer challenge once. It returns the
// body, the Link header, and the token to send on the following requests.
func registryGET(rawURL, username, password, token string) (body []byte, link, usedToken string, err error) {
	resp, token, err := registryDo(http.MethodGet, rawURL, "", username, password, token)
	if err != nil {
		return nil, "", token, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", token, fmt.Errorf("registry returned %d", resp.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxTagResponse))
	return body, resp.Header.Get("Link"), token, err
}

// registryDo sends one request, answering a Bearer challenge once. The caller
// closes the response body and reads the status.
func registryDo(method, rawURL, accept, username, password, token string) (*http.Response, string, error) {
	resp, err := doRequest(method, rawURL, accept, username, password, token)
	if err != nil {
		return nil, token, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, token, nil
	}
	challenge := resp.Header.Get("Www-Authenticate")
	_ = resp.Body.Close()
	if !strings.HasPrefix(challenge, "Bearer ") {
		return nil, token, fmt.Errorf("unexpected auth challenge: %s", challenge)
	}
	token, err = exchangeBearerToken(challenge[7:], username, password)
	if err != nil {
		return nil, "", err
	}
	if resp, err = doRequest(method, rawURL, accept, "", "", token); err != nil {
		return nil, token, fmt.Errorf("%s (retry) %s: %w", method, rawURL, err)
	}
	return resp, token, nil
}

// doRequest sends one request: a bearer token when there is one, Basic auth
// when there are credentials and no token.
func doRequest(method, rawURL, accept, username, password, token string) (*http.Response, error) {
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	switch {
	case token != "":
		req.Header.Set("Authorization", "Bearer "+token)
	case username != "" && password != "":
		req.SetBasicAuth(username, password)
	}
	resp, err := registryHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, rawURL, err)
	}
	return resp, nil
}

// manifestAccept lists every manifest kind a tag can point to. A multi-platform
// tag is an index (or a Docker manifest list), and its digest is the one `docker
// pull` records in RepoDigests; asking for a single-platform manifest only
// would make the registry convert or refuse, and the digests would not match.
var manifestAccept = strings.Join([]string{
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.docker.distribution.manifest.v2+json",
}, ", ")

// ManifestDigest returns the digest a tag currently points to, from the
// registry's Docker-Content-Digest header.
//
// It is a HEAD request: Docker Hub does not count a HEAD against the pull rate
// limit, and nothing but the header is needed.
func ManifestDigest(apiURL, repo, tag, username, password string) (string, error) {
	rawURL := fmt.Sprintf("%s/v2/%s/manifests/%s", strings.TrimSuffix(apiURL, "/"), repo, url.PathEscape(tag))
	resp, _, err := registryDo(http.MethodHead, rawURL, manifestAccept, username, password, "")
	if err != nil {
		return "", err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry returned %d for %s:%s", resp.StatusCode, repo, tag)
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("registry gave no digest for %s:%s", repo, tag)
	}
	return digest, nil
}

// RegistryAPIBase is the v2 API root of a registry named in an image reference:
// Docker Hub has its own host, a local registry is reached over plain HTTP, and
// everything else over HTTPS. An empty registry is Docker Hub.
func RegistryAPIBase(registry string) string {
	switch {
	case registry == "":
		return "https://registry-1.docker.io"
	case strings.HasPrefix(registry, "localhost") || strings.HasPrefix(registry, "127.0.0.1"):
		return "http://" + registry
	}
	return "https://" + registry
}

// resolveNext is the URL a Link header's rel="next" points to, resolved against
// the page it came with — registries send it as a path — or "" when there is
// none. A next link that leads back to the page just read ends the walk rather
// than looping on it.
func resolveNext(current, link string) string {
	for _, part := range strings.Split(link, ",") {
		target, rel, ok := strings.Cut(strings.TrimSpace(part), ";")
		if !ok || !strings.Contains(rel, `rel="next"`) {
			continue
		}
		target = strings.Trim(strings.TrimSpace(target), "<>")
		base, err := url.Parse(current)
		if err != nil {
			return ""
		}
		ref, err := url.Parse(target)
		if err != nil {
			return ""
		}
		if next := base.ResolveReference(ref).String(); next != current {
			return next
		}
	}
	return ""
}

// exchangeBearerToken fetches a bearer token from the registry's token endpoint.
func exchangeBearerToken(challenge, username, password string) (string, error) {
	params := parseBearerChallenge(challenge)
	realm, ok := params["realm"]
	if !ok {
		return "", fmt.Errorf("bearer challenge missing realm")
	}
	u, err := url.Parse(realm)
	if err != nil {
		return "", fmt.Errorf("parse realm %q: %w", realm, err)
	}
	q := u.Query()
	if s, ok := params["service"]; ok {
		q.Set("service", s)
	}
	if s, ok := params["scope"]; ok {
		q.Set("scope", s)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	if username != "" && password != "" {
		req.SetBasicAuth(username, password)
	}
	resp, err := registryHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}
	var result struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if result.Token != "" {
		return result.Token, nil
	}
	if result.AccessToken != "" {
		return result.AccessToken, nil
	}
	return "", fmt.Errorf("token response contained no token field")
}

// parseBearerChallenge parses the value portion of a Www-Authenticate: Bearer header.
// Example: `realm="https://auth.docker.io/token",service="registry.docker.io"`
func parseBearerChallenge(s string) map[string]string {
	params := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		idx := strings.IndexByte(part, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(part[:idx])
		val := strings.Trim(strings.TrimSpace(part[idx+1:]), `"`)
		params[key] = val
	}
	return params
}
