package docker

import (
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
	if err := requireDocker(); err != nil {
		return nil, err
	}

	args := []string{"ps"}
	if all {
		args = append(args, "--all")
	}
	args = append(args, "--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.State}}\t{{.Status}}\t{{.CreatedAt}}\t{{.Ports}}")

	output, err := dockerOutput(args...)
	if err != nil {
		return nil, wrapErr("docker ps", err)
	}

	var containers []Container
	for _, line := range splitLines(output) {
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
	if err := requireDocker(); err != nil {
		return nil, err
	}

	output, err := dockerOutput("stats", "--no-stream", "--format",
		"{{.ID}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.BlockIO}}")
	if err != nil {
		return nil, wrapErr("docker stats", err)
	}

	metrics := make(map[string]Container)
	for _, line := range splitLines(output) {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 5 {
			continue
		}

		cpu, _ := strconv.ParseFloat(strings.TrimSuffix(parts[1], "%"), 64)
		mem, _ := strconv.ParseFloat(strings.TrimSuffix(parts[3], "%"), 64)
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
	return mutate("docker stop", "stop", id)
}

// RestartContainer restarts a container by ID
func RestartContainer(id string) error {
	return mutate("docker restart", "restart", id)
}

// PauseContainer pauses a running container by ID
func PauseContainer(id string) error {
	return mutate("docker pause", "pause", id)
}

// UnpauseContainer resumes a paused container by ID
func UnpauseContainer(id string) error {
	return mutate("docker unpause", "unpause", id)
}

// RemoveContainer removes a container by ID. If force is true, uses -f flag.
func RemoveContainer(id string, force bool) error {
	args := []string{"rm", id}
	if force {
		args = []string{"rm", "-f", id}
	}
	return mutate("docker rm", args...)
}

// PruneContainers removes all stopped containers
func PruneContainers() (string, error) {
	return prune("docker container prune", "container", "prune", "-f")
}

// GetContainerLogs returns the last N lines of logs for a container.
// When timestamps is true, each line is prefixed with its RFC3339Nano timestamp.
func GetContainerLogs(id string, tail int, timestamps bool) (string, error) {
	args := []string{"logs", "--tail", strconv.Itoa(tail)}
	if timestamps {
		args = append(args, "--timestamps")
	}
	args = append(args, id)

	output, err := dockerCombined(args...)
	if err != nil {
		return "", errWithOutput("docker logs", output)
	}
	return string(output), nil
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

	if runner.LookPath() != nil {
		return stats
	}

	output, err := dockerOutput("ps", "-a", "--format", "{{.State}}")
	if err != nil {
		return stats
	}

	stats.Available = true

	for _, line := range splitLines(output) {
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
