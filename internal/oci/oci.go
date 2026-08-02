package oci

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

// Client handles OCI registry operations
type Client struct {
	registryURL string
	username    string
	password    string
	httpClient  *http.Client
}

// Template represents an OCI template with its files
type Template struct {
	Name  string
	Tag   string
	Files map[string]string // filename -> content
}

// TemplateEntry represents a template available in the registry
type TemplateEntry struct {
	Repository string // full repository path (e.g., "group/project/templates/java-library")
	Tag        string // version tag (e.g., "v1")
	Name       string // display name (e.g., "java-library:v1")
}

// NewClient creates a new OCI client
func NewClient(registryURL, username, password string) *Client {
	return &Client{
		registryURL: strings.TrimSuffix(registryURL, "/"),
		username:    username,
		password:    password,
		httpClient:  &http.Client{},
	}
}

// ListTags lists available tags for a repository (templates)
func (c *Client) ListTags(repository string) ([]string, error) {
	url := fmt.Sprintf("%s/v2/%s/tags/list", c.registryURL, repository)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if c.username != "" && c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list tags: %s (GET %s)", resp.Status, url)
	}

	var result struct {
		Tags []string `json:"tags"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Tags, nil
}

// listCatalog lists all repositories in the registry via /v2/_catalog
func (c *Client) listCatalog() ([]string, error) {
	url := fmt.Sprintf("%s/v2/_catalog?n=1000", c.registryURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if c.username != "" && c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list catalog: %s (GET %s)", resp.Status, url)
	}

	var result struct {
		Repositories []string `json:"repositories"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Repositories, nil
}

// ListTemplates lists all available templates under a base repository path.
// It queries the registry catalog, filters repositories matching the basePath prefix,
// then lists tags for each matching repository.
func (c *Client) ListTemplates(basePath string) ([]TemplateEntry, error) {
	repos, err := c.listCatalog()
	if err != nil {
		return nil, fmt.Errorf("list catalog: %w", err)
	}

	prefix := basePath + "/"
	var entries []TemplateEntry

	for _, repo := range repos {
		if !strings.HasPrefix(repo, prefix) {
			continue
		}

		shortName := strings.TrimPrefix(repo, prefix)

		tags, err := c.ListTags(repo)
		if err != nil {
			// Skip repos we can't list tags for
			continue
		}

		for _, tag := range tags {
			entries = append(entries, TemplateEntry{
				Repository: repo,
				Tag:        tag,
				Name:       shortName + ":" + tag,
			})
		}
	}

	return entries, nil
}

// DownloadTemplate downloads and extracts a template from the registry
func (c *Client) DownloadTemplate(repository, tag string) (*Template, error) {
	// Get manifest
	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", c.registryURL, repository, tag)

	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	if c.username != "" && c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get manifest: %s", resp.Status)
	}

	var manifest struct {
		Layers []struct {
			MediaType string `json:"mediaType"`
			Digest    string `json:"digest"`
		} `json:"layers"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, err
	}

	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("no layers found in manifest")
	}

	// Download first layer (assumed to be tar.gz of template files)
	layerDigest := manifest.Layers[0].Digest
	files, err := c.downloadAndExtractLayer(repository, layerDigest)
	if err != nil {
		return nil, err
	}

	return &Template{
		Name:  repository,
		Tag:   tag,
		Files: files,
	}, nil
}

// downloadAndExtractLayer downloads a layer and extracts its contents
func (c *Client) downloadAndExtractLayer(repository, digest string) (map[string]string, error) {
	url := fmt.Sprintf("%s/v2/%s/blobs/%s", c.registryURL, repository, digest)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if c.username != "" && c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download layer: %s", resp.Status)
	}

	return extractTarGz(resp.Body)
}

// extractTarGz extracts a tar.gz archive and returns file contents
func extractTarGz(r io.Reader) (map[string]string, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = gzr.Close() }()

	tr := tar.NewReader(gzr)
	files := make(map[string]string)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// Skip directories
		if header.Typeflag == tar.TypeDir {
			continue
		}

		// Read file content
		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}

		path, err := sanitizeArchivePath(header.Name)
		if err != nil {
			return nil, err
		}

		files[path] = string(content)
	}

	return files, nil
}

// sanitizeArchivePath normalises a tar member name into a path relative to the
// archive root, and rejects anything that escapes it. Returning the parent
// references verbatim would hand a Zip Slip to any caller that writes these
// keys under a target directory.
func sanitizeArchivePath(name string) (string, error) {
	// Tar always uses forward slashes; normalise Windows-style separators so a
	// member named `..\..\etc\passwd` is not waved through as a plain filename.
	clean := strings.ReplaceAll(name, `\`, "/")
	// Drop the leading separator *before* cleaning: path.Clean("/../x") returns
	// "/x", which would silently absorb the traversal instead of exposing it.
	clean = path.Clean(strings.TrimLeft(clean, "/"))

	if clean == "" || clean == "." {
		return "", fmt.Errorf("archive contains an entry with an empty path: %q", name)
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("archive entry escapes the root: %q", name)
	}

	return clean, nil
}
