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
