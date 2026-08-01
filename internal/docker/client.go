package docker

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Container represents a Docker container with its metrics
type Container struct {
	ID         string
	Name       string
	Image      string
	State      string // running, exited, paused, created, restarting, dead
	Status     string // "Up 2 hours", "Exited (0) 5min ago"
	CreatedAt  string // "2 hours ago"
	Ports      string
	CPUPercent float64
	MemUsage   string // "150MiB / 8GiB"
	MemPercent float64
	NetIO      string // "1.2kB / 3.4kB"
	NetRX      int64  // received bytes (parsed from NetIO)
	NetTX      int64  // transmitted bytes (parsed from NetIO)
	BlockIO    string // "1.2MB / 3.4MB"
	BlockRX    int64  // block bytes read (parsed from BlockIO)
	BlockTX    int64  // block bytes written (parsed from BlockIO)
}

// ListContainers returns a list of containers. If all is true, includes stopped containers.
func ListContainers(all bool) ([]Container, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found: %w", err)
	}

	args := []string{"ps", "--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.State}}\t{{.Status}}\t{{.CreatedAt}}\t{{.Ports}}"}
	if all {
		args = append(args[:1], append([]string{"--all"}, args[1:]...)...)
	}

	cmd := exec.Command("docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps failed: %w", err)
	}

	var containers []Container
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 7)
		if len(parts) < 4 {
			continue
		}
		c := Container{
			ID:    parts[0],
			Name:  parts[1],
			Image: parts[2],
			State: parts[3],
		}
		if len(parts) > 4 {
			c.Status = parts[4]
		}
		if len(parts) > 5 {
			c.CreatedAt = parts[5]
		}
		if len(parts) > 6 {
			c.Ports = parts[6]
		}
		containers = append(containers, c)
	}

	return containers, nil
}

// GetContainerMetrics returns CPU/Memory/Net metrics for running containers
func GetContainerMetrics() (map[string]Container, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found: %w", err)
	}

	cmd := exec.Command("docker", "stats", "--no-stream", "--format", "{{.ID}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.BlockIO}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker stats failed: %w", err)
	}

	metrics := make(map[string]Container)
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 5 {
			continue
		}

		cpuStr := strings.TrimSuffix(parts[1], "%")
		cpu, _ := strconv.ParseFloat(cpuStr, 64)

		memStr := strings.TrimSuffix(parts[3], "%")
		mem, _ := strconv.ParseFloat(memStr, 64)

		netRX, netTX := parseNetIO(parts[4])
		var blockIO string
		var blockRX, blockTX int64
		if len(parts) >= 6 {
			blockIO = parts[5]
			blockRX, blockTX = parseNetIO(blockIO)
		}
		metrics[parts[0]] = Container{
			ID:         parts[0],
			CPUPercent: cpu,
			MemUsage:   parts[2],
			MemPercent: mem,
			NetIO:      parts[4],
			NetRX:      netRX,
			NetTX:      netTX,
			BlockIO:    blockIO,
			BlockRX:    blockRX,
			BlockTX:    blockTX,
		}
	}

	return metrics, nil
}

// StopContainer stops a container by ID
func StopContainer(id string) error {
	cmd := exec.Command("docker", "stop", id)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker stop failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// RestartContainer restarts a container by ID
func RestartContainer(id string) error {
	cmd := exec.Command("docker", "restart", id)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker restart failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// PauseContainer pauses a running container by ID
func PauseContainer(id string) error {
	cmd := exec.Command("docker", "pause", id)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker pause failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// UnpauseContainer resumes a paused container by ID
func UnpauseContainer(id string) error {
	cmd := exec.Command("docker", "unpause", id)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker unpause failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// RemoveContainer removes a container by ID. If force is true, uses -f flag.
func RemoveContainer(id string, force bool) error {
	args := []string{"rm", id}
	if force {
		args = []string{"rm", "-f", id}
	}
	cmd := exec.Command("docker", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker rm failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// GetContainerLogs returns the last N lines of logs for a container.
// When timestamps is true, each line is prefixed with its RFC3339Nano timestamp.
func GetContainerLogs(id string, tail int, timestamps bool) (string, error) {
	args := []string{"logs", "--tail", strconv.Itoa(tail)}
	if timestamps {
		args = append(args, "--timestamps")
	}
	args = append(args, id)
	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker logs failed: %s", strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

// OCIStats contains OCI resource counts and disk usage
type OCIStats struct {
	Available       bool
	ImagesCount     int
	ImagesSize      string
	ContainersCount int
	ContainersSize  string
	VolumesCount    int
	VolumesSize     string
}

// FetchOCIStats returns OCI resource counts and disk usage via docker system df
func FetchOCIStats() OCIStats {
	stats := OCIStats{}

	if _, err := exec.LookPath("docker"); err != nil {
		return stats
	}

	cmd := exec.Command("docker", "system", "df", "--format", "{{.Type}}\t{{.TotalCount}}\t{{.Size}}")
	output, err := cmd.Output()
	if err != nil {
		return stats
	}

	stats.Available = true

	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		typeName := parts[0]
		count, _ := strconv.Atoi(parts[1])
		size := parts[2]

		switch typeName {
		case "Images":
			stats.ImagesCount = count
			stats.ImagesSize = size
		case "Containers":
			stats.ContainersCount = count
			stats.ContainersSize = size
		case "Local Volumes":
			stats.VolumesCount = count
			stats.VolumesSize = size
		}
	}

	return stats
}

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
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found: %w", err)
	}

	// {{.Size}} from docker image ls gives disk usage (not content size)
	cmd := exec.Command("docker", "image", "ls", "--format", "{{.ID}}\t{{.Repository}}\t{{.Tag}}\t{{.Size}}\t{{.CreatedAt}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker image ls failed: %w", err)
	}

	var images []Image
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
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

	// Collect all image IDs
	ids := make([]string, 0, len(images))
	for _, img := range images {
		ids = append(ids, img.ID)
	}

	// docker image inspect --format '{{.ID}} {{.Size}}' id1 id2 ...
	args := append([]string{"image", "inspect", "--format", "{{.ID}}\t{{.Size}}"}, ids...)
	cmd := exec.Command("docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return
	}

	// Build lookup by short ID (strip "sha256:" prefix, keep first 12 chars)
	lookup := make(map[string]int64)
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		fullID := strings.TrimPrefix(parts[0], "sha256:")
		shortID := fullID
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

// enrichImagesWithDiskUsage populates UniqueSize and Containers from docker system df -v.
// Parses the raw text output because --format with -v does not expose per-image fields.
func enrichImagesWithDiskUsage(images []Image) {
	cmd := exec.Command("docker", "system", "df", "-v")
	output, err := cmd.Output()
	if err != nil {
		return
	}

	type dfInfo struct {
		UniqueSize int64
		Containers int
	}
	lookup := make(map[string]dfInfo)

	lines := strings.Split(string(output), "\n")
	inImages := false
	// Column start positions derived from the header line
	var tagPos, uniquePos, contPos int
	headerFound := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect the Images section
		if strings.HasPrefix(trimmed, "Images space usage:") {
			inImages = true
			continue
		}
		// End of Images section
		if inImages && (strings.HasPrefix(trimmed, "Containers space usage:") ||
			strings.HasPrefix(trimmed, "Local Volumes space usage:") ||
			strings.HasPrefix(trimmed, "Build cache usage:")) {
			break
		}
		if !inImages || trimmed == "" {
			continue
		}

		// Parse the header line to discover column positions
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

		// Extract fields using fixed column positions from the header
		repo := strings.TrimSpace(safeSlice(line, 0, tagPos))
		tag := strings.TrimSpace(safeSlice(line, tagPos, tagPos+20)) // TAG column is short
		// Clean tag: take first word only (avoids bleeding into IMAGE ID)
		if sp := strings.IndexByte(tag, ' '); sp > 0 {
			tag = tag[:sp]
		}
		uniqueStr := strings.TrimSpace(safeSlice(line, uniquePos, contPos))
		contStr := strings.TrimSpace(safeSlice(line, contPos, len(line)))
		// Containers count is the first word of the remaining text
		if sp := strings.IndexByte(contStr, ' '); sp > 0 {
			contStr = contStr[:sp]
		}

		if repo == "" || tag == "" {
			continue
		}

		key := repo + ":" + tag
		containers, _ := strconv.Atoi(contStr)
		lookup[key] = dfInfo{UniqueSize: parseSize(uniqueStr), Containers: containers}
	}

	for i := range images {
		key := images[i].Repository + ":" + images[i].Tag
		if info, ok := lookup[key]; ok {
			images[i].UniqueSize = info.UniqueSize
			images[i].Containers = info.Containers
		}
	}
}

// safeSlice returns s[start:end] clamped to valid bounds
func safeSlice(s string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if start >= len(s) {
		return ""
	}
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}

// parseNetIO parses Docker NetIO string "1.2kB / 3.4kB" into received and transmitted bytes
func parseNetIO(netIO string) (rx, tx int64) {
	parts := strings.SplitN(netIO, "/", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	return parseSize(strings.TrimSpace(parts[0])), parseSize(strings.TrimSpace(parts[1]))
}

// parseSize parses Docker human-readable sizes like "1.5GB", "256MB", "10.2kB" into bytes
func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	multipliers := []struct {
		suffix string
		mult   float64
	}{
		{"GB", 1e9},
		{"MB", 1e6},
		{"kB", 1e3},
		{"B", 1},
	}

	for _, m := range multipliers {
		if numStr, ok := strings.CutSuffix(s, m.suffix); ok {
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0
			}
			return int64(num * m.mult)
		}
	}
	return 0
}

// RemoveImage removes a Docker image by ID. If force is true, uses -f flag.
func RemoveImage(id string, force bool) error {
	args := []string{"rmi", id}
	if force {
		args = []string{"rmi", "-f", id}
	}
	cmd := exec.Command("docker", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker rmi failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// PruneImages removes all dangling (unused) images and returns the output
// PruneContainers removes all stopped containers
func PruneContainers() (string, error) {
	cmd := exec.Command("docker", "container", "prune", "-f")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker container prune failed: %s", strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func PruneImages() (string, error) {
	cmd := exec.Command("docker", "image", "prune", "-f")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker image prune failed: %s", strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// ContainerStats contains Docker container counts
type ContainerStats struct {
	Available bool
	Running   int
	Stopped   int
	Paused    int
}

// FetchContainerStats queries Docker for container status counts.
// Uses the docker CLI to avoid importing the heavy Docker SDK.
func FetchContainerStats() ContainerStats {
	stats := ContainerStats{}

	// Check if docker is available
	if _, err := exec.LookPath("docker"); err != nil {
		return stats
	}

	// Get all containers with their status
	cmd := exec.Command("docker", "ps", "-a", "--format", "{{.State}}")
	output, err := cmd.Output()
	if err != nil {
		return stats
	}

	stats.Available = true

	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		switch line {
		case "running":
			stats.Running++
		case "paused":
			stats.Paused++
		default:
			// exited, created, restarting, removing, dead
			stats.Stopped++
		}
	}

	return stats
}

// Network represents a Docker network
type Network struct {
	ID      string
	Name    string
	Driver  string
	Scope   string
	Created string
}

// ListNetworks returns a list of Docker networks
func ListNetworks() ([]Network, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found: %w", err)
	}
	cmd := exec.Command("docker", "network", "ls", "--format", "{{.ID}}\t{{.Name}}\t{{.Driver}}\t{{.Scope}}\t{{.CreatedAt}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker network ls failed: %w", err)
	}
	var networks []Network
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 4 {
			continue
		}
		n := Network{ID: parts[0], Name: parts[1], Driver: parts[2], Scope: parts[3]}
		if len(parts) > 4 {
			n.Created = parts[4]
		}
		networks = append(networks, n)
	}
	return networks, nil
}

// CreateNetwork creates a Docker network with the given name and driver
func CreateNetwork(name, driver string) error {
	if driver == "" {
		driver = "bridge"
	}
	cmd := exec.Command("docker", "network", "create", "--driver", driver, name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker network create failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// RemoveNetwork removes a Docker network by ID or name
func RemoveNetwork(id string) error {
	cmd := exec.Command("docker", "network", "rm", id)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker network rm failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// PruneNetworks removes all unused Docker networks
func PruneNetworks() (string, error) {
	cmd := exec.Command("docker", "network", "prune", "-f")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker network prune failed: %s", strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// Volume represents a Docker volume
type Volume struct {
	Name       string
	Driver     string
	Mountpoint string
	Created    string
}

// ListVolumes returns a list of Docker volumes
func ListVolumes() ([]Volume, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found: %w", err)
	}
	cmd := exec.Command("docker", "volume", "ls", "--format", "{{.Name}}\t{{.Driver}}\t{{.Mountpoint}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls failed: %w", err)
	}
	var volumes []Volume
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		v := Volume{Name: parts[0], Driver: parts[1]}
		if len(parts) > 2 {
			v.Mountpoint = parts[2]
		}
		volumes = append(volumes, v)
	}
	return volumes, nil
}

// CreateVolume creates a Docker volume with the given name and optional driver
func CreateVolume(name, driver string) error {
	args := []string{"volume", "create"}
	if driver != "" {
		args = append(args, "--driver", driver)
	}
	args = append(args, name)
	cmd := exec.Command("docker", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker volume create failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// RemoveVolume removes a Docker volume by name
func RemoveVolume(name string) error {
	cmd := exec.Command("docker", "volume", "rm", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker volume rm failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// PruneVolumes removes all unused Docker volumes
func PruneVolumes() (string, error) {
	cmd := exec.Command("docker", "volume", "prune", "-f")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker volume prune failed: %s", strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// ContainerLaunchOptions holds parameters for running a container
type ContainerLaunchOptions struct {
	Name        string   // container name (optional)
	Image       string   // image to run
	Entrypoint  string   // override default entrypoint (optional)
	Ports       []string // "hostPort:containerPort"
	Env         []string // "KEY=VALUE"
	Volumes     []string // "name:/path"
	Network     string   // network name
	User        string   // user to run as (--user UID:GID or username, optional)
	Detach      bool     // run in background (-d)
	Remove      bool     // automatically remove the container when it exits (--rm)
	Interactive bool     // keep STDIN open even if not attached (-i)
	TTY         bool     // allocate a pseudo-TTY (-t)
}

// buildLaunchArgs constructs the docker run argument list for the given options.
func buildLaunchArgs(opts ContainerLaunchOptions) []string {
	args := []string{"run"}
	if opts.Remove {
		args = append(args, "--rm")
	}
	if opts.Detach {
		args = append(args, "-d")
	}
	if opts.Interactive {
		args = append(args, "-i")
	}
	if opts.TTY {
		args = append(args, "-t")
	}
	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}
	if opts.Entrypoint != "" {
		args = append(args, "--entrypoint", opts.Entrypoint)
	}
	for _, p := range opts.Ports {
		if p != "" {
			args = append(args, "-p", p)
		}
	}
	for _, e := range opts.Env {
		if e != "" {
			args = append(args, "-e", e)
		}
	}
	for _, v := range opts.Volumes {
		if v != "" {
			args = append(args, "-v", v)
		}
	}
	if opts.Network != "" {
		args = append(args, "--network", opts.Network)
	}
	if opts.User != "" {
		args = append(args, "--user", opts.User)
	}
	args = append(args, opts.Image)
	return args
}

// BuildLaunchCmd returns a ready-to-run exec.Cmd for the given options without executing it.
// Use this when you need to hand off the process to a terminal (e.g. tea.ExecProcess for -it mode).
func BuildLaunchCmd(opts ContainerLaunchOptions) (*exec.Cmd, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found: %w", err)
	}
	return exec.Command("docker", buildLaunchArgs(opts)...), nil
}

// LaunchContainer runs a new container with the given options and returns its ID/output.
// Do NOT use this for interactive (-it) containers — use BuildLaunchCmd + tea.ExecProcess instead.
func LaunchContainer(opts ContainerLaunchOptions) (string, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return "", fmt.Errorf("docker not found: %w", err)
	}
	cmd := exec.Command("docker", buildLaunchArgs(opts)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker run failed: %s", strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// VerifyEntrypoint checks if the given binary is available inside an image
// by running a short-lived throwaway container with /bin/sh -c "command -v <binary>".
// Returns true if the binary is found, false otherwise.
func VerifyEntrypoint(image, entrypoint string) (bool, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return false, fmt.Errorf("docker not found: %w", err)
	}
	cmd := exec.Command("docker", "run", "--rm", "--entrypoint", "/bin/sh",
		image, "-c", "command -v "+entrypoint)
	err := cmd.Run()
	return err == nil, nil
}

// PullImage pulls a Docker image from a registry.
func PullImage(imageName string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found: %w", err)
	}
	cmd := exec.Command("docker", "pull", imageName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker pull failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// RegistryLogin authenticates with a Docker/OCI registry using `docker login`.
// The password is passed via stdin to avoid exposing it in the process list.
func RegistryLogin(registryURL, username, password string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found: %w", err)
	}
	cmd := exec.Command("docker", "login", registryURL, "-u", username, "--password-stdin")
	cmd.Stdin = strings.NewReader(password)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker login failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// dockerHubKeys lists all keys Docker uses for Docker Hub in ~/.docker/config.json.
// `docker login docker.io` stores credentials under "https://index.docker.io/v1/".
var dockerHubKeys = []string{
	"docker.io",
	"registry-1.docker.io",
	"https://index.docker.io/v1/",
	"https://index.docker.io/v1",
	"https://registry-1.docker.io",
}

// registryCandidates returns all URL variants to look up in ~/.docker/config.json.
func registryCandidates(registryURL string) []string {
	base := strings.TrimSuffix(registryURL, "/")

	// Check if this URL is any known Docker Hub alias; if so, return the full set.
	for _, alias := range dockerHubKeys {
		if strings.EqualFold(base, strings.TrimSuffix(alias, "/")) {
			return dockerHubKeys
		}
	}

	// Generic: bare hostname, https://, http:// variants.
	candidates := []string{base}
	if strings.HasPrefix(base, "https://") {
		candidates = append(candidates, strings.TrimPrefix(base, "https://"))
	} else if strings.HasPrefix(base, "http://") {
		candidates = append(candidates, strings.TrimPrefix(base, "http://"))
	} else {
		candidates = append(candidates, "https://"+base, "http://"+base)
	}
	return candidates
}

// IsRegistryLoggedIn reports whether stored credentials exist for registryURL
// by inspecting ~/.docker/config.json.
func IsRegistryLoggedIn(registryURL string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(home, ".docker", "config.json"))
	if err != nil {
		return false
	}
	var cfg struct {
		Auths map[string]json.RawMessage `json:"auths"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false
	}
	for _, c := range registryCandidates(registryURL) {
		if _, ok := cfg.Auths[c]; ok {
			return true
		}
	}
	return false
}

// RegistryLogout removes stored credentials for a Docker/OCI registry.
// It runs `docker logout` (cleans system credential stores) then directly
// removes all matching keys from ~/.docker/config.json to handle cases where
// docker logout leaves behind alias entries (e.g. docker.io vs https://index.docker.io/v1/).
func RegistryLogout(registryURL string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found: %w", err)
	}
	cmd := exec.Command("docker", "logout", registryURL)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker logout failed: %s", strings.TrimSpace(string(output)))
	}
	// Also remove all matching alias keys directly from config.json.
	_ = removeFromDockerConfig(registryURL)
	return nil
}

// removeFromDockerConfig removes all URL variants of registryURL from the auths
// section of ~/.docker/config.json. Errors are non-fatal (best effort).
func removeFromDockerConfig(registryURL string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configPath := filepath.Join(home, ".docker", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	authsRaw, ok := raw["auths"]
	if !ok {
		return nil
	}
	var auths map[string]json.RawMessage
	if err := json.Unmarshal(authsRaw, &auths); err != nil {
		return err
	}

	changed := false
	for _, candidate := range registryCandidates(registryURL) {
		if _, exists := auths[candidate]; exists {
			delete(auths, candidate)
			changed = true
		}
	}
	if !changed {
		return nil
	}

	updated, err := json.Marshal(auths)
	if err != nil {
		return err
	}
	raw["auths"] = updated
	out, err := json.MarshalIndent(raw, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, out, 0600)
}

// GetStoredCreds retrieves Docker credentials for registryURL from
// ~/.docker/config.json. It tries the per-registry credential helper
// (credHelpers), then the global credsStore, and finally falls back to the
// inline base64-encoded auth field. Returns ok=false when no credentials are
// found.
func GetStoredCreds(registryURL string) (username, password string, ok bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", false
	}
	data, err := os.ReadFile(filepath.Join(home, ".docker", "config.json"))
	if err != nil {
		return "", "", false
	}

	var cfg struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
		CredsStore  string            `json:"credsStore"`
		CredHelpers map[string]string `json:"credHelpers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", "", false
	}

	// Normalise the URL to a bare hostname for helper lookups.
	hostname := strings.TrimSuffix(registryURL, "/")
	for _, prefix := range []string{"https://", "http://"} {
		hostname = strings.TrimPrefix(hostname, prefix)
	}

	helper := cfg.CredHelpers[hostname]
	if helper == "" {
		helper = cfg.CredsStore
	}
	if helper != "" {
		if u, p, ok := getCredsFromHelper(helper, hostname); ok {
			return u, p, true
		}
	}

	for _, candidate := range registryCandidates(registryURL) {
		if entry, ok := cfg.Auths[candidate]; ok && entry.Auth != "" {
			decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
			if err != nil {
				continue
			}
			if idx := strings.IndexByte(string(decoded), ':'); idx >= 0 {
				return string(decoded[:idx]), string(decoded[idx+1:]), true
			}
		}
	}
	return "", "", false
}

// getCredsFromHelper calls `docker-credential-<helper> get` and parses the JSON response.
func getCredsFromHelper(helper, serverURL string) (username, password string, ok bool) {
	cmd := exec.Command("docker-credential-"+helper, "get") //nolint:gosec
	cmd.Stdin = strings.NewReader(serverURL)
	out, err := cmd.Output()
	if err != nil {
		return "", "", false
	}
	var result struct {
		Username string `json:"Username"`
		Secret   string `json:"Secret"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", "", false
	}
	if result.Secret == "" {
		return "", "", false
	}
	return result.Username, result.Secret, true
}

// RegistryAlias pairs a URL prefix with a short alias for display purposes.
type RegistryAlias struct {
	URL   string
	Alias string
}

// ApplyAliases replaces registry URL prefixes in an image name with their configured alias.
// Example: URL="gitlab.com/my-group", Alias="gl" turns
// "gitlab.com/my-group/my-image:1.0" into "gl/my-image:1.0".
func ApplyAliases(imageName string, aliases []RegistryAlias) string {
	for _, a := range aliases {
		if a.Alias == "" || a.URL == "" {
			continue
		}
		prefix := strings.TrimSuffix(a.URL, "/") + "/"
		if strings.HasPrefix(imageName, prefix) {
			return a.Alias + "/" + strings.TrimPrefix(imageName, prefix)
		}
	}
	return imageName
}

// NetworkContainer represents a container connected to a Docker network
type NetworkContainer struct {
	Name       string
	IPv4       string
	MacAddress string
}

// networkInspectContainer is used for JSON parsing of docker network inspect output
type networkInspectContainer struct {
	Name        string `json:"Name"`
	MacAddress  string `json:"MacAddress"`
	IPv4Address string `json:"IPv4Address"`
}

// networkInspectResult is the top-level JSON structure from docker network inspect
type networkInspectResult struct {
	Containers map[string]networkInspectContainer `json:"Containers"`
}

// InspectNetwork returns the containers connected to a Docker network by ID or name.
func InspectNetwork(id string) ([]NetworkContainer, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found: %w", err)
	}
	cmd := exec.Command("docker", "network", "inspect", id)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker network inspect failed: %w", err)
	}
	var results []networkInspectResult
	if err := json.Unmarshal(output, &results); err != nil {
		return nil, fmt.Errorf("failed to parse network inspect output: %w", err)
	}
	if len(results) == 0 {
		return nil, nil
	}
	var containers []NetworkContainer
	for _, c := range results[0].Containers {
		ip := c.IPv4Address
		if idx := strings.Index(ip, "/"); idx > 0 {
			ip = ip[:idx]
		}
		containers = append(containers, NetworkContainer{
			Name:       c.Name,
			IPv4:       ip,
			MacAddress: c.MacAddress,
		})
	}
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Name < containers[j].Name
	})
	return containers, nil
}

// RunDiagnosticContainer runs a command in an ephemeral container attached to a Docker network.
// The image must include ping, curl, and nc (netcat). Container is removed after execution (--rm).
func RunDiagnosticContainer(networkID, image string, command []string) (string, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return "", fmt.Errorf("docker not found: %w", err)
	}
	args := []string{"run", "--rm", "--network", networkID, image}
	args = append(args, command...)
	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(output))
	if err != nil {
		return out, fmt.Errorf("diagnostic command failed: %w", err)
	}
	return out, nil
}

// GetImageExposedPorts returns the container port specs declared by EXPOSE in an image.
// Each entry uses the "port/protocol" format, e.g. "80/tcp" or "53/udp".
func GetImageExposedPorts(imageName string) ([]string, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found: %w", err)
	}
	cmd := exec.Command("docker", "image", "inspect", "--format", "{{json .Config.ExposedPorts}}", imageName)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker image inspect failed: %w", err)
	}
	raw := strings.TrimSpace(string(output))
	if raw == "null" || raw == "" {
		return nil, nil
	}
	// raw looks like: {"80/tcp":{},"443/tcp":{}}
	raw = strings.TrimPrefix(raw, "{")
	raw = strings.TrimSuffix(raw, "}")
	if raw == "" {
		return nil, nil
	}
	var ports []string
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if idx := strings.Index(part, ":"); idx > 0 {
			portSpec := strings.Trim(part[:idx], "\"") // e.g. "80/tcp"
			if portSpec != "" {
				ports = append(ports, portSpec)
			}
		}
	}
	return ports, nil
}
