package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// OC-25 (DEVDESK-823): who uses what, read in one pass over every container, stopped
// ones included. `D` on an image, a volume or a network reads this snapshot and asks
// the engine for nothing; a view refreshes it with its lists.
//
// The shapes below are the ones measured on 2026-11-28 on docker 29.10.1 and 28.5.2
// and podman 6.2.1 and 5.8.11, rootful and rootless (docs/measurements/
// 2026-11-28-container-usage-shapes.md):
//
//   - the image is joined on `Image` of the inspect (an ID, with "sha256:" on docker and
//     without on podman), never on `Config.Image`, `ImageName` or a tag, and never
//     through `ps --filter ancestor=` (podman answers another question than docker);
//   - the volume is joined on `Mounts[].Name` of type "volume" (OC-22, VolumeUsers);
//   - the network is joined on the NAME, the key of `NetworkSettings.Networks`: its
//     NetworkID is empty for a docker container that was only created and is "none"
//     for podman's none network. `network inspect .Containers` lists the running
//     containers only and is never read.
//
// A shape that is not one of those is "unknown" for the kind it carries, never
// "unused", and does not take the other kinds down.

// usageDeadline bounds ListContainerUsage as a whole, as ImagesInUse is bounded.
const usageDeadline = 60 * time.Second

// usageTemplate prints the whole inspect of a container as one JSON object per line,
// the same text on both engines. A narrower template (`{"Id":{{json .Id}},...}`) does
// not work: podman rejects `.Id` ("can't evaluate field Id", exit 125), its template
// field is `.ID`, while the JSON it prints has `Id` on both engines.
//
// Probe 2026-11 on docker 29.10.1 and 28.5.2, podman 6.2.1 and 5.8.11 rootless:
// `Name` has a leading "/" on docker and none on podman; `Mounts[].Name` is present
// for named and anonymous volumes and absent for a bind mount; podman adds `Options`.
const usageTemplate = `{{json .}}`

// ContainerUsage is who uses what, read in one pass over every container, stopped
// ones included. The zero value is a usage that was never read: for images and
// networks it answers "unknown", never "unused".
type ContainerUsage struct {
	read       bool
	containers []usedContainer
	volumes    map[string][]string
}

// usedContainer is what one inspect says about the container, reduced to what a join
// reads. A kind that could not be read is flagged, not guessed.
type usedContainer struct {
	name string

	image      string // the ID, without "sha256:"
	imageKnown bool

	networks      []string // the keys of NetworkSettings.Networks
	networksKnown bool
}

// inspectedContainer is one line of the grouped inspect. Mounts is decoded strictly
// (a Mounts that is not a list fails the read, as it does for OC-22); the rest is kept
// raw and read tolerantly, so an unexpected Image or Networks makes one kind unknown
// without breaking the volumes.
type inspectedContainer struct {
	ContainerMounts
	Image           json.RawMessage `json:"Image"`
	NetworkSettings json.RawMessage `json:"NetworkSettings"`
	HostConfig      json.RawMessage `json:"HostConfig"`
}

// ListContainerUsage reads the usage with a deadline of its own, the one of ImagesInUse.
func ListContainerUsage() (ContainerUsage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), usageDeadline)
	defer cancel()
	return ListContainerUsageContext(ctx)
}

// ListContainerUsageContext reads every container, stopped ones included, with ONE
// `<engine> ps -a -q --no-trunc` and, when there is at least one, ONE grouped
// `<engine> inspect --format {{json .}} <id>...`: never a call per image, volume,
// network or container. The answers are joined to the IDs by Id, never by position
// (the measurement of 2026-11-28 shows the order of the arguments on docker and podman,
// but nothing may rely on it).
//
// It fails closed: an error of `ps`, an ID that is not a container ID, an error of
// `inspect` (a container that disappeared between the two calls included), an output
// that is empty or unreadable while IDs exist, one that does not answer exactly once
// for every ID asked, a Mounts that is not a list, or a ctx that is over (checked
// before each call), is an error and a usage that was never read. A caller never
// mistakes "could not tell" for "unused".
func ListContainerUsageContext(ctx context.Context) (ContainerUsage, error) {
	if err := engineAvailable(); err != nil {
		return ContainerUsage{}, err
	}
	if err := ctx.Err(); err != nil {
		return ContainerUsage{}, wrapErr(cmdLabel("ps"), err)
	}
	out, err := runner.Run(dockerCmd{Args: []string{"ps", "-a", "-q", "--no-trunc"}, Ctx: ctx})
	if err != nil {
		return ContainerUsage{}, readFailure(cmdLabel("ps"), err)
	}
	ids, err := parseContainerIDs(out)
	if err != nil {
		return ContainerUsage{}, err
	}
	if len(ids) == 0 {
		return ContainerUsage{read: true, volumes: map[string][]string{}}, nil
	}
	if err := ctx.Err(); err != nil {
		return ContainerUsage{}, wrapErr(cmdLabel("inspect"), err)
	}

	args := append([]string{"inspect", "--format", usageTemplate}, ids...)
	out, err = runner.Run(dockerCmd{Args: args, Ctx: ctx})
	if err != nil {
		return ContainerUsage{}, readFailure(cmdLabel("inspect"), err)
	}
	inspected, err := parseInspectedContainers(out, ids)
	if err != nil {
		return ContainerUsage{}, err
	}

	mounts := make([]ContainerMounts, 0, len(inspected))
	used := make([]usedContainer, 0, len(inspected))
	for _, c := range inspected {
		mounts = append(mounts, c.ContainerMounts)
		used = append(used, readUsedContainer(c))
	}
	return ContainerUsage{read: true, containers: used, volumes: VolumeUsers(mounts)}, nil
}

// ImageUsers names the containers created from exactly this image: the ID of their
// `Image` (with or without "sha256:") against the short or full ID of the list. Never
// an image built on it, which `rmi <id>` does not refuse. known is false when the
// usage was never read, when imageID is not an image ID, or when the image of any
// container could not be read (that container might be the user).
func (u ContainerUsage) ImageUsers(imageID string) (names []string, known bool) {
	if !u.read || !IsImageID(imageID) {
		return nil, false
	}
	for _, c := range u.containers {
		if !c.imageKnown {
			return nil, false
		}
		if SameImageID(c.image, imageID) {
			names = append(names, c.name)
		}
	}
	return sortedNames(names), true
}

// VolumeUsers names the containers that mount the volume, the OC-22 computation
// (VolumeUsers). Mounts is read strictly, so once the usage was read the answer is
// known, and a volume nobody mounts is known to be unused. known is false when the
// usage was never read (the zero value, or the one returned with an error) or when name
// is empty: nil names are then "could not tell", never "unused".
func (u ContainerUsage) VolumeUsers(name string) (names []string, known bool) {
	if !u.read || name == "" {
		return nil, false
	}
	return u.volumes[name], true
}

// NetworkUsers names the containers connected to the network, joined on its NAME.
// known is false when the usage was never read, when n has no name, or when the
// networks of any container could not be read.
func (u ContainerUsage) NetworkUsers(n Network) (names []string, known bool) {
	if !u.read || n.Name == "" {
		return nil, false
	}
	for _, c := range u.containers {
		if !c.networksKnown {
			return nil, false
		}
		if slices.Contains(c.networks, n.Name) {
			names = append(names, c.name)
		}
	}
	return sortedNames(names), true
}

// sortedNames sorts and de-duplicates; the result is never nil.
func sortedNames(names []string) []string {
	if names == nil {
		return []string{}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// readUsedContainer reads the image and the networks of one container, tolerantly.
func readUsedContainer(c inspectedContainer) usedContainer {
	u := usedContainer{name: c.Name}
	var image string
	if json.Unmarshal(c.Image, &image) == nil {
		image = strings.TrimPrefix(image, "sha256:")
		if IsImageID(image) {
			u.image, u.imageKnown = image, true
		}
	}
	u.networks, u.networksKnown = readNetworkNames(c.NetworkSettings, c.HostConfig)
	return u
}

// readNetworkNames reads the keys of NetworkSettings.Networks. A null or absent
// Networks is a fact, "on no managed network", only for a NetworkMode that manages
// none (rootless podman's pasta, or slirp4netns); with any other mode it is unknown.
// An empty object is "on no network" (`none` is a key, not an empty object), except for
// a container that shares the network of another one (NetworkMode `container:<id>`),
// whose own Networks says nothing: that is unknown. Not measured: that mode, `host`,
// macvlan and ipvlan, pods.
func readNetworkNames(settings, hostConfig json.RawMessage) (names []string, known bool) {
	var s struct {
		Networks json.RawMessage `json:"Networks"`
	}
	if !isJSONObject(settings) || json.Unmarshal(settings, &s) != nil {
		return nil, false
	}
	if len(s.Networks) == 0 || bytes.Equal(bytes.TrimSpace(s.Networks), []byte("null")) {
		var h struct {
			NetworkMode string `json:"NetworkMode"`
		}
		if !isJSONObject(hostConfig) || json.Unmarshal(hostConfig, &h) != nil {
			return nil, false
		}
		return nil, h.NetworkMode == "pasta" || h.NetworkMode == "slirp4netns"
	}
	var networks map[string]*struct {
		NetworkID string `json:"NetworkID"`
	}
	if !isJSONObject(s.Networks) || json.Unmarshal(s.Networks, &networks) != nil {
		return nil, false
	}
	if len(networks) == 0 && sharesNetworkOfAnother(hostConfig) {
		return nil, false
	}
	for name, entry := range networks {
		if entry == nil {
			return nil, false
		}
		names = append(names, name)
	}
	return names, true
}

// sharesNetworkOfAnother reports a NetworkMode of the form `container:<id>`; an unreadable
// HostConfig counts as one, the safe side.
func sharesNetworkOfAnother(hostConfig json.RawMessage) bool {
	var h struct {
		NetworkMode string `json:"NetworkMode"`
	}
	if !isJSONObject(hostConfig) || json.Unmarshal(hostConfig, &h) != nil {
		return true
	}
	return strings.HasPrefix(h.NetworkMode, "container:")
}

func isJSONObject(raw json.RawMessage) bool {
	return bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{"))
}

// parseInspectedContainers reads one JSON object per line and checks that it answers
// exactly once for every ID asked. An Id matches an asked ID when equal, or when it
// extends it (an engine that ignored --no-trunc printed short IDs). The name loses
// docker's leading "/" and, when empty, becomes the short ID.
func parseInspectedContainers(out []byte, asked []string) ([]inspectedContainer, error) {
	answered := make([]bool, len(asked))
	var containers []inspectedContainer
	for _, line := range splitLines(out) {
		var c inspectedContainer
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
