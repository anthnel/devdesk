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
}

// FetchOCIStats returns OCI resource counts and disk usage via docker system df
func FetchOCIStats() OCIStats {
	stats := OCIStats{}

	if runner.LookPath() != nil {
		return stats
	}

	output, err := dockerOutput("system", "df", "--format", "{{.Type}}\t{{.TotalCount}}\t{{.Size}}")
	if err != nil {
		return stats
	}

	stats.Available = true

	for _, line := range splitLines(output) {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		count, _ := strconv.Atoi(parts[1])
		size := parts[2]

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
		}
	}

	return stats
}
