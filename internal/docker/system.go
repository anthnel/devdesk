package docker

import (
	"strconv"
	"strings"
)

// OCIStats contains OCI resource counts and disk usage
type OCIStats struct {
	Available       bool
	ImagesCount     int
	ImagesSize      string
	ContainersCount int
	ContainersSize  string
	VolumesCount    int
	VolumesSize     string

	// NetworksCount does **not** come from `docker system df`, which has no row
	// for networks: they hold no bytes, so the disk report ignores them. It is
	// counted by a separate `docker network ls`, and it is the only figure in
	// this struct that costs a second call.
	NetworksCount int

	// BuildCacheSize is the fourth row `docker system df` prints, and the one
	// that answers "where did the disk go" most often — a build cache commonly
	// reaches tens of gigabytes. It was already part of the reclaimable sum;
	// only its own size used to be discarded.
	BuildCacheSize string

	// Reclaimable is what `docker system df` already reports and this package
	// used to drop on the floor: the space a prune would free, summed across
	// the three families. Docker gives it as "12.3GB (45%)" — only the size
	// is kept, the percentage is relative to its own family and would make no
	// sense once added up.
	Reclaimable string
}

// FetchOCIStats returns OCI resource counts and disk usage via docker system df
func FetchOCIStats() OCIStats {
	stats := OCIStats{}

	if runner.LookPath() != nil {
		return stats
	}

	output, err := dockerOutput("system", "df", "--format",
		"{{.Type}}\t{{.TotalCount}}\t{{.Size}}\t{{.Reclaimable}}")
	if err != nil {
		return stats
	}

	stats.Available = true
	var reclaimable int64

	for _, line := range splitLines(output) {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 3 {
			continue
		}
		count, _ := strconv.Atoi(parts[1])
		size := parts[2]
		if len(parts) >= 4 {
			// "12.3GB (45%)" — the percentage is relative to its own family
			// and would not survive an addition.
			reclaimable += parseSize(strings.TrimSpace(strings.Split(parts[3], "(")[0]))
		}

		switch parts[0] {
		case "Images":
			stats.ImagesCount = count
			stats.ImagesSize = size
		case "Containers":
			stats.ContainersCount = count
			stats.ContainersSize = size
		case "Local Volumes":
			stats.VolumesCount = count
			stats.VolumesSize = size
		case "Build Cache":
			stats.BuildCacheSize = size
		}
	}

	if reclaimable > 0 {
		stats.Reclaimable = formatSize(reclaimable)
	}

	// A list that fails leaves the count at zero rather than failing the
	// whole call: the sizes were just read and are worth returning.
	if networks, err := ListNetworks(); err == nil {
		stats.NetworksCount = len(networks)
	}

	return stats
}
