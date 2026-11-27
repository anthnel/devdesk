package docker

import (
	"encoding/json"
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

// volumeUsersTemplate prints one JSON object per container, with the three keys the
// reader needs. The same on both engines; `{{json .}}` would read as well, the reader
// keeps only these three.
const volumeUsersTemplate = `{"Id":{{json .Id}},"Name":{{json .Name}},"Mounts":{{json .Mounts}}}`

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

// ListVolumeUsers reads every container, stopped ones included, with ONE
// `<engine> ps -a -q --no-trunc` and, when there is at least one, ONE grouped
// `<engine> inspect --format <template> <id>...`: never a call per volume or per
// container. The answers are joined to the IDs by Id, never by position (docker
// prints the containers in reverse order of creation, podman in order).
//
// It fails closed: an error of `ps`, an ID that is not a container ID, an error of
// `inspect` (a container that disappeared between the two calls included), an output
// that is empty or unreadable while IDs exist, one that does not answer exactly once
// for every ID asked, or a Mounts that is not a list, is an error and no map. A caller
// never mistakes "could not tell" for "unused".
func ListVolumeUsers() (map[string][]string, error) {
	if err := engineAvailable(); err != nil {
		return nil, err
	}
	out, err := dockerOutput("ps", "-a", "-q", "--no-trunc")
	if err != nil {
		return nil, wrapErr(cmdLabel("ps"), err)
	}
	ids, err := parseContainerIDs(out)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return map[string][]string{}, nil
	}

	args := append([]string{"inspect", "--format", volumeUsersTemplate}, ids...)
	out, err = dockerOutput(args...)
	if err != nil {
		return nil, wrapErr(cmdLabel("inspect"), err)
	}
	containers, err := parseContainerMounts(out, ids)
	if err != nil {
		return nil, err
	}
	return VolumeUsers(containers), nil
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

// parseContainerMounts reads one JSON object per line and checks that it answers
// exactly once for every ID asked. An Id matches an asked ID when equal, or when it
// extends it (an engine that ignored --no-trunc printed short IDs).
func parseContainerMounts(out []byte, asked []string) ([]ContainerMounts, error) {
	answered := make([]bool, len(asked))
	var containers []ContainerMounts
	for _, line := range splitLines(out) {
		var c ContainerMounts
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("%s: unreadable output: %w", cmdLabel("inspect"), err)
		}
		i := slices.IndexFunc(asked, func(id string) bool { return c.ID == id || (c.ID != "" && strings.HasPrefix(c.ID, id)) })
		if i < 0 || answered[i] {
			return nil, fmt.Errorf("%s: unexpected answer for container %q", cmdLabel("inspect"), c.ID)
		}
		answered[i] = true
		c.Name = strings.TrimPrefix(c.Name, "/")
		if c.Name == "" {
			c.Name = c.ID[:min(len(c.ID), minShortIDLen)]
		}
		containers = append(containers, c)
	}
	for i, ok := range answered {
		if !ok {
			return nil, fmt.Errorf("%s: no answer for container %s", cmdLabel("inspect"), asked[i])
		}
	}
	return containers, nil
}
