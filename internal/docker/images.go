package docker

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/anthnel/devdesk/internal/engine"
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
	// RepoDigests are the registry digests the engine recorded for the image
	// ("repo@sha256:…"). Empty for an image built or loaded rather than pulled:
	// nothing then says which registry content it is (§3.88).
	RepoDigests []string
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
	if err := requireEngine(); err != nil {
		return nil, err
	}

	// {{.Size}} from docker image ls gives disk usage (not content size)
	output, err := dockerOutput("image", "ls", "--format", templates().ImageLS)
	if err != nil {
		return nil, wrapErr(cmdLabel("image ls"), err)
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

// enrichImagesWithContentSize populates Size (content size) and RepoDigests from
// docker image inspect.
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

	args := append([]string{"image", "inspect", "--format", templates().ImageInspect}, ids...)
	output, err := dockerOutput(args...)
	if err != nil {
		return
	}

	lookup := parseImageInspect(output)
	for i := range images {
		if info, ok := lookup[images[i].ID]; ok {
			images[i].Size = info.size
			images[i].RepoDigests = info.repoDigests
		}
	}
}

type inspected struct {
	size        int64
	repoDigests []string
}

// parseImageInspect reads ImageInspect's lines, keyed by short ID (no "sha256:"
// prefix, first 12 characters).
func parseImageInspect(output []byte) map[string]inspected {
	lookup := make(map[string]inspected)
	for _, line := range splitLines(output) {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		shortID := strings.TrimPrefix(parts[0], "sha256:")
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		var info inspected
		info.size, _ = strconv.ParseInt(parts[1], 10, 64)
		if len(parts) == 3 {
			info.repoDigests = parseRepoDigests(parts[2])
		}
		lookup[shortID] = info
	}
	return lookup
}

// parseRepoDigests reads `{{json .RepoDigests}}`. Anything unreadable is no
// digest, which is what an image nobody pulled has anyway.
func parseRepoDigests(s string) []string {
	var digests []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &digests); err != nil {
		return nil
	}
	return digests
}

// ContainerImageDigests returns, for each container, the registry digests of
// the image it runs — the image it was created from, which a later pull of the
// same tag does not change. Keyed by the container ID as given; a container
// that is gone, or an image with no digest, is absent.
func ContainerImageDigests(containerIDs []string) map[string][]string {
	out := map[string][]string{}
	if len(containerIDs) == 0 || requireEngine() != nil {
		return out
	}
	args := append([]string{"container", "inspect", "--format", "{{.Image}}"}, containerIDs...)
	output, err := dockerOutput(args...)
	if err != nil {
		// One container removed between the list and this call fails the whole
		// inspect; the next refresh asks again.
		return out
	}
	imageOf := map[string]string{}
	var imageIDs []string
	for i, line := range splitLines(output) {
		if i >= len(containerIDs) {
			break
		}
		id := strings.TrimSpace(line)
		if id == "" {
			continue
		}
		if _, seen := imageOf[id]; !seen {
			imageIDs = append(imageIDs, id)
		}
		imageOf[containerIDs[i]] = id
	}
	if len(imageIDs) == 0 {
		return out
	}
	args = append([]string{"image", "inspect", "--format", templates().ImageInspect}, imageIDs...)
	if output, err = dockerOutput(args...); err != nil {
		return out
	}
	byImage := parseImageInspect(output)
	for container, image := range imageOf {
		short := strings.TrimPrefix(image, "sha256:")
		if len(short) > 12 {
			short = short[:12]
		}
		if info, ok := byImage[short]; ok && len(info.repoDigests) > 0 {
			out[container] = info.repoDigests
		}
	}
	return out
}

// ImageRepoDigests returns the registry digests of each image the engine holds,
// keyed by the reference asked. An image the engine does not have is absent from
// the map. One call per image: `image inspect` fails the whole call when one of
// several references is missing, and a Dockerfile's base often is.
func ImageRepoDigests(refs []string) map[string][]string {
	out := map[string][]string{}
	if requireEngine() != nil {
		return out
	}
	for _, ref := range refs {
		if _, done := out[ref]; done || ref == "" {
			continue
		}
		output, err := dockerOutput("image", "inspect", "--format", templates().ImageRepoDigests, ref)
		if err != nil {
			continue // not local: nothing to compare with
		}
		out[ref] = parseRepoDigests(string(output))
	}
	return out
}

// dfInfo holds the per-image figures scraped from `docker system df -v`.
type dfInfo struct {
	UniqueSize int64
	Containers int
}

// enrichImagesWithDiskUsage populates UniqueSize and Containers from
// `<engine> system df -v`.
//
// It parses the raw text output because --format with -v does not expose
// per-image fields — which is why this is the one parsed output with no
// template, and the one that does not carry over to another engine. The table
// is fixed-width, read at offsets taken from its header line, so an engine that
// prints a different header would have its rows sliced at the wrong columns and
// report numbers that are wrong rather than absent.
//
// Only docker is known to print it (engine.Shape.ParsesSystemDFVerbose). Under
// podman both columns stay zero, which the table renders as a dim "-": an
// absence reads as an absence, a wrong number does not.
//
// TODO(§3.67): measure `podman system df -v` and, if its table is compatible,
// let ParsesSystemDFVerbose say so.
func enrichImagesWithDiskUsage(images []Image) {
	if !engine.Current().ParsesSystemDFVerbose() {
		return
	}
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
	return mutate(cmdLabel("rmi"), args...)
}

// ContainersUsingImage names every container, running or not, created from the
// image or from an image built on it. An image one of them uses cannot be
// removed without breaking it, and `rmi` would refuse anyway (§3.88).
func ContainersUsingImage(imageID string) ([]string, error) {
	if err := requireEngine(); err != nil {
		return nil, err
	}
	output, err := dockerOutput("ps", "-a", "--filter", "ancestor="+imageID, "--format", "{{.Names}}")
	if err != nil {
		return nil, wrapErr(cmdLabel("ps"), err)
	}
	return splitLines(output), nil
}

// ImageID returns the full ID of the image a reference names locally.
func ImageID(ref string) (string, error) {
	if err := requireEngine(); err != nil {
		return "", err
	}
	output, err := dockerOutput("image", "inspect", "--format", "{{.Id}}", ref)
	if err != nil {
		return "", wrapErr(cmdLabel("image inspect"), err)
	}
	return strings.TrimSpace(string(output)), nil
}

// SameImageID reports whether two image IDs name the same image, whatever the
// form each is in: full with its "sha256:" prefix, or the 12-character short
// form `image ls` prints.
func SameImageID(a, b string) bool {
	a, b = strings.TrimPrefix(a, "sha256:"), strings.TrimPrefix(b, "sha256:")
	if a == "" || b == "" {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// PruneImages removes all dangling (unused) images and returns the output
func PruneImages() (string, error) {
	return prune(cmdLabel("image prune"), "image", "prune", "-f")
}

// PullImage pulls a Docker image from a registry.
func PullImage(imageName string) error {
	return PullImageContext(context.Background(), imageName)
}

// PullImageContext pulls a Docker image, stopping when ctx is cancelled.
//
// It is the one mutating call in this package that takes a context: a pull is
// the only one long enough for `K` to be offered on it, and cutting it leaves
// nothing behind — Docker resumes a pull by layer (jobs.KindPull.Cancellable).
func PullImageContext(ctx context.Context, imageName string) error {
	if err := requireEngine(); err != nil {
		return err
	}
	return mutateContext(ctx, cmdLabel("pull"), "pull", imageName)
}

// GetImageExposedPorts returns the container port specs declared by EXPOSE in an image.
// Each entry uses the "port/protocol" format, e.g. "80/tcp" or "53/udp".
func GetImageExposedPorts(imageName string) ([]string, error) {
	if err := requireEngine(); err != nil {
		return nil, err
	}
	output, err := dockerOutput("image", "inspect", "--format", templates().ImageExposedPorts, imageName)
	if err != nil {
		return nil, wrapErr(cmdLabel("image inspect"), err)
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
