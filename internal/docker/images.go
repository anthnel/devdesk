package docker

import (
	"strconv"
	"strings"
)

// Image represents a Docker image with its metadata
type Image struct {
	ID         string
	Repository string
	Tag        string
	Size       int64 // Content size (virtual size) in bytes
	UniqueSize int64 // Disk usage (unique layers) in bytes
	Containers int
	CreatedAt  string
}

// Name returns "repository:tag", omitting the tag suffix when tag is "<none>".
func (img Image) Name() string {
	if img.Tag == "" || img.Tag == "<none>" {
		return img.Repository
	}
	return img.Repository + ":" + img.Tag
}

// ScanTarget returns the best identifier to pass to Trivy for a local scan.
// When an image has no tag, the short ID is used so Trivy resolves it locally
// instead of trying to pull "repository:latest" from a registry.
func (img Image) ScanTarget() string {
	if img.Tag == "" || img.Tag == "<none>" {
		return img.ID
	}
	return img.Name()
}

// ListImages returns a list of local Docker images
func ListImages() ([]Image, error) {
	if err := requireDocker(); err != nil {
		return nil, err
	}

	// {{.Size}} from docker image ls gives disk usage (not content size)
	output, err := dockerOutput("image", "ls", "--format",
		"{{.ID}}\t{{.Repository}}\t{{.Tag}}\t{{.Size}}\t{{.CreatedAt}}")
	if err != nil {
		return nil, wrapErr("docker image ls", err)
	}

	var images []Image
	for _, line := range splitLines(output) {
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 3 {
			continue
		}
		img := Image{
			ID:         parts[0],
			Repository: parts[1],
			Tag:        parts[2],
		}
		if len(parts) > 3 {
			// docker image ls {{.Size}} = disk usage
			img.UniqueSize = parseSize(parts[3])
		}
		if len(parts) > 4 {
			img.CreatedAt = parts[4]
		}
		images = append(images, img)
	}

	// Enrich with content size from docker image inspect and
	// more precise disk usage from docker system df -v
	enrichImagesWithDiskUsage(images)
	enrichImagesWithContentSize(images)

	return images, nil
}

// enrichImagesWithContentSize populates Size (content size) from docker image inspect.
// docker image inspect .Size gives the real content size (compressed layers in content store),
// while docker image ls .Size gives disk usage (unpacked layers on disk).
func enrichImagesWithContentSize(images []Image) {
	if len(images) == 0 {
		return
	}

	ids := make([]string, 0, len(images))
	for _, img := range images {
		ids = append(ids, img.ID)
	}

	args := append([]string{"image", "inspect", "--format", "{{.ID}}\t{{.Size}}"}, ids...)
	output, err := dockerOutput(args...)
	if err != nil {
		return
	}

	// Build lookup by short ID (strip "sha256:" prefix, keep first 12 chars)
	lookup := make(map[string]int64)
	for _, line := range splitLines(output) {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		shortID := strings.TrimPrefix(parts[0], "sha256:")
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		size, _ := strconv.ParseInt(parts[1], 10, 64)
		lookup[shortID] = size
	}

	for i := range images {
		if size, ok := lookup[images[i].ID]; ok {
			images[i].Size = size
		}
	}
}

// dfInfo holds the per-image figures scraped from `docker system df -v`.
type dfInfo struct {
	UniqueSize int64
	Containers int
}

// enrichImagesWithDiskUsage populates UniqueSize and Containers from docker system df -v.
// Parses the raw text output because --format with -v does not expose per-image fields.
func enrichImagesWithDiskUsage(images []Image) {
	output, err := dockerOutput("system", "df", "-v")
	if err != nil {
		return
	}

	lookup := parseImagesDiskUsage(string(output))
	for i := range images {
		key := images[i].Repository + ":" + images[i].Tag
		if info, ok := lookup[key]; ok {
			images[i].UniqueSize = info.UniqueSize
			images[i].Containers = info.Containers
		}
	}
}

// parseImagesDiskUsage extracts the "Images space usage" table from
// `docker system df -v`, keyed by "repository:tag".
//
// The table is fixed-width rather than delimited, so column offsets are taken
// from the header line and every row is sliced at those offsets.
func parseImagesDiskUsage(output string) map[string]dfInfo {
	lookup := make(map[string]dfInfo)

	inImages := false
	headerFound := false
	var tagPos, uniquePos, contPos int

	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "Images space usage:") {
			inImages = true
			continue
		}
		if inImages && (strings.HasPrefix(trimmed, "Containers space usage:") ||
			strings.HasPrefix(trimmed, "Local Volumes space usage:") ||
			strings.HasPrefix(trimmed, "Build cache usage:")) {
			break
		}
		if !inImages || trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "REPOSITORY") {
			tagPos = strings.Index(line, "TAG")
			uniquePos = strings.Index(line, "UNIQUE SIZE")
			contPos = strings.Index(line, "CONTAINERS")
			headerFound = tagPos > 0 && uniquePos > 0 && contPos > 0
			continue
		}
		if !headerFound {
			continue
		}

		repo := strings.TrimSpace(safeSlice(line, 0, tagPos))
		// The TAG column is short; take the first word so it cannot bleed into IMAGE ID.
		tag := firstWord(strings.TrimSpace(safeSlice(line, tagPos, tagPos+tagColumnWidth)))
		uniqueStr := strings.TrimSpace(safeSlice(line, uniquePos, contPos))
		contStr := firstWord(strings.TrimSpace(safeSlice(line, contPos, len(line))))

		if repo == "" || tag == "" {
			continue
		}
		containers, _ := strconv.Atoi(contStr)
		lookup[repo+":"+tag] = dfInfo{UniqueSize: parseSize(uniqueStr), Containers: containers}
	}

	return lookup
}

// tagColumnWidth is how far past the TAG offset a tag may run before the next
// column starts. Docker does not pad this column to a documented width, so the
// value is a heuristic wide enough for realistic tags.
const tagColumnWidth = 20

// firstWord returns s up to its first space.
func firstWord(s string) string {
	if sp := strings.IndexByte(s, ' '); sp > 0 {
		return s[:sp]
	}
	return s
}

// RemoveImage removes a Docker image by ID. If force is true, uses -f flag.
func RemoveImage(id string, force bool) error {
	args := []string{"rmi", id}
	if force {
		args = []string{"rmi", "-f", id}
	}
	return mutate("docker rmi", args...)
}

// PruneImages removes all dangling (unused) images and returns the output
func PruneImages() (string, error) {
	return prune("docker image prune", "image", "prune", "-f")
}

// PullImage pulls a Docker image from a registry.
func PullImage(imageName string) error {
	if err := requireDocker(); err != nil {
		return err
	}
	return mutate("docker pull", "pull", imageName)
}

// GetImageExposedPorts returns the container port specs declared by EXPOSE in an image.
// Each entry uses the "port/protocol" format, e.g. "80/tcp" or "53/udp".
func GetImageExposedPorts(imageName string) ([]string, error) {
	if err := requireDocker(); err != nil {
		return nil, err
	}
	output, err := dockerOutput("image", "inspect", "--format", "{{json .Config.ExposedPorts}}", imageName)
	if err != nil {
		return nil, wrapErr("docker image inspect", err)
	}
	return parseExposedPorts(string(output)), nil
}

// parseExposedPorts pulls the port specs out of an ExposedPorts JSON object,
// which looks like {"80/tcp":{},"443/tcp":{}}.
func parseExposedPorts(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "null" || raw == "" {
		return nil
	}
	raw = strings.TrimSuffix(strings.TrimPrefix(raw, "{"), "}")
	if raw == "" {
		return nil
	}

	var ports []string
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if idx := strings.Index(part, ":"); idx > 0 {
			if portSpec := strings.Trim(part[:idx], "\""); portSpec != "" {
				ports = append(ports, portSpec)
			}
		}
	}
	return ports
}
