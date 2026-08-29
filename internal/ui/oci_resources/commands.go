package ociresources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/registrymgr"
	"github.com/anthnel/devdesk/internal/scan"
)

const refreshInterval = 10 * time.Second

// tickCmd returns a tick command for periodic refresh
func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return RefreshTickMsg(t)
	})
}

// fetchImages fetches the image list
func fetchImages() tea.Cmd {
	return func() tea.Msg {
		images, err := docker.ListImages()
		return ImagesListMsg{Images: images, Err: err}
	}
}

// loadScanCache loads the image scan cache from disk
func loadScanCache() tea.Cmd {
	return func() tea.Msg {
		c, err := cache.NewImageScanCache()
		if err != nil {
			return ScanCacheLoadedMsg{Entries: nil}
		}
		return ScanCacheLoadedMsg{Entries: c.GetAll()}
	}
}

// deleteScanCacheCmd removes the given image keys from the disk cache (Rule 126: scan all purges cache)
func deleteScanCacheCmd(keys []string) tea.Cmd {
	return func() tea.Msg {
		c, err := cache.NewImageScanCache()
		if err != nil {
			return nil
		}
		for _, key := range keys {
			_ = c.Delete(key)
		}
		return nil
	}
}

// removeImageCmd removes a Docker image
func removeImageCmd(id, name string) tea.Cmd {
	return func() tea.Msg {
		err := docker.RemoveImage(id, false)
		return ImageActionMsg{Action: "remove", ID: id, Name: name, Err: err}
	}
}

// pruneImagesCmd prunes unused Docker images
func pruneImagesCmd() tea.Cmd {
	return func() tea.Msg {
		output, err := docker.PruneImages()
		return PruneCompleteMsg{Output: output, Err: err}
	}
}

// imageScanJob pairs a display name (used as cache key) with the actual Trivy
// scan target. For untagged images the target is the short image ID so Trivy
// resolves the image locally instead of pulling "repo:latest" from a registry.
type imageScanJob struct {
	Name   string // cache key and display name
	Target string // Trivy scan target (may differ from Name for untagged images)
}

// scanOneImageCmd scans a single image, using a semaphore to limit concurrency.
// It emits ImageScanStartingMsg before scanning and ImageScanFinishedMsg after.
func scanOneImageCmd(job imageScanJob, opts scan.ScanOptions, sem chan struct{}) tea.Cmd {
	// The context the scan runs under, so `K` can stop it (D7). It travels on
	// the starting message, which is the same Update that marks the item
	// running — two steps would leave a window where the row is running and
	// cannot be stopped.
	ctx, cancel := context.WithCancel(context.Background())

	return tea.Sequence(
		// The starting message waits for its turn in the pool, which is what
		// makes queued and running mean different things (D6). Emitted before
		// the wait — as it was — every image in a batch reported itself running
		// the instant the batch was dispatched, so twelve rows spun on four
		// workers.
		func() tea.Msg {
			sem <- struct{}{}
			return ImageScanStartingMsg{ImageName: job.Name, Cancel: cancel}
		},
		func() tea.Msg {
			defer func() { <-sem }()
			// Releases the context whether the scan was cancelled or ran to the
			// end; the registry drops its copy when the item settles.
			defer cancel()

			scanner := scan.NewScanner(opts)
			result, err := scanner.Scan(ctx, job.Target, scan.TargetImage)
			if err != nil {
				log.Printf("ERROR: Scan failed for %s: %v", job.Name, err)
				return ImageScanFinishedMsg{ImageName: job.Name, Err: err}
			}

			// Scan() never returns a non-nil error; scanner errors land in result.Errors.
			// If every scanner failed (errors present, no findings), surface it as an error.
			if len(result.Errors) > 0 && result.TotalFindings() == 0 {
				combined := strings.Join(result.Errors, "; ")
				log.Printf("ERROR: Scan errors for %s: %s", job.Name, combined)
				return ImageScanFinishedMsg{ImageName: job.Name, Err: fmt.Errorf("%s", combined)}
			}

			entry := cache.ImageScanEntry{
				Critical: result.Counts.Critical,
				High:     result.Counts.High,
				Medium:   result.Counts.Medium,
				Low:      result.Counts.Low,
				// Trivy lit les couches d'une image, ce que Gitleaks ne sait pas
				// faire : c'est ce qui donne une étape secrets à un scan d'image,
				// et donc un verdict à enregistrer.
				Sensitive: result.SecretVerdict(),
				ScannedAt: result.EndTime,
			}
			scanCache, cErr := cache.NewImageScanCache()
			if cErr != nil {
				log.Printf("ERROR: Failed to open scan cache: %v", cErr)
			} else if sErr := scanCache.Set(job.Name, entry); sErr != nil {
				log.Printf("ERROR: Failed to cache scan for %s: %v", job.Name, sErr)
			}
			// Rule 126: persist full result so Enter can load it without re-scanning
			if sErr := cache.SaveImageScanResult(job.Name, result); sErr != nil {
				log.Printf("ERROR: Failed to save full scan result for %s: %v", job.Name, sErr)
			}
			return ImageScanFinishedMsg{ImageName: job.Name, Entry: entry}
		},
	)
}

// batchScanCmd scans all provided images in parallel using a worker pool
// of runtime.NumCPU()/2 workers (minimum 1).
func batchScanCmd(jobs []imageScanJob, opts scan.ScanOptions) tea.Cmd {
	numWorkers := max(runtime.NumCPU()/2, 1)
	sem := make(chan struct{}, numWorkers)

	cmds := make([]tea.Cmd, len(jobs))
	for i, job := range jobs {
		cmds[i] = scanOneImageCmd(job, opts, sem)
	}
	return tea.Batch(cmds...)
}

// fetchNetworks fetches the Docker network list
func fetchNetworks() tea.Cmd {
	return func() tea.Msg {
		networks, err := docker.ListNetworks()
		return NetworksListMsg{Networks: networks, Err: err}
	}
}

// createNetworkCmd creates a Docker network
func createNetworkCmd(name, driver string) tea.Cmd {
	return func() tea.Msg {
		err := docker.CreateNetwork(name, driver)
		return NetworkActionMsg{Action: "create", Err: err}
	}
}

// removeNetworkCmd removes a Docker network by ID
func removeNetworkCmd(id string) tea.Cmd {
	return func() tea.Msg {
		err := docker.RemoveNetwork(id)
		return NetworkActionMsg{Action: "remove", ID: id, Err: err}
	}
}

// pruneNetworksCmd prunes unused Docker networks
func pruneNetworksCmd() tea.Cmd {
	return func() tea.Msg {
		output, err := docker.PruneNetworks()
		return NetworkPruneCompleteMsg{Output: output, Err: err}
	}
}

// fetchVolumes fetches the Docker volume list
func fetchVolumes() tea.Cmd {
	return func() tea.Msg {
		volumes, err := docker.ListVolumes()
		return VolumesListMsg{Volumes: volumes, Err: err}
	}
}

// createVolumeCmd creates a Docker volume
func createVolumeCmd(name, driver string) tea.Cmd {
	return func() tea.Msg {
		err := docker.CreateVolume(name, driver)
		return VolumeActionMsg{Action: "create", Err: err}
	}
}

// removeVolumeCmd removes a Docker volume by name
func removeVolumeCmd(name string) tea.Cmd {
	return func() tea.Msg {
		err := docker.RemoveVolume(name)
		return VolumeActionMsg{Action: "remove", Name: name, Err: err}
	}
}

// pruneVolumesCmd prunes unused Docker volumes
func pruneVolumesCmd() tea.Cmd {
	return func() tea.Msg {
		output, err := docker.PruneVolumes()
		return VolumePruneCompleteMsg{Output: output, Err: err}
	}
}

// fetchImageExposedPortsCmd fetches EXPOSE ports from an image for the launch form
func fetchImageExposedPortsCmd(imageName string) tea.Cmd {
	return func() tea.Msg {
		ports, err := docker.GetImageExposedPorts(imageName)
		return ImageExposedPortsMsg{ImageName: imageName, Ports: ports, Err: err}
	}
}

// launchContainerCmd runs a container with the given options
func launchContainerCmd(opts docker.ContainerLaunchOptions) tea.Cmd {
	return func() tea.Msg {
		id, err := docker.LaunchContainer(opts)
		return ContainerLaunchCompleteMsg{ContainerID: id, Err: err}
	}
}

// registryLoginCmd runs `docker login` for the given registry.
// If password is empty, attempts login without a password (uses stored credentials).
func registryLoginCmd(registryURL, username, password string) tea.Cmd {
	return func() tea.Msg {
		log.Printf("INFO [oci_resources] docker login %s (user: %s)", registryURL, username)
		err := docker.RegistryLogin(registryURL, username, password)
		return RegistryLoginCompleteMsg{RegistryURL: registryURL, Err: err}
	}
}

// checkRegistryLoginStatusCmd checks ~/.docker/config.json for all provided registry URLs.
func checkRegistryLoginStatusCmd(urls []string) tea.Cmd {
	return func() tea.Msg {
		status := make(map[string]bool, len(urls))
		for _, url := range urls {
			status[url] = docker.IsRegistryLoggedIn(url)
		}
		return RegistryLoginStatusMsg{Status: status}
	}
}

// registryLogoutCmd runs `docker logout` for the given registry URL.
func registryLogoutCmd(registryURL string) tea.Cmd {
	return func() tea.Msg {
		log.Printf("INFO [oci_resources] docker logout %s", registryURL)
		err := docker.RegistryLogout(registryURL)
		return RegistryLogoutCompleteMsg{RegistryURL: registryURL, Err: err}
	}
}

// inspectNetworkCmd fetches the containers connected to a Docker network
func inspectNetworkCmd(networkID, networkName string) tea.Cmd {
	return func() tea.Msg {
		containers, err := docker.InspectNetwork(networkID)
		return NetworkInspectLoadedMsg{
			NetworkID:   networkID,
			NetworkName: networkName,
			Containers:  containers,
			Err:         err,
		}
	}
}

// runDiagnosticContainerCmd runs a connectivity test in an ephemeral network-multitool container
func runDiagnosticContainerCmd(networkID, image string, command []string) tea.Cmd {
	return func() tea.Msg {
		output, err := docker.RunDiagnosticContainer(networkID, image, command)
		return DiagnosticTestCompleteMsg{Output: output, Err: err}
	}
}

// loadLaunchOptionsCmd loads cached launch options for an image from disk.
func loadLaunchOptionsCmd(imageKey string) tea.Cmd {
	return func() tea.Msg {
		c, err := cache.NewLaunchOptionsCache()
		if err != nil {
			log.Printf("ERROR [oci_resources] load launch options cache: %v", err)
			return LaunchOptionsCacheLoadedMsg{ImageName: imageKey, Entry: nil}
		}
		return LaunchOptionsCacheLoadedMsg{ImageName: imageKey, Entry: c.Get(imageKey)}
	}
}

// saveLaunchOptionsCmd saves launch options for an image to disk (fire-and-forget).
func saveLaunchOptionsCmd(imageKey string, entry cache.LaunchOptionsEntry) tea.Cmd {
	return func() tea.Msg {
		c, err := cache.NewLaunchOptionsCache()
		if err != nil {
			log.Printf("ERROR [oci_resources] open launch options cache: %v", err)
			return nil
		}
		if err := c.Set(imageKey, entry); err != nil {
			log.Printf("ERROR [oci_resources] save launch options for %s: %v", imageKey, err)
		}
		return nil
	}
}

// copyToClipboardCmd writes the given text to the system clipboard (Rule 110: I/O in Cmd).
func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		err := clipboard.WriteAll(text)
		return ClipboardCopyMsg{Err: err}
	}
}

// verifyEntrypointCmd checks if an entrypoint binary exists inside an image.
// Uses a 600ms debounce delay so rapid keystrokes don't flood docker.
// The seq parameter guards against stale results: results are discarded if seq
// no longer matches the form's current verifySeq when the message is received.
func verifyEntrypointCmd(seq int, image, entrypoint string) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(600 * time.Millisecond)
		ok, _ := docker.VerifyEntrypoint(image, entrypoint)
		return EntrypointVerifyFinishedMsg{Seq: seq, OK: ok}
	}
}

// ---- Registry browser commands ----

var ociHTTPClient = &http.Client{Timeout: 15 * time.Second}

// registryAPIURL converts a user-specified registry URL into a v2 API base URL.
//
// This is the one place that *keeps* a scheme rather than stripping it: an
// explicit `http://` is how a registry on a plain-HTTP port is reached, and
// upgrading it would break that registry rather than fix anything. Everything
// Docker-facing goes through registryHost instead (D39).
func registryAPIURL(registryURL string) string {
	base := strings.TrimSuffix(strings.TrimSpace(registryURL), "/")
	if isDockerHub(base) {
		return "https://registry-1.docker.io"
	}
	lower := strings.ToLower(base)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return "https://" + base
	}
	return base
}

// doRegistryGETBytes performs a GET on rawURL, handling bearer token auth on 401.
func doRegistryGETBytes(rawURL, username, password string) ([]byte, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if username != "" && password != "" {
		req.SetBasicAuth(username, password)
	}
	resp, err := ociHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		wwwAuth := resp.Header.Get("Www-Authenticate")
		if !strings.HasPrefix(wwwAuth, "Bearer ") {
			return nil, fmt.Errorf("unexpected auth challenge: %s", wwwAuth)
		}
		token, tErr := exchangeBearerToken(wwwAuth[7:], username, password)
		if tErr != nil {
			return nil, tErr
		}
		req2, err := http.NewRequest("GET", rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build retry request: %w", err)
		}
		req2.Header.Set("Authorization", "Bearer "+token)
		resp2, err := ociHTTPClient.Do(req2)
		if err != nil {
			return nil, fmt.Errorf("GET (retry) %s: %w", rawURL, err)
		}
		defer func() { _ = resp2.Body.Close() }()
		if resp2.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("registry returned %d", resp2.StatusCode)
		}
		return io.ReadAll(resp2.Body)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
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
	resp, err := ociHTTPClient.Do(req)
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

// fetchRegistryTags fetches the tag list for a repository from the registry API.
func fetchRegistryTags(apiURL, repo, username, password string) ([]string, error) {
	endpoint := fmt.Sprintf("%s/v2/%s/tags/list", strings.TrimSuffix(apiURL, "/"), repo)
	body, err := doRegistryGETBytes(endpoint, username, password)
	if err != nil {
		return nil, err
	}
	var result struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode tags response: %w", err)
	}
	return result.Tags, nil
}

// searchRegistryTagsCmd fetches tags from one registry and returns a MultiRegistryTagsLoadedMsg.
func searchRegistryTagsCmd(entryKey, registryURL, alias, apiURL, repo, username, password string) tea.Cmd {
	return func() tea.Msg {
		tags, err := fetchRegistryTags(apiURL, repo, username, password)
		return MultiRegistryTagsLoadedMsg{
			EntryKey: entryKey, RegistryURL: registryURL, Alias: alias, Repo: repo, Tags: tags, Err: err,
		}
	}
}

// fetchDockerHubTagsMeta fetches last_updated times from the Docker Hub public API.
// Works for public repos (no auth needed). Returns partial results on error.
func fetchDockerHubTagsMeta(namespace, repo string) (map[string]time.Time, error) {
	endpoint := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/%s/tags?page_size=100&ordering=last_updated", namespace, repo)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := ociHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hub API returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result struct {
		Results []struct {
			Name        string `json:"name"`
			LastUpdated string `json:"last_updated"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	meta := make(map[string]time.Time, len(result.Results))
	for _, r := range result.Results {
		if t, err := time.Parse(time.RFC3339Nano, r.LastUpdated); err == nil {
			meta[r.Name] = t
		}
	}
	return meta, nil
}

// loadMultiRegistryTagsMetaCmd fetches tag metadata for one registry in the background.
// For Docker Hub it uses the Hub API; other registries return empty (no error).
func loadMultiRegistryTagsMetaCmd(entryKey, registryURL, repo string) tea.Cmd {
	return func() tea.Msg {
		empty := MultiRegistryTagsMetaMsg{EntryKey: entryKey, RegistryURL: registryURL, Repo: repo}
		base := strings.ToLower(strings.TrimSuffix(registryURL, "/"))
		isDockerhub := base == "docker.io" || base == "registry-1.docker.io"
		if !isDockerhub {
			return empty
		}
		parts := strings.SplitN(repo, "/", 2)
		if len(parts) != 2 {
			return empty
		}
		meta, err := fetchDockerHubTagsMeta(parts[0], parts[1])
		return MultiRegistryTagsMetaMsg{
			EntryKey: entryKey, RegistryURL: registryURL, Repo: repo, Meta: meta, Err: err,
		}
	}
}

// pullRegistryImageCmd pulls a Docker image to the local store.
func pullRegistryImageCmd(imageName string) tea.Cmd {
	return func() tea.Msg {
		err := docker.PullImage(imageName)
		return RegistryPullCompleteMsg{ImageName: imageName, Err: err}
	}
}

// detectRegistryGroupCmd calls the generic registrymgr.DetectGroup to determine
// whether reg is a group repository and enumerate its members.
// Always emits RegistryGroupDetectedMsg (Members==nil means not a group).
func detectRegistryGroupCmd(reg config.RegistryItem, password string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		username := reg.Username
		// D12: a registry the user marked anonymous is probed anonymously.
		// Docker keys credentials by host, so without this gate the credentials
		// stored for any registry on that host went out to all of them — which,
		// for a Nexus instance, is every repository it serves.
		if config.UsesCredentials(config.ResolveAuthMode(reg, nil)) {
			username, password = discoveryCreds(reg, username, password)
		} else {
			username, password = "", ""
		}

		info := registrymgr.RegistryInfo{
			Provider:      reg.Provider,
			URL:           reg.URL,
			ManagementURL: reg.ManagementURL,
			Username:      username,
			Password:      password,
		}
		members, err := registrymgr.DetectGroup(ctx, info)
		if err == nil {
			cacheGroupMembers(reg.Slug, members)
		}
		return RegistryGroupDetectedMsg{RegistryURL: reg.URL, Slug: reg.Slug, Members: members, Err: err}
	}
}

// cacheGroupMembers records what a discovery found, so the next open reads it
// from disk instead of the network (§3.8, decision 3).
//
// A discovery that found nothing is stored too: "asked, and it is not a group"
// is an answer, and not storing it is what makes a non-group get probed forever.
// An error is not stored — an unreachable manager must not overwrite what was
// last known.
func cacheGroupMembers(slug string, members []registrymgr.GroupMember) {
	if slug == "" {
		return
	}
	c, err := cache.NewRegistryGroupCache()
	if err != nil {
		log.Printf("ERROR [oci_resources] open the registry group cache: %v", err)
		return
	}
	entry := cache.RegistryGroupEntry{
		Members:      make([]cache.RegistryGroupMember, 0, len(members)),
		DiscoveredAt: time.Now(),
	}
	for _, m := range members {
		entry.Members = append(entry.Members, cache.RegistryGroupMember{Alias: m.Alias, URL: m.URL, RepoPrefix: m.RepoPrefix})
	}
	if err := c.Set(slug, entry); err != nil {
		log.Printf("ERROR [oci_resources] save group members for %q: %v", slug, err)
	}
}

// loadBrowserSelectionCmd reads the registries this context had unchecked.
func loadBrowserSelectionCmd() tea.Cmd {
	return func() tea.Msg {
		c, err := cache.NewBrowserSelectionCache()
		if err != nil {
			log.Printf("ERROR [oci_resources] open the browser selection cache: %v", err)
			return BrowserSelectionLoadedMsg{}
		}
		ctx, err := config.GetCurrentContext()
		if err != nil {
			ctx = "default"
		}
		return BrowserSelectionLoadedMsg{Deselected: c.Deselected(ctx)}
	}
}

// saveBrowserSelectionCmd records what the user unchecked, for this context.
func saveBrowserSelectionCmd(deselected []string) tea.Cmd {
	return func() tea.Msg {
		c, err := cache.NewBrowserSelectionCache()
		if err != nil {
			log.Printf("ERROR [oci_resources] open the browser selection cache: %v", err)
			return nil
		}
		ctx, err := config.GetCurrentContext()
		if err != nil {
			ctx = "default"
		}
		if err := c.SetDeselected(ctx, deselected); err != nil {
			log.Printf("ERROR [oci_resources] save the browser selection: %v", err)
		}
		return nil
	}
}

// loadRegistryGroupCache reads every cached discovery from disk.
func loadRegistryGroupCache() tea.Cmd {
	return func() tea.Msg {
		c, err := cache.NewRegistryGroupCache()
		if err != nil {
			log.Printf("ERROR [oci_resources] open the registry group cache: %v", err)
			return RegistryGroupCacheLoadedMsg{}
		}
		return RegistryGroupCacheLoadedMsg{Entries: c.GetAll()}
	}
}

// discoveryCreds completes the credentials the group probe authenticates with.
// A username or password already stated outranks anything stored: it is the more
// recent statement of intent, and it is how a wrong stored credential is worked
// around.
func discoveryCreds(reg config.RegistryItem, username, password string) (string, string) {
	storedUser, storedPass, _ := docker.GetStoredCreds(reg.URL)
	if username == "" {
		username = storedUser
	}
	if password == "" {
		password = storedPass
	}
	if reg.ManagementURL == "" || password != "" {
		return username, password
	}

	// The management host may differ from the Docker registry host. Docker
	// stores credentials by hostname only, so the repository path has to be
	// stripped before looking one up.
	mgmtLookup := reg.ManagementURL
	if u, err := url.Parse(reg.ManagementURL); err == nil && u.Host != "" {
		mgmtLookup = u.Scheme + "://" + u.Host
	}
	mgmtUser, mgmtPass, ok := docker.GetStoredCreds(mgmtLookup)
	if !ok {
		return username, password
	}
	if username == "" {
		username = mgmtUser
	}
	return username, mgmtPass
}
