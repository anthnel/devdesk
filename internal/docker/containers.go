package docker

import (
	"fmt"
	"log"
	"strconv"
	"strings"
)

// Container represents a Docker container with its metrics
type Container struct {
	ID        string
	Name      string
	Image     string
	State     string // running, exited, paused, created, restarting, dead
	Status    string // "Up 2 hours", "Exited (0) 5min ago"
	CreatedAt string // "2 hours ago"
	// Ports is parsed here rather than carried as the string docker printed:
	// the view must not learn to read Docker's output, and an opaque string
	// cannot be searched by port number. See ParseContainerPorts.
	Ports      []PortBinding
	CPUPercent float64
	MemUsage   string // "150MiB / 8GiB"
	// MemBytes is the used half of MemUsage. It exists because MemPercent
	// cannot be aggregated: it is a share of *this* container's limit, so two
	// of them are fractions of different wholes (see Aggregate).
	MemBytes   int64
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
	if err := requireEngine(); err != nil {
		return nil, err
	}

	args := []string{"ps"}
	if all {
		args = append(args, "--all")
	}
	args = append(args, "--format", templates().PS)

	output, err := dockerOutput(args...)
	if err != nil {
		return nil, wrapErr(cmdLabel("ps"), err)
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
			c.Ports = ParseContainerPorts(parts[6])
		}
		containers = append(containers, c)
	}

	return containers, nil
}

// GetContainerMetrics returns CPU/Memory/Net metrics for running containers
func GetContainerMetrics() (map[string]Container, error) {
	if err := requireEngine(); err != nil {
		return nil, err
	}

	output, err := dockerOutput("stats", "--no-stream", "--format", templates().Stats)
	if err != nil {
		return nil, wrapErr(cmdLabel("stats"), err)
	}

	metrics := make(map[string]Container)
	for _, line := range splitLines(output) {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 5 {
			continue
		}

		cpu, _ := strconv.ParseFloat(strings.TrimSuffix(parts[1], "%"), 64)
		mem, _ := strconv.ParseFloat(strings.TrimSuffix(parts[3], "%"), 64)
		memBytes, _ := parsePair(parts[2])
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
			MemBytes:   memBytes,
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

// Aggregate is what every running container adds up to, **on the daemon's
// scale**: both percentages run from 0 to 100, whatever the machine.
//
// That normalization is the whole of this type, and it exists because neither
// figure `docker stats` prints is one to begin with.
//
//   - `.CPUPerc` is relative to **one core**: Docker computes
//     `(cpuDelta/systemDelta) × onlineCPUs × 100`, so a container busy on two
//     cores reports 199% (measured, not assumed). Summed over the containers,
//     the raw total runs to `NCPU × 100` — 1400% on a sixteen-CPU daemon, which
//     is a true statement about fourteen cores and not a percentage of
//     anything. Divided by NCPU it becomes one.
//   - `.MemPerc` is relative to **that container's own limit**. Summing them
//     adds fractions of different wholes: a container capped at 256 MiB sitting
//     at 0.13% and one against the daemon's 15 GiB at 0.03% do not add up to
//     0.16% of anything. So the bytes are summed instead, over the daemon's own
//     total.
//
// It is measured *inside* the Docker VM on Windows and macOS, so it is still
// not addable with the host's numbers — it is a subset, and both dashboard
// sections say so in their title. What changes here is that the two are now at
// least in the same unit.
type Aggregate struct {
	Available  bool
	Running    int
	CPUPercent float64
	MemPercent float64

	// Cores is the daemon's CPU count, which is what CPUPercent was divided by.
	// The dashboard prints it beside the figure for the same reason the host
	// section prints its own: 40% of four cores and 40% of thirty-two do not
	// describe the same machine.
	Cores int
}

// Capacity is what the daemon has to give, and therefore the denominator every
// percentage above is taken against.
type Capacity struct {
	// Cores is `docker info`'s NCPU and MemTotal its memory, both as the
	// *daemon* sees them — which on Windows and macOS is the VM's allocation
	// and not the machine's. Reading the host's core count instead would be
	// wrong on exactly the two platforms where Docker is not the host.
	Cores    int
	MemTotal int64
}

// OK reports whether the capacity can serve as a denominator.
func (c Capacity) OK() bool { return c.Cores > 0 && c.MemTotal > 0 }

// FetchCapacity reads what the daemon has, from `docker info`.
//
// It is read on every aggregate rather than memoized, and the 240 ms it costs
// (measured, against the 1–2 s `docker stats` already spends) is what buys it:
// a Docker Desktop reconfigured with a new CPU allocation changes this number
// under a running DevDesk, and a memo would divide by the old one for the life
// of the process — silently, since a wrong denominator still produces a
// plausible percentage.
func FetchCapacity() (Capacity, error) {
	if err := requireEngine(); err != nil {
		return Capacity{}, err
	}
	output, err := dockerOutput("info", "--format", templates().Info)
	if err != nil {
		return Capacity{}, wrapErr(cmdLabel("info"), err)
	}

	fields := strings.SplitN(strings.TrimSpace(string(output)), "\t", 2)
	if len(fields) < 2 {
		return Capacity{}, fmt.Errorf("%s: unreadable capacity %q", cmdLabel("info"), output)
	}
	cores, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil {
		return Capacity{}, fmt.Errorf("%s: unreadable NCPU %q: %w", cmdLabel("info"), fields[0], err)
	}
	total, err := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
	if err != nil {
		return Capacity{}, fmt.Errorf("%s: unreadable MemTotal %q: %w", cmdLabel("info"), fields[1], err)
	}
	return Capacity{Cores: cores, MemTotal: total}, nil
}

// FetchAggregateMetrics reports what the running containers are using, as a
// share of what the daemon has.
//
// `docker stats --no-stream` costs about two seconds, measured: that is what
// earns it its own clock rather than a turn in the fast refresh.
//
// A capacity it cannot read makes the whole thing unavailable rather than a
// number on an unstated scale. That is the choice this function exists to make:
// the raw sums are not percentages, so there is nothing honest to show without
// their denominator — and `-` is a word this dashboard already has.
func FetchAggregateMetrics() Aggregate {
	metrics, err := GetContainerMetrics()
	if err != nil {
		log.Printf("ERROR [docker] aggregate metrics: %v", err)
		return Aggregate{}
	}

	capacity, err := FetchCapacity()
	if err != nil {
		log.Printf("ERROR [docker] aggregate capacity: %v", err)
		return Aggregate{}
	}
	if !capacity.OK() {
		log.Printf("ERROR [docker] aggregate capacity: daemon reports %d cores and %d bytes",
			capacity.Cores, capacity.MemTotal)
		return Aggregate{}
	}

	return aggregate(metrics, capacity)
}

// aggregate is the arithmetic on its own, so a test can hand it readings rather
// than a daemon.
func aggregate(metrics map[string]Container, capacity Capacity) Aggregate {
	agg := Aggregate{Available: true, Running: len(metrics), Cores: capacity.Cores}

	var memBytes int64
	for _, c := range metrics {
		agg.CPUPercent += c.CPUPercent
		memBytes += c.MemBytes
	}
	agg.CPUPercent /= float64(capacity.Cores)
	agg.MemPercent = float64(memBytes) / float64(capacity.MemTotal) * 100
	return agg
}

// StopContainer stops a container by ID
func StopContainer(id string) error {
	return mutate(cmdLabel("stop"), "stop", id)
}

// RestartContainer restarts a container by ID
func RestartContainer(id string) error {
	return mutate(cmdLabel("restart"), "restart", id)
}

// PauseContainer pauses a running container by ID
func PauseContainer(id string) error {
	return mutate(cmdLabel("pause"), "pause", id)
}

// UnpauseContainer resumes a paused container by ID
func UnpauseContainer(id string) error {
	return mutate(cmdLabel("unpause"), "unpause", id)
}

// RemoveContainer removes a container by ID. If force is true, uses -f flag.
func RemoveContainer(id string, force bool) error {
	args := []string{"rm", id}
	if force {
		args = []string{"rm", "-f", id}
	}
	return mutate(cmdLabel("rm"), args...)
}

// PruneContainers removes all stopped containers
func PruneContainers() (string, error) {
	return prune(cmdLabel("container prune"), "container", "prune", "-f")
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
		return "", errWithOutput(cmdLabel("logs"), output)
	}
	return string(output), nil
}

// InspectContainer returns the full `docker inspect` JSON for a container.
//
// The ID is validated rather than trusted: it reaches this from a table row,
// and a malformed one is a bug worth failing on rather than a string handed to
// a subprocess.
func InspectContainer(id string) ([]byte, error) {
	if !IsContainerID(id) {
		return nil, fmt.Errorf("%s failed: %q is not a container ID", cmdLabel("inspect"), id)
	}
	output, err := dockerCombined("inspect", id)
	if err != nil {
		return nil, errWithOutput(cmdLabel("inspect"), output)
	}
	return output, nil
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

	output, err := dockerOutput("ps", "-a", "--format", templates().PSState)
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
