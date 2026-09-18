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
	Name    string
	Tag     string
	Entries []ArchiveFile
}

// ArchiveFile is one regular file read from an archive.
type ArchiveFile struct {
	Path       string // relative to the archive root, forward slashes
	Content    []byte
	Executable bool
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
	entries, err := c.downloadAndExtractLayer(repository, layerDigest)
	if err != nil {
		return nil, err
	}

	return &Template{
		Name:    repository,
		Tag:     tag,
		Entries: entries,
	}, nil
}

// downloadAndExtractLayer downloads a layer and extracts its contents
func (c *Client) downloadAndExtractLayer(repository, digest string) ([]ArchiveFile, error) {
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

	return ReadTarGz(resp.Body)
}

// ReadTarGz reads a gzipped tar archive into its regular files.
func ReadTarGz(r io.Reader) ([]ArchiveFile, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = gzr.Close() }()
	return ReadTar(gzr)
}

// ReadTar reads a tar archive into its regular files.
//
// Directories and pax headers are skipped. Anything else that is not a regular
// file — a symlink, a hard link, a device — is refused rather than read: the
// old behavior turned a symlink into an empty file, which is a template
// silently missing what it says it contains.
func ReadTar(r io.Reader) ([]ArchiveFile, error) {
	tr := tar.NewReader(r)
	var files []ArchiveFile

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch header.Typeflag {
		case tar.TypeDir, tar.TypeXGlobalHeader:
			continue
		case tar.TypeReg:
		default:
			return nil, fmt.Errorf("archive entry %q is not a regular file (type %q)", header.Name, header.Typeflag)
		}

		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}

		path, err := sanitizeArchivePath(header.Name)
		if err != nil {
			return nil, err
		}

		files = append(files, ArchiveFile{
			Path:       path,
			Content:    content,
			Executable: header.Mode&0o111 != 0,
		})
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
