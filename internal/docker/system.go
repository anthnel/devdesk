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
	// for networks: they hold no bytes, so the disk report ignores them. Il est
	// compté par un `docker network ls` séparé, et c'est le seul chiffre de
	// cette structure qui coûte un second appel.
	NetworksCount int

	// BuildCacheSize is the fourth row `docker system df` prints, and the one
	// that answers "where did the disk go" most often — un cache de build
	// atteint couramment des dizaines de gigaoctets. Il entrait déjà dans la
	// somme du récupérable ; seule sa taille propre était jetée.
	BuildCacheSize string

	// Reclaimable is what `docker system df` already reports and this package
	// used to drop on the floor: la place qu'un prune rendrait, sommée sur les
	// trois familles. Docker la donne sous forme "12.3GB (45%)" — seule la
	// taille est gardée, le pourcentage est relatif à sa propre famille et
	// n'aurait pas de sens une fois additionné.
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
			// "12.3GB (45%)" — le pourcentage est relatif à sa propre famille
			// et ne survivrait pas à une addition.
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

	// Une liste qui échoue laisse le compte à zéro plutôt que de faire échouer
	// tout l'appel : les tailles viennent d'être lues et valent d'être rendues.
	if networks, err := ListNetworks(); err == nil {
		stats.NetworksCount = len(networks)
	}

	return stats
}
