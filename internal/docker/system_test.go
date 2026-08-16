package docker

import (
	"errors"
	"slices"
	"testing"
)

func TestFetchOCIStatsParsesEachResourceType(t *testing.T) {
	stubOutput(t, "system",
		"Images\t12\t1.5GB\n"+
			"Containers\t3\t250MB\n"+
			"Local Volumes\t7\t400MB\n"+
			"Build Cache\t20\t2GB\n")

	got := FetchOCIStats()

	if !got.Available {
		t.Error("Available = false although docker responded")
	}
	if got.ImagesCount != 12 || got.ImagesSize != "1.5GB" {
		t.Errorf("images = %d/%q, want 12/\"1.5GB\"", got.ImagesCount, got.ImagesSize)
	}
	if got.ContainersCount != 3 || got.ContainersSize != "250MB" {
		t.Errorf("containers = %d/%q, want 3/\"250MB\"", got.ContainersCount, got.ContainersSize)
	}
	// Docker labels the row "Local Volumes", not "Volumes".
	if got.VolumesCount != 7 || got.VolumesSize != "400MB" {
		t.Errorf("volumes = %d/%q, want 7/\"400MB\"", got.VolumesCount, got.VolumesSize)
	}
	// Le cache de build entrait dans la somme du récupérable, mais sa taille
	// propre était jetée — or c'est elle qui répond le plus souvent à « où est
	// passé le disque ».
	if got.BuildCacheSize != "2GB" {
		t.Errorf("BuildCacheSize = %q, want %q", got.BuildCacheSize, "2GB")
	}
}

// `docker system df` n'a pas de ligne pour les réseaux : ils ne portent pas
// d'octets, donc le rapport disque les ignore. Le compte vient d'un second
// appel, et une liste qui échoue ne doit pas emporter les tailles avec elle.
func TestFetchOCIStatsCountsTheNetworksSeparately(t *testing.T) {
	stub(t, &stubRunner{output: map[string][]byte{
		"system":  []byte("Images\t12\t1.5GB\n"),
		"network": []byte("abc\tbridge\tbridge\tlocal\n" + "def\thost\thost\tlocal\n"),
	}})

	if got := FetchOCIStats().NetworksCount; got != 2 {
		t.Errorf("NetworksCount = %d, want 2", got)
	}
}

func TestAFailingNetworkListStillLeavesTheSizes(t *testing.T) {
	stub(t, &stubRunner{
		output: map[string][]byte{"system": []byte("Images\t12\t1.5GB\n")},
		err:    map[string]error{"network": errExit},
	})

	got := FetchOCIStats()
	if got.ImagesSize != "1.5GB" || !got.Available {
		t.Errorf("a failing network list dropped the disk report: %+v", got)
	}
	if got.NetworksCount != 0 {
		t.Errorf("NetworksCount = %d after a failing list, want 0", got.NetworksCount)
	}
}

// Le reclaimable est sommé sur les trois familles, et le pourcentage que Docker
// accole à chaque taille est relatif à sa propre famille : il ne survivrait pas
// à l'addition, donc il est écarté.
func TestFetchOCIStatsSumsTheReclaimableSpace(t *testing.T) {
	stubOutput(t, "system",
		"Images\t12\t1.5GB\t1.2GB (80%)\n"+
			"Containers\t3\t250MB\t250MB (100%)\n"+
			"Local Volumes\t7\t400MB\t0B (0%)\n"+
			"Build Cache\t20\t2GB\t2GB (100%)\n")

	// 1.2GB + 250MB + 0 + 2GB, en unités décimales comme docker system df.
	if got := FetchOCIStats().Reclaimable; got != "3.5GB" {
		t.Errorf("Reclaimable = %q, want %q", got, "3.5GB")
	}
}

// Un daemon qui ne rend pas la colonne — un format plus ancien — ne doit pas
// produire un "0B" qui se lirait comme « rien à récupérer ».
func TestAMissingReclaimableColumnIsNotZero(t *testing.T) {
	stubOutput(t, "system", "Images\t12\t1.5GB\nContainers\t3\t250MB\n")

	if got := FetchOCIStats().Reclaimable; got != "" {
		t.Errorf("Reclaimable = %q with no column in the output, want empty", got)
	}
}

// The dashboard renders these counters unconditionally, so a host without
// Docker must produce a zero value flagged unavailable rather than an error.
func TestFetchOCIStatsUnavailable(t *testing.T) {
	tests := []struct {
		name string
		s    *stubRunner
	}{
		{"docker missing", &stubRunner{missing: true}},
		{"docker fails", &stubRunner{err: map[string]error{"system": errors.New("daemon not running")}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub(t, tt.s)

			if got := FetchOCIStats(); got != (OCIStats{}) {
				t.Errorf("FetchOCIStats() = %+v, want the zero value", got)
			}
		})
	}
}

func TestPruneCommands(t *testing.T) {
	tests := []struct {
		name     string
		call     func() (string, error)
		key      string
		wantArgs []string
	}{
		{"containers", PruneContainers, "container", []string{"container", "prune", "-f"}},
		{"images", PruneImages, "image", []string{"image", "prune", "-f"}},
		{"networks", PruneNetworks, "network", []string{"network", "prune", "-f"}},
		{"volumes", PruneVolumes, "volume", []string{"volume", "prune", "-f"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := stubOutput(t, tt.key, "Total reclaimed space: 1.2GB\n")

			got, err := tt.call()

			if err != nil {
				t.Fatalf("prune returned error = %v", err)
			}
			if got != "Total reclaimed space: 1.2GB" {
				t.Errorf("report = %q, want it trimmed", got)
			}
			if !slices.Equal(s.lastArgs(), tt.wantArgs) {
				t.Errorf("args = %v, want %v", s.lastArgs(), tt.wantArgs)
			}
			// -f is what keeps prune from blocking on a confirmation prompt.
			if !slices.Contains(s.lastArgs(), "-f") {
				t.Errorf("args = %v, missing -f: prune would wait for confirmation", s.lastArgs())
			}
		})
	}
}
