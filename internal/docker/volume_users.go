package docker

import (
	"fmt"
	"slices"
	"strings"
)

// OC-22 (DEVDESK-820, criterion 2): which containers use which volume, stopped ones
// included. A volume a container references, running or not, is never eligible for
// removal: its data is the container's.
//
// `ps --format {{.Mounts}}` cannot answer it: podman prints the destinations, not the
// volume names, and docker cuts the names at 14 characters without --no-trunc (probe
// of 2026-11 on docker 28.5.2 and 29.10.1, podman 5.8.11 and 6.2.1). The answer comes
// from `inspect`, in one grouped call.

// Mount is one entry of a container's `Mounts` as `inspect` prints it. Only Type
// "volume" with a Name is a volume; a bind mount has an empty Name, and tmpfs and the
// others are not volumes either. Nothing is deduced from the position of a mount in
// the list: its order is not stable between versions.
type Mount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
}

// ContainerMounts is what the inspect of one container says about its mounts.
type ContainerMounts struct {
	ID   string `json:"Id"`
	Name string `json:"Name"` // docker prints it with a leading "/", ListVolumeUsers drops it
	// Mounts is a list: any other JSON shape fails the decoding.
	Mounts []Mount `json:"Mounts"`
}

// VolumeUsers maps a volume name to the names of the containers that mount it,
// whatever their state: sorted, without duplicates, named and anonymous volumes
// alike, never a bind mount.
func VolumeUsers(containers []ContainerMounts) map[string][]string {
	users := map[string][]string{}
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type != "volume" || m.Name == "" {
				continue
			}
			users[m.Name] = append(users[m.Name], c.Name)
		}
	}
	for volume, names := range users {
		slices.Sort(names)
		users[volume] = slices.Compact(names)
	}
	return users
}

// UnusedVolumes keeps, in order, the volumes nobody uses.
func UnusedVolumes(volumes []Volume, users map[string][]string) []Volume {
	var unused []Volume
	for _, v := range volumes {
		if len(users[v.Name]) == 0 {
			unused = append(unused, v)
		}
	}
	return unused
}

// ListVolumeUsers is the volume part of ListContainerUsage (OC-25), the same read
// `D` on an image or a network uses: ONE `ps -a -q --no-trunc` and, when there is at
// least one container, ONE grouped `inspect`, never a call per volume or per
// container, and the same failures: an error and no map, never "unused".
func ListVolumeUsers() (map[string][]string, error) {
	u, err := ListContainerUsage()
	if err != nil {
		return nil, err
	}
	return u.volumes, nil
}

// parseContainerIDs reads the IDs `ps -q` printed, one per line, each validated and
// the repeated ones kept once. Any other text is an error.
func parseContainerIDs(out []byte) ([]string, error) {
	var ids []string
	for _, line := range splitLines(out) {
		id := strings.TrimSpace(line)
		if !IsContainerID(id) {
			return nil, fmt.Errorf("%s: unreadable output: %q is not a container ID", cmdLabel("ps"), id)
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
